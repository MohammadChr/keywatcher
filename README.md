# VaultWatch

Lightweight DevOps asset expiry manager. Tracks certificates, tokens, API keys,
and any time-limited secrets. Sends alerts via Slack, Mattermost, or webhooks.
Exposes Prometheus metrics. Deployable on Kubernetes.

## Quick Start (local)

```bash
# Start dependencies
docker compose -f docker-compose.dev.yml up -d

# Set env
export KEYWATCHER_DB_URL="postgres://keywatcher:devpassword@localhost:5432/keywatcher?sslmode=disable"
export KEYWATCHER_JWT_SECRET="changeme-dev-only"
export KEYWATCHER_AUTH_METHODS="local"

# Run migrations
make migrate-up

# Start app
make dev
```

## API Quick Reference

| Method | Path | Description |
|---|---|---|
| POST | /api/v1/auth/login | Login (returns JWT) |
| GET  | /api/v1/assets | List assets |
| POST | /api/v1/assets | Create asset |
| GET  | /api/v1/assets/:id | Get asset |
| PUT  | /api/v1/assets/:id | Update asset |
| DELETE | /api/v1/assets/:id | Delete asset |
| GET  | /metrics | Prometheus scrape |
| GET  | /healthz | Liveness probe |
| GET  | /readyz  | Readiness probe |

## Grafana Dashboard

Import `docs/grafana-dashboard.json` into Grafana → Dashboards → Import.
Point to your Prometheus datasource.

## Deploy to Kubernetes

```bash
# Create secrets
kubectl create secret generic keywatcher-db --from-literal=database-url="postgres://..."
kubectl create secret generic keywatcher-jwt --from-literal=jwt-secret="your-secret"

# Deploy
helm upgrade --install keywatcher deploy/helm/ \
  --set image.repository=your-registry/keywatcher \
  --set image.tag=latest
```

## Final check

Verify docs/prometheus.yml is valid YAML.
Verify docs/grafana-dashboard.json is valid JSON (use python3 -c "import json,sys; json.load(sys.stdin)" < docs/grafana-dashboard.json).
Verify docker-compose.dev.yml is valid YAML.
