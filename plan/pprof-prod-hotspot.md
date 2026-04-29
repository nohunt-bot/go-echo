# Enabling pprof in Production & Hotspot Investigation

## Core Principles

Three rules for pprof in production: keep it internal, shut it down when done, and don't set the sampling rate too high.

The server won't start a pprof listener unless `PPROF_PORT` is set, so there's no risk of it being exposed by default. Enable it via env when you need to investigate, then remove it when done.

---

## How to Enable

No need to rebuild the image — just patch the env to trigger a rolling update:

```bash
kubectl set env deployment/<your-deployment> \
  PPROF_PORT=6060 \
  -n <namespace>

kubectl rollout status deployment/<your-deployment> -n <namespace>
```

Once the rollout completes, confirm pprof is running by checking the logs:

```bash
kubectl logs <pod-name> -n <namespace> | grep pprof
# pprof listening on :6060/debug/pprof/
```

---

## How to Connect

The pprof port is only accessible inside the pod. Use `port-forward` to expose it locally. Pick the pod with the highest CPU or memory usage:

```bash
kubectl top pods -n <namespace> --sort-by=cpu | head -5

kubectl port-forward pod/<pod-name> 6060:6060 -n <namespace> &
```

Verify the tunnel is working:

```bash
curl -s http://localhost:6060/debug/pprof/
```

---

## Investigating Hotspots

### High CPU

When a pod's CPU approaches its limit, Kubernetes throttles it — latency degrades even if I/O looks healthy. Start here:

```bash
go tool pprof -http=:8888 \
  "http://localhost:6060/debug/pprof/profile?seconds=30"
```

Open the Flame Graph in the browser and look for the widest frames. Common patterns for this service:

| Flame graph observation | Direction |
|---|---|
| `encoding/json` is wide | Too much serialization — reduce allocations or consider sonic |
| `cassandra` driver is wide | Result set too large, or queries too frequent without caching |
| `runtime.mallocgc` is wide | GC pressure — large objects being allocated per request |
| `syscall` is wide | Frequent I/O syscalls — connection pool may be undersized |

### Goroutine Buildup

Spiking response times or pods restarting repeatedly usually points here. First check whether the goroutine count is growing:

```bash
# Run once, wait a minute, run again — if the number keeps climbing, something is stuck
curl -s "http://localhost:6060/debug/pprof/goroutine?debug=1" | head -1
```

Then inspect the stacks:

```bash
go tool pprof http://localhost:6060/debug/pprof/goroutine
(pprof) top 10
(pprof) traces
```

| Where goroutines are stuck | Where to look |
|---|---|
| `gocql` query execute | Cassandra connection pool exhausted — check `NumConns` setting |
| `redis` pipeline / TxPipeline | Retry storm — check whether retry log volume has spiked |
| `time.Sleep` | Goroutines waiting in backoff |
| channel receive | Semaphore full — concurrency limiter is blocking |

### Memory Leak (Before OOMKill)

A single snapshot won't tell you much — compare two points in time:

```bash
curl -o /tmp/heap1.pb.gz "http://localhost:6060/debug/pprof/heap"
sleep 300
curl -o /tmp/heap2.pb.gz "http://localhost:6060/debug/pprof/heap"

go tool pprof -http=:8888 \
  -base /tmp/heap1.pb.gz /tmp/heap2.pb.gz
```

In the diff, anything with steadily growing `inuse_space` is the source of the leak.

### High Latency with Normal CPU and Goroutines

This usually means I/O blocking. The block profile is disabled by default — enable it at startup:

```go
runtime.SetBlockProfileRate(100) // don't set to 1 in production — too expensive
```

```bash
go tool pprof -http=:8888 http://localhost:6060/debug/pprof/block
```

The widest frame in the Flame Graph is the I/O call blocking the longest.

---

## How to Disable

```bash
kubectl set env deployment/<your-deployment> \
  PPROF_PORT- \
  -n <namespace>

kubectl rollout status deployment/<your-deployment> -n <namespace>
```

The trailing `-` in `PPROF_PORT-` is the kubectl syntax for removing an env var.

---

## Quick Reference

```bash
# CPU (30-second sample)
go tool pprof -http=:8888 "http://localhost:6060/debug/pprof/profile?seconds=30"

# Goroutine
go tool pprof -http=:8888 http://localhost:6060/debug/pprof/goroutine

# Memory diff
go tool pprof -http=:8888 -base /tmp/heap1.pb.gz /tmp/heap2.pb.gz

# Block
go tool pprof -http=:8888 http://localhost:6060/debug/pprof/block
```

## Symptom Decision Tree

```
restart / latency anomaly
  ├─ OOMKilled                  → heap diff
  ├─ goroutine count growing    → goroutine + traces
  ├─ CPU near limit             → cpu flame graph
  └─ all of the above normal   → block profile
```
