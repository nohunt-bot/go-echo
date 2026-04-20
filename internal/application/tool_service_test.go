package application_test

import (
	"context"
	"testing"

	"github.com/ch/go_echo/internal/application"
	"github.com/ch/go_echo/internal/domain/tool"
	toolmemory "github.com/ch/go_echo/internal/infrastructure/memory/tool"
	"github.com/google/uuid"
)

func newToolSvc() *application.ToolService {
	return application.NewToolService(toolmemory.NewRepository())
}

func TestToolCreateAndGet(t *testing.T) {
	ctx := context.Background()
	svc := newToolSvc()

	created, err := svc.CreateTool(ctx, &tool.CreateRequest{
		Name:     "Hammer",
		Category: "hand-tool",
		Status:   "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == (uuid.UUID{}) {
		t.Fatal("expected non-zero UUID")
	}

	got, err := svc.GetTool(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Hammer" {
		t.Fatalf("got name %q", got.Name)
	}
}

func TestToolListTools(t *testing.T) {
	ctx := context.Background()
	svc := newToolSvc()
	svc.CreateTool(ctx, &tool.CreateRequest{Name: "A", Category: "cat", Status: "active"})
	svc.CreateTool(ctx, &tool.CreateRequest{Name: "B", Category: "cat", Status: "inactive"})

	tools, err := svc.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
}

func TestToolGetNotFound(t *testing.T) {
	ctx := context.Background()
	svc := newToolSvc()
	_, err := svc.GetTool(ctx, uuid.New())
	if err != tool.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
