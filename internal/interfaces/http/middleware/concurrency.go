package middleware

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// ConcurrencyLimiter rejects requests when more than max handlers are
// executing simultaneously, preventing resource exhaustion under burst traffic.
func ConcurrencyLimiter(max int) echo.MiddlewareFunc {
	sem := make(chan struct{}, max)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				return next(c)
			default:
				return c.JSON(http.StatusServiceUnavailable, map[string]interface{}{
					"success": false,
					"error":   "server is busy, please retry later",
				})
			}
		}
	}
}
