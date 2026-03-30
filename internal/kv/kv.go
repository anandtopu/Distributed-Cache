package kv

import (
	"context"
	"time"
)

type Item struct {
	Value     []byte
	ExpiresAt time.Time
}

type Store interface {
	Get(ctx context.Context, key string) (Item, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration)
	Delete(ctx context.Context, key string)
}
