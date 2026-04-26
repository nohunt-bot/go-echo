package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domain "github.com/ch/go_echo/internal/domain/tool"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

const (
	toolPrefix = "tool:"
	toolsIndex = "tools:index"
	toolTTL    = 24 * time.Hour
)

type toolRepository struct {
	client goredis.UniversalClient
}

func NewToolRepository(client goredis.UniversalClient) domain.Repository {
	return &toolRepository{client: client}
}

func toolKey(id uuid.UUID) string { return toolPrefix + id.String() }

func (r *toolRepository) FindAll(ctx context.Context) ([]*domain.Tool, error) {
	var ids []string
	if err := withRetry(ctx, func() error {
		var e error
		ids, e = r.client.SMembers(ctx, toolsIndex).Result()
		return e
	}); err != nil {
		return nil, fmt.Errorf("tools.FindAll: %w", err)
	}
	if len(ids) == 0 {
		return []*domain.Tool{}, nil
	}

	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = toolPrefix + id
	}

	var vals []interface{}
	if err := withRetry(ctx, func() error {
		var e error
		vals, e = r.client.MGet(ctx, keys...).Result()
		return e
	}); err != nil {
		return nil, fmt.Errorf("tools.FindAll: %w", err)
	}

	out := make([]*domain.Tool, 0, len(vals))
	var stale []interface{}
	for i, v := range vals {
		if v == nil {
			stale = append(stale, ids[i])
			continue
		}
		var t domain.Tool
		if err := json.Unmarshal([]byte(v.(string)), &t); err != nil {
			return nil, fmt.Errorf("tools.FindAll unmarshal: %w", err)
		}
		out = append(out, &t)
	}
	if len(stale) > 0 {
		r.client.SRem(ctx, toolsIndex, stale...)
	}
	return out, nil
}

func (r *toolRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Tool, error) {
	var val string
	err := withRetry(ctx, func() error {
		var e error
		val, e = r.client.Get(ctx, toolKey(id)).Result()
		return e
	})
	if errors.Is(err, goredis.Nil) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("tools.FindByID: %w", err)
	}

	var t domain.Tool
	if err := json.Unmarshal([]byte(val), &t); err != nil {
		return nil, fmt.Errorf("tools.FindByID unmarshal: %w", err)
	}
	return &t, nil
}

func (r *toolRepository) Create(ctx context.Context, t *domain.Tool) (*domain.Tool, error) {
	t.ID = uuid.New()
	data, err := json.Marshal(t)
	if err != nil {
		return nil, fmt.Errorf("tools.Create marshal: %w", err)
	}

	if err := withRetry(ctx, func() error {
		pipe := r.client.Pipeline()
		pipe.Set(ctx, toolKey(t.ID), data, toolTTL)
		pipe.SAdd(ctx, toolsIndex, t.ID.String())
		_, e := pipe.Exec(ctx)
		return e
	}); err != nil {
		return nil, fmt.Errorf("tools.Create: %w", err)
	}
	return t, nil
}
