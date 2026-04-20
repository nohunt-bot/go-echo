package tool

import (
	"context"
	"sync"

	domain "github.com/ch/go_echo/internal/domain/tool"
	"github.com/google/uuid"
)

type repository struct {
	mu    sync.RWMutex
	store map[uuid.UUID]*domain.Tool
}

func NewRepository() domain.Repository {
	return &repository{store: make(map[uuid.UUID]*domain.Tool)}
}

func (r *repository) FindAll(_ context.Context) ([]*domain.Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*domain.Tool, 0, len(r.store))
	for _, t := range r.store {
		out = append(out, t)
	}
	return out, nil
}

func (r *repository) FindByID(_ context.Context, id uuid.UUID) (*domain.Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.store[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return t, nil
}

func (r *repository) Create(_ context.Context, t *domain.Tool) (*domain.Tool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t.ID = uuid.New()
	r.store[t.ID] = t
	return t, nil
}
