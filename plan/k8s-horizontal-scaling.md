# Horizontal Pod Scaling: Time-Based & Condition-Based

Two complementary approaches: **HPA** reacts to live metrics; **scheduled scaling** prepares for known traffic patterns before load arrives. Use them together.

---

## 1. Condition-Based: Horizontal Pod Autoscaler (HPA)

HPA watches a metric and adjusts the replica count when it crosses a threshold.

### CPU / Memory (built-in)

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: go-echo-hpa
  namespace: <namespace>
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: <your-deployment>
  minReplicas: 2
  maxReplicas: 20
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 60   # scale up when avg CPU > 60%
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 70
```

> **Rule of thumb:** target 60–70% CPU, not 80%+. Leaving headroom means new pods are ready before existing ones saturate.

### Tuning Scale Behavior

Prevent flapping with `behavior` — scale up fast, scale down slowly:

```yaml
spec:
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 30     # react within 30s
      policies:
        - type: Percent
          value: 100                     # allow doubling per window
          periodSeconds: 30
    scaleDown:
      stabilizationWindowSeconds: 300    # wait 5 min before shrinking
      policies:
        - type: Pods
          value: 2                       # remove at most 2 pods at a time
          periodSeconds: 60
```

### Custom Metrics (e.g. RPS, Queue Depth)

Requires the [Prometheus Adapter](https://github.com/kubernetes-sigs/prometheus-adapter) or similar:

```yaml
metrics:
  - type: Pods
    pods:
      metric:
        name: http_requests_per_second
      target:
        type: AverageValue
        averageValue: "500"    # scale when avg RPS per pod > 500
```

Apply and verify:

```bash
kubectl apply -f hpa.yaml
kubectl get hpa go-echo-hpa -n <namespace> -w
```

---

## 2. Time-Based: Scheduled Scaling

HPA cannot anticipate known traffic spikes (marketing campaigns, business hours, batch jobs). Pre-scale before load arrives.

### Option A — KEDA CronScaler (recommended)

[KEDA](https://keda.sh) manages a `ScaledObject` that overrides HPA replica counts on a cron schedule.

```yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: go-echo-cron-scaler
  namespace: <namespace>
spec:
  scaleTargetRef:
    name: <your-deployment>
  minReplicaCount: 2
  maxReplicaCount: 20
  triggers:
    - type: cron
      metadata:
        timezone: Asia/Taipei
        start: "0 8 * * 1-5"    # Mon–Fri 08:00 — scale up
        end:   "0 20 * * 1-5"   # Mon–Fri 20:00 — scale down
        desiredReplicas: "10"
    - type: cron
      metadata:
        timezone: Asia/Taipei
        start: "30 11 * * 5"    # Friday 11:30 — pre-scale for weekend
        end:   "0 9 * * 1"      # Monday 09:00 — return to normal
        desiredReplicas: "6"
```

Install KEDA if not already present:

```bash
helm repo add kedacore https://kedacore.github.io/charts
helm install keda kedacore/keda --namespace keda --create-namespace
```

### Option B — kubectl patch via CronJob (no KEDA)

A lightweight alternative using a Kubernetes CronJob to patch the deployment directly.

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: scale-up-business-hours
  namespace: <namespace>
spec:
  schedule: "0 8 * * 1-5"        # Mon–Fri 08:00 (UTC — adjust as needed)
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: scaler-sa  # needs patch permission on deployments
          restartPolicy: OnFailure
          containers:
            - name: kubectl
              image: bitnami/kubectl:latest
              command:
                - kubectl
                - scale
                - deployment/<your-deployment>
                - --replicas=10
                - -n
                - <namespace>
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: scale-down-off-hours
  namespace: <namespace>
spec:
  schedule: "0 20 * * 1-5"
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: scaler-sa
          restartPolicy: OnFailure
          containers:
            - name: kubectl
              image: bitnami/kubectl:latest
              command:
                - kubectl
                - scale
                - deployment/<your-deployment>
                - --replicas=2
                - -n
                - <namespace>
```

Required RBAC for the CronJob service account:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: deployment-scaler
  namespace: <namespace>
rules:
  - apiGroups: ["apps"]
    resources: ["deployments", "deployments/scale"]
    verbs: ["get", "patch", "update"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: deployment-scaler-binding
  namespace: <namespace>
subjects:
  - kind: ServiceAccount
    name: scaler-sa
roleRef:
  kind: Role
  name: deployment-scaler
  apiGroup: rbac.authorization.k8s.io
```

---

## 3. Combining HPA + Scheduled Scaling

The recommended production setup: scheduled scaling sets the floor, HPA handles unexpected bursts.

```
Traffic pattern      Replicas
─────────────────────────────────────────────
Off-hours            min 2   (HPA floor)
Business hours       min 10  (cron override)
Sudden spike         up to 20 (HPA scaleUp)
Post-spike           back to 10 (HPA scaleDown, stabilized)
Off-hours again      back to 2 (cron override)
```

When using KEDA + HPA together, KEDA manages the `minReplicas` floor; HPA manages the upper range. Do not set conflicting `minReplicas` in both objects.

---

## 4. Prerequisites: Readiness Probes

Scaling only prevents anomalies if new pods are ready before traffic reaches them. Ensure your deployment has a readiness probe:

```yaml
readinessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5
  failureThreshold: 3
```

Without this, Kubernetes routes traffic to pods that haven't finished starting up.

---

## 5. Monitoring

```bash
# Watch HPA status and current replica count
kubectl get hpa -n <namespace> -w

# Check HPA events (scale up/down decisions)
kubectl describe hpa go-echo-hpa -n <namespace>

# Check CronJob history
kubectl get cronjobs -n <namespace>
kubectl get jobs -n <namespace>

# Current replica count
kubectl get deployment <your-deployment> -n <namespace>
```

Key Prometheus metrics to alert on:

| Metric | Alert condition |
|---|---|
| `kube_horizontalpodautoscaler_status_current_replicas` | Equals `maxReplicas` for > 5 min |
| `kube_deployment_status_replicas_unavailable` | > 0 |
| `kube_pod_container_status_restarts_total` | Rate > 0 during scale events |

---

## Decision Tree

```
Known traffic spike at a fixed time?
  ├─ Yes → scheduled scaling (KEDA cron or CronJob)
  └─ No  → HPA on CPU/memory/custom metrics
              │
              └─ Both patterns present?
                   └─ Yes → HPA + KEDA together (floor + burst)
```
