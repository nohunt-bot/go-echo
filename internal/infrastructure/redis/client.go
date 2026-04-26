package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/ch/go_echo/config"
	goredis "github.com/redis/go-redis/v9"
)

// NewClient returns a UniversalClient: standalone when Addrs has one entry,
// cluster mode when Addrs has multiple entries.
func NewClient(cfg config.RedisConfig) (goredis.UniversalClient, error) {
	client := goredis.NewUniversalClient(&goredis.UniversalOptions{
		Addrs:    cfg.Addrs,
		Password: cfg.Password,

		PoolSize:        cfg.PoolSize,
		MinIdleConns:    cfg.MinIdleConns,
		PoolTimeout:     cfg.PoolTimeout,
		ConnMaxIdleTime: cfg.ConnMaxIdleTime,
		ConnMaxLifetime: cfg.ConnMaxLifetime,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis: ping %v: %w", cfg.Addrs, err)
	}
	return client, nil
}
