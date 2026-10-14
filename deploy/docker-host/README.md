# Docker-host agent (VM with Docker)

Run the same OpsPilot agent binary on a Linux VM with Docker Engine. The agent watches
allowlisted containers and sends structured incidents to the SaaS API.

## Prerequisites

- Docker Engine on the host
- Outbound HTTPS to OpsPilot API
- Install token from OpsPilot

## Quick test (Mac Docker Desktop)

```bash
cd /path/to/opspilot-agent
go build -o opspilot-agent ./cmd/agent

export OPSPILOT_AGENT_MODE=docker-host
export OPSPILOT_API_URL=http://localhost:3000
export OPSPILOT_AGENT_TOKEN=install_lab_opspilot_mvp
export OPSPILOT_ENVIRONMENT=lab
export OPSPILOT_CLUSTER_NAME=docker-desktop
export OPSPILOT_DOCKER_WATCH=all
export OPSPILOT_DATA_DIR=/tmp/opspilot-docker

./opspilot-agent
```

Label a container to watch (production style):

```bash
docker run -d --name demo-api \
  --label opspilot.watch=true \
  --label opspilot.service=ticket-api \
  nginx:alpine
```

## Environment variables

| Variable | Description |
|----------|-------------|
| `OPSPILOT_AGENT_MODE` | `docker-host` |
| `OPSPILOT_DOCKER_HOST` | Default `unix:///var/run/docker.sock` |
| `OPSPILOT_DOCKER_WATCH` | `label:opspilot.watch=true` (default), `name:prefix`, or `all` |
| `OPSPILOT_SERVICE` | Default service name for incidents |
| `OPSPILOT_POLL_SECONDS` | Poll interval (default 30) |

## systemd install (Linux VM)

1. Copy binary to `/usr/local/bin/opspilot-agent`
2. Copy `deploy/docker-host/opspilot-agent.env.example` → `/etc/opspilot/agent.env`
3. Copy `deploy/docker-host/opspilot-agent.service` → `/etc/systemd/system/`
4. `sudo systemctl daemon-reload && sudo systemctl enable --now opspilot-agent`

## Detected incident types

- `ContainerUnhealthy` — Docker health check failing
- `ContainerRestartLoop` — high restart count
- `ContainerExited` — non-zero exit
- `ContainerOOMKilled` — OOM killed by kernel
