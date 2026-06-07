# Agent: Viewer Restriction + User Management Fix

Two precise fixes only. Do not touch anything else.

---

## FIX 1 — Viewer sees ONLY the Dashboard tab

In index.html, find the showApp() function.
After login, read the role from the JWT and hide tabs accordingly.

Replace the tab-hiding logic with this exact code inside showApp():

```javascript
function showApp() {
    document.getElementById('login-screen').style.display  = 'none'
    document.getElementById('setup-screen').style.display  = 'none'
    document.getElementById('app').style.display = 'block'

    // Decode role from JWT
    window.currentUserRole = getRoleFromToken(token)
    const isAdmin = window.currentUserRole === 'admin'

    // Show/hide tabs based on role
    const adminOnlyTabs = ['assets', 'add-asset', 'users', 'settings']
    adminOnlyTabs.forEach(tabName => {
        const el = document.querySelector(`.tab[data-tab="${tabName}"]`)
        if (el) el.style.display = isAdmin ? '' : 'none'
    })

    // Always start on dashboard
    showTab('dashboard')
}
```

Make sure getRoleFromToken() is defined:
```javascript
function getRoleFromToken(jwt) {
    try {
        const payload = JSON.parse(atob(jwt.split('.')[1]))
        return payload.role || 'viewer'
    } catch(e) { return 'viewer' }
}
```

Also: if a viewer somehow navigates to a hidden tab via showTab(),
add a guard at the top of showTab():
```javascript
function showTab(name) {
    const adminOnly = ['assets', 'add-asset', 'users', 'settings']
    if (adminOnly.includes(name) && window.currentUserRole !== 'admin') {
        name = 'dashboard'
    }
    // ... rest of existing showTab code unchanged
}
```

---

## FIX 2 — Admin can edit user details, change role, and delete user

### Backend — add DELETE and PUT endpoints

In internal/handler/user.go add two handlers:

```go
// PUT /api/v1/users/{id} — edit username and email
func (h *UserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
    id, err := uuid.Parse(chi.URLParam(r, "id"))
    if err != nil {
        writeError(w, http.StatusBadRequest, "invalid id")
        return
    }

    var req struct {
        Username string `json:"username"`
        Email    string `json:"email"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid body")
        return
    }
    if req.Username == "" || req.Email == "" {
        writeError(w, http.StatusBadRequest, "username and email required")
        return
    }
    if err := h.store.UpdateUser(r.Context(), id, req.Username, req.Email); err != nil {
        writeError(w, http.StatusInternalServerError, "failed to update user")
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// DELETE /api/v1/users/{id} — delete user
func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
    id, err := uuid.Parse(chi.URLParam(r, "id"))
    if err != nil {
        writeError(w, http.StatusBadRequest, "invalid id")
        return
    }

    // Prevent self-delete
    claims := auth.GetClaims(r)
    if claims != nil && claims.UserID == id {
        writeError(w, http.StatusBadRequest, "cannot delete your own account")
        return
    }

    if err := h.store.DeleteUser(r.Context(), id); err != nil {
        writeError(w, http.StatusInternalServerError, "failed to delete user")
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
```

Add to store interface (internal/store/store.go):
```go
UpdateUser(ctx context.Context, id uuid.UUID, username, email string) error
DeleteUser(ctx context.Context, id uuid.UUID) error
```

Add to internal/store/postgres.go:
```go
func (s *PostgresStore) UpdateUser(ctx context.Context, id uuid.UUID, username, email string) error {
    _, err := s.pool.Exec(ctx,
        "UPDATE users SET username=@username, email=@email WHERE id=@id",
        pgx.NamedArgs{"username": username, "email": email, "id": id})
    if err != nil {
        return fmt.Errorf("store.UpdateUser: %w", err)
    }
    return nil
}

func (s *PostgresStore) DeleteUser(ctx context.Context, id uuid.UUID) error {
    _, err := s.pool.Exec(ctx,
        "DELETE FROM users WHERE id=@id",
        pgx.NamedArgs{"id": id})
    if err != nil {
        return fmt.Errorf("store.DeleteUser: %w", err)
    }
    return nil
}
```

Register in server.go (inside admin-only group):
```
PUT    /api/v1/users/{id}       → userHandler.UpdateUser
DELETE /api/v1/users/{id}       → userHandler.DeleteUser
```

### Frontend — Users table with Edit, Role toggle, Delete

Replace the users table row rendering inside loadUsers() with:

```javascript
tbody.innerHTML = users.map(u => {
    const isSelf = token && getRoleFromToken(token) &&
        (() => { try { return JSON.parse(atob(token.split('.')[1])).uid === u.id } catch(e){return false} })()
    return `<tr>
        <td><strong>${u.username}</strong></td>
        <td>${u.email}</td>
        <td><span class="badge badge-${u.auth_method==='local'?'blue':u.auth_method==='oidc'?'purple':'green'}">${u.auth_method}</span></td>
        <td><span class="badge badge-${u.role==='admin'?'orange':'gray'}">${u.role||'viewer'}</span></td>
        <td>${u.last_login ? formatDate(u.last_login) : 'Never'}</td>
        <td style="display:flex;gap:6px;flex-wrap:wrap">
            <button class="btn btn-sm" onclick="editUser('${u.id}','${u.username}','${u.email}')">Edit</button>
            <button class="btn btn-sm" style="background:#f1f5f9"
                onclick="toggleRole('${u.id}','${u.role||'viewer'}')"
                ${isSelf ? 'disabled title="Cannot change own role"' : ''}>
                ${u.role==='admin' ? '→ Viewer' : '→ Admin'}
            </button>
            <button class="btn btn-sm btn-danger"
                onclick="deleteUser('${u.id}','${u.username}')"
                ${isSelf ? 'disabled title="Cannot delete yourself"' : ''}>Delete</button>
        </td>
    </tr>`
}).join('')
```

Update the Users table headers to:
```html
<tr>
    <th>Username</th><th>Email</th><th>Auth</th>
    <th>Role</th><th>Last Login</th><th>Actions</th>
</tr>
```

Add these JS functions:

```javascript
function editUser(id, currentUsername, currentEmail) {
    const overlay = document.createElement('div')
    overlay.id = 'edit-user-overlay'
    overlay.style.cssText = `position:fixed;inset:0;background:rgba(0,0,0,0.5);
        display:flex;align-items:center;justify-content:center;z-index:1000`
    overlay.innerHTML = `
        <div style="background:white;border-radius:12px;padding:32px;width:420px;max-width:92vw">
            <h3 style="margin-bottom:20px;font-size:18px;font-weight:600">Edit User</h3>
            <div class="form-group"><label>Username</label>
                <input id="eu-username" value="${currentUsername}"></div>
            <div class="form-group"><label>Email</label>
                <input id="eu-email" type="email" value="${currentEmail}"></div>
            <div id="eu-error" class="error" style="margin-bottom:8px"></div>
            <div style="display:flex;gap:8px;margin-top:16px">
                <button class="btn btn-primary" onclick="submitEditUser('${id}')">Save</button>
                <button class="btn" style="background:#f1f5f9"
                    onclick="document.getElementById('edit-user-overlay').remove()">Cancel</button>
            </div>
        </div>`
    document.body.appendChild(overlay)
    overlay.addEventListener('click', e => { if(e.target===overlay) overlay.remove() })
}

async function submitEditUser(id) {
    const username = document.getElementById('eu-username').value.trim()
    const email    = document.getElementById('eu-email').value.trim()
    const errEl    = document.getElementById('eu-error')
    if (!username || !email) { errEl.textContent = 'All fields required'; return }

    const r = await fetch(`/api/v1/users/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type':'application/json', Authorization:`Bearer ${token}` },
        body: JSON.stringify({ username, email })
    })
    const data = await r.json()
    if (!r.ok || data.error) { errEl.textContent = data.error || 'Failed to save'; return }
    document.getElementById('edit-user-overlay').remove()
    loadUsers()
}

function deleteUser(id, username) {
    const overlay = document.createElement('div')
    overlay.id = 'delete-user-overlay'
    overlay.style.cssText = `position:fixed;inset:0;background:rgba(0,0,0,0.5);
        display:flex;align-items:center;justify-content:center;z-index:1000`
    overlay.innerHTML = `
        <div style="background:white;border-radius:12px;padding:32px;width:360px;text-align:center">
            <p style="font-size:16px;font-weight:600;margin-bottom:8px">Delete User?</p>
            <p style="color:#64748b;margin-bottom:24px">"${username}" will be permanently removed.</p>
            <div style="display:flex;gap:8px;justify-content:center">
                <button class="btn btn-danger" onclick="confirmDeleteUser('${id}')">Delete</button>
                <button class="btn" style="background:#f1f5f9"
                    onclick="document.getElementById('delete-user-overlay').remove()">Cancel</button>
            </div>
        </div>`
    document.body.appendChild(overlay)
}

async function confirmDeleteUser(id) {
    const r = await fetch(`/api/v1/users/${id}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` }
    })
    document.getElementById('delete-user-overlay')?.remove()
    if (r.ok) loadUsers()
    else alert('Delete failed')
}
```

---

## Final check
1. Run `go build ./...` — fix all errors
2. Run `docker compose -f docker-compose.dev.yml up --build -d`
3. Test as admin: all 5 tabs visible, Users tab shows Edit/Role/Delete buttons
4. Test as viewer: only Dashboard tab visible
5. Report results
