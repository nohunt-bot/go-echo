# pprof Integration and Usage

## Why a separate port

pprof cannot share the Echo main port. CPU profiles run for 30 seconds by default — the `ContextTimeout` middleware will kill the request before it finishes. The rate limiter will also start returning 429s on the repeated pprof HTTP calls. And the main port is public-facing; pprof should never be.

Spin up a separate `net/http` server using Go's standard `DefaultServeMux`, no middleware attached.

## How to wire it in

In `cmd/server/main.go`, after Echo starts:

```go
import _ "net/http/pprof" // side-effect import, registers handlers on DefaultServeMux

// inside run()
if pprofPort := os.Getenv("PPROF_PORT"); pprofPort != "" {
    go func() {
        log.Printf("pprof listening on :%s/debug/pprof/", pprofPort)
        if err := http.ListenAndServe(":"+pprofPort, nil); err != nil {
            log.Printf("pprof server error: %v", err)
        }
    }()
}
```

Local dev:

```bash
PPROF_PORT=6060 go run ./cmd/server
```

On K8s, patch the env — no image rebuild needed:

```yaml
env:
  - name: PPROF_PORT
    value: "6060"
```

If `PPROF_PORT` is not set, the goroutine never starts. Safe by default in prod.

---

## Profiles and when to use them

### Goroutine leak

This is the most common failure mode for this service. Pod memory slowly climbs, request queue depth keeps growing, eventually ends in OOMKill or liveness timeout.

Check whether the count is growing, then look at the stacks:

```bash
# Run this a minute apart and compare the totals
curl -s "http://localhost:6060/debug/pprof/goroutine?debug=1" | head -1

# Drop into the interactive interface
go tool pprof http://localhost:6060/debug/pprof/goroutine
(pprof) top
(pprof) traces
```

Common places goroutines get stuck:

- `gocql` query execute — connection pool exhausted, new requests queuing
- `redis: TxPipeline` or `EXEC` — retry storm, goroutines sleeping through backoff
- channel receive — concurrency limiter semaphore is full

### Block profile (high latency, low CPU)

CPU is idle, goroutine count is normal, but response time is still slow. This points to I/O blocking.

This profile is off by default. Enable it at startup — `rate=1` captures everything, use `100` in prod:

```go
runtime.SetBlockProfileRate(100)
```

```bash
go tool pprof -http=:8888 http://localhost:6060/debug/pprof/block
```

The widest frame in the flame graph is where the process is blocking the longest. If Cassandra or Redis I/O is above 80%, the bottleneck is in that layer — check connection pool sizing and timeout config.

### CPU profile

When CPU usage approaches the K8s limit, throttling kicks in and latency suffers even if I/O is fine.

```bash
go tool pprof -http=:8888\
  "http://localhost:6060/debug/pprof/profile?seconds=30"
```

Look at the widest frame in the flame graph. Common hotspots in this service: `encoding/json` from heavy serialization, and `runtime.mallocgc` from GC pressure when large objects are allocated per request.

### Heap profile (OOMKill)

A single snapshot doesn't tell you much. You need to diff two points in time to find what's growing:

```bash
curl -o /tmp/heap1.pb.gz "http://localhost:6060/debug/pprof/heap"
sleep 300
curl -o /tmp/heap2.pb.gz "http://localhost:6060/debug/pprof/heap"

go tool pprof -http=:8888 -base /tmp/heap1.pb.gz /tmp/heap2.pb.gz
```

In the interactive interface, sort by `inuse_space`. Anything that keeps growing and isn't being collected is your leak:

```
(pprof) top -inuse_space
(pprof) list <funcname>
```

### Mutex profile

Goroutine count is normal, CPU is fine, block profile shows nothing — but latency is still elevated. Check mutex contention as a last resort:

```go
runtime.SetMutexProfileFraction(1)
```

```bash
go tool pprof http://localhost:6060/debug/pprof/mutex
```

---

## Accessing pprof on K8s

The pprof port is not exposed externally. Use `kubectl port-forward`:

```bash
# Pick the pod with the highest CPU or memory
kubectl top pods -n <namespace> --sort-by=cpu | head -5

kubectl port-forward pod/<pod-name> 6060:6060 -n <namespace>
```

Open another terminal and run pprof commands against `localhost:6060`. Close the forward when you're done — don't leave it open.

---

## Decision tree

```
pod restarting or latency spike
  │
  ├─ OOMKilled                → heap diff (two snapshots)
  ├─ goroutine count climbing → goroutine profile + traces
  ├─ CPU near limit           → cpu profile, flame graph
  └─ all of the above normal  → block profile (I/O wait)
```
