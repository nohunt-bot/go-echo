package middleware

import (
	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"
)

func RequestID() echo.MiddlewareFunc {
	return echomw.RequestIDWithConfig(echomw.RequestIDConfig{
		RequestIDHandler: func(c echo.Context, id string) {
			c.Set("request_id", id)
		},
	})
}
