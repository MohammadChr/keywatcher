package handler

import (
	"encoding/json"
	"net/http"
	"time"
	"vaultwatch/internal/auth"
	"vaultwatch/internal/model"
	"vaultwatch/internal/store"
	"vaultwatch/config"
	"github.com/google/uuid"
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

	// Try local first, then LDAP
	for _, method := range h.cfg.AuthMethods {
		switch method {
		case "local":
			user, _ = auth.AuthenticateLocal(r.Context(), h.store, req.Username, req.Password)
		case "ldap":
			if h.ldap != nil {
				user, _ = h.ldap.Authenticate(r.Context(), h.store, req.Username, req.Password)
			}
		}
		if user != nil {
			break
		}
	}

	if user == nil {
		// Always return generic message — don't expose internal details or reveal username exists (Fix 3.5)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	_ = h.store.UpdateLastLogin(r.Context(), user.ID)

	token, err := auth.IssueToken(user, h.cfg.JWTSecret, 8*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "an internal error occurred")
		return
	}

	// Set secure cookies with proper flags (Fix 3.2)
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
		MaxAge:   8 * 3600,
	})

	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "session", MaxAge: -1, Path: "/"})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

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

func (h *AuthHandler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	if h.oidc == nil {
		writeError(w, http.StatusNotFound, "OIDC not enabled")
		return
	}
	code  := r.URL.Query().Get("code")
	nonceCookie, _ := r.Cookie("oidc_nonce")
	nonce := ""
	if nonceCookie != nil { nonce = nonceCookie.Value }
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
