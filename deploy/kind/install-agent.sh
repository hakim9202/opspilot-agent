#!/usr/bin/env bash
set -euo pipefail

CONTEXT="${KUBE_CONTEXT:-kind-opspilot-lab}"
CLUSTER_NAME="${CLUSTER_NAME:-opspilot-lab}"
IMAGE="${IMAGE:-opspilot-agent:0.1.0}"
API_URL="${OPSPILOT_API_URL:-http://host.docker.internal:3000}"
INSTALL_TOKEN="${OPSPILOT_AGENT_TOKEN:-install_lab_opspilot_mvp}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

echo "Building agent image ${IMAGE}..."
if ! docker build -t "${IMAGE}" "${ROOT_DIR}"; then
  echo "ERROR: docker build failed" >&2
  exit 1
fi

echo "Loading image into kind cluster ${CLUSTER_NAME}..."
if ! kind load docker-image "${IMAGE}" --name "${CLUSTER_NAME}"; then
  echo "ERROR: kind load failed" >&2
  exit 1
fi

echo "Installing/upgrading Helm release on context ${CONTEXT}..."
helm upgrade --install opspilot-agent \
  "${ROOT_DIR}/deploy/helm/opspilot-agent" \
  --kube-context "${CONTEXT}" \
  --namespace opspilot-system \
  --create-namespace \
  --set image.repository="${IMAGE%%:*}" \
  --set image.tag="${IMAGE##*:}" \
  --set apiUrl="${API_URL}" \
  --set installToken="${INSTALL_TOKEN}" \
  --set clusterName="${CLUSTER_NAME}" \
  --wait \
  --timeout 120s

echo "Agent pods:"
kubectl --context "${CONTEXT}" -n opspilot-system get pods -o wide
echo "Logs (last 50):"
kubectl --context "${CONTEXT}" -n opspilot-system logs -l app.kubernetes.io/name=opspilot-agent --tail=50 || true
