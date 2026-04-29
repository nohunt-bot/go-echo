# Streaming Response in Go

## Why streaming

The current `FindAll` loads every row into memory before writing the response. For large Cassandra result sets this means:

- Memory spikes proportional to result size
- Client waits until all rows are fetched before receiving anything
- A single slow page from Cassandra blocks the entire response

Streaming flips this: rows are written to the response as they come off the Cassandra iterator. Memory stays flat, and the client starts receiving data immediately.

---

## How HTTP streaming works in Go

Go's `http.ResponseWriter` implements `io.Writer`. Once you call `WriteHeader` (or the first `Write`), the status code is locked and headers are sent. After that you can keep writing to the body.

The critical piece is `http.Flusher`. Without explicit flushes, the Go HTTP server buffers writes and the client won't see data until the buffer fills or the handler returns.

```go
flusher, ok := w.(http.Flusher)
if !ok {
    // HTTP/2 always supports flushing; HTTP/1.1 depends on the server
    http.Error(w, "streaming not supported", http.StatusInternalServerError)
    return
}

enc := json.NewEncoder(w)
for _, item := range items {
    enc.Encode(item)  // writes one JSON object per line
    flusher.Flush()   // push bytes to the client immediately
}
```

Two things to handle that don't exist in a normal response:

1. **You can't change the status code mid-stream.** If the first row succeeds but the 500th fails, the client already received `200 OK`. The only option is to signal the error in the payload itself.
2. **Client disconnects.** The request context is cancelled when the client closes the connection. Check `ctx.Err()` inside the loop.

---

## Response format

NDJSON (newline-delimited JSON) is the standard for streaming JSON. One JSON object per line, no wrapping array. This lets clients parse incrementally without buffering the full response.

```
{"id":"abc","name":"Alice","email":"alice@example.com"}
{"id":"def","name":"Bob","email":"bob@example.com"}
{"error":"cassandra timeout after row 42"}
```

If an error occurs mid-stream, write an error object as the last line. The client is responsible for checking whether the last line is an error.

---

## Interface design

### 1. Repository layer

Add a `Stream` method alongside the existing `FindAll`. Don't replace it — `FindAll` is still useful for small result sets.

```go
// internal/domain/user/repository.go

type Repository interface {
    FindAll(ctx context.Context) ([]*User, error)
    FindByID(ctx context.Context, id uuid.UUID) (*User, error)
    Create(ctx context.Context, u *User) (*User, error)
    Stream(ctx context.Context) (<-chan *User, <-chan error)
}
```

Two channels: one for data, one for a terminal error. The producer closes the data channel when done. The consumer drains the data channel and then checks the error channel.

### 2. Service layer

Pass the channels through. The service layer doesn't need to buffer.

```go
// internal/application/user_service.go

func (s *UserService) StreamUsers(ctx context.Context) (<-chan *user.User, <-chan error) {
    return s.repo.Stream(ctx)
}
```

### 3. Handler interface

```go
// internal/interfaces/http/handler/user.go

type userService interface {
    ListUsers(ctx context.Context) ([]*user.User, error)
    GetUser(ctx context.Context, id uuid.UUID) (*user.User, error)
    CreateUser(ctx context.Context, req *user.CreateRequest) (*user.User, error)
    StreamUsers(ctx context.Context) (<-chan *user.User, <-chan error)
}
```

---

## Implementation

### Repository — Cassandra

gocqlx's `IterRelease` returns a `*gocqlx.Iterx` which wraps the gocql iterator. Scan row by row and send into the channel.

```go
// internal/infrastructure/cassandra/user_repository.go

func (r *userRepository) Stream(ctx context.Context) (<-chan *user.User, <-chan error) {
    out := make(chan *user.User)
    errc := make(chan error, 1)

    go func() {
        defer close(out)
        defer close(errc)

        stmt, names := qb.Select(usersTable).Columns("id", "name", "email").ToCql()
        iter := r.session.ContextQuery(ctx, stmt, names).IterRelease()
        defer iter.Close()

        var row userRow
        for iter.StructScan(&row) {
            select {
            case <-ctx.Done():
                errc <- ctx.Err()
                return
            case out <- toDomain(row):
            }
        }

        if err := iter.Close(); err != nil {
            errc <- fmt.Errorf("users.Stream: %w", err)
        }
    }()

    return out, errc
}
```

### Handler

```go
// internal/interfaces/http/handler/user.go

func (h *UserHandler) Stream(c echo.Context) error {
    w := c.Response()
    w.Header().Set("Content-Type", "application/x-ndjson")
    w.Header().Set("X-Content-Type-Options", "nosniff")
    w.WriteHeader(http.StatusOK)

    flusher, ok := w.Writer.(http.Flusher)
    if !ok {
        return echo.NewHTTPError(http.StatusInternalServerError, "streaming not supported")
    }

    enc := json.NewEncoder(w)
    users, errc := h.svc.StreamUsers(c.Request().Context())

    for u := range users {
        if err := enc.Encode(u); err != nil {
            // client likely disconnected; context will be cancelled, loop exits
            break
        }
        flusher.Flush()
    }

    if err := <-errc; err != nil {
        // write error as last NDJSON line — client checks for this
        enc.Encode(map[string]string{"error": err.Error()})
        flusher.Flush()
    }

    return nil
}
```

### Route registration

```go
// internal/interfaces/http/server.go

api.GET("/users/stream", userH.Stream)
```

---

## Memory repository (for tests)

```go
// internal/infrastructure/memory/user/repository.go

func (r *repository) Stream(ctx context.Context) (<-chan *user.User, <-chan error) {
    out := make(chan *user.User)
    errc := make(chan error, 1)

    go func() {
        defer close(out)
        defer close(errc)

        r.mu.RLock()
        defer r.mu.RUnlock()

        for _, u := range r.data {
            select {
            case <-ctx.Done():
                errc <- ctx.Err()
                return
            case out <- u:
            }
        }
    }()

    return out, errc
}
```

---

## Client consumption

```bash
# curl with --no-buffer to print each line as it arrives
curl --no-buffer http://localhost:8080/api/v1/users/stream

# jq processes each line as a separate JSON object
curl --no-buffer http://localhost:8080/api/v1/users/stream | jq -c '.'
```

---

## Things that will break if you're not careful

**Timeout middleware.** The existing `ContextTimeoutWithConfig` will cancel the request context after `REQUEST_TIMEOUT` (default 30s). A streaming response that takes longer than 30s will be cut off. Either exclude the stream route from the timeout middleware or set a higher timeout for it specifically.

```go
// register stream route before the timeout middleware group
e.GET("/api/v1/users/stream", userH.Stream)  // no timeout
api := e.Group("/api/v1", timeoutMiddleware)  // other routes keep timeout
```

**Rate limiter and concurrency limiter.** Streaming connections hold a slot in the concurrency semaphore for the duration of the stream. A `MAX_CONCURRENT=100` with 100 active streams means no capacity for regular requests. Consider a separate, lower limit for stream endpoints.

**Error visibility.** Once the first byte is written, the HTTP status is 200. Errors after that point are invisible to anything that only checks status codes — load balancers, Kubernetes probes, monitoring. Log errors server-side and include them in the NDJSON payload.

**Redis repository.** The current Redis repository doesn't have a natural iterator — it loads all keys then fetches each one individually. Implementing `Stream` there means fetching in batches (e.g., `SSCAN` + pipeline). Worth doing only if Cassandra is unavailable and Redis is the fallback.
