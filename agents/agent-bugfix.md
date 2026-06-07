# Agent: Bug Fix — UI & Backend

You are fixing critical bugs in VaultWatch. Read CLAUDE.md first.
Fix ONLY what is listed here. Do not refactor anything else.

---

## BUG 1 — First run does not redirect to /setup

### Problem
App starts, user opens browser, sees login instead of setup page.

### Fix — internal/server/server.go
The RequireSetupComplete middleware must run on ALL routes including the HTML root.
Make sure the middleware is registered BEFORE any route handler, like this:

```go
// Apply setup check globally first
s.router.Use(func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        skip := r.URL.Path == "/setup" ||
            strings.HasPrefix(r.URL.Path, "/setup/") ||
            r.URL.Path == "/healthz" ||
            r.URL.Path == "/readyz" ||
            r.URL.Path == "/metrics"
        if skip {
            next.ServeHTTP(w, r)
            return
        }
        done, err := store.IsSetupCompleted(r.Context())
        if err != nil || !done {
            if strings.HasPrefix(r.URL.Path, "/api/") {
                w.Header().Set("Content-Type", "application/json")
                w.WriteHeader(http.StatusServiceUnavailable)
                w.Write([]byte(`{"error":"setup_required"}`))
                return
            }
            http.Redirect(w, r, "/setup", http.StatusFound)
            return
        }
        next.ServeHTTP(w, r)
    })
})
```

The Go server struct must receive the store as a dependency so the middleware can call IsSetupCompleted.
Update server.New() to accept store.Store as a parameter.
Update main.go to pass the store to server.New().

---

## BUG 2 — Login shows no error on wrong password

### Problem
User types wrong password, nothing happens or blank screen.

### Fix — internal/handler/auth.go
The Login handler must return a clear JSON error. Verify it looks exactly like this:

```go
if user == nil {
    writeError(w, http.StatusUnauthorized, "invalid username or password")
    return
}
```

Also make sure the auth loop does not swallow errors silently.
Replace the auth loop with this explicit version:

```go
var authErr error
for _, method := range h.cfg.AuthMethods {
    switch method {
    case "local":
        user, authErr = auth.AuthenticateLocal(r.Context(), h.store, req.Username, req.Password)
    case "ldap":
        if h.ldap != nil {
            user, authErr = h.ldap.Authenticate(r.Context(), h.store, req.Username, req.Password)
        }
    }
    if user != nil {
        break
    }
}
if user == nil {
    msg := "invalid username or password"
    if authErr != nil {
        msg = authErr.Error()
    }
    writeError(w, http.StatusUnauthorized, msg)
    return
}
```

### Fix — index.html login JS
The login submit function must read and display the error field:

```javascript
async function submitLogin() {
    const username = document.getElementById('login-username').value.trim()
    const password = document.getElementById('login-password').value
    const errEl = document.getElementById('login-error')
    errEl.textContent = ''

    if (!username || !password) {
        errEl.textContent = 'Username and password are required'
        return
    }

    try {
        const r = await fetch('/api/v1/auth/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username, password })
        })
        const data = await r.json()
        if (!r.ok || data.error) {
            errEl.textContent = data.error || 'Login failed'
            return
        }
        token = data.data.token
        localStorage.setItem('vw_token', token)
        showApp()
        loadDashboard()
    } catch (e) {
        errEl.textContent = 'Network error — is the server running?'
    }
}
```

Make sure the login form has:
- `id="login-username"` on the username input
- `id="login-password"` on the password input
- `id="login-error"` on a div/p below the button for error text
- The submit button calls `submitLogin()` on click (not form submit)

---

## BUG 3 — All tabs disabled after login

### Problem
After login the tabs (Dashboard, Assets, Add Asset, Users) are not clickable or content is empty.

### Fix — index.html
The showApp() function must:
1. Hide login div, show app div
2. Call showTab('dashboard') immediately
3. showTab() must hide ALL tab content divs, then show the correct one AND call the load function

Replace showTab with:
```javascript
function showTab(name) {
    // Update tab button styles
    document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'))
    const activeTab = document.querySelector(`.tab[data-tab="${name}"]`)
    if (activeTab) activeTab.classList.add('active')

    // Hide all content panels
    document.querySelectorAll('.tab-content').forEach(el => el.style.display = 'none')

    // Show selected panel
    const panel = document.getElementById(`tab-${name}`)
    if (panel) panel.style.display = 'block'

    // Load data for the tab
    if (name === 'dashboard') loadDashboard()
    if (name === 'assets')    loadAssets()
    if (name === 'users')     loadUsers()
}
```

Make sure every tab button has `data-tab="dashboard"` (or assets/add-asset/users) attribute
and calls `showTab(this.dataset.tab)` on click.

Make sure every tab content div has:
- class `tab-content`
- id matching `tab-dashboard`, `tab-assets`, `tab-add-asset`, `tab-users`
- `style="display:none"` by default (showTab will reveal them)

---

## BUG 4 — Certificate PEM upload does not auto-detect expiry date

### Problem
User pastes PEM in Add Asset form, expiry date field stays empty.

### Fix — index.html
Add a client-side PEM parser. When the PEM textarea changes, parse the cert
and populate the expires_at field automatically.

Add this JavaScript function:
```javascript
function parsePemExpiry(pem) {
    // Extract base64 from PEM
    const b64 = pem
        .replace(/-----BEGIN CERTIFICATE-----/, '')
        .replace(/-----END CERTIFICATE-----/, '')
        .replace(/\s+/g, '')
    
    try {
        // Decode base64 to binary
        const der = atob(b64)
        const bytes = new Uint8Array(der.length)
        for (let i = 0; i < der.length; i++) bytes[i] = der.charCodeAt(i)
        
        // ASN.1 DER parse — find the validity sequence
        // TBSCertificate > validity > notAfter
        // We scan for UTCTime (0x17) or GeneralizedTime (0x18) tags
        // The second occurrence of these tags is notAfter
        let count = 0
        let i = 0
        while (i < bytes.length - 2) {
            if (bytes[i] === 0x17 || bytes[i] === 0x18) {
                count++
                if (count === 2) {
                    // This is notAfter
                    const len = bytes[i + 1]
                    const str = String.fromCharCode(...bytes.slice(i + 2, i + 2 + len))
                    // UTCTime format: YYMMDDHHMMSSZ
                    // GeneralizedTime: YYYYMMDDHHMMSSZ
                    let year, month, day
                    if (bytes[i] === 0x17) {
                        // UTCTime — 2-digit year, >= 50 means 1900s else 2000s
                        const yy = parseInt(str.substring(0, 2))
                        year = yy >= 50 ? 1900 + yy : 2000 + yy
                        month = str.substring(2, 4)
                        day = str.substring(4, 6)
                    } else {
                        // GeneralizedTime — 4-digit year
                        year = str.substring(0, 4)
                        month = str.substring(4, 6)
                        day = str.substring(6, 8)
                    }
                    return `${year}-${month}-${day}`
                }
            }
            // Skip past this TLV
            const tag = bytes[i]
            let length = bytes[i + 1]
            if (length & 0x80) {
                const numBytes = length & 0x7f
                length = 0
                for (let j = 0; j < numBytes; j++) {
                    length = (length << 8) | bytes[i + 2 + j]
                }
                i += numBytes
            }
            i += 2 + length
        }
    } catch(e) {
        console.error('PEM parse error:', e)
    }
    return null
}
```

Wire it up — on the PEM textarea's `input` event:
```javascript
document.getElementById('asset-pem').addEventListener('input', function() {
    const date = parsePemExpiry(this.value)
    if (date) {
        document.getElementById('asset-expires').value = date
        document.getElementById('pem-hint').textContent = '✓ Expiry detected: ' + date
        document.getElementById('pem-hint').style.color = '#16a34a'
    }
})
```

Make sure the PEM textarea has `id="asset-pem"`, the date input has `id="asset-expires"`,
and there is a small `<span id="pem-hint"></span>` below the PEM field.

Also: when type changes to "certificate", show the PEM field; otherwise hide it.
When type changes away from "certificate", clear the PEM field and expiry date.

```javascript
document.getElementById('asset-type').addEventListener('change', function() {
    const isCert = this.value === 'certificate'
    document.getElementById('pem-group').style.display = isCert ? 'block' : 'none'
    if (!isCert) {
        document.getElementById('asset-pem').value = ''
        document.getElementById('pem-hint').textContent = ''
    }
})
```

Wrap the PEM textarea in a div with `id="pem-group"` that starts hidden:
`<div id="pem-group" style="display:none">`

---

## BUG 5 — Assets and Dashboard show nothing after creating an asset

### Problem
User creates an asset, goes to Dashboard or Assets tab, sees empty table.

### Root cause A — API response shape mismatch
The Go handler returns `{"data": {"assets": [...], "total": N}, "error": null}`
but the JS reads `data.data.assets` or maybe just `data.assets`. Verify the exact shape.

### Fix — index.html loadAssets()
```javascript
async function loadAssets() {
    const errEl = document.getElementById('assets-error')
    try {
        const r = await fetch('/api/v1/assets?limit=100', {
            headers: { Authorization: `Bearer ${token}` }
        })
        if (!r.ok) {
            const d = await r.json()
            if (errEl) errEl.textContent = d.error || 'Failed to load assets'
            return
        }
        const data = await r.json()
        // Handle both response shapes
        const assets = data.data?.assets ?? data.data ?? data.assets ?? []
        renderAssetsTable(assets)
        return assets
    } catch(e) {
        if (errEl) errEl.textContent = 'Network error loading assets'
        console.error(e)
        return []
    }
}
```

### Fix — index.html loadDashboard()
```javascript
async function loadDashboard() {
    const assets = await loadAssets()  // reuse, returns array
    if (!assets) return

    const total    = assets.length
    const expiring = assets.filter(a => {
        const d = daysUntil(a.expires_at)
        return d >= 0 && d <= 30
    }).length
    const critical = assets.filter(a => {
        const d = daysUntil(a.expires_at)
        return d >= 0 && d <= 7
    }).length
    const expired  = assets.filter(a => daysUntil(a.expires_at) < 0).length

    document.getElementById('stat-total').textContent    = total
    document.getElementById('stat-expiring').textContent = expiring
    document.getElementById('stat-critical').textContent = critical
    document.getElementById('stat-expired').textContent  = expired

    // Top 10 soonest expiring
    const soon = [...assets]
        .sort((a, b) => new Date(a.expires_at) - new Date(b.expires_at))
        .slice(0, 10)
    
    const tbody = document.getElementById('dashboard-table-body')
    if (!tbody) return
    tbody.innerHTML = soon.map(a => {
        const days = daysUntil(a.expires_at)
        return `<tr>
            <td>${a.name}</td>
            <td>${a.type}</td>
            <td>${formatDate(a.expires_at)}</td>
            <td style="color:${dayColor(days)}">${daysLabel(days)}</td>
        </tr>`
    }).join('')
}
```

### Fix — renderAssetsTable()
```javascript
function renderAssetsTable(assets) {
    const tbody = document.getElementById('assets-table-body')
    if (!tbody) return
    if (!assets || assets.length === 0) {
        tbody.innerHTML = '<tr><td colspan="6" style="text-align:center;color:#94a3b8;padding:32px">No assets yet. Add one using the Add Asset tab.</td></tr>'
        return
    }
    tbody.innerHTML = assets.map(a => {
        const days = daysUntil(a.expires_at)
        const rowBg = days < 0 ? '#fef2f2' : days <= 7 ? '#fff7ed' : days <= 30 ? '#fefce8' : 'white'
        return `<tr style="background:${rowBg}">
            <td>${a.name}</td>
            <td>${a.type}</td>
            <td>${formatDate(a.expires_at)}</td>
            <td style="color:${dayColor(days)};font-weight:600">${daysLabel(days)}</td>
            <td><span class="badge ${statusBadgeClass(days)}">${statusLabel(days)}</span></td>
            <td>
                <button class="btn btn-sm" onclick="editAsset('${a.id}')">Edit</button>
                <button class="btn btn-sm btn-danger" onclick="deleteAsset('${a.id}','${a.name}')">Delete</button>
            </td>
        </tr>`
    }).join('')
}
```

### Fix — helper functions (add these if missing)
```javascript
function daysUntil(dateStr) {
    const ms = new Date(dateStr) - new Date()
    return Math.floor(ms / 86400000)
}

function formatDate(dateStr) {
    return new Date(dateStr).toLocaleDateString('en-GB', {
        day:'2-digit', month:'short', year:'numeric'
    })
}

function dayColor(days) {
    if (days < 0)   return '#dc2626'
    if (days <= 7)  return '#ea580c'
    if (days <= 30) return '#ca8a04'
    return '#16a34a'
}

function daysLabel(days) {
    if (days < 0)  return `Expired ${Math.abs(days)}d ago`
    if (days === 0) return 'Expires today'
    return `${days}d left`
}

function statusLabel(days) {
    if (days < 0)   return 'Expired'
    if (days <= 30) return 'Expiring'
    return 'Valid'
}

function statusBadgeClass(days) {
    if (days < 0)   return 'badge-red'
    if (days <= 7)  return 'badge-orange'
    if (days <= 30) return 'badge-yellow'
    return 'badge-green'
}
```

### Fix — submitAsset() must reload after success
```javascript
async function submitAsset() {
    // ... collect form values ...
    const payload = {
        name:        document.getElementById('asset-name').value.trim(),
        type:        document.getElementById('asset-type').value,
        expires_at:  document.getElementById('asset-expires').value
                        ? new Date(document.getElementById('asset-expires').value).toISOString()
                        : undefined,
        description: document.getElementById('asset-desc').value.trim(),
        tags:        parseTags(document.getElementById('asset-tags').value),
        metadata:    document.getElementById('asset-pem').value
                        ? { pem: document.getElementById('asset-pem').value }
                        : {}
    }

    if (!payload.name) {
        document.getElementById('asset-form-error').textContent = 'Name is required'
        return
    }
    if (!payload.type) {
        document.getElementById('asset-form-error').textContent = 'Type is required'
        return
    }

    const r = await fetch('/api/v1/assets', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            Authorization: `Bearer ${token}`
        },
        body: JSON.stringify(payload)
    })
    const data = await r.json()
    if (!r.ok || data.error) {
        document.getElementById('asset-form-error').textContent = data.error || 'Failed to create asset'
        return
    }

    // Success — reset form and switch to assets tab
    document.getElementById('add-asset-form').reset()
    document.getElementById('asset-form-success').textContent = 'Asset created!'
    document.getElementById('pem-group').style.display = 'none'
    setTimeout(() => {
        document.getElementById('asset-form-success').textContent = ''
        showTab('assets')
    }, 1200)
}

function parseTags(str) {
    const tags = {}
    if (!str.trim()) return tags
    str.split(',').forEach(pair => {
        const [k, v] = pair.split('=')
        if (k && v) tags[k.trim()] = v.trim()
    })
    return tags
}
```

Add to the Add Asset form HTML:
- `id="add-asset-form"` on the form element
- `id="asset-name"`, `id="asset-type"`, `id="asset-expires"`, `id="asset-desc"`, `id="asset-tags"`
- `id="asset-form-error"` div for errors
- `id="asset-form-success"` div for success message

---

## BUG 6 — Users tab shows nothing / is broken

### Fix — loadUsers()
```javascript
async function loadUsers() {
    const r = await fetch('/api/v1/users', {
        headers: { Authorization: `Bearer ${token}` }
    })
    if (!r.ok) return
    const data = await r.json()
    const users = data.data ?? []

    const tbody = document.getElementById('users-table-body')
    if (!tbody) return
    if (users.length === 0) {
        tbody.innerHTML = '<tr><td colspan="4" style="text-align:center;color:#94a3b8;padding:32px">No users found.</td></tr>'
        return
    }
    tbody.innerHTML = users.map(u => `<tr>
        <td>${u.username}</td>
        <td>${u.email}</td>
        <td><span class="badge badge-${u.auth_method === 'local' ? 'blue' : u.auth_method === 'oidc' ? 'purple' : 'green'}">${u.auth_method}</span></td>
        <td>${u.last_login ? formatDate(u.last_login) : 'Never'}</td>
    </tr>`).join('')
}
```

Add `.badge-blue { background:#dbeafe;color:#1d4ed8 }` and `.badge-purple { background:#f3e8ff;color:#7c3aed }` to the CSS.

Also fix internal/handler/user.go — ListUsers currently returns an empty stub.
Add ListUsers to the store interface and implement it properly:

Add to internal/store/store.go:
```go
ListUsers(ctx context.Context) ([]*model.User, error)
```

Add to internal/store/postgres.go:
```go
func (s *PostgresStore) ListUsers(ctx context.Context) ([]*model.User, error) {
    rows, err := s.pool.Query(ctx,
        "SELECT id,username,email,password_hash,auth_method,created_at,last_login FROM users ORDER BY created_at ASC")
    if err != nil {
        return nil, fmt.Errorf("store.ListUsers: %w", err)
    }
    defer rows.Close()
    var users []*model.User
    for rows.Next() {
        u, err := scanUser(rows)
        if err != nil { return nil, err }
        users = append(users, u)
    }
    return users, nil
}
```

Update internal/handler/user.go ListUsers to use the store:
```go
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
    users, err := h.store.ListUsers(r.Context())
    if err != nil {
        writeError(w, http.StatusInternalServerError, "failed to list users")
        return
    }
    if users == nil { users = []*model.User{} }
    writeJSON(w, http.StatusOK, users)
}
```

---

## Final check
1. Run `go build ./...` — fix all errors
2. Run `docker compose -f docker-compose.dev.yml up --build -d`
3. Open http://localhost:8080 — verify it redirects to /setup
4. Complete setup, login, create a test asset, verify it appears in Assets and Dashboard tabs
5. Report any remaining errors
