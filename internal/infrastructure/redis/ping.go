package redis

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"
)

type Pinger struct{ client *goredis.Client }

func NewPinger(client *goredis.Client) *Pinger { return &Pinger{client: client} }

func (p *Pinger) Ping(ctx context.Context) error {
	if err := p.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	return nil
}
