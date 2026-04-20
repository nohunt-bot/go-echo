package cassandra

import (
	"context"
	"fmt"

	"github.com/scylladb/gocqlx/v2"
)

type Pinger struct{ session gocqlx.Session }

func NewPinger(session gocqlx.Session) *Pinger { return &Pinger{session: session} }

func (p *Pinger) Ping(_ context.Context) error {
	if err := p.session.ExecStmt("SELECT now() FROM system.local"); err != nil {
		return fmt.Errorf("cassandra: %w", err)
	}
	return nil
}
