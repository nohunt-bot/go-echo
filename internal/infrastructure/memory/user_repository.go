package memory

import (
	"sync"

	"github.com/ch/go_echo/internal/domain/user"
	"github.com/google/uuid"
)

type userRepository struct {
	mu    sync.RWMutex
	store map[uuid.UUID]*user.User
}

func NewUserRepository() user.Repository {
	return &userRepository{store: make(map[uuid.UUID]*user.User)}
}

func (r *userRepository) FindAll() ([]*user.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*user.User, 0, len(r.store))
	for _, u := range r.store {
		out = append(out, u)
	}
	return out, nil
}

func (r *userRepository) FindByID(id uuid.UUID) (*user.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.store[id]
	if !ok {
		return nil, user.ErrNotFound
	}
	return u, nil
}

func (r *userRepository) Create(u *user.User) (*user.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u.ID = uuid.New()
	r.store[u.ID] = u
	return u, nil
}
