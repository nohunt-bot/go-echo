package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Port      string
	Env       string
	Server    ServerConfig
	Cassandra CassandraConfig
	Redis     RedisConfig
}

type ServerConfig struct {
	RequestTimeout     time.Duration
	ShutdownTimeout    time.Duration
	MaxConcurrent      int
	RateLimitPerSecond int
}

type CassandraConfig struct {
	Hosts    []string
	Keyspace string
	Username string
	Password string
	NumConns int // connections per host
}

type RedisConfig struct {
	Addrs           []string
	Password        string
	PoolSize        int
	MinIdleConns    int
	PoolTimeout     time.Duration
	ConnMaxIdleTime time.Duration
	ConnMaxLifetime time.Duration
}

func Load() *Config {
	return &Config{
		Port: envOr("PORT", "8080"),
		Env:  envOr("APP_ENV", "development"),
		Server: ServerConfig{
			RequestTimeout:     parseDuration(envOr("REQUEST_TIMEOUT", "30s")),
			ShutdownTimeout:    parseDuration(envOr("SHUTDOWN_TIMEOUT", "30s")),
			MaxConcurrent:      parseInt(envOr("MAX_CONCURRENT", "100")),
			RateLimitPerSecond: parseInt(envOr("RATE_LIMIT_PER_SEC", "20")),
		},
		Cassandra: CassandraConfig{
			Hosts:    strings.Split(envOr("CASSANDRA_HOSTS", "127.0.0.1"), ","),
			Keyspace: envOr("CASSANDRA_KEYSPACE", "go_echo"),
			Username: os.Getenv("CASSANDRA_USERNAME"),
			Password: os.Getenv("CASSANDRA_PASSWORD"),
			NumConns: parseInt(envOr("CASSANDRA_NUM_CONNS", "2")),
		},
		Redis: RedisConfig{
			Addrs:           strings.Split(envOr("REDIS_ADDRS", "127.0.0.1:6379"), ","),
			Password:        os.Getenv("REDIS_PASSWORD"),
			PoolSize:        parseInt(envOr("REDIS_POOL_SIZE", "10")),
			MinIdleConns:    parseInt(envOr("REDIS_MIN_IDLE_CONNS", "2")),
			PoolTimeout:     parseDuration(envOr("REDIS_POOL_TIMEOUT", "4s")),
			ConnMaxIdleTime: parseDuration(envOr("REDIS_CONN_MAX_IDLE_TIME", "5m")),
			ConnMaxLifetime: parseDuration(envOr("REDIS_CONN_MAX_LIFETIME", "1h")),
		},
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 30 * time.Second
	}
	return d
}

func parseInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	if n <= 0 {
		return 1
	}
	return n
}
