package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/ch/go_echo/config"
	"github.com/ch/go_echo/internal/infrastructure/cassandra"
	redisinfra "github.com/ch/go_echo/internal/infrastructure/redis"
	httpserver "github.com/ch/go_echo/internal/interfaces/http"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg := config.Load()

	redisClient, err := redisinfra.NewClient(cfg.Redis)
	if err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	defer redisClient.Close()

	session, err := cassandra.NewSession(cfg.Cassandra)
	if err != nil {
		return fmt.Errorf("cassandra: %w", err)
	}
	defer session.Close()

	e := httpserver.New(cfg.Server, &session, redisClient)

	serverErr := make(chan error, 1)
	go func() {
		if err := e.Start(":" + cfg.Port); !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return fmt.Errorf("server: %w", err)
	case <-quit:
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	// defer session.Close() and defer redisClient.Close() run here, in LIFO order
	return nil
}
