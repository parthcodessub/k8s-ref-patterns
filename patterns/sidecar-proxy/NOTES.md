Kubernetes Observability: Prometheus Metrics with OpenTelemetry DaemonSet

This guide documents a scalable pattern for collecting application metrics in Kubernetes using the OpenTelemetry (OTEL) Collector running as a DaemonSet.

This pattern decouples the application from the monitoring infrastructure. The application simply exposes metrics, and the infrastructure "discovers" and scrapes them automatically.

1. The Application Layer (Golang)

The application has one job: Expose metrics on an HTTP endpoint. It does not know about the Collector, New Relic, or Datadog.

A. The Go Code (main.go)

We use the official Prometheus client libraries.

promauto: Automatically registers metrics (like Counters/Gauges) to the default registry.

promhttp: Provides an http.Handler that formats internal metrics into the text-based exposition format Prometheus expects.

package main

import (
    "net/http"
    "[github.com/prometheus/client_golang/prometheus](https://github.com/prometheus/client_golang/prometheus)"
    "[github.com/prometheus/client_golang/prometheus/promauto](https://github.com/prometheus/client_golang/prometheus/promauto)"
    "[github.com/prometheus/client_golang/prometheus/promhttp](https://github.com/prometheus/client_golang/prometheus/promhttp)"
)

// Define the metric
var opsProcessed = promauto.NewCounter(prometheus.CounterOpts{
    Name: "myapp_processed_ops_total",
    Help: "The total number of processed operations",
})

func main() {
    // ... logic to increment opsProcessed ...

    // Expose the metrics endpoint
    http.Handle("/metrics", promhttp.Handler())
    http.ListenAndServe(":2112", nil)
}


B. The Deployment Manifest

This is where we "advertise" our metrics to the cluster.

containerPort: 2112: This is critical. Kubernetes Service Discovery reads this port. If this is present, the OTEL collector automatically knows the target address is <PodIP>:2112.

Annotations: These act as a filter. We tell the collector "Scrape this pod, but ignore others."

apiVersion: apps/v1
kind: Deployment
metadata:
  name: metrics-app
spec:
  template:
    metadata:
      annotations:
        # The "Beacon" for the Collector
        prometheus.io/scrape: "true"
        prometheus.io/port: "2112"
        prometheus.io/path: "/metrics"
    spec:
      containers:
        - name: main-app
          ports:
            - containerPort: 2112 # <--- Enables automatic discovery


2. The Infrastructure Layer (OTEL DaemonSet)

The Collector runs as a DaemonSet (one agent per Node). This is efficient because 1 agent can scrape 20 pods on the same node, rather than running 20 sidecars.

A. RBAC (Permissions)

The Collector needs permission to talk to the Kubernetes API to ask: "What pods are running on this node?"

ClusterRole: Grants get, list, watch on pods and nodes.

B. The Collector Config (otel-config.yaml)

This is the brain of the operation.

1. Receiver (Prometheus)

We use the prometheus receiver, which is a full Prometheus server embedded inside the collector.

kubernetes_sd_configs: The "Service Discovery" module. We set role: pod. This queries the K8s API for all pods on the node.

2. Relabeling (The Logic)

Service Discovery finds everything. We use relabel_configs to filter the noise.

relabel_configs:
  # Rule 1: Filter
  # Look at the annotation 'prometheus.io/scrape'. 
  # If it is NOT 'true', drop this target.
  - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
    action: keep
    regex: true

  # Rule 2: Path Customization
  # If the app exposes metrics on /internal/metrics instead of /metrics,
  # we can read that from the annotation and update the scrape path.
  - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_path]
    action: replace
    target_label: __metrics_path__
    regex: (.+)


Key Learning: We do not need to manually construct the address (IP:Port) using regex replacement if the Deployment properly defines containerPort. Kubernetes Service Discovery automatically populates the __address__ label correctly in that scenario.

3. How It Works (The Data Flow)

Deployment: You deploy metrics-app. It lands on Node A, gets IP 10.244.0.5, and starts listening on port 2112.

Discovery: The OTEL Collector running on Node A polls the Kubelet/API. It sees a new pod.

Evaluation: The Collector checks the Pod's metadata.

Does it have prometheus.io/scrape: "true"? YES. -> Keep it.

Does it have a port defined? YES (2112). -> Target address is 10.244.0.5:2112.

Scrape: Every 10 seconds, the Collector sends an HTTP GET to http://10.244.0.5:2112/metrics.

Export: The app returns the metrics. The Collector batches them and exports them (to stdout/debug, file, or a backend like New Relic).

Why this pattern is "DevOps Friendly"

Separation of Concerns: Developers just add annotations. Ops manages the collector.

Zero-Touch: You can deploy 50 new microservices. As long as they have the annotation, the Collector starts tracking them instantly without config changes.