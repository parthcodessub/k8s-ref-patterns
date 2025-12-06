Kubernetes Volume Patterns: From Primitives to Node Access

1. The Core Constructs (The "Triad")

Before looking at specific use cases, you must understand how Kubernetes abstracts storage. It decouples the request for storage from the implementation of storage.

Concepts

StorageClass (SC): The "Profile". It tells Kubernetes how to create storage (e.g., AWS gp3, Azure Managed Disk).

Is this required? Yes, for Dynamic Provisioning. If you want Kubernetes to talk to AWS/Azure and create a disk automatically when a user asks for it, you need a StorageClass.

Exception: You can do "Static Provisioning" where an admin manually creates a disk in the cloud, manually creates a PersistentVolume (PV) object pointing to it, and then a user claims it. This is rare in modern enterprises.

# Kubernetes volume patterns — from primitives to node access

This note walks through common Kubernetes volume patterns, the core concepts you need to understand, and practical use cases (Config injection, scratch space, host access, persistent data, and lifecycle considerations).

---

## 1. Core constructs (the triad)

Kubernetes decouples the request for storage from the implementation. The three primary objects are:

- **StorageClass (SC)** — the profile describing how to provision storage (e.g., AWS gp3, Azure Managed Disk). Required for dynamic provisioning.
- **PersistentVolumeClaim (PVC)** — the "ticket" or request for storage (size, access modes). Use when data should outlive the Pod.
- **PersistentVolume (PV)** — the actual storage resource bound to a PVC.

Notes:
- Static provisioning (an admin creates a PV pointing to a pre-existing disk) is possible but less common in modern clouds.
- Volume expansion: if the StorageClass has `allowVolumeExpansion: true`, you can edit the PVC to increase size (shrinking is typically unsupported).

#### More clarity: when to create each object

- A **StorageClass** is generally created by platform engineers or provided by your cloud. It contains the driver name and parameters (IOPS, disk type, reclaim policy, etc.). You typically reference a StorageClass from a PVC (via `storageClassName`) so the cluster knows how to dynamically provision a PV.
- A **PVC** is created by an application or Helm chart that needs storage. It expresses intent (size, access modes, storage class). The PVC is the user-facing API.
- A **PV** is created automatically when a PVC is dynamically provisioned, or created manually during static provisioning. Users normally interact only with PVCs; admins manage PVs when necessary.

### Access modes

- **ReadWriteOnce (RWO)** — mount read-write by a single node (block storage: EBS, Azure Disk). Good for databases.
- **ReadWriteMany (RWX)** — mount read-write by multiple nodes (network filesystems: EFS, Azure Files). Good for shared assets.
- **ReadWriteOncePod (RWOP)** — stricter variant restricting a volume to a single Pod.

#### Practical guidance on choosing access modes

- Use **RWO** for stateful applications that require a single writer (Postgres, MySQL). If you run multiple replicas that need separate data, give each replica its own PVC.
- Use **RWX** for truly shared file storage (web servers serving the same files, shared caches). Note that RWX backends are typically network filesystems and can have different performance/consistency characteristics.
- **RWOP** is useful when you must ensure only one pod (not just one node) uses the volume — helpful for strict single-active workflows.

### Dynamic provisioning workflow

1. Pod references a PVC.
2. PVC is created; if no matching PV exists, the StorageClass (via the CSI driver) provisions a cloud disk.
3. The PV is bound to the PVC. Subsequent Pod restarts reattach to the existing PV (no new provisioning).

#### Notes on timing and failure modes

- The provisioning step involves the CSI controller talking to the cloud provider API; if the controller lacks IAM permissions, provisioning will fail and the PVC will remain `Pending`.
- If provisioning succeeds but mounting fails, the PV may be `Bound` while the Pod remains `ContainerCreating` with a `MountVolume` error. Check node logs and CSI node-driver logs in that case.

Example PVC:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: db-data-claim
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  storageClassName: ebs-sc-production
```

### EFS vs S3

- **EFS (file)**: uses a CSI driver (`efs.csi.aws.com`), supports `ReadWriteMany` and behaves like a shared filesystem.
- **S3 (object)**: usually accessed via SDK/API calls (not a PVC). There are CSI drivers that present S3 as a mount, but they have limitations and are not a universal replacement for a filesystem.

---

## 2. Configuration & secrets (the injection pattern)

Mount `ConfigMap`s and `Secret`s as volumes to inject config files or credentials into containers instead of baking them into images.

Pattern:
1. Create a `ConfigMap` or `Secret`.
2. Mount it as a volume in the Pod manifest.

Example (mount a single file using `subPath`):

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: app-with-config
spec:
  containers:
    - name: my-app
      image: my-app:v1
      volumeMounts:
        - name: config-vol
          mountPath: /etc/app/config.json
          subPath: config.json
          readOnly: true
  volumes:
    - name: config-vol
      configMap:
        name: my-app-settings
```

#### Atomic symlink behavior and `subPath` caveat

Kubernetes manages ConfigMap volumes with a timestamped directory and symlink (`..data -> ..TIMESTAMP`). Updating the ConfigMap swaps the symlink atomically so files update in-place.

If you use `subPath`, Kubernetes bind-mounts the specific file inode directly into the container; this bypasses the symlink mechanism and prevents live updates from being visible until the Pod is restarted.

#### Suggested approaches if you need live updates with `subPath`-like behavior

- Avoid `subPath` when you need live updates. Mount the whole directory and let the application read the file path.
- If you must use `subPath`, implement a restart strategy (rolling update) or signal the process to re-open files after a ConfigMap change.

---

## 3. Scratch space (the `emptyDir` pattern)

Use `emptyDir` for temporary scratch space shared between containers in the same Pod. Data is ephemeral and deleted when the Pod is removed.

Advantages over the container writable layer:

- Survives container restarts within the same Pod.
- Avoids copy-on-write performance penalties of overlay filesystems.
- Can be shared between containers in a Pod (sidecars).
- Optionally set `sizeLimit` to bound ephemeral storage usage.

#### When not to use `emptyDir`

- Do not use `emptyDir` for data that must survive node failure, eviction, or Pod deletion. For durable data, use a PVC backed by persistent storage.
- `emptyDir` on `medium: Memory` consumes node RAM and is limited by node memory; use with caution for large workloads.

Example:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: data-processor
spec:
  containers:
  - name: worker
    image: processor:latest
    volumeMounts:
    - mountPath: /cache
      name: scratch-pad
  volumes:
  - name: scratch-pad
    emptyDir:
      sizeLimit: 500Mi
      # medium: Memory  # uncomment to use tmpfs
```

---

## 4. Node access (the `hostPath` pattern)

`hostPath` mounts a file or directory from the node into the Pod. It is powerful but dangerous.

Use cases:

- Log aggregators (Fluentd) that must read `/var/log/containers`.
- CNI/network agents needing node-level configuration.
- Docker-in-Docker (CI runners mounting `/var/run/docker.sock`).

Security warning: `hostPath` gives pods direct access to the host filesystem and can lead to cluster compromise if misused. Enforce policies (Pod Security Standards, OPA Gatekeeper) to restrict `hostPath` to system namespaces.

#### Safer alternatives to `hostPath`

- Use a privileged DaemonSet running in a restricted namespace to perform node-level operations, and expose only a limited API or shared mount to application teams.
- Use sidecar or init containers to move just the required files into a controlled volume rather than mounting the entire host directory.

Example:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: log-agent
spec:
  containers:
  - name: fluentd
    image: fluentd:latest
    volumeMounts:
    - mountPath: /var/log
      name: varlog
    - mountPath: /var/lib/docker/containers
      name: dockercontainerlog
      readOnly: true
  volumes:
  - name: varlog
    hostPath:
      path: /var/log
  - name: dockercontainerlog
    hostPath:
      path: /var/lib/docker/containers
```

---

## 5. Persistent data (stateful pattern)

Databases and stateful workloads should use a `PersistentVolumeClaim` so data survives Pod restarts and rescheduling.

Example Pod using a PVC:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: postgres-pod
spec:
  containers:
    - name: postgres
      image: postgres:13
      volumeMounts:
        - mountPath: /var/lib/postgresql/data
          name: db-storage
  volumes:
    - name: db-storage
      persistentVolumeClaim:
        claimName: db-data-claim
```

---

## 6. Storage lifecycle (when does data die?)

Every StorageClass (and PV) has a `reclaimPolicy` that controls what happens when the PVC is deleted:

- **Delete** (default for many cloud StorageClasses): deleting the PVC also deletes the PV and the underlying cloud disk.
- **Retain** (safer for databases): deleting the PVC marks the PV Released but leaves the cloud disk intact for manual recovery.

Tip: When tearing down clusters, check for orphaned cloud disks (they can persist and incur costs if you delete the cluster before cleaning PVCs).

#### Practical recovery and backup notes

- For production databases, prefer `Retain` reclaim policy combined with automated snapshotting (cloud snapshots) rather than relying solely on `Retain` for recovery.
- Automate snapshots and retention policies in your cloud account to avoid manual recovery steps when clusters are deleted.
- To recover a `Retain`-ed disk, an admin can create a PV that references the existing cloud disk and bind it to a PVC for the application to reuse.

---

## 7. Under the hood: CSI driver architecture

CSI drivers are implemented as pods in the cluster (controller + node components):

- **Controller (Deployment)** — talks to the cloud API to create/delete volumes and attach them to nodes.
- **Node driver (DaemonSet)** — runs on each node to format and mount devices into the OS namespace.

If a PVC is `Pending`, check the controller logs for permission or API errors. If a Pod is stuck with `MountFailed`, check the node driver logs on that node.

#### How CSI components map to troubleshooting steps

- `kubectl get pvc` shows `Pending`: inspect CSI controller logs (often in `kube-system`) and verify IAM/Cloud permissions for the controller.
- Pod-level `MountVolume` or `AttachVolume` errors: inspect CSI node-driver logs on the node where the Pod is scheduled.
- Volume attached but inaccessible: check kernel logs (`dmesg`) on the node for filesystem or device errors.

You can list installed drivers with:

```sh
kubectl get csidrivers
# NAME              ATTACHREQUIRED   PODINFOONMOUNT   STORAGECAPACITY   MODES        AGE
# ebs.csi.aws.com   true             false            false             Persistent   10d
# efs.csi.aws.com   false            false            false             Persistent   5d
```

---

## 8. Installation: the chicken-and-egg question

PVCs assume the CSI driver is already present. Drivers are installed in one of three ways:

- **Automatic (managed clusters):** cloud providers often pre-install standard drivers (EBS, GCE PD) when the cluster is created.
- **Semi-automatic (managed add-ons):** some drivers (EFS, advanced features) are added as cluster add-ons.
- **Manual (Helm / manifests):** platform engineers must install third-party drivers explicitly.

When a driver is installed it registers a `CSIDriver` object in the cluster. Ensure the driver is installed before creating StorageClasses that reference it.

#### Quick verification after installation

Run these commands to validate the driver installation and configuration:

```sh
kubectl get pods -n kube-system | grep csi
kubectl get csidrivers
kubectl describe storageclass <your-storageclass-name>
```

If controller or node pods are CrashLooping, inspect their logs and check RBAC/service account permissions for the CSI controller components.