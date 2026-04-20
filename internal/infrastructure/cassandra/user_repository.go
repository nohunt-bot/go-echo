package cassandra

import (
	"context"
	"fmt"
	"time"

	"github.com/ch/go_echo/internal/domain/user"
	"github.com/gocql/gocql"
	"github.com/google/uuid"
	"github.com/scylladb/gocqlx/v2"
	"github.com/scylladb/gocqlx/v2/qb"
)

const usersTable = "users"

// userRow is the Cassandra-specific mapping struct.
// It keeps gocql types out of the domain layer.
type userRow struct {
	ID    gocql.UUID `db:"id"`
	Name  string     `db:"name"`
	Email string     `db:"email"`
}

func toRow(u *user.User) userRow {
	return userRow{ID: gocql.UUID(u.ID), Name: u.Name, Email: u.Email}
}

func toDomain(r userRow) *user.User {
	return &user.User{ID: uuid.UUID(r.ID), Name: r.Name, Email: r.Email}
}

type userRepository struct {
	session gocqlx.Session
}

func NewUserRepository(session gocqlx.Session) user.Repository {
	return &userRepository{session: session}
}

func (r *userRepository) FindAll() ([]*user.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stmt, names := qb.Select(usersTable).Columns("id", "name", "email").ToCql()

	var rows []userRow
	if err := r.session.ContextQuery(ctx, stmt, names).SelectRelease(&rows); err != nil {
		return nil, fmt.Errorf("users.FindAll: %w", err)
	}

	out := make([]*user.User, len(rows))
	for i, row := range rows {
		out[i] = toDomain(row)
	}
	return out, nil
}

func (r *userRepository) FindByID(id uuid.UUID) (*user.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stmt, names := qb.Select(usersTable).
		Columns("id", "name", "email").
		Where(qb.Eq("id")).
		ToCql()

	var row userRow
	err := r.session.ContextQuery(ctx, stmt, names).
		BindMap(qb.M{"id": gocql.UUID(id)}).
		GetRelease(&row)

	if err == gocql.ErrNotFound {
		return nil, user.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("users.FindByID: %w", err)
	}
	return toDomain(row), nil
}

func (r *userRepository) Create(u *user.User) (*user.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	u.ID = uuid.New()
	row := toRow(u)

	stmt, names := qb.Insert(usersTable).Columns("id", "name", "email").ToCql()
	if err := r.session.ContextQuery(ctx, stmt, names).
		BindStruct(row).
		ExecRelease(); err != nil {
		return nil, fmt.Errorf("users.Create: %w", err)
	}
	return u, nil
}
