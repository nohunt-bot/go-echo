package application

import (
	"context"

	"github.com/ch/go_echo/internal/domain/user"
	"github.com/google/uuid"
)

type UserService struct {
	repo user.Repository
}

func NewUserService(repo user.Repository) *UserService {
	return &UserService{repo: repo}
}

func (s *UserService) ListUsers(ctx context.Context) ([]*user.User, error) {
	return s.repo.FindAll(ctx)
}

func (s *UserService) GetUser(ctx context.Context, id uuid.UUID) (*user.User, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *UserService) CreateUser(ctx context.Context, req *user.CreateRequest) (*user.User, error) {
	u := &user.User{Name: req.Name, Email: req.Email}
	return s.repo.Create(ctx, u)
}
