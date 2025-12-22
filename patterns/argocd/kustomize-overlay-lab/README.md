# Kustomize & Argo CD Overlay Lab

This lab demonstrates the **industry-standard pattern** for managing multi-environment Kubernetes applications using **Kustomize Overlays** and **Argo CD**.

## 🏗️ Lab Architecture

We built a "Promotion Pipeline" for a web service (`my-app`) targeting two environments: **Dev** and **QA**.

### 1. The Directory Structure
This structure adheres to the "DRY" (Don't Repeat Yourself) principle using Kustomize inheritance.

```text
patterns/argocd/kustomize-overlay-lab/
├── base/                   # 🟢 The "Vanilla" Configuration
│   ├── deployment.yaml     # Defines the core logic (replicas=1, image=nginx:1.21)
│   ├── service.yaml        # Defines the stable network endpoint
│   └── kustomization.yaml  # Aggregates the base resources
│
├── overlays/               # 🟡 Environment Specifics
│   ├── dev/
│   │   └── kustomization.yaml  # HEAVY REUSE: Inherits 'base', adds namePrefix 'dev-'
│   └── qa/
│       └── kustomization.yaml  # PATCHING: Inherits 'base', patches replicas=2
│
└── argocd/                 # 🔵 The Argo CD "Control Plane"
    ├── dev-app.yaml        # AUTO-SYNC: Deploys 'overlays/dev' automatically
    └── qa-app.yaml         # MANUAL-SYNC: Deploys 'overlays/qa' but waits for approval
```

## 🚀 The Promotion Story (GitOps Workflow)

We simulated a real-world software release cycle. Here is how the "GitOps Machinery" works:

| Stage | Action | Argo CD Behavior |
| :--- | :--- | :--- |
| **1. Development** | Engineer updates `base/deployment.yaml` to `nginx:1.22` and pushes to Git. | **Dev App** (Auto-Sync) detects the change and immediately updates the Dev cluster. |
| **2. Testing** | QA Engineers verifying the Dev environment. | **QA App** (Manual-Sync) detects the change, turns **Yellow (OutOfSync)**, but **DOES NOT** deploy. |
| **3. Approval** | Platform Lead/Manager reviews the "Diff" in Argo CD UI. | The human clicks the **SYNC** button. This is the "Approval Gate." |
| **4. Production** | (Not in lab, but same logic) | Same as QA. Production is strictly gated. |

---

## 🏢 Enterprise Patterns for Platform Engineers

If you are designing a platform for 500+ developers, "just making it work" isn't enough. You need scalability and safety.

### 1. Kustomize vs. Helm (The "Great Debate")
*   **Use Helm when:** You are shipping off-the-shelf software (Prometheus, Cert-Manager) to other people. It's a "Package Manager."
*   **Use Kustomize when:** You are managing *internal* applications. You own the code. You don't need the complexity of Helm Templating (`{{ .Values.foo }}`). You just want to patch `replicaCount` for Prod.
*   **The Hybrid:** Use Helm to *render* the YAML, and Kustomize to *patch* it last-mile.

### 2. The "App of Apps" & ApplicationSets
Managing `dev-app.yaml` and `qa-app.yaml` manually is fine for 1 app. For 50 apps, it's toil.
*   **Pattern**: Create a **Root App** that watches specific folders.
*   **Advanced**: Use **ApplicationSets** with a **Matrix Generator**. It allows you to say: *"For every folder in `/apps/*` AND every cluster in my secret list, create an Argo App."*

---

## 🎓 Lead SRE / Platform Engineer Interview Guide

Preparing for a senior role? Here are the "Gotchas" and deep-dive topics you need to master.

### 🚩 The "Gotchas" (Experience Indicators)
1.  **The "Prune" Trap**:
    *   *Question*: "I removed a deployment from Git, but it's still running in the cluster. Why?"
    *   *Answer*: Check if `prune: true` is enabled in the SyncPolicy. If it's disabled (default), Argo CD will "abandon" the resource rather than delete it.
2.  **The "Crashing Loop" (Health vs. Sync)**:
    *   *Question*: "My pod is crashing (CrashLoopBackOff). Will Argo CD mark the App as 'Failed'?"
    *   *Answer*: No. Argo CD syncs *manifests*. If the YAML applied successfully, the Sync is "OK". The *Health Status* will be "Degraded". You must monitor Health, not just Sync.
3.  **The "Shared Resource" Conflict**:
    *   *Scenario*: Two different Argo Apps try to manage the same namespace or ConfigMap.
    *   *Result*: They will fight forever ("Trashing"). Argo CD creates a "Shared Resource Warning". You must use `annotation: argocd.argoproj.io/tracking-id` or separate them.

### 🧠 Deep Dive Concepts
1.  **Sync Waves & Hooks**:
    *   *Problem*: "My App fails because the Database isn't ready yet."
    *   *Solution*: Annotate the DB with `sync-wave: "1"` and the App with `sync-wave: "2"`. Argo CD waits for Wave 1 to be "Healthy" before starting Wave 2.
2.  **Drift Detection**:
    *   *Concept*: Kustomize builds the "Desired State." Kubectl gets the "Live State." Argo CD diffs them.
    *   *Pro Tip*: If you use `HorizontalPodAutoscaler` (HPA), Argo CD will constantly see "Drift" on the `replicas` field. **Fix**: Use `ignoreDifferences` in the Argo Application configuration to ignore the `replicas` field for that Deployment.

### 🗣️ Sample "Senior" Answer
**Q: "How do you handle secrets in a GitOps environment?"**
> "We strictly follow the 'No Secrets in Git' rule. We use **External Secrets Operator (ESO)**. In Git, we commit an `ExternalSecret` manifest that references a path in AWS Secrets Manager or HashiCorp Vault. The ESO operator running in the cluster authenticates to the vault, fetches the actual secret, and creates a native Kubernetes `Secret` object. This keeps our Git history clean and allows for automated secret rotation."
