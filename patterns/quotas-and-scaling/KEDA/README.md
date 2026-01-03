# KEDA (Kubernetes Event-driven Autoscaling)

## 1. Executive Summary

**The Problem**: Standard Kubernetes HPA scales primarily on CPU & Memory. This is **Reactive**. By the time CPU spikes, your queue might already have 10,000 pending jobs, meaning you are already behind.

**The Solution**: KEDA scales based on **External Events** (Queue Depth, Kafka Lag, SQL Query Results). It is **Proactive**.

**The Superpower**: **Scale to Zero**.
If there are 0 items in the queue, KEDA shuts down your deployment completely (0 pods). Standard HPA cannot do this (minimum is usually 1).

---

## 2. Architecture Deep Dive

KEDA is not just a "Metrics Adapter"; it is a **Systems Controller** that orchestrates two distinct lifecycles: **Activation (0 ↔ 1)** and **Scaling (1 ↔ N)**.

### 2.1. The Components
KEDA relies on three key components to function:

1.  **KEDA Operator (Agent)**:
    *   **Role**: The "Brain". It manages the CRDs (`ScaledObject`, `ScaledJob`, `TriggerAuthentication`).
    *   **Function**: It continuously runs a polling loop against the external event source (e.g., polling SQS every 30s).
    *   **Responsibility**: It is the **ONLY** component responsible for scaling from **0 to 1** and **1 to 0**.
2.  **KEDA Metrics Server**:
    *   **Role**: The "Bridge". It implements the Kubernetes External Metrics API.
    *   **Function**: When the HPA asks "What is the current queue length?", the KEDA Metrics Server translates that request into a call to the external provider (SQS/Kafka).
    *   **Responsibility**: It feeds data to the HPA for scaling from **1 to N**.
3.  **Admisson Webhooks**:
    *   Validates CRD configurations to prevent misconfiguration (e.g., preventing invalid Polling intervals).

### 2.2. The Scaling Workflow (0 → 1 → N)
KEDA splits scaling into two phases. This is the most critical concept to understand for debugging.

```mermaid
graph TD
    subgraph "Phase 1: Activation (KEDA Operator)"
        Event[External Event\n(e.g., 5 messages in Queue)]
        Operator[KEDA Operator]
        Deploy[Deployment]
        
        Event -->|Poll| Operator
        Operator -->|0 Messages?| Deploy(Scale to 0)
        Operator -->|1+ Messages?| Deploy(Scale to 1)
    end

    subgraph "Phase 2: Horizontal Scaling (HPA + Metrics Server)"
        HMS[KEDA Metrics Server]
        K8sHPA[Kubernetes HPA]
        Prom/SQS[External Source]
        
        K8sHPA -->|Request Metric| HMS
        HMS -->|Query API| Prom/SQS
        K8sHPA -->|Calc Replicas| Deploy(Scale to N)
    end
    
    Operator -.->|Creates| K8sHPA
```

1.  **Phase 1: Activation (The Cold Start)**
    *   The **KEDA Operator** polls the external source.
    *   If Queue Depth = 0: It manually sets the Deployment replicas to 0. (HPA is deactivated).
    *   If Queue Depth > 0: It manually scales Deployment to 1. **Then it hands control over to the HPA.**
2.  **Phase 2: HPA Scaling (The Load)**
    *   Once the deployment is at 1 replica, the **HPA** takes over.
    *   The HPA polls the **KEDA Metrics Server** to get the current metric value.
    *   HPA calculates replicas: `ceil(currentMetricValue / targetMetricValue)`.

### 2.3. ScaledObject vs. ScaledJob (Architecture Decision)
Not all workloads should use a `Deployment`.

| Feature | ScaledObject (Deployment) | ScaledJob (Kubernetes Job) |
| :--- | :--- | :--- |
| **Workload Type** | **Continuous Listeners**. Web servers, Consumers polling a queue. | **One-off Tasks**. Video transcoding, large data batch processing. |
| **Logic** | Scales the *number of running pods*. | Creates *one K8s Job* per event. |
| **Scale Down** | **Dangerous**. HPA might kill a pod while it is processing a message. requires `preStop` hooks for graceful shutdown. | **Safe**. The Pod runs until completion (Success/Failure). It is never killed prematurely by autoscaling. |
| **Max Scale** | Limited by `maxReplicaCount`. | Limited by `parallelism` and `maxPollingInterval`. |
| **Best For** | High-throughput, short-lived messages (RabbitMQ, HTTP). | Long-running (>5 min) expensive tasks (Video rendering). |

### 2.4. Authentication Architecture (`TriggerAuthentication`)
KEDA decouples credentials from the scaling logic.
*   **TriggerAuthentication**: Namespace-scoped. Used by ScaledObjects in the same namespace.
*   **ClusterTriggerAuthentication**: Cluster-scoped. Used by ScaledObjects across all namespaces (e.g., a shared Kafka cluster for the whole company).
*   **Pod Identity / IRSA**: The preferred architectural pattern. KEDA assumes the IAM role of the Service Account to avoid handling secrets altogether.

---

## 3. Production Patterns (The "Big 3")

### Pattern A: The Queue Consumer (The Classic)
**Use Case**: Image processing, video transcoding, email sending.
*   **Trigger**: Redis List, AWS SQS, Kafka, RabbitMQ.
*   **Behavior**:
    *   0 messages → 0 Pods.
    *   100 messages → 1 Pod.
    *   10,000 messages → 50 Pods.
*   **Value**: You stop paying for idle workers when no jobs exist.

### Pattern B: The Cron Scaler (The Predictable Spike)
**Use Case**: A trading app that opens at 9:30 AM EST.
*   **Trigger**: Cron (Linux Crontab syntax).
*   **Behavior**: "At 9:00 AM, force scale to 20 replicas. At 4:00 PM, scale back to 2."
*   **Value**: Prevents "Cold Start" lag. Pods are pre-warmed before users arrive.

### Pattern C: The SQL Scaler (The Legacy Bridge)
**Use Case**: A legacy monolith that uses a Postgres table as a "Job Queue" (`SELECT * FROM jobs WHERE status = 'pending'`).
*   **Trigger**: SQL Query row count (`SELECT COUNT(*)...`).
*   **Behavior**: Count > 50 → Scale Up.
*   **Value**: Modernizes legacy apps without rewriting their database logic.

---

## 4. Architectural Deep Dive: Design Decisions

### 4.1. The Database Scaler Debate
**Intuition**: "Is it wise to allow KEDA to connect to the DB directly? Is this legacy?"
**Verdict**: **Yes, it is largely an anti-pattern.**

1.  **The Danger**:
    *   **Performance Hit**: `SELECT COUNT(*)` on a large table is expensive. Polling your Production Primary DB every 5 seconds steals IOPS from user transactions.
    *   **Thundering Herd**: If KEDA scales your app to 100 pods, those 100 pods opens 100 DB connections immediately, potentially DDoSing your database.
    *   **Security**: You must share DB credentials with the KEDA Operator.
2.  **The Modern Approach (Prometheus Wrapper)**:
    *   Instead of KEDA polling the DB, your Application (or a sidecar) exposes a metric: `job_queue_depth{status="pending"}`.
    *   KEDA scales based on the **Prometheus Scaler**.
    *   *Benefit*: Decoupling and Caching. KEDA talks to Prometheus, not your sensitive DB.
3.  **Why use SQL Scaler?** Pragmatism. It helps migrate 15-year-old monoliths to Kubernetes without a 2-year rewrite to Kafka. **Use only for migration paths.**

### 4.2. The S3 / File Upload Model
**Intuition**: "Does KEDA watch (list) files in a bucket? Or use EventBridge?"
**Verdict**: **Do NOT use the polling model. Use the Queue-Based pattern.**

1.  **The Naive Way (Polling)**:
    *   Running `aws s3 list-objects` every 10 seconds.
    *   *Why it fails*: **Cost**. AWS charges for every LIST request. Scanning huge buckets is also slow (latency).
2.  **The Production Way (S3 → SQS → KEDA)**:
    *   **The Workflow**: User uploads file → S3 Event Notification → SQS Queue (`{"event": "ObjectCreated"}`).
    *   **KEDA**: Watches the **SQS Queue Depth**, not the bucket.
    *   *Benefit*: Cost-efficient, faster, and reliable (retries via SQS visibility timeouts).

---

## 5. Enterprise Best Practices (Governance & Stability)

In an interview, do not just say "I set up KEDA." Say "**I set up KEDA safely.**"

| Category | Best Practice | The "Why" (The Risk) |
| :--- | :--- | :--- |

| **Security** | **TriggerAuthentication** | **Anti-Pattern**: Hardcoding AWS Keys/DB Passwords in YAML. <br>**Fix**: `TriggerAuthentication` CRD references Kubernetes Secrets or AWS IAM Roles (IRSA), keeping `ScaledObject` clean. |
| **Safety** | **maxReplicaCount** | **The Bankruptcy Stopper**. A bug in Producer pushes 1M jobs → KEDA scales to 5,000 pods → Cluster Crash / Huge Bill. <br>**Fix**: Always cap replicas based on DB connection limits. |
| **Stability** | **cooldownPeriod** | **Prevent Flapping**. Queue goes 10→0→10→0. KEDA thrashing causes CPU spikes. <br>**Fix**: Set `cooldownPeriod: 300` (5 mins). Wait before scaling down to zero. |
| **Reliability** | **Fallback Strategy** | **What if Datadog is down?** If KEDA is blind, it might scale to 0 during peak. <br>**Fix**: `fallback` section: "If metric is unreachable, force replica count to 10 until restored." |

---

## 6. What Can Trigger KEDA? (The Catalog)

Enterprises use KEDA to bridge the gap between "Business Signals" and "Infrastructure."

*   **Messaging (The Worker Pattern)**: AWS SQS, Kafka (Lag), RabbitMQ, Azure Service Bus.
*   **Databases (The Legacy Bridge)**: PostgreSQL, MySQL, MongoDB, Redis.
*   **Observability (The Traffic Burst)**: Prometheus (Most Common), Datadog, AWS CloudWatch, New Relic.
*   **Storage (The Batch Processor)**: AWS S3 (via Queue), Azure Blob Storage.

---

## 7. Appendix: The Metrics Server Gap (Why KEDA?)
**"Why do I need KEDA? Can't I just use the Kubernetes Metrics Server?"**

No. The Metrics Server is designed for **"Resource Stability,"** not "Application Scaling."

### 7.1. What is the Metrics Server?
It is a lightweight, cluster-wide aggregator of resource usage data (CPU & Memory).
*   **Purpose**: Enabling HPA (CPU/RAM) and `kubectl top`.
*   **Philosophy**: "Narrow and Deep." It is NOT a monitoring solution.

### 7.2. What it Collects (and What it Ignores)
It intentionally filters out most data to remain lightweight.

| Metric Type | Metrics Server (Standard HPA) | Real Monitoring (Prometheus / Datadog) |
| :--- | :--- | :--- |
| **CPU** | ✅ Yes (mili-cores) | ✅ Yes |
| **Memory** | ✅ Yes (MiB) | ✅ Yes |
| **Network** | ❌ No | ✅ Yes (Packet/sec, Bandwidth) |
| **Disk I/O** | ❌ No | ✅ Yes (IOPS, Latency) |
| **Application** | ❌ **No** (Queue Depth, HTTP RPS) | ✅ **Yes (This is why you need KEDA)** |
| **History** | ❌ No (Real-time only) | ✅ Yes (Historical Graphs) |

### 7.3. "Out of the Box" Availability
Distribution matters.
*   **GKE / AKS**: ✅ Pre-installed & Managed.
*   **K3s**: ✅ Included (as an Addon).
*   **EKS / Standard Kubeadm**: ❌ **Not installed**. You must install it manually (Bitnami Helm Chart or Official Manifests).

> **Key Takeaway**: KEDA bridges the gap. It reads the "Real Monitoring" metrics (bottom right) and translates them so the HPA *thinks* they are simple Resource metrics.