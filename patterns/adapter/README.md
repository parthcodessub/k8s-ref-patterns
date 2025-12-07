 # Kubernetes Adapter Pattern — "The Translator"

This repository demonstrates the Adapter (sidecar) pattern in Kubernetes. A legacy application writes metrics in a proprietary format to a shared file; an adapter container (sidecar) reads that file, transforms the data, and exposes Prometheus-compatible metrics over HTTP.

---

## 1 — Sidecar vs Adapter (Quick Comparison)

| Concern | Sidecar ("Helper") | Adapter ("Translator") |
|---|---:|:---|
| Primary goal | Extend or augment functionality (logs, syncs) | Standardize or translate interfaces |
| Analogy | Motorcycle sidecar (adds capacity) | Travel power adapter (changes plug shape) |
| Data flow | Parallel / independent | Intercept and transform |
| Typical examples | Fluentd, Envoy | JMX exporter, log normalization, auth proxy |

---

## 2 — Project Layout

```
patterns/adapter/
├── legacy/
│   ├── main.py        # Legacy app (writes metrics to file)
│   └── Dockerfile
├── adapter/
│   ├── adapter.py     # Adapter (reads file, serves HTTP /metrics)
│   └── Dockerfile
└── manifests/
    └── deployment.yaml # K8s Deployment (1 Pod, 2 containers)
```

---

## 3 — Implementation Details

### A. Problem (Legacy app)

The legacy Python app cannot be modified. It writes status lines to `/var/log/app/status.txt` in a proprietary format, for example:

```
SystemStatus: OK
MemUsageMB: 450
CPU_Load: 32
```

### B. Solution (Adapter sidecar)

- The adapter container shares the same volume (`/var/log/app`) with the legacy app.
- It reads and parses `status.txt`.
- It converts values as needed (e.g., MB → bytes).
- It exposes the translated metrics at `http://localhost:8080/metrics` in Prometheus format.

### C. Deployment snippet (shared volume)

```yaml
volumes:
  - name: shared-logs
    emptyDir: {}

containers:
  - name: legacy-app
    # ...
    volumeMounts:
      - name: shared-logs
        mountPath: /var/log/app

  - name: adapter-sidecar
    # ...
    volumeMounts:
      - name: shared-logs
        mountPath: /var/log/app
        readOnly: true
```

Notes:
- `emptyDir` lifecycle is tied to the Pod (ephemeral).
- The adapter mounts the shared volume read-only to avoid corrupting logs.

---

## 4 — How to run (local / Minikube)

1) Build images locally:

```bash
# If using Minikube, expose Docker env: eval $(minikube docker-env)
docker build -t legacy-app:v1 ./legacy
docker build -t adapter-app:v1 ./adapter
```

2) Deploy to cluster:

```bash
kubectl apply -f manifests/deployment.yaml
```

3) Verify and test (port-forward):

```bash
POD=$(kubectl get pods -l app=adapter-demo -o jsonpath="{.items[0].metadata.name}")
kubectl port-forward $POD 8080:8080
curl http://localhost:8080/metrics
```

Expected Prometheus output example:

```
# HELP legacy_memory_usage_bytes Memory usage converted to bytes
# TYPE legacy_memory_usage_bytes gauge
legacy_memory_usage_bytes 471859200
```

---

## 5 — Troubleshooting notes (real-world debugging)

- Symptom: `CrashLoopBackOff` for the adapter container.
- To inspect logs for a specific container:

```bash
kubectl logs deployment/adapter-pattern-demo -c adapter-sidecar --previous
```

- Common causes: Docker build context missing files, syntax/indentation errors in `adapter.py`, incorrect volume mounts.
- Fix: correct the Dockerfile context, rebuild images, re-deploy.

---

## 6 — Gotchas & Best Practices

- Use a Deployment (not a raw Pod) for self-healing and rescheduling.
- `emptyDir` is ephemeral — not suitable for long-term persistence.
- Scaling: each Pod gets its own `emptyDir` and adapter instance; Prometheus scrapes each Pod separately.
- Add a `livenessProbe` for the adapter to ensure `/metrics` is responsive.
- Python logs are buffered by default; set `PYTHONUNBUFFERED=1` or run `python -u` in the container to get real-time logs.

---

## 7 — Common Enterprise Use Cases
1. Java Monolith → JMX exporter

- Problem: Large, legacy Java applications expose operational metrics via JMX (Java Management Extensions). JMX is a binary/Java-native protocol and cannot be scraped by Prometheus directly.

- Adapter approach: run a JMX-to-Prometheus exporter as a sidecar. Two common modes:
  - Attach as a Java agent inside the same JVM (requires JVM arg changes).
  - Run as a separate container that connects to the app's exposed JMX port (remote JMX) and exposes `/metrics` over HTTP.

- Typical topology and ports:
  - Java app: exposes JMX on `localhost:1099` (or configured JMX port).
  - JMX exporter sidecar: connects to `localhost:1099`, exposes Prometheus endpoint on `:9404` (example).

- Minimal container snippet (conceptual):

```yaml
- name: jmx-exporter
  image: prom/jmx-exporter:latest
  args: ["/etc/jmx-exporter/config.yml"]
  ports:
    - containerPort: 9404
  env:
    - name: JMX_HOST
      value: "localhost"
    - name: JMX_PORT
      value: "1099"
  volumeMounts:
    - name: jmx-config
      mountPath: /etc/jmx-exporter
```

- Config and best practices:
  - Provide a tuned `config.yml` that maps JMX beans to Prometheus metrics (drop noisy beans).
  - Use pod-local networking (no external exposure) and restrict RBAC/service discovery so only Prometheus scrapes the exporter.
  - If attaching as a Java agent is possible, it's lower-overhead but requires changing JVM startup args (not always allowed for immutable images).

- Troubleshooting:
  - If exporter returns empty metrics, verify the JMX host/port and any required authentication.
  - Check for JVM process permissions and remote JMX settings (RMI can be tricky with hostnames).
  - Use a temporary `kubectl exec` into the pod to test connectivity to the JMX port.

2. Cloud SQL / RDS Proxy

- Problem: Managed DBs often require complex authentication (cloud IAM, ephemeral tokens, TLS) and connection management (timeouts, failover). Embedding that logic into every app is error-prone.

- Adapter approach: run an auth/connection proxy as a sidecar that handles provider-specific auth and exposes a plain local TCP port the app can use (e.g., `localhost:5432` for Postgres or `localhost:3306` for MySQL).

- Example adapters:
  - Google Cloud: Cloud SQL Auth Proxy (`gcr.io/cloudsql-docker/gce-proxy`).
  - AWS: RDS Proxy (managed) or a small sidecar that handles IAM auth and TLS.

- Minimal container snippet (conceptual):

```yaml
- name: cloud-sql-proxy
  image: gcr.io/cloudsql-docker/gce-proxy:latest
  args: ["-instances=${INSTANCE_CONNECTION_NAME}=tcp:0.0.0.0:5432","-credential_file=/secrets/creds.json"]
  volumeMounts:
    - name: cloudsql-creds
      mountPath: /secrets
```

- Config and best practices:
  - Store provider credentials in a Kubernetes `Secret` and mount them into the proxy container.
  - Use pod-level DNS and `localhost` in application connection strings so the app connects only to the proxy.
  - Limit network exposure: do not expose the proxy port outside the Pod unless necessary.
  - For multi-tenant or high-traffic workloads, prefer managed DB proxies (RDS Proxy, Cloud SQL Proxy with connection pooling) instead of a basic sidecar.

- Troubleshooting:
  - Authentication failures: verify service account/credential permissions and instance connection names.
  - TLS errors: ensure the proxy has updated CA bundles and the DB instance accepts the TLS configuration.
  - Socket/port conflicts: ensure the app and proxy use distinct ports inside the Pod.

3. Log normalization

- Problem: Legacy apps emit free-form, multi-line or non-JSON logs that central logging systems struggle to parse. These logs may contain important fields split across multiple lines.

- Adapter approach: run a log-tail-and-normalize container (Fluent Bit, Fluentd, Filebeat, or a small custom parser) as a sidecar that:
  - tails the application's log file (shared volume),
  - parses multi-line events, extracts structured fields, and
  - emits a single-line JSON object to stdout or forwards to a logging endpoint.

- Typical pipeline:
  - Legacy app writes to `/var/log/legacy.log` (shared `emptyDir` or hostPath).
  - Adapter tails `/var/log/legacy.log`, uses regex/multi-line parser to combine related lines, enriches with metadata (pod, container), and outputs JSON.
  - Kubernetes node logging agent (or central collector) picks up JSON from stdout or receives it from the adapter.

- Example Fluent Bit `tail` input (conceptual):

```ini
[INPUT]
    Name              tail
    Path              /var/log/legacy.log
    Parser            multiline_custom
    Tag               legacy.app

[PARSER]
    Name              multiline_custom
    Format            regex
    Regex             ^\[?(?<level>ERROR|WARN|INFO)\]?\s+(?<date>\d{4}-\d{2}-\d{2})\s+(?<msg>.*)
    Time_Key          date
```

- Best practices:
  - Build robust parsing rules and test them against real logs.
  - Avoid heavy processing in the adapter if logs are extremely high-volume — consider pushing parsing to a central pipeline with more resources.
  - Emit structured JSON with a stable schema (timestamp, level, msg, trace_id, user, etc.) so downstream systems can index and query efficiently.

- Troubleshooting:
  - Missing fields: validate your parser regex against representative log samples.
  - Performance: monitor CPU/memory for the adapter; consider batching or rate-limiting for noisy logs.
  - Multi-line boundaries: ensure your parser correctly identifies the start/end of events (timestamps or known prefixes help).

