# Raw Kubernetes manifests

Prefer the Helm chart under `../helm/opspilot-agent` for installs.

The chart creates:
- ServiceAccount
- ClusterRole / ClusterRoleBinding (get/list/watch only; no Secrets)
- Deployment (no Service / no inbound ports)
