# Kubernetes Node-Agent Pattern: Prometheus Metrics with OpenTelemetry

This project demonstrates the **Node Agent Pattern** (often implemented as a DaemonSet) for Kubernetes observability.

> **Architectural Note**: While often grouped with "sidecars" in conversation, this pattern is distinct. It creates a **1-to-N relationship** (one collector agent serving many application pods) rather than the 1-to-1 relationship of a true sidecar.

## 1. Concept: Node Agent vs. Sidecar

Understanding the difference is critical for cost and performance at scale.

| Feature | Sidecar Pattern | Node Agent Pattern (This Project) |
| :--- | :--- | :--- |
| **Topology** | 1 Agent per Pod. | 1 Agent per Node. |
| **Relationship** | The agent lives *inside* the App Pod. | The agent lives in its *own* Pod (DaemonSet). |
| **Network** | Communicates via `localhost`. | Communicates via Pod IP (Network). |
| **Resource Cost** | **High**. (100 Apps = 100 Agents). | **Low**. (100 Apps = ~5 Agents). |
| **Best For** | Service Mesh (Envoy), Complex Auth Proxies, strict tenant isolation. | Telemetry (Logs/Metrics), Security Scanning. |

## 2. Project Structure

Recommended folder name: `patterns/daemonset-collector/`

```text
patterns/daemonset-collector/
├── app/
│   ├── main.go        # The "App" (Exposes /metrics on port 2112)
│   └── Dockerfile
└── infra/
    ├── manifests/
    │   ├── deployment.yaml      # App Deployment (annotated for scraping)
    │   └── otel-daemonset.yaml  # The Node Agent (One Collector per Node)
```



60: 
61: ## 3. Architecture Diagram
62: 
63: This diagram illustrates how the DaemonSet Collector (Node Agent) interacts with multiple application pods on each node and reports to a central backend.
64: 
65: ```mermaid
66: graph TD
67:     subgraph "Kubernetes Cluster"
68:         subgraph "Node A (10.0.0.1)"
69:             AppA1[App Pod 1<br/>(10.244.0.5)]
70:             AppA2[App Pod 2<br/>(10.244.0.6)]
71:             CollectorA[<b>DaemonSet Pod</b><br/>(OTEL Collector)]
72:             
73:             CollectorA -->|Scrape HTTP| AppA1
74:             CollectorA -->|Scrape HTTP| AppA2
75:         end
76: 
77:         subgraph "Node B (10.0.0.2)"
78:             AppB1[App Pod 3<br/>(10.244.1.8)]
79:             CollectorB[<b>DaemonSet Pod</b><br/>(OTEL Collector)]
80:             
81:             CollectorB -->|Scrape HTTP| AppB1
82:         end
83: 
84:         API[Kubernetes API]
85:     end
86: 
87:     subgraph "External Observability"
88:         Backend[Central Backend<br/>(Datadog / Honeycomb / Prometheus)]
89:     end
90: 
91:     %% Discovery Flow
92:     CollectorA -.->|Watch/List| API
93:     CollectorB -.->|Watch/List| API
94: 
95:     %% Export Flow
96:     CollectorA ==>|Push (OTLP)| Backend
97:     CollectorB ==>|Push (OTLP)| Backend
98: 
99:     style CollectorA fill:#f9f,stroke:#333,stroke-width:2px
100:     style CollectorB fill:#f9f,stroke:#333,stroke-width:2px
101: ```
102: 
103: ## 4. Implementation Details

### A. The Application Layer (Golang)

The application has one job: Expose metrics on an HTTP endpoint. It does not know about the Collector, New Relic, or Datadog.

**The Go Code (`main.go`)**:

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
    
    // Listen on port 2112 (Standard Prometheus port convention)
    _ = http.ListenAndServe(":2112", nil)
}
```

**Deployment Manifest (Advertising Metrics)**:

Annotate the Pod so the Collector knows it should scrape the target. Defining `containerPort` helps Kubernetes service discovery populate the target address automatically.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: metrics-app
spec:
  template:
    metadata:
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "2112"
        prometheus.io/path: "/metrics"
    spec:
      containers:
        - name: main-app
          image: metrics-app:v1
          ports:
            - containerPort: 2112
```

### B. Infrastructure Layer (OTEL Collector as DaemonSet)

We run the OTEL Collector as a DaemonSet (one agent per node). This is more resource-efficient than running a sidecar per pod because a single agent can scrape many pods on the same node.

#### 1. RBAC Permissions
The Collector needs RBAC permission to query the Kubernetes API (pods and nodes) for service discovery.
*   **ClusterRole**: `get`, `list`, `watch` on `pods` and `nodes`.

#### 2. Collector Configuration (High Level)
*   **Receiver**: Use the `prometheus` receiver to scrape endpoints.
*   **Service Discovery**: Configure `kubernetes_sd_configs` with `role: pod`.
*   **Relabeling**: Use `relabel_configs` to filter targets and map annotations.

**Example Relabeling Rules**:
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

#### 3. Enterprise Configuration (Exporters)
In production, you configure an OTLP exporter to send metrics to a centralized backend (like Honeycomb, Datadog, or managed Prometheus).

```yaml
exporters:
  otlp:
    endpoint: "api.honeycomb.io:443"
    headers:
      "x-honeycomb-team": "${env:HONEYCOMB_API_KEY}"
    tls:
      insecure: false
```

#### 4. Data Flow (How it works)
1.  **Deploy**: `metrics-app` is scheduled on a node (e.g., IP `10.244.0.5`) and listens on port 2112.
2.  **Discover**: The OTEL Collector (DaemonSet) on that node polls the Kubernetes API and discovers the new pod.
3.  **Filter**: The Collector checks the pod's metadata. It sees `prometheus.io/scrape: "true"` and adds it to the target list.
4.  **Scrape**: The Collector requests `http://10.244.0.5:2112/metrics` every 10 seconds.
5.  **Export**: The Collector processes the metrics and pushes them to the backend.

## 5. Real-World Enterprise Strategy (Beyond Manual Code)

In large organizations, you rarely write manual instrumentation for every process. Metrics typically come from three sources:

### A. Infrastructure Metrics (CPU, Memory)
*   **Source**: Kubelet / cAdvisor.
*   **How**: The Collector scrapes the Kubelet API directly. No app changes needed.

### B. Runtime Metrics (Python/Node Auto-instrumentation)
*   **Source**: Auto-instrumentation agents (monkey patching).
*   **How**: An agent wraps the runtime at startup.

```bash
opentelemetry-instrument --metrics_exporter prometheus_client python main.py
```

> **Note**: These often expose metrics on a different port (e.g., 9464). You must ensure your Pod annotations match this port.

### C. Framework Metrics (Go Middleware)
*   **Source**: Middleware libraries (Gin, Echo, net/http).
*   **How**: Add one line of middleware to your router.

```go
r.Use(otelgin.Middleware("my-service-name"))
```

*   **Metrics**: `http_server_duration_milliseconds`, `http_requests_total`.

## 6. Hybrid Instrumentation Strategy

You will often combine auto-instrumentation with manual business metrics.

### A. The "Two-Port Problem"
Auto-instrumentation agents often start their own HTTP server (e.g., port 9464) to serve metrics, while your app serves business logic on port 8080.
*   **Issue**: Kubernetes annotations usually support only one scrape port (`prometheus.io/port`).
*   **Result**: You might need two sets of annotations or complex relabeling to scrape both.

### B. Solution: OTLP Push
Instead of having the Collector pull (scrape) from the app, configure the App/Agent to **push** metrics via OTLP to the local Collector.
1.  **App Config**: `OTEL_METRICS_EXPORTER=otlp`
2.  **Collector Config**: Enable the `otlp` receiver (gRPC/HTTP).
3.  **Benefit**: No need to manage scrape ports or annotations for every service.

## 7. Engineering Best Practices & Gotchas

### 1. The "Localhost" Trap
*   **Scenario**: You move from a Sidecar to a DaemonSet.
*   **The Error**: Your configuration tries to scrape `localhost:2112`.
*   **Why**: In a DaemonSet, `localhost` refers to the **Node Agent's pod**, not the Application's pod.
*   **Fix**: Ensure your discovery config uses `role: pod` so it resolves the actual Pod IP (e.g., 10.x.x.x).

### 2. Scale & Resource Efficiency
Why DaemonSet? If you have 50 microservices running 2 replicas each (100 pods total) on 5 Nodes:
*   **Sidecar Approach**: 100 App Containers + 100 Collector Containers. (Heavy Memory usage).
*   **DaemonSet Approach**: 100 App Containers + 5 Collector Containers. (Huge savings).

### 3. Security Boundaries
*   **DaemonSet Risk**: The Node Agent needs broad RBAC permissions (list pods cluster-wide) to discover targets.
*   **Sidecar Advantage**: A sidecar only needs permissions relevant to its specific parent pod. In highly sensitive multi-tenant clusters, Sidecars may be preferred for stricter isolation.

## 8. Advanced DaemonSet Configurations (Lead Engineer Tips)

To make your DaemonSet truly production-ready, you must go beyond the basic `kind: DaemonSet` manifest.

### 8.1. Running on Control Plane Nodes (Tolerations)
**Problem**: By default, DaemonSets do not schedule on Control Plane (Master) nodes because they are "Tainted" (`node-role.kubernetes.io/master:NoSchedule`).
**Fix**: Add tolerations to ensure your metrics collector runs *everywhere*, including the master nodes (which also need monitoring!).

```yaml
spec:
  template:
    spec:
      tolerations:
      # Allow execution on Control Plane nodes
      - key: node-role.kubernetes.io/master
        operator: Exists
        effect: NoSchedule
      - key: node-role.kubernetes.io/control-plane
        operator: Exists
        effect: NoSchedule
```

### 8.2. Protecting the Collector (PriorityClass)
**Problem**: When a Node runs out of memory (OOM), Kubernetes evicts pods. If it evicts your Metrics Collector, you go blind exactly when you need visibility the most.
**Fix**: Assign a high `PriorityClass` so Kubernetes kills the application pods *before* it kills the monitoring agent.

```yaml
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: monitor-critical
value: 1000000
globalDefault: false
description: "Ensure Monitoring Agents are never evicted."
---
apiVersion: apps/v1
kind: DaemonSet
spec:
  template:
    spec:
      priorityClassName: monitor-critical
```

### 8.3. Safe Rollouts (UpdateStrategy)
**Problem**: By default, a DaemonSet configuration update might restart all agents simultaneously or too quickly, causing a gap in metrics.
**Fix**: Use `RollingUpdate` with `maxUnavailable`. Setting `maxUnavailable: 1` ensures only one node loses its collector at a time.

```yaml
spec:
  updateStrategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
```

### 8.4. HostPath & HostPort (The "Break Glass" Access)
Sometimes the DaemonSet needs access to the underlying Host.

*   **HostPath**: Used for Log Collection. You mount `/var/log/pods` from the Node into the DaemonSet container so the agent can tail the log files directly.
*   **HostPort**: Used if the DaemonSet needs to receive traffic on a static IP/Port (e.g., acting as a Syslog receiver on port 514). *Use with caution: ports cannot conflict.*

```yaml
spec:
  containers:
  - name: collector
    volumeMounts:
    - name: varlog
      mountPath: /var/log
  volumes:
  - name: varlog
    hostPath:
      path: /var/log
```