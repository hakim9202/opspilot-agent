# Docker build

```bash
docker build -t opspilot-agent:0.1.0 ../..
```

The image is outbound-only (no EXPOSE). Load into kind via `../kind/install-agent.sh`.
