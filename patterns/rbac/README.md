# Enterprise RBAC: The "Hidden" Security Layer

This is a crucial pivot. In an interview, moving from "How do I build it?" (Operators) to "How do I secure it?" (RBAC) shows you have the full platform picture.

In an Enterprise, **RBAC (Role-Based Access Control)** is not about hand-crafting rules for every user. It is about implementing **Scalable Patterns** that map to your organization's structure.

Here is the deep dive into Enterprise RBAC, followed by labs using your existing Kind setup.

---

## 1. The Core Enterprise Pattern: "Persona-Based" Access
Enterprises rarely assign permissions to individuals (e.g., "Bob"). They assign permissions to **Groups** (via OIDC/Active Directory) which map to **Personas**.

| Persona | Scope | Typical Permissions | The "Why" |
| :--- | :--- | :--- | :--- |
| **Platform SRE** | Cluster | `cluster-admin` | Need to upgrade the cluster, manage nodes, install Operators. |
| **App Developer** | Namespace | `edit` (minus some rights) | Can manage Deployments/Services in `team-a`, but cannot touch Quotas, Roles, or other namespaces. |
| **Viewer/Auditor** | Cluster/NS | `view` | Read-only access for debugging or compliance. |
| **CI/CD Bot** | Namespace | Specific (Apply YAMLs) | Automated pipeline accounts (ServiceAccounts). |

---

## 2. Technical Details: The "Hidden" Mechanisms
To be a Lead, you need to know how the gears turn.

### The Basics
*   **Deny by Default**: Kubernetes has no "Deny" rules. You only have "Allow." If you don't explicitly allow it, it is forbidden.
*   **Role vs. ClusterRole**:
    *   **Role**: Namespaced (e.g., "Can read pods in `default`").
    *   **ClusterRole**: Global (e.g., "Can read nodes") OR a template for a Role.
*   **The "Bind"**: A **RoleBinding** connects a **Subject** (User/Group/ServiceAccount) to a **Role**.

### Deep Dive: Users vs. Groups
**Question**: "How does the binding work? Do we only use one or the other?"

**Answer**: You can use both, but Enterprises almost exclusively use **Groups**.

1.  **The Concept**: Kubernetes trusts the Authenticator (OIDC, Certificates, Cloud IAM). It does not have an internal database of users.
    *   **User**: A specific identity (e.g., `jane@company.com`).
    *   **Group**: A logical collection (e.g., `groups=["engineering", "us-east-team"]`).

2.  **How Bindings Work (The Union)**: Permissions are **Additive**.
    *   If Jane is in the `engineering` group (Read Access) AND is bound as User `jane` (Write Access), she gets **Read AND Write**.

3.  **The Enterprise Pattern (Why avoid Users?)**:
    *   If you bind to User `jane` and she leaves the company, you must hunt down every RoleBinding in every namespace to delete it.
    *   If you bind to Group `engineering`, you just remove her from the group in Active Directory/Okta. Her access vanishes immediately.

**The Hybrid Approach:**
```yaml
subjects:
# 1. THE STANDARD: Broad access via Groups
- kind: Group
  name: "oidc:engineering-team"
  apiGroup: rbac.authorization.k8s.io

# 2. THE EXCEPTION: Specific Service Accounts (Bots)
- kind: ServiceAccount
  name: ci-cd-bot
  namespace: default

# 3. THE EMERGENCY: Break-glass User (Rare)
- kind: User
  name: "admin-user-1"
  apiGroup: rbac.authorization.k8s.io
```

---

## 3. Lab: The "Restricted Developer" Pattern

*   **Scenario**: You have a developer who needs to debug applications.
*   **Requirement**: They can list Pods, view Logs, and exec into containers.
*   **Constraint**: They **MUST NOT** see Secrets (which might contain DB passwords).
*   **Challenge**: The default Kubernetes `edit` ClusterRole allows reading Secrets. We need a custom role.
*   **Method**: We will use the Impersonation feature (`kubectl --as`) to test this.

### Step 1: Create the Namespace
```bash
kubectl create namespace dev-team
```

### Step 2: Create the Custom Role
**File**: [manifests/dev-role.yaml](manifests/dev-role.yaml)
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  namespace: dev-team
  name: app-debugger
rules:
# 1. Allow core workload viewing (No Secrets!)
- apiGroups: [""]
  resources: ["pods", "pods/log", "services", "configmaps"]
  verbs: ["get", "list", "watch"]
# 2. Allow executing commands
- apiGroups: [""]
  resources: ["pods/exec"]
  verbs: ["create"]
# 3. Allow managing deployments
- apiGroups: ["apps"]
  resources: ["deployments", "replicasets"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```
**Apply it:**
```bash
kubectl apply -f patterns/rbac/manifests/dev-role.yaml
```

### Step 3: Bind the Role to a "User"
> [!NOTE]
> **Why not a Group?**
> In a real Enterprise with OIDC, you would bind this Role to a **Group** (e.g., `dev-team`). For this lab, we bind to a **User** (`jane`) to easily simulate permissions with `kubectl --as jane`.

**File**: [manifests/dev-binding.yaml](manifests/dev-binding.yaml)
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: jane-debugger-binding
  namespace: dev-team
subjects:
- kind: User
  name: jane
  apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: Role
  name: app-debugger
  apiGroup: rbac.authorization.k8s.io
```
**Apply it:**
```bash
kubectl apply -f patterns/rbac/manifests/dev-binding.yaml
```

### Step 4: Verify (The "Can-I" Command)
**Can Jane list pods?**
```bash
kubectl auth can-i list pods --namespace dev-team --as jane
# Output: yes
```

**Can Jane read secrets?**
```bash
kubectl auth can-i get secrets --namespace dev-team --as jane
# Output: no (Victory!)
```

---

## 4. Lab: ClusterRole Aggregation (The "Magic" Label)

**The Scenario**: You have installed your AppService CRD. By default, the built-in Kubernetes `view` ClusterRole (used by auditors) does not know about your AppService.

*   **Old Way**: You manually edit the system `view` role (Bad practice! Updates will overwrite your changes).
*   **Lead Way**: You create a new ClusterRole with a specific label, and the Kubernetes API server **automatically merges** it into the `view` role.

### Step 1: Check the Status Quo
Check if the default `view` role lets you see AppServices.
```bash
# We check if the 'view' role includes 'appservices'
kubectl describe clusterrole view | grep appservices
# Result: Likely empty.
```

### Step 2: Create the Aggregated Role
We create a new ClusterRole. The magic is in the `metadata.labels`.

**File**: [manifests/appservice-viewer.yaml](manifests/appservice-viewer.yaml)
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: view-appservices
  labels:
    # THIS IS THE MAGIC LINE:
    rbac.authorization.k8s.io/aggregate-to-view: "true"
rules:
- apiGroups: ["webapp.mydomain.com"]
  resources: ["appservices"]
  verbs: ["get", "list", "watch"]
```

**Apply it:**
```bash
kubectl apply -f patterns/rbac/manifests/appservice-viewer.yaml
```

### Step 3: Verify the "Magic"
The Kubernetes Controller Manager watches for roles with that label and updates the system `view` role instantly.

```bash
kubectl describe clusterrole view | grep appservices
# Result: You should now see your custom resource listed inside the system role!
```

---

## 5. Enterprise Strategy: Defaults vs. Custom?

### Available Aggregators
Out of the box, Kubernetes provides exactly three magic labels:

| Label Key | Target Role | Use Case |
| :--- | :--- | :--- |
| `rbac.authorization.k8s.io/aggregate-to-view: "true"` | `view` | "I want readonly users to see my new CRD." |
| `rbac.authorization.k8s.io/aggregate-to-edit: "true"` | `edit` | "I want developers to Create/Update my new CRD." |
| `rbac.authorization.k8s.io/aggregate-to-admin: "true"` | `admin` | "I want namespace admins to manage this CRD's config." |

> **Why no `aggregate-to-cluster-admin`?**
> Because `cluster-admin` is already a "God Mode" role (`verbs: ['*'], resources: ['*']`). It automatically has permission to access everything.

### Decision Matrix
**Option A: Extending Default Roles (The "Add-On" Pattern)**
*   **Best for**: 3rd Party Tools & Operators (Prometheus, ArgoCD).
*   **Action**: Add `aggregate-to-edit: "true"` to your Operator's ClusterRole.
*   **Pros**: Zero friction. It just works.
*   **Cons**: You inherit the "baggage" of default roles (e.g., `view` allows reading all ConfigMaps).

**Option B: Custom Base Roles (The "Governance" Pattern)**
*   **Best for**: Internal Team Personas where security is strict.
*   **Action**: Create your own "Base Role" with a custom `aggregationRule` and your own custom labels (e.g., `acme.com/aggregate-to-developer`).
*   **Why**: Allows you to strip permissions (like strict Secret denial) while still keeping the modularity of aggregation.

---

## 6. Lead Level Interview Considerations (Gotchas)

### Gotcha 1: The `system:masters` Trap
*   **Question**: "I removed the cluster-admin binding for my user, but I still have full access. Why?"
*   **Answer**: Check your client certificate `O` (Organization) field. If it is `system:masters`, you bypass RBAC entirely. The API server has a hardcoded break-glass mechanism for this group.

### Gotcha 2: Privilege Escalation (The "Root" Backdoor)
*   **Scenario**: A developer with "Create Pod" rights deletes a node.
*   **How**: They created a **"Privileged Pod"** mounting `hostPath: /`. They exec'd in, became root on the node, and shut it down.
*   **The Fix**: RBAC is just the first layer. You need **Policy Enforcement** (Kyverno/OPA) to block privileged pod creation.

### Gotcha 3: The "Self-Escalation" Prevention
*   **Scenario**: You give Bob permission to edit Roles. Can Bob edit his own Role to give himself `cluster-admin`?
*   **Answer**: **No.** Kubernetes has a built-in prevention mechanism. You cannot create or update a Role to have permissions you don't already possess yourself.

### Gotcha 4: "Can-I" for CI/CD
In your CI/CD pipelines, don't just fail with "Forbidden." Use `auth can-i` to pre-flight check permissions for better error messages.

```bash
if ! kubectl auth can-i create deployments; then
  echo "Error: This pipeline token lacks deployment permissions!"
  exit 1
fi
```