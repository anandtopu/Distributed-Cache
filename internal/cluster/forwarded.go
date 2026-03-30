package cluster

import (
	"context"

	"google.golang.org/grpc/metadata"
)

func IsForwardedGRPC(ctx context.Context) bool {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return false
	}
	vals := md.Get(forwardedMetadataKey)
	return len(vals) > 0 && vals[0] == "1"
}
