# Agent: Bug Fix Round 2

Fix ONLY what is listed. Do not touch anything else.
All fixes are in internal/server/static/index.html unless stated otherwise.

---

## BUG 1 — Asset Edit button does not work

The editAsset() function is missing or broken. Add/replace it:

```javascript
async function editAsset(id) {
    // Fetch the asset
    const r = await fetch(`/api/v1/assets/${id}`, {
        headers: { Authorization: `Bearer ${token}` }
    })
    const data = await r.json()
    const a = data.data
    if (!a) { alert('Could not load asset'); return }

    // Build a simple inline edit modal
    const overlay = document.createElement('div')
    overlay.id = 'edit-overlay'
    overlay.style.cssText = `
        position:fixed;inset:0;background:rgba(0,0,0,0.5);
        display:flex;align-items:center;justify-content:center;z-index:1000`

    const tagsStr = Object.entries(a.tags || {}).map(([k,v]) => `${k}=${v}`).join(',')
    const expiryDate = a.expires_at ? a.expires_at.substring(0,10) : ''

    overlay.innerHTML = `
        <div style="background:white;border-radius:12px;padding:32px;width:480px;max-width:90vw">
            <h3 style="margin-bottom:20px;font-size:18px">Edit Asset</h3>
            <div class="form-group"><label>Name</label>
                <input id="edit-name" value="${a.name}"></div>
            <div class="form-group"><label>Type</label>
                <select id="edit-type">
                    ${['certificate','token','api_key','secret','custom'].map(t =>
                        `<option ${a.type===t?'selected':''}>${t}</option>`).join('')}
                </select></div>
            <div class="form-group"><label>Expires At</label>
                <input id="edit-expires" type="date" value="${expiryDate}"></div>
            <div class="form-group"><label>Description</label>
                <textarea id="edit-desc">${a.description||''}</textarea></div>
            <div class="form-group"><label>Tags (key=value,key=value)</label>
                <input id="edit-tags" value="${tagsStr}"></div>
            <div id="edit-error" class="error"></div>
            <div style="display:flex;gap:8px;margin-top:16px">
                <button class="btn btn-primary" onclick="submitEdit('${id}')">Save</button>
                <button class="btn" onclick="document.getElementById('edit-overlay').remove()"
                    style="background:#f1f5f9">Cancel</button>
            </div>
        </div>`

    document.body.appendChild(overlay)
    // Close on backdrop click
    overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove() })
}

async function submitEdit(id) {
    const payload = {
        name:        document.getElementById('edit-name').value.trim(),
        type:        document.getElementById('edit-type').value,
        expires_at:  new Date(document.getElementById('edit-expires').value).toISOString(),
        description: document.getElementById('edit-desc').value.trim(),
        tags:        parseTags(document.getElementById('edit-tags').value)
    }
    const r = await fetch(`/api/v1/assets/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type':'application/json', Authorization:`Bearer ${token}` },
        body: JSON.stringify(payload)
    })
    const data = await r.json()
    if (!r.ok || data.error) {
        document.getElementById('edit-error').textContent = data.error || 'Save failed'
        return
    }
    document.getElementById('edit-overlay').remove()
    loadAssets()
}
```

Also fix deleteAsset() — it must use a confirm dialog, not browser confirm():
```javascript
function deleteAsset(id, name) {
    const overlay = document.createElement('div')
    overlay.id = 'delete-overlay'
    overlay.style.cssText = `position:fixed;inset:0;background:rgba(0,0,0,0.5);
        display:flex;align-items:center;justify-content:center;z-index:1000`
    overlay.innerHTML = `
        <div style="background:white;border-radius:12px;padding:32px;width:380px;text-align:center">
            <p style="margin-bottom:8px;font-size:16px;font-weight:600">Delete Asset?</p>
            <p style="color:#64748b;margin-bottom:24px">"${name}" will be permanently deleted.</p>
            <div style="display:flex;gap:8px;justify-content:center">
                <button class="btn btn-danger" onclick="confirmDelete('${id}')">Delete</button>
                <button class="btn" onclick="document.getElementById('delete-overlay').remove()"
                    style="background:#f1f5f9">Cancel</button>
            </div>
        </div>`
    document.body.appendChild(overlay)
}

async function confirmDelete(id) {
    const r = await fetch(`/api/v1/assets/${id}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` }
    })
    document.getElementById('delete-overlay')?.remove()
    if (r.ok) loadAssets()
    else alert('Delete failed')
}
```

---

## BUG 2 — Users tab: "Add User" button/form does not work

The Users tab must have a working Create User form. Replace the entire Users tab HTML with:

```html
<div id="tab-users" class="tab-content" style="display:none">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:20px">
        <h2 style="font-size:18px;font-weight:600">Users</h2>
        <button class="btn btn-primary" onclick="toggleCreateUser()">+ Add User</button>
    </div>

    <!-- Create user form (hidden by default) -->
    <div id="create-user-form" style="display:none;background:white;border:1px solid #e2e8f0;
         border-radius:8px;padding:24px;margin-bottom:20px;max-width:480px">
        <h3 style="margin-bottom:16px;font-size:15px;font-weight:600">Create Local User</h3>
        <div class="form-group"><label>Username</label>
            <input id="new-username" placeholder="john"></div>
        <div class="form-group"><label>Email</label>
            <input id="new-email" type="email" placeholder="john@example.com"></div>
        <div class="form-group"><label>Password</label>
            <input id="new-password" type="password" placeholder="min 8 characters"></div>
        <div id="create-user-error" class="error"></div>
        <div id="create-user-success" class="success"></div>
        <div style="display:flex;gap:8px;margin-top:4px">
            <button class="btn btn-primary" onclick="submitCreateUser()">Create</button>
            <button class="btn" onclick="toggleCreateUser()"
                style="background:#f1f5f9">Cancel</button>
        </div>
    </div>

    <table>
        <thead>
            <tr>
                <th>Username</th><th>Email</th>
                <th>Auth Method</th><th>Last Login</th>
            </tr>
        </thead>
        <tbody id="users-table-body">
            <tr><td colspan="4" style="text-align:center;color:#94a3b8;padding:32px">
                Loading...</td></tr>
        </tbody>
    </table>
</div>
```

Add these JS functions:
```javascript
function toggleCreateUser() {
    const form = document.getElementById('create-user-form')
    form.style.display = form.style.display === 'none' ? 'block' : 'none'
    document.getElementById('create-user-error').textContent = ''
    document.getElementById('create-user-success').textContent = ''
}

async function submitCreateUser() {
    const username = document.getElementById('new-username').value.trim()
    const email    = document.getElementById('new-email').value.trim()
    const password = document.getElementById('new-password').value
    const errEl    = document.getElementById('create-user-error')
    const okEl     = document.getElementById('create-user-success')

    errEl.textContent = ''
    okEl.textContent  = ''

    if (!username || !email || !password) {
        errEl.textContent = 'All fields are required'
        return
    }
    if (password.length < 8) {
        errEl.textContent = 'Password must be at least 8 characters'
        return
    }

    const r = await fetch('/api/v1/users', {
        method: 'POST',
        headers: { 'Content-Type':'application/json', Authorization:`Bearer ${token}` },
        body: JSON.stringify({ username, email, password })
    })
    const data = await r.json()
    if (!r.ok || data.error) {
        errEl.textContent = data.error || 'Failed to create user'
        return
    }
    okEl.textContent = `User "${username}" created successfully`
    document.getElementById('new-username').value = ''
    document.getElementById('new-email').value    = ''
    document.getElementById('new-password').value = ''
    setTimeout(() => {
        toggleCreateUser()
        loadUsers()
    }, 1500)
}
```

---

## BUG 3 — Certificate upload: file picker instead of textarea

Replace the PEM textarea in the Add Asset form with a proper file upload input
that reads the file content AND parses the expiry client-side.

Replace the pem-group div with:
```html
<div id="pem-group" style="display:none">
    <div class="form-group">
        <label>Upload Certificate File (.pem, .crt, .cer)</label>
        <input id="asset-pem-file" type="file" accept=".pem,.crt,.cer,.cert"
               style="padding:8px;border:2px dashed #cbd5e1;border-radius:6px;
                      background:#f8fafc;cursor:pointer">
        <p style="font-size:12px;color:#64748b;margin-top:4px">
            Or paste PEM text below
        </p>
    </div>
    <div class="form-group">
        <label>PEM Certificate (paste)</label>
        <textarea id="asset-pem" rows="6"
                  placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"></textarea>
    </div>
    <div id="pem-hint" style="font-size:13px;margin-top:-8px;margin-bottom:12px"></div>
</div>
```

Update the JS — wire file upload to auto-read and parse:
```javascript
// File upload handler
document.getElementById('asset-pem-file').addEventListener('change', function() {
    const file = this.files[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = function(e) {
        const pem = e.target.result
        document.getElementById('asset-pem').value = pem
        tryParsePem(pem)
    }
    reader.readAsText(file)
})

// Textarea paste handler
document.getElementById('asset-pem').addEventListener('input', function() {
    tryParsePem(this.value)
})

function tryParsePem(pem) {
    const hintEl = document.getElementById('pem-hint')
    if (!pem.trim()) { hintEl.textContent = ''; return }
    const date = parsePemExpiry(pem)
    if (date) {
        document.getElementById('asset-expires').value = date
        hintEl.textContent = '✓ Expiry auto-detected: ' + date
        hintEl.style.color = '#16a34a'
    } else {
        hintEl.textContent = '⚠ Could not detect expiry — please enter manually'
        hintEl.style.color = '#ea580c'
    }
}
```

Also update submitAsset() to include the PEM in metadata when present:
```javascript
const pemValue = document.getElementById('asset-pem').value.trim()
const payload = {
    name:        document.getElementById('asset-name').value.trim(),
    type:        document.getElementById('asset-type').value,
    expires_at:  document.getElementById('asset-expires').value
                    ? new Date(document.getElementById('asset-expires').value + 'T00:00:00').toISOString()
                    : undefined,
    description: document.getElementById('asset-desc').value.trim(),
    tags:        parseTags(document.getElementById('asset-tags').value),
    metadata:    pemValue ? { pem: pemValue } : {}
}
```

---

## BUG 4 — OIDC login button missing from login screen

Add an OIDC login button to the login page. It only shows if the backend has OIDC enabled.
On page load (inside checkSetup, after confirming setup is done), call GET /setup/status
and also check if OIDC is available:

Add to Go — internal/handler/setup.go, new endpoint GET /auth/methods:
```go
func (h *SetupHandler) AuthMethods(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, http.StatusOK, map[string]any{
        "methods": h.cfg.AuthMethods,
        "oidc_login_url": func() string {
            for _, m := range h.cfg.AuthMethods {
                if m == "oidc" {
                    return "/api/v1/auth/oidc/login"
                }
            }
            return ""
        }(),
    })
}
```

Register in server.go (public, no auth needed):
```
GET /api/v1/auth/methods → setupHandler.AuthMethods
```

Also add GET /api/v1/auth/oidc/login to auth handler that redirects to OIDC provider:
```go
func (h *AuthHandler) OIDCLogin(w http.ResponseWriter, r *http.Request) {
    if h.oidc == nil {
        writeError(w, http.StatusNotFound, "OIDC not enabled")
        return
    }
    state := uuid.New().String()
    nonce := uuid.New().String()
    http.SetCookie(w, &http.Cookie{
        Name: "oidc_nonce", Value: nonce,
        HttpOnly: true, SameSite: http.SameSiteLaxMode,
        Path: "/", MaxAge: 600,
    })
    http.SetCookie(w, &http.Cookie{
        Name: "oidc_state", Value: state,
        HttpOnly: true, SameSite: http.SameSiteLaxMode,
        Path: "/", MaxAge: 600,
    })
    http.Redirect(w, r, h.oidc.AuthCodeURL(state, nonce), http.StatusFound)
}
```

Register: GET /api/v1/auth/oidc/login → authHandler.OIDCLogin (public)

In the OIDC callback, read nonce from cookie:
```go
nonceCookie, _ := r.Cookie("oidc_nonce")
nonce := ""
if nonceCookie != nil { nonce = nonceCookie.Value }
user, err := h.oidc.Exchange(r.Context(), h.store, code, nonce)
```

In index.html, after checking setup, load auth methods:
```javascript
async function loadAuthMethods() {
    try {
        const r = await fetch('/api/v1/auth/methods')
        const data = await r.json()
        const methods = data.data?.methods ?? []
        const oidcUrl = data.data?.oidc_login_url ?? ''

        const oidcBtn = document.getElementById('oidc-login-btn')
        if (oidcBtn) {
            if (oidcUrl) {
                oidcBtn.style.display = 'block'
                oidcBtn.onclick = () => window.location.href = oidcUrl
            } else {
                oidcBtn.style.display = 'none'
            }
        }
    } catch(e) { /* OIDC not configured, hide button */ }
}
```

Add to login card HTML (after the login button):
```html
<div style="text-align:center;margin-top:12px;color:#94a3b8;font-size:13px">or</div>
<button id="oidc-login-btn" class="btn" onclick=""
    style="display:none;width:100%;background:#f1f5f9;margin-top:8px;padding:10px">
    🔐 Sign in with SSO (OIDC)
</button>
```

Call loadAuthMethods() at the end of checkSetup() when showing the login screen.

---

## Final check
1. Run `go build ./...` — fix all compile errors
2. Run `docker compose -f docker-compose.dev.yml up --build -d`
3. Test:
   - Open http://localhost:8080
   - Create an asset → verify it appears in Assets tab and Dashboard
   - Click Edit → verify modal opens with correct data, saves correctly
   - Click Delete → verify confirm dialog appears, asset removed after confirm
   - Go to Users tab → verify Add User form works
   - Upload a .crt file in Add Asset → verify expiry is auto-detected
4. Report results
