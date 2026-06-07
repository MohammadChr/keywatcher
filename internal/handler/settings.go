package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
	"vaultwatch/internal/notify"
	"vaultwatch/internal/store"

	"github.com/rs/zerolog/log"
)

type SettingsHandler struct {
	store store.Store
}

func NewSettingsHandler(s store.Store) *SettingsHandler {
	return &SettingsHandler{store: s}
}

type AuthSettings struct {
	OIDCEnabled      bool   `json:"oidc_enabled"`
	OIDCIssuer       string `json:"oidc_issuer"`
	OIDCClientID     string `json:"oidc_client_id"`
	OIDCClientSecret string `json:"oidc_client_secret,omitempty"`
	LDAPEnabled      bool   `json:"ldap_enabled"`
	LDAPURL          string `json:"ldap_url"`
	LDAPBindDN       string `json:"ldap_bind_dn"`
	LDAPBindPassword string `json:"ldap_bind_password,omitempty"`
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
		OIDCEnabled:      s["auth_oidc_enabled"] == "true",
		OIDCIssuer:       s["auth_oidc_issuer"],
		OIDCClientID:     s["auth_oidc_client_id"],
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
		"auth_oidc_enabled":      boolStr(req.OIDCEnabled),
		"auth_oidc_issuer":       req.OIDCIssuer,
		"auth_oidc_client_id":    req.OIDCClientID,
		"auth_ldap_enabled":      boolStr(req.LDAPEnabled),
		"auth_ldap_url":          req.LDAPURL,
		"auth_ldap_bind_dn":      req.LDAPBindDN,
		"auth_ldap_base_dn":      req.LDAPBaseDN,
		"auth_ldap_user_filter":  req.LDAPUserFilter,
	}
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
	if s == "" {
		return ""
	}
	return "••••••••"
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Alert settings
type AlertSettings struct {
	WarningDays  int    `json:"warning_days"`
	CriticalDays int    `json:"critical_days"`
	Interval     string `json:"interval"`

	SlackWebhook  string `json:"slack_webhook"`
	SlackToken    string `json:"slack_token"`
	SlackChannel  string `json:"slack_channel"`

	MattermostWebhook string `json:"mattermost_webhook"`
	MattermostToken   string `json:"mattermost_token"`
	MattermostChannel string `json:"mattermost_channel"`

	WebhookURL string `json:"webhook_url"`

	TelegramToken  string `json:"telegram_token"`
	TelegramChatID string `json:"telegram_chat_id"`

	TemplateWarning  string `json:"template_warning"`
	TemplateCritical string `json:"template_critical"`
	TemplateExpired  string `json:"template_expired"`
}

func (h *SettingsHandler) GetAlerts(w http.ResponseWriter, r *http.Request) {
	s, err := h.store.GetAllSettings(r.Context(), "alert_")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load alert settings")
		return
	}
	warningDays, _  := strconv.Atoi(s["alert_warning_days"])
	criticalDays, _ := strconv.Atoi(s["alert_critical_days"])
	if warningDays == 0  { warningDays = 30 }
	if criticalDays == 0 { criticalDays = 7 }

	writeJSON(w, http.StatusOK, AlertSettings{
		WarningDays:       warningDays,
		CriticalDays:      criticalDays,
		Interval:          s["alert_interval"],
		SlackWebhook:      s["alert_slack_webhook"],
		SlackToken:        maskSecret(s["alert_slack_token"]),
		SlackChannel:      s["alert_slack_channel"],
		MattermostWebhook: s["alert_mattermost_webhook"],
		MattermostToken:   maskSecret(s["alert_mattermost_token"]),
		MattermostChannel: s["alert_mattermost_channel"],
		WebhookURL:        s["alert_webhook_url"],
		TelegramToken:     maskSecret(s["alert_telegram_token"]),
		TelegramChatID:    s["alert_telegram_chat_id"],
		TemplateWarning:   s["alert_template_warning"],
		TemplateCritical:  s["alert_template_critical"],
		TemplateExpired:   s["alert_template_expired"],
	})
}

func (h *SettingsHandler) UpdateAlerts(w http.ResponseWriter, r *http.Request) {
	var req AlertSettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	ctx := r.Context()
	saves := map[string]string{
		"alert_warning_days":      strconv.Itoa(req.WarningDays),
		"alert_critical_days":     strconv.Itoa(req.CriticalDays),
		"alert_interval":          req.Interval,
		"alert_slack_webhook":     req.SlackWebhook,
		"alert_slack_channel":     req.SlackChannel,
		"alert_mattermost_webhook": req.MattermostWebhook,
		"alert_mattermost_channel": req.MattermostChannel,
		"alert_webhook_url":       req.WebhookURL,
		"alert_telegram_chat_id":  req.TelegramChatID,
		"alert_template_warning":  req.TemplateWarning,
		"alert_template_critical": req.TemplateCritical,
		"alert_template_expired":  req.TemplateExpired,
	}
	// Only overwrite masked secrets if user typed a new value
	if req.SlackToken != "••••••••"        { saves["alert_slack_token"] = req.SlackToken }
	if req.MattermostToken != "••••••••"   { saves["alert_mattermost_token"] = req.MattermostToken }
	if req.TelegramToken != "••••••••"     { saves["alert_telegram_token"] = req.TelegramToken }

	for k, v := range saves {
		if err := h.store.SetSetting(ctx, k, v); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save "+k)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (h *SettingsHandler) TestAlert(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Error().Interface("panic", rec).Msg("TestAlert panic recovered")
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("internal error: %v", rec))
		}
	}()

	var req struct {
		Channel string `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	// 10 second timeout — never block forever
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	s, err := h.store.GetAllSettings(ctx, "alert_")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load settings")
		return
	}

	// Load templates
	templates := &notify.Templates{
		Warning:  s["alert_template_warning"],
		Critical: s["alert_template_critical"],
		Expired:  s["alert_template_expired"],
	}
	if templates.Warning == "" {
		templates.Warning = notify.DefaultTemplates.Warning
	}
	if templates.Critical == "" {
		templates.Critical = notify.DefaultTemplates.Critical
	}
	if templates.Expired == "" {
		templates.Expired = notify.DefaultTemplates.Expired
	}

	msg := notify.Message{
		Title:     "🧪 Test Alert from KeyWatcher",
		AssetName: "test-certificate",
		AssetType: "certificate",
		DaysLeft:  7,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		Tags:      map[string]string{"env": "test"},
		Severity:  notify.SeverityCritical,
	}
	msg.Body = templates.Render(msg)

	var sendErr error
	switch req.Channel {
	case "slack":
		webhook := s["alert_slack_webhook"]
		if webhook == "" {
			writeError(w, http.StatusBadRequest, "Slack webhook URL is not configured")
			return
		}
		sendErr = notify.NewSlack(webhook).Send(ctx, msg)

	case "mattermost":
		webhook := s["alert_mattermost_webhook"]
		if webhook == "" {
			writeError(w, http.StatusBadRequest, "Mattermost webhook URL is not configured")
			return
		}
		sendErr = notify.NewMattermost(webhook).Send(ctx, msg)

	case "webhook":
		url := s["alert_webhook_url"]
		if url == "" {
			writeError(w, http.StatusBadRequest, "Webhook URL is not configured")
			return
		}
		sendErr = notify.NewWebhook(url).Send(ctx, msg)

	case "telegram":
		botToken := s["alert_telegram_token"]
		chatID := s["alert_telegram_chat_id"]
		if botToken == "" || chatID == "" {
			writeError(w, http.StatusBadRequest, "Telegram bot token and chat ID are required")
			return
		}
		sendErr = notify.NewTelegram(botToken, chatID).Send(ctx, msg)

	default:
		writeError(w, http.StatusBadRequest, "unknown channel: "+req.Channel)
		return
	}

	if sendErr != nil {
		writeError(w, http.StatusBadGateway, "test failed: "+sendErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}
