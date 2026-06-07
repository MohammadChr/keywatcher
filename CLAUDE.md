# VaultWatch — CLAUDE.md

## Project Overview
VaultWatch is a lightweight DevOps asset expiry manager built in Go.
It tracks certificates, tokens, API keys, and any time-limited secrets.
It exposes a Prometheus metrics endpoint, sends notifications via Slack/Mattermost/webhooks,
and supports OIDC, LDAP, and local authentication.

## Core Principles
- **Simplicity first** — no unnecessary abstractions. Flat packages over deep nesting.
- **Single binary** — the app compiles to one binary. No dynamic plugins.
- **12-factor app** — all config via environment variables or a config file, never hardcoded.
- **Fail loudly** — prefer explicit error returns over panics. Log errors with context.
- **Idiomatic Go** — follow effective Go, use stdlib where possible before adding deps.

## Tech Stack
| Layer | Choice | Reason |
|---|---|---|
| Language | Go 1.22+ | Single binary, low memory, native concurrency |
| Web framework | Chi router | Lightweight, stdlib-compatible, no magic |
| Database | PostgreSQL 15+ | JSONB for flexible metadata, pgx driver |
| Migrations | golang-migrate | SQL-first, version controlled |
| Auth | go-jose (OIDC), ldap.v3 (LDAP) | Battle-tested libs |
| Metrics | prometheus/client_golang | Native, zero overhead |
| Config | viper | Env vars + YAML, precedence chain |
| Logging | zerolog | Structured JSON logging, zero alloc |
| Container | Kubernetes (k8s) | Helm chart included |

## Project Structure
```
vaultwatch/
├── CLAUDE.md                  ← you are here
├── README.md
├── Makefile                   ← all dev commands live here
├── go.mod
├── go.sum
├── main.go                    ← entrypoint only, wires everything
├── config/
│   └── config.go              ← Viper config struct + loader
├── internal/
│   ├── server/
│   │   ├── server.go          ← HTTP server setup, middleware chain
│   │   └── routes.go          ← route registration only
│   ├── handler/
│   │   ├── asset.go           ← CRUD for assets (certs, tokens, etc)
│   │   ├── auth.go            ← login/logout/session handlers
│   │   └── metrics.go         ← Prometheus handler (just exposes /metrics)
│   ├── auth/
│   │   ├── oidc.go            ← OIDC provider integration
│   │   ├── ldap.go            ← LDAP bind + search
│   │   └── local.go           ← bcrypt local accounts
│   ├── model/
│   │   ├── asset.go           ← Asset struct + types enum
│   │   └── user.go            ← User struct
│   ├── store/
│   │   ├── store.go           ← DB interface (for mocking)
│   │   ├── postgres.go        ← pgx pool + query implementations
│   │   └── migrations/        ← SQL migration files (001_init.up.sql etc)
│   ├── expiry/
│   │   └── checker.go         ← Background goroutine: checks expiry, fires alerts
│   ├── notify/
│   │   ├── notifier.go        ← Notifier interface
│   │   ├── slack.go           ← Slack webhook sender
│   │   ├── mattermost.go      ← Mattermost webhook sender
│   │   └── webhook.go         ← Generic webhook sender
│   └── metrics/
│       └── metrics.go         ← Prometheus gauge/counter definitions
├── deploy/
│   ├── helm/                  ← Helm chart for Kubernetes
│   │   ├── Chart.yaml
│   │   ├── values.yaml
│   │   └── templates/
│   └── docker/
│       └── Dockerfile
└── docs/
    └── api.md                 ← API reference
```

## Commands (always use Makefile)
```bash
make dev          # run locally with hot reload (air)
make build        # compile binary to ./bin/vaultwatch
make test         # run all tests with race detector
make lint         # golangci-lint
make migrate-up   # apply DB migrations
make migrate-down # rollback last migration
make docker-build # build Docker image
make helm-deploy  # deploy to current k8s context
```

## Environment Variables
All config is via env vars (or config.yaml). Never hardcode secrets.
```
# Server
VAULTWATCH_PORT=8080
VAULTWATCH_LOG_LEVEL=info          # debug | info | warn | error

# Database
VAULTWATCH_DB_URL=postgres://user:pass@host:5432/vaultwatch?sslmode=require

# Auth — set the method(s) to enable (comma-separated: local,oidc,ldap)
VAULTWATCH_AUTH_METHODS=local,oidc

# OIDC
VAULTWATCH_OIDC_ISSUER=https://accounts.example.com
VAULTWATCH_OIDC_CLIENT_ID=vaultwatch
VAULTWATCH_OIDC_CLIENT_SECRET=secret

# LDAP
VAULTWATCH_LDAP_URL=ldap://ldap.example.com:389
VAULTWATCH_LDAP_BIND_DN=cn=vaultwatch,dc=example,dc=com
VAULTWATCH_LDAP_BIND_PASSWORD=secret
VAULTWATCH_LDAP_BASE_DN=ou=users,dc=example,dc=com
VAULTWATCH_LDAP_USER_FILTER=(uid=%s)

# Expiry checker
VAULTWATCH_CHECK_INTERVAL=1h       # how often to scan for expiring assets
VAULTWATCH_WARN_DAYS=30,14,7,1    # alert at these thresholds (days before expiry)

# Notifications (at least one required for alerts)
VAULTWATCH_NOTIFY_SLACK_WEBHOOK=https://hooks.slack.com/services/...
VAULTWATCH_NOTIFY_MATTERMOST_WEBHOOK=https://mattermost.example.com/hooks/...
VAULTWATCH_NOTIFY_GENERIC_WEBHOOK=https://my.webhook.example.com/alert

# Prometheus
VAULTWATCH_METRICS_PATH=/metrics   # default, change if needed
```

## Data Model — Asset
An Asset is anything with an expiry date. The `type` field drives display + parsing logic.
```go
type AssetType string
const (
    AssetTypeCert    AssetType = "certificate"
    AssetTypeToken   AssetType = "token"
    AssetTypeAPIKey  AssetType = "api_key"
    AssetTypeSecret  AssetType = "secret"      // generic
    AssetTypeCustom  AssetType = "custom"
)

type Asset struct {
    ID          uuid.UUID         // primary key
    Name        string            // human label e.g. "prod-api-cert"
    Type        AssetType
    ExpiresAt   time.Time         // THE critical field
    Description string
    Tags        map[string]string // e.g. {"env":"prod","team":"platform"}
    Metadata    map[string]any    // type-specific data (cert SANs, token scopes…)
    CreatedAt   time.Time
    UpdatedAt   time.Time
    CreatedBy   string            // user ID
}
```

## Prometheus Metrics to Expose
Define these in `internal/metrics/metrics.go`:
```
keywatcher_asset_expiry_days{name, type, env}   Gauge   Days until expiry (negative = already expired)
keywatcher_assets_total{type, status}           Gauge   Count by type and status (valid/expiring/expired)
keywatcher_notifications_sent_total{channel}    Counter Notifications sent per channel
keywatcher_check_duration_seconds               Histogram Time taken for expiry check sweep
```

## API Endpoints
```
POST   /api/v1/auth/login          Login (any method)
POST   /api/v1/auth/logout
GET    /api/v1/auth/oidc/callback  OIDC redirect target

GET    /api/v1/assets              List assets (filter: ?type=cert&status=expiring)
POST   /api/v1/assets              Create asset
GET    /api/v1/assets/:id          Get one asset
PUT    /api/v1/assets/:id          Update asset
DELETE /api/v1/assets/:id          Delete asset

GET    /metrics                    Prometheus scrape endpoint (no auth)
GET    /healthz                    Liveness probe (no auth)
GET    /readyz                     Readiness probe (no auth)
```

## Auth Middleware Rules
- `/metrics`, `/healthz`, `/readyz` — always public, no auth
- All `/api/v1/*` routes — require valid session token in Authorization header or cookie
- Session tokens are JWT signed with a server secret (HS256 for local/LDAP, RS256 for OIDC)
- Sessions expire after 8h by default (configurable)

## Coding Conventions
- All errors must be wrapped: `fmt.Errorf("store.GetAsset: %w", err)`
- No `panic()` in production code paths — only in `main()` during startup validation
- All handlers follow signature: `func (h *Handler) MethodName(w http.ResponseWriter, r *http.Request)`
- Use `render.JSON(w, r, payload)` (chi/render) for all JSON responses
- DB queries use named parameters (pgx v5), never string interpolation
- Migrations are numbered: `001_init.up.sql`, `001_init.down.sql`, always paired
- Tests go in `_test.go` files next to the code they test
- Table-driven tests for all pure functions
- Integration tests (DB) are in `store/postgres_test.go`, skipped if no DB env

## Notification Format
All notifiers send the same structured payload — the `notify.Message` struct:
```go
type Message struct {
    Title    string
    AssetName string
    AssetType string
    DaysLeft  int
    ExpiresAt time.Time
    Tags      map[string]string
    Severity  string  // "warning" | "critical" | "expired"
}
```
Each notifier formats this into its own wire format (Slack Block Kit, Mattermost payload, etc.)

## Kubernetes Deployment Notes
- App runs as a single `Deployment` with 2 replicas minimum
- Prometheus scraping via pod annotations: `prometheus.io/scrape: "true"`, `prometheus.io/port: "8080"`, `prometheus.io/path: "/metrics"`
- DB credentials via Kubernetes Secret mounted as env vars
- Liveness probe: `GET /healthz` every 15s
- Readiness probe: `GET /readyz` every 10s (checks DB connectivity)
- Resource requests: `cpu: 50m, memory: 64Mi` / limits: `cpu: 200m, memory: 128Mi`
- HorizontalPodAutoscaler on CPU > 70%

## What Claude Should NOT Do
- Do not add ORMs (no GORM, no ent). Use raw SQL with pgx.
- Do not add a frontend framework. UI is out of scope — this is an API + metrics service.
- Do not use global variables for anything except the Prometheus registry.
- Do not add a cache layer unless explicitly asked.
- Do not change the folder structure without asking first.
- Do not upgrade Go module major versions without asking.

## Definition of Done (per feature)
1. Handler implemented and registered in routes.go
2. Store method implemented with SQL migration if schema changes
3. Unit tests written (table-driven)
4. Prometheus metric updated if relevant
5. CLAUDE.md updated if new env var or endpoint added
6. `make test` and `make lint` pass
