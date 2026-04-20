package cassandra

import (
	"context"
	"fmt"

	domain "github.com/ch/go_echo/internal/domain/tool"
	"github.com/gocql/gocql"
	"github.com/google/uuid"
	"github.com/scylladb/gocqlx/v2"
	"github.com/scylladb/gocqlx/v2/qb"
)

const toolsTable = "tools"

type toolRow struct {
	ID          gocql.UUID `db:"id"`
	Name        string     `db:"name"`
	Description string     `db:"description"`
	Category    string     `db:"category"`
	Status      string     `db:"status"`
}

func toToolRow(t *domain.Tool) toolRow {
	return toolRow{
		ID:          gocql.UUID(t.ID),
		Name:        t.Name,
		Description: t.Description,
		Category:    t.Category,
		Status:      t.Status,
	}
}

func toToolDomain(r toolRow) *domain.Tool {
	return &domain.Tool{
		ID:          uuid.UUID(r.ID),
		Name:        r.Name,
		Description: r.Description,
		Category:    r.Category,
		Status:      r.Status,
	}
}

type toolRepository struct {
	session gocqlx.Session
}

func NewToolRepository(session gocqlx.Session) domain.Repository {
	return &toolRepository{session: session}
}

func (r *toolRepository) FindAll(ctx context.Context) ([]*domain.Tool, error) {
	stmt, names := qb.Select(toolsTable).
		Columns("id", "name", "description", "category", "status").
		ToCql()

	var rows []toolRow
	if err := r.session.ContextQuery(ctx, stmt, names).SelectRelease(&rows); err != nil {
		return nil, fmt.Errorf("tools.FindAll: %w", err)
	}

	out := make([]*domain.Tool, len(rows))
	for i, row := range rows {
		out[i] = toToolDomain(row)
	}
	return out, nil
}

func (r *toolRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Tool, error) {
	stmt, names := qb.Select(toolsTable).
		Columns("id", "name", "description", "category", "status").
		Where(qb.Eq("id")).
		ToCql()

	var row toolRow
	err := r.session.ContextQuery(ctx, stmt, names).
		BindMap(qb.M{"id": gocql.UUID(id)}).
		GetRelease(&row)

	if err == gocql.ErrNotFound {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("tools.FindByID: %w", err)
	}
	return toToolDomain(row), nil
}

func (r *toolRepository) Create(ctx context.Context, t *domain.Tool) (*domain.Tool, error) {
	t.ID = uuid.New()
	row := toToolRow(t)

	stmt, names := qb.Insert(toolsTable).
		Columns("id", "name", "description", "category", "status").
		ToCql()

	if err := r.session.ContextQuery(ctx, stmt, names).
		BindStruct(row).
		ExecRelease(); err != nil {
		return nil, fmt.Errorf("tools.Create: %w", err)
	}
	return t, nil
}
