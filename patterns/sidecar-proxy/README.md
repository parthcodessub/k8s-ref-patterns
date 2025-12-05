Kubernetes Observability: Prometheus Metrics with OpenTelemetry DaemonSet

This guide documents a scalable pattern for collecting application metrics in Kubernetes using the OpenTelemetry (OTEL) Collector running as a DaemonSet.

This pattern decouples the application from the monitoring infrastructure. The application simply exposes metrics, and the infrastructure "discovers" and scrapes them automatically.

1. The Application Layer (Golang)

The application has one job: Expose metrics on an HTTP endpoint. It does not know about the Collector, New Relic, or Datadog.

A. The Go Code (main.go)

Kubernetes observability: Prometheus metrics with an OpenTelemetry DaemonSet

This note documents a scalable pattern for collecting application metrics in Kubernetes using the OpenTelemetry (OTEL) Collector running as a DaemonSet.

The core idea: applications simply expose Prometheus-format metrics; the Collector discovers and scrapes them automatically using Kubernetes service discovery and pod annotations.

---

## 1) Application layer (Golang)

The application has one responsibility: expose metrics on an HTTP endpoint (for example `/metrics`). It should not be coupled to any specific backend (OTEL, New Relic, Datadog).

### A. Example Go code (`main.go`)

We use the official Prometheus client libraries. `promauto` registers metrics to the default registry; `promhttp` exposes an HTTP handler that serves metrics in Prometheus's exposition format.

```go
package main

import (
    "net/http"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

var opsProcessed = promauto.NewCounter(prometheus.CounterOpts{
    Name: "myapp_processed_ops_total",
    Help: "The total number of processed operations",
})

func main() {
    // increment opsProcessed where appropriate in your app logic

    // expose the metrics endpoint
    http.Handle("/metrics", promhttp.Handler())
    _ = http.ListenAndServe(":2112", nil)
}
```

### B. Deployment manifest (advertise metrics)

Annotate the Pod so the Collector knows it should scrape the target. Defining `containerPort` helps Kubernetes service discovery populate the target address automatically (`<PodIP>:<port>`).

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: metrics-app
spec:
  replicas: 1
  template:
    metadata:
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "2112"
        prometheus.io/path: "/metrics"
    spec:
      containers:
        - name: main-app
          image: your-image:latest
          ports:
            - containerPort: 2112
```

Key points:
- `prometheus.io/scrape: "true"` — tells the Collector to keep this target.
- `prometheus.io/port` — ensures the service discovery can populate the `__address__` label.

---

## 2) Infrastructure layer (OTEL Collector as a DaemonSet)

Run the OTEL Collector as a DaemonSet (one agent per node). This is often more resource-efficient than running a sidecar per pod because a single agent can scrape many pods on the same node.

### A. RBAC

The Collector needs RBAC permission to query the Kubernetes API (pods/nodes) for service discovery. Typical ClusterRole bindings include `get`, `list`, `watch` on `pods` and `nodes`.

### B. Collector configuration (high level)

- **Receiver**: use the `prometheus` receiver to scrape Prometheus-format endpoints.
- **Service discovery**: configure `kubernetes_sd_configs` with `role: pod` to discover pod targets.
- **Relabeling**: use `relabel_configs` to filter targets and map annotations into scrape targets.

Example relabeling rules (YAML excerpt):

```yaml
relabel_configs:
  # Keep only pods annotated with prometheus.io/scrape=true
  - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
    action: keep
    regex: true

  # Replace metrics path if an annotation is provided
  - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_path]
    action: replace
    target_label: __metrics_path__
    regex: (.+)
```


Key learning: if the `containerPort` is defined on the Pod spec, Kubernetes SD will populate `__address__` automatically (no need to manually build IP:port strings with regex).

### C. Enterprise configuration (Exporters)

In development you may use debug or file exporters to validate metrics flow. In an enterprise environment, configure an OTLP exporter (or vendor-specific exporter) to send metrics to a centralized observability backend. Keep credentials out of plain text — use Kubernetes `Secret`s or environment variables, and enable TLS for production endpoints.

Example OTLP exporter configuration (replace values with your vendor settings):

```yaml
exporters:
  otlp:
    endpoint: "api.honeycomb.io:443" # replace with vendor endpoint
    headers:
      "x-honeycomb-team": "YOUR_API_KEY" # authentication header (use secrets)
    tls:
      insecure: false

service:
  pipelines:
    metrics:
      receivers: [prometheus]
      processors: [batch]
      exporters: [otlp]
```

This keeps the application decoupled from the backend: operations can change the destination by updating the Collector configuration without touching application code.

---
---

## 3) Data flow (how it works)

1. Deploy `metrics-app`; it gets scheduled on a node (e.g., Pod IP `10.244.0.5`) and listens on port `2112`.
2. The OTEL Collector (DaemonSet) on the same node calls the Kubernetes API (or Kubelet) and discovers pods.
3. The Collector evaluates pod metadata: if `prometheus.io/scrape: "true"` and a port is present, it keeps the target.
4. The Collector scrapes `http://10.244.0.5:2112/metrics` at the configured scrape interval (for example, every 10s).
5. The Collector processes, batches, and exports metrics to a backend (stdout, file, or an observability backend).

---

## 4) Why this pattern is DevOps-friendly

- **Separation of concerns**: Developers only need to expose metrics and add annotations; operations manages the Collector and export pipelines.
- **Zero-touch scaling**: New services annotated for scraping are discovered automatically without updating Collector configs.
