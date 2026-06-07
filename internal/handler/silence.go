package handler

import (
	"encoding/json"
	"net/http"
	"time"
	"vaultwatch/internal/auth"
	"vaultwatch/internal/model"
	"vaultwatch/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type SilenceHandler struct {
	store store.Store
}

func NewSilenceHandler(s store.Store) *SilenceHandler {
	return &SilenceHandler{store: s}
}

// POST /api/v1/assets/{id}/silence
func (h *SilenceHandler) Silence(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid asset id")
		return
	}
	var req struct {
		Note      string `json:"note"`
		ExpiresIn string `json:"expires_in"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	claims := auth.GetClaims(r)
	silencedBy := ""
	if claims != nil {
		silencedBy = claims.Email
	}

	var expiresAt *time.Time
	if req.ExpiresIn != "" {
		d, err := time.ParseDuration(req.ExpiresIn)
		if err == nil {
			t := time.Now().Add(d)
			expiresAt = &t
		}
	}

	if err := h.store.SilenceAsset(r.Context(), id, silencedBy, req.Note, expiresAt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to silence asset")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "silenced"})
}

// DELETE /api/v1/assets/{id}/silence
func (h *SilenceHandler) Unsilence(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid asset id")
		return
	}
	if err := h.store.UnsilenceAsset(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unsilence asset")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unsilenced"})
}

// GET /api/v1/silences
func (h *SilenceHandler) List(w http.ResponseWriter, r *http.Request) {
	silences, err := h.store.ListSilences(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list silences")
		return
	}
	if silences == nil {
		silences = []*model.Silence{}
	}
	writeJSON(w, http.StatusOK, silences)
}
