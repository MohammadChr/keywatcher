package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"keywatcher/config"
	"keywatcher/internal/auth"
	"keywatcher/internal/model"
	"keywatcher/internal/store"
)

type SetupHandler struct {
	store store.Store
	cfg   *config.Config
}

func NewSetupHandler(s store.Store, cfg *config.Config) *SetupHandler {
	return &SetupHandler{store: s, cfg: cfg}
}

// GET /setup/status — called by UI to check if setup is needed
func (h *SetupHandler) Status(w http.ResponseWriter, r *http.Request) {
	done, err := h.store.IsSetupCompleted(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot check setup status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"setup_completed": done})
}

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

// POST /setup — create the first admin user
func (h *SetupHandler) Complete(w http.ResponseWriter, r *http.Request) {
	// Block if already set up
	done, _ := h.store.IsSetupCompleted(r.Context())
	if done {
		writeError(w, http.StatusForbidden, "setup already completed")
		return
	}

	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Validate setup input strictly (Fix 10.2)
	if len(req.Username) < 3 || len(req.Username) > 50 {
		writeError(w, http.StatusBadRequest, "username must be 3-50 characters")
		return
	}
	if len(req.Password) < 12 {
		writeError(w, http.StatusBadRequest, "password must be at least 12 characters")
		return
	}
	if len(req.Email) == 0 || req.Email[len(req.Email)-1] == '@' {
		// Simple email validation
		hasAt := false
		for _, c := range req.Email {
			if c == '@' {
				hasAt = true
			}
		}
		if !hasAt {
			writeError(w, http.StatusBadRequest, "invalid email address")
			return
		}
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	user := &model.User{
		ID:           uuid.New(),
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: &hash,
		AuthMethod:   model.AuthMethodLocal,
		Role:         model.RoleAdmin,
		IsRoot:       true,
	}
	err = h.store.CreateUser(r.Context(), user)
	if err != nil {
		// Never expose internal errors (Fix 5.1)
		log.Error().Err(err).Msg("failed to create setup user")
		writeError(w, http.StatusInternalServerError, "failed to complete setup")
		return
	}

	if err := h.store.CompleteSetup(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete setup")
		return
	}

	// Issue token so user is immediately logged in after setup
	user, _ = h.store.GetUserByUsername(r.Context(), req.Username)
	token, _ := auth.IssueToken(user, h.cfg.JWTSecret, 8*time.Hour)

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "setup completed",
		"token":  token,
	})
}
