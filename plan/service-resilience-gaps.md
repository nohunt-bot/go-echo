# Service Resilience Gaps

Gap analysis based on the current codebase. Each item references the exact file and line where the issue lives.

---

## P0 — Will cause outages

These will produce incidents. Fix before anything else.

---

### 1. Liveness probe must not check dependencies

**The problem**

`handler/health.go` pings both Cassandra and Redis and returns 503 if either fails. If this endpoint is used as the Kubernetes liveness probe (which is the natural default), a transient Cassandra slowdown causes the probe to fail → K8s kills a perfectly healthy pod → the pod restarts → the problem compounds.

Liveness means "is this process alive and worth keeping." It should never call external dependencies. That's readiness.

**Fix: split the endpoint in two**

```
GET /health/live   → always 200 if the process is running (no dependency checks)
GET /health/ready  → pings Cassandra + Redis, returns 503 if either is down
```

`handler/health.go` — split `Check` into two handlers:

```go
// Liveness: always returns 200 while the process is running
func (h *HealthHandler) Live(c echo.Context) error {
    return c.JSON(http.StatusOK, map[string]string{"status": "alive"})
}

// Readiness: check dependencies before accepting traffic
func (h *HealthHandler) Ready(c echo.Context) error {
    // existing ping logic, unchanged
}
```

`server.go` — register both routes:

```go
e.GET("/health/live",  healthH.Live)
e.GET("/health/ready", healthH.Ready)
```

Kubernetes probe config:

```yaml
livenessProbe:
  httpGet:
    path: /health/live
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10
  failureThreshold: 3

readinessProbe:
  httpGet:
    path: /health/ready
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5
  failureThreshold: 2

startupProbe:                    # protects slow cold-starts (OOMKill recovery, scale-up)
  httpGet:
    path: /health/live
    port: 8080
  failureThreshold: 30           # 30 × 2s = 60s grace window before liveness kicks in
  periodSeconds: 2
```

---

### 2. Missing pre-stop hook causes rolling deploy errors

**The problem**

`main.go` correctly calls `e.Shutdown(ctx)` on SIGTERM. But Kubernetes does two things simultaneously when terminating a pod:
1. Sends SIGTERM to the container
2. Removes the pod from the load balancer endpoint list

The endpoint removal propagates through kube-proxy with a small delay (~1–2s). During this window, traffic still arrives at a pod that has already started shutting down → 502/503s on every rolling deploy.

**Fix: add a pre-stop sleep**

In the deployment manifest:

```yaml
lifecycle:
  preStop:
    exec:
      command: ["sleep", "5"]   # wait for endpoint removal to propagate before SIGTERM
```

Also ensure `terminationGracePeriodSeconds` is greater than `SHUTDOWN_TIMEOUT` + the pre-stop sleep:

```yaml
terminationGracePeriodSeconds: 40   # 5s preStop + 30s shutdown + 5s buffer
```

No Go code changes needed.

---

### 3. Redis retry has no jitter — thundering herd on recovery

**The problem**

`infrastructure/redis/retry.go` uses pure exponential backoff (`100ms → 200ms → 400ms → 1s`). When Redis recovers after a hiccup, every goroutine that was in backoff wakes up at exactly the same interval and slams Redis simultaneously. This can re-trigger the failure and create a retry storm.

**Fix: add full jitter to `withRetry`**

`infrastructure/redis/retry.go`:

```go
import "math/rand/v2"

// replace the backoff doubling block with:
jitter := time.Duration(rand.Int64N(int64(backoff)))
sleep := backoff/2 + jitter   // sleep between 50% and 100% of backoff
if sleep > maxBackoff {
    sleep = maxBackoff
}
timer := time.NewTimer(sleep)
```

This spreads retry wakeups across a window instead of synchronising them.

---

## P1 — Hard to diagnose during incidents

These don't cause outages directly but make incidents take much longer to resolve.

---

### 4. Cassandra retry budget can consume the entire request timeout

**The problem**

`cassandra/session.go` configures:
- `Timeout: 5s` per query attempt
- `NumRetries: 5` with exponential backoff (`100ms → 5s`)

Worst case per request: 5 attempts × 5s timeout + accumulated backoff ≈ 25–30s. `REQUEST_TIMEOUT` defaults to 30s. These two timers are racing. Under load, request context cancellation and Cassandra retry exhaustion will happen near-simultaneously, making latency non-deterministic and hard to attribute to either layer.

**Fix: reduce Cassandra retry count and make budgets explicit**

`cassandra/session.go`:

```go
cluster.RetryPolicy = &gocql.ExponentialBackoffRetryPolicy{
    Min:        50 * time.Millisecond,
    Max:        500 * time.Millisecond,
    NumRetries: 2,              // 3 total attempts; fast-fail to the request timeout
}
```

Rule: `(NumRetries + 1) × QueryTimeout + total backoff` must be well under `REQUEST_TIMEOUT`, leaving headroom for Redis and handler processing.

Add a comment in `config.go` documenting the budget relationship so future changes don't break the invariant.

---

### 5. Request ID is generated but never appears in logs

**The problem**

`middleware/request_id.go` generates a request ID and sets it in the response header. But Echo's default `Logger()` middleware doesn't know about it. During an incident, you cannot grep application logs and correlate them to a specific request or trace it across log lines.

**Fix: use Echo's structured logger with the request ID**

`server.go` — replace `echomw.Logger()` with a custom format:

```go
e.Use(echomw.RequestLoggerWithConfig(echomw.RequestLoggerConfig{
    LogMethod:    true,
    LogURI:       true,
    LogStatus:    true,
    LogLatency:   true,
    LogRequestID: true,
    LogError:     true,
    LogValuesFunc: func(c echo.Context, v echomw.RequestLoggerValues) error {
        log.Printf("method=%s uri=%s status=%d latency=%s request_id=%s err=%v",
            v.Method, v.URI, v.Status, v.Latency, v.RequestID, v.Error)
        return nil
    },
}))
```

This is a two-line config change. Every log line now carries `request_id`, making it trivial to grep for a specific request's full lifecycle.

---

### 6. Service fails fast on startup if Cassandra is not ready

**The problem**

`cassandra/session.go` calls `cluster.CreateSession()` once and returns the error immediately. In Kubernetes, if the pod starts before Cassandra is ready (cold deploy, node restart, scale-up), the service crashes and enters a CrashLoopBackOff. The startup probe above buys time for the process to stay alive, but the process itself still exits immediately.

**Fix: retry the session creation with backoff**

`cassandra/session.go`:

```go
func NewSession(cfg config.CassandraConfig) (gocqlx.Session, error) {
    cluster := newCluster(cfg)

    var (
        session gocqlx.Session
        err     error
        backoff = 1 * time.Second
    )
    for attempt := 0; attempt < 10; attempt++ {
        session, err = gocqlx.WrapSession(cluster.CreateSession())
        if err == nil {
            return session, nil
        }
        log.Printf("cassandra: connect attempt %d failed: %v, retrying in %s", attempt+1, err, backoff)
        time.Sleep(backoff)
        if backoff *= 2; backoff > 30*time.Second {
            backoff = 30 * time.Second
        }
    }
    return gocqlx.Session{}, fmt.Errorf("cassandra: failed to connect after retries: %w", err)
}
```

---

## P2 — Systemic resilience gaps

Important for production stability but not immediately fire-fighting material.

---

### 7. No circuit breaker on Cassandra calls

**The problem**

If Cassandra becomes slow (not down, just slow), every incoming request holds a goroutine for up to 30s waiting for query results. Goroutines pile up. Eventually the concurrency limiter fills (503s) or memory exhausts (OOMKill). The service has no way to stop accepting work it cannot complete.

A circuit breaker detects that Cassandra is failing and fast-fails new requests for a recovery window, allowing the dependency time to heal without accumulating more load.

**Suggested library:** `github.com/sony/gobreaker`

Wrap each repository method:

```go
// infrastructure/cassandra/user_repository.go
type UserRepository struct {
    session gocqlx.Session
    cb      *gobreaker.CircuitBreaker
}

func (r *UserRepository) List(ctx context.Context) ([]user.User, error) {
    result, err := r.cb.Execute(func() (interface{}, error) {
        // existing query logic
    })
    if err != nil {
        return nil, err   // gobreaker.ErrOpenState propagates as 503 upstream
    }
    return result.([]user.User), nil
}
```

Circuit breaker state transitions:
```
Closed (normal) → too many errors → Open (fast-fail) → timeout → Half-Open (probe) → success → Closed
```

---

### 8. Rate limiter is per-pod, not per-cluster

**The problem**

`RATE_LIMIT_PER_SEC=20` is configured per Echo instance. With 5 replicas behind a load balancer, the effective per-IP limit for the cluster is `20 × 5 = 100 req/s`. This is undocumented and counterintuitive — operators setting `RATE_LIMIT_PER_SEC=20` expect a cluster-wide limit of 20.

Additionally, the in-memory rate limiter state is lost on pod restart. A burst that triggers restart also resets all rate limit counters.

**Options (in order of effort):**
1. Document the per-pod behaviour in `CLAUDE.md` so operators know to divide the desired cluster limit by replica count.
2. Move rate limiting to the ingress/gateway layer (Nginx, Envoy, Kong) where it is enforced once, globally.
3. Use a Redis-backed rate limiter that shares state across pods.

Option 1 is a one-line documentation change. Option 2 is the correct long-term fix.

---

## P3 — Observability improvements

These are valuable but don't affect service behaviour directly.

---

### 9. No distributed tracing

Without tracing, you cannot see how a single request's 6s latency is distributed across the handler → Cassandra → Redis call chain. pprof shows aggregates; tracing shows individual request paths.

**Suggested approach:** OpenTelemetry SDK with Jaeger or Tempo as the backend.

Files to touch:
- `cmd/server/main.go` — initialise the tracer provider
- `internal/interfaces/http/server.go` — add `otelecho.Middleware` to instrument HTTP layer
- `internal/infrastructure/cassandra/*_repository.go` — wrap queries with spans
- `internal/infrastructure/redis/*_repository.go` — wrap calls with spans

This is the largest effort item on this list. Defer until P0–P2 are resolved.

---

### 10. No connection pool observability

Prometheus metrics (from `prometheus-metrics.md`) track HTTP layer. There is no visibility into whether the Cassandra or Redis connection pools are saturated. Pool exhaustion is the most likely cause of the 6s latency and is invisible until you manually run pprof.

Add these custom metrics to `internal/interfaces/http/metrics/metrics.go`:

```go
CassandraPoolWaitDuration = prometheus.NewHistogram(...)   // time waiting for a free connection
RedisPoolWaitDuration     = prometheus.NewHistogram(...)
RedisPoolHits             = prometheus.NewCounter(...)     // conn obtained from pool
RedisPoolMisses           = prometheus.NewCounter(...)     // conn created (pool was empty)
```

`go-redis` exposes pool stats via `client.PoolStats()` — collect them in a background goroutine and record them every 10s.

---

## Summary

| # | Issue | File(s) to change | Effort |
|---|---|---|---|
| P0-1 | Split liveness / readiness probes | `handler/health.go`, `server.go`, K8s manifests | Small |
| P0-2 | Add pre-stop hook | K8s deployment manifest only | Trivial |
| P0-3 | Add jitter to Redis retry | `infrastructure/redis/retry.go` | Trivial |
| P1-4 | Reduce Cassandra retry budget | `cassandra/session.go`, `config.go` | Small |
| P1-5 | Request ID in logs | `server.go` | Small |
| P1-6 | Cassandra startup retry | `cassandra/session.go` | Small |
| P2-7 | Circuit breaker on Cassandra | `infrastructure/cassandra/*_repository.go` | Medium |
| P2-8 | Document per-pod rate limit | `CLAUDE.md` (or ingress config) | Trivial |
| P3-9 | Distributed tracing | Multiple files | Large |
| P3-10 | Connection pool metrics | `metrics/metrics.go`, repositories | Medium |
