package application_test

import (
	"testing"

	"github.com/ch/go_echo/internal/application"
	"github.com/ch/go_echo/internal/domain/user"
	"github.com/ch/go_echo/internal/infrastructure/memory"
	"github.com/google/uuid"
)

func newSvc() *application.UserService {
	return application.NewUserService(memory.NewUserRepository())
}

func TestCreateAndGet(t *testing.T) {
	svc := newSvc()

	created, err := svc.CreateUser(&user.CreateRequest{Name: "Alice", Email: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == (uuid.UUID{}) {
		t.Fatal("expected non-zero UUID")
	}

	got, err := svc.GetUser(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "alice@example.com" {
		t.Fatalf("got email %q", got.Email)
	}
}

func TestListUsers(t *testing.T) {
	svc := newSvc()
	svc.CreateUser(&user.CreateRequest{Name: "A", Email: "a@x.com"})
	svc.CreateUser(&user.CreateRequest{Name: "B", Email: "b@x.com"})

	users, err := svc.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
}

func TestGetNotFound(t *testing.T) {
	svc := newSvc()
	_, err := svc.GetUser(uuid.New())
	if err != user.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
