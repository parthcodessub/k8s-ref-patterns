# Lab: The "Consumer" Experience (Cert-Manager)

## 1. Overview regarding 3rd Party Operators
In the industry, you rarely write Controllers from scratch (like in the previous lab). 99% of the time, you consume them.

The workflow usually looks like this:
1.  **Need**: "We need to manage SSL certificates" on our Ingress.
2.  **Find**: You grab the off-the-shelf Operator (e.g., Cert-Manager).
3.  **Install**: Usually via Helm or `kubectl apply`.
4.  **Use**: You create the CRDs (Intent) that the Operator provides, and it gives you the Native Resources (Reality).

---

## 2. Lab: Cert-Manager

**Why Cert-Manager?** It is the industry standard for X.509 certificate management in Kubernetes.
**The Pattern:**
`Custom Resource (Certificate)` $\rightarrow$ **Operator Logic** $\rightarrow$ `Native Resource (Secret)`

### Step 1: Install the Operator
We will install the "Control Plane" (the logic). Since we are avoiding Helm to keep things clear, we will use the direct manifest.

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml
```

#### What just happened? (Understanding the Output)
When you ran that command, you saw a flood of output. Here is the breakdown:

| Category | Output Lines | Explanation |
| :--- | :--- | :--- |
| **CRDs** | `customresourcedefinition... created` | The API Schema. It taught Kubernetes what a `Certificate`, `Issuer`, and `CertificateRequest` look like. |
| **Identity** | `serviceaccount`, `role`, `binding` | **Service Accounts**: Identity for the pods. <br> **RBAC**: Permissions. The Operator needs "Cluster Admin" like powers to read Ingresses and write Secrets in *any* namespace. |
| **Logic** | `deployment.apps` | The actual software. Creates 3 specific controllers (see below). |
| **Validation** | `validatingwebhookconfiguration` | **The Bouncer**. It registers a webhook so that if you try to apply an invalid Certificate YAML, the API server rejects it immediately. |

#### The 3 Pods Explained
If you run `kubectl get pods -n cert-manager`, you see 3 distinctive pods. These run **Per Cluster**, not per node.

1.  **`cert-manager` (The Controller)**: The brain. It watches for `Certificate` resources and talks to the Issuer (e.g., Let's Encrypt) to get the certs.
2.  **`cert-manager-webhook` (The Validator)**: A dynamic admission controller. It ensures you don't submit broken YAML configuration.
3.  **`cert-manager-cainjector` (The Helper)**: A utility that injects CA bundles into webhooks. It's internal plumbing to make sure the Webhook pod can talk to the API server securely.

---

### Step 2: Configure the "Issuer"
Most Operators need a global configuration before they can work. For Cert-Manager, this is the `ClusterIssuer`. It tells the Operator *how* to sign certificates.

We will create a simple Self-Signed Issuer (great for internal testing).

**File**: [manifests/issuer.yaml](manifests/issuer.yaml)
```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: selfsigned-issuer
spec:
  selfSigned: {}
```

**Apply it:**
```bash
kubectl apply -f 3rd-party-crds/manifests/issuer.yaml
```

---

### Step 3: Create a Custom Resource (The User Action)
Now we act as the developer. We want an SSL certificate. Instead of running `openssl` commands manually, we just ask Kubernetes for it.

**File**: [manifests/certificate.yaml](manifests/certificate.yaml)
(See file for detailed comments on fields)

```bash
kubectl apply -f 3rd-party-crds/manifests/certificate.yaml
```

---

### Step 4: Verify the "Magic" (Reconciliation)
The Cert-Manager Operator saw your `Certificate` CR. Its logic kicked in:
1.  Generated a Private Key.
2.  Created a Certificate Signing Request (CSR).
3.  Signed it using the `selfsigned-issuer`.
4.  Saved the result as a Kubernetes Secret.

**Check the Intent (CR):**
```bash
kubectl describe certificate my-test-cert
```
*Look at the `Events` at the bottom. You will see the operator "Issuing" and then "Stored new private key".*

**Check the Reality (Secret):**
```bash
kubectl get secret my-test-cert-tls
```
If you see that Secret, the pattern worked. The Operator successfully translated your **Intent** (Certificate CR) into **Reality** (Secret).

---

### Step 5: How an App uses the Secret
The ultimate goal isn't the Secret itself, but using it in an App (e.g., Nginx).
The App doesn't know about Cert-Manager. It just mounts the standard K8s Secret.

**File**: [manifests/deployment-using-cert.yaml](manifests/deployment-using-cert.yaml)

```yaml
      containers:
      - name: nginx
        volumeMounts:
        - name: tls-certs
          mountPath: "/etc/nginx/ssl" # <--- App reads files here
      volumes:
      - name: tls-certs
        secret:
          secretName: my-test-cert-tls # <--- K8s injects the Operator-created secret
```

---

## 3. Lead Level Interview Considerations

When using 3rd party operators, the interview questions shift from "How do you write code?" to "How do you manage lifecycle?":

### 1. The "Chicken and Egg" Problem (Installation Order)
*   **Question**: "How do you handle installing an Operator that defines CRDs, and applying instances of those CRDs in the same Helm chart?"
*   **Answer**: You usually can't reliably. The CRDs must be established in the API server before any resource tries to use them. In ArgoCD, this often means using SyncWaves (install CRDs in Wave -1, Resources in Wave 0) or separate applications.

### 2. Scope & Security
*   **Question**: "Should you run one Operator per namespace or one per cluster?"
*   **Answer**: **Cluster-scoped** (like we just did) is standard for infrastructure (Cert-Manager, Ingress). **Namespace-scoped** is better for multi-tenant SaaS where Team A's database operator shouldn't see Team B's databases.

### 3. The "CRD Upgrade" Nightmare (Data Loss)
*   **Scenario**: You rename `spec.image` to `spec.containerImage` in your Go code and apply the new CRD.
*   **The Trap**: Kubernetes does not automatically migrate data. Existing objects in etcd still have `image`, but the new CRD expects `containerImage`. This causes validation errors or potential crashes.
*   **The Lead Answer**: **Conversion Webhooks**. You keep `v1` (with `image`) and create `v2` (with `containerImage`). You write a webhook that translates `v1` requests to `v2` on the fly.
*   **Interview Gold**: Mention **Stored Versions**. You need to know which version is actually saved in etcd vs. which is served.

### 4. The "Privilege Escalation" Backdoor
*   **Scenario**: You allow developers to create `AppService` CRs, but not Pods. The CRD has a `spec.volumes` field.
*   **The Trap**: A developer mounts `/etc/shadow` via the CR. The Operator (running as ClusterAdmin) creates the Pod with that volume. The developer gains root access.
*   **The Lead Answer**: **Operators are a security proxy**.
    *   **Sanitization**: Explicitly validate inputs. Don't blindly copy spec fields to Pods.
    *   **Hardcoding**: Don't expose sensitive fields like `volumes` unless necessary.
    *   **Least Privilege**: Does the Operator really need ClusterAdmin?

### 5. The "Two-Brain" Problem (High Availability)
*   **Scenario**: You set `replicas: 2` for your Operator deployment to ensure HA.
*   **The Trap**: Both pods run the reconcile loop. They fight over updates, leading to race conditions and "Flapping" states.
*   **The Lead Answer**: **Leader Election**.
    *   Use `client-go` or Kubebuilder's default leader election.
    *   Pods race to grab a `Lease` (lock) in the cluster.
    *   Only the "Leader" reconciles; the "Standby" waits.

### 6. The "Uninstall" Catastrophe
*   **Scenario**: You run `helm uninstall cert-manager` or `kubectl delete -f cert-manager.yaml`.
*   **The Trap**: Deleting a CRD definition deletes **ALL** instances of that CRD.
    *   Deleting the `Certificate` CRD deletes all 5,000 `Certificate` objects.
    *   This triggers Garbage Collection, deleting all associated Secrets.
    *   **Result**: Global production outage.
*   **The Lead Answer**:
    *   **Separate Lifecycles**: Manage CRDs separately from the Operator deployment.
    *   **Helm Resource Policy**: Use `helm.sh/resource-policy": keep` on CRDs so they survive uninstall.