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


## 4) Real-world enterprise strategy — avoid manual coding

In large organizations with many services, you rarely write manual instrumentation for every process. Standard metrics typically come from three sources: infrastructure, runtime auto-instrumentation, and framework middleware.

### A. Infrastructure metrics (CPU, memory, disk)

- **Source:** Kubelet / cAdvisor
- **How it works:** The node reports CPU/Memory/Disk usage for pods. The OTEL Collector can scrape the Kubelet (or integrate with the metrics API) to collect pod-level resource metrics without any application changes.

### B. Runtime metrics (Python auto-instrumentation)

- **Source:** Auto-instrumentation agents (monkey patching)
- **How it works:** For interpreted languages (Python, Node.js), an auto-instrumentation wrapper bootstraps the runtime and injects instrumentation at import time. This requires no code changes in your app.

Example (Python wrapper):

```sh
opentelemetry-instrument \
  --traces_exporter console \
  --metrics_exporter prometheus_client \
  python main.py
```

Notes:
- The wrapper intercepts imports and replaces selected modules with instrumented versions.
- The auto-instrumentation agent may expose a Prometheus endpoint (commonly ports like `9464` or `8000`).
- Ensure your Pod annotations match the agent port, e.g.:

```yaml
prometheus.io/port: "9464"
```

### C. Framework metrics (Go / middleware)

- **Source:** Framework middleware (configuration over code)
- **How it works:** In compiled languages such as Go, you typically add middleware once (for example in your router) to enable HTTP/server-level metrics. This requires a one-time change rather than copying metric boilerplate everywhere.

Example (Gin + OpenTelemetry middleware):

```go
import (
  "github.com/gin-gonic/gin"
  "go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func main() {
  r := gin.Default()
  // one line to enable HTTP metrics/traces
  r.Use(otelgin.Middleware("my-service-name"))

  r.GET("/users", func(c *gin.Context) {
    c.JSON(200, gin.H{"status": "ok"})
  })
  _ = r.Run(":8080")
}
```

Metrics typically exposed by middleware:

- `http_server_duration_milliseconds_bucket` (latency histogram)
- `http_requests_total` (counter with status labels)
- `http_request_content_length`

If middleware serves metrics on the application port, annotate the Pod accordingly:

```yaml
prometheus.io/port: "8080"
```

Summary: manual instrumentation (as in Section 1) is usually reserved for custom business metrics. System, runtime, and framework metrics are commonly automated via agents or middleware.

## 5) Why this pattern is DevOps-friendly

- **Separation of concerns**: Developers only need to expose metrics and add annotations; operations manages the Collector and export pipelines.
- **Zero-touch scaling**: New services annotated for scraping are discovered automatically without updating Collector configs.


## 6) Hybrid instrumentation strategy (Combining auto + manual)

In practice you'll often combine auto-instrumentation (runtime/framework agents) with manual business metrics. The goal is to make common metrics automatic while still allowing custom metrics where needed.

### A. Single-endpoint model (Go)

In compiled languages like Go it's common to register all metric sources to a single Prometheus registry and expose one `/metrics` endpoint.

Example patterns:

```go
// runtime collector
prometheus.MustRegister(collectors.NewGoCollector())

// middleware (one-time setup)
router.Use(otelgin.Middleware("my-service-name"))

// custom business metric
opsProcessed.Inc()

// single handler serves everything
http.Handle("/metrics", promhttp.Handler())
```

Outcome: the Collector scrapes one port (for example `:2112`) and receives runtime, middleware, and custom metrics in one request.

### B. The two-port problem (Python / Java agents)

Auto-instrumentation agents often expose their own Prometheus endpoint on a different port (e.g., `9464`) while the app serves business metrics on the application port (e.g., `5000`). Kubernetes Pod annotations commonly allow only a single `prometheus.io/port`, so pulling both endpoints from the DaemonSet can be awkward.

Problems this creates:
- Hard to configure a single scrape target for both agent and app metrics
- Increased operational complexity when plugins/agents change default ports

### C. Solution: use OTLP (push) rather than separate Prometheus pulls

When agents and app code can push metrics, prefer OTLP (gRPC/HTTP) to centralize streams without relying on multiple scrape ports.

How it works:

- Configure the auto-instrumentation agent to export via OTLP (local or to the Collector):

```sh
OTEL_METRICS_EXPORTER=otlp
# agent-specific config: set OTLP endpoint to the local collector (e.g. localhost:4317)
```

- Configure your manual instrumentation to export via OTLP to the same Collector.
- The Collector receives (pulling is not needed), merges streams, and forwards metrics to configured exporters.

Benefits:
- Avoids multi-port scraping complexity
- Centralizes processing, batching, and export logic in the Collector
- Easier to secure and manage (TLS, auth) in one place

Recommendation: when an auto-agent exposes metrics on a separate port, prefer pushing via OTLP to the Collector and use the Collector as the single aggregation/forwarding point.
