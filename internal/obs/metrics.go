package obs

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	CacheOpsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_ops_total",
		Help: "Total cache operations executed (local or routed).",
	}, []string{"op", "result"})

	CacheOpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cache_op_duration_seconds",
		Help:    "Latency of cache operations.",
		Buckets: prometheus.DefBuckets,
	}, []string{"op"})

	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests.",
	}, []string{"method", "path", "status"})

	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request duration.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	GRPCRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "grpc_requests_total",
		Help: "Total gRPC requests.",
	}, []string{"method", "code"})

	GRPCRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "grpc_request_duration_seconds",
		Help:    "gRPC request duration.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method"})
)

func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

func ObserveCache(op string, start time.Time, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	CacheOpsTotal.WithLabelValues(op, result).Inc()
	CacheOpDuration.WithLabelValues(op).Observe(time.Since(start).Seconds())
}
