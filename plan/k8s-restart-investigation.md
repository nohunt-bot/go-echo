# K8s DAO Service — Restart Investigation

## Background

Go DAO service running on Kubernetes. Downstream systems pull data through it from Cassandra, with Redis as a caching layer. Pods keep restarting; individual requests are taking up to **6 seconds**.

---

## Most likely cause

The liveness probe `failureThreshold × periodSeconds` is probably lower than 6 seconds. Once a slow request causes the probe to miss enough consecutive checks, K8s marks the pod unhealthy and kills it. Confirm this first before digging into the application layer.

---

## Step 1: Confirm why the pod is restarting (K8s layer)

```bash
# Full pod events — look for "Liveness probe failed" or "OOMKilled"
kubectl describe pod <pod-name> -n <namespace>

# Filter the noise
kubectl get events -n <namespace> \
  --sort-by='.lastTimestamp' \
  | grep -i "liveness\|restart\|kill\|oom"

# Check current liveness probe config
kubectl get deployment <name> -o yaml | grep -A 15 livenessProbe

# Log from the previous crash
kubectl logs <pod-name> --previous

# Check if it was OOMKilled
kubectl get pod <pod-name> \
  -o jsonpath='{.status.containerStatuses[*].lastState.terminated}'
```

Key thing to check: is `failureThreshold × periodSeconds` less than 6 seconds?

---

## Step 2: Application layer (Go pprof)

See `pprof-guide.md` for full setup. The short version — make sure `PPROF_PORT` is set and use `kubectl port-forward` to reach it.

```bash
# Goroutine count — if it keeps climbing, something is leaking or backing up
go tool pprof http://localhost:6060/debug/pprof/goroutine

# Block profile — find which I/O call is blocking (requires SetBlockProfileRate)
go tool pprof http://localhost:6060/debug/pprof/block

# Mutex contention
go tool pprof http://localhost:6060/debug/pprof/mutex

# CPU flame graph
go tool pprof -http=:8888 \
  "http://localhost:6060/debug/pprof/profile?seconds=30"
```

| Signal | Normal | Abnormal |
|---|---|---|
| Goroutine count | Stable | Keeps climbing |
| Block profile | No long waits | I/O wait concentrated on Cassandra / Redis |
| Heap alloc | Stable | Spiking before OOM |

---

## Step 3: Cassandra

```bash
nodetool status
nodetool tpstats   # look for Dropped or Blocked in the thread pool stats
```

Enable slow query logging in `cassandra.yaml`:

```yaml
slow_query_log_timeout_in_ms: 500
```

Add temporary timing around queries to pinpoint which ones are slow:

```go
start := time.Now()
err := q.ExecRelease()
log.Printf("cassandra query took %v, table=%s", time.Since(start), table)
```

Things to check in the gocql config:

- `NumConns` — is the per-host connection count high enough for this traffic?
- `Timeout` — is the query timeout being cancelled early by the request context?
- `RetryPolicy` — is retry backoff compounding the latency under high load?

---

## Step 4: Redis

```bash
redis-cli SLOWLOG GET 50
redis-cli SLOWLOG LEN

redis-cli CLUSTER INFO
redis-cli CLUSTER NODES

# Use with caution in prod — high traffic makes MONITOR expensive
redis-cli MONITOR
```

Watch out for retry storms. The `withRetry` helper (3 retries, exponential backoff 100ms → 1s) can turn a short Redis hiccup into a cascade: many goroutines retrying simultaneously, each sleeping through backoff, then all hitting Redis at once again.

```bash
# Check whether retry log volume spikes during high traffic
grep "retry" <app-log> | awk '{print $1}' | sort | uniq -c
```

---

## Step 5: Resources

```bash
kubectl top pods -n <namespace>
kubectl top nodes
kubectl logs <pod-name> -f
```

---

## Tooling reference

| Layer | Tool | What you're looking for |
|---|---|---|
| K8s | `kubectl describe`, `kubectl get events` | Restart cause — liveness vs OOMKill |
| Go app | `pprof` (goroutine, block, mutex, cpu) | Goroutine leak, I/O block, CPU hotspot |
| Cassandra | `nodetool`, slow query log | Slow queries, thread pool drops |
| Redis | `SLOWLOG`, `CLUSTER INFO` | Slow commands, unhealthy cluster nodes |
| Distributed trace | Jaeger / OpenTelemetry | Which layer is eating the 6 seconds |
| Metrics | Prometheus + Grafana | Goroutine trends, latency histogram, error rate |

---

## Where to start

1. `kubectl describe pod` — liveness timeout or OOMKill?
2. `pprof/goroutine` — is the count climbing? Fastest signal.
3. Per-layer timing log — is the 6s coming from Cassandra or Redis?
4. `nodetool tpstats` — any Dropped messages in the Cassandra thread pool?
5. `SLOWLOG GET` + retry log volume — is Redis the bottleneck, or is it a retry storm?
