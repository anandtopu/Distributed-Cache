package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"distributed-cache/internal/cache"
	"distributed-cache/internal/cluster"
	cachev1 "distributed-cache/internal/gen/cache/v1"
	"distributed-cache/internal/obs"
	"distributed-cache/internal/transport/grpcapi"
	"distributed-cache/internal/transport/httpapi"

	"google.golang.org/grpc"
)

func main() {
	var (
		httpAddr   = flag.String("http", ":8080", "HTTP listen address")
		grpcAddr   = flag.String("grpc", ":9090", "gRPC listen address")
		capacity   = flag.Int("capacity", 100_000, "max number of items")
		cleanupInt = flag.Duration("cleanup-interval", 30*time.Second, "expired entry cleanup interval")
		cleanupMax = flag.Int("cleanup-max-scan", 50_000, "max entries to scan per cleanup run")

		nodeID      = flag.String("node-id", "node1", "cluster node id")
		nodesSpec   = flag.String("nodes", "", "cluster nodes as id=host:port,id2=host:port")
		vnodes      = flag.Int("vnodes", 100, "consistent hashing virtual nodes per node")
		replication = flag.Int("replication", 1, "replication factor")
	)
	flag.Parse()

	logger := obs.NewJSONLogger(slog.LevelInfo)
	slog.SetDefault(logger)

	c := cache.New(*capacity)
	var cl *cluster.Cluster
	if *nodesSpec != "" {
		nodes, err := cluster.ParseNodes(*nodesSpec)
		if err != nil {
			logger.Error("parse nodes", slog.Any("error", err))
			os.Exit(1)
		}
		ring, err := cluster.NewRing(nodes, *vnodes)
		if err != nil {
			logger.Error("new ring", slog.Any("error", err))
			os.Exit(1)
		}
		cl = cluster.New(*nodeID, c, ring, *replication)
		defer cl.Close()
		logger.Info("cluster enabled", slog.String("node_id", *nodeID), slog.Int("replication", *replication), slog.Int("nodes", len(nodes)))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go startJanitor(ctx, c, *cleanupInt, *cleanupMax)

	var httpHandler http.Handler
	if cl != nil {
		httpHandler = httpapi.NewClustered(cl).Router()
	} else {
		httpHandler = httpapi.New(c).Router()
	}
	httpHandler = obs.HTTPMetricsMiddleware(httpHandler)
	httpHandler = obs.HTTPAccessLogMiddleware(logger)(httpHandler)
	httpSrv := &http.Server{Addr: *httpAddr, Handler: httpHandler}
	go func() {
		logger.Info("http listening", slog.String("addr", *httpAddr))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", slog.Any("error", err))
			cancel()
		}
	}()

	lis, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		logger.Error("grpc listen", slog.Any("error", err))
		os.Exit(1)
	}
	grpcSrv := grpc.NewServer(grpc.UnaryInterceptor(obs.GRPCUnaryServerInterceptor))
	if cl != nil {
		cachev1.RegisterCacheServiceServer(grpcSrv, grpcapi.NewClustered(cl))
	} else {
		cachev1.RegisterCacheServiceServer(grpcSrv, grpcapi.New(c))
	}
	go func() {
		logger.Info("grpc listening", slog.String("addr", *grpcAddr))
		if err := grpcSrv.Serve(lis); err != nil {
			logger.Error("grpc server error", slog.Any("error", err))
			cancel()
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
		logger.Info("shutdown signal received")
	case <-ctx.Done():
		logger.Info("context canceled")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	grpcSrv.GracefulStop()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func startJanitor(ctx context.Context, c *cache.Cache, interval time.Duration, maxScan int) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = c.CleanupExpired(time.Now(), maxScan)
		}
	}
}
