# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Run the server (requires Cassandra; see env vars below)
go run ./cmd/server

# Build binary
go build -o bin/server ./cmd/server

# Run all tests with race detector
go test -race ./...

# Run a single package
go test -race ./internal/application/...

# Run a single test by name
go test -race ./internal/application/... -run TestCreateAndGet

# Test coverage
go test -race ./... -cover

go mod tidy
```

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP listen port |
| `APP_ENV` | `development` | Runtime environment |
| `CASSANDRA_HOSTS` | `127.0.0.1` | Comma-separated node addresses |
| `CASSANDRA_KEYSPACE` | `go_echo` | Cassandra keyspace |
| `CASSANDRA_USERNAME` | _(none)_ | Optional auth |
| `CASSANDRA_PASSWORD` | _(none)_ | Optional auth |

## Database setup

Apply the schema once before starting the server:

```bash
cqlsh < db/migrations/001_create_users.cql
```

## Architecture

Domain-Driven Design with strict dependency direction:

```
interfaces/http → application → domain ← infrastructure
```

```
internal/
├── domain/user/            Entity, Repository interface, ErrNotFound
├── application/            UserService use-case (depends only on domain)
├── infrastructure/
│   ├── cassandra/          Session factory + Cassandra UserRepository
│   └── memory/             In-memory UserRepository (dev / tests)
└── interfaces/http/
    ├── handler/            Echo handlers; defines its own userService interface
    ├── middleware/         RequestID and future middleware
    └── server.go           Echo setup, DI wiring, route registration
```

### Layer rules

| Layer | May import | Must NOT import |
|---|---|---|
| `domain` | stdlib, `google/uuid` | anything in `internal/` |
| `application` | `domain` | Echo, gocql, infrastructure |
| `infrastructure` | `domain`, gocql, gocqlx | `application`, `interfaces` |
| `interfaces/http` | `application`, `domain`, `pkg/response` | `infrastructure` directly (wired in `server.go`) |

### Key conventions

- Primary keys are `uuid.UUID` (`google/uuid`) throughout the domain and application layers. The Cassandra repo converts to/from `gocql.UUID` internally via a `userRow` struct — `gocql` types never escape into the domain.
- All Cassandra queries use `context.WithTimeout` (5 s) and the `gocqlx` query builder (`qb.Select`, `qb.Insert`). No raw CQL string building.
- `server.go` is the only place that selects which repository implementation to use (Cassandra when a session is provided, in-memory otherwise) and wires the full dependency graph.
- `pkg/response` provides the shared JSON envelope `{success, data?, error?}`. All handlers use it.
- To add a new resource: `domain/<name>/` → `infrastructure/cassandra/` + `infrastructure/memory/` → `application/` → `interfaces/http/handler/` → register in `server.go`.
