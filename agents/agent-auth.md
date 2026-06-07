# Agent: Auth

You own all authentication logic. Read CLAUDE.md first.
No global state. Every function is pure or takes explicit dependencies.

## Task 1 — Local Auth (internal/auth/local.go)
```go
package auth

import (
    "context"
    "fmt"
    "vaultwatch/internal/model"
    "vaultwatch/internal/store"
    "golang.org/x/crypto/bcrypt"
    "github.com/google/uuid"
)

const bcryptCost = 12

func HashPassword(plain string) (string, error) {
    b, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
    if err != nil {
        return "", fmt.Errorf("auth.HashPassword: %w", err)
    }
    return string(b), nil
}

func VerifyPassword(hash, plain string) bool {
    return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

func AuthenticateLocal(ctx context.Context, s store.Store, username, password string) (*model.User, error) {
    user, err := s.GetUserByUsername(ctx, username)
    if err != nil {
        return nil, fmt.Errorf("auth.AuthenticateLocal: %w", err)
    }
    if user == nil || user.PasswordHash == nil {
        return nil, fmt.Errorf("auth.AuthenticateLocal: invalid credentials")
    }
    if !VerifyPassword(*user.PasswordHash, password) {
        return nil, fmt.Errorf("auth.AuthenticateLocal: invalid credentials")
    }
    return user, nil
}

func CreateLocalUser(ctx context.Context, s store.Store, username, email, password string) error {
    if username == "" || email == "" || password == "" {
        return fmt.Errorf("auth.CreateLocalUser: all fields required")
    }
    if len(password) < 8 {
        return fmt.Errorf("auth.CreateLocalUser: password must be at least 8 characters")
    }
    hash, err := HashPassword(password)
    if err != nil {
        return err
    }
    return s.CreateUser(ctx, &model.User{
        ID:           uuid.New(),
        Username:     username,
        Email:        email,
        PasswordHash: &hash,
        AuthMethod:   model.AuthMethodLocal,
    })
}
```

## Task 2 — LDAP Auth (internal/auth/ldap.go)
```go
package auth

import (
    "context"
    "crypto/tls"
    "fmt"
    "strings"
    "sync"
    "vaultwatch/internal/model"
    "vaultwatch/internal/store"
    "github.com/google/uuid"
    ldap "github.com/go-ldap/ldap/v3"
)

type LDAPConfig struct {
    URL          string
    BindDN       string
    BindPassword string
    BaseDN       string
    UserFilter   string // e.g. "(uid=%s)"
}

type LDAPAuthenticator struct {
    cfg  LDAPConfig
    mu   sync.Mutex
    conn *ldap.Conn
}

func NewLDAPAuthenticator(cfg LDAPConfig) *LDAPAuthenticator {
    return &LDAPAuthenticator{cfg: cfg}
}

func (l *LDAPAuthenticator) connect() error {
    var conn *ldap.Conn
    var err error
    if strings.HasPrefix(l.cfg.URL, "ldaps://") {
        conn, err = ldap.DialURL(l.cfg.URL, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: false}))
    } else {
        conn, err = ldap.DialURL(l.cfg.URL)
        if err == nil {
            err = conn.StartTLS(&tls.Config{InsecureSkipVerify: false})
        }
    }
    if err != nil {
        return fmt.Errorf("ldap.connect: %w", err)
    }
    l.conn = conn
    return nil
}

func (l *LDAPAuthenticator) Authenticate(ctx context.Context, s store.Store, username, password string) (*model.User, error) {
    l.mu.Lock()
    defer l.mu.Unlock()

    if l.conn == nil {
        if err := l.connect(); err != nil {
            return nil, err
        }
    }

    // Bind with service account
    if err := l.conn.Bind(l.cfg.BindDN, l.cfg.BindPassword); err != nil {
        l.conn = nil
        return nil, fmt.Errorf("ldap.Authenticate service bind: %w", err)
    }

    // Search for user
    filter := fmt.Sprintf(l.cfg.UserFilter, ldap.EscapeFilter(username))
    result, err := l.conn.Search(&ldap.SearchRequest{
        BaseDN: l.cfg.BaseDN,
        Scope:  ldap.ScopeWholeSubtree,
        Filter: filter,
        Attributes: []string{"dn", "mail", "cn"},
    })
    if err != nil || len(result.Entries) == 0 {
        return nil, fmt.Errorf("ldap.Authenticate: user not found")
    }

    userDN := result.Entries[0].DN
    email  := result.Entries[0].GetAttributeValue("mail")

    // Verify user password
    if err := l.conn.Bind(userDN, password); err != nil {
        return nil, fmt.Errorf("ldap.Authenticate: invalid credentials")
    }

    // Re-bind as service account for future ops
    _ = l.conn.Bind(l.cfg.BindDN, l.cfg.BindPassword)

    // Upsert user in local DB
    user := &model.User{
        ID:         uuid.New(),
        Username:   username,
        Email:      email,
        AuthMethod: model.AuthMethodLDAP,
    }
    if err := s.CreateUser(ctx, user); err != nil {
        return nil, fmt.Errorf("ldap.Authenticate upsert: %w", err)
    }
    return user, nil
}
```
Run: `go get github.com/go-ldap/ldap/v3`

## Task 3 — OIDC Auth (internal/auth/oidc.go)
```go
package auth

import (
    "context"
    "fmt"
    "vaultwatch/internal/model"
    "vaultwatch/internal/store"
    "github.com/coreos/go-oidc/v3/oidc"
    "github.com/google/uuid"
    "golang.org/x/oauth2"
)

type OIDCConfig struct {
    Issuer       string
    ClientID     string
    ClientSecret string
    RedirectURL  string
}

type OIDCAuthenticator struct {
    cfg      OIDCConfig
    provider *oidc.Provider
    oauth2   *oauth2.Config
    verifier *oidc.IDTokenVerifier
}

func NewOIDCAuthenticator(ctx context.Context, cfg OIDCConfig) (*OIDCAuthenticator, error) {
    provider, err := oidc.NewProvider(ctx, cfg.Issuer)
    if err != nil {
        return nil, fmt.Errorf("oidc.NewProvider: %w", err)
    }
    o := &OIDCAuthenticator{
        cfg:      cfg,
        provider: provider,
        verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
        oauth2: &oauth2.Config{
            ClientID:     cfg.ClientID,
            ClientSecret: cfg.ClientSecret,
            RedirectURL:  cfg.RedirectURL,
            Endpoint:     provider.Endpoint(),
            Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
        },
    }
    return o, nil
}

func (o *OIDCAuthenticator) AuthCodeURL(state, nonce string) string {
    return o.oauth2.AuthCodeURL(state, oidc.Nonce(nonce))
}

func (o *OIDCAuthenticator) Exchange(ctx context.Context, s store.Store, code string, nonce string) (*model.User, error) {
    token, err := o.oauth2.Exchange(ctx, code)
    if err != nil {
        return nil, fmt.Errorf("oidc.Exchange: %w", err)
    }
    rawID, ok := token.Extra("id_token").(string)
    if !ok {
        return nil, fmt.Errorf("oidc.Exchange: no id_token")
    }
    idToken, err := o.verifier.Verify(ctx, rawID)
    if err != nil {
        return nil, fmt.Errorf("oidc.Exchange verify: %w", err)
    }
    if idToken.Nonce != nonce {
        return nil, fmt.Errorf("oidc.Exchange: nonce mismatch")
    }
    var claims struct {
        Email    string `json:"email"`
        Name     string `json:"name"`
        Subject  string `json:"sub"`
    }
    if err := idToken.Claims(&claims); err != nil {
        return nil, fmt.Errorf("oidc.Exchange claims: %w", err)
    }
    user := &model.User{
        ID:         uuid.New(),
        Username:   claims.Name,
        Email:      claims.Email,
        AuthMethod: model.AuthMethodOIDC,
    }
    if err := s.CreateUser(ctx, user); err != nil {
        return nil, fmt.Errorf("oidc.Exchange upsert: %w", err)
    }
    return user, nil
}
```
Run: `go get github.com/coreos/go-oidc/v3 golang.org/x/oauth2`

## Task 4 — JWT Session (internal/auth/session.go)
```go
package auth

import (
    "fmt"
    "net/http"
    "strings"
    "time"
    "vaultwatch/internal/model"
    "github.com/golang-jwt/jwt/v5"
    "github.com/google/uuid"
)

type Claims struct {
    UserID     uuid.UUID          `json:"uid"`
    Email      string             `json:"email"`
    AuthMethod model.AuthMethod   `json:"auth_method"`
    jwt.RegisteredClaims
}

func IssueToken(user *model.User, secret string, ttl time.Duration) (string, error) {
    claims := Claims{
        UserID:     user.ID,
        Email:      user.Email,
        AuthMethod: user.AuthMethod,
        RegisteredClaims: jwt.RegisteredClaims{
            Subject:   user.ID.String(),
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
            IssuedAt:  jwt.NewNumericDate(time.Now()),
        },
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    signed, err := token.SignedString([]byte(secret))
    if err != nil {
        return "", fmt.Errorf("auth.IssueToken: %w", err)
    }
    return signed, nil
}

func ValidateToken(tokenStr, secret string) (*Claims, error) {
    token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
        if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
            return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
        }
        return []byte(secret), nil
    })
    if err != nil {
        return nil, fmt.Errorf("auth.ValidateToken: %w", err)
    }
    claims, ok := token.Claims.(*Claims)
    if !ok || !token.Valid {
        return nil, fmt.Errorf("auth.ValidateToken: invalid claims")
    }
    return claims, nil
}

type contextKey string
const ClaimsKey contextKey = "claims"

func RequireAuth(secret string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            var tokenStr string

            // Try Authorization header first
            if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
                tokenStr = strings.TrimPrefix(h, "Bearer ")
            }
            // Fall back to cookie
            if tokenStr == "" {
                if c, err := r.Cookie("session"); err == nil {
                    tokenStr = c.Value
                }
            }

            if tokenStr == "" {
                http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
                return
            }

            claims, err := ValidateToken(tokenStr, secret)
            if err != nil {
                http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
                return
            }

            ctx := r.Context()
            ctx = setInContext(ctx, ClaimsKey, claims)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

func GetClaims(r *http.Request) *Claims {
    v := r.Context().Value(ClaimsKey)
    if v == nil { return nil }
    c, _ := v.(*Claims)
    return c
}

func setInContext(ctx interface{ Value(any) any }, key, val any) interface{ Value(any) any } {
    // use standard context
    return ctx
}
```

Fix the context helper — use `"context"` package properly:
Replace the setInContext function with:
```go
import "context"
// in RequireAuth:
ctx = context.WithValue(r.Context(), ClaimsKey, claims)
```

## Task 5 — Auth Handlers (internal/handler/auth.go)
```go
package handler

import (
    "encoding/json"
    "net/http"
    "time"
    "vaultwatch/internal/auth"
    "vaultwatch/internal/model"
    "vaultwatch/internal/store"
    "vaultwatch/config"
)

type AuthHandler struct {
    store store.Store
    cfg   *config.Config
    oidc  *auth.OIDCAuthenticator // nil if not enabled
    ldap  *auth.LDAPAuthenticator // nil if not enabled
}

func NewAuthHandler(s store.Store, cfg *config.Config, oidcA *auth.OIDCAuthenticator, ldapA *auth.LDAPAuthenticator) *AuthHandler {
    return &AuthHandler{store: s, cfg: cfg, oidc: oidcA, ldap: ldapA}
}

type loginRequest struct {
    Username string `json:"username"`
    Password string `json:"password"`
    Method   string `json:"method"` // optional: force a specific method
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
    var req loginRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request body")
        return
    }

    var user *model.User
    var err error

    // Try local first, then LDAP
    for _, method := range h.cfg.AuthMethods {
        switch method {
        case "local":
            user, err = auth.AuthenticateLocal(r.Context(), h.store, req.Username, req.Password)
        case "ldap":
            if h.ldap != nil {
                user, err = h.ldap.Authenticate(r.Context(), h.store, req.Username, req.Password)
            }
        }
        if user != nil {
            break
        }
    }

    if user == nil {
        writeError(w, http.StatusUnauthorized, "invalid credentials")
        return
    }

    _ = h.store.UpdateLastLogin(r.Context(), user.ID)

    token, err := auth.IssueToken(user, h.cfg.JWTSecret, 8*time.Hour)
    if err != nil {
        writeError(w, http.StatusInternalServerError, "could not issue token")
        return
    }

    http.SetCookie(w, &http.Cookie{
        Name:     "session",
        Value:    token,
        HttpOnly: true,
        SameSite: http.SameSiteLaxMode,
        Path:     "/",
        MaxAge:   8 * 3600,
    })

    writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
    http.SetCookie(w, &http.Cookie{Name: "session", MaxAge: -1, Path: "/"})
    writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *AuthHandler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
    if h.oidc == nil {
        writeError(w, http.StatusNotFound, "OIDC not enabled")
        return
    }
    code  := r.URL.Query().Get("code")
    nonce := r.URL.Query().Get("state")
    user, err := h.oidc.Exchange(r.Context(), h.store, code, nonce)
    if err != nil {
        writeError(w, http.StatusUnauthorized, "OIDC exchange failed")
        return
    }
    token, err := auth.IssueToken(user, h.cfg.JWTSecret, 8*time.Hour)
    if err != nil {
        writeError(w, http.StatusInternalServerError, "could not issue token")
        return
    }
    http.SetCookie(w, &http.Cookie{Name: "session", Value: token, HttpOnly: true, Path: "/", MaxAge: 8 * 3600})
    http.Redirect(w, r, "/", http.StatusFound)
}

// shared helpers
func writeJSON(w http.ResponseWriter, status int, v any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(map[string]any{"data": v, "error": nil})
}

func writeError(w http.ResponseWriter, status int, msg string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(map[string]any{"data": nil, "error": msg})
}
```

## Final check
Run `go build ./...` — fix any errors before finishing.
