package cassandra

import (
	"fmt"
	"time"

	"github.com/ch/go_echo/config"
	"github.com/gocql/gocql"
	"github.com/scylladb/gocqlx/v2"
)

// NewSession creates a gocqlx session. Caller must call session.Close() on shutdown.
func NewSession(cfg config.CassandraConfig) (gocqlx.Session, error) {
	cluster := gocql.NewCluster(cfg.Hosts...)
	cluster.Keyspace = cfg.Keyspace
	cluster.Consistency = gocql.Quorum
	cluster.Timeout = 5 * time.Second
	cluster.ConnectTimeout = 10 * time.Second
	cluster.RetryPolicy = &gocql.ExponentialBackoffRetryPolicy{
		Min:        100 * time.Millisecond,
		Max:        5 * time.Second,
		NumRetries: 5,
	}
	cluster.PoolConfig.HostSelectionPolicy =
		gocql.TokenAwareHostPolicy(gocql.RoundRobinHostPolicy())

	if cfg.Username != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: cfg.Username,
			Password: cfg.Password,
		}
	}

	session, err := gocqlx.WrapSession(cluster.CreateSession())
	if err != nil {
		return gocqlx.Session{}, fmt.Errorf("cassandra: create session: %w", err)
	}
	return session, nil
}
