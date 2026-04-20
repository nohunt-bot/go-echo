package tool

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("tool not found")

type Repository interface {
	FindAll(ctx context.Context) ([]*Tool, error)
	FindByID(ctx context.Context, id uuid.UUID) (*Tool, error)
	Create(ctx context.Context, t *Tool) (*Tool, error)
}
