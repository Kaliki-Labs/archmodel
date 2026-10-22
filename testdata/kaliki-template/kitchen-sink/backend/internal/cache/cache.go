// Package cache provides the Redis client used for request-time caching.
package cache

import (
	"context"

	"github.com/redis/go-redis/v9"

	"github.com/example/kitchen-sink-app/backend/internal/config"
)

type Cache struct {
	Client *redis.Client
}

// Connect parses the redis URL and verifies connectivity.
func Connect(ctx context.Context, cfg config.RedisConfig) (*Cache, error) {
	opts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &Cache{Client: client}, nil
}

func (c *Cache) Close() error { return c.Client.Close() }
