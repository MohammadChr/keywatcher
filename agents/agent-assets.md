# Agent: Assets

You own the asset CRUD layer and certificate parser. Read CLAUDE.md first.
Depends on: store package, auth session, model package all being complete.

## Task 1 — Certificate Parser (add to internal/model/asset.go)

Append to the existing internal/model/asset.go file:
```go
import (
    "crypto/sha256"
    "crypto/x509"
    "encoding/pem"
    "fmt"
)

func ParseCertificate(pemData string) (*CertInfo, error) {
    block, _ := pem.Decode([]byte(pemData))
    if block == nil {
        return nil, fmt.Errorf("model.ParseCertificate: invalid PEM data")
    }
    cert, err := x509.ParseCertificate(block.Bytes)
    if err != nil {
        return nil, fmt.Errorf("model.ParseCertificate: %w", err)
    }
    fp := sha256.Sum256(cert.Raw)
    sans := cert.DNSNames
    for _, ip := range cert.IPAddresses {
        sans = append(sans, ip.String())
    }
    return &CertInfo{
        Subject:     cert.Subject.CommonName,
        Issuer:      cert.Issuer.CommonName,
        SANs:        sans,
        ExpiresAt:   cert.NotAfter,
        Fingerprint: fmt.Sprintf("%x", fp),
    }, nil
}
```

## Task 2 — Asset Handler (internal/handler/asset.go)
```go
package handler

import (
    "encoding/json"
    "net/http"
    "strconv"
    "vaultwatch/internal/auth"
    "vaultwatch/internal/model"
    "vaultwatch/internal/store"
    "github.com/go-chi/chi/v5"
    "github.com/google/uuid"
)

type AssetHandler struct {
    store store.Store
}

func NewAssetHandler(s store.Store) *AssetHandler {
    return &AssetHandler{store: s}
}

func (h *AssetHandler) ListAssets(w http.ResponseWriter, r *http.Request) {
    q := r.URL.Query()
    page, _  := strconv.Atoi(q.Get("page"))
    limit, _ := strconv.Atoi(q.Get("limit"))
    if page < 1  { page = 1 }
    if limit < 1 { limit = 50 }

    f := store.AssetFilter{
        Type:   q.Get("type"),
        Status: q.Get("status"),
        Page:   page,
        Limit:  limit,
    }
    if tag := q.Get("tag"); tag != "" {
        // format: key=value
        for i, c := range tag {
            if c == '=' {
                f.TagKey   = tag[:i]
                f.TagValue = tag[i+1:]
                break
            }
        }
    }

    assets, total, err := h.store.ListAssets(r.Context(), f)
    if err != nil {
        writeError(w, http.StatusInternalServerError, "failed to list assets")
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{
        "assets": assets,
        "total":  total,
        "page":   page,
        "limit":  limit,
    })
}

type createAssetRequest struct {
    Name        string            `json:"name"`
    Type        model.AssetType   `json:"type"`
    ExpiresAt   string            `json:"expires_at"` // RFC3339
    Description string            `json:"description"`
    Tags        map[string]string `json:"tags"`
    Metadata    map[string]any    `json:"metadata"`
}

func (h *AssetHandler) CreateAsset(w http.ResponseWriter, r *http.Request) {
    var req createAssetRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request body")
        return
    }
    if req.Name == "" {
        writeError(w, http.StatusBadRequest, "name is required")
        return
    }
    if !req.Type.Valid() {
        writeError(w, http.StatusBadRequest, "invalid asset type")
        return
    }

    claims := auth.GetClaims(r)
    createdBy := ""
    if claims != nil { createdBy = claims.UserID.String() }

    a := &model.Asset{
        ID:          uuid.New(),
        Name:        req.Name,
        Type:        req.Type,
        Description: req.Description,
        Tags:        req.Tags,
        Metadata:    req.Metadata,
        CreatedBy:   createdBy,
    }
    if a.Tags == nil     { a.Tags = map[string]string{} }
    if a.Metadata == nil { a.Metadata = map[string]any{} }

    // Auto-parse certificate PEM
    if req.Type == model.AssetTypeCert {
        if pem, ok := req.Metadata["pem"].(string); ok && pem != "" {
            info, err := model.ParseCertificate(pem)
            if err == nil {
                a.ExpiresAt = info.ExpiresAt
                a.Metadata["subject"]     = info.Subject
                a.Metadata["issuer"]      = info.Issuer
                a.Metadata["sans"]        = info.SANs
                a.Metadata["fingerprint"] = info.Fingerprint
            }
        }
    }

    // If expires_at not set by cert parsing, parse from request
    if a.ExpiresAt.IsZero() {
        if req.ExpiresAt == "" {
            writeError(w, http.StatusBadRequest, "expires_at is required")
            return
        }
        t, err := parseTime(req.ExpiresAt)
        if err != nil {
            writeError(w, http.StatusBadRequest, "invalid expires_at format (use RFC3339)")
            return
        }
        a.ExpiresAt = t
    }

    if err := h.store.CreateAsset(r.Context(), a); err != nil {
        writeError(w, http.StatusInternalServerError, "failed to create asset")
        return
    }
    writeJSON(w, http.StatusCreated, a)
}

func (h *AssetHandler) GetAsset(w http.ResponseWriter, r *http.Request) {
    id, err := uuid.Parse(chi.URLParam(r, "id"))
    if err != nil {
        writeError(w, http.StatusBadRequest, "invalid id")
        return
    }
    a, err := h.store.GetAsset(r.Context(), id)
    if err != nil || a == nil {
        writeError(w, http.StatusNotFound, "asset not found")
        return
    }
    writeJSON(w, http.StatusOK, a)
}

func (h *AssetHandler) UpdateAsset(w http.ResponseWriter, r *http.Request) {
    id, err := uuid.Parse(chi.URLParam(r, "id"))
    if err != nil {
        writeError(w, http.StatusBadRequest, "invalid id")
        return
    }
    existing, err := h.store.GetAsset(r.Context(), id)
    if err != nil || existing == nil {
        writeError(w, http.StatusNotFound, "asset not found")
        return
    }

    // Authorization: only creator can update
    claims := auth.GetClaims(r)
    if claims != nil && existing.CreatedBy != claims.UserID.String() {
        writeError(w, http.StatusForbidden, "not allowed")
        return
    }

    var req createAssetRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request body")
        return
    }

    if req.Name != ""    { existing.Name = req.Name }
    if req.Type.Valid()  { existing.Type = req.Type }
    if req.Description != "" { existing.Description = req.Description }
    if req.Tags != nil   { existing.Tags = req.Tags }
    if req.Metadata != nil { existing.Metadata = req.Metadata }
    if req.ExpiresAt != "" {
        t, err := parseTime(req.ExpiresAt)
        if err == nil { existing.ExpiresAt = t }
    }

    if err := h.store.UpdateAsset(r.Context(), existing); err != nil {
        writeError(w, http.StatusInternalServerError, "failed to update asset")
        return
    }
    writeJSON(w, http.StatusOK, existing)
}

func (h *AssetHandler) DeleteAsset(w http.ResponseWriter, r *http.Request) {
    id, err := uuid.Parse(chi.URLParam(r, "id"))
    if err != nil {
        writeError(w, http.StatusBadRequest, "invalid id")
        return
    }
    if err := h.store.DeleteAsset(r.Context(), id); err != nil {
        writeError(w, http.StatusInternalServerError, "failed to delete asset")
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func parseTime(s string) (time.Time, error) {
    import "time"
    formats := []string{time.RFC3339, "2006-01-02", "2006-01-02T15:04:05"}
    for _, f := range formats {
        if t, err := time.Parse(f, s); err == nil {
            return t, nil
        }
    }
    return time.Time{}, fmt.Errorf("cannot parse time: %s", s)
}
```

Fix the parseTime function — move the imports to the top of the file and use proper Go syntax. Add `"fmt"` and `"time"` to the import block at the top.

## Task 3 — Wire Routes in server (update internal/server/server.go)

Update internal/server/server.go to register all routes.
Add the full route registration using chi with the RequireAuth middleware.
The server struct should accept store, config, and all auth handlers.
Routes to register:
- POST   /api/v1/auth/login         → authHandler.Login (public)
- POST   /api/v1/auth/logout        → authHandler.Logout (public)
- GET    /api/v1/auth/oidc/callback → authHandler.OIDCCallback (public)
- GET    /metrics                   → promhttp.Handler() (public)
- GET    /healthz                   → inline 200 (public)
- GET    /readyz                    → inline DB ping (public)
- GET    /api/v1/assets             → assetHandler.ListAssets (RequireAuth)
- POST   /api/v1/assets             → assetHandler.CreateAsset (RequireAuth)
- GET    /api/v1/assets/{id}        → assetHandler.GetAsset (RequireAuth)
- PUT    /api/v1/assets/{id}        → assetHandler.UpdateAsset (RequireAuth)
- DELETE /api/v1/assets/{id}        → assetHandler.DeleteAsset (RequireAuth)

## Final check
Run `go build ./...` — fix any compile errors before finishing.
