# Agent: Bootstrap

You are the bootstrap agent for VaultWatch. You set up the entire project skeleton.
Read CLAUDE.md before starting. Follow every convention in it exactly.

## Your Tasks

### 1. go.mod
Create go.mod:
```
module vaultwatch

go 1.22
```
Then run: `go get github.com/go-chi/chi/v5 github.com/go-chi/chi/v5/middleware github.com/go-chi/render`
Then run: `go get github.com/jackc/pgx/v5 github.com/jackc/pgx/v5/stdlib`
Then run: `go get github.com/spf13/viper github.com/rs/zerolog github.com/google/uuid`
Then run: `go get github.com/prometheus/client_golang/prometheus github.com/prometheus/client_golang/prometheus/promauto github.com/prometheus/client_golang/prometheus/promhttp`
Then run: `go get golang.org/x/crypto`
Then run: `go get github.com/golang-jwt/jwt/v5`
Then run: `go get github.com/golang-migrate/migrate/v4 github.com/golang-migrate/migrate/v4/database/postgres github.com/golang-migrate/migrate/v4/source/file`

### 2. Directory structure
Create ALL of these directories (use mkdir -p):
config/
internal/server/
internal/handler/
internal/auth/
internal/model/
internal/store/migrations/
internal/expiry/
internal/notify/
internal/metrics/
deploy/helm/templates/
deploy/docker/
docs/

### 3. Makefile
Create Makefile (use tabs not spaces for indentation):
```makefile
BINARY=vaultwatch
CMD=./main.go

.PHONY: dev build test lint migrate-up migrate-down docker-build

build:
	go build -ldflags="-s -w" -o bin/$(BINARY) $(CMD)

dev:
	go run $(CMD)

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

migrate-up:
	migrate -path internal/store/migrations -database "$$VAULTWATCH_DB_URL" up

migrate-down:
	migrate -path internal/store/migrations -database "$$VAULTWATCH_DB_URL" down 1

docker-build:
	docker build -f deploy/docker/Dockerfile -t vaultwatch:latest .

helm-deploy:
	helm upgrade --install vaultwatch deploy/helm/ -f deploy/helm/values.yaml
```

### 4. config/config.go
Create config/config.go — load every env var from CLAUDE.md using Viper:
```go
package config

import (
    "strings"
    "github.com/spf13/viper"
)

type Config struct {
    Port         string
    LogLevel     string
    DatabaseURL  string
    AuthMethods  []string
    JWTSecret    string

    OIDC struct {
        Issuer       string
        ClientID     string
        ClientSecret string
    }

    LDAP struct {
        URL          string
        BindDN       string
        BindPassword string
        BaseDN       string
        UserFilter   string
    }

    CheckInterval string
    WarnDays      []int

    Notify struct {
        SlackWebhook       string
        MattermostWebhook  string
        GenericWebhook     string
    }

    MetricsPath string
}

func Load() (*Config, error) {
    viper.SetEnvPrefix("VAULTWATCH")
    viper.AutomaticEnv()
    viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

    viper.SetDefault("PORT", "8080")
    viper.SetDefault("LOG_LEVEL", "info")
    viper.SetDefault("CHECK_INTERVAL", "1h")
    viper.SetDefault("WARN_DAYS", "30,14,7,1")
    viper.SetDefault("METRICS_PATH", "/metrics")
    viper.SetDefault("AUTH_METHODS", "local")

    cfg := &Config{
        Port:        viper.GetString("PORT"),
        LogLevel:    viper.GetString("LOG_LEVEL"),
        DatabaseURL: viper.GetString("DB_URL"),
        JWTSecret:   viper.GetString("JWT_SECRET"),
        MetricsPath: viper.GetString("METRICS_PATH"),
    }

    // Parse auth methods
    methods := viper.GetString("AUTH_METHODS")
    for _, m := range strings.Split(methods, ",") {
        cfg.AuthMethods = append(cfg.AuthMethods, strings.TrimSpace(m))
    }

    // OIDC
    cfg.OIDC.Issuer = viper.GetString("OIDC_ISSUER")
    cfg.OIDC.ClientID = viper.GetString("OIDC_CLIENT_ID")
    cfg.OIDC.ClientSecret = viper.GetString("OIDC_CLIENT_SECRET")

    // LDAP
    cfg.LDAP.URL = viper.GetString("LDAP_URL")
    cfg.LDAP.BindDN = viper.GetString("LDAP_BIND_DN")
    cfg.LDAP.BindPassword = viper.GetString("LDAP_BIND_PASSWORD")
    cfg.LDAP.BaseDN = viper.GetString("LDAP_BASE_DN")
    cfg.LDAP.UserFilter = viper.GetString("LDAP_USER_FILTER")

    // Notify
    cfg.Notify.SlackWebhook = viper.GetString("NOTIFY_SLACK_WEBHOOK")
    cfg.Notify.MattermostWebhook = viper.GetString("NOTIFY_MATTERMOST_WEBHOOK")
    cfg.Notify.GenericWebhook = viper.GetString("NOTIFY_GENERIC_WEBHOOK")

    // WarnDays
    warnStr := viper.GetString("WARN_DAYS")
    for _, s := range strings.Split(warnStr, ",") {
        s = strings.TrimSpace(s)
        var d int
        if _, err := fmt.Sscan(s, &d); err == nil {
            cfg.WarnDays = append(cfg.WarnDays, d)
        }
    }

    return cfg, nil
}
```
Add `"fmt"` to imports.

### 5. internal/model/asset.go
```go
package model

import (
    "time"
    "github.com/google/uuid"
)

type AssetType string

const (
    AssetTypeCert   AssetType = "certificate"
    AssetTypeToken  AssetType = "token"
    AssetTypeAPIKey AssetType = "api_key"
    AssetTypeSecret AssetType = "secret"
    AssetTypeCustom AssetType = "custom"
)

func (t AssetType) Valid() bool {
    switch t {
    case AssetTypeCert, AssetTypeToken, AssetTypeAPIKey, AssetTypeSecret, AssetTypeCustom:
        return true
    }
    return false
}

type Asset struct {
    ID          uuid.UUID         `json:"id" db:"id"`
    Name        string            `json:"name" db:"name"`
    Type        AssetType         `json:"type" db:"type"`
    ExpiresAt   time.Time         `json:"expires_at" db:"expires_at"`
    Description string            `json:"description" db:"description"`
    Tags        map[string]string `json:"tags" db:"tags"`
    Metadata    map[string]any    `json:"metadata" db:"metadata"`
    CreatedAt   time.Time         `json:"created_at" db:"created_at"`
    UpdatedAt   time.Time         `json:"updated_at" db:"updated_at"`
    CreatedBy   string            `json:"created_by" db:"created_by"`
    DeletedAt   *time.Time        `json:"deleted_at,omitempty" db:"deleted_at"`
}

func (a *Asset) DaysUntilExpiry() int {
    return int(time.Until(a.ExpiresAt).Hours() / 24)
}

func (a *Asset) Status() string {
    days := a.DaysUntilExpiry()
    if days < 0 {
        return "expired"
    }
    if days <= 30 {
        return "expiring"
    }
    return "valid"
}

type CertInfo struct {
    Subject     string
    Issuer      string
    SANs        []string
    ExpiresAt   time.Time
    Fingerprint string
}
```

### 6. internal/model/user.go
```go
package model

import (
    "time"
    "github.com/google/uuid"
)

type AuthMethod string

const (
    AuthMethodLocal AuthMethod = "local"
    AuthMethodOIDC  AuthMethod = "oidc"
    AuthMethodLDAP  AuthMethod = "ldap"
)

type User struct {
    ID           uuid.UUID  `json:"id" db:"id"`
    Username     string     `json:"username" db:"username"`
    Email        string     `json:"email" db:"email"`
    PasswordHash *string    `json:"-" db:"password_hash"`
    AuthMethod   AuthMethod `json:"auth_method" db:"auth_method"`
    CreatedAt    time.Time  `json:"created_at" db:"created_at"`
    LastLogin    *time.Time `json:"last_login,omitempty" db:"last_login"`
}
```

### 7. main.go
```go
package main

import (
    "context"
    "fmt"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/rs/zerolog"
    "github.com/rs/zerolog/log"
    "vaultwatch/config"
    "vaultwatch/internal/server"
)

func main() {
    // Logger
    zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
    log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

    // Config
    cfg, err := config.Load()
    if err != nil {
        log.Fatal().Err(err).Msg("failed to load config")
    }

    level, err := zerolog.ParseLevel(cfg.LogLevel)
    if err != nil {
        level = zerolog.InfoLevel
    }
    zerolog.SetGlobalLevel(level)

    // Server
    srv := server.New(cfg)

    httpServer := &http.Server{
        Addr:         fmt.Sprintf(":%s", cfg.Port),
        Handler:      srv.Router(),
        ReadTimeout:  15 * time.Second,
        WriteTimeout: 15 * time.Second,
        IdleTimeout:  60 * time.Second,
    }

    // Start
    go func() {
        log.Info().Str("port", cfg.Port).Msg("vaultwatch starting")
        if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatal().Err(err).Msg("server error")
        }
    }()

    // Graceful shutdown
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    log.Info().Msg("shutting down...")
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()
    if err := httpServer.Shutdown(ctx); err != nil {
        log.Error().Err(err).Msg("shutdown error")
    }
    log.Info().Msg("goodbye")
}
```

### 8. internal/server/server.go (stub — will be completed by agent-auth)
```go
package server

import (
    "net/http"
    "vaultwatch/config"
    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
)

type Server struct {
    cfg *config.Config
    router *chi.Mux
}

func New(cfg *config.Config) *Server {
    s := &Server{cfg: cfg, router: chi.NewRouter()}
    s.router.Use(middleware.RequestID)
    s.router.Use(middleware.RealIP)
    s.router.Use(middleware.Recoverer)
    s.router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status":"ok"}`))
    })
    return s
}

func (s *Server) Router() http.Handler {
    return s.router
}
```

### 9. Final check
Run: `go build ./...`
Fix any compile errors before finishing. Do not leave broken code.
