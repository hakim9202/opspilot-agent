#!/usr/bin/env bash
# Break demo-healthy-b → OOMKilled (tiny limit + memory hog)
set -euo pipefail

CONTEXT="${KUBE_CONTEXT:-kind-opspilot-lab}"
echo "Breaking demo-healthy-b (OOM) on ${CONTEXT}..."
kubectl --context "${CONTEXT}" -n opspilot-demo patch deploy/demo-healthy-b --type=json -p='[
  {"op":"replace","path":"/spec/template/spec/containers/0/resources","value":{"requests":{"memory":"16Mi"},"limits":{"memory":"16Mi"}}},
  {"op":"replace","path":"/spec/template/spec/containers/0/command","value":["sh","-c","dd if=/dev/zero of=/dev/null bs=1M count=200 || true; sleep 1; exit 1"]}
]'
kubectl --context "${CONTEXT}" -n opspilot-demo get pods -l app=demo-healthy-b -w
