package user

import (
	"errors"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("user not found")

type Repository interface {
	FindAll() ([]*User, error)
	FindByID(id uuid.UUID) (*User, error)
	Create(u *User) (*User, error)
}
