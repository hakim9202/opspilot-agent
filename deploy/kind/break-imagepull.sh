#!/usr/bin/env bash
# Break demo-healthy-a → ImagePullBackOff (bad image)
set -euo pipefail

CONTEXT="${KUBE_CONTEXT:-kind-opspilot-lab}"
echo "Breaking demo-healthy-a (bad image) on ${CONTEXT}..."
kubectl --context "${CONTEXT}" -n opspilot-demo set image deploy/demo-healthy-a web=opspilot.invalid/does-not-exist:latest
kubectl --context "${CONTEXT}" -n opspilot-demo get pods -l app=demo-healthy-a -w
