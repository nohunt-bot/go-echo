package httpserver

import (
	"github.com/ch/go_echo/internal/application"
	"github.com/ch/go_echo/internal/infrastructure/cassandra"
	"github.com/ch/go_echo/internal/infrastructure/memory"
	"github.com/ch/go_echo/internal/interfaces/http/handler"
	"github.com/ch/go_echo/internal/interfaces/http/middleware"
	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"
	"github.com/scylladb/gocqlx/v2"
)

func New(session *gocqlx.Session) *echo.Echo {
	e := echo.New()

	e.Use(echomw.Logger())
	e.Use(echomw.Recover())
	e.Use(middleware.RequestID())

	registerRoutes(e, session)
	return e
}

func registerRoutes(e *echo.Echo, session *gocqlx.Session) {
	var userSvc *application.UserService
	if session != nil {
		userSvc = application.NewUserService(cassandra.NewUserRepository(*session))
	} else {
		userSvc = application.NewUserService(memory.NewUserRepository())
	}

	h := handler.NewUserHandler(userSvc)

	api := e.Group("/api/v1")
	api.GET("/users", h.List)
	api.GET("/users/:id", h.Get)
	api.POST("/users", h.Create)
}
