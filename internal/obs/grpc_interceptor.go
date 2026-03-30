package obs

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

func GRPCUnaryServerInterceptor(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	st := status.Convert(err)
	GRPCRequestsTotal.WithLabelValues(info.FullMethod, st.Code().String()).Inc()
	GRPCRequestDuration.WithLabelValues(info.FullMethod).Observe(time.Since(start).Seconds())
	return resp, err
}
