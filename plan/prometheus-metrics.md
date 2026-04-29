# Adding Prometheus Metrics

## What you get out of the box

Using the Echo-contrib Prometheus middleware, every HTTP request is automatically tracked:

| Metric | Type | Labels |
|---|---|---|
| `echo_requests_total` | Counter | method, host, url, status |
| `echo_request_duration_seconds` | Histogram | method, host, url, status |
| `echo_requests_in_flight` | Gauge | method, host, url |

These three cover 90% of what you need for dashboards and alerts.

---

## Step 1 — Add the dependency

```bash
go get github.com/labstack/echo-contrib/echoprometheus
```

This pulls `prometheus/client_golang` transitively. No other packages needed.

---

## Step 2 — Modify `internal/interfaces/http/server.go`

Two changes: add the middleware and expose the `/metrics` endpoint.

```go
import (
    // existing imports ...
    "github.com/labstack/echo-contrib/echoprometheus"
)

func New(cfg config.ServerConfig, session *gocqlx.Session, redisClient goredis.UniversalClient) *echo.Echo {
    e := echo.New()

    e.Use(echomw.Logger())
    e.Use(echomw.Recover())
    e.Use(middleware.RequestID())
    e.Use(echoprometheus.NewMiddleware("go_echo"))  // ← add this; prefix scopes all metrics
    e.Use(echomw.RateLimiter(...))
    e.Use(middleware.ConcurrencyLimiter(cfg.MaxConcurrent))
    e.Use(echomw.ContextTimeoutWithConfig(...))

    // expose /metrics before registerRoutes so it bypasses the rate limiter
    e.GET("/metrics", echoprometheus.NewHandler())   // ← add this

    registerRoutes(e, session, redisClient)
    return e
}
```

**Why before `registerRoutes`:** `/metrics` must not be wrapped by the rate limiter or concurrency limiter — Prometheus scrapes it on a fixed interval and a 429/503 would produce gaps in your graphs.

**Why the prefix `"go_echo"`:** All auto-generated metric names become `go_echo_requests_total`, `go_echo_request_duration_seconds`, etc. Without a prefix, the names are generic and clash if you run multiple services in the same Prometheus instance.

---

## Step 3 — Verify locally

```bash
go run ./cmd/server
curl http://localhost:8080/metrics
```

You should see output like:

```
# HELP go_echo_requests_total How many HTTP requests processed
# TYPE go_echo_requests_total counter
go_echo_requests_total{code="200",host="localhost:8080",method="GET",url="/health"} 1

# HELP go_echo_request_duration_seconds The HTTP request latencies in seconds
# TYPE go_echo_request_duration_seconds histogram
go_echo_request_duration_seconds_bucket{...,le="0.005"} 1
...
```

---

## Custom metrics (optional but recommended)

The built-in metrics track HTTP layer only. Add custom metrics when you need to observe infrastructure-layer behavior (Cassandra query time, Redis retry count, etc.).

Create a new file `internal/interfaces/http/metrics/metrics.go`:

```go
package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
    // How long Cassandra queries take
    CassandraQueryDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "go_echo_cassandra_query_duration_seconds",
            Help:    "Cassandra query latency",
            Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5},
        },
        []string{"operation"}, // "list_users", "get_user", etc.
    )

    // Count Redis retries — spike here means Redis is unstable
    RedisRetryTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "go_echo_redis_retry_total",
            Help: "Number of Redis operation retries",
        },
        []string{"operation"},
    )
)

func init() {
    prometheus.MustRegister(CassandraQueryDuration, RedisRetryTotal)
}
```

Then instrument the Cassandra repository (`infrastructure/cassandra/user_repository.go`) like this:

```go
import "github.com/ch/go_echo/internal/interfaces/http/metrics"

func (r *UserRepository) List(ctx context.Context) ([]user.User, error) {
    timer := prometheus.NewTimer(metrics.CassandraQueryDuration.WithLabelValues("list_users"))
    defer timer.ObserveDuration()

    // existing query logic ...
}
```

And in the Redis retry helper (`infrastructure/redis/retry.go`):

```go
import "github.com/ch/go_echo/internal/interfaces/http/metrics"

// inside withRetry, when a retry happens:
metrics.RedisRetryTotal.WithLabelValues(operation).Inc()
```

---

## Files changed summary

| File | Change |
|---|---|
| `go.mod` / `go.sum` | New dependency `echo-contrib/echoprometheus` (via `go get`) |
| `internal/interfaces/http/server.go` | Add middleware + `/metrics` route (2 lines) |
| `internal/interfaces/http/metrics/metrics.go` | **New file** — custom metric declarations (optional) |
| `internal/infrastructure/cassandra/*_repository.go` | Add timer around each query (optional) |
| `internal/infrastructure/redis/retry.go` | Increment retry counter on each retry attempt (optional) |

---

## Prometheus scrape config

Add this to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: go_echo
    static_configs:
      - targets: ["<pod-ip>:8080"]
    metrics_path: /metrics
    scrape_interval: 15s
```

On Kubernetes, use a `ServiceMonitor` (if using the Prometheus Operator) instead:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: go-echo
  namespace: <namespace>
spec:
  selector:
    matchLabels:
      app: go-echo          # must match your Service labels
  endpoints:
    - port: http
      path: /metrics
      interval: 15s
```

---

## Key alert rules to add after metrics are flowing

```yaml
# prometheus/alerts.yaml
groups:
  - name: go_echo
    rules:
      - alert: HighErrorRate
        expr: |
          rate(go_echo_requests_total{code=~"5.."}[5m])
          / rate(go_echo_requests_total[5m]) > 0.01
        for: 2m
        annotations:
          summary: "Error rate above 1% for 2 minutes"

      - alert: HighP99Latency
        expr: |
          histogram_quantile(0.99,
            rate(go_echo_request_duration_seconds_bucket[5m])
          ) > 2
        for: 5m
        annotations:
          summary: "p99 latency above 2s for 5 minutes"

      - alert: CassandraQuerySlow
        expr: |
          histogram_quantile(0.95,
            rate(go_echo_cassandra_query_duration_seconds_bucket[5m])
          ) > 0.5
        for: 3m
        annotations:
          summary: "Cassandra p95 query time above 500ms"
```

---

## Minimal implementation checklist

- [ ] `go get github.com/labstack/echo-contrib/echoprometheus`
- [ ] Add `echoprometheus.NewMiddleware("go_echo")` in `server.go` (before rate limiter)
- [ ] Add `e.GET("/metrics", echoprometheus.NewHandler())` in `server.go`
- [ ] `curl localhost:8080/metrics` returns data
- [ ] Add scrape config to Prometheus or create ServiceMonitor
- [ ] Confirm metrics appear in Prometheus UI under `go_echo_*`
