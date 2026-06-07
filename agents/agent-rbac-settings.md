# Agent: RBAC + Auth Settings UI

Two features. Read CLAUDE.md first. Do not touch anything not mentioned here.

---

## FEATURE 1 — Two user roles: admin and viewer

### Step 1 — DB migration (004_roles.up.sql)
```sql
ALTER TABLE users ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'viewer'
    CHECK (role IN ('admin', 'viewer'));

-- The first user created (setup) is always admin
-- Update existing users: if only one user exists, make them admin
UPDATE users SET role = 'admin'
WHERE id = (SELECT id FROM users ORDER BY created_at ASC LIMIT 1);
```

Create 004_roles.down.sql:
```sql
ALTER TABLE users DROP COLUMN IF EXISTS role;
```

### Step 2 — Update model/user.go
Add role field:
```go
type UserRole string
const (
    RoleAdmin  UserRole = "admin"
    RoleViewer UserRole = "viewer"
)

// Add to User struct:
Role UserRole `json:"role" db:"role"`
```

### Step 3 — Update store

In store/postgres.go, update ALL user SELECT queries to include role:
```sql
SELECT id,username,email,password_hash,auth_method,role,created_at,last_login FROM users ...
```

Update scanUser() to scan role:
```go
func scanUser(row scanner) (*model.User, error) {
    var u model.User
    err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash,
        &u.AuthMethod, &u.Role, &u.CreatedAt, &u.LastLogin)
    ...
}
```

Update CreateUser INSERT to include role:
```sql
INSERT INTO users (id,username,email,password_hash,auth_method,role)
VALUES (@id,@username,@email,@password_hash,@auth_method,@role)
ON CONFLICT (email) DO UPDATE SET last_login=NOW()
```
Pass role in NamedArgs: `"role": string(u.Role)`

Add to store interface:
```go
UpdateUserRole(ctx context.Context, id uuid.UUID, role model.UserRole) error
```

Add to postgres.go:
```go
func (s *PostgresStore) UpdateUserRole(ctx context.Context, id uuid.UUID, role model.UserRole) error {
    _, err := s.pool.Exec(ctx,
        "UPDATE users SET role=@role WHERE id=@id",
        pgx.NamedArgs{"role": string(role), "id": id})
    return err
}
```

### Step 4 — Add role to JWT claims

In internal/auth/session.go, add Role to Claims:
```go
type Claims struct {
    UserID     uuid.UUID        `json:"uid"`
    Email      string           `json:"email"`
    AuthMethod model.AuthMethod `json:"auth_method"`
    Role       model.UserRole   `json:"role"`
    jwt.RegisteredClaims
}
```

Update IssueToken to include role:
```go
claims := Claims{
    UserID:     user.ID,
    Email:      user.Email,
    AuthMethod: user.AuthMethod,
    Role:       user.Role,
    ...
}
```

### Step 5 — Role middleware

Add to internal/auth/session.go:
```go
// RequireAdmin blocks viewers from admin-only endpoints
func RequireAdmin(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        claims := GetClaims(r)
        if claims == nil || claims.Role != model.RoleAdmin {
            http.Error(w, `{"error":"forbidden","message":"admin access required"}`,
                http.StatusForbidden)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

### Step 6 — Apply role middleware to routes in server.go

Viewer can access (read-only):
- GET /api/v1/assets
- GET /api/v1/assets/{id}

Admin only (wrap with RequireAdmin after RequireAuth):
- POST   /api/v1/assets
- PUT    /api/v1/assets/{id}
- DELETE /api/v1/assets/{id}
- GET    /api/v1/users
- POST   /api/v1/users
- PUT    /api/v1/users/{id}/role
- GET    /api/v1/settings
- PUT    /api/v1/settings

Route registration pattern:
```go
// Authenticated routes
s.router.Group(func(r chi.Router) {
    r.Use(auth.RequireAuth(cfg.JWTSecret))

    // Viewer routes
    r.Get("/api/v1/assets", assetHandler.ListAssets)
    r.Get("/api/v1/assets/{id}", assetHandler.GetAsset)

    // Admin-only routes
    r.Group(func(r chi.Router) {
        r.Use(auth.RequireAdmin)
        r.Post("/api/v1/assets", assetHandler.CreateAsset)
        r.Put("/api/v1/assets/{id}", assetHandler.UpdateAsset)
        r.Delete("/api/v1/assets/{id}", assetHandler.DeleteAsset)
        r.Get("/api/v1/users", userHandler.ListUsers)
        r.Post("/api/v1/users", userHandler.CreateUser)
        r.Put("/api/v1/users/{id}/role", userHandler.UpdateRole)
        r.Get("/api/v1/settings", settingsHandler.Get)
        r.Put("/api/v1/settings", settingsHandler.Update)
    })
})
```

### Step 7 — User handler: UpdateRole

Add to internal/handler/user.go:
```go
func (h *UserHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
    id, err := uuid.Parse(chi.URLParam(r, "id"))
    if err != nil {
        writeError(w, http.StatusBadRequest, "invalid id")
        return
    }

    // Prevent self-demotion
    claims := auth.GetClaims(r)
    if claims != nil && claims.UserID == id {
        writeError(w, http.StatusBadRequest, "cannot change your own role")
        return
    }

    var req struct {
        Role string `json:"role"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid body")
        return
    }
    if req.Role != "admin" && req.Role != "viewer" {
        writeError(w, http.StatusBadRequest, "role must be admin or viewer")
        return
    }
    if err := h.store.UpdateUserRole(r.Context(), id, model.UserRole(req.Role)); err != nil {
        writeError(w, http.StatusInternalServerError, "failed to update role")
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
```

### Step 8 — First user (setup) is always admin

In internal/handler/setup.go, Complete() handler:
When creating the user, set Role to admin:
```go
// In auth.CreateLocalUser, pass role. Update the function signature:
// CreateLocalUser(ctx, store, username, email, password, role)
// For setup: pass model.RoleAdmin
// For regular user creation: pass model.RoleViewer (default)
```

Update auth/local.go CreateLocalUser to accept role parameter:
```go
func CreateLocalUser(ctx context.Context, s store.Store, username, email, password string, role model.UserRole) error {
    ...
    return s.CreateUser(ctx, &model.User{
        ID:           uuid.New(),
        Username:     username,
        Email:        email,
        PasswordHash: &hash,
        AuthMethod:   model.AuthMethodLocal,
        Role:         role,
    })
}
```

Call sites:
- setup.go Complete(): pass model.RoleAdmin
- handler/user.go CreateUser(): pass model.RoleViewer

---

## FEATURE 2 — Auth settings (enable/disable OIDC and LDAP from UI)

Local auth is ALWAYS on. OIDC and LDAP can be toggled.

### Step 1 — DB migration (005_settings.up.sql)
```sql
-- Reuse app_settings table from migration 003
INSERT INTO app_settings (key, value) VALUES
    ('auth_oidc_enabled', 'false'),
    ('auth_ldap_enabled', 'false'),
    ('auth_oidc_issuer', ''),
    ('auth_oidc_client_id', ''),
    ('auth_oidc_client_secret', ''),
    ('auth_ldap_url', ''),
    ('auth_ldap_bind_dn', ''),
    ('auth_ldap_bind_password', ''),
    ('auth_ldap_base_dn', ''),
    ('auth_ldap_user_filter', '(uid=%s)')
ON CONFLICT (key) DO NOTHING;
```

005_settings.down.sql:
```sql
DELETE FROM app_settings WHERE key LIKE 'auth_%';
```

### Step 2 — Store methods for settings

Add to store interface:
```go
GetSetting(ctx context.Context, key string) (string, error)
SetSetting(ctx context.Context, key, value string) error
GetAllSettings(ctx context.Context, prefix string) (map[string]string, error)
```

Add to postgres.go:
```go
func (s *PostgresStore) GetSetting(ctx context.Context, key string) (string, error) {
    var value string
    err := s.pool.QueryRow(ctx,
        "SELECT value FROM app_settings WHERE key=@key",
        pgx.NamedArgs{"key": key}).Scan(&value)
    if err != nil { return "", err }
    return value, nil
}

func (s *PostgresStore) SetSetting(ctx context.Context, key, value string) error {
    _, err := s.pool.Exec(ctx,
        "INSERT INTO app_settings (key,value) VALUES (@key,@value) ON CONFLICT (key) DO UPDATE SET value=@value",
        pgx.NamedArgs{"key": key, "value": value})
    return err
}

func (s *PostgresStore) GetAllSettings(ctx context.Context, prefix string) (map[string]string, error) {
    rows, err := s.pool.Query(ctx,
        "SELECT key,value FROM app_settings WHERE key LIKE @prefix",
        pgx.NamedArgs{"prefix": prefix + "%"})
    if err != nil { return nil, err }
    defer rows.Close()
    result := map[string]string{}
    for rows.Next() {
        var k, v string
        if err := rows.Scan(&k, &v); err != nil { return nil, err }
        result[k] = v
    }
    return result, nil
}
```

### Step 3 — Settings handler (internal/handler/settings.go)

```go
package handler

import (
    "encoding/json"
    "net/http"
    "vaultwatch/internal/store"
)

type SettingsHandler struct {
    store store.Store
}

func NewSettingsHandler(s store.Store) *SettingsHandler {
    return &SettingsHandler{store: s}
}

type AuthSettings struct {
    // OIDC
    OIDCEnabled      bool   `json:"oidc_enabled"`
    OIDCIssuer       string `json:"oidc_issuer"`
    OIDCClientID     string `json:"oidc_client_id"`
    OIDCClientSecret string `json:"oidc_client_secret,omitempty"` // masked on GET

    // LDAP
    LDAPEnabled      bool   `json:"ldap_enabled"`
    LDAPURL          string `json:"ldap_url"`
    LDAPBindDN       string `json:"ldap_bind_dn"`
    LDAPBindPassword string `json:"ldap_bind_password,omitempty"` // masked on GET
    LDAPBaseDN       string `json:"ldap_base_dn"`
    LDAPUserFilter   string `json:"ldap_user_filter"`
}

func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
    s, err := h.store.GetAllSettings(r.Context(), "auth_")
    if err != nil {
        writeError(w, http.StatusInternalServerError, "failed to load settings")
        return
    }
    settings := AuthSettings{
        OIDCEnabled:  s["auth_oidc_enabled"] == "true",
        OIDCIssuer:   s["auth_oidc_issuer"],
        OIDCClientID: s["auth_oidc_client_id"],
        // Mask secrets — return placeholder if set
        OIDCClientSecret: maskSecret(s["auth_oidc_client_secret"]),
        LDAPEnabled:      s["auth_ldap_enabled"] == "true",
        LDAPURL:          s["auth_ldap_url"],
        LDAPBindDN:       s["auth_ldap_bind_dn"],
        LDAPBindPassword: maskSecret(s["auth_ldap_bind_password"]),
        LDAPBaseDN:       s["auth_ldap_base_dn"],
        LDAPUserFilter:   s["auth_ldap_user_filter"],
    }
    writeJSON(w, http.StatusOK, settings)
}

func (h *SettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
    var req AuthSettings
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid body")
        return
    }
    ctx := r.Context()

    saves := map[string]string{
        "auth_oidc_enabled":   boolStr(req.OIDCEnabled),
        "auth_oidc_issuer":    req.OIDCIssuer,
        "auth_oidc_client_id": req.OIDCClientID,
        "auth_ldap_enabled":   boolStr(req.LDAPEnabled),
        "auth_ldap_url":       req.LDAPURL,
        "auth_ldap_bind_dn":   req.LDAPBindDN,
        "auth_ldap_base_dn":   req.LDAPBaseDN,
        "auth_ldap_user_filter": req.LDAPUserFilter,
    }
    // Only overwrite secrets if they are not the masked placeholder
    if req.OIDCClientSecret != "••••••••" && req.OIDCClientSecret != "" {
        saves["auth_oidc_client_secret"] = req.OIDCClientSecret
    }
    if req.LDAPBindPassword != "••••••••" && req.LDAPBindPassword != "" {
        saves["auth_ldap_bind_password"] = req.LDAPBindPassword
    }

    for k, v := range saves {
        if err := h.store.SetSetting(ctx, k, v); err != nil {
            writeError(w, http.StatusInternalServerError, "failed to save: "+k)
            return
        }
    }
    writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func maskSecret(s string) string {
    if s == "" { return "" }
    return "••••••••"
}

func boolStr(b bool) string {
    if b { return "true" }
    return "false"
}
```

### Step 4 — Wire settings handler in server.go and main.go

In main.go create: `settingsHandler := handler.NewSettingsHandler(store)`
Pass it to server.New() and register the routes (already listed in Step 6 above).

---

## FEATURE 3 — Settings page in the UI

### Step 1 — Add Settings tab to navigation

In index.html add a fifth tab button:
```html
<div class="tab" data-tab="settings" onclick="showTab('settings')">⚙ Settings</div>
```

Add to showTab():
```javascript
if (name === 'settings') loadSettings()
```

### Step 2 — Settings tab HTML

Add this tab content div:
```html
<div id="tab-settings" class="tab-content" style="display:none">
    <h2 style="font-size:18px;font-weight:600;margin-bottom:24px">Settings</h2>

    <!-- Auth Methods -->
    <div style="background:white;border:1px solid #e2e8f0;border-radius:8px;
                padding:24px;margin-bottom:20px">
        <h3 style="font-size:15px;font-weight:600;margin-bottom:4px">Authentication</h3>
        <p style="color:#64748b;font-size:13px;margin-bottom:20px">
            Local auth is always enabled. Enable additional providers below.
        </p>

        <!-- OIDC -->
        <div style="border:1px solid #e2e8f0;border-radius:8px;padding:16px;margin-bottom:16px">
            <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px">
                <div>
                    <div style="font-weight:600">OIDC / SSO</div>
                    <div style="font-size:13px;color:#64748b">Keycloak, Okta, Google Workspace, etc.</div>
                </div>
                <label style="display:flex;align-items:center;gap:8px;cursor:pointer">
                    <input type="checkbox" id="oidc-enabled" onchange="toggleOIDCFields()"
                           style="width:16px;height:16px">
                    <span style="font-size:13px">Enable</span>
                </label>
            </div>
            <div id="oidc-fields" style="display:none">
                <div class="form-group"><label>Issuer URL</label>
                    <input id="oidc-issuer" placeholder="https://accounts.example.com"></div>
                <div class="form-group"><label>Client ID</label>
                    <input id="oidc-client-id" placeholder="vaultwatch"></div>
                <div class="form-group"><label>Client Secret</label>
                    <input id="oidc-client-secret" type="password" placeholder="leave blank to keep existing"></div>
            </div>
        </div>

        <!-- LDAP -->
        <div style="border:1px solid #e2e8f0;border-radius:8px;padding:16px">
            <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px">
                <div>
                    <div style="font-weight:600">LDAP / Active Directory</div>
                    <div style="font-size:13px;color:#64748b">OpenLDAP, Microsoft AD, FreeIPA, etc.</div>
                </div>
                <label style="display:flex;align-items:center;gap:8px;cursor:pointer">
                    <input type="checkbox" id="ldap-enabled" onchange="toggleLDAPFields()"
                           style="width:16px;height:16px">
                    <span style="font-size:13px">Enable</span>
                </label>
            </div>
            <div id="ldap-fields" style="display:none">
                <div class="form-group"><label>LDAP URL</label>
                    <input id="ldap-url" placeholder="ldap://ldap.example.com:389"></div>
                <div class="form-group"><label>Bind DN</label>
                    <input id="ldap-bind-dn" placeholder="cn=vaultwatch,dc=example,dc=com"></div>
                <div class="form-group"><label>Bind Password</label>
                    <input id="ldap-bind-password" type="password" placeholder="leave blank to keep existing"></div>
                <div class="form-group"><label>Base DN</label>
                    <input id="ldap-base-dn" placeholder="ou=users,dc=example,dc=com"></div>
                <div class="form-group"><label>User Filter</label>
                    <input id="ldap-user-filter" placeholder="(uid=%s)"></div>
            </div>
        </div>

        <div id="settings-error" class="error" style="margin-top:12px"></div>
        <div id="settings-success" class="success" style="margin-top:12px"></div>
        <button class="btn btn-primary" style="margin-top:16px" onclick="saveSettings()">
            Save Settings
        </button>
        <p style="font-size:12px;color:#94a3b8;margin-top:8px">
            ⚠ Changes take effect on next login. Active sessions are not affected.
        </p>
    </div>
</div>
```

### Step 3 — Settings JS functions

```javascript
async function loadSettings() {
    try {
        const r = await fetch('/api/v1/settings', {
            headers: { Authorization: `Bearer ${token}` }
        })
        if (r.status === 403) {
            document.getElementById('tab-settings').innerHTML =
                '<div style="padding:40px;text-align:center;color:#94a3b8">Admin access required to view settings.</div>'
            return
        }
        const data = await r.json()
        const s = data.data

        document.getElementById('oidc-enabled').checked   = s.oidc_enabled
        document.getElementById('oidc-issuer').value      = s.oidc_issuer || ''
        document.getElementById('oidc-client-id').value   = s.oidc_client_id || ''
        document.getElementById('oidc-client-secret').placeholder =
            s.oidc_client_secret ? 'leave blank to keep existing' : 'enter client secret'

        document.getElementById('ldap-enabled').checked        = s.ldap_enabled
        document.getElementById('ldap-url').value              = s.ldap_url || ''
        document.getElementById('ldap-bind-dn').value          = s.ldap_bind_dn || ''
        document.getElementById('ldap-bind-password').placeholder =
            s.ldap_bind_password ? 'leave blank to keep existing' : 'enter bind password'
        document.getElementById('ldap-base-dn').value          = s.ldap_base_dn || ''
        document.getElementById('ldap-user-filter').value      = s.ldap_user_filter || '(uid=%s)'

        toggleOIDCFields()
        toggleLDAPFields()
    } catch(e) {
        console.error('loadSettings error:', e)
    }
}

function toggleOIDCFields() {
    const enabled = document.getElementById('oidc-enabled').checked
    document.getElementById('oidc-fields').style.display = enabled ? 'block' : 'none'
}

function toggleLDAPFields() {
    const enabled = document.getElementById('ldap-enabled').checked
    document.getElementById('ldap-fields').style.display = enabled ? 'block' : 'none'
}

async function saveSettings() {
    const errEl = document.getElementById('settings-error')
    const okEl  = document.getElementById('settings-success')
    errEl.textContent = ''
    okEl.textContent  = ''

    const payload = {
        oidc_enabled:       document.getElementById('oidc-enabled').checked,
        oidc_issuer:        document.getElementById('oidc-issuer').value.trim(),
        oidc_client_id:     document.getElementById('oidc-client-id').value.trim(),
        oidc_client_secret: document.getElementById('oidc-client-secret').value,
        ldap_enabled:       document.getElementById('ldap-enabled').checked,
        ldap_url:           document.getElementById('ldap-url').value.trim(),
        ldap_bind_dn:       document.getElementById('ldap-bind-dn').value.trim(),
        ldap_bind_password: document.getElementById('ldap-bind-password').value,
        ldap_base_dn:       document.getElementById('ldap-base-dn').value.trim(),
        ldap_user_filter:   document.getElementById('ldap-user-filter').value.trim(),
    }

    const r = await fetch('/api/v1/settings', {
        method: 'PUT',
        headers: { 'Content-Type':'application/json', Authorization:`Bearer ${token}` },
        body: JSON.stringify(payload)
    })
    const data = await r.json()
    if (!r.ok || data.error) {
        errEl.textContent = data.error || 'Failed to save settings'
        return
    }
    okEl.textContent = '✓ Settings saved successfully'
    setTimeout(() => { okEl.textContent = '' }, 3000)
}
```

### Step 4 — Role badge in Users table + role change button

Update renderUsersTable (inside loadUsers) to show role and allow admins to change it:
```javascript
tbody.innerHTML = users.map(u => `<tr>
    <td>${u.username}</td>
    <td>${u.email}</td>
    <td><span class="badge badge-${u.auth_method==='local'?'blue':u.auth_method==='oidc'?'purple':'green'}">${u.auth_method}</span></td>
    <td><span class="badge badge-${u.role==='admin'?'orange':'gray'}">${u.role||'viewer'}</span></td>
    <td>${u.last_login ? formatDate(u.last_login) : 'Never'}</td>
    <td>
        <button class="btn btn-sm" onclick="toggleRole('${u.id}','${u.role||'viewer'}')">
            ${u.role==='admin' ? 'Make Viewer' : 'Make Admin'}
        </button>
    </td>
</tr>`).join('')
```

Add column header for Role in Users table: Username | Email | Auth | Role | Last Login | Actions

Add JS:
```javascript
async function toggleRole(id, currentRole) {
    const newRole = currentRole === 'admin' ? 'viewer' : 'admin'
    if (!confirm(`Change this user to ${newRole}?`)) return
    const r = await fetch(`/api/v1/users/${id}/role`, {
        method: 'PUT',
        headers: { 'Content-Type':'application/json', Authorization:`Bearer ${token}` },
        body: JSON.stringify({ role: newRole })
    })
    const data = await r.json()
    if (!r.ok || data.error) { alert(data.error || 'Failed to change role'); return }
    loadUsers()
}
```

Add to CSS: `.badge-orange { background:#ffedd5;color:#c2410c } .badge-gray { background:#f1f5f9;color:#475569 }`

### Step 5 — Hide admin-only UI elements for viewer role

After login, decode the JWT role claim and store it:
```javascript
function getRoleFromToken(jwt) {
    try {
        const payload = JSON.parse(atob(jwt.split('.')[1]))
        return payload.role || 'viewer'
    } catch(e) { return 'viewer' }
}

// In showApp(), after setting token:
const userRole = getRoleFromToken(token)

// Hide admin-only elements for viewers
if (userRole !== 'admin') {
    // Hide Add Asset tab
    document.querySelector('[data-tab="add-asset"]').style.display = 'none'
    // Hide Users tab
    document.querySelector('[data-tab="users"]').style.display = 'none'
    // Hide Settings tab
    document.querySelector('[data-tab="settings"]').style.display = 'none'
    // Hide Edit/Delete buttons in asset table (re-render without them)
}

// Store role globally for use in renderAssetsTable
window.currentUserRole = userRole
```

In renderAssetsTable(), conditionally show action buttons:
```javascript
const actions = window.currentUserRole === 'admin'
    ? `<button class="btn btn-sm" onclick="editAsset('${a.id}')">Edit</button>
       <button class="btn btn-sm btn-danger" onclick="deleteAsset('${a.id}','${a.name.replace(/'/g,"\\'")}')">Delete</button>`
    : `<span style="color:#94a3b8;font-size:12px">read only</span>`
```

---

## Final check
1. Run `go build ./...` — fix all errors
2. Run migration: `make migrate-up` or run manually inside docker:
   `docker compose -f docker-compose.dev.yml exec postgres psql -U vaultwatch -d vaultwatch -f /dev/stdin < internal/store/migrations/004_roles.up.sql`
3. Run `docker compose -f docker-compose.dev.yml up --build -d`
4. Test:
   - Admin sees all 5 tabs
   - Viewer sees only Dashboard and Assets (read-only)
   - Settings tab saves OIDC/LDAP config to DB
   - Users tab shows role badge and toggle button
5. Report results
