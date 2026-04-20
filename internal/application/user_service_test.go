package application_test

import (
	"context"
	"testing"

	"github.com/ch/go_echo/internal/application"
	"github.com/ch/go_echo/internal/domain/user"
	"github.com/ch/go_echo/internal/infrastructure/memory"
	"github.com/google/uuid"
)

func newUserSvc() *application.UserService {
	return application.NewUserService(memory.NewUserRepository())
}

func TestCreateAndGet(t *testing.T) {
	ctx := context.Background()
	svc := newUserSvc()

	created, err := svc.CreateUser(ctx, &user.CreateRequest{Name: "Alice", Email: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == (uuid.UUID{}) {
		t.Fatal("expected non-zero UUID")
	}

	got, err := svc.GetUser(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "alice@example.com" {
		t.Fatalf("got email %q", got.Email)
	}
}

func TestListUsers(t *testing.T) {
	ctx := context.Background()
	svc := newUserSvc()
	svc.CreateUser(ctx, &user.CreateRequest{Name: "A", Email: "a@x.com"})
	svc.CreateUser(ctx, &user.CreateRequest{Name: "B", Email: "b@x.com"})

	users, err := svc.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
}

func TestGetNotFound(t *testing.T) {
	ctx := context.Background()
	svc := newUserSvc()
	_, err := svc.GetUser(ctx, uuid.New())
	if err != user.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
