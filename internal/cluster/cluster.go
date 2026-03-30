package cluster

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"distributed-cache/internal/cache"
	cachev1 "distributed-cache/internal/gen/cache/v1"
	"distributed-cache/internal/kv"

	"github.com/cespare/xxhash/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const forwardedMetadataKey = "x-cache-forwarded"

type Node struct {
	ID       string
	GRPCAddr string
}

type ringPoint struct {
	h uint64
	n int
}

type Ring struct {
	nodes  []Node
	points []ringPoint
}

func NewRing(nodes []Node, vnodes int) (*Ring, error) {
	if len(nodes) == 0 {
		return nil, errors.New("no nodes")
	}
	if vnodes <= 0 {
		vnodes = 100
	}

	pts := make([]ringPoint, 0, len(nodes)*vnodes)
	for i, n := range nodes {
		for v := 0; v < vnodes; v++ {
			h := xxhash.Sum64String(fmt.Sprintf("%s#%d", n.ID, v))
			pts = append(pts, ringPoint{h: h, n: i})
		}
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].h < pts[j].h })

	return &Ring{nodes: append([]Node(nil), nodes...), points: pts}, nil
}

func (r *Ring) ReplicasForKey(key string, repl int) []Node {
	if repl <= 0 {
		repl = 1
	}
	if repl > len(r.nodes) {
		repl = len(r.nodes)
	}

	h := xxhash.Sum64String(key)
	i := sort.Search(len(r.points), func(i int) bool { return r.points[i].h >= h })
	if i == len(r.points) {
		i = 0
	}

	out := make([]Node, 0, repl)
	seen := make(map[int]struct{}, repl)
	for j := 0; len(out) < repl && j < len(r.points); j++ {
		p := r.points[(i+j)%len(r.points)]
		if _, ok := seen[p.n]; ok {
			continue
		}
		seen[p.n] = struct{}{}
		out = append(out, r.nodes[p.n])
	}
	return out
}

type Cluster struct {
	localID string
	local   *cache.Cache
	ring    *Ring
	repl    int

	dialTimeout time.Duration
	callTimeout time.Duration

	mu      sync.Mutex
	clients map[string]cachev1.CacheServiceClient
	conns   map[string]*grpc.ClientConn
}

func ParseNodes(spec string) ([]Node, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("nodes spec is empty")
	}

	parts := strings.Split(spec, ",")
	nodes := make([]Node, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kvp := strings.SplitN(p, "=", 2)
		if len(kvp) != 2 {
			return nil, fmt.Errorf("invalid node spec %q, expected id=host:port", p)
		}
		id := strings.TrimSpace(kvp[0])
		addr := strings.TrimSpace(kvp[1])
		if id == "" || addr == "" {
			return nil, fmt.Errorf("invalid node spec %q", p)
		}
		if _, ok := seen[id]; ok {
			return nil, fmt.Errorf("duplicate node id %q", id)
		}
		seen[id] = struct{}{}
		nodes = append(nodes, Node{ID: id, GRPCAddr: addr})
	}
	if len(nodes) == 0 {
		return nil, errors.New("no nodes parsed")
	}
	return nodes, nil
}

func New(localID string, local *cache.Cache, ring *Ring, repl int) *Cluster {
	return &Cluster{
		localID:     localID,
		local:       local,
		ring:        ring,
		repl:        repl,
		dialTimeout: 2 * time.Second,
		callTimeout: 300 * time.Millisecond,
		clients:     map[string]cachev1.CacheServiceClient{},
		conns:       map[string]*grpc.ClientConn{},
	}
}

func (c *Cluster) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cc := range c.conns {
		_ = cc.Close()
	}
	c.conns = map[string]*grpc.ClientConn{}
	c.clients = map[string]cachev1.CacheServiceClient{}
}

func (c *Cluster) Local() *cache.Cache { return c.local }

func (c *Cluster) Get(ctx context.Context, key string) (kv.Item, error) {
	replicas := c.ring.ReplicasForKey(key, c.repl)
	var lastErr error

	for _, n := range replicas {
		if n.ID == c.localID {
			item, err := c.local.Get(ctx, key)
			if err == nil {
				return kv.Item{Value: item.Value, ExpiresAt: item.ExpiresAt}, nil
			}
			lastErr = err
			continue
		}

		cli, err := c.clientForNode(ctx, n)
		if err != nil {
			lastErr = err
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, c.callTimeout)
		callCtx = metadata.AppendToOutgoingContext(callCtx, forwardedMetadataKey, "1")
		resp, err := cli.Get(callCtx, &cachev1.GetRequest{Key: key})
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		var exp time.Time
		if resp.GetExpiresAtUnixMs() > 0 {
			exp = time.UnixMilli(resp.GetExpiresAtUnixMs())
		}
		return kv.Item{Value: resp.GetValue(), ExpiresAt: exp}, nil
	}

	if lastErr == nil {
		lastErr = cache.ErrNotFound
	}
	return kv.Item{}, lastErr
}

func (c *Cluster) Set(ctx context.Context, key string, value []byte, ttl time.Duration) {
	replicas := c.ring.ReplicasForKey(key, c.repl)
	if len(replicas) == 0 {
		return
	}

	// Best-effort replication with improved availability:
	// - Try to write to all replicas.
	// - Do the first successful replica synchronously so the caller doesn't depend
	//   on a single primary node being reachable.
	// - Replicate to the rest asynchronously.
	var wrote bool
	for _, n := range replicas {
		if n.ID == c.localID {
			c.local.Set(ctx, key, value, ttl)
			wrote = true
			break
		}
		cli, err := c.clientForNode(ctx, n)
		if err != nil {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, c.callTimeout)
		callCtx = metadata.AppendToOutgoingContext(callCtx, forwardedMetadataKey, "1")
		_, err = cli.Set(callCtx, &cachev1.SetRequest{Key: key, Value: value, TtlMs: ttl.Milliseconds()})
		cancel()
		if err == nil {
			wrote = true
			break
		}
	}

	if !wrote {
		// No reachable replicas; nothing else to do.
		return
	}

	for _, n := range replicas {
		n := n
		go func() {
			bg, cancel := context.WithTimeout(context.Background(), c.callTimeout)
			defer cancel()

			if n.ID == c.localID {
				c.local.Set(bg, key, value, ttl)
				return
			}
			cli, err := c.clientForNode(bg, n)
			if err != nil {
				return
			}
			bg = metadata.AppendToOutgoingContext(bg, forwardedMetadataKey, "1")
			_, _ = cli.Set(bg, &cachev1.SetRequest{Key: key, Value: value, TtlMs: ttl.Milliseconds()})
		}()
	}
}

func (c *Cluster) Delete(ctx context.Context, key string) {
	replicas := c.ring.ReplicasForKey(key, c.repl)
	for _, n := range replicas {
		if n.ID == c.localID {
			c.local.Delete(ctx, key)
			continue
		}
		cli, err := c.clientForNode(ctx, n)
		if err != nil {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, c.callTimeout)
		callCtx = metadata.AppendToOutgoingContext(callCtx, forwardedMetadataKey, "1")
		_, _ = cli.Delete(callCtx, &cachev1.DeleteRequest{Key: key})
		cancel()
	}
}

func (c *Cluster) clientForNode(ctx context.Context, n Node) (cachev1.CacheServiceClient, error) {
	c.mu.Lock()
	cli, ok := c.clients[n.ID]
	c.mu.Unlock()
	if ok {
		return cli, nil
	}

	dctx, cancel := context.WithTimeout(ctx, c.dialTimeout)
	defer cancel()
	cc, err := grpc.DialContext(dctx, n.GRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	newCli := cachev1.NewCacheServiceClient(cc)

	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.clients[n.ID]; ok {
		_ = cc.Close()
		return existing, nil
	}
	c.clients[n.ID] = newCli
	c.conns[n.ID] = cc
	return newCli, nil
}
