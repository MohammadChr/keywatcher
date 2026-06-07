# Agent: Grafana Dashboard & Prometheus Config

You own the observability layer. Read CLAUDE.md first.

## Task 1 — Prometheus scrape config (docs/prometheus.yml)
```yaml
global:
  scrape_interval: 30s
  evaluation_interval: 30s

scrape_configs:
  - job_name: vaultwatch
    static_configs:
      - targets: ["vaultwatch:8080"]
    # For k8s, use kubernetes_sd_configs instead:
    # kubernetes_sd_configs:
    #   - role: pod
    # relabel_configs:
    #   - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
    #     action: keep
    #     regex: "true"
    #   - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_path]
    #     action: replace
    #     target_label: __metrics_path__
    #   - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_port, __meta_kubernetes_pod_ip]
    #     action: replace
    #     regex: (\d+);(.*)
    #     replacement: $2:$1
    #     target_label: __address__
```

## Task 2 — Grafana Dashboard (docs/grafana-dashboard.json)

Create a complete Grafana dashboard JSON model with these panels. 
Use datasource variable $datasource. Dashboard UID: vaultwatch-main.
Set refresh to 5m, default time range last 7d.

Panel layout (use gridPos):
- Row 1 (y=0, h=4): 4 stat panels side by side
- Row 2 (y=4, h=8): table (left, w=12) + gauge (right, w=12)  
- Row 3 (y=12, h=8): time series (left, w=12) + stat (right, w=12)

Panel 1 — "Total Assets" stat (x=0,w=6):
  query: sum(vaultwatch_assets_total)
  color: blue, unit: short, no graph

Panel 2 — "Expiring Soon" stat (x=6,w=6):
  query: sum(vaultwatch_assets_total{status="expiring"})
  color: orange/yellow thresholds: 0=green, 1=yellow, 5=orange

Panel 3 — "Expired" stat (x=12,w=6):
  query: sum(vaultwatch_assets_total{status="expired"})
  color: red thresholds: 0=green, 1=red

Panel 4 — "Last Check Duration" stat (x=18,w=6):
  query: vaultwatch_check_duration_seconds_sum / vaultwatch_check_duration_seconds_count
  unit: s, color: green

Panel 5 — "Assets Expiring in 30 Days" table (y=4, x=0, w=12):
  query: vaultwatch_asset_expiry_days > 0 < 30
  columns: asset name (label "name"), type (label "type"), env (label "env"), days (value)
  sort by value ascending
  color cells: 0-7 = red, 7-14 = orange, 14-30 = yellow

Panel 6 — "Days Until Expiry" gauge (y=4, x=12, w=12):
  query: topk(10, vaultwatch_asset_expiry_days)
  unit: d (days), min=0, max=365
  thresholds: 0=red, 7=orange, 30=yellow, 90=green

Panel 7 — "Notifications Sent" time series (y=12, x=0, w=12):
  query: rate(vaultwatch_notifications_sent_total[5m])
  legend: {{channel}}
  unit: ops

Panel 8 — "Assets by Type" bar gauge (y=12, x=12, w=12):
  query: sum by(type) (vaultwatch_assets_total)
  legend: {{type}}
  orientation: horizontal

Generate the complete valid JSON for this dashboard.
The JSON must be importable directly into Grafana via Dashboards → Import → Upload JSON.
Include the full "__inputs" and "__requires" sections at the top for portability.

## Task 3 — Docker Compose for local dev (docker-compose.dev.yml in root)
```yaml
version: "3.9"
services:
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: vaultwatch
      POSTGRES_USER: vaultwatch
      POSTGRES_PASSWORD: devpassword
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data

  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./docs/prometheus.yml:/etc/prometheus/prometheus.yml:ro
    ports:
      - "9090:9090"
    command:
      - "--config.file=/etc/prometheus/prometheus.yml"
      - "--storage.tsdb.retention.time=7d"

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      GF_SECURITY_ADMIN_PASSWORD: admin
      GF_USERS_ALLOW_SIGN_UP: "false"
    volumes:
      - grafdata:/var/lib/grafana

volumes:
  pgdata:
  grafdata:
```

## Task 4 — README.md
Create a README.md in the project root:

# VaultWatch

Lightweight DevOps asset expiry manager. Tracks certificates, tokens, API keys,
and any time-limited secrets. Sends alerts via Slack, Mattermost, or webhooks.
Exposes Prometheus metrics. Deployable on Kubernetes.

## Quick Start (local)

```bash
# Start dependencies
docker compose -f docker-compose.dev.yml up -d

# Set env
export VAULTWATCH_DB_URL="postgres://vaultwatch:devpassword@localhost:5432/vaultwatch?sslmode=disable"
export VAULTWATCH_JWT_SECRET="changeme-dev-only"
export VAULTWATCH_AUTH_METHODS="local"

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
kubectl create secret generic vaultwatch-db --from-literal=database-url="postgres://..."
kubectl create secret generic vaultwatch-jwt --from-literal=jwt-secret="your-secret"

# Deploy
helm upgrade --install vaultwatch deploy/helm/ \
  --set image.repository=your-registry/vaultwatch \
  --set image.tag=latest
```

## Final check
Verify docs/prometheus.yml is valid YAML.
Verify docs/grafana-dashboard.json is valid JSON (use python3 -c "import json,sys; json.load(sys.stdin)" < docs/grafana-dashboard.json).
Verify docker-compose.dev.yml is valid YAML.
