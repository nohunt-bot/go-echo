package main

import (
	"log"

	"github.com/ch/go_echo/config"
	"github.com/ch/go_echo/internal/infrastructure/cassandra"
	httpserver "github.com/ch/go_echo/internal/interfaces/http"
)

func main() {
	cfg := config.Load()

	session, err := cassandra.NewSession(cfg.Cassandra)
	if err != nil {
		log.Fatalf("cassandra: %v", err)
	}
	defer session.Close()

	e := httpserver.New(&session)
	e.Logger.Fatal(e.Start(":" + cfg.Port))
}
