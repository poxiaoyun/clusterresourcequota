# ClusterResourceQuota AI Instructions

## Project Overview
ClusterResourceQuota (CRQ) is a Kubernetes controller that extends Resource Quota capabilities. It enables:
- **Multi-Namespace Quota Management**: Define limits across multiple namespaces using label selectors.
- **Node Selector Scopes**: Apply quotas based on pod node selectors (e.g., limiting specific GPU models).
- **Compatibility**: Provides a custom `ResourceQuota` type that is compatible with `corev1.ResourceQuota`.

## Architecture
- **Framework**: Built using `sigs.k8s.io/controller-runtime`.
- **API Group**: `quota.xiaoshiai.cn/v1`.
- **CRDs**:
  - `ClusterResourceQuota` (Cluster-scoped): Selects namespaces via `spec.namespaceSelector`.
  - `ResourceQuota` (Namespace-scoped): Custom implementation supporting extended scopes.
- **Controller Logic**:
  - `ClusterResourceQuotaReconciler` (`clusterresourcequota-controller.go`) watches `ClusterResourceQuota` and `Namespace`.
  - **Namespace Watch**: Changes to Namespaces trigger reconciliation of matching `ClusterResourceQuota` objects.
  - **Sync**: The controller manages the distribution and aggregation of quotas across selected namespaces.

## Development Workflows

### Build & Run
- **Build Binaries**: `make build` (builds for linux/amd64 and linux/arm64 in `bin/`).
- **Build Docker Image**: `make release-image`.
- **Helm Package**: `make release-helm`.

### Code Generation
This project uses a hybrid of `controller-gen` and `k8s.io/code-generator`.
- **Run All Generation**: `make generate`
- **CRD Manifests**: `make generate-crd` (uses `controller-gen`, outputs to `deploy/clusterresourcequota/crds`).
- **Clientsets/Listers/Informers**: `make generate-code` (uses `hack/update-codegen.sh`).
  - **Important**: Always run `make generate` after modifying types in `apis/quota/v1/types.go`.

### Testing
- **Unit Tests**: Standard Go testing. `go test ./...`
- **E2E Tests**: Located in `test/e2e/`.

## Code Conventions
- **API Definitions**: Located in `apis/quota/v1/types.go`. Use `+kubebuilder` and `+genclient` markers.
- **Controller**:
  - Use `Reconcile(ctx context.Context, req reconcile.Request)` pattern.
  - Use `predicate.Predicate` to filter events (e.g., `OnClusterResourceQuotaSpecChange`).
  - Handle `Namespace` changes by mapping them back to relevant `ClusterResourceQuota` objects.
- **Logging**: Use `xiaoshiai.cn/common/log`.
- **Constants**: Define in `apis/quota/v1/constants.go`.

## Key Files
- `apis/quota/v1/types.go`: CRD struct definitions.
- `clusterresourcequota-controller.go`: Main reconciliation loop.
- `hack/update-codegen.sh`: Script for generating typed clients.
- `deploy/clusterresourcequota/values.yaml`: Helm chart configuration.

## Common Tasks
- **Adding a Field**:
  1. Update struct in `apis/quota/v1/types.go`.
  2. Run `make generate`.
  3. Update controller logic if necessary.
- **Modifying Controller Logic**:
  - Edit `clusterresourcequota-controller.go`.
  - Ensure `Setup` function watches relevant resources.
- **New Feature**:
  - Scan existing logic for integration points.
  - Before implementing any new feature, produce a short implementation plan that describes the proposed approach, the key files and functions to change, expected risks/side-effects, and any migration or generation steps required. Present this plan and wait for explicit confirmation or approval before modifying the codebase.
  - Explain design decisions and ask for feedback if unsure.
