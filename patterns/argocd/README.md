# Argo CD & GitOps Patterns

This guide covers the fundamental principles of GitOps, common enterprise deployment patterns using Argo CD, best practices for repository management, and a hands-on tutorial for setting up a practice environment.

## Part 1: Introduction to GitOps

GitOps is not just a tool; it is an operational framework that applies Developer workflows (Git, Pull Requests, Code Reviews) to Infrastructure and Operations. It fundamentally changes who controls the production state and how changes are applied.

### Core Principles
To be truly "GitOps," a system must adhere to these four rules (defined by the OpenGitOps project):

1. **Declarative**: The entire system must be described declaratively (e.g., Kubernetes YAML, Helm Charts). You say *what* you want ("3 replicas"), not *how* to do it ("start 3 servers").
2. **Versioned & Immutable**: The "Desired State" is stored in Git. Git is the Single Source of Truth.
3. **Pulled Automatically**: Software agents (like Argo CD) automatically pull the desired state from Git.
4. **Continuously Reconciled**: The agent continuously observes the Actual State (live cluster) and compares it with the Desired State (Git). If they differ (drift), the agent corrects the Actual State.

### GitOps vs. Traditional CI/CD ("Push" vs. "Pull")

| Feature | Traditional CI/CD ("Push") | GitOps ("Pull") |
| :--- | :--- | :--- |
| **Architecture** | **Push**: CI pipeline runs `kubectl apply`. | **Pull**: In-cluster agent watches Git and applies changes. |
| **Security** | **Risky**: CI server needs "God Mode" credentials. | **Secure**: Cluster pulls from Git. No external writes to API. |
| **Drift Detection** | **None**: Manual `kubectl edit` changes are ignored by CI. | **Active**: Agent detects "OutOfSync" and can revert manual changes. |
| **Rollbacks** | **Complex**: Requires custom pipelines. | **Simple**: `git revert`. Agent syncs to previous commit. |

---

## Part 2: Enterprise Patterns

These are the standard architectural patterns used in production environments.

### 1. The "App of Apps" Pattern (Bootstrapping)
Instead of manually creating applications in the UI, you create one "Parent Application" that points to a Git folder containing YAML files for other applications.

> [!TIP]
> **Use Case**: Bootstrapping a new cluster. Apply one manifest, and Prometheus, Grafana, Istio, and your microservices install automatically. This simplifies Disaster Recovery.

### 2. The Hub-and-Spoke Model
- **Architecture**: One "Management Cluster" (Hub) running Argo CD deploys applications to multiple "Target Clusters" (Spokes: Dev, QA, Prod).
- **Benefit**: Centralized governance and a single pane of glass for all environments.

### 3. Multi-Tenancy with "AppProjects"
Use the `AppProject` CRD to restrict teams and enforce permissions.
*Example*: The "Frontend Team" Project can only deploy to the `frontend` namespace and can only pull from distinct repositories.

---

## Part 3: Repository Strategy

How you structure your code and manifests is just as important as the tooling.

### 1. Separate vs. Combined Repositories?
**The Verdict: SEPARATE REPOSITORIES.**

It is standard enterprise best practice to separate your Application Source Code (Python/Go) from your Kubernetes Manifests (YAML/Helm).

**Why?**
- **The CI Loop Problem**: If kept together, every README tweak runs your CI pipeline. If you change `replicas: 2` to `3` in Kubernetes YAML, you shouldn't trigger a full Docker build. Separation decouples the build lifecycle (CI) from the deployment lifecycle (CD).
- **Access Control**: Developers need write access to App Code, but often only SREs/Platform Admins (or the CI bot) should write to Production Manifests.
- **Audit Logs**: The commit history for the Manifest repo becomes a clean audit log ("Updated image to v1.2"), uncluttered by app code commits ("Fixed typo").

**The Ideal Workflow**:
1. Dev commits to `App-Repo`.
2. CI builds Docker Image `v2`.
3. CI triggers a script to update `Manifest-Repo` (e.g., updates `image: myapp:v2`).
4. Argo CD sees the change in `Manifest-Repo` and syncs.

### 2. Single Repo for Cluster State?
**The Verdict: YES (The "Fleet" or "Management" Repo).**

While application source code lives in many repos, the configuration for the cluster itself is best kept centralized.

The **"App of Apps" Pattern** usually lives in a "Cluster Repo" containing folders like:
- `/monitoring` (Prometheus, Grafana)
- `/security` (Gatekeeper, policies)
- `/tenants/team-a` (References Team A's Manifest Repo)

**Why this wins**:
- **Disaster Recovery**: If the cluster dies, point a new cluster at this repo, and the entire infrastructure rebuilds itself.
- **Searchability**: "Where is that environment variable set?" one `grep` finds it across the whole organization.

---

## Part 4: Advanced Concepts

### Drift vs. Rollbacks (The "Silent Failure" Problem)
A common misunderstanding is the difference between "Self-Heal" and "Rollback".

- **Argo CD Self-Heal**: Protects against **Drift**. If someone manually deletes a Service, Argo CD recreates it. It *does not* automatically rollback a bad deployment. If v2 crashes (CrashLoopBackOff), Argo CD reports it as "Degraded" but stays on v2.
- **Argo Rollouts**: This is the tool for **Automated Rollbacks**. It monitors error rates and automatically aborts the rollout if metrics fail.

### Notifications: How do we know it failed?
Since Argo CD pulls changes, there is no CI console to watch. You must implement notifications.

1. **Slack/PagerDuty Integration**: Configure triggers:
    - `on-health-degraded` (Deployment crashed)
    - `on-sync-failed` (YAML invalid)
    - `on-deployed` (Success)
2. **Commit Status**: Argo CD can update your GitHub Commit/PR status with a green checkmark or red X.
3. **Sync Wave Hooks**: Run Kubernetes Jobs on failure to post to custom webhooks.

---

## Part 5: Production Best Practices & Mitigations

### 1. Secrets Management (The "Elephant in the Room")
**The Risk**: You cannot store raw Kubernetes Secrets (YAML with base64 encoded passwords) in Git. Anyone with read access to the repo owns your database credentials.

**The Solution**: Use the **External Secrets Operator (ESO)**.

#### How it works
1.  **AWS Secrets Manager/Vault**: Holds the actual password (`CorrectHorseBatteryStaple`).
2.  **Git Repo**: Holds a public `ExternalSecret` CRD. This contains no sensitive data, just a pointer: "Go fetch `prod/db/pass` from AWS".
3.  **The Operator**: Reads the YAML, authenticates to AWS, and downloads the secret.
4.  **Kubernetes**: The Operator creates a standard K8s Secret named `db-pass`.
5.  **Your App**: Mounts `db-pass` as an environment variable.

#### Why isn't this insecure?
You might ask: "If it ends up as a normal secret, haven't we just moved the problem?"
**No, because we solved the three biggest risks:**
-   **Git History Protection**: The most common leak happens when a developer accidentally commits `.env` to Git. With ESO, the actual password *never* touches a laptop or Git.
-   **Rotation & Sync**: If you rotate the database password in AWS, ESO automatically updates the Kubernetes Secret. No manual `kubectl apply` needed.
-   **Access Control (RBAC)**: While the secret exists in the cluster, you use RBAC to ensure only the Application Pods can read it. Developers can be blocked from running `kubectl get secrets`.

### 2. Data Loss Prevention (The "Prune" Disaster)
**The Risk**: If `prune: true` is enabled (common for clean clusters), deleting a resource from Git deletes it from the cluster. If a dev accidentally deletes a PVC definition, Argo CD will delete the Production Volume.

**The Mitigation**: Annotate critical stateful resources (PVCs, Databases):
```yaml
metadata:
  annotations:
    argocd.argoproj.io/sync-options: Prune=false
```
This tells Argo: "Even if this is missing from Git, **DO NOT** delete it from the cluster."

### 3. Dependency Hell & Sync Waves
**The Risk**: Kubernetes is eventually consistent. If you deploy an App and a DB simultaneously, the App might crash if the DB isn't ready.

**The Mitigation**: Use **Sync Waves**. Argo CD processes waves in order and waits for "Healthy" status before proceeding.
```yaml
# Database Manifest
metadata:
  annotations:
    argocd.argoproj.io/sync-wave: "1"

# App Manifest
metadata:
  annotations:
    argocd.argoproj.io/sync-wave: "2"
```

### 4. The "Cluster Admin" Security Risk
**The Risk**: Installing Argo CD with `cluster-admin` privileges gives it root access to everything. If the UI is compromised, the whole cluster is compromised.

**The Mitigation**:
-   **Role Separation**: Give Argo CD specific RoleBindings for specific namespaces, not cluster-wide admin.
-   **Namespace Isolation**: Run separate Argo CD instances for sensitive teams (e.g., Finance).

### 5. Monorepo Performance
**The Risk**: If you have 500 apps in one repo, Argo CD scans the entire repo for every commit, causing massive latency.

**The Mitigation**:
-   **Webhooks**: Configure GitHub webhooks so Argo CD only polls on actual commits.
-   **Sharding**: Split the Argo CD "Repo Server" component into multiple replicas to handle the load.

---

## Part 6: Tutorial (The Practice Lab)

This tutorial guides you through installing Argo CD and deploying a sample application using a remote VM setup or local cluster.

### Prerequisites
- `kubectl` installed and configured.
- (Optional) SSH access if using a Remote VM.

### Step 1: Install Argo CD
We will install Argo CD in its own namespace.

```bash
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
```

Wait for components to be ready:
```bash
kubectl wait --for=condition=available deployment -l "app.kubernetes.io/name=argocd-server" -n argocd --timeout=300s
```

### Step 2: Access the UI
**Option A: Port-Forwarding**
Forward the port from the cluster to your local machine:
```bash
kubectl port-forward svc/argocd-server -n argocd 8080:443
```

> [!NOTE]
> **Remote VM Users**: You also need to SSH tunnel from your Mac to the VM:
> `ssh -L 8080:localhost:8080 user@remote-vm-ip`

**Verification**: Open `https://localhost:8080` in your browser. Accept the security warning.

### Step 3: Get Credentials
Retrieve the initial admin password:

```bash
kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d; echo
```
- **Username**: `admin`
- **Password**: (Output from command above)

### Step 4: Deploy Your First App
We will deploy the demo "Guestbook" app using the declarative GitOps approach.

```bash
cat <<EOF | kubectl apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: guestbook
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: guestbook
  destination:
    server: https://kubernetes.default.svc
    namespace: default
  syncPolicy:
    automated: 
      prune: true
      selfHeal: true
EOF
```

### Step 5: Verify & Observe Self-Healing
1. Check the UI at `https://localhost:8080`. The "guestbook" app should be **Synced** and **Healthy**.
2. **Simulate Drift**: Manually delete the service.
   ```bash
   kubectl delete service guestbook-ui -n default
   ```
3. **Observe**: Argo CD detects the drift and immediately recreates the service because `selfHeal: true` is set.
4. Verify it is back:
   ```bash
   kubectl get svc guestbook-ui -n default
   ```
   **Result**: You have successfully implemented an auto-healing deployment pipeline.

### Step 6: Advanced Drift Simulation (The "Rogue Admin")
This scenario demonstrates how GitOps prevents "Shadow IT" and undocumented changes.

1.  **The Scenario**: A developer manually increases capacity to handle a traffic spike but forgets to update Git.
    Run this command:
    ```bash
    kubectl scale deployment guestbook-ui --replicas=5 -n default
    ```

2.  **Observe the Drift**:
    -   Go to the Argo CD UI.
    -   The "Guestbook" card will turn **Yellow (OutOfSync)**.
    -   Click **App Diff**: You will see **Live State: 5** vs **Desired State: 1**.

3.  **The Fix (Governance)**:
    -   In a strict GitOps environment, if it's not in Git, it shouldn't exist.
    -   Click **SYNC** > Ensure **Prune** is checked > Click **SYNCHRONIZE**.

4.  **Verification**:
    Argo CD forces the cluster back to the state defined in Git.
    ```bash
    kubectl get deployment guestbook-ui -n default
    ```
    You should see it has returned to **1 replica**.