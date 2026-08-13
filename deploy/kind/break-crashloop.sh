#!/usr/bin/env bash
# Break demo-healthy-c → CrashLoopBackOff (exit 2)
set -euo pipefail

CONTEXT="${KUBE_CONTEXT:-kind-opspilot-lab}"
echo "Breaking demo-healthy-c (exit 2) on ${CONTEXT}..."
kubectl --context "${CONTEXT}" -n opspilot-demo patch deploy/demo-healthy-c --type=json -p='[
  {"op":"replace","path":"/spec/template/spec/containers/0/command","value":["sh","-c","echo FATAL: database connection refused; exit 2"]}
]'
kubectl --context "${CONTEXT}" -n opspilot-demo get pods -l app=demo-healthy-c -w
