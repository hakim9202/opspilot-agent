#!/usr/bin/env bash
# Break demo-healthy-a → InvalidImageName (illegal image reference)
# Distinct from ImagePullBackOff: kubelet rejects the name before any registry pull.
set -euo pipefail

CONTEXT="${KUBE_CONTEXT:-kind-opspilot-lab}"
echo "Breaking demo-healthy-a (InvalidImageName) on ${CONTEXT}..."
# Uppercase + illegal chars → InvalidImageName (not a DNS/registry failure).
kubectl --context "${CONTEXT}" -n opspilot-demo set image \
  deploy/demo-healthy-a web='INVALID_IMAGE!!'
kubectl --context "${CONTEXT}" -n opspilot-demo get pods -l app=demo-healthy-a -o wide
echo "Watch: kubectl --context ${CONTEXT} -n opspilot-demo get pods -l app=demo-healthy-a -w"
