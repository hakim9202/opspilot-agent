#!/usr/bin/env bash
set -euo pipefail

CONTEXT="${KUBE_CONTEXT:-kind-opspilot-lab}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Resetting opspilot-demo to 3 healthy baselines on ${CONTEXT}..."
kubectl --context "${CONTEXT}" apply -f "${SCRIPT_DIR}/demos.yaml"

# Remove legacy broken demos if present
for d in demo-healthy demo-crashloop demo-exit2 demo-imagepull demo-oom; do
  kubectl --context "${CONTEXT}" -n opspilot-demo delete deploy "$d" --ignore-not-found >/dev/null
done

kubectl --context "${CONTEXT}" -n opspilot-demo rollout status deploy/demo-healthy-a --timeout=90s
kubectl --context "${CONTEXT}" -n opspilot-demo rollout status deploy/demo-healthy-b --timeout=90s
kubectl --context "${CONTEXT}" -n opspilot-demo rollout status deploy/demo-healthy-c --timeout=90s
kubectl --context "${CONTEXT}" -n opspilot-demo get deploy,pods
echo "Ready: 3 healthy pods. Break them manually when you want incidents."
