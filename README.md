# ClusterResourceQuota

ClusterResourceQuota (CRQ) is an extension to Kubernetes Resource Quota that allows cluster administrators to define resource usage limits across multiple namespaces based on label selectors. This feature is particularly useful in multi-tenant environments where resource allocation needs to be managed at a higher level.

## Features

- Multi-Namespace Quota Management
- Node Selector Scope Enhancement ResourceQuota
- Original ResourceQuota Compatibility, You can safely use ResourceQuota instead of corev1.ResourceQuota

Scope `NodeSelector` allows users to set resource quotas based on pod's node selectors.

For example, you can limit the number of GPUs of a specific model.

```yaml
spec:
  scopes:
    - NodeSelector
  scopeSelector:
    matchExpressions:
      - scopeName: "NodeSelector"
        operator: "In"
        values:
          - nvidia.com/gpu.product=A100
```

## Installation

```bash
helm install clusterresourcequota ./charts/clusterresourcequota
```

## Usage

Setting up a ClusterResourceQuota:

```yaml
apiVersion: quota.xiaoshiai.cn/v1
kind: ClusterResourceQuota
metadata:
  name: limit-a100-gpu-on-team-a
spec:
  namespaceSelector:
    matchLabels:
      tenant: team-a
  scopes:
    - NodeSelector
  scopeSelector:
    matchExpressions:
      - scopeName: "NodeSelector"
        operator: "In"
        values:
          - nvidia.com/gpu.product=A100
  hard:
    cpu: "4"
    memory: "8Gi"
    requests.nvidia.com/gpu: "4"
```

Or Only Use Resource Quota

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: limit-a100-gpu
spec:
  scopes:
    - NodeSelector
  scopeSelector:
    matchExpressions:
      - scopeName: "NodeSelector"
        operator: "In"
        values:
          - nvidia.com/gpu.product=A100
  hard:
    requests.nvidia.com/gpu: "4"
```
