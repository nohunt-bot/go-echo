package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/ch/go_echo/internal/domain/tool"
	"github.com/ch/go_echo/pkg/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type toolService interface {
	ListTools(ctx context.Context) ([]*tool.Tool, error)
	GetTool(ctx context.Context, id uuid.UUID) (*tool.Tool, error)
	CreateTool(ctx context.Context, req *tool.CreateRequest) (*tool.Tool, error)
}

type ToolHandler struct {
	svc toolService
}

func NewToolHandler(svc toolService) *ToolHandler {
	return &ToolHandler{svc: svc}
}

func (h *ToolHandler) List(c echo.Context) error {
	tools, err := h.svc.ListTools(c.Request().Context())
	if err != nil {
		return response.InternalError(c, err.Error())
	}
	return response.OK(c, tools)
}

func (h *ToolHandler) Get(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.BadRequest(c, "invalid id: must be a UUID")
	}
	t, err := h.svc.GetTool(c.Request().Context(), id)
	if errors.Is(err, tool.ErrNotFound) {
		return response.NotFound(c, "tool not found")
	}
	if err != nil {
		return response.InternalError(c, err.Error())
	}
	return response.OK(c, t)
}

func (h *ToolHandler) Create(c echo.Context) error {
	var req tool.CreateRequest
	if err := c.Bind(&req); err != nil {
		return response.BadRequest(c, err.Error())
	}
	if req.Name == "" || req.Category == "" || req.Status == "" {
		return response.BadRequest(c, "name, category and status are required")
	}
	created, err := h.svc.CreateTool(c.Request().Context(), &req)
	if err != nil {
		return response.InternalError(c, err.Error())
	}
	return c.JSON(http.StatusCreated, response.Response{Success: true, Data: created})
}
