# Enterprise Gateway API: The Next Gen Ingress

Here is the conceptual breakdown followed by a hands-on lab that fits your local Kind setup.

## 1. Conceptual Leap: Ingress vs. Gateway API

### The Old Way (Ingress)
*   **Monolithic**: One object (`Ingress`) defined everything: the load balancer, the SSL certs, and the routing logic (`/api` -> service).
*   **Vendor Lock-in**: Want rewrite rules? You need `nginx.ingress.kubernetes.io/rewrite-target`. Switch to AWS ALB? That annotation is useless; you need `alb.ingress.kubernetes.io/...`.
*   **Role Confusion**: The Developer and the Platform Engineer fight over the same YAML file.

### The New Way (Gateway API)
It splits the responsibility into three layers (Personas):
1.  **GatewayClass** (Infrastructure Provider): "I offer an AWS ALB" or "I offer an Nginx Proxy."
2.  **Gateway** (Platform Admin): "I want to provision one Load Balancer on port 80/443."
3.  **HTTPRoute** (Developer): "I want to attach my `/users` route to that Gateway."

---

## 2. How does it work with Cloud Load Balancers?
You asked: *"In a traditional cloud, you'd have a load balancer outside... how would that work?"*

It works exactly the same way, but purely declarative.

### Ingress Controller Model
You create an `Ingress`. The controller sees it and (usually) configures an Nginx pod inside your cluster. You then manually create a Service of type `LoadBalancer` to expose that Nginx pod to the cloud.

### Gateway API Model
1.  You create a `Gateway` resource.
2.  The Cloud Controller (e.g., AWS Gateway API Controller or GKE Gateway Controller) sees this.
3.  It **automatically provisions** the physical Cloud Load Balancer (ALB/NLB/GLB) for you.
4.  It gives you the DNS name in `Gateway.Status.Addresses`.

**Verdict**: The `Gateway` object is your request for a Cloud Load Balancer.

---

## 3. Lab: Envoy Gateway (The Future Standard)
We will use **Envoy Gateway**. It is a CNCF project that implements the Gateway API using Envoy Proxy. It is lightweight and perfect for your Kind setup.

### Prerequisites
Ensure your Kind cluster is running:
```bash
kubectl cluster-info
```

### Step 1: Install the Gateway API CRDs
Kubernetes does not have these resources by default yet. We must install the "schema" (CRDs).

```bash
# Install the Standard Gateway API CRDs (v1.2.0)
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.0/standard-install.yaml
```

### Step 2: Install Envoy Gateway (The Controller)
This is the "Brain" that will watch our resources and configure the Envoy proxies.

```bash
# Install using Helm (or direct YAML for simplicity)
helm install eg oci://docker.io/envoyproxy/gateway-helm --version v1.2.0 -n envoy-gateway-system --create-namespace
```

Wait for it to be ready:
```bash
kubectl wait --timeout=5m -n envoy-gateway-system deployment/envoy-gateway --for=condition=Available
```

### Step 3: Create the Gateway (The "Load Balancer")
We are telling Envoy Gateway: "Give me an entry point on port 80."

**File**: `my-gateway.yaml`
```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: eg
spec:
  controllerName: gateway.envoyproxy.io/gatewayclass-controller
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: my-gateway
  namespace: default
spec:
  gatewayClassName: eg
  listeners:
    - name: http
      protocol: HTTP
      port: 80
```

**Apply it:**
```bash
kubectl apply -f my-gateway.yaml
```

**Check the status:**
```bash
kubectl get gateway my-gateway
```
> **Note**: In a real cloud, `ADDRESS` would show an AWS ALB DNS name. In Kind, it might show a private IP or remain pending until we configure Layer 2 (MetalLB), but for this lab, Envoy Gateway usually assigns an internal Service IP we can port-forward to.

### Step 4: Deploy a Test App (The Backend)
Let's deploy a simple "echo" service.

```bash
kubectl create deployment echo --image=gcr.io/google-samples/hello-app:1.0
kubectl expose deployment echo --port=8080 --target-port=8080
```

### Step 5: Create the Route (The Logic)
This is where the **Developer Persona** comes in. We attach a route to the Gateway.

**File**: `echo-route.yaml`
```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: echo-route
  namespace: default
spec:
  parentRefs:
    - name: my-gateway # <--- I am attaching to the Platform team's Gateway
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /echo
      backendRefs:
        - name: echo
          port: 8080
```

**Apply it:**
```bash
kubectl apply -f echo-route.yaml
```

### Step 6: Test It
Since we are in Kind without a cloud Load Balancer, we need to port-forward the Envoy Proxy (the Data Plane) that the Gateway created for us.

**Find the Envoy Proxy Pod**: Envoy Gateway creates a managed Deployment for the proxy in the `envoy-gateway-system` namespace (usually named something like `envoy-default-my-gateway-...`).

```bash
kubectl get pods -n envoy-gateway-system
# Look for a pod starting with 'envoy-default-my-gateway'
```

**Port Forward**:
```bash
# Replace the pod name with the one you found above
kubectl port-forward -n envoy-gateway-system <ENVOY_POD_NAME> 8080:8080
```
*(Note: Envoy Gateway usually listens on port 8080 or 80 inside the pod depending on config. Try `8080:80` if `8080:8080` fails, or check the Service created in that namespace).*

**Curl it** (Open a new terminal):
```bash
curl http://localhost:8080/echo
```
**Success Condition**: You should see: `Hello, world! Version: 1.0.0...`

---

## 4. Lead Engineer "Gotchas" for Gateway API

### CRD Versioning Hell
Gateway API is stable (`v1`), but many implementations still rely on `v1beta1` or `v1alpha2` features. Always check `kubectl get crd gateways.gateway.networking.k8s.io` to see what version is installed before upgrading your controller.

### Cross-Namespace Routing (Shared Gateway)
*   **Scenario**: Platform team owns `Gateway` in `infra-ns`. Dev team owns `HTTPRoute` in `app-ns`.
*   **The Trap**: By default, a Gateway might not trust routes from other namespaces.
*   **The Fix**: The Gateway must explicitly allow it using `allowedRoutes`:

```yaml
listeners:
- allowedRoutes:
    namespaces:
      from: All # or Selector
```

### Gateway vs. GatewayClass
Don't confuse them.
*   **GatewayClass** is the template (like `StorageClass`).
*   **Gateway** is the instance (like `PersistentVolumeClaim`).

---

## 5. The Summary: The "Role-Based" Architecture
The key to understanding Gateway API is realizing that **Configuration (YAML)** is separated from **Execution (Traffic)**.

### 1. The Configuration Flow (The "Brain")
*This happens asynchronously before any user makes a request.*

1.  The **Platform Engineer** (You) applied the `GatewayClass` and `Gateway` YAMLs to the API Server.
2.  The **Envoy Controller** (the brain running in `envoy-gateway-system`) was watching the API Server. It saw the Gateway request for "Port 80".
3.  **The Controller Acted**: It spun up the Envoy Proxy Pod (the data plane). Crucially, because it knows it's running in a container, it decided to listen on unprivileged port 10080 internally, intending to map outer port 80 to it.
4.  The **Developer** (You) applied the `Deployment`, `Service`, and `HTTPRoute` YAMLs.
5.  **The Controller Acted Again**: It saw the `HTTPRoute`. It translated that YAML into raw Envoy configuration (xDS) and pushed it into the running Proxy Pod, telling it: *"If you see path `/echo`, send it to the Service named `echo`."*

### 2. The Traffic Flow (The "Muscle")
*This is what happened when you ran `curl`.*

1.  **The Tunnel**: Your `kubectl port-forward` created a direct tunnel from your laptop to port 10080 on the Proxy Pod.
2.  **The Request**: Your `curl localhost:8080/echo` traveled through the tunnel and hit the Proxy Pod.
3.  **The Routing Decision**: The Proxy Pod looked at its internal config map (which it got from the controller in step 1). It matched `/echo`.
4.  **The Destination**: The Proxy matched the route to the Kubernetes Service named `echo`. It used Kube DNS to find the IP of the actual backend pod and forwarded the request.

### The Diagram
Here is how it all connects. Notice the distinction between Configuration Flow (dashed lines, happening in the background) and Actual Traffic Flow (thick solid lines, happening during your curl).

```mermaid
graph TD
    %% Definining Styles
    classDef config fill:#f9f,stroke:#333,stroke-width:2px,stroke-dasharray: 5 5;
    classDef dataplane fill:#dbf,stroke:#333,stroke-width:4px;
    classDef k8sObj fill:#eee,stroke:#333,stroke-width:1px;

    subgraph "Local Laptop"
        User[User Terminal<br>curl localhost:8080/echo]
        PF[kubectl port-forward<br>8080:10080]
    end

    subgraph "Kubernetes Cluster (Kind)"
        API[K8s API Server<br>(etcd)]

        subgraph "Control Plane (The Brain)"
            Controller[Envoy Gateway Controller<br>Pod]:::config
        end

        subgraph "Data Plane (The Muscle)"
            Proxy[Envoy Proxy Pod<br>Listening on :10080]:::dataplane
        end

        subgraph "Developer Resources (default ns)"
            GW[Gateway YAML<br>Requests Port 80]:::k8sObj
            Route[HTTPRoute YAML<br>Path: /echo -> Svc: echo]:::k8sObj
            Svc[Service: echo<br>ClusterIP]:::k8sObj
            BackendPod[Backend Pod<br>echo deployment]
        end
    end

    %% Configuration Flow (Dashed)
    GW -.-> API
    Route -.-> API
    Svc -.-> API
    Controller -.- |Watches for config changes| API
    Controller -.- |1. Creates Pod<br>2. Pushes Routing Config| Proxy

    %% Traffic Flow (Solid Thick)
    User ==> |Actual Request| PF
    PF ==> |Tunnel via API Server| Proxy
    Proxy ==> |Matches /echo route<br>Forwards to Service IP| Svc
    Svc ==> |Load balances to| BackendPod
```