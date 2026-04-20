package application

import (
	"context"

	"github.com/ch/go_echo/internal/domain/tool"
	"github.com/google/uuid"
)

type ToolService struct {
	repo tool.Repository
}

func NewToolService(repo tool.Repository) *ToolService {
	return &ToolService{repo: repo}
}

func (s *ToolService) ListTools(ctx context.Context) ([]*tool.Tool, error) {
	return s.repo.FindAll(ctx)
}

func (s *ToolService) GetTool(ctx context.Context, id uuid.UUID) (*tool.Tool, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *ToolService) CreateTool(ctx context.Context, req *tool.CreateRequest) (*tool.Tool, error) {
	t := &tool.Tool{
		Name:        req.Name,
		Description: req.Description,
		Category:    req.Category,
		Status:      req.Status,
	}
	return s.repo.Create(ctx, t)
}
