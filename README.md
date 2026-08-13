# OpsPilot Agent

Lightweight Go agent that runs inside a customer Kubernetes cluster, collects approved operational signals (read-only), redacts secrets, and ships structured events to the OpsPilot SaaS API.

## Features (MVP)

- Agent registration with installation token
- Heartbeats
- Kubernetes pod / deployment / event collection via client-go informers
- Secret redaction
- HTTPS batch transport with local retry queue
- Read-only RBAC (no cluster mutations, no Secrets access)

## Quick start (kind lab)

```bash
# Prerequisites: Docker Desktop, Go, kind, helm, kubectl
./deploy/kind/setup-cluster.sh
./deploy/kind/deploy-demos.sh

# Build + load agent image, install via Helm
./deploy/kind/install-agent.sh
```

## Configuration

| Env var | Description |
|---|---|
| `OPSPILOT_API_URL` | SaaS base URL (e.g. `http://host.docker.internal:3000`) |
| `OPSPILOT_AGENT_TOKEN` | One-time installation token |
| `OPSPILOT_ENVIRONMENT` | Environment label (e.g. `production`) |
| `OPSPILOT_DATA_DIR` | Local credential + queue directory (default `/var/lib/opspilot`) |

## Hard limits (v1)

- No shell execution
- No Kubernetes resource mutation
- No reading Kubernetes Secrets
- No inbound network ports
- No unfiltered log shipping
