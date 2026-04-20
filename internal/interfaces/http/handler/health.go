package handler

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type HealthHandler struct {
	pingers map[string]Pinger
}

func NewHealthHandler(pingers map[string]Pinger) *HealthHandler {
	return &HealthHandler{pingers: pingers}
}

func (h *HealthHandler) Check(c echo.Context) error {
	ctx := c.Request().Context()
	status := make(map[string]string, len(h.pingers)+1)
	healthy := true

	for name, p := range h.pingers {
		if err := p.Ping(ctx); err != nil {
			status[name] = err.Error()
			healthy = false
		} else {
			status[name] = "ok"
		}
	}

	code := http.StatusOK
	status["status"] = "healthy"
	if !healthy {
		code = http.StatusServiceUnavailable
		status["status"] = "unhealthy"
	}
	return c.JSON(code, status)
}
