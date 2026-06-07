# Agent: Frontend UI (Minimal)

You own the frontend. No frameworks. No build tools. No npm.
One HTML file served directly by Go. That's it.

## Rules
- Single file: internal/server/static/index.html
- Vanilla JS only — no React, no Vue, no bundler
- CSS: inline <style> block, no external CSS files
- Icons: use Unicode characters or simple SVG inline — no icon libraries
- HTTP calls: native fetch() API only
- No node_modules, no package.json, no vite, no webpack

## Task 1 — Create internal/server/static/index.html

Build a single HTML file with these sections:

### Login screen
Shown when not authenticated. Centered on page.
Fields: username, password. Submit button.
On success: store JWT in localStorage, show main app.
On error: show error message below form.

### Main app (shown after login)
Simple top navigation bar:
- Left: "VaultWatch" text
- Right: current username + Logout button

Four tabs (just show/hide divs, no routing):
- Dashboard
- Assets  
- Add Asset
- Users

### Dashboard tab
Four number cards in a row: Total | Expiring (≤30d) | Critical (≤7d) | Expired
Below: a plain HTML table of the 10 soonest-expiring assets.
Columns: Name | Type | Expires | Days Left
Color the Days Left cell: red if expired, orange if ≤7, yellow if ≤30, green otherwise.

### Assets tab
A plain HTML table. Columns: Name | Type | Expires | Days Left | Status | Actions
Actions: Edit button (opens inline edit form below table) | Delete button (confirm then delete)
Add a simple text input at top to filter by name (client-side, no API call).
Status badge: just colored text — Expired/Expiring/Valid.

### Add Asset tab
A plain form. Fields:
- Name (text input)
- Type (select: certificate / token / api_key / secret / custom)
- Expires At (date input)
- Description (textarea, optional)
- Tags (one text input, format: key=value,key=value)
- PEM Certificate (textarea, only show when type=certificate)
  Below PEM field: small text "Expiry will be auto-detected from certificate"
Submit button. Show success or error message after submit.

### Users tab
A plain HTML table: Username | Email | Auth Method | Last Login
Below: small form to create a local user: Username | Email | Password | Create button

## Task 2 — JavaScript (inline in the same HTML file)

Write a <script> block at the bottom with these functions:

```javascript
const API = '/api/v1'
let token = localStorage.getItem('vw_token') || ''

// Generic fetch wrapper
async function api(method, path, body) {
  const res = await fetch(API + path, {
    method,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {})
    },
    body: body ? JSON.stringify(body) : undefined
  })
  if (res.status === 401) { logout(); return }
  return res.json()
}

// Auth
async function login(username, password) { ... }
function logout() { token = ''; localStorage.removeItem('vw_token'); showLogin() }

// Navigation
function showTab(name) { /* hide all tab divs, show the one matching name */ }

// Dashboard
async function loadDashboard() {
  // fetch /assets, calculate counts, render stat cards and table
}

// Assets
async function loadAssets() { /* fetch and render table */ }
async function deleteAsset(id) { /* confirm, delete, reload */ }
async function submitAsset(formData) { /* POST /assets */ }

// Users  
async function loadUsers() { /* fetch GET /api/v1/users and render */ }
async function createUser(data) { /* POST /api/v1/users */ }

// Init
if (token) showApp()
else showLogin()
```

## Task 3 — CSS (inline in the same HTML file)

Minimal styles only:
```css
* { box-sizing: border-box; margin: 0; padding: 0; font-family: system-ui, sans-serif; }
body { background: #f5f5f5; color: #333; }
.nav { background: #1e293b; color: white; padding: 12px 24px; display: flex; justify-content: space-between; align-items: center; }
.nav button { background: transparent; color: #94a3b8; border: 1px solid #334155; padding: 4px 12px; border-radius: 4px; cursor: pointer; }
.tabs { display: flex; gap: 0; border-bottom: 2px solid #e2e8f0; padding: 0 24px; background: white; }
.tab { padding: 12px 20px; cursor: pointer; border-bottom: 2px solid transparent; margin-bottom: -2px; }
.tab.active { border-bottom-color: #3b82f6; color: #3b82f6; font-weight: 500; }
.content { padding: 24px; max-width: 1200px; margin: 0 auto; }
.cards { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; margin-bottom: 24px; }
.card { background: white; border-radius: 8px; padding: 20px; border: 1px solid #e2e8f0; }
.card .number { font-size: 32px; font-weight: 700; }
.card .label { color: #64748b; font-size: 14px; margin-top: 4px; }
table { width: 100%; border-collapse: collapse; background: white; border-radius: 8px; overflow: hidden; }
th { background: #f8fafc; padding: 12px 16px; text-align: left; font-size: 13px; color: #64748b; border-bottom: 1px solid #e2e8f0; }
td { padding: 12px 16px; border-bottom: 1px solid #f1f5f9; font-size: 14px; }
tr:last-child td { border-bottom: none; }
.btn { padding: 8px 16px; border-radius: 6px; border: none; cursor: pointer; font-size: 14px; }
.btn-primary { background: #3b82f6; color: white; }
.btn-danger { background: #ef4444; color: white; }
.btn-sm { padding: 4px 10px; font-size: 12px; }
input, select, textarea { width: 100%; padding: 8px 12px; border: 1px solid #d1d5db; border-radius: 6px; font-size: 14px; margin-bottom: 12px; }
.form-group label { display: block; font-size: 13px; font-weight: 500; margin-bottom: 4px; color: #374151; }
.login-box { max-width: 360px; margin: 100px auto; background: white; padding: 32px; border-radius: 12px; border: 1px solid #e2e8f0; }
.error { color: #ef4444; font-size: 13px; margin-top: 8px; }
.success { color: #22c55e; font-size: 13px; margin-top: 8px; }
.badge { display: inline-block; padding: 2px 8px; border-radius: 99px; font-size: 12px; font-weight: 500; }
.badge-red { background: #fee2e2; color: #dc2626; }
.badge-orange { background: #ffedd5; color: #ea580c; }
.badge-yellow { background: #fef9c3; color: #ca8a04; }
.badge-green { background: #dcfce7; color: #16a34a; }
```

## Task 4 — Add /api/v1/users endpoints to Go backend

Create internal/handler/user.go:
```go
package handler

import (
    "encoding/json"
    "net/http"
    "vaultwatch/internal/auth"
    "vaultwatch/internal/store"
)

type UserHandler struct {
    store store.Store
}

func NewUserHandler(s store.Store) *UserHandler {
    return &UserHandler{store: s}
}

func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
    // For now return empty list — full implementation can be added later
    // The store does not have ListUsers yet, so return a stub
    writeJSON(w, http.StatusOK, []any{})
}

func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Username string `json:"username"`
        Email    string `json:"email"`
        Password string `json:"password"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request body")
        return
    }
    if err := auth.CreateLocalUser(r.Context(), h.store, req.Username, req.Email, req.Password); err != nil {
        writeError(w, http.StatusBadRequest, err.Error())
        return
    }
    writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}
```

Register in internal/server/server.go (inside the RequireAuth group):
```
GET  /api/v1/users   → userHandler.ListUsers
POST /api/v1/users   → userHandler.CreateUser
```

## Task 5 — Serve the HTML file from Go

In internal/server/server.go, serve index.html for all non-API routes:
```go
// After all /api routes, add:
s.router.Get("/*", func(w http.ResponseWriter, r *http.Request) {
    http.ServeFile(w, r, "internal/server/static/index.html")
})
```

Or use embed if preferred:
```go
//go:embed static/index.html
var indexHTML string

s.router.Get("/*", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/html")
    w.Write([]byte(indexHTML))
})
```

## Task 6 — Final check
Run `go build ./...` — fix any errors.
Open internal/server/static/index.html in a browser directly and verify it renders without errors.
