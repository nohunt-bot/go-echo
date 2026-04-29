# Rate Limiting in Go

## What's already in this service

The service already has two layers of protection in `server.go`:

```go
// Per-IP token bucket — rejects with 429
echomw.RateLimiter(echomw.NewRateLimiterMemoryStoreWithConfig(
    echomw.RateLimiterMemoryStoreConfig{
        Rate:      rate.Limit(cfg.RateLimitPerSecond), // default: 20 req/s
        Burst:     cfg.RateLimitPerSecond * 3,          // default: 60
        ExpiresIn: 3 * time.Minute,
    },
))

// Global concurrency cap — rejects with 503
middleware.ConcurrencyLimiter(cfg.MaxConcurrent) // default: 100
```

These two solve different problems. The rate limiter controls request **frequency** per client. The concurrency limiter controls how many requests are **in-flight simultaneously** across all clients. You need both.

The problem with the current setup: the rate limiter lives in memory, per-pod. In a multi-replica deployment, each pod has its own counter. A client can send 20 req/s to each pod independently, effectively multiplying the limit by the replica count. This document covers the options for fixing that and adding more granular control.

---

## Algorithms

Understanding the algorithm matters because each one has different behavior under burst traffic.

### Token bucket

Each client gets a bucket with a max capacity (burst). Tokens refill at a fixed rate. A request consumes one token; if the bucket is empty, the request is rejected.

**Behavior:** allows short bursts up to the bucket size, then smooths out to the refill rate. This is what `golang.org/x/time/rate` implements, and what Echo's built-in rate limiter uses.

```go
import "golang.org/x/time/rate"

limiter := rate.NewLimiter(rate.Limit(20), 60) // 20/s, burst 60

if !limiter.Allow() {
    // reject
}

// Or block until a token is available (useful in background workers)
if err := limiter.Wait(ctx); err != nil {
    // context cancelled
}
```

### Sliding window

Counts requests in a rolling time window (e.g., last 60 seconds). More accurate than token bucket for strict rate enforcement, but more expensive to compute — typically needs Redis sorted sets.

### Fixed window

Simplest: count requests per fixed time window (per minute, per hour). Cheap to implement with a Redis counter and TTL. Downside: allows 2x the rate at window boundaries (burst at end of one window + start of next).

### Leaky bucket

Requests enter a queue and drain at a fixed rate. Smooths out bursts completely. Rarely used in HTTP APIs because it adds latency — clients wait in queue instead of getting rejected immediately.

**For this service:** token bucket is the right choice for per-IP limits. Sliding window in Redis is worth adding for per-user or per-API-key limits where accuracy matters more.

---

## In-memory vs Redis-backed

| | In-memory (`golang.org/x/time/rate`) | Redis-backed |
|---|---|---|
| Latency | Zero | +1 Redis RTT per request |
| Accuracy in multi-pod | Per-pod, not global | Global across all pods |
| State loss on restart | Resets to zero | Survives restarts |
| Complexity | Trivial | Needs Redis + Lua script |

**Rule of thumb:** in-memory for per-pod protection (DoS mitigation), Redis for per-user/per-tenant limits that need to be globally enforced.

---

## Implementation options

### Option 1: Fix the existing in-memory limiter (quick win)

The current implementation already works correctly per-pod. If the primary concern is a single abusive client, the existing setup is fine — the client gets rate-limited on whichever pod it hits, even if not globally.

Nothing to change here except tuning the numbers based on load test results.

### Option 2: Redis-backed global rate limiter

Use Redis with a Lua script for atomic increment + TTL. The Lua script runs atomically on the Redis server, avoiding race conditions without a transaction.

```go
// internal/interfaces/http/middleware/rate_limit.go

package middleware

import (
    "context"
    "fmt"
    "net/http"
    "time"

    "github.com/labstack/echo/v4"
    goredis "github.com/redis/go-redis/v9"
)

// slidingWindowScript is a Lua script that implements a fixed-window counter.
// Returns the current count after incrementing.
// KEYS[1] = rate limit key, ARGV[1] = window TTL in seconds
var slidingWindowScript = goredis.NewScript(`
    local count = redis.call("INCR", KEYS[1])
    if count == 1 then
        redis.call("EXPIRE", KEYS[1], ARGV[1])
    end
    return count
`)

type RedisRateLimiter struct {
    client  goredis.UniversalClient
    limit   int
    window  time.Duration
    keyFunc func(c echo.Context) string
}

func NewRedisRateLimiter(client goredis.UniversalClient, limit int, window time.Duration) *RedisRateLimiter {
    return &RedisRateLimiter{
        client: client,
        limit:  limit,
        window: window,
        keyFunc: func(c echo.Context) string {
            return fmt.Sprintf("ratelimit:%s", c.RealIP())
        },
    }
}

func (rl *RedisRateLimiter) Middleware() echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            key := rl.keyFunc(c)
            windowSecs := int(rl.window.Seconds())

            count, err := slidingWindowScript.Run(
                c.Request().Context(),
                rl.client,
                []string{key},
                windowSecs,
            ).Int()

            if err != nil {
                // Redis down — fail open to avoid blocking all traffic
                return next(c)
            }

            c.Response().Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", rl.limit))
            c.Response().Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", max(0, rl.limit-count)))

            if count > rl.limit {
                return c.JSON(http.StatusTooManyRequests, map[string]interface{}{
                    "success": false,
                    "error":   "rate limit exceeded",
                })
            }

            return next(c)
        }
    }
}

func max(a, b int) int {
    if a > b {
        return a
    }
    return b
}
```

Wire it into `server.go`:

```go
redisLimiter := middleware.NewRedisRateLimiter(
    redisClient,
    100,            // 100 requests
    time.Minute,    // per minute, globally
)
e.Use(redisLimiter.Middleware())
```

### Option 3: Per-route limits

Different endpoints warrant different limits. `POST /users` (write) should be tighter than `GET /users/:id` (read).

```go
// Apply a stricter limiter only to write endpoints
writeLimiter := echomw.RateLimiter(echomw.NewRateLimiterMemoryStoreWithConfig(
    echomw.RateLimiterMemoryStoreConfig{
        Rate:  rate.Limit(5),
        Burst: 10,
    },
))

api.POST("/users", userH.Create, writeLimiter)
api.POST("/tools", toolH.Create, writeLimiter)
```

### Option 4: Per-user / per-API-key limits

When downstream systems identify themselves (via header or JWT), limit by identity instead of IP. Change the `keyFunc`:

```go
keyFunc: func(c echo.Context) string {
    apiKey := c.Request().Header.Get("X-API-Key")
    if apiKey != "" {
        return fmt.Sprintf("ratelimit:key:%s", apiKey)
    }
    return fmt.Sprintf("ratelimit:ip:%s", c.RealIP())
},
```

---

## Middleware order matters

The current order in `server.go`:

```
Logger → Recover → RequestID → RateLimiter → ConcurrencyLimiter → Timeout → handler
```

Rate limiter before concurrency limiter is correct. Rate limiter rejects abusive clients early, before they consume a concurrency slot. Swapping the order would let rate-limited clients hold slots briefly before being rejected.

Never put rate limiting after business logic — by then you've already paid the cost of the request.

---

## Failure behavior

When Redis is unavailable, the rate limiter should **fail open** (let requests through) rather than failing closed (blocking everyone). A Redis outage that also takes down your API is worse than temporarily unenforced rate limits.

```go
if err != nil {
    // log the error, increment a metric, but don't block the request
    log.Printf("rate limiter redis error: %v", err)
    return next(c)
}
```

---

## Response headers

Always include these headers so clients know their current standing:

```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 42
X-RateLimit-Reset: 1714500000   // Unix timestamp of window reset
Retry-After: 30                 // seconds, only on 429 responses
```

---

## What to watch in production

Rate limiter behavior shows up in two places:

**429 rate in metrics** — a sudden spike means either an abusive client or a downstream system retrying aggressively. Check the source IP in logs.

**Retry storms** — if a downstream system receives 429s and immediately retries without backoff, the rate limiter makes things worse, not better. Make sure clients implement exponential backoff with jitter. Document this in the API contract.

```bash
# Check 429 rate in logs
kubectl logs <pod> -n <namespace> | grep '"status":429' | wc -l

# Redis key inspection (find top offenders)
redis-cli KEYS "ratelimit:*" | head -20
redis-cli GET "ratelimit:ip:1.2.3.4"
```

---

## Checklist before shipping a rate limiter change

- [ ] Tested with load test that the 429 kicks in at the right threshold
- [ ] Confirmed the service doesn't 429 its own health check endpoint
- [ ] Redis failure mode tested — verify fail-open behavior works
- [ ] Response headers included (`X-RateLimit-*`, `Retry-After`)
- [ ] Downstream teams notified of the limit so they can implement backoff
- [ ] Metrics/alerts on 429 rate configured
