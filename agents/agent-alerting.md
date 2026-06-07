# Agent: Alerting Settings & Silence

Read CLAUDE.md first. Do not touch anything not mentioned here.

---

## FEATURE 1 — DB migrations

### 008_alerting.up.sql
```sql
-- Alerting global settings (reuse app_settings table)
INSERT INTO app_settings (key, value) VALUES
    ('alert_warning_days',     '30'),
    ('alert_critical_days',    '7'),
    ('alert_interval',         '1h'),
    ('alert_slack_webhook',    ''),
    ('alert_slack_token',      ''),
    ('alert_slack_channel',    ''),
    ('alert_mattermost_webhook',''),
    ('alert_mattermost_token', ''),
    ('alert_mattermost_channel',''),
    ('alert_webhook_url',      ''),
    ('alert_telegram_token',   ''),
    ('alert_telegram_chat_id', '')
ON CONFLICT (key) DO NOTHING;

-- Silence table
CREATE TABLE IF NOT EXISTS alert_silences (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id   UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    silenced_by TEXT NOT NULL,
    silenced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ,          -- NULL = silenced forever until unsilenced
    note        TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_silence_asset ON alert_silences(asset_id);
```

### 008_alerting.down.sql
```sql
DROP TABLE IF EXISTS alert_silences;
DELETE FROM app_settings WHERE key LIKE 'alert_%';
```

---

## FEATURE 2 — Store methods

Add to internal/store/store.go:
```go
// Silence
SilenceAsset(ctx context.Context, assetID uuid.UUID, silencedBy string, note string, expiresAt *time.Time) error
UnsilenceAsset(ctx context.Context, assetID uuid.UUID) error
IsAssetSilenced(ctx context.Context, assetID uuid.UUID) (bool, error)
ListSilences(ctx context.Context) ([]*model.Silence, error)
```

Add to internal/model/asset.go:
```go
type Silence struct {
    ID         uuid.UUID  `json:"id"`
    AssetID    uuid.UUID  `json:"asset_id"`
    SilencedBy string     `json:"silenced_by"`
    SilencedAt time.Time  `json:"silenced_at"`
    ExpiresAt  *time.Time `json:"expires_at"`
    Note       string     `json:"note"`
}
```

Add to internal/store/postgres.go:
```go
func (s *PostgresStore) SilenceAsset(ctx context.Context, assetID uuid.UUID, silencedBy, note string, expiresAt *time.Time) error {
    _, err := s.pool.Exec(ctx, `
        INSERT INTO alert_silences (asset_id, silenced_by, note, expires_at)
        VALUES (@asset_id, @silenced_by, @note, @expires_at)
        ON CONFLICT (asset_id) DO UPDATE
            SET silenced_by=@silenced_by, silenced_at=NOW(), note=@note, expires_at=@expires_at`,
        pgx.NamedArgs{
            "asset_id": assetID, "silenced_by": silencedBy,
            "note": note, "expires_at": expiresAt,
        })
    return err
}

func (s *PostgresStore) UnsilenceAsset(ctx context.Context, assetID uuid.UUID) error {
    _, err := s.pool.Exec(ctx,
        "DELETE FROM alert_silences WHERE asset_id=@id",
        pgx.NamedArgs{"id": assetID})
    return err
}

func (s *PostgresStore) IsAssetSilenced(ctx context.Context, assetID uuid.UUID) (bool, error) {
    var exists bool
    err := s.pool.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1 FROM alert_silences
            WHERE asset_id=@id
            AND (expires_at IS NULL OR expires_at > NOW())
        )`, pgx.NamedArgs{"id": assetID}).Scan(&exists)
    return exists, err
}

func (s *PostgresStore) ListSilences(ctx context.Context) ([]*model.Silence, error) {
    rows, err := s.pool.Query(ctx, `
        SELECT id, asset_id, silenced_by, silenced_at, expires_at, note
        FROM alert_silences
        WHERE expires_at IS NULL OR expires_at > NOW()
        ORDER BY silenced_at DESC`)
    if err != nil { return nil, err }
    defer rows.Close()
    var silences []*model.Silence
    for rows.Next() {
        var si model.Silence
        if err := rows.Scan(&si.ID, &si.AssetID, &si.SilencedBy,
            &si.SilencedAt, &si.ExpiresAt, &si.Note); err != nil {
            return nil, err
        }
        silences = append(silences, &si)
    }
    return silences, nil
}
```

---

## FEATURE 3 — Alerting config handler

Add to internal/handler/settings.go:

```go
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
    var req struct {
        Channel string `json:"channel"` // slack | mattermost | webhook | telegram
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid body")
        return
    }
    ctx := r.Context()
    s, _ := h.store.GetAllSettings(ctx, "alert_")

    msg := notify.Message{
        Title:     "🧪 Test Alert from KeyWatcher",
        AssetName: "test-certificate",
        AssetType: "certificate",
        DaysLeft:  7,
        ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
        Tags:      map[string]string{"env": "test"},
        Severity:  notify.SeverityCritical,
    }

    var err error
    switch req.Channel {
    case "slack":
        if s["alert_slack_webhook"] == "" {
            writeError(w, http.StatusBadRequest, "Slack webhook not configured")
            return
        }
        n := notify.NewSlack(s["alert_slack_webhook"])
        err = n.Send(ctx, msg)
    case "mattermost":
        if s["alert_mattermost_webhook"] == "" {
            writeError(w, http.StatusBadRequest, "Mattermost webhook not configured")
            return
        }
        n := notify.NewMattermost(s["alert_mattermost_webhook"])
        err = n.Send(ctx, msg)
    case "webhook":
        if s["alert_webhook_url"] == "" {
            writeError(w, http.StatusBadRequest, "Webhook URL not configured")
            return
        }
        n := notify.NewWebhook(s["alert_webhook_url"])
        err = n.Send(ctx, msg)
    case "telegram":
        if s["alert_telegram_token"] == "" || s["alert_telegram_chat_id"] == "" {
            writeError(w, http.StatusBadRequest, "Telegram not configured")
            return
        }
        n := notify.NewTelegram(s["alert_telegram_token"], s["alert_telegram_chat_id"])
        err = n.Send(ctx, msg)
    default:
        writeError(w, http.StatusBadRequest, "unknown channel: "+req.Channel)
        return
    }

    if err != nil {
        writeError(w, http.StatusBadGateway, "test failed: "+err.Error())
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}
```
Add imports: `"strconv"` `"time"` `"keywatcher/internal/notify"`

---

## FEATURE 4 — Telegram notifier

Create internal/notify/telegram.go:
```go
package notify

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"
)

type TelegramNotifier struct {
    token  string
    chatID string
    client *http.Client
}

func NewTelegram(token, chatID string) *TelegramNotifier {
    return &TelegramNotifier{
        token:  token,
        chatID: chatID,
        client: &http.Client{Timeout: 10 * time.Second},
    }
}

func (t *TelegramNotifier) Name() string { return "telegram" }

func (t *TelegramNotifier) Send(ctx context.Context, msg Message) error {
    text := fmt.Sprintf(
        "%s *%s*\n\n*Asset:* %s\n*Type:* %s\n*Days left:* %d\n*Expires:* %s\n*Severity:* %s",
        msg.SeverityEmoji(), msg.Title,
        msg.AssetName, msg.AssetType, msg.DaysLeft,
        msg.ExpiresAt.Format("2006-01-02"),
        string(msg.Severity),
    )
    payload := map[string]any{
        "chat_id":    t.chatID,
        "text":       text,
        "parse_mode": "Markdown",
    }
    body, _ := json.Marshal(payload)
    url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
    if err != nil { return fmt.Errorf("telegram.Send: %w", err) }
    req.Header.Set("Content-Type", "application/json")
    resp, err := t.client.Do(req)
    if err != nil { return fmt.Errorf("telegram.Send: %w", err) }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        return fmt.Errorf("telegram.Send: status %d", resp.StatusCode)
    }
    return nil
}
```

---

## FEATURE 5 — Silence handlers

Create internal/handler/silence.go:
```go
package handler

import (
    "encoding/json"
    "net/http"
    "time"
    "keywatcher/internal/auth"
    "keywatcher/internal/store"
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
        ExpiresIn string `json:"expires_in"` // e.g. "24h", "7d", "" = forever
    }
    json.NewDecoder(r.Body).Decode(&req)

    claims := auth.GetClaims(r)
    silencedBy := ""
    if claims != nil { silencedBy = claims.Email }

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
    if silences == nil { silences = []*model.Silence{} }
    writeJSON(w, http.StatusOK, silences)
}
```
Add import `"keywatcher/internal/model"`

---

## FEATURE 6 — Register new routes in server.go

Add to main.go:
```go
silenceHandler  := handler.NewSilenceHandler(store)
```

Register in server.go inside admin-only group:
```
GET  /api/v1/settings/alerts      → settingsHandler.GetAlerts
PUT  /api/v1/settings/alerts      → settingsHandler.UpdateAlerts
POST /api/v1/settings/alerts/test → settingsHandler.TestAlert
GET  /api/v1/silences             → silenceHandler.List
POST /api/v1/assets/{id}/silence  → silenceHandler.Silence
DELETE /api/v1/assets/{id}/silence → silenceHandler.Unsilence
```

---

## FEATURE 7 — Update expiry checker to use DB alert settings + respect silence

In internal/expiry/checker.go, update the run() function to load alert settings from DB:

Add a store reference and load settings at the start of each run():
```go
func (c *Checker) run(ctx context.Context) {
    timer := prom.NewTimer(metrics.CheckDuration)
    defer timer.ObserveDuration()

    // Load current alert settings from DB
    settings, err := c.store.GetAllSettings(ctx, "alert_")
    if err != nil {
        log.Error().Err(err).Msg("checker: failed to load alert settings")
    }
    // Parse warn days from settings
    warnDays := parseWarnDays(settings["alert_warning_days"], settings["alert_critical_days"])

    assets, err := c.store.ListAllActive(ctx)
    if err != nil {
        log.Error().Err(err).Msg("checker: failed to list assets")
        return
    }
    metrics.UpdateMetrics(assets)
    for _, a := range assets {
        c.checkAsset(ctx, a, warnDays)
    }
}

func parseWarnDays(warningStr, criticalStr string) []int {
    warning, err := strconv.Atoi(warningStr)
    if err != nil || warning == 0 { warning = 30 }
    critical, err := strconv.Atoi(criticalStr)
    if err != nil || critical == 0 { critical = 7 }
    return []int{warning, critical, 1}
}
```

Update checkAsset() to check silence before sending:
```go
func (c *Checker) checkAsset(ctx context.Context, a *model.Asset, warnDays []int) {
    // Check if silenced
    silenced, err := c.store.IsAssetSilenced(ctx, a.ID)
    if err == nil && silenced {
        return // skip — asset is silenced
    }
    // ... rest of existing checkAsset logic unchanged
}
```

---

## FEATURE 8 — Settings UI: Alerting tab

In index.html, add a second settings section inside the Settings tab,
BELOW the Auth Methods section:

```html
<!-- Alerting Settings -->
<div style="background:white;border:1px solid #e2e8f0;border-radius:8px;padding:24px;margin-bottom:20px">
    <h3 style="font-size:15px;font-weight:600;margin-bottom:4px">Alert Rules</h3>
    <p style="color:#64748b;font-size:13px;margin-bottom:20px">
        Configure when alerts fire and how often to check.
    </p>
    <div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:16px">
        <div class="form-group">
            <label>⚠ Warning threshold (days)</label>
            <input id="alert-warning-days" type="number" min="1" placeholder="30">
        </div>
        <div class="form-group">
            <label>🚨 Critical threshold (days)</label>
            <input id="alert-critical-days" type="number" min="1" placeholder="7">
        </div>
        <div class="form-group">
            <label>Check interval</label>
            <select id="alert-interval">
                <option value="15m">Every 15 minutes</option>
                <option value="30m">Every 30 minutes</option>
                <option value="1h">Every hour</option>
                <option value="6h">Every 6 hours</option>
                <option value="24h">Every 24 hours</option>
            </select>
        </div>
    </div>
</div>

<!-- Notification Channels -->
<div style="background:white;border:1px solid #e2e8f0;border-radius:8px;padding:24px;margin-bottom:20px">
    <h3 style="font-size:15px;font-weight:600;margin-bottom:4px">Notification Channels</h3>
    <p style="color:#64748b;font-size:13px;margin-bottom:20px">
        Configure one or more channels. All enabled channels receive alerts.
    </p>

    <!-- Slack -->
    <div style="border:1px solid #e2e8f0;border-radius:8px;padding:16px;margin-bottom:12px">
        <div style="font-weight:600;margin-bottom:12px">Slack</div>
        <div class="form-group"><label>Incoming Webhook URL</label>
            <input id="alert-slack-webhook" placeholder="https://hooks.slack.com/services/..."></div>
        <div class="form-group"><label>Bot Token (optional, for bot messages)</label>
            <input id="alert-slack-token" type="password" placeholder="xoxb-..."></div>
        <div class="form-group"><label>Channel (optional)</label>
            <input id="alert-slack-channel" placeholder="#alerts"></div>
        <button class="btn btn-sm" style="background:#f1f5f9"
            onclick="testAlert('slack')">Send Test</button>
        <span id="test-slack-result" style="font-size:13px;margin-left:8px"></span>
    </div>

    <!-- Mattermost -->
    <div style="border:1px solid #e2e8f0;border-radius:8px;padding:16px;margin-bottom:12px">
        <div style="font-weight:600;margin-bottom:12px">Mattermost</div>
        <div class="form-group"><label>Incoming Webhook URL</label>
            <input id="alert-mattermost-webhook" placeholder="https://mattermost.example.com/hooks/..."></div>
        <div class="form-group"><label>Bot Token (optional)</label>
            <input id="alert-mattermost-token" type="password" placeholder="token..."></div>
        <div class="form-group"><label>Channel (optional)</label>
            <input id="alert-mattermost-channel" placeholder="alerts"></div>
        <button class="btn btn-sm" style="background:#f1f5f9"
            onclick="testAlert('mattermost')">Send Test</button>
        <span id="test-mattermost-result" style="font-size:13px;margin-left:8px"></span>
    </div>

    <!-- Telegram -->
    <div style="border:1px solid #e2e8f0;border-radius:8px;padding:16px;margin-bottom:12px">
        <div style="font-weight:600;margin-bottom:12px">Telegram</div>
        <p style="font-size:12px;color:#64748b;margin-bottom:12px">
            Create a bot via @BotFather, add it to your channel, then paste the token and chat ID below.
        </p>
        <div class="form-group"><label>Bot Token</label>
            <input id="alert-telegram-token" type="password" placeholder="123456:ABC-DEF..."></div>
        <div class="form-group"><label>Chat ID</label>
            <input id="alert-telegram-chat-id" placeholder="-1001234567890"></div>
        <button class="btn btn-sm" style="background:#f1f5f9"
            onclick="testAlert('telegram')">Send Test</button>
        <span id="test-telegram-result" style="font-size:13px;margin-left:8px"></span>
    </div>

    <!-- Generic Webhook -->
    <div style="border:1px solid #e2e8f0;border-radius:8px;padding:16px">
        <div style="font-weight:600;margin-bottom:12px">Generic Webhook</div>
        <p style="font-size:12px;color:#64748b;margin-bottom:12px">
            POSTs a JSON payload to any URL. Compatible with PagerDuty, OpsGenie, custom endpoints.
        </p>
        <div class="form-group"><label>Webhook URL</label>
            <input id="alert-webhook-url" placeholder="https://my.service.com/alert"></div>
        <button class="btn btn-sm" style="background:#f1f5f9"
            onclick="testAlert('webhook')">Send Test</button>
        <span id="test-webhook-result" style="font-size:13px;margin-left:8px"></span>
    </div>
</div>

<div id="alert-settings-error" class="error" style="margin-bottom:8px"></div>
<div id="alert-settings-success" class="success" style="margin-bottom:8px"></div>
<button class="btn btn-primary" onclick="saveAlertSettings()">Save Alert Settings</button>
```

---

## FEATURE 9 — Alert settings JS

```javascript
async function loadAlertSettings() {
    try {
        const r = await fetch('/api/v1/settings/alerts', {
            headers: { Authorization: `Bearer ${token}` }
        })
        if (!r.ok) return
        const data = await r.json()
        const s = data.data

        document.getElementById('alert-warning-days').value  = s.warning_days  || 30
        document.getElementById('alert-critical-days').value = s.critical_days || 7

        const intervalEl = document.getElementById('alert-interval')
        if (intervalEl) intervalEl.value = s.interval || '1h'

        document.getElementById('alert-slack-webhook').value     = s.slack_webhook     || ''
        document.getElementById('alert-slack-channel').value     = s.slack_channel     || ''
        document.getElementById('alert-mattermost-webhook').value = s.mattermost_webhook || ''
        document.getElementById('alert-mattermost-channel').value = s.mattermost_channel || ''
        document.getElementById('alert-webhook-url').value       = s.webhook_url       || ''
        document.getElementById('alert-telegram-chat-id').value  = s.telegram_chat_id  || ''

        // Masked secrets — show placeholder
        const maskedFields = [
            ['alert-slack-token',       s.slack_token],
            ['alert-mattermost-token',  s.mattermost_token],
            ['alert-telegram-token',    s.telegram_token],
        ]
        maskedFields.forEach(([id, val]) => {
            const el = document.getElementById(id)
            if (el) el.placeholder = val ? 'leave blank to keep existing' : 'not set'
        })
    } catch(e) { console.error('loadAlertSettings:', e) }
}

async function saveAlertSettings() {
    const errEl = document.getElementById('alert-settings-error')
    const okEl  = document.getElementById('alert-settings-success')
    errEl.textContent = ''
    okEl.textContent  = ''

    const payload = {
        warning_days:        parseInt(document.getElementById('alert-warning-days').value)  || 30,
        critical_days:       parseInt(document.getElementById('alert-critical-days').value) || 7,
        interval:            document.getElementById('alert-interval').value,
        slack_webhook:       document.getElementById('alert-slack-webhook').value.trim(),
        slack_token:         document.getElementById('alert-slack-token').value,
        slack_channel:       document.getElementById('alert-slack-channel').value.trim(),
        mattermost_webhook:  document.getElementById('alert-mattermost-webhook').value.trim(),
        mattermost_token:    document.getElementById('alert-mattermost-token').value,
        mattermost_channel:  document.getElementById('alert-mattermost-channel').value.trim(),
        webhook_url:         document.getElementById('alert-webhook-url').value.trim(),
        telegram_token:      document.getElementById('alert-telegram-token').value,
        telegram_chat_id:    document.getElementById('alert-telegram-chat-id').value.trim(),
    }

    const r = await fetch('/api/v1/settings/alerts', {
        method: 'PUT',
        headers: { 'Content-Type':'application/json', Authorization:`Bearer ${token}` },
        body: JSON.stringify(payload)
    })
    const data = await r.json()
    if (!r.ok || data.error) {
        errEl.textContent = data.error || 'Failed to save'
        return
    }
    okEl.textContent = '✓ Alert settings saved'
    setTimeout(() => { okEl.textContent = '' }, 3000)
}

async function testAlert(channel) {
    const resultEl = document.getElementById(`test-${channel}-result`)
    if (resultEl) { resultEl.textContent = 'Sending...'; resultEl.style.color = '#64748b' }

    const r = await fetch('/api/v1/settings/alerts/test', {
        method: 'POST',
        headers: { 'Content-Type':'application/json', Authorization:`Bearer ${token}` },
        body: JSON.stringify({ channel })
    })
    const data = await r.json()
    if (resultEl) {
        if (r.ok && !data.error) {
            resultEl.textContent = '✓ Sent!'
            resultEl.style.color = '#16a34a'
        } else {
            resultEl.textContent = '✗ ' + (data.error || 'Failed')
            resultEl.style.color = '#dc2626'
        }
        setTimeout(() => { resultEl.textContent = '' }, 4000)
    }
}
```

Call `loadAlertSettings()` inside `loadSettings()` at the end.

---

## FEATURE 10 — Silence button in Assets table

In renderAssetsTable(), add a Silence/Unsilence button to each row.
The asset object needs a silenced field — update ListAssets in Go to join silences:

In internal/store/postgres.go, update ListAllActive and ListAssets queries
to LEFT JOIN alert_silences and return is_silenced bool:

```sql
SELECT a.id, a.name, a.type, a.expires_at, a.description, a.tags, a.metadata,
       a.created_at, a.updated_at, a.created_by, a.deleted_at,
       CASE WHEN s.asset_id IS NOT NULL
            AND (s.expires_at IS NULL OR s.expires_at > NOW())
            THEN true ELSE false END as is_silenced
FROM assets a
LEFT JOIN alert_silences s ON s.asset_id = a.id
WHERE a.deleted_at IS NULL
ORDER BY a.expires_at ASC
```

Add IsSilenced bool to model.Asset:
```go
IsSilenced bool `json:"is_silenced" db:"is_silenced"`
```

Update scanAsset() to scan the extra column:
```go
err := row.Scan(
    &a.ID, &a.Name, &a.Type, &a.ExpiresAt, &a.Description,
    &tagsRaw, &metaRaw, &a.CreatedAt, &a.UpdatedAt, &a.CreatedBy,
    &a.DeletedAt, &a.IsSilenced,
)
```

In index.html renderAssetsTable(), add silence button to actions:
```javascript
const silenceBtn = window.currentUserRole === 'admin'
    ? a.is_silenced
        ? `<button class="btn btn-sm" style="background:#fef9c3;color:#854d0e"
               onclick="unsilenceAsset('${a.id}')">🔔 Unsilence</button>`
        : `<button class="btn btn-sm" style="background:#f1f5f9"
               onclick="silenceAsset('${a.id}','${a.name}')">🔕 Silence</button>`
    : ''
```

Add silence/unsilence functions:
```javascript
function silenceAsset(id, name) {
    const overlay = document.createElement('div')
    overlay.id = 'silence-overlay'
    overlay.style.cssText = `position:fixed;inset:0;background:rgba(0,0,0,0.5);
        display:flex;align-items:center;justify-content:center;z-index:1000`
    overlay.innerHTML = `
        <div style="background:white;border-radius:12px;padding:32px;width:420px;max-width:92vw">
            <h3 style="margin-bottom:8px;font-size:18px;font-weight:600">🔕 Silence Alerts</h3>
            <p style="color:#64748b;font-size:13px;margin-bottom:16px">
                No alerts will be sent for "<strong>${name}</strong>" while silenced.
            </p>
            <div class="form-group"><label>Duration</label>
                <select id="silence-duration">
                    <option value="">Until manually unsilenced</option>
                    <option value="1h">1 hour</option>
                    <option value="24h">24 hours</option>
                    <option value="168h">7 days</option>
                    <option value="720h">30 days</option>
                </select>
            </div>
            <div class="form-group"><label>Note (optional)</label>
                <input id="silence-note" placeholder="e.g. renewal in progress"></div>
            <div style="display:flex;gap:8px;margin-top:16px">
                <button class="btn btn-primary" onclick="confirmSilence('${id}')">Silence</button>
                <button class="btn" style="background:#f1f5f9"
                    onclick="document.getElementById('silence-overlay').remove()">Cancel</button>
            </div>
        </div>`
    document.body.appendChild(overlay)
    overlay.addEventListener('click', e => { if(e.target===overlay) overlay.remove() })
}

async function confirmSilence(id) {
    const duration = document.getElementById('silence-duration').value
    const note     = document.getElementById('silence-note').value.trim()
    const r = await fetch(`/api/v1/assets/${id}/silence`, {
        method: 'POST',
        headers: { 'Content-Type':'application/json', Authorization:`Bearer ${token}` },
        body: JSON.stringify({ expires_in: duration, note })
    })
    document.getElementById('silence-overlay')?.remove()
    if (r.ok) loadAssets()
    else alert('Failed to silence asset')
}

async function unsilenceAsset(id) {
    const r = await fetch(`/api/v1/assets/${id}/silence`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` }
    })
    if (r.ok) loadAssets()
    else alert('Failed to unsilence asset')
}
```

---

## Final check
1. Run migration:
   docker compose -f docker-compose.dev.yml exec postgres psql -U keywatcher -d keywatcher -f /dev/stdin < internal/store/migrations/008_alerting.up.sql
2. Run: go build ./...
3. Run: docker compose -f docker-compose.dev.yml up --build -d
4. Test:
   - Settings tab shows Alerting section with all 4 channels
   - "Send Test" button works for each configured channel
   - Assets table shows 🔕 Silence button per row
   - Silenced assets show 🔔 Unsilence button in yellow
   - Silenced assets are skipped by expiry checker
5. Report results

---

## FEATURE 11 — Alert Templates

### Step 1 — DB migration (009_alert_templates.up.sql)

Create internal/store/migrations/009_alert_templates.up.sql:
```sql
INSERT INTO app_settings (key, value) VALUES
    ('alert_template_warning',  '⚠️ *{asset_name}* is expiring in *{days_left} days*\nType: {asset_type}\nExpires: {expires_at}\nEnvironment: {env}'),
    ('alert_template_critical', '🚨 *{asset_name}* expires in *{days_left} days* — ACTION REQUIRED\nType: {asset_type}\nExpires: {expires_at}\nEnvironment: {env}'),
    ('alert_template_expired',  '☠️ *{asset_name}* has EXPIRED\nType: {asset_type}\nExpired: {expires_at}\nEnvironment: {env}')
ON CONFLICT (key) DO NOTHING;
```

Create 009_alert_templates.down.sql:
```sql
DELETE FROM app_settings WHERE key LIKE 'alert_template_%';
```

### Step 2 — Available template variables
```
{asset_name}  → asset name
{asset_type}  → certificate / token / api_key / secret / custom
{days_left}   → days until expiry (negative = already expired)
{expires_at}  → expiry date YYYY-MM-DD
{env}         → value of asset tag "env", or "unknown"
{severity}    → warning / critical / expired
{emoji}       → ⚠️ / 🚨 / ☠️
```

### Step 3 — Template renderer (internal/notify/template.go)
```go
package notify

import (
    "fmt"
    "strings"
)

type Templates struct {
    Warning  string
    Critical string
    Expired  string
}

var DefaultTemplates = Templates{
    Warning:  "⚠️ *{asset_name}* is expiring in *{days_left} days*\nType: {asset_type}\nExpires: {expires_at}\nEnvironment: {env}",
    Critical: "🚨 *{asset_name}* expires in *{days_left} days* — ACTION REQUIRED\nType: {asset_type}\nExpires: {expires_at}\nEnvironment: {env}",
    Expired:  "☠️ *{asset_name}* has EXPIRED\nType: {asset_type}\nExpired: {expires_at}\nEnvironment: {env}",
}

func (t *Templates) Render(msg Message) string {
    tmpl := t.Warning
    switch msg.Severity {
    case SeverityCritical:
        tmpl = t.Critical
    case SeverityExpired:
        tmpl = t.Expired
    }
    if tmpl == "" { tmpl = DefaultTemplates.Warning }

    env := msg.Tags["env"]
    if env == "" { env = "unknown" }

    r := strings.NewReplacer(
        "{asset_name}", msg.AssetName,
        "{asset_type}", msg.AssetType,
        "{days_left}",  fmt.Sprintf("%d", msg.DaysLeft),
        "{expires_at}", msg.ExpiresAt.Format("2006-01-02"),
        "{env}",        env,
        "{severity}",   string(msg.Severity),
        "{emoji}",      msg.SeverityEmoji(),
    )
    return r.Replace(tmpl)
}
```

### Step 4 — Add Body field to Message struct in internal/notify/notifier.go
```go
type Message struct {
    Title     string
    AssetName string
    AssetType string
    DaysLeft  int
    ExpiresAt time.Time
    Tags      map[string]string
    Severity  Severity
    Body      string  // pre-rendered template body
}
```

### Step 5 — Update all notifiers to use msg.Body

In internal/notify/slack.go Send():
```go
func (s *SlackNotifier) Send(ctx context.Context, msg Message) error {
    text := msg.Body
    if text == "" {
        text = fmt.Sprintf("%s %s\n*Asset:* %s | *Days left:* %d",
            msg.SeverityEmoji(), msg.Title, msg.AssetName, msg.DaysLeft)
    }
    payload := map[string]any{
        "attachments": []map[string]any{{
            "color": msg.ColorHex(),
            "blocks": []map[string]any{
                {"type": "section", "text": map[string]any{
                    "type": "mrkdwn", "text": text,
                }},
            },
        }},
    }
    return postJSON(ctx, s.client, s.webhookURL, payload)
}
```

In internal/notify/mattermost.go Send():
```go
func (m *MattermostNotifier) Send(ctx context.Context, msg Message) error {
    text := msg.Body
    if text == "" {
        text = fmt.Sprintf("%s %s — %s", msg.SeverityEmoji(), msg.AssetName, msg.Title)
    }
    return postJSON(ctx, m.client, m.webhookURL, map[string]any{"text": text})
}
```

In internal/notify/telegram.go Send():
```go
func (t *TelegramNotifier) Send(ctx context.Context, msg Message) error {
    text := msg.Body
    if text == "" {
        text = fmt.Sprintf("%s *%s*\n%s", msg.SeverityEmoji(), msg.Title, msg.AssetName)
    }
    payload := map[string]any{
        "chat_id":    t.chatID,
        "text":       text,
        "parse_mode": "Markdown",
    }
    body, _ := json.Marshal(payload)
    url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
    if err != nil { return fmt.Errorf("telegram.Send: %w", err) }
    req.Header.Set("Content-Type", "application/json")
    resp, err := t.client.Do(req)
    if err != nil { return fmt.Errorf("telegram.Send: %w", err) }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        return fmt.Errorf("telegram.Send: status %d", resp.StatusCode)
    }
    return nil
}
```

### Step 6 — Update expiry checker to load and use templates

In internal/expiry/checker.go update run() to load templates from DB:
```go
func (c *Checker) run(ctx context.Context) {
    timer := prom.NewTimer(metrics.CheckDuration)
    defer timer.ObserveDuration()

    settings, _ := c.store.GetAllSettings(ctx, "alert_")

    warnDays := parseWarnDays(settings["alert_warning_days"], settings["alert_critical_days"])

    templates := &notify.Templates{
        Warning:  settings["alert_template_warning"],
        Critical: settings["alert_template_critical"],
        Expired:  settings["alert_template_expired"],
    }
    if templates.Warning  == "" { templates.Warning  = notify.DefaultTemplates.Warning }
    if templates.Critical == "" { templates.Critical = notify.DefaultTemplates.Critical }
    if templates.Expired  == "" { templates.Expired  = notify.DefaultTemplates.Expired }

    assets, err := c.store.ListAllActive(ctx)
    if err != nil {
        log.Error().Err(err).Msg("checker: failed to list assets")
        return
    }
    metrics.UpdateMetrics(assets)
    for _, a := range assets {
        c.checkAsset(ctx, a, warnDays, templates)
    }
}
```

Update checkAsset() to accept templates and render body:
```go
func (c *Checker) checkAsset(ctx context.Context, a *model.Asset, warnDays []int, templates *notify.Templates) {
    silenced, err := c.store.IsAssetSilenced(ctx, a.ID)
    if err == nil && silenced { return }

    days := a.DaysUntilExpiry()
    threshold := 0
    for _, d := range warnDays {
        if days <= d { threshold = d }
    }
    if threshold == 0 && days > 0 { return }

    already, err := c.store.WasNotifiedRecently(ctx, a.ID, threshold, 23*time.Hour)
    if err != nil || already { return }

    severity := notify.SeverityWarning
    if days <= 0 { severity = notify.SeverityExpired }
    if days <= 7 { severity = notify.SeverityCritical }

    title := "Asset expiring soon"
    if severity == notify.SeverityExpired { title = "Asset has expired" }

    msg := notify.Message{
        Title:     title,
        AssetName: a.Name,
        AssetType: string(a.Type),
        DaysLeft:  days,
        ExpiresAt: a.ExpiresAt,
        Tags:      a.Tags,
        Severity:  severity,
    }
    msg.Body = templates.Render(msg)

    if err := c.notifier.Send(ctx, msg); err != nil {
        log.Error().Err(err).Str("asset", a.Name).Msg("notification failed")
        return
    }
    for _, name := range c.notifier.Names() {
        metrics.NotificationsSent.WithLabelValues(name).Inc()
        _ = c.store.LogNotification(ctx, a.ID, threshold, name)
    }
    log.Info().Str("asset", a.Name).Int("days_left", days).Msg("notification sent")
}
```

### Step 7 — Update AlertSettings struct and handler

In internal/handler/settings.go add to AlertSettings:
```go
TemplateWarning  string `json:"template_warning"`
TemplateCritical string `json:"template_critical"`
TemplateExpired  string `json:"template_expired"`
```

In GetAlerts() add to return value:
```go
TemplateWarning:  s["alert_template_warning"],
TemplateCritical: s["alert_template_critical"],
TemplateExpired:  s["alert_template_expired"],
```

In UpdateAlerts() add to saves map:
```go
"alert_template_warning":  req.TemplateWarning,
"alert_template_critical": req.TemplateCritical,
"alert_template_expired":  req.TemplateExpired,
```

Update TestAlert() to render template before sending:
```go
// Load templates
templates := &notify.Templates{
    Warning:  s["alert_template_critical"], // use critical for test
    Critical: s["alert_template_critical"],
    Expired:  s["alert_template_expired"],
}
if templates.Critical == "" { templates.Critical = notify.DefaultTemplates.Critical }
msg.Body = templates.Render(msg)
```

### Step 8 — Template UI in Settings tab

Add this section in index.html BETWEEN Alert Rules and Notification Channels:
```html
<div style="background:white;border:1px solid #e2e8f0;border-radius:8px;
            padding:24px;margin-bottom:20px">
    <h3 style="font-size:15px;font-weight:600;margin-bottom:4px">Alert Templates</h3>
    <p style="color:#64748b;font-size:13px;margin-bottom:8px">
        Customize the message format for each severity level.
    </p>
    <div style="background:#f8fafc;border:1px solid #e2e8f0;border-radius:6px;
                padding:12px;margin-bottom:16px;font-size:12px;
                color:#475569;line-height:2">
        <strong>Available variables:</strong><br>
        <code>{asset_name}</code> &nbsp;
        <code>{asset_type}</code> &nbsp;
        <code>{days_left}</code> &nbsp;
        <code>{expires_at}</code> &nbsp;
        <code>{env}</code> &nbsp;
        <code>{severity}</code> &nbsp;
        <code>{emoji}</code>
    </div>
    <div class="form-group">
        <label style="color:#ca8a04">⚠️ Warning template</label>
        <textarea id="alert-template-warning" rows="3"
            style="font-family:monospace;font-size:13px"></textarea>
    </div>
    <div class="form-group">
        <label style="color:#ea580c">🚨 Critical template</label>
        <textarea id="alert-template-critical" rows="3"
            style="font-family:monospace;font-size:13px"></textarea>
    </div>
    <div class="form-group">
        <label style="color:#dc2626">☠️ Expired template</label>
        <textarea id="alert-template-expired" rows="3"
            style="font-family:monospace;font-size:13px"></textarea>
    </div>
    <button class="btn btn-sm" style="background:#f1f5f9"
        onclick="resetTemplates()">↩ Reset to defaults</button>
</div>
```

### Step 9 — Template JS functions

Add to index.html:
```javascript
const DEFAULT_TEMPLATES = {
    warning:  "⚠️ *{asset_name}* is expiring in *{days_left} days*\nType: {asset_type}\nExpires: {expires_at}\nEnvironment: {env}",
    critical: "🚨 *{asset_name}* expires in *{days_left} days* — ACTION REQUIRED\nType: {asset_type}\nExpires: {expires_at}\nEnvironment: {env}",
    expired:  "☠️ *{asset_name}* has EXPIRED\nType: {asset_type}\nExpired: {expires_at}\nEnvironment: {env}"
}

function resetTemplates() {
    document.getElementById('alert-template-warning').value  = DEFAULT_TEMPLATES.warning
    document.getElementById('alert-template-critical').value = DEFAULT_TEMPLATES.critical
    document.getElementById('alert-template-expired').value  = DEFAULT_TEMPLATES.expired
}
```

Add to loadAlertSettings() after loading other fields:
```javascript
document.getElementById('alert-template-warning').value  =
    s.template_warning  || DEFAULT_TEMPLATES.warning
document.getElementById('alert-template-critical').value =
    s.template_critical || DEFAULT_TEMPLATES.critical
document.getElementById('alert-template-expired').value  =
    s.template_expired  || DEFAULT_TEMPLATES.expired
```

Add to saveAlertSettings() payload:
```javascript
template_warning:  document.getElementById('alert-template-warning').value,
template_critical: document.getElementById('alert-template-critical').value,
template_expired:  document.getElementById('alert-template-expired').value,
```
