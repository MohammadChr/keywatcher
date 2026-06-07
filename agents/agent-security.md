# Agent: Security Hardening

Full security audit and hardening. Read CLAUDE.md first.
Fix every item below. Do not skip anything.

---

## AUDIT CATEGORY 1 — Information Leakage (Browser / JS Console)

### Fix 1.1 — Remove ALL console.log from production JS

Open internal/server/static/index.html.
Find and remove EVERY console.log, console.error, console.warn, console.info call.
Replace them all with a no-op logger that only fires in debug mode:

Add this at the very TOP of the <script> block:
```javascript
// Production logger — silent unless ?debug=1 in URL
var __debug = window.location.search.indexOf('debug=1') !== -1
var log = {
    info:  function() { if (__debug) console.log.apply(console, arguments) },
    error: function() { if (__debug) console.error.apply(console, arguments) },
    warn:  function() { if (__debug) console.warn.apply(console, arguments) }
}
```

Then replace every:
    console.log(...)   → log.info(...)
    console.error(...) → log.error(...)
    console.warn(...)  → log.warn(...)

### Fix 1.2 — Remove infrastructure details from error messages shown to users

In all JS error handlers, never show raw server error messages to users.
Replace patterns like:
    errEl.textContent = data.error || 'Failed'
With a sanitized version that hides internal details:
```javascript
function userError(msg) {
    // Never expose stack traces, SQL errors, internal paths
    if (!msg) return 'An error occurred. Please try again.'
    // Strip common internal details
    if (msg.indexOf('sql') !== -1 || msg.indexOf('pq:') !== -1 ||
        msg.indexOf('postgres') !== -1 || msg.indexOf('panic') !== -1) {
        return 'A server error occurred. Please contact your administrator.'
    }
    return msg
}
```

Use userError() everywhere an error is shown to the user in the UI.

### Fix 1.3 — Remove X-Powered-By and Server headers

In internal/server/server.go, add this middleware FIRST in the chain:
```go
s.router.Use(func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Remove headers that reveal infrastructure
        w.Header().Del("X-Powered-By")
        w.Header().Set("Server", "")
        next.ServeHTTP(w, r)
    })
})
```

### Fix 1.4 — Remove version info from /healthz and /readyz

Change /healthz from:
    {"status":"ok","version":"..."}
To just:
    {"status":"ok"}

Remove any Go version, build time, or binary name from health endpoints.

---

## AUDIT CATEGORY 2 — Security Headers

### Fix 2.1 — Add security headers to every response

Add this middleware in server.go AFTER the server header removal:
```go
s.router.Use(func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.Header().Set("X-Frame-Options", "DENY")
        w.Header().Set("X-XSS-Protection", "1; mode=block")
        w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
        w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
        w.Header().Set("Content-Security-Policy",
            "default-src 'self'; "+
            "script-src 'self' 'unsafe-inline'; "+
            "style-src 'self' 'unsafe-inline'; "+
            "img-src 'self' blob: data:; "+
            "connect-src 'self'; "+
            "frame-ancestors 'none'")
        next.ServeHTTP(w, r)
    })
})
```

---

## AUDIT CATEGORY 3 — Authentication & Session Security

### Fix 3.1 — JWT secret validation at startup

In main.go, after loading config, validate JWT secret:
```go
if cfg.JWTSecret == "" {
    log.Fatal().Msg("KEYWATCHER_JWT_SECRET is required and must not be empty")
}
if len(cfg.JWTSecret) < 32 {
    log.Fatal().Msg("KEYWATCHER_JWT_SECRET must be at least 32 characters")
}
```

### Fix 3.2 — Secure cookie flags

In internal/handler/auth.go, update ALL cookie settings:
```go
http.SetCookie(w, &http.Cookie{
    Name:     "session",
    Value:    token,
    HttpOnly: true,           // JS cannot access
    Secure:   true,           // HTTPS only (set false only in dev)
    SameSite: http.SameSiteStrictMode,  // Strict, not Lax
    Path:     "/",
    MaxAge:   8 * 3600,
})
```

Add a config flag KEYWATCHER_COOKIE_SECURE (default true, set false for local dev).
In docker-compose.dev.yml add: KEYWATCHER_COOKIE_SECURE=false

### Fix 3.3 — Rate limit login endpoint

Add rate limiting to POST /api/v1/auth/login to prevent brute force.
Use a simple in-memory rate limiter — max 5 attempts per IP per minute:

Create internal/auth/ratelimit.go:
```go
package auth

import (
    "net"
    "net/http"
    "sync"
    "time"
)

type rateLimiter struct {
    mu       sync.Mutex
    attempts map[string][]time.Time
    max      int
    window   time.Duration
}

func NewRateLimiter(max int, window time.Duration) *rateLimiter {
    rl := &rateLimiter{
        attempts: make(map[string][]time.Time),
        max:      max,
        window:   window,
    }
    // Clean up old entries every minute
    go func() {
        for range time.Tick(time.Minute) {
            rl.mu.Lock()
            cutoff := time.Now().Add(-window)
            for ip, times := range rl.attempts {
                var fresh []time.Time
                for _, t := range times {
                    if t.After(cutoff) {
                        fresh = append(fresh, t)
                    }
                }
                if len(fresh) == 0 {
                    delete(rl.attempts, ip)
                } else {
                    rl.attempts[ip] = fresh
                }
            }
            rl.mu.Unlock()
        }
    }()
    return rl
}

func (rl *rateLimiter) Allow(ip string) bool {
    rl.mu.Lock()
    defer rl.mu.Unlock()
    cutoff := time.Now().Add(-rl.window)
    var fresh []time.Time
    for _, t := range rl.attempts[ip] {
        if t.After(cutoff) {
            fresh = append(fresh, t)
        }
    }
    if len(fresh) >= rl.max {
        rl.attempts[ip] = fresh
        return false
    }
    rl.attempts[ip] = append(fresh, time.Now())
    return true
}

func (rl *rateLimiter) Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ip, _, err := net.SplitHostPort(r.RemoteAddr)
        if err != nil { ip = r.RemoteAddr }
        if !rl.Allow(ip) {
            w.Header().Set("Content-Type", "application/json")
            w.Header().Set("Retry-After", "60")
            w.WriteHeader(http.StatusTooManyRequests)
            w.Write([]byte(`{"error":"too many login attempts, please wait"}`))
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

In server.go, apply rate limiter only to the login route:
```go
loginLimiter := auth.NewRateLimiter(5, time.Minute)
s.router.With(loginLimiter.Middleware).Post("/api/v1/auth/login", s.authHandler.Login)
```

### Fix 3.4 — Constant-time password comparison

In internal/auth/local.go, make sure VerifyPassword uses bcrypt
which is already constant-time. But also make sure GetUserByUsername
always takes the same time whether user exists or not — use a dummy hash compare:
```go
func AuthenticateLocal(ctx context.Context, s store.Store, username, password string) (*model.User, error) {
    user, err := s.GetUserByUsername(ctx, username)
    if err != nil {
        // Still do a dummy bcrypt to prevent timing attacks
        bcrypt.CompareHashAndPassword([]byte("$2a$12$dummy.hash.to.prevent.timing.attack"), []byte(password))
        return nil, fmt.Errorf("invalid credentials")
    }
    if user == nil || user.PasswordHash == nil {
        bcrypt.CompareHashAndPassword([]byte("$2a$12$dummy.hash.to.prevent.timing.attack"), []byte(password))
        return nil, fmt.Errorf("invalid credentials")
    }
    if !VerifyPassword(*user.PasswordHash, password) {
        return nil, fmt.Errorf("invalid credentials")
    }
    return user, nil
}
```

### Fix 3.5 — Do not reveal whether username exists

In internal/handler/auth.go Login(), always return the same error message
regardless of whether the user was not found or password was wrong:
```go
writeError(w, http.StatusUnauthorized, "invalid username or password")
```
Never say "user not found" or "wrong password" — both reveal information.

---

## AUDIT CATEGORY 4 — Input Validation & Injection Prevention

### Fix 4.1 — Validate and sanitize all asset inputs in handler

In internal/handler/asset.go CreateAsset() and UpdateAsset():
```go
// Name: strip control characters, limit length
if len(req.Name) > 200 {
    writeError(w, http.StatusBadRequest, "name too long (max 200 characters)")
    return
}
// Description: limit length
if len(req.Description) > 2000 {
    writeError(w, http.StatusBadRequest, "description too long (max 2000 characters)")
    return
}
// Tags: limit count and key/value length
if len(a.Tags) > 20 {
    writeError(w, http.StatusBadRequest, "too many tags (max 20)")
    return
}
for k, v := range a.Tags {
    if len(k) > 50 || len(v) > 200 {
        writeError(w, http.StatusBadRequest, "tag key/value too long")
        return
    }
}
```

### Fix 4.2 — Validate expires_at is not absurdly far in the future

```go
maxExpiry := time.Now().AddDate(50, 0, 0) // 50 years max
if a.ExpiresAt.After(maxExpiry) {
    writeError(w, http.StatusBadRequest, "expiry date too far in the future")
    return
}
```

### Fix 4.3 — UUID validation in all handlers

Every handler that reads {id} from URL must validate it is a valid UUID
before hitting the database. This is already done with uuid.Parse() — verify
ALL handlers do this and return 400 (not 500) on invalid UUID.

---

## AUDIT CATEGORY 5 — Error Handling (No Stack Traces to Client)

### Fix 5.1 — Never return internal errors to client

In ALL handler files, replace any pattern like:
```go
writeError(w, http.StatusInternalServerError, err.Error())
```
With:
```go
log.Error().Err(err).Msg("operation failed")
writeError(w, http.StatusInternalServerError, "an internal error occurred")
```

The err.Error() must NEVER go into the HTTP response body.
Search for this pattern:
    grep -n "err.Error()" internal/handler/*.go

Fix every occurrence that sends err.Error() to the client.

### Fix 5.2 — Panic recovery with no details

Make sure the Recoverer middleware in server.go does not expose panic details.
Add a custom recoverer that logs but does not expose:
```go
s.router.Use(func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        defer func() {
            if rec := recover(); rec != nil {
                log.Error().Interface("panic", rec).Str("path", r.URL.Path).Msg("panic recovered")
                w.Header().Set("Content-Type", "application/json")
                w.WriteHeader(http.StatusInternalServerError)
                w.Write([]byte(`{"error":"internal server error"}`))
            }
        }()
        next.ServeHTTP(w, r)
    })
})
```

---

## AUDIT CATEGORY 6 — Metrics Endpoint Security

### Fix 6.1 — Protect /metrics endpoint

The Prometheus /metrics endpoint exposes internal application details.
It should NOT be public. Add optional basic auth or IP allowlist.

Add to config:
```go
MetricsToken string // if set, require Bearer token on /metrics
```
Env var: KEYWATCHER_METRICS_TOKEN

In server.go, wrap /metrics:
```go
s.router.Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
    if s.cfg.MetricsToken != "" {
        auth := r.Header.Get("Authorization")
        if auth != "Bearer "+s.cfg.MetricsToken {
            w.WriteHeader(http.StatusUnauthorized)
            return
        }
    }
    promhttp.Handler().ServeHTTP(w, r)
})
```

In docker-compose.dev.yml, add Prometheus scrape auth:
```yaml
# In prometheus.yml scrape config add:
# bearer_token: your-metrics-token
```

---

## AUDIT CATEGORY 7 — DB Security

### Fix 7.1 — Connection string must never be logged

In internal/store/postgres.go NewPostgres():
```go
// NEVER log the DSN — it contains credentials
log.Info().Msg("connecting to database")
// NOT: log.Info().Str("dsn", dsn).Msg(...)
```

### Fix 7.2 — Statement timeout

Add statement timeout to prevent long-running queries from blocking:
```go
cfg.ConnConfig.RuntimeParams["statement_timeout"] = "30000" // 30 seconds
cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "60000"
```

---

## AUDIT CATEGORY 8 — Secrets in Config

### Fix 8.1 — Mask secrets in any debug logging

In config/config.go, add a String() method that masks secrets:
```go
func (c *Config) SafeLog() map[string]interface{} {
    return map[string]interface{}{
        "port":         c.Port,
        "log_level":    c.LogLevel,
        "auth_methods": c.AuthMethods,
        "db_url":       "****",
        "jwt_secret":   "****",
        "oidc_issuer":  c.OIDC.Issuer,
        "oidc_client_id": c.OIDC.ClientID,
    }
}
```

In main.go log startup config using SafeLog():
```go
log.Info().Interface("config", cfg.SafeLog()).Msg("keywatcher starting")
```

---

## AUDIT CATEGORY 9 — CORS

### Fix 9.1 — Restrict CORS

In server.go, replace any permissive CORS with strict settings:
```go
s.router.Use(func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        origin := r.Header.Get("Origin")
        // Only allow same origin — no cross-origin API access
        if origin != "" {
            // In production, set KEYWATCHER_ALLOWED_ORIGIN to your domain
            allowed := s.cfg.AllowedOrigin
            if allowed == "" { allowed = "http://localhost:8080" }
            if origin == allowed {
                w.Header().Set("Access-Control-Allow-Origin", origin)
                w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
                w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
                w.Header().Set("Access-Control-Max-Age", "3600")
            }
        }
        if r.Method == http.MethodOptions {
            w.WriteHeader(http.StatusNoContent)
            return
        }
        next.ServeHTTP(w, r)
    })
})
```

Add to config:
```go
AllowedOrigin string // KEYWATCHER_ALLOWED_ORIGIN
```

---

## AUDIT CATEGORY 10 — Setup Endpoint Security

### Fix 10.1 — Lock down /setup after first use

The /setup endpoint must be completely disabled after setup is complete.
Currently it checks the DB — but it should also be rate limited.

In internal/handler/setup.go Complete():
Add rate limit — max 3 attempts per IP:
```go
// Add at top of file
var setupAttempts = auth.NewRateLimiter(3, 10*time.Minute)
```

In server.go, wrap setup route:
```go
s.router.With(setupAttempts.Middleware).Post("/setup", s.setupHandler.Complete)
```

### Fix 10.2 — Validate setup input strictly

In setup.go Complete():
```go
if len(req.Username) < 3 || len(req.Username) > 50 {
    writeError(w, http.StatusBadRequest, "username must be 3-50 characters")
    return
}
if len(req.Password) < 12 {
    writeError(w, http.StatusBadRequest, "password must be at least 12 characters")
    return
}
if !strings.Contains(req.Email, "@") {
    writeError(w, http.StatusBadRequest, "invalid email address")
    return
}
```

---

## Final check
1. Run: go build ./...  — fix all errors
2. Run: docker compose -f docker-compose.dev.yml up --build -d
3. Run these security checks:

# Check no server header leaks
curl -I http://localhost:8080/healthz | grep -i "server\|x-powered"

# Check security headers present
curl -I http://localhost:8080/ | grep -i "x-frame\|x-content\|content-security"

# Check login rate limiting (run 6 times fast)
for i in 1 2 3 4 5 6; do
    curl -s -o /dev/null -w "%{http_code}\n" -X POST http://localhost:8080/api/v1/auth/login \
        -H "Content-Type: application/json" \
        -d '{"username":"x","password":"wrong"}'
done
# 6th attempt must return 429

# Check metrics not public (if token set)
curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/metrics

# Check browser console is clean (no internal details)
# Open http://localhost:8080 in browser, open DevTools Console
# Must show NO logs unless ?debug=1 is in URL

4. Report results of all checks.
