package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"distributed-cache/internal/cache"
	"distributed-cache/internal/cluster"
	"distributed-cache/internal/obs"

	"github.com/go-chi/chi/v5"
)

const forwardedHeader = "X-Cache-Forwarded"

type Server struct {
	cache *cache.Cache
	cl    *cluster.Cluster
}

type setRequest struct {
	Value      string `json:"value"`
	TTLms      *int64 `json:"ttl_ms,omitempty"`
	TTLSeconds *int64 `json:"ttl_seconds,omitempty"`
}

type getResponse struct {
	Value           string `json:"value"`
	ExpiresAtUnixMs int64  `json:"expires_at_unix_ms,omitempty"`
}

func New(cacheImpl *cache.Cache) *Server {
	return &Server{cache: cacheImpl}
}

func NewClustered(cl *cluster.Cluster) *Server {
	return &Server{cache: cl.Local(), cl: cl}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Get("/{key}", s.handleGet)
	r.Post("/{key}", s.handleSet)
	r.Delete("/{key}", s.handleDelete)
	r.Handle("/metrics", obs.MetricsHandler())

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return r
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	key := chi.URLParam(r, "key")

	item, err := func() (cache.Item, error) {
		if s.cl == nil || r.Header.Get(forwardedHeader) == "1" {
			return s.cache.Get(r.Context(), key)
		}
		it, err := s.cl.Get(r.Context(), key)
		if err != nil {
			return cache.Item{}, err
		}
		return cache.Item{Value: it.Value, ExpiresAt: it.ExpiresAt}, nil
	}()
	obs.ObserveCache("get", start, err)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	resp := getResponse{Value: base64.StdEncoding.EncodeToString(item.Value)}
	if !item.ExpiresAt.IsZero() {
		resp.ExpiresAtUnixMs = item.ExpiresAt.UnixMilli()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSet(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	key := chi.URLParam(r, "key")

	var req setRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	value, err := base64.StdEncoding.DecodeString(req.Value)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var ttl time.Duration
	if req.TTLms != nil {
		ttl = time.Duration(*req.TTLms) * time.Millisecond
	} else if req.TTLSeconds != nil {
		ttl = time.Duration(*req.TTLSeconds) * time.Second
	} else if q := r.URL.Query().Get("ttl_ms"); q != "" {
		ms, err := strconv.ParseInt(q, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		ttl = time.Duration(ms) * time.Millisecond
	}

	if s.cl == nil || r.Header.Get(forwardedHeader) == "1" {
		s.cache.Set(r.Context(), key, value, ttl)
	} else {
		s.cl.Set(r.Context(), key, value, ttl)
	}
	obs.ObserveCache("set", start, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	key := chi.URLParam(r, "key")
	if s.cl == nil || r.Header.Get(forwardedHeader) == "1" {
		s.cache.Delete(r.Context(), key)
	} else {
		s.cl.Delete(r.Context(), key)
	}
	obs.ObserveCache("delete", start, nil)
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
