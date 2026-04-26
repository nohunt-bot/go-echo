# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Run the server
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
| `REQUEST_TIMEOUT` | `30s` | Per-request deadline; supports Go duration strings (`30s`, `1m`) |
| `MAX_CONCURRENT` | `100` | Max simultaneous in-flight requests (global semaphore) |
| `RATE_LIMIT_PER_SEC` | `20` | Max requests/sec per IP (burst = rate × 3) |
| `CASSANDRA_HOSTS` | `127.0.0.1` | Comma-separated node addresses |
| `CASSANDRA_KEYSPACE` | `go_echo` | Cassandra keyspace |
| `CASSANDRA_USERNAME` | _(none)_ | Optional auth |
| `CASSANDRA_PASSWORD` | _(none)_ | Optional auth |
| `REDIS_ADDRS` | `127.0.0.1:6379` | Comma-separated Redis cluster node addresses |
| `REDIS_PASSWORD` | _(none)_ | Optional auth |

## Database setup

```bash
# Cassandra schema (run once)
cqlsh < db/migrations/001_create_users.cql
cqlsh < db/migrations/002_create_tools.cql
```

## Architecture

Domain-Driven Design with strict dependency direction:

```
interfaces/http → application → domain ← infrastructure
```

```
internal/
├── domain/
│   ├── user/               Entity, Repository interface, ErrNotFound
│   └── tool/               Entity, Repository interface, ErrNotFound
├── application/            UserService, ToolService (depend only on domain)
├── infrastructure/
│   ├── cassandra/          Session factory; User/Tool repository (production)
│   ├── redis/              Redis client + retry helper; User/Tool repository (fallback)
│   └── memory/             In-memory User/Tool repository (unit tests only)
└── interfaces/http/
    ├── handler/            Echo handlers (user, tool); each defines its own service interface
    ├── middleware/         RequestID, ConcurrencyLimiter
    └── server.go           Echo setup, middleware chain, DI wiring, route registration
```

## Layer rules

| Layer | May import | Must NOT import |
|---|---|---|
| `domain` | stdlib, `google/uuid` | anything in `internal/` |
| `application` | `domain` | Echo, gocql, redis, infrastructure |
| `infrastructure` | `domain`, driver libs | `application`, `interfaces` |
| `interfaces/http` | `application`, `domain`, `pkg/response` | infrastructure directly (wired in `server.go`) |

## Request middleware chain

```
Logger → Recover → RequestID → RateLimiter → ConcurrencyLimiter → Timeout → handler
```

| Middleware | Behaviour when triggered |
|---|---|
| RateLimiter | 429 — per-IP token bucket |
| ConcurrencyLimiter | 503 — global semaphore (buffered channel) |
| Timeout | 503 — per-request context deadline |

## Infrastructure conventions

**Cassandra**
- `gocqlx` query builder (`qb.Select`, `qb.Insert`) only — no raw CQL string building.
- Each query wraps its own `context.WithTimeout(5s)`.
- `gocql.UUID` is confined to `infrastructure/cassandra` via a `*Row` struct; domain uses `uuid.UUID`.

**Redis**
- Keys: `user:{uuid}`, `tool:{uuid}` (JSON, TTL 24 h); index sets: `users:index`, `tools:index`.
- `Create` uses `TxPipeline` (MULTI/EXEC) to atomically write the key and update the index.
- All calls are wrapped with `withRetry` (3 retries, exponential backoff 100 ms → 1 s).
- `redis.Nil`, `context.Canceled`, and `context.DeadlineExceeded` are not retried.

**Repository selection** (`server.go`)
- Cassandra session present → Cassandra repositories.
- No session → Redis repositories.
- Memory repositories are used only in unit tests.

## Adding a new domain

```
domain/<name>/entity.go          struct + CreateRequest
domain/<name>/repository.go      Repository interface + ErrNotFound
application/<name>_service.go    Use-case methods
application/<name>_service_test.go
infrastructure/cassandra/<name>_repository.go
infrastructure/redis/<name>_repository.go
infrastructure/memory/<name>/repository.go
interfaces/http/handler/<name>.go
db/migrations/NNN_create_<name>s.cql
```

Then register routes and wire dependencies in `interfaces/http/server.go`.
