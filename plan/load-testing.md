# Load Testing

## What we're trying to find

This isn't about hitting a big number. The service is already showing 6s response times in production under real traffic, which means we have a bottleneck somewhere between the connection pools and the middleware limits. Load testing here has three goals:

1. Reproduce the degradation in a controlled environment so we can profile it safely
2. Identify which layer saturates first — concurrency limiter, Cassandra pool, or Redis pool
3. Establish a baseline so config changes have measurable before/after numbers

---

## Current limits (from config defaults)

| Config | Default | Note |
|---|---|---|
| `RATE_LIMIT_PER_SEC` | 20 req/s per IP, burst 60 | Will mask real bottlenecks if load comes from one IP |
| `MAX_CONCURRENT` | 100 | Global semaphore — 503 when full |
| `REQUEST_TIMEOUT` | 30s | Hard ceiling per request |
| `CASSANDRA_NUM_CONNS` | 2 per host | Almost certainly the bottleneck under load |
| `REDIS_POOL_SIZE` | 10 | May saturate before Cassandra |

`CASSANDRA_NUM_CONNS=2` is worth flagging immediately. At 2 connections per host, even moderate concurrency will exhaust the pool and queue requests, which directly explains the 6s tail latency.

---

## Tool

Use [k6](https://k6.io). It's scriptable, produces percentile histograms out of the box, and the output maps cleanly to what Prometheus/Grafana shows.

```bash
brew install k6
```

---

## Before running any test

**Disable the per-IP rate limiter for load tests**, or set `RATE_LIMIT_PER_SEC` to something large. If all load comes from one machine, you'll hit the rate limit at 20 req/s and never see actual application bottlenecks.

```bash
# When deploying the test target
RATE_LIMIT_PER_SEC=10000 go run ./cmd/server
```

Also enable pprof so you can observe the application while the test runs:

```bash
PPROF_PORT=6060 RATE_LIMIT_PER_SEC=10000 go run ./cmd/server
```

---

## Test scenarios

### 1. Baseline

Ramp up slowly to understand steady-state behavior before stress. Run this first, every time.

```js
// k6/baseline.js
import http from 'k6/http';
import { check } from 'k6';

export const options = {
  stages: [
    { duration: '1m', target: 20 },   // ramp up
    { duration: '3m', target: 20 },   // hold
    { duration: '30s', target: 0 },   // ramp down
  ],
  thresholds: {
    http_req_duration: ['p95<500'],
    http_req_failed: ['rate<0.01'],
  },
};

export default function () {
  const res = http.get('http://localhost:8080/api/v1/users');
  check(res, { 'status 200': (r) => r.status === 200 });
}
```

```bash
k6 run k6/baseline.js
```

**What to watch:** p50, p95, p99 latency. If p95 > 500ms at 20 VUs with default config, the connection pool is the issue, not traffic volume.

---

### 2. Sustained load

Simulate expected production traffic for an extended period. The primary goal here is catching goroutine leaks and memory growth — things that only show up after running for a while.

```js
// k6/sustained.js
import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '2m', target: 50 },
    { duration: '20m', target: 50 },   // hold long enough to see memory trend
    { duration: '1m', target: 0 },
  ],
  thresholds: {
    http_req_duration: ['p99<2000'],
    http_req_failed: ['rate<0.01'],
  },
};

export default function () {
  const res = http.get('http://localhost:8080/api/v1/users');
  check(res, { 'status 200': (r) => r.status === 200 });
  sleep(0.5);
}
```

While this runs, monitor goroutine count every few minutes:

```bash
watch -n 60 'curl -s "http://localhost:6060/debug/pprof/goroutine?debug=1" | head -1'
```

If the goroutine count climbs linearly with time and doesn't stabilize, there's a leak.

---

### 3. Spike test

Simulate a sudden traffic burst — downstream system retrying after an outage, batch job starting, etc.

```js
// k6/spike.js
import http from 'k6/http';
import { check } from 'k6';

export const options = {
  stages: [
    { duration: '30s', target: 10 },    // normal
    { duration: '10s', target: 200 },   // spike — beyond MAX_CONCURRENT
    { duration: '2m', target: 200 },    // hold
    { duration: '30s', target: 10 },    // recover
    { duration: '1m', target: 10 },     // verify recovery
  ],
};

export default function () {
  const res = http.get('http://localhost:8080/api/v1/users');
  check(res, {
    'status 200 or 503': (r) => r.status === 200 || r.status === 503,
  });
}
```

At 200 VUs, requests above `MAX_CONCURRENT=100` should get 503s immediately from the concurrency limiter. What we're looking for is whether the service **recovers** cleanly after the spike drops — latency should return to baseline, goroutine count should stabilize. If it doesn't recover, that's the leak.

---

### 4. Connection pool saturation test

This one is specific to the `CASSANDRA_NUM_CONNS=2` situation. Drive enough concurrent requests to saturate the pool and observe what happens to latency.

```js
// k6/pool-saturation.js
import http from 'k6/http';
import { check } from 'k6';

export const options = {
  stages: [
    { duration: '1m', target: 10 },
    { duration: '1m', target: 30 },
    { duration: '1m', target: 60 },
    { duration: '1m', target: 100 },
    { duration: '2m', target: 100 },
    { duration: '1m', target: 0 },
  ],
};

export default function () {
  const res = http.get('http://localhost:8080/api/v1/users');
  check(res, { 'not 500': (r) => r.status !== 500 });
}
```

Run this twice: once with `CASSANDRA_NUM_CONNS=2` (default), once with `CASSANDRA_NUM_CONNS=10`. If p99 latency drops significantly, the pool was the bottleneck.

---

## What to observe while tests run

### pprof (in another terminal)

```bash
# Goroutine count — run every minute during sustained and spike tests
curl -s "http://localhost:6060/debug/pprof/goroutine?debug=1" | head -1

# If goroutines are climbing, grab the stacks
go tool pprof http://localhost:6060/debug/pprof/goroutine

# CPU flame graph — run during the hold phase
go tool pprof -http=:8888 \
  "http://localhost:6060/debug/pprof/profile?seconds=30"
```

### Cassandra thread pool

```bash
# Run during the hold phase of sustained and pool-saturation tests
watch -n 10 'nodetool tpstats | grep -E "Stage|Native"'
```

Dropped messages in `Native-Transport-Requests` means Cassandra is the bottleneck.

### Redis

```bash
redis-cli SLOWLOG GET 20
redis-cli INFO stats | grep -E "rejected_connections|connected_clients"
```

### K8s (if running against a cluster)

Use k9s for real-time observation during tests — faster than running kubectl commands manually.

```bash
k9s -n <namespace>
```

Key views to have open while a test runs:

| k9s shortcut | What to watch |
|---|---|
| `:pods` | Pod status and restart count — any non-zero restart count during a test is a failure |
| `l` on a pod | Live log stream — watch for timeout errors, 503s, and panic stack traces |
| `:events` | Liveness probe failures and OOMKill events show up here first |
| `shift-f` on a pod | Port-forward directly from k9s to reach pprof without leaving the terminal |

---

## Tuning knobs and expected impact

After identifying the bottleneck, these are the levers to pull:

| Config | Current default | Suggested starting point | Why |
|---|---|---|---|
| `CASSANDRA_NUM_CONNS` | 2 | 8–10 | 2 connections per host is almost certainly the primary bottleneck |
| `REDIS_POOL_SIZE` | 10 | 20–30 | Increase proportionally with `MAX_CONCURRENT` |
| `MAX_CONCURRENT` | 100 | Tune after pool changes | Raising this without fixing the pool just queues more requests to the same bottleneck |
| `RATE_LIMIT_PER_SEC` | 20 per IP | Keep for prod, bypass for tests | Exists to protect the service, not a performance lever |

Don't change more than one variable per test run. Otherwise you won't know which change caused the improvement.

---

## Other approaches and when to use them

### vegeta — constant rate testing

The key difference from k6: k6 is VU-based, vegeta is rate-based. With VU-based testing, when the server slows down, VUs naturally slow down too because they're waiting for responses — so your effective RPS drops as latency rises. This masks queue buildup. Vegeta sends at a fixed rate regardless of server response time, which more accurately reflects real traffic and makes it easier to find the exact RPS where the service breaks.

```bash
brew install vegeta
```

Find the breaking point:

```bash
# Ramp from 10 to 100 req/s in steps, watch where latency degrades
echo "GET http://localhost:8080/api/v1/users" | \
  vegeta attack -rate=50/s -duration=60s | \
  vegeta report

# Latency histogram
echo "GET http://localhost:8080/api/v1/users" | \
  vegeta attack -rate=50/s -duration=60s | \
  vegeta report -type=hist[0,10ms,50ms,100ms,500ms,1s,2s,5s]
```

Run at progressively higher rates (10, 20, 50, 100 req/s) until p99 breaks the SLO threshold. That's your saturation point.

When to use over k6: when you need a precise RPS number, or when you want to verify that the service holds at a specific rate under sustained traffic.

---

### Chaos Mesh — fault injection

Not load testing in the traditional sense, but essential for an SRE view of this service. Cassandra and Redis are both external dependencies with retry logic and connection pools — you need to know what happens when they degrade or go down, not just when traffic is high.

Install into the cluster:

```bash
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm install chaos-mesh chaos-mesh/chaos-mesh -n chaos-mesh --create-namespace
```

Scenarios worth running:

**Network latency injection into Cassandra**

```yaml
apiVersion: chaos-mesh.org/v1alpha1
kind: NetworkChaos
metadata:
  name: cassandra-latency
spec:
  action: delay
  mode: all
  selector:
    namespaces: [<namespace>]
    labelSelectors:
      app: cassandra
  delay:
    latency: "200ms"
    correlation: "25"
    jitter: "50ms"
  duration: "5m"
```

Inject 200ms of latency into Cassandra traffic and watch whether the service's 30s request timeout and gocql retry policy handle it gracefully — or whether goroutines pile up waiting.

**Pod failure**

```yaml
apiVersion: chaos-mesh.org/v1alpha1
kind: PodChaos
metadata:
  name: kill-one-pod
spec:
  action: pod-kill
  mode: one
  selector:
    namespaces: [<namespace>]
    labelSelectors:
      app: <your-app>
  duration: "1m"
```

Validate that K8s restarts the pod and traffic recovers within the expected window. Also confirms the liveness probe config is reasonable.

What to watch during chaos tests: error rate in k6/vegeta output, recovery time after chaos is removed, and whether the `withRetry` helper in the Redis layer triggers a retry storm when connectivity is restored all at once.

---

### wrk — raw throughput benchmark

Use this only when you need to know the theoretical maximum RPS the service can handle and you suspect single-machine k6 or vegeta isn't generating enough load. C-based, extremely low overhead.

```bash
brew install wrk

# 12 threads, 400 connections, 30 second run
wrk -t12 -c400 -d30s http://localhost:8080/api/v1/users
```

wrk tells you the ceiling. It doesn't tell you much about latency distribution or failure behavior. Once you know the ceiling, use vegeta or k6 to test at 50–70% of it for sustained tests.

---

### hey — quick spot checks

One-liner for when you just want a fast sanity check after a config change.

```bash
go install github.com/rakyll/hey@latest

# 200 requests, 20 concurrent
hey -n 200 -c 20 http://localhost:8080/api/v1/users
```

Not useful for sustained tests or scenarios. Keep it for ad-hoc checks between test runs.

---

### Distributed k6

If the service is fast enough that a single machine can't saturate it, run k6 across multiple instances. In practice this rarely comes up — if you're hitting single-machine limits before hitting service limits, wrk is a better tool.

For large-scale scenarios, k6 OSS supports distributed execution via the `k6 operator` on K8s:

```bash
# Deploy k6 operator
helm repo add grafana https://grafana.github.io/helm-charts
helm install k6-operator grafana/k6-operator -n k6-operator --create-namespace
```

Only worth the setup if you need to simulate hundreds of thousands of req/s.

---

### Tool selection summary

| Tool | Best for | Skip when |
|---|---|---|
| k6 | Multi-stage scenarios, goroutine leak detection (long runs) | Need precise RPS control |
| vegeta | Finding exact RPS breaking point, validating SLO at fixed rate | Complex multi-step scenarios |
| Chaos Mesh | Testing retry logic, connection pool recovery, pod failure | You don't have a chaos environment isolated from prod |
| wrk | Measuring theoretical throughput ceiling | You need latency distribution or scenario scripting |
| hey | Quick sanity check after a config change | Anything sustained or scenario-based |

---

## Thresholds to pass before calling it stable

These are the minimum bars. Adjust based on your actual SLOs.

| Metric | Target |
|---|---|
| p95 latency (sustained 50 VUs) | < 500ms |
| p99 latency (sustained 50 VUs) | < 2s |
| Error rate (non-503) | < 0.1% |
| Goroutine count after 20min sustained | Stable (not growing) |
| Recovery after spike | Latency returns to baseline within 60s |
| Pod restarts during any test | 0 |
