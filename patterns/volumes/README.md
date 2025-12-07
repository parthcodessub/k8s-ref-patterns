 # Kubernetes volume patterns — from primitives to node access

This note walks through common Kubernetes volume patterns, the core concepts you need to understand, and practical use cases (config injection, scratch space, host access, persistent data, and lifecycle considerations).

## Table of contents

- [Core constructs (the triad)](#core-constructs-the-triad)
- [Configuration & secrets (injection pattern)](#configuration--secrets-the-injection-pattern)
- [Scratch space (emptyDir)](#scratch-space-the-emptydir-pattern)
- [Node access (hostPath)](#node-access-the-hostpath-pattern)
- [Persistent data (stateful pattern)](#persistent-data-stateful-pattern)
- [Storage lifecycle](#storage-lifecycle)
- [CSI driver architecture](#under-the-hood-csi-driver-architecture)
- [Installation & verification](#installation-the-chicken-and-egg-question)

---

## Core constructs (the triad)

Kubernetes decouples the request for storage from the implementation. The three primary objects are:

- **StorageClass (SC)** — the profile describing how to provision storage (e.g., AWS gp3, Azure Managed Disk). Used for dynamic provisioning.
- **PersistentVolumeClaim (PVC)** — the "ticket" or request for storage (size, access modes). Use when data should outlive the Pod.
- **PersistentVolume (PV)** — the actual storage resource bound to a PVC.

Notes:

- Static provisioning (an admin creates a PV pointing to a pre-existing disk) is possible but less common in modern clouds.
- Volume expansion: if the StorageClass has `allowVolumeExpansion: true`, you can edit the PVC to increase size (shrinking is typically unsupported).

### Who creates each object

- StorageClasses are usually created by platform engineers or provided by the cloud. They contain driver name and parameters (IOPS, disk type, reclaim policy).
- PVCs are created by applications or Helm charts — they express intent (size, access modes, storage class).
- PVs are created automatically by dynamic provisioning (CSI) or manually by admins for static provisioning.

### Access modes

- `ReadWriteOnce` (RWO) — mount read-write by a single node (block storage: EBS, Azure Disk). Good for databases.
- `ReadWriteMany` (RWX) — mount read-write by multiple nodes (network filesystems: EFS, Azure Files).
- `ReadWriteOncePod` (RWOP) — restricts a volume to a single Pod.

Choose appropriately: RWO for single-writer stateful apps, RWX for shared file storage, RWOP when single-Pod semantics are required.

### Dynamic provisioning workflow

1. Application or manifest creates a PVC.
2. If no matching PV exists, the StorageClass (via the CSI driver) provisions a cloud disk.
3. The PV is bound to the PVC and the Pod consumes the PVC.

If provisioning fails, PVC remains `Pending`. If mount fails, PV may be `Bound` while the Pod is `ContainerCreating` with a `MountVolume` error — inspect CSI logs.

### Example: PVC, Pod, and resulting PV

PVC (request):

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

Pod (consumes PVC):

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-database-pod
spec:
  containers:
    - name: mongo
      image: mongo
      volumeMounts:
        - mountPath: "/data/db"
          name: my-storage-vol
  volumes:
    - name: my-storage-vol
      persistentVolumeClaim:
        claimName: db-data-claim
```

PV (hidden result):

```yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  name: pvc-1234-abcd-5678-ef90
spec:
  capacity:
    storage: 10Gi
  accessModes:
    - ReadWriteOnce
  persistentVolumeReclaimPolicy: Delete
  storageClassName: ebs-sc-production
  claimRef:
    name: db-data-claim
    namespace: default
    kind: PersistentVolumeClaim
  csi:
    driver: ebs.csi.aws.com
    volumeHandle: vol-0a1b2c3d4e5f6g7h8
status:
  phase: Bound
```

### EFS vs S3

- **EFS** (file): uses a CSI driver (`efs.csi.aws.com`), supports RWX, behaves like a shared filesystem.
- **S3** (object): usually accessed via SDK/API. CSI drivers that mount S3 exist but have limitations — S3 is not a drop-in filesystem replacement in most cases.

## Configuration & secrets (the injection pattern)

Mount `ConfigMap`s and `Secret`s as volumes to inject config files or credentials rather than baking them into images.

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

Note: Kubernetes manages ConfigMap volumes with a timestamped directory and an atomic symlink (e.g. `..data -> ..TIMESTAMP`). Using `subPath` bind-mounts the file inode and bypasses that symlink, so live updates are not visible until the Pod restarts.

If you need live updates, prefer mounting the whole directory or use rolling restarts / process signaling when `subPath` is unavoidable.

## Scratch space (the emptyDir pattern)

Use `emptyDir` for temporary scratch space shared between containers in the same Pod. It's ephemeral and deleted when the Pod is removed.

Advantages:

- Survives container restarts inside the Pod.
- Avoids some overlayfs copy-on-write penalties.
- Can be shared between sidecars.

Limitations:

- Data is lost if the Pod is deleted or the node fails.
- `emptyDir` with `medium: Memory` consumes node RAM.

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

## Node access (the hostPath pattern)

`hostPath` mounts a file or directory from the node into the Pod. It's powerful but potentially dangerous — it grants direct host filesystem access.

Common uses:

- Log collectors (DaemonSets) reading `/var/log/containers`.
- CNI/network agents needing node files.
- CI runners mounting `/var/run/docker.sock`.

Security: restrict `hostPath` access via Pod Security Standards or admission controllers (OPA/Gatekeeper). Prefer running a privileged DaemonSet in a restricted namespace and exposing a narrow API to app teams instead of giving apps host access directly.

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

## Persistent data (stateful pattern)

Stateful workloads (databases, queues) should use PVCs so data survives Pod restarts and rescheduling.

Example:

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

## Storage lifecycle (when does data die?)

`reclaimPolicy` controls PV behavior when a PVC is deleted:

- `Delete` (common): deleting the PVC also deletes the PV and underlying cloud disk.
- `Retain` (safer for DBs): deletes the PVC but leaves the cloud disk for manual recovery.

Tips:

- Automate snapshots in the cloud rather than relying solely on `Retain`.
- When tearing down clusters, check for orphaned cloud disks to avoid unexpected costs.

To recover a `Retain`-ed disk, create a PV referencing the existing cloud disk, then bind it to a PVC.

## Under the hood: CSI driver architecture

CSI drivers run as controller and node components:

- Controller (Deployment) — talks to the cloud API to create/delete volumes and attach them to nodes.
- Node driver (DaemonSet) — runs on each node to format and mount devices.

Troubleshooting hints:

- `kubectl get pvc` shows `Pending`: inspect CSI controller logs and IAM/permission issues.
- Pod `MountVolume`/`AttachVolume` errors: inspect CSI node-driver logs on the node running the Pod.
- Volume attached but inaccessible: check `dmesg`/kernel logs on the node.

List installed drivers and a simple output example:

```bash
kubectl get csidrivers
# NAME              ATTACHREQUIRED   PODINFOONMOUNT   STORAGECAPACITY   MODES        AGE
# ebs.csi.aws.com   true             false            false             Persistent   10d
# efs.csi.aws.com   false            false            false             Persistent   5d
```

## Installation: the chicken-and-egg question

Drivers must be installed before relying on dynamic provisioning:

- Automatic: managed clusters often include common drivers.
- Semi-automatic: managed add-ons for drivers like EFS.
- Manual: install via Helm/manifests for third-party drivers.

Verify driver installation:

```bash
kubectl get pods -n kube-system | grep csi
kubectl get csidrivers
kubectl describe storageclass <your-storageclass-name>
```

If controller or node pods are `CrashLoopBackOff`, check logs and RBAC/service account permissions for CSI components.
