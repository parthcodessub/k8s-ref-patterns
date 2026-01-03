# Kubernetes Quotas, Limits, and Scaling Patterns

**Current Status**: `Draft`  
**Target Audience**: Senior/Lead SREs, Platform Engineers  
**Prerequisites**: Basic understanding of Pods, Deployments, and YAML.

---

## 1. The Core Concepts: Resource Model

In Kubernetes, resource management is the foundation of cluster stability and efficiency. It operates on a specific contract between the Developer (The Pod) and the Platform (The Node).

### 1.1 Requests (The "Contract")
*   **What it is**: The minimum amount of resources a container is guaranteed.
*   **Mechanism**: The **Kubernetes Scheduler** uses requests to find a Node with enough available capacity.
*   **SRE Implication**: **Scheduling Guarantees**. If you don't set requests, your Pod is a "BestEffort" second-class citizen. It can be potentially placed anywhere but has no guarantee of resources, leading to noisy neighbor issues.

### 1.2 Limits (The "Ceiling")
*   **What it is**: The maximum amount of resources a container is allowed to consume.
*   **Mechanism**: Enforced by the **Linux Kernel (cgroups)**.
*   **SRE Implication**: **Neighbor Protection**. Limits prevent a memory leak or CPU spike in one application from starving other applications on the same node.

---

## 2. Governance Layers

We implement resource management at three distinct layers of governance.

### Level A: The Manifest (Developer Responsibility)
The most basic layer. Developers explicitly define what their specific workload needs.

```yaml
resources:
  requests:
    memory: "64Mi"  # Guaranteed minimum
    cpu: "250m"     # 1/4th of a core (Contract)
  limits:
    memory: "128Mi" # Hard stop (OOMKill if exceeded)
    cpu: "500m"     # Throttling ceiling (CFS Scheduler)
```

### Level B: The LimitRange (Policy Enforcement)
*   **Analogy**: "The Suitcase Rule" — *No single suitcase on this flight can weigh more than 50 lbs.*
*   **Scope**: **Single Pod/Container**.
*   **Problem**: Developers are forgetful or lazy. They deploy Pods without resources.
*   **Solution**: The `LimitRange` automatically injects **Default Requests/Limits** if they are missing, and enforces **Min/Max bounds** (e.g., no Pod can be larger than 2GB).

### Level C: The ResourceQuota (Budget Control)
*   **Analogy**: "The Cargo Hold Rule" — *The total weight of all luggage on this flight cannot exceed 5,000 lbs.*
*   **Scope**: **Entire Namespace**.
*   **Problem**: A single team sets massive requests (e.g., 100 CPUs) and hogs the entire cluster, verifying their "Suitcase" size but ignoring the total capacity.
*   **Solution**: The `ResourceQuota` puts a hard cap on the **aggregate consumption** of the namespace.

---

## 3. Dynamic Scaling Patterns

Static limits are rarely enough. Real-world traffic fluctuates.

### 3.1 Horizontal Pod Autoscaler (HPA)
*   **Use Case**: Stateless applications (APIs, Web Servers).
*   **Mechanism**: Scales the **count** of Replicas based on CPU/Memory utilization or Custom Metrics (e.g., maintain 50% CPU target).
*   **Best Practice**: 
    *   Set `minReplicas` >= 2 for High Availability.
    *   **Always** set Resources Requests/Limits, otherwise HPA cannot calculate utilization %.

### 3.2 Vertical Pod Autoscaler (VPA)
*   **Use Case**: Stateful applications (Databases), Monoliths, or "Right-Sizing" discovery.
*   **Mechanism**: Adjusts the **size** (CPU/RAM requests) of the Pods.
*   **Modes**:
    *   `Auto`: Restarts pods with new sizes (Disruptive, handle with care).
    *   `Off` (Recommendation Mode): The "Goldilocks" approach. It only suggests numbers but touches nothing.

> **CRITICAL WARNING**: Do **NOT** mix HPA and VPA on the same metric (e.g., both CPU). They will fight.
> *   HPA sees high CPU -> Adds Replicas.
> *   VPA sees high CPU -> Increases Request Size.
> *   Result: Massive waste of resources.

---

### 3.3 Deep Dive: The Autoscaling Lifecycle
*What actually happens under the hood when scaling triggers?*

#### A. HPA Lifecycle (Scale Out)
The HPA loop is purely control-plane driven.
1.  **Collection**: The **Metrics Server** scrapes `cgroups` usage from every Kubelet (usually every 15-60s).
2.  **Calculation**: The **HPA Controller** (inside Kube-Controller-Manager) polls the Metrics Server. It detects: `CurrentUsage (80%) > Target (50%)`.
3.  **Decision**: It applies the math: `Desired = Current * (80/50)`.
4.  **Update**: It modifies the **Deployment object** directly (`replicas: 2 -> 4`).
5.  **Reconciliation**: The **Deployment Controller** sees the drift and creates new Pods.
6.  **Scaling**: The **Scheduler** assigns the new Pods to Nodes.

#### B. VPA Lifecycle (Resize/Restart)
VPA is more complex because it cannot change resources of a running Pod. It must kill and recreate them. It uses 3 distinct components:
1.  **The Recommender** (Analysis):
    *   Continuously looks at historical metrics.
    *   Updates the `VPA Object status` with a recommendation (e.g., "This pod needs 500m"). It touches **nothing** else.
2.  **The Updater** (The Assassin):
    *   Runs only if `updateMode: "Auto"`.
    *   Checks if running Pods match the Recommendation.
    *   If a Pod is "significantly" undersized (outside bounds), the Updater **Evicts (Kills)** the Pod.
3.  **The Admission Webhook** (The Mutator):
    *   The Deployment Controller sees the Pod died and creates a **replacement** Pod.
    *   **Crucial Step**: Before the new Pod is persisted to Etcd, the **VPA Admission Webhook** intercepts the creation request.
    *   It **Patches (Mutates)** the Pod Manifest, overriding the original `100m` request with the recommended `500m`.
    *   The new Pod starts up with the correct, larger size.

---

## 4. Lead-Level Best Practices (The "Interview Gold")

### #1: The "CPU Limit" Controversy (Throttling)
*   **The Trap**: Setting a strict CPU limit (e.g., `1000m`) can cause **Tail Latency**. If a container wants `1001m` for just 50ms, the Linux Kernel (CFS) will "throttle" (pause) the thread. This looks like application lag.
*   **The Lead View**: 
    *   **Always** set CPU Requests (vital for scheduling).
    *   **Consider removing** CPU Limits for latency-sensitive, critical applications. Trust the Node's relative weight sharing (requests) to handle contention.

### #2: Memory is Binary (OOMKill)
*   CPU is compressible (it just gets slow). Memory is incompressible (the app crashes/OOMKills).
*   **The Lead View**: For managed runtimes (Java/Go), it is often safer to set **Memory Requests == Memory Limits**. This grants the Pod "Guaranteed" QoS class for memory and prevents the Node from evicting it during pressure.

### #3: VPA "Goldilocks" Mode
*   Don't guess resource numbers.
*   **Strategy**: Run VPA in `updateMode: "Off"` in Production. Let it collect data for 2 weeks. Then, read the `status.recommendation` to hardcode accurate numbers into your manifests.

---

## 5. Lab: Limits, Quotas, and Scaling

In this lab, we will:
1.  Implement strict Governance (LimitRange & ResourceQuota).
2.  Attempt to break these rules.
3.  Use VPA to "Right-Size" a stressing application.

### Step 1: Setup Governance
Create a namespace with strict "Suitcase" (LimitRange) and "Cargo" (ResourceQuota) rules.

```bash
# 1. Create a fresh namespace
kubectl create namespace constraints-lab

# 2. Apply the "Suitcase Rule" (LimitRange)
# - Default Memory: 200Mi (If dev forgets to set it)
# - Max Memory per Pod: 400Mi (No pod can be bigger than this)
cat <<EOF | kubectl apply -n constraints-lab -f -
apiVersion: v1
kind: LimitRange
metadata:
  name: mem-limit-range
spec:
  limits:
  - default:
      memory: 200Mi
    defaultRequest:
      memory: 100Mi
    max:
      memory: 400Mi
    type: Container
EOF

# 3. Apply the "Cargo Hold Rule" (ResourceQuota)
# - Total Memory for the whole namespace: 1Gi (approx 1000Mi)
cat <<EOF | kubectl apply -n constraints-lab -f -
apiVersion: v1
kind: ResourceQuota
metadata:
  name: ns-quota
spec:
  hard:
    requests.memory: 1Gi
    limits.memory: 1Gi
EOF
```

### Step 2: Test The "Suitcase" Rule
Try to deploy a Pod that exceeds the `max` limit (500Mi > 400Mi).

```bash
kubectl run fatty-pod --image=nginx --restart=Never --limits=memory=500Mi -n constraints-lab
```
**Expected Result**: `Error from server (Forbidden): ... maximum memory usage per Container is 400Mi`

### Step 3: Test The "Cargo Hold" Rule
Try to exhaust the Namespace quota.
*   Capacity: 1000Mi
*   Pod Size: 200Mi (Default)
*   Max Pods: 5

```bash
# Deploy 4 replicas (4 * 200Mi = 800Mi). This fits.
kubectl create deployment worker --image=nginx --replicas=4 -n constraints-lab

# Scale to 6 replicas (6 * 200Mi = 1200Mi). This breaks the 1000Mi Quota.
kubectl scale deployment worker --replicas=6 -n constraints-lab

# Check who survived
kubectl get replicaset -n constraints-lab
```
**Observation**: You will see `Desired: 6, Current: 5`. The 6th pod was blocked. Check events with `kubectl describe rs -n constraints-lab` to see the "exceeded quota" error.

### Step 4: Vertical Pod Autoscaling (VPA) Lab
Now we will use the VPA to detect an under-provisioned application.

**1. Inspect the Manifest**  
We have prepared a comprehensive lab file at `manifests/vpa-lab.yaml`. 
It contains:
*   **The Workload**: A "spinach-stress" app that requests very little (100m CPU) but generates high load.
*   **The Policy**: A VPA configuration in `auto` mode (which allows restarts) with `minAllowed` and `maxAllowed` safety guards.

**2. Deploy and Observe**

```bash
# Apply the lab
kubectl apply -f manifests/vpa-lab.yaml

# Watch the Pods
kubectl get pods -w
```
**Observation**: 
1.  Pods start running locally.
2.  VPA analyzes metrics (takes 1-2 mins).
3.  VPA terminates the pods (`Terminating`) and restarts them (`Pending` -> `Running`).
4.  Inspect the new size:
    ```bash
    kubectl get pod -l app=spinach-stress -o jsonpath="{.items[0].spec.containers[0].resources.requests.cpu}"
    ```
    It should be much higher than `100m` (likely `500m+` depending on your CPU).

---
**Cleanup**
```bash
kubectl delete ns constraints-lab
kubectl delete -f manifests/vpa-lab.yaml
```

---

## 6. The Modern Ecosystem (Industry Standards)

While Native HPA and VPA are the foundation, modern enterprises often require more advanced tools.

### 6.1 KEDA (Kubernetes Event-Driven Autoscaling)
*   **Role**: The "Event-Driven" Scaler.
*   **Problem**: Native HPA scales primarily on CPU/Memory. By the time CPU spikes, your queue might already be backed up by 10,000 messages.
*   **The KEDA Fix**: Scales Pods based on **External Events** (Queue Depth, Kafka Lag, Database Queries).
*   **Scenario**: You have an SQS queue. 0 messages = 0 Pods. 1000 messages = 10 Pods.
*   **Superpower**: **Scale-to-Zero**. Native HPA cannot scale to 0 (minimum is 1). KEDA can shut down your deployment completely when idle and wake it up instantly when an event arrives.

### 6.2 StormForge / Cast AI (The "AI" Optimizers)
*   **Role**: The Cost Cutters.
*   **Problem**: Native VPA is simple ("You used 100MB yesterday, so set limit to 100MB"). It doesn't understand context or application performance/latency.
*   **The AI Fix**: These are commercial tools (SaaS) that use Machine Learning to stress-test your app in non-prod.
*   **Scenario**: "We found that if you give this Java app 10% more CPU, latency drops by 50%, but giving it more RAM does nothing."
*   **Value**: They automate the "Goldilocks" tuning at a massive scale, often saving 30-50% on cloud bills compared to raw VPA.

### 6.3 Karpenter (Node Autoscaling, but faster)
*   **Role**: The "Just-in-Time" Scheduler.
*   **Problem**: HPA adds a pod. The Cluster Autoscaler sees the node is full and waits 4-5 minutes to spin up a new EC2 instance.
*   **The Karpenter Fix**: It sees the "Pending" pod and **immediately** launches a node of the exact size needed (e.g., a tiny instance for a tiny pod) in seconds, bypassing standard Auto Scaling Groups.

### Comparison: When to use what?

| Tool | Trigger | Best Use Case | Enterprise Status |
| :--- | :--- | :--- | :--- |
| **Native HPA** | CPU / Memory | Web Servers, Stateless APIs | **Baseline**. Used for simple HTTP apps. |
| **Native VPA** | Historical Usage | CronJobs, Dev Environments | **Limited**. Used cautiously due to restarts. |
| **KEDA** | Queue Depth / Events | Workers, Event-Driven Architectures | **Standard**. The default choice for background processors. |
| **StormForge** | ML Analysis | Java/complex Monoliths | **Advanced**. Used by FinOps teams to cut costs. |

### Lead-Level Recommendation
If I were designing a platform today:
1.  **Web Apps (APIs)**: Use HPA based on **Custom Metrics** (Requests Per Second), not just CPU.
2.  **Worker Apps (Background)**: Use **KEDA** based on Queue Lag.
3.  **Sizing**: Use **Goldilocks** (VPA in recommendation mode) to generate reports, but apply changes manually via GitOps to avoid random production restarts.

---

## 7. Cloud Provider Specifics (GKE vs EKS)

Different clouds handle these concepts differently.

### 7.1 Google Kubernetes Engine (GKE): "Batteries Included"
Google tries to abstract the complexity away. Autoscaling feels "native".
*   **Managed VPA**: It is a simple checkbox: "Enable Vertical Pod Autoscaling." Google manages the control plane (Recommender/Updater) for you.
*   **GKE Autopilot**: In this mode, VPA is **enforced**. You cannot turn it off. Google automatically rightsizes your pods and bills you only for what the pods request.
*   **Multidimensional Pod Autoscaling (MPA)**:
    *   *Problem*: Standard K8s forbids using HPA and VPA on CPU at the same time.
    *   *The GKE Fix*: MPA (Beta) allows you to use **HPA for CPU** (add more replicas) while simultaneously using **VPA for Memory** (make existing replicas bigger). This is a powerful pattern for memory-leaking apps that also need high concurrency.

### 7.2 Amazon EKS: "Do It Yourself" (mostly)
AWS takes a "Vanilla Kubernetes" approach.
*   **Standard HPA**: You must install the **Metrics Server** yourself (or via an EKS Add-on).
*   **Standard VPA**: You must install the **VPA Controller** yourself (like in our lab) and manage its updates.
*   **The EKS Superpower (Karpenter)**: While EKS is standard on Pod scaling, it leads on Node scaling with **Karpenter**.

### Summary Comparison

| Feature | GKE (Google) | EKS (AWS) |
| :--- | :--- | :--- |
| **Horizontal Scaling (HPA)** | **Built-in**. Metrics server is pre-installed. | **Add-on**. You must ensure Metrics Server is installed. |
| **Vertical Scaling (VPA)** | **Managed**. Checkbox to enable. | **Self-Managed**. You install/maintain VPA. |
| **Advanced Modes** | **MPA**. Scales CPU horizontally and RAM vertically. | **None**. Use standard community tools. |
| **Node Autoscaling** | GKE Node Auto-provisioning (NAP). | **Karpenter** (Open Source, but AWS-native). |
