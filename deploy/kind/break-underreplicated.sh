#!/usr/bin/env bash
# Scale a broken (or soon-broken) demo to 2 replicas → DeploymentUnhealthy
set -euo pipefail

CONTEXT="${KUBE_CONTEXT:-kind-opspilot-lab}"
TARGET="${1:-demo-healthy-c}"
echo "Scaling ${TARGET} to 2 on ${CONTEXT} (expect unavailable if pods are failing)..."
kubectl --context "${CONTEXT}" -n opspilot-demo scale "deploy/${TARGET}" --replicas=2
kubectl --context "${CONTEXT}" -n opspilot-demo get deploy "${TARGET}" -o wide
kubectl --context "${CONTEXT}" -n opspilot-demo get pods -l "app=${TARGET}"
