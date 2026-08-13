#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-opspilot-lab}"
CONTEXT="kind-${CLUSTER_NAME}"

if kind get clusters | grep -qx "${CLUSTER_NAME}"; then
  echo "kind cluster '${CLUSTER_NAME}' already exists"
else
  echo "Creating kind cluster '${CLUSTER_NAME}'..."
  kind create cluster --name "${CLUSTER_NAME}" --wait 120s
fi

echo "Verifying context ${CONTEXT} (never use company OpenShift context)..."
kubectl --context "${CONTEXT}" cluster-info
kubectl --context "${CONTEXT}" get nodes
echo "Done. Always use: kubectl --context ${CONTEXT} ..."
