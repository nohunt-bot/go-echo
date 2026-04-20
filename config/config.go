package config

import (
	"os"
	"strings"
)

type Config struct {
	Port      string
	Env       string
	Cassandra CassandraConfig
}

type CassandraConfig struct {
	Hosts    []string
	Keyspace string
	Username string
	Password string
}

func Load() *Config {
	return &Config{
		Port: envOr("PORT", "8080"),
		Env:  envOr("APP_ENV", "development"),
		Cassandra: CassandraConfig{
			Hosts:    strings.Split(envOr("CASSANDRA_HOSTS", "127.0.0.1"), ","),
			Keyspace: envOr("CASSANDRA_KEYSPACE", "go_echo"),
			Username: os.Getenv("CASSANDRA_USERNAME"),
			Password: os.Getenv("CASSANDRA_PASSWORD"),
		},
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
