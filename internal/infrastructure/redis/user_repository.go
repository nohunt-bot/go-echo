package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domain "github.com/ch/go_echo/internal/domain/user"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

const (
	userPrefix = "user:"
	usersIndex = "users:index"
	userTTL    = 24 * time.Hour
)

type userRepository struct {
	client *goredis.Client
}

func NewUserRepository(client *goredis.Client) domain.Repository {
	return &userRepository{client: client}
}

func userKey(id uuid.UUID) string { return userPrefix + id.String() }

func (r *userRepository) FindAll(ctx context.Context) ([]*domain.User, error) {
	var ids []string
	if err := withRetry(ctx, func() error {
		var e error
		ids, e = r.client.SMembers(ctx, usersIndex).Result()
		return e
	}); err != nil {
		return nil, fmt.Errorf("users.FindAll: %w", err)
	}
	if len(ids) == 0 {
		return []*domain.User{}, nil
	}

	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = userPrefix + id
	}

	var vals []interface{}
	if err := withRetry(ctx, func() error {
		var e error
		vals, e = r.client.MGet(ctx, keys...).Result()
		return e
	}); err != nil {
		return nil, fmt.Errorf("users.FindAll: %w", err)
	}

	out := make([]*domain.User, 0, len(vals))
	var stale []interface{}
	for i, v := range vals {
		if v == nil {
			stale = append(stale, ids[i])
			continue
		}
		var u domain.User
		if err := json.Unmarshal([]byte(v.(string)), &u); err != nil {
			return nil, fmt.Errorf("users.FindAll unmarshal: %w", err)
		}
		out = append(out, &u)
	}
	if len(stale) > 0 {
		r.client.SRem(ctx, usersIndex, stale...)
	}
	return out, nil
}

func (r *userRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	var val string
	err := withRetry(ctx, func() error {
		var e error
		val, e = r.client.Get(ctx, userKey(id)).Result()
		return e
	})
	if errors.Is(err, goredis.Nil) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("users.FindByID: %w", err)
	}

	var u domain.User
	if err := json.Unmarshal([]byte(val), &u); err != nil {
		return nil, fmt.Errorf("users.FindByID unmarshal: %w", err)
	}
	return &u, nil
}

func (r *userRepository) Create(ctx context.Context, u *domain.User) (*domain.User, error) {
	u.ID = uuid.New()
	data, err := json.Marshal(u)
	if err != nil {
		return nil, fmt.Errorf("users.Create marshal: %w", err)
	}

	if err := withRetry(ctx, func() error {
		pipe := r.client.TxPipeline()
		pipe.Set(ctx, userKey(u.ID), data, userTTL)
		pipe.SAdd(ctx, usersIndex, u.ID.String())
		_, e := pipe.Exec(ctx)
		return e
	}); err != nil {
		return nil, fmt.Errorf("users.Create: %w", err)
	}
	return u, nil
}
