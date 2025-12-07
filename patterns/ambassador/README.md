# Kubernetes Ambassador Pattern — "The Smart Proxy"

[![Kubernetes](https://img.shields.io/badge/kubernetes-%23326ce5.svg?style=for-the-badge&logo=kubernetes&logoColor=white)](https://kubernetes.io/)
[![Go](https://img.shields.io/badge/go-%2300ADD8.svg?style=for-the-badge&logo=go&logoColor=white)](https://golang.org/)
[![Nginx](https://img.shields.io/badge/nginx-%23009639.svg?style=for-the-badge&logo=nginx&logoColor=white)](https://nginx.org/)

## 📖 Overview

This repository documents the **Ambassador Pattern** in Kubernetes. In this pattern, a **sidecar container** acts as a proxy for communication between the application container and external services.

The application connects to `localhost`, and the Ambassador handles the complexity of connecting to the actual remote service (authentication, circuit breaking, retries, connection pooling, etc.).

### 🎯 Key Benefits

- **Separation of Concerns**: Application logic remains clean and focused on business requirements
- **Language Agnostic**: Proxy logic works regardless of application programming language
- **Centralized Management**: Update connection logic without redeploying applications
- **Security**: Credentials and certificates managed separately from application code
- **Resilience**: Built-in retry, circuit breaking, and failover capabilities

---

## 1️⃣ Concept: The "Out-of-Process" Proxy

### Why would you put a proxy inside your Pod?

Imagine your application needs to connect to a legacy database that requires complex mutual TLS (mTLS) handshakes, or a flaky external API that needs sophisticated retry logic.

#### ❌ Without Ambassador

You import heavy libraries into your Python/Go code to handle mTLS and retries. If you have 10 microservices in different languages, you implement this logic **10 times** with:
- Inconsistent retry strategies
- Duplicated security logic
- Language-specific TLS libraries
- Maintenance burden across multiple codebases

#### ✅ With Ambassador

Your app simply connects to `localhost:8080`. The Ambassador sidecar:
1. Listens on port `8080`
2. Handles the mTLS/Retries/Authentication
3. Forwards the request to the remote service
4. Returns the response to your app

**Result**: Your application code stays simple, testable, and maintainable.

---

## 2️⃣ Comparison: The "Sidecar Family"

All three patterns use a multi-container Pod (sidecar architecture), but their **intent** is different.

| Feature | **Sidecar** ("Helper") | **Adapter** ("Translator") | **Ambassador** ("Proxy") |
|---------|------------------------|----------------------------|--------------------------|
| **Primary Goal** | Enhance the main app (logging, syncing) | Standardize output interface | Mask complexity of external connections |
| **Data Flow** | Parallel or Independent | Outbound (App → Adapter → World) | Two-way Proxy (App ↔ Ambassador ↔ World) |
| **Analogy** | Motorcycle Sidecar | Power Plug Adapter | A Diplomat / Translator |
| **Key Example** | Fluentd (Log shipping) | Prometheus JMX Exporter | Cloud SQL Proxy, Envoy (Service Mesh) |
| **Network Focus** | Internal Pod operations | Outbound data transformation | Bidirectional external communication |

### 🔍 Visual Comparison

```
Sidecar:        [App] → [Logs] → [Fluentd] → External Log Store
Adapter:        [App] → [Metrics] → [Adapter] → Prometheus
Ambassador:     [App] ↔ [Ambassador] ↔ External Service/DB
```

---

## 3️⃣ Implementation: "Hello World" Resilience Proxy

This project implements a simple **"Resilience Ambassador"** demonstration:

- **Client (Golang)**: A lightweight container that attempts to fetch data. It is hardcoded to call `http://localhost:8080`. It knows nothing about the outside world.
- **Ambassador (Nginx)**: A sidecar listening on port `8080`. It receives the request and proxies it to `httpbin.org`.

### 📂 A. Project Structure

```
patterns/ambassador/
├── app/
│   ├── main.go        # The "Naive" App (Calls localhost)
│   └── Dockerfile     # Multi-stage Go build
├── ambassador-proxy/
│   ├── nginx.conf     # The Proxy Logic (Retries, Circuit Breaking)
│   └── Dockerfile     # Nginx config injection
└── manifests/
    └── ambassador-proxy.yaml # The Multi-Container Pod
```

### 💻 B. The Code

#### 1. The Client (`app/main.go`)

Notice the client logic is **extremely simple**. It assumes the service is local.

```go
func main() {
    // The application thinks it is talking to a local service.
    targetURL := "http://localhost:8080/get"
    
    for {
        resp, err := http.Get(targetURL)
        if err != nil {
            fmt.Printf("Error reaching ambassador: %v\n", err)
        }
        // ... handle response
        time.Sleep(5 * time.Second)
    }
}
```

**Key Points**:
- No external URL configuration needed
- No retry logic in application code
- No TLS certificate handling
- Application remains simple and testable
#### 2. The Proxy Config (`ambassador-proxy/nginx.conf`)

The complexity of where the service lives is **hidden here**.

```nginx
server {
    listen 8080;
    location / {
        # Proxy Logic
        proxy_pass http://httpbin.org;
        proxy_set_header Host httpbin.org;
        
        # Additional resilience features (can be added):
        # proxy_connect_timeout 5s;
        # proxy_read_timeout 10s;
        # proxy_next_upstream error timeout http_500 http_502 http_503;
    }
}
```

**Key Points**:
- All external service details encapsulated in proxy config
- Easy to add timeouts, retries, and circuit breaking
- Can be updated without touching application code
- Supports advanced features like load balancing, rate limiting

#### 3. The Deployment (`manifests/ambassador-proxy.yaml`)

The critical piece is defining **both containers in one Pod spec**. They share the `localhost` network namespace.

```yaml
spec:
  containers:
    - name: client-app
      image: client-app:v1
      resources:
        requests:
          cpu: "50m"
          memory: "50Mi"
        limits:
          cpu: "100m"
          memory: "100Mi"
    - name: ambassador-proxy
      image: ambassador-proxy:v1
      ports:
        - containerPort: 8080
      resources:
        requests:
          cpu: "50m"
          memory: "50Mi"
        limits:
          cpu: "100m"
          memory: "100Mi"
```

**Key Points**:
- Both containers share the same network namespace
- `localhost` communication is zero-hop (no network latency)
- Each container can have independent resource limits
- Both containers are scheduled on the same node together

### 🚀 C. How to Run

#### Step 1: Build Images

```bash
# Build the client application
docker build -t client-app:v1 ./app

# Build the ambassador proxy
docker build -t ambassador-proxy:v1 ./ambassador-proxy
```

**Note**: If you see a deprecation warning about the legacy builder, use BuildKit:

```bash
# Option 1: Enable BuildKit for these builds
DOCKER_BUILDKIT=1 docker build -t client-app:v1 ./app
DOCKER_BUILDKIT=1 docker build -t ambassador-proxy:v1 ./ambassador-proxy

# Option 2: Use buildx (recommended for multi-platform)
docker buildx build --platform linux/amd64 -t client-app:v1 --load ./app
docker buildx build --platform linux/amd64 -t ambassador-proxy:v1 --load ./ambassador-proxy
```

#### Step 2: Deploy to Kubernetes

```bash
kubectl apply -f manifests/ambassador-proxy.yaml
```

#### Step 3: Verify Deployment

```bash
# Check pod status
kubectl get pods -l app=ambassador

# View client application logs
kubectl logs -l app=ambassador -c client-app

# View ambassador proxy logs
kubectl logs -l app=ambassador -c ambassador-proxy

# Expected output in client-app logs:
# "Success! Status: 200 OK"
```

#### Step 4: Test and Debug

```bash
# Describe the pod to see both containers
kubectl describe pod -l app=ambassador

# Execute into the client container
kubectl exec -it <pod-name> -c client-app -- sh

# Execute into the ambassador container
kubectl exec -it <pod-name> -c ambassador-proxy -- sh

# Port-forward to test locally (optional)
kubectl port-forward <pod-name> 8080:8080
curl http://localhost:8080/get
```
---

## 4️⃣ Deep Dive: Enterprise Use Cases

### A. 🗄️ Database Connectivity (Cloud SQL Auth Proxy)

This is the **most common use case** in GCP/AWS environments.

#### The Problem

Connecting to a production DB over the internet requires:
- SSL/TLS certificates
- Whitelisted IPs
- IAM authentication
- Connection pooling
- Credential rotation

Rotating these credentials inside application code is a **security nightmare** and creates:
- Hardcoded credentials in source code
- Manual certificate updates requiring redeployments
- Inconsistent security policies across services
- Exposure of sensitive credentials in logs/errors

#### The Ambassador Solution

The **"Cloud SQL Auth Proxy"** container runs alongside your app.

#### The Flow

```
┌─────────────────────────────────────────┐
│              Pod                        │
│  ┌──────────┐         ┌──────────────┐ │
│  │   App    │ ─────→  │ Cloud SQL    │ │
│  │          │localhost│ Auth Proxy   │ │
│  └──────────┘  :5432  └──────┬───────┘ │
│                               │         │
└───────────────────────────────┼─────────┘
                                │
                        Encrypted Tunnel
                        (IAM Authenticated)
                                │
                                ↓
                        ┌───────────────┐
                        │  Cloud SQL    │
                        │   Database    │
                        └───────────────┘
```

**Detailed Steps**:
1. App connects to `localhost:5432` (PostgreSQL port)
2. Ambassador intercepts the connection
3. Ambassador uses the **Pod's Service Account identity** to authenticate with GCP IAM
4. Ambassador establishes a secure, encrypted tunnel to the DB instance
5. Data flows through the tunnel transparently

#### Benefits

- ✅ **Developers**: Treat the DB like a local Docker container
- ✅ **Security Teams**: Handle IAM policies centrally
- ✅ **Operations**: Rotate credentials without application changes
- ✅ **Compliance**: Enforce encryption and audit trails

#### Example Configuration

```yaml
containers:
- name: app
  image: myapp:v1
  env:
  - name: DB_HOST
    value: "localhost"
  - name: DB_PORT
    value: "5432"
    
- name: cloud-sql-proxy
  image: gcr.io/cloudsql-docker/gce-proxy:latest
  command:
  - "/cloud_sql_proxy"
  - "-instances=project:region:instance=tcp:5432"
  securityContext:
    runAsNonRoot: true
```

---

### B. 🕸️ Service Mesh Data Plane (Envoy)

In a service mesh (Istio/Linkerd), an **Envoy proxy** is injected as an ambassador into every Pod.

#### The Problem

Microservices need:
- **Observability**: Distributed tracing, metrics, logging
- **Security**: Mutual TLS (mTLS) between all services
- **Traffic Management**: Canary deployments, A/B testing, circuit breaking
- **Resilience**: Retries, timeouts, rate limiting

Implementing these in every microservice means:
- Code duplication across teams
- Language-specific implementations
- Inconsistent behavior
- Difficult to update policies

#### The Ambassador Solution

Envoy intercepts **all inbound and outbound traffic** at the network layer.

#### The Flow

```
Service A Pod              Service B Pod
┌──────────────┐          ┌──────────────┐
│   App A      │          │   App B      │
│      ↓       │          │      ↑       │
│  Envoy A     │  ─────→  │  Envoy B     │
│  (Ambassador)│  mTLS +  │  (Ambassador)│
│              │  Trace   │              │
└──────────────┘          └──────────────┘
```

**Detailed Steps**:
1. App A tries to call App B at `http://app-b:8080`
2. **Ambassador A** intercepts the request
3. Ambassador A adds a **trace ID** (distributed tracing)
4. Ambassador A encrypts the request with **mTLS**
5. Ambassador A sends it to **Ambassador B**
6. Ambassador B decrypts it and validates the certificate
7. Ambassador B passes it to App B on `localhost`
8. Response flows back through the same path

#### Benefits

- ✅ **Zero Code Changes**: Get end-to-end encryption and tracing without modifying applications
- ✅ **Polyglot Support**: Works with any language (Go, Python, Java, Node.js)
- ✅ **Centralized Policies**: Update retry/timeout policies cluster-wide
- ✅ **Observability**: Automatic metrics and tracing integration

#### Key Features

| Feature | How It Works |
|---------|-------------|
| **mTLS** | Automatic certificate generation and rotation |
| **Tracing** | Injects trace headers (X-B3, Jaeger) |
| **Retries** | Configurable retry budgets and backoff |
| **Circuit Breaking** | Automatic failure detection and isolation |
| **Load Balancing** | Client-side LB with health checks |
| **Rate Limiting** | Token bucket algorithm per service |

---

### C. 🏦 Legacy mTLS Encapsulation

#### The Problem

A legacy banking backend requires:
- A **specific client certificate** format (PKCS#12)
- Legacy cipher suites (TLS 1.0/1.1)
- Custom authentication headers

Your new Node.js microservice:
- Uses modern TLS libraries that don't support legacy formats
- Struggles with certificate conversion
- Requires complex OpenSSL integration

#### The Ambassador Solution

Run an **Nginx ambassador** with the certificates mounted.

#### The Flow

```
┌─────────────────────────────────┐
│          Pod                    │
│  ┌──────────┐    ┌───────────┐ │      ┌──────────────┐
│  │ Node.js  │───→│   Nginx   │─┼─────→│ Legacy Bank  │
│  │   App    │HTTP│Ambassador │ │HTTPS │   Backend    │
│  └──────────┘    └───────────┘ │ mTLS │              │
│     Simple        Handles       │      └──────────────┘
│     HTTP Call     Certificates  │
└─────────────────────────────────┘
```

**Detailed Steps**:
1. Node.js talks **HTTP** to `localhost:8080`
2. Nginx adds the **Client Certificate** (mounted as a Secret)
3. Nginx talks **HTTPS with mTLS** to the bank
4. Nginx handles certificate validation and cipher negotiation

#### Configuration Example

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: bank-client-cert
type: kubernetes.io/tls
data:
  tls.crt: <base64-encoded-cert>
  tls.key: <base64-encoded-key>
---
containers:
- name: app
  image: nodejs-app:v1
  
- name: nginx-ambassador
  image: nginx:alpine
  volumeMounts:
  - name: client-certs
    mountPath: /etc/nginx/certs
    readOnly: true
  - name: nginx-config
    mountPath: /etc/nginx/nginx.conf
    subPath: nginx.conf
    
volumes:
- name: client-certs
  secret:
    secretName: bank-client-cert
- name: nginx-config
  configMap:
    name: nginx-mtls-config
```

**Nginx Config** (`nginx.conf`):
```nginx
server {
    listen 8080;
    
    location / {
        proxy_pass https://legacy-bank.example.com;
        
        # mTLS Configuration
        proxy_ssl_certificate /etc/nginx/certs/tls.crt;
        proxy_ssl_certificate_key /etc/nginx/certs/tls.key;
        proxy_ssl_protocols TLSv1 TLSv1.1 TLSv1.2;
        proxy_ssl_ciphers HIGH:!aNULL:!MD5;
        
        # Additional headers
        proxy_set_header X-Client-ID "microservice-001";
    }
}
```

#### Benefits

- ✅ **Encapsulation**: Application code stays simple
- ✅ **Security**: Certificates managed via Kubernetes Secrets
- ✅ **Flexibility**: Easy to rotate certificates or update TLS config
- ✅ **Testing**: Mock the ambassador in development environments

---

## 5️⃣ Lead DevOps Engineer: Interview Questions & Gotchas

If you are designing systems at a high level, these are the **edge cases you must know**.

---

### Q1: "How do you handle the Startup Race Condition?" 🏁

#### The Issue

Containers in a Pod start **in parallel**. The Application might start up and try to call `localhost:8080` **before** the Ambassador (Nginx/Envoy) is ready to listen. The app crashes with "connection refused".

**Timeline**:
```
T=0s:  Pod created, both containers start simultaneously
T=1s:  App container starts → Tries localhost:8080 → FAILS (Ambassador not ready)
T=2s:  Ambassador starts listening on port 8080
T=3s:  App container crashes (exit code 1)
```

#### The Fix(es)

##### Option 1: **Retry Logic in Application** (Recommended)

The application must have **robust retry logic** on startup with exponential backoff.

```go
func waitForAmbassador() {
    maxRetries := 10
    for i := 0; i < maxRetries; i++ {
        conn, err := net.DialTimeout("tcp", "localhost:8080", 1*time.Second)
        if err == nil {
            conn.Close()
            return // Ambassador is ready
        }
        backoff := time.Duration(math.Pow(2, float64(i))) * time.Second
        log.Printf("Ambassador not ready, retry %d/%d in %v", i+1, maxRetries, backoff)
        time.Sleep(backoff)
    }
    log.Fatal("Ambassador failed to start")
}
```

##### Option 2: **Kubernetes 1.29+ Native Sidecar Containers**

Use the new native `restartPolicy: Always` in `initContainers` to ensure the proxy is ready before the main app starts.

```yaml
initContainers:
- name: ambassador-proxy
  image: nginx:alpine
  restartPolicy: Always  # New in K8s 1.29+
  ports:
  - containerPort: 8080
  
containers:
- name: app
  image: myapp:v1
  # This starts only after ambassador-proxy is running
```

**Benefits**:
- ✅ Native Kubernetes feature (no hacky scripts)
- ✅ Guarantees ordering
- ✅ Ambassador restarts independently if it crashes

##### Option 3: **postStart Hook** (Hacky, Not Recommended)

A lifecycle hook where the app container waits for `localhost:8080` to be available before running the main binary.

```yaml
containers:
- name: app
  image: myapp:v1
  lifecycle:
    postStart:
      exec:
        command:
        - /bin/sh
        - -c
        - |
          until nc -z localhost 8080; do
            echo "Waiting for ambassador..."
            sleep 1
          done
```

**Drawbacks**:
- Requires `nc` (netcat) in the image
- Delays all container startups
- Not idiomatic Kubernetes

---

### Q2: "What is the 'Sidecar Termination' problem in Jobs?" 🛑

#### The Issue

You have a Kubernetes **Job** (batch process) that uses an Ambassador to talk to a DB:
1. The main process finishes its work and exits with code `0`
2. However, the Ambassador is **still running**
3. Because one container is still running, the Pod never enters `Completed` state
4. The Job hangs forever (or until `activeDeadlineSeconds`)

**Pod Status**:
```
NAME                     READY   STATUS    RESTARTS   AGE
batch-job-xyz            1/2     Running   0          5m
```
(Notice: 1/2 → One container finished, one still running)

#### The Fix(es)

##### Option 1: **Shared Process Namespace** (Simple)

Allow containers to see each other's processes, so the app can kill the sidecar on exit.

```yaml
spec:
  shareProcessNamespace: true
  containers:
  - name: batch-app
    image: batch-processor:v1
    command:
    - /bin/sh
    - -c
    - |
      # Do batch work
      python process_data.py
      
      # Kill the ambassador when done
      kill $(pgrep nginx)
  
  - name: ambassador
    image: nginx:alpine
```

**How it works**:
- All containers share PID namespace
- App can use `pgrep`/`pkill` to find and terminate the ambassador
- Pod enters `Completed` state after both containers exit

##### Option 2: **Ambassador `/quit` Endpoint**

Configure the Ambassador to listen on a special endpoint that gracefully shuts it down.

**Nginx Config**:
```nginx
server {
    listen 8080;
    
    location / {
        proxy_pass http://database:5432;
    }
    
    location /quit {
        return 200 "Shutting down\n";
        access_log off;
        
        # Trigger graceful shutdown
        content_by_lua_block {
            os.execute("nginx -s quit")
        }
    }
}
```

**Application**:
```python
# Do batch work
process_data()

# Signal ambassador to shut down
requests.get("http://localhost:8080/quit")
sys.exit(0)
```

##### Option 3: **PreStop Hook** (Ambassador Auto-Terminates)

Ambassador watches the main container and exits when it terminates.

```yaml
containers:
- name: batch-app
  image: batch-processor:v1

- name: ambassador
  image: nginx:alpine
  lifecycle:
    preStop:
      exec:
        command:
        - /bin/sh
        - -c
        - |
          # Wait for main container to exit
          while kill -0 1 2>/dev/null; do
            sleep 1
          done
          nginx -s quit
```

---

### Q3: "What is the resource overhead?" 📊

#### The Reality

##### CPU Overhead

**Encryption/decryption** (mTLS) is **CPU intensive**. At high scale (10k req/s), the Ambassador can consume **more CPU than the app itself**.

**Benchmarks** (typical):
| Scenario | App CPU | Ambassador CPU | Total |
|----------|---------|----------------|-------|
| No Encryption | 200m | 0m | 200m |
| TLS Termination | 200m | 150m | 350m |
| mTLS (Both Ways) | 200m | 300m | 500m |
| With Retries + Tracing | 200m | 400m | 600m |

**Key Insight**: **Budget 1.5-3x CPU** for high-security workloads with mTLS.

##### Memory Overhead

| Component | Memory |
|-----------|--------|
| Nginx (Idle) | ~10MB |
| Nginx (Active, 1k conn) | ~50MB |
| Envoy (Idle) | ~50MB |
| Envoy (Active, 1k conn) | ~150MB |

**Key Insight**: Envoy is **feature-rich but memory-heavy**. Use Nginx for simple proxying.

##### Network Latency

Adding an Ambassador adds:
- **2 network hops** (local loopback)
- **Serialization time** (parsing headers, etc.)

**Typical Overhead**:
- Loopback latency: **~0.1-0.5ms**
- Proxy processing: **~1-2ms**
- Total: **~2ms** (negligible for most workloads)

**Critical for**:
- High Frequency Trading (HFT): Every microsecond matters
- Real-time gaming: Sub-10ms response times required

**Not critical for**:
- Web applications: 50-200ms response times
- Batch processing: Seconds to minutes per task

#### Recommendation

- **Profile your workload** with `kubectl top` and load testing
- **Set resource limits** to prevent Ambassador from starving the app
- **Monitor P99 latency** to catch regressions

```yaml
resources:
  requests:
    cpu: "100m"      # Baseline for light traffic
    memory: "50Mi"
  limits:
    cpu: "500m"      # Burst capacity for spikes
    memory: "200Mi"
```

---

### Q4: "Why not just use a library?" 📚

#### The Trade-off: "Smart Client" vs. "Smart Proxy"

| Aspect | **Library** (Smart Client) | **Ambassador** (Smart Proxy) |
|--------|---------------------------|------------------------------|
| **Latency** | Lower (direct connection) | +1-2ms (loopback overhead) |
| **Implementation** | Per language (Go, Python, Java, Node.js) | Language agnostic |
| **Updates** | Requires recompiling + redeploying all apps | Config change only |
| **Code Complexity** | App code includes retry/circuit breaking | App code stays simple |
| **Testability** | Mock library in unit tests | Mock external service |
| **Observability** | Instrumentation in every service | Centralized at proxy layer |
| **Security** | Credentials distributed in apps | Credentials in proxy only |

#### When to Use a Library

✅ **Ultra-low latency requirements** (< 5ms)
✅ **Homogeneous stack** (all services in one language)
✅ **Simple use case** (basic HTTP calls, no auth)
✅ **Small team** (easy to coordinate updates)

**Example**: gRPC client library with built-in load balancing.

#### When to Use Ambassador

✅ **Polyglot environment** (Go, Python, Java, Node.js)
✅ **Complex auth** (mTLS, OAuth, API keys)
✅ **Operational flexibility** (update policies without redeploys)
✅ **Security requirements** (centralized credential management)
✅ **Legacy integration** (connect to systems you can't modify)

**Example**: Service mesh with Envoy for all inter-service communication.

#### Hybrid Approach

Many organizations use **both**:
- **Library** for internal service-to-service calls (low latency)
- **Ambassador** for external APIs, databases, and legacy systems (security + flexibility)

---

## 🎓 Additional Resources

### Official Documentation

- [Kubernetes Multi-Container Pods](https://kubernetes.io/docs/concepts/workloads/pods/#how-pods-manage-multiple-containers)
- [Sidecar Containers (K8s 1.29+)](https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/)
- [Envoy Proxy](https://www.envoyproxy.io/)
- [Istio Service Mesh](https://istio.io/)

### Related Patterns

- **Sidecar Pattern**: [../sidecar/README.md](../sidecar/README.md)
- **Adapter Pattern**: [../adapter/README.md](../adapter/README.md)
- **Init Container Pattern**: [../init-container/README.md](../init-container/README.md)

### Tools & Projects

- [Cloud SQL Auth Proxy](https://cloud.google.com/sql/docs/mysql/sql-proxy)
- [AWS RDS Proxy](https://aws.amazon.com/rds/proxy/)
- [Nginx](https://nginx.org/)
- [HAProxy](https://www.haproxy.org/)
- [Envoy](https://www.envoyproxy.io/)

---

## 🤝 Contributing

Found an issue or want to add a use case? Contributions are welcome!

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/new-use-case`
3. Commit your changes: `git commit -am 'Add new enterprise use case'`
4. Push to the branch: `git push origin feature/new-use-case`
5. Submit a Pull Request

---

## 📝 License

This project is part of the `k8s-ref-patterns` repository. See the root LICENSE file for details.

---

## 📧 Contact

Questions? Open an issue or reach out to the maintainers.

**Happy Proxying! 🚀**