package httpserver

import (
	"net/http"
	"time"

	"github.com/ch/go_echo/config"
	"github.com/ch/go_echo/internal/application"
	"github.com/ch/go_echo/internal/infrastructure/cassandra"
	redisinfra "github.com/ch/go_echo/internal/infrastructure/redis"
	"github.com/ch/go_echo/internal/interfaces/http/handler"
	"github.com/ch/go_echo/internal/interfaces/http/middleware"
	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"
	goredis "github.com/redis/go-redis/v9"
	"github.com/scylladb/gocqlx/v2"
	"golang.org/x/time/rate"
)

func New(cfg config.ServerConfig, session *gocqlx.Session, redisClient goredis.UniversalClient) *echo.Echo {
	e := echo.New()

	e.Use(echomw.Logger())
	e.Use(echomw.Recover())
	e.Use(middleware.RequestID())
	e.Use(echomw.RateLimiter(echomw.NewRateLimiterMemoryStoreWithConfig(
		echomw.RateLimiterMemoryStoreConfig{
			Rate:      rate.Limit(cfg.RateLimitPerSecond),
			Burst:     cfg.RateLimitPerSecond * 3,
			ExpiresIn: 3 * time.Minute,
		},
	)))
	e.Use(middleware.ConcurrencyLimiter(cfg.MaxConcurrent))
	e.Use(echomw.ContextTimeoutWithConfig(echomw.ContextTimeoutConfig{
		Timeout: cfg.RequestTimeout,
		ErrorHandler: func(err error, c echo.Context) error {
			return c.JSON(http.StatusServiceUnavailable, map[string]interface{}{
				"success": false,
				"error":   "request timeout",
			})
		},
	}))

	registerRoutes(e, session, redisClient)
	return e
}

func registerRoutes(e *echo.Echo, session *gocqlx.Session, redisClient goredis.UniversalClient) {
	// health check (no rate limit / timeout middleware applied)
	pingers := map[string]handler.Pinger{
		"redis": redisinfra.NewPinger(redisClient),
	}
	if session != nil {
		pingers["cassandra"] = cassandra.NewPinger(*session)
	}
	e.GET("/health", handler.NewHealthHandler(pingers).Check)

	api := e.Group("/api/v1")

	// users
	var userSvc *application.UserService
	if session != nil {
		userSvc = application.NewUserService(cassandra.NewUserRepository(*session))
	} else {
		userSvc = application.NewUserService(redisinfra.NewUserRepository(redisClient))
	}
	userH := handler.NewUserHandler(userSvc)
	api.GET("/users", userH.List)
	api.GET("/users/:id", userH.Get)
	api.POST("/users", userH.Create)

	// tools
	var toolSvc *application.ToolService
	if session != nil {
		toolSvc = application.NewToolService(cassandra.NewToolRepository(*session))
	} else {
		toolSvc = application.NewToolService(redisinfra.NewToolRepository(redisClient))
	}
	toolH := handler.NewToolHandler(toolSvc)
	api.GET("/tools", toolH.List)
	api.GET("/tools/:id", toolH.Get)
	api.POST("/tools", toolH.Create)
}
