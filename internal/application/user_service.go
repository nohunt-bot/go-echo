package application

import (
	"github.com/ch/go_echo/internal/domain/user"
	"github.com/google/uuid"
)

type UserService struct {
	repo user.Repository
}

func NewUserService(repo user.Repository) *UserService {
	return &UserService{repo: repo}
}

func (s *UserService) ListUsers() ([]*user.User, error) {
	return s.repo.FindAll()
}

func (s *UserService) GetUser(id uuid.UUID) (*user.User, error) {
	return s.repo.FindByID(id)
}

func (s *UserService) CreateUser(req *user.CreateRequest) (*user.User, error) {
	u := &user.User{Name: req.Name, Email: req.Email}
	return s.repo.Create(u)
}
