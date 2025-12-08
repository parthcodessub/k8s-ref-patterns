# Service Mesh Pattern — "The Network of Ambassadors"

This repository documents the Service Mesh Pattern. While not a "coding" pattern you implement inside your app, it is an architectural pattern that fundamentally changes how microservices communicate.

---

## 📖 1. Concept: What is a Service Mesh?

A Service Mesh is a dedicated infrastructure layer for handling service-to-service communication.

- In the **Ambassador Pattern**, you manually added a proxy sidecar (like Nginx) to handle traffic for a specific pod.
- In a **Service Mesh** (like Istio), a control plane automatically injects an Ambassador proxy (Envoy) into **every** Pod in your cluster.

These proxies intercept all network traffic (inbound and outbound). Your application code sends a request to "billing-service", but the proxy actually grabs it, encrypts it, decides where to send it, and records how long it took.

### The Two Planes

1.  **Data Plane (The Proxies)**: The sidecars (e.g., Envoy) running next to your apps. They do the actual packet moving.
2.  **Control Plane (The Brain)**: The central server (e.g., Istiod) that programs the proxies. You tell the Control Plane: "Send 10% of traffic to V2", and it updates the config of 500 sidecars instantly.

---

## ❓ 2. The Core Question: Why isn't Kubernetes DNS (.svc) enough?

Kubernetes Service Discovery (`kube-dns` / `CoreDNS`) acts like a **Phonebook**.
Service Mesh acts like a **Secure Armored Courier**.

| Feature | Kubernetes Native (ClusterIP + DNS) | Service Mesh (Istio/Linkerd) |
| :--- | :--- | :--- |
| **Discovery** | "Where is the Billing Service?" → Returns IP 10.2.2.1. | "Where is the Billing Service?" → Intercepts traffic to handle delivery. |
| **Reliability** | None. If the call fails, your App must retry (code change). | **Automatic**. The mesh retries 3 times with exponential backoff before your App even knows it failed. |
| **Security** | None. Traffic is plaintext (HTTP). Any Pod can talk to any Pod. | **mTLS**. Traffic is automatically encrypted. Identity is verified (SPIFFE). |
| **Observability** | None. You must log requests manually. | **Golden Signals**. You get Success Rate, Latency, and Throughput metrics for free for every link. |
| **Traffic Control** | Round Robin (Random). 50/50 split only. | **Percentage & Header based**. "Send 1% of iPhone users to V2". |

**The Verdict**: Kubernetes DNS tells you *where* someone is. Service Mesh ensures the message gets there *safely, reliably, and observably*.

---

## 🏢 3. Top 3 Enterprise Use Cases

### A. Zero Trust Security (mTLS)
**Scenario**: You are a Fintech company. Auditors require that all internal traffic between microservices be encrypted.

-   **Without Mesh**: You must implement HTTPS and manage certificates inside every Java, Go, and Node.js app. Certificate rotation is a nightmare.
-   **With Mesh**: You enable "Strict mTLS" in Istio. The sidecars automatically handshake, encrypt traffic, and rotate certificates every hour. The application knows nothing about it (it still speaks HTTP to localhost).

### B. Canary Deployments (Traffic Shifting)
**Scenario**: You are deploying a risky update to the "Checkout" service.

-   **Without Mesh**: You deploy the new Pod. K8s Service sends traffic round-robin. If you have 2 replicas (1 Old, 1 New), 50% of users get the new version instantly. Too risky.
-   **With Mesh**: You deploy the new version but tell the Mesh: "Send 0% traffic to V2". Then, you update a VirtualService rule: "Send 1%". Then "5%". If errors spike, you revert instantly.

### C. Chaos Engineering & Resilience
**Scenario**: You want to know what happens if the "User Profile" service is slow.

-   **Without Mesh**: You have to modify the code to sleep/delay.
-   **With Mesh**: You inject a "Fault" configuration. "Delay 50% of requests by 2 seconds." You verify if the frontend handles this gracefully (e.g., shows a loading spinner) or crashes.

---

## 🛠 4. Kubernetes Options: The Landscape

1.  **Istio**: The Heavyweight Champion. Most feature-rich, massive community, but complex to manage. Best for large enterprises.
2.  **Linkerd**: The Lightweight Challenger. Written in Rust, incredibly fast, simple ("it just works"). Best for teams wanting mTLS/Observability without complexity.
3.  **Consul Connect**: Best for HashiCorp shops or hybrid VM/K8s environments.
4.  **Cilium**: The eBPF future. Sidecarless architecture (uses the kernel). Extremely fast.

---

## 🧪 Practical Lab: Service Mesh "Hello World" (Istio)

This lab demonstrates **Automatic Retries** using Istio.

**The Scenario**:
1.  **Echo Service** (Backend): Fails 30% of the time (returns 503).
2.  **Caller Service** (Frontend): Calls Echo.
3.  **Goal**: Fix the 503 errors without modifying the source code.

### Prerequisites: Install Istio (KinD / Minikube)

You need the Istio Control Plane running.

#### Option A: The "Official" Way (Recommended)
This downloads the binary and samples.
```bash
curl -L https://istio.io/downloadIstio | sh -
cd istio-*
export PATH=$PWD/bin:$PATH
istioctl install --set profile=demo -y
kubectl apply -f samples/addons
```

#### Option B: The "Homebrew" Way (MacOS)
If you installed via `brew install istioctl`, you lack the samples. Install them manually:
```bash
istioctl install --set profile=demo -y
# Install Addons (Kiali, Jaeger, Prometheus, Grafana)
kubectl apply -f https://raw.githubusercontent.com/istio/istio/master/samples/addons/kiali.yaml
kubectl apply -f https://raw.githubusercontent.com/istio/istio/master/samples/addons/prometheus.yaml
kubectl apply -f https://raw.githubusercontent.com/istio/istio/master/samples/addons/jaeger.yaml
kubectl apply -f https://raw.githubusercontent.com/istio/istio/master/samples/addons/grafana.yaml
```

**Verify Installation**:
```bash
kubectl rollout status deployment/kiali -n istio-system
```

---

### Step 1: Enable Injection & Deploy

Tell Istio to automatically inject sidecars in the `default` namespace.

```bash
kubectl label namespace default istio-injection=enabled
```

Build and load the images (if using KinD):
```bash
docker build -t mesh-app:v1 ./patterns/service-mesh/app
kind load docker-image mesh-app:v1
```

Deploy the application:
```bash
kubectl apply -f patterns/service-mesh/manifests/apps.yaml
```

**Verify**:
```bash
kubectl get pods
# You should see "2/2" in the READY column (App + Sidecar)
# NAME                      READY   STATUS    RESTARTS
# caller-7b8c...           2/2     Running   0
# echo-v1-6c9...           2/2     Running   0
```

---

### Step 2: Reproduce the Failure

The `echo` service is hardcoded to fail 30% of requests. Let's see it.

1.  **Port-forward the caller**:
    ```bash
    kubectl port-forward deploy/caller 8080:8080
    ```

2.  **Generate traffic**:
    ```bash
    while true; do curl http://localhost:8080; echo; sleep 0.5; done
    ```

**Output**:
```text
Backend replied: 200 OK
Backend replied: 503 Service Unavailable  <-- ERROR!
Backend replied: 200 OK
```

---

### Step 3: Fix with Mesh (VirtualService)

We will apply an Istio `VirtualService` that configures the sidecar to **automatically retry** failed requests.

```bash
kubectl apply -f patterns/service-mesh/manifests/mesh-config.yaml
```

**Watch the traffic loop again**. The 503 errors should disappear immediately.

**Why?**
The backend `echo` pod is *still* failing. However, the Envoy sidecar catches the 503, retries the request (up to 3 times), and eventually gets a 200. The `caller` app never sees the failure.

---

### Step 4: Visualize (Kiali)

Istio comes with Kiali, a powerful observability dashboard.

```bash
istioctl dashboard kiali
```

1.  Navigate to **Graph**.
2.  Select `default` namespace.
3.  Enable **Traffic Animation** in Display Settings.
4.  Click the edge between `caller` and `echo`. You will see the requests and the retries happening in real-time.

---

### ⚠️ Critical Concept: Header Propagation

For distributed tracing (Jaeger) to work, your application **MUST** forward trace headers.

Look at `app/main.go`:
```go
var traceHeaders = []string{"x-request-id", "x-b3-traceid", ...}
// ...
for _, h := range traceHeaders {
    req.Header.Set(h, r.Header.Get(h))
}
```

If you omit this, the Mesh can see traffic entering and leaving, but it cannot "stitch" the span together into a single trace. This is the **only code change** required for full Mesh observance.

1. Concept: What is a Service Mesh?

A Service Mesh is a dedicated infrastructure layer for handling service-to-service communication.

In the Ambassador Pattern, you manually added a proxy sidecar (like Nginx) to handle traffic for a specific pod.
In a Service Mesh (like Istio), a control plane automatically injects an Ambassador proxy (Envoy) into every Pod in your cluster.

These proxies intercept all network traffic (inbound and outbound). Your application code sends a request to "billing-service", but the proxy actually grabs it, encrypts it, decides where to send it, and records how long it took.

The Two Planes

Data Plane (The Proxies): The sidecars (e.g., Envoy) running next to your apps. They do the actual packet moving.

Control Plane (The Brain): The central server (e.g., Istiod) that programs the proxies. You tell the Control Plane: "Send 10% of traffic to V2", and it updates the config of 500 sidecars instantly.

2. The Core Question: Why isn't Kubernetes DNS (.svc) enough?

Kubernetes Service Discovery (kube-dns / CoreDNS) acts like a Phonebook.
Service Mesh acts like a Secure Armored Courier.

Feature

Kubernetes Native (ClusterIP + DNS)

Service Mesh (Istio/Linkerd)

Discovery

"Where is the Billing Service?" -> Returns IP 10.2.2.1.

"Where is the Billing Service?" -> Intercepts traffic to handle delivery.

Reliability

None. If the call fails, your App must retry (code change).

Automatic. The mesh retries 3 times with exponential backoff before your App even knows it failed.

Security

None. Traffic is plaintext (HTTP). Any Pod can talk to any Pod.

mTLS. Traffic is automatically encrypted. Identity is verified (SPIFFE).

Observability

None. You must log requests manually.

Golden Signals. You get Success Rate, Latency, and Throughput metrics for free for every link.

Traffic Control

Round Robin (Random). 50/50 split only.

Percentage & Header based. "Send 1% of iPhone users to V2".

The Verdict: Kubernetes DNS tells you where someone is. Service Mesh ensures the message gets there safely, reliably, and observably.

3. Top 3 Enterprise Use Cases

A. Zero Trust Security (mTLS)

Scenario: You are a Fintech company. Auditors require that all internal traffic between microservices be encrypted.

Without Mesh: You must implement HTTPS and manage certificates inside every Java, Go, and Node.js app. Certificate rotation is a nightmare.

With Mesh: You enable "Strict mTLS" in Istio. The sidecars automatically handshake, encrypt traffic, and rotate certificates every hour. The application knows nothing about it (it still speaks HTTP to localhost).

B. Canary Deployments (Traffic Shifting)

Scenario: You are deploying a risky update to the "Checkout" service.

Without Mesh: You deploy the new Pod. K8s Service sends traffic round-robin. If you have 2 replicas (1 Old, 1 New), 50% of users get the new version instantly. Too risky.

With Mesh: You deploy the new version but tell the Mesh: "Send 0% traffic to V2". Then, you update a VirtualService rule: "Send 1%". Then "5%". If errors spike, you revert instantly.

C. Chaos Engineering & Resilience

Scenario: You want to know what happens if the "User Profile" service is slow.

Without Mesh: You have to modify the code to sleep/delay.

With Mesh: You inject a "Fault" configuration. "Delay 50% of requests by 2 seconds." You verify if the frontend handles this gracefully (e.g., shows a loading spinner) or crashes.

4. Deep Dive: Q&A on Architecture

Q: Is this just for internal (Cluster) traffic?

A: Primarily, Yes. This is known as East-West traffic.

Internal: Service A calling Service B is the sweet spot.

External: For calling external APIs (e.g., Stripe), you usually rely on standard DNS. However, meshes offer "Egress Gateways" if you want to force all external traffic through a single monitored exit point (common in high-security banking).

Q: Why not just use a Cloud Load Balancer (ALB)?

A: Cost and Complexity.
You could put an ALB between every microservice, but:

Cost: You pay per ALB. 100 Microservices = 100 ALBs.

Latency: Traffic has to leave the cluster, go to the cloud router, and come back ("Hairpinning").

Mesh: Mesh gives you the intelligence of an ALB (Retries, Headers, Splits) but distributed inside the cluster with zero hardware cost. It is a "software load balancer" running on localhost.

Q: Is it faster?

A: No, technically it is slightly slower.
Adding a sidecar adds two tiny network hops (localhost -> Sidecar -> Network -> Sidecar -> App).

The Trade-off: You accept ~2ms of latency in exchange for massive operational powers (Metric, Logs, Encryption) that would otherwise require thousands of lines of code to implement manually.

5. Kubernetes Options: The Landscape

1. Istio

The Heavyweight Champion. backed by Google/IBM.

Pros: Most feature-rich. Massive community. Can run VM workloads.

Cons: Complex to manage. High learning curve. "Envoy" sidecars can consume significant RAM.

Best for: Large enterprises needing granular traffic control and strict security.

2. Linkerd

The Lightweight Challenger. CNCF Graduated project.

Pros: Incredibly fast (written in Rust). Very simple to install ("it just works"). Low resource usage.

Cons: Fewer "fancy" features than Istio (e.g., less complex external authorization support).

Best for: Teams who want mTLS and Observability without the complexity of Istio.

3. Consul Connect (HashiCorp)

The Hybrid.

Best for: Organizations already using HashiCorp Vault/Consul, or those spanning legacy VMs and Kubernetes.

4. Cilium (eBPF - Sidecarless)

The Future?

Instead of injecting a container into every Pod, it uses eBPF to handle logic at the Linux Kernel level.

Pros: No sidecar overhead. Extremely fast.

6. Resources & The "Injection" Magic

A. Resource Overhead (The "Tax")

You asked about the cost for 50 microservices. Let's assume you run 2 replicas of each (100 Pods total).

Memory (The main cost):

Envoy (Istio): Uses ~50MB to 100MB per sidecar to store the map of the cluster.

Calculation: 100 Pods * 80MB = 8GB RAM overhead cluster-wide.

Optimization: You can use Sidecar resources to limit what each proxy sees (e.g., "Billing only needs to know about Payment"), reducing RAM to ~30MB.

CPU:

Envoy uses ~0.1 vCPU per 1,000 requests/second.

If your cluster is idle, CPU usage is near zero. It scales linearly with traffic.

Linkerd (Rust):

Much lighter. Typically ~10-20MB RAM per sidecar.

Calculation: 100 Pods * 15MB = 1.5GB RAM overhead.

B. How "Automatic Injection" Works

Misconception: The mesh modifies running pods.
Reality: The mesh modifies pods before they are created.

The Mechanism is a Mutating Admission Controller.

You: Run kubectl apply -f deployment.yaml.

API Server: Receives the YAML. It sees the namespace has a label istio-injection=enabled.

Webhook: The API Server pauses and sends the YAML to istiod (The Control Plane).

Mutation: istiod edits the YAML in-flight, adding the envoy-proxy container spec to it.

Persistence: The API Server saves the modified YAML to etcd.

Scheduler: Schedules the Pod (which now has 2 containers).

Q: What if I roll out a bad config?

Existing Pods: They are safe. They keep running with the old (working) config.

New Pods: If the Control Plane is down or sends bad config, new Pods will fail to start (CreateError).

Traffic: If you push a bad routing rule (e.g., "Send 100% traffic to non-existent-service"), existing proxies will update instantly and start returning 503 errors.

7. Gotchas: When NOT to use a Mesh

"Do I need a Service Mesh?" is a common interview question.

Latency Sensitive Apps: The mesh adds 2 hops (App -> Sidecar -> Sidecar -> Target). This adds ~2-5ms of latency. For High Frequency Trading, this is unacceptable.

Small Clusters: If you have 5 microservices, a Service Mesh is overkill. The complexity of managing Istio exceeds the value it provides. Use standard K8s NetworkPolicies instead.

Cost: Sidecars consume CPU/RAM. In a cluster with 1,000 Pods, running 1,000 Envoy proxies requires significant compute resources.

8. Day 2 Operations: How Updates Work

The most common confusion is distinguishing between Code updates and Config updates.

The "Smart TV" Analogy

Think of the Sidecar Proxy as a Smart TV installed in your house (Pod).

Injection (Installation): This is the heavy lifting. You have to buy the TV and mount it to the wall. This happens only once when the Pod is created.

Configuration (Streaming): You want to change the channel or watch a new movie. You just press a button on the remote (Control Plane). The TV updates instantly. You do not need to buy a new TV to change the channel.

Real-World Scenarios

Scenario A: Changing Traffic Rules (The "Channel Change")

Action: You want to shift traffic from V1 to V2.

Command: kubectl apply -f virtual-service.yaml

Mechanism:

The Control Plane detects the change.

It pushes the new routing table to all 1,000 running Envoy proxies via a long-lived gRPC connection (xDS Protocol).

Result: Traffic shifts instantly. No Pod restarts required.

Scenario B: Upgrading the Mesh Version (The "New TV")

Action: You want to upgrade Istio from v1.18 to v1.19 (to get security patches for the Envoy binary itself).

Mechanism:

You upgrade the Control Plane.

The running sidecars are now "old". They still work, but they are running the old binary.

Result: You MUST restart your Pods (kubectl rollout restart deployment) to pick up the new Envoy binary image.

9. Deep Dive: Traffic Routing Logic

This is the configuration you actually write. It relies on Kubernetes Services for discovery, but Mesh Objects for logic.

1. The Setup (The "Dumb" Service)

You still need a standard Kubernetes Service. The Mesh uses this name for discovery, but it ignores the simple ClusterIP routing.

kind: Service
metadata:
  name: payment-service
spec:
  selector:
    app: payment
  ports:
    - port: 80


2. The Logic (The "Smart" Overlay)

This VirtualService wraps the physical service with Layer 7 intelligence. It "hijacks" the request based on the Host Header payment-service.

apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: payment-route
spec:
  hosts:
    - payment-service  # Intercept traffic destined for this Hostname
  http:
  - match:
    - headers:
        user-agent:
          regex: ".*iPhone.*" # Logic: "If User is on iPhone..."
    route:
    - destination:
        host: payment-service
        subset: v2            # "...send to V2"
  - route:
    - destination:
        host: payment-service
        subset: v1            # "...everyone else to V1"


3. The Packet Flow (Under the Hood)

App (Client): Resolves DNS for payment-service. Kube-DNS returns ClusterIP 10.0.0.50.

App: Sends TCP packet to 10.0.0.50:80.

Envoy (Sidecar): Intercepts the packet via iptables.

Envoy: Ignores the 10.0.0.50 IP. Instead, it reads the HTTP Host Header (payment-service).

Envoy: Checks its internal Route Table (loaded from the VirtualService above).

Envoy: "Ah, this is an iPhone user. I need subset: v2."

Envoy: Looks up the Pod IPs for V2 (e.g., 10.244.1.2).

Envoy: Sends the packet directly to the destination Pod IP (bypassing the Service ClusterIP completely).

Key Takeaway: The ClusterIP is just a dummy address to get the packet out of the app. The Sidecar does the actual routing logic based on Names and Headers.


Practical Lab: Service Mesh "Hello World" (Istio)

This lab demonstrates Automatic Retries using Istio.

The Scenario:

Echo Service: A backend that fails 30% of the time (returns 503).

Caller Service: A frontend that calls Echo.

Goal: Fix the 503 errors without modifying the Go code.

Prerequisite: Install Istio on KinD

Since you are running KinD, we need to install the Mesh Control Plane.

Download Istio:

curl -L [https://istio.io/downloadIstio](https://istio.io/downloadIstio) | sh -
cd istio-*
export PATH=$PWD/bin:$PATH


Install to Cluster (Demo Profile):
The demo profile enables high levels of tracing and logging, perfect for learning.

istioctl install --set profile=demo -y


(Note: Ensure your Docker has at least 4GB RAM allocated)

Enable Sidecar Injection:
Tell Istio to watch the default namespace.

kubectl label namespace default istio-injection=enabled


Step 1: Build & Deploy Apps

Build the Image:
We use the same image for both services.

docker build -t mesh-app:v1 ./patterns/service-mesh/app

# Load into KinD (Important!)
kind load docker-image mesh-app:v1


Deploy:

kubectl apply -f patterns/service-mesh/manifests/apps.yaml


Verify Injection:
Check that your pods now have 2/2 containers (1 App + 1 Envoy Sidecar).

kubectl get pods
# NAME                      READY   STATUS    RESTARTS
# caller-7b8c...           2/2     Running   0
# echo-v1-6c9...           2/2     Running   0


Step 2: Test the "Broken" App

The echo service is programmed to fail 30% of the time. Let's see it fail.

Port Forward the Caller:

kubectl port-forward deploy/caller 8080:8080


Generate Traffic:
Run this loop in a terminal. You will see occasional 503 Service Unavailable.

while true; do curl http://localhost:8080; echo; sleep 0.5; done


Output:

Backend replied: 200 OK
Backend replied: 200 OK
Backend replied: 503 Service Unavailable  <-- ERROR!
Backend replied: 200 OK


Step 3: Fix it with Mesh (VirtualService)

We will apply a rule that tells the Envoy sidecar: "If echo fails, try again up to 3 times."

Apply the Rule:

kubectl apply -f patterns/service-mesh/manifests/mesh-config.yaml


Watch the Traffic Again:
Go back to your curl loop. The 503 errors should disappear.

Why?
The Backend is still failing, but the Sidecar is catching the 503, retrying instantly, and eventually getting a 200. The Caller (your curl loop) never sees the error.

Step 4: Visualize (Kiali)

The demo profile includes Kiali, a dashboard to visualize the mesh.

Launch Kiali:

istioctl dashboard kiali


Navigate to Graph. Select default namespace.

Enable "Traffic Animation" in Display Settings.

You will see caller -> echo. If you click the edge, you can see the requests and the retries happening in real-time.

Critical Code Concept: Header Propagation

Look at app/main.go. You will see this block:

var traceHeaders = []string{"x-request-id", "x-b3-traceid", ...}
// ...
for _, h := range traceHeaders {
    req.Header.Set(h, r.Header.Get(h))
}


If you delete this code, Tracing breaks.
The Mesh can trace Client -> Sidecar and Sidecar -> Server, but it loses the context inside the Go binary unless you pass these headers from the incoming Request to the outgoing Request. This is the one code change required to be "Mesh Compatible" for observability.



---

## 🔍 Appendix: Under the Hood

### 1. Verification Output (Reference)
If you run `kubectl get all -n istio-system`, you will see something like this.
<details>
<summary>Click to see full output</summary>

```text
NAME                                        READY   STATUS    RESTARTS   AGE
pod/istio-egressgateway-6df4f877f8-r76cs    1/1     Running   0          27s
pod/istio-ingressgateway-58485f6998-dwzl6   1/1     Running   0          27s
pod/istiod-55df86bcc4-jl4n5                 1/1     Running   0          35s

NAME                                  TYPE           CLUSTER-IP      EXTERNAL-IP   PORT(S)                                                                      AGE
service/istio-egressgateway           ClusterIP      10.96.237.238   <none>        80/TCP,443/TCP                                                               27s
service/istio-ingressgateway          LoadBalancer   10.96.18.228    <pending>     15021:31452/TCP,80:31904/TCP,443:30894/TCP,31400:32306/TCP,15443:32435/TCP   27s
service/istiod                        ClusterIP      10.96.14.27     <none>        15010/TCP,15012/TCP,443/TCP,15014/TCP                                        35s

NAME                                   READY   UP-TO-DATE   AVAILABLE   AGE
deployment.apps/istio-egressgateway    1/1     1            1           27s
deployment.apps/istio-ingressgateway   1/1     1            1           27s
deployment.apps/istiod                 1/1     1            1           35s
```
</details>

### 2. How Sidecar Injection Actually Works

1.  **The Label**: `kubectl label namespace default istio-injection=enabled` adds a metadata label to the namespace.
2.  **The Trigger**: Istio installs a **Mutating Admission Webhook**. This tells Kubernetes: *"Before you create ANY pod in a labeled namespace, talk to me first."*
3.  **The Action**:
    -   If the label is **missing**: Kubernetes creates the Pod normally (1 container).
    -   If the label is **present**: Kubernetes pauses creation, sends the YAML to `istiod`, and `istiod` patches the YAML to include the `envoy-proxy` container.

### 3. The "Missing" Resources

If you run `kubectl get all`, you won't see `VirtualServices` or `Gateways`. This is because `get all` only shows default Kubernetes resources (Pods, Services, Deployments).

To see Service Mesh resources, you must query them by their Custom Resource Definition (CRD) names:

```bash
# List all VirtualServices
kubectl get virtualservices
# OR use the short alias
kubectl get vs
```


