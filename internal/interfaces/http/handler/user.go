package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/ch/go_echo/internal/domain/user"
	"github.com/ch/go_echo/pkg/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type userService interface {
	ListUsers(ctx context.Context) ([]*user.User, error)
	GetUser(ctx context.Context, id uuid.UUID) (*user.User, error)
	CreateUser(ctx context.Context, req *user.CreateRequest) (*user.User, error)
}

type UserHandler struct {
	svc userService
}

func NewUserHandler(svc userService) *UserHandler {
	return &UserHandler{svc: svc}
}

func (h *UserHandler) List(c echo.Context) error {
	users, err := h.svc.ListUsers(c.Request().Context())
	if err != nil {
		return response.InternalError(c, err.Error())
	}
	return response.OK(c, users)
}

func (h *UserHandler) Get(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.BadRequest(c, "invalid id: must be a UUID")
	}
	u, err := h.svc.GetUser(c.Request().Context(), id)
	if errors.Is(err, user.ErrNotFound) {
		return response.NotFound(c, "user not found")
	}
	if err != nil {
		return response.InternalError(c, err.Error())
	}
	return response.OK(c, u)
}

func (h *UserHandler) Create(c echo.Context) error {
	var req user.CreateRequest
	if err := c.Bind(&req); err != nil {
		return response.BadRequest(c, err.Error())
	}
	if req.Name == "" || req.Email == "" {
		return response.BadRequest(c, "name and email are required")
	}
	created, err := h.svc.CreateUser(c.Request().Context(), &req)
	if err != nil {
		return response.InternalError(c, err.Error())
	}
	return c.JSON(http.StatusCreated, response.Response{Success: true, Data: created})
}
