# Agent: First-Run Setup & Docker Compose Fix

You are applying two critical patches to VaultWatch.
Read CLAUDE.md first. Do not change anything not mentioned here.

---

## PATCH 1 — First-Run Setup (no pre-created user needed)

### How it works
On first startup, if the `users` table is empty, VaultWatch enters "setup mode".
Setup mode serves a one-time setup page at `/setup`.
All other routes redirect to `/setup` until setup is complete.
After the admin user is created, setup mode is gone forever.

### Step 1 — Add setup_completed flag to DB

Create migration 003_setup.up.sql in internal/store/migrations/:
```sql
CREATE TABLE IF NOT EXISTS app_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO app_settings (key, value) VALUES ('setup_completed', 'false')
ON CONFLICT DO NOTHING;
```

Create 003_setup.down.sql:
```sql
DELETE FROM app_settings WHERE key = 'setup_completed';
DROP TABLE IF EXISTS app_settings;
```

### Step 2 — Add store methods

Add to internal/store/store.go interface:
```go
IsSetupCompleted(ctx context.Context) (bool, error)
CompleteSetup(ctx context.Context) error
```

Add to internal/store/postgres.go:
```go
func (s *PostgresStore) IsSetupCompleted(ctx context.Context) (bool, error) {
    var value string
    err := s.pool.QueryRow(ctx,
        "SELECT value FROM app_settings WHERE key = 'setup_completed'").Scan(&value)
    if err != nil {
        return false, fmt.Errorf("store.IsSetupCompleted: %w", err)
    }
    return value == "true", nil
}

func (s *PostgresStore) CompleteSetup(ctx context.Context) error {
    _, err := s.pool.Exec(ctx,
        "UPDATE app_settings SET value = 'true' WHERE key = 'setup_completed'")
    return err
}
```

### Step 3 — Setup handler (internal/handler/setup.go)

```go
package handler

import (
    "encoding/json"
    "net/http"
    "vaultwatch/internal/auth"
    "vaultwatch/internal/store"
    "vaultwatch/config"
    "time"
)

type SetupHandler struct {
    store store.Store
    cfg   *config.Config
}

func NewSetupHandler(s store.Store, cfg *config.Config) *SetupHandler {
    return &SetupHandler{store: s, cfg: cfg}
}

// GET /setup/status — called by UI to check if setup is needed
func (h *SetupHandler) Status(w http.ResponseWriter, r *http.Request) {
    done, err := h.store.IsSetupCompleted(r.Context())
    if err != nil {
        writeError(w, http.StatusInternalServerError, "cannot check setup status")
        return
    }
    writeJSON(w, http.StatusOK, map[string]bool{"setup_completed": done})
}

// POST /setup — create the first admin user
func (h *SetupHandler) Complete(w http.ResponseWriter, r *http.Request) {
    // Block if already set up
    done, _ := h.store.IsSetupCompleted(r.Context())
    if done {
        writeError(w, http.StatusForbidden, "setup already completed")
        return
    }

    var req struct {
        Username string `json:"username"`
        Email    string `json:"email"`
        Password string `json:"password"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request body")
        return
    }
    if len(req.Password) < 8 {
        writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
        return
    }

    if err := auth.CreateLocalUser(r.Context(), h.store, req.Username, req.Email, req.Password); err != nil {
        writeError(w, http.StatusBadRequest, err.Error())
        return
    }

    if err := h.store.CompleteSetup(r.Context()); err != nil {
        writeError(w, http.StatusInternalServerError, "failed to complete setup")
        return
    }

    // Issue token so user is immediately logged in after setup
    user, _ := h.store.GetUserByUsername(r.Context(), req.Username)
    token, _ := auth.IssueToken(user, h.cfg.JWTSecret, 8*time.Hour)

    writeJSON(w, http.StatusOK, map[string]string{
        "status": "setup completed",
        "token":  token,
    })
}
```

### Step 4 — Setup middleware

Add to internal/auth/session.go:
```go
// RequireSetupComplete redirects to setup if setup is not done yet.
// Apply this to ALL routes except /setup/* and /healthz and /readyz and /metrics
func RequireSetupComplete(s store.Store) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Skip check for setup routes themselves
            if strings.HasPrefix(r.URL.Path, "/setup") ||
               r.URL.Path == "/healthz" ||
               r.URL.Path == "/readyz" ||
               r.URL.Path == "/metrics" {
                next.ServeHTTP(w, r)
                return
            }
            done, err := s.IsSetupCompleted(r.Context())
            if err != nil || !done {
                // API requests get JSON, browser requests get redirect
                if strings.HasPrefix(r.URL.Path, "/api/") {
                    w.Header().Set("Content-Type", "application/json")
                    w.WriteHeader(http.StatusServiceUnavailable)
                    w.Write([]byte(`{"error":"setup_required","redirect":"/setup"}`))
                    return
                }
                http.Redirect(w, r, "/setup", http.StatusFound)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

Add import: `"vaultwatch/internal/store"` and `"strings"` to that file.

### Step 5 — Register setup routes in server

In internal/server/server.go, add BEFORE the RequireSetupComplete middleware:
```go
// Setup routes — always public, no auth, no setup check
s.router.Get("/setup/status", setupHandler.Status)
s.router.Post("/setup", setupHandler.Complete)
```

Apply RequireSetupComplete middleware to the router globally:
```go
s.router.Use(RequireSetupComplete(store))
```
This must be added AFTER the public health/metrics routes but BEFORE API routes.

### Step 6 — Update the HTML UI

In internal/server/static/index.html, add setup screen logic:

On page load, BEFORE showing login or app, call GET /setup/status.
If setup_completed is false: show setup screen.
If setup_completed is true: show login or app as normal.

Setup screen HTML (centered card, same style as login):
```
Title: "Welcome to VaultWatch"
Subtitle: "Create your admin account to get started"
Fields:
  - Username (text, required)
  - Email (email, required)  
  - Password (password, required, min 8 chars)
  - Confirm Password (password, required, must match)
Button: "Create Admin Account"
On success: store token, show main app
On error: show error message
```

Add this JS function:
```javascript
async function checkSetup() {
    const res = await fetch('/setup/status')
    const data = await res.json()
    if (!data.data.setup_completed) {
        showSetup()
    } else if (token) {
        showApp()
    } else {
        showLogin()
    }
}

async function submitSetup() {
    const username = document.getElementById('setup-username').value
    const email = document.getElementById('setup-email').value
    const password = document.getElementById('setup-password').value
    const confirm = document.getElementById('setup-confirm').value
    
    if (password !== confirm) {
        document.getElementById('setup-error').textContent = 'Passwords do not match'
        return
    }
    
    const res = await api('POST', '/setup', { username, email, password })
    // /setup is not under /api/v1, call fetch directly:
    const r = await fetch('/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, email, password })
    })
    const data = await r.json()
    if (data.error) {
        document.getElementById('setup-error').textContent = data.error
        return
    }
    token = data.data.token
    localStorage.setItem('vw_token', token)
    showApp()
}

// Change init from:
//   if (token) showApp() else showLogin()
// To:
checkSetup()
```

---

## PATCH 2 — Docker Compose builds the app locally

Replace the existing docker-compose.dev.yml in the project root with this:

```yaml
version: "3.9"

services:
  # ── App ──────────────────────────────────────────────────────
  app:
    build:
      context: .
      dockerfile: deploy/docker/Dockerfile
    ports:
      - "8080:8080"
    environment:
      VAULTWATCH_PORT: "8080"
      VAULTWATCH_LOG_LEVEL: "debug"
      VAULTWATCH_DB_URL: "postgres://vaultwatch:devpassword@postgres:5432/vaultwatch?sslmode=disable"
      VAULTWATCH_JWT_SECRET: "dev-secret-change-in-production"
      VAULTWATCH_AUTH_METHODS: "local"
      VAULTWATCH_CHECK_INTERVAL: "1m"        # faster for dev
      VAULTWATCH_WARN_DAYS: "30,14,7,1"
      # Uncomment to test notifications locally:
      # VAULTWATCH_NOTIFY_SLACK_WEBHOOK: "https://hooks.slack.com/..."
    depends_on:
      postgres:
        condition: service_healthy
    restart: unless-stopped
    # Mount source for faster iteration — rebuild image to pick up Go changes
    # For live reload, use the dev-hot target below instead

  # ── Database ─────────────────────────────────────────────────
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: vaultwatch
      POSTGRES_USER: vaultwatch
      POSTGRES_PASSWORD: devpassword
    ports:
      - "5432:5432"           # expose so you can connect with psql or DBeaver
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U vaultwatch"]
      interval: 5s
      timeout: 5s
      retries: 5

  # ── Prometheus ───────────────────────────────────────────────
  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./docs/prometheus.yml:/etc/prometheus/prometheus.yml:ro
    ports:
      - "9090:9090"
    command:
      - "--config.file=/etc/prometheus/prometheus.yml"
      - "--storage.tsdb.retention.time=7d"

  # ── Grafana ──────────────────────────────────────────────────
  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      GF_SECURITY_ADMIN_PASSWORD: admin
      GF_USERS_ALLOW_SIGN_UP: "false"
    volumes:
      - grafdata:/var/lib/grafana
    depends_on:
      - prometheus

volumes:
  pgdata:
  grafdata:
```

Also update docs/prometheus.yml so Prometheus can reach the app container by service name:
```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: vaultwatch
    static_configs:
      - targets: ["app:8080"]
```

Also add these Makefile targets:
```makefile
# Run full stack locally with Docker Compose (builds the app image)
compose-up:
	docker compose -f docker-compose.dev.yml up --build

# Run in background
compose-up-d:
	docker compose -f docker-compose.dev.yml up --build -d

# Stop everything
compose-down:
	docker compose -f docker-compose.dev.yml down

# Tail logs from the app only
compose-logs:
	docker compose -f docker-compose.dev.yml logs -f app

# Rebuild and restart only the app container (DB keeps running)
compose-restart:
	docker compose -f docker-compose.dev.yml up --build -d app
```

---

## Final check
Run `go build ./...` — fix any errors.
Verify docker-compose.dev.yml is valid: `docker compose -f docker-compose.dev.yml config`
