package grpcapi

import (
	"context"
	"errors"
	"time"

	"distributed-cache/internal/cache"
	"distributed-cache/internal/cluster"
	cachev1 "distributed-cache/internal/gen/cache/v1"
	"distributed-cache/internal/obs"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	cachev1.UnimplementedCacheServiceServer
	cache *cache.Cache
	cl    *cluster.Cluster
}

func New(cacheImpl *cache.Cache) *Server {
	return &Server{cache: cacheImpl}
}

func NewClustered(cl *cluster.Cluster) *Server {
	return &Server{cache: cl.Local(), cl: cl}
}

func (s *Server) Get(ctx context.Context, req *cachev1.GetRequest) (*cachev1.GetResponse, error) {
	start := time.Now()
	item, err := func() (cache.Item, error) {
		if s.cl == nil || cluster.IsForwardedGRPC(ctx) {
			return s.cache.Get(ctx, req.GetKey())
		}
		it, err := s.cl.Get(ctx, req.GetKey())
		if err != nil {
			return cache.Item{}, err
		}
		return cache.Item{Value: it.Value, ExpiresAt: it.ExpiresAt}, nil
	}()
	obs.ObserveCache("get", start, err)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "key not found")
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	resp := &cachev1.GetResponse{Value: item.Value}
	if !item.ExpiresAt.IsZero() {
		resp.ExpiresAtUnixMs = item.ExpiresAt.UnixMilli()
	}
	return resp, nil
}

func (s *Server) Set(ctx context.Context, req *cachev1.SetRequest) (*cachev1.SetResponse, error) {
	start := time.Now()
	var ttl time.Duration
	if req.GetTtlMs() > 0 {
		ttl = time.Duration(req.GetTtlMs()) * time.Millisecond
	}
	if s.cl == nil || cluster.IsForwardedGRPC(ctx) {
		s.cache.Set(ctx, req.GetKey(), req.GetValue(), ttl)
	} else {
		s.cl.Set(ctx, req.GetKey(), req.GetValue(), ttl)
	}
	obs.ObserveCache("set", start, nil)
	return &cachev1.SetResponse{}, nil
}

func (s *Server) Delete(ctx context.Context, req *cachev1.DeleteRequest) (*cachev1.DeleteResponse, error) {
	start := time.Now()
	if s.cl == nil || cluster.IsForwardedGRPC(ctx) {
		s.cache.Delete(ctx, req.GetKey())
	} else {
		s.cl.Delete(ctx, req.GetKey())
	}
	obs.ObserveCache("delete", start, nil)
	return &cachev1.DeleteResponse{}, nil
}
