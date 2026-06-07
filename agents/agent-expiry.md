# Agent: Expiry, Notifications & Metrics

You own the background expiry checker, all notification senders, and Prometheus metrics.
Read CLAUDE.md first. No global state except the Prometheus registry (promauto handles that).

## Task 1 — Prometheus Metrics (internal/metrics/metrics.go)
```go
package metrics

import (
    "vaultwatch/internal/model"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
    "time"
)

var (
    AssetExpiryDays = promauto.NewGaugeVec(prometheus.GaugeOpts{
        Name: "vaultwatch_asset_expiry_days",
        Help: "Days until asset expiry. Negative means already expired.",
    }, []string{"name", "type", "env"})

    AssetsTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
        Name: "vaultwatch_assets_total",
        Help: "Total assets by type and status.",
    }, []string{"type", "status"})

    NotificationsSent = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "vaultwatch_notifications_sent_total",
        Help: "Notifications sent per channel.",
    }, []string{"channel"})

    CheckDuration = promauto.NewHistogram(prometheus.HistogramOpts{
        Name:    "vaultwatch_check_duration_seconds",
        Help:    "Time taken for the expiry check sweep.",
        Buckets: []float64{0.1, 0.5, 1, 5, 10},
    })
)

func UpdateMetrics(assets []*model.Asset) {
    // Reset gauges before recalculating
    AssetExpiryDays.Reset()
    AssetsTotal.Reset()

    counts := map[string]map[string]float64{}

    for _, a := range assets {
        env := a.Tags["env"]
        if env == "" { env = "unknown" }

        days := float64(time.Until(a.ExpiresAt).Hours()) / 24
        AssetExpiryDays.WithLabelValues(a.Name, string(a.Type), env).Set(days)

        status := a.Status()
        t := string(a.Type)
        if counts[t] == nil { counts[t] = map[string]float64{} }
        counts[t][status]++
    }

    for assetType, statuses := range counts {
        for status, count := range statuses {
            AssetsTotal.WithLabelValues(assetType, status).Set(count)
        }
    }
}
```

## Task 2 — Notifier Interface (internal/notify/notifier.go)
```go
package notify

import (
    "context"
    "time"
)

type Severity string
const (
    SeverityWarning  Severity = "warning"  // <= 30 days
    SeverityCritical Severity = "critical" // <= 7 days
    SeverityExpired  Severity = "expired"  // past
)

type Message struct {
    Title     string
    AssetName string
    AssetType string
    DaysLeft  int
    ExpiresAt time.Time
    Tags      map[string]string
    Severity  Severity
}

func (m Message) SeverityEmoji() string {
    switch m.Severity {
    case SeverityWarning:  return "⚠️"
    case SeverityCritical: return "🚨"
    case SeverityExpired:  return "☠️"
    }
    return "ℹ️"
}

func (m Message) ColorHex() string {
    switch m.Severity {
    case SeverityWarning:  return "#ffcc00"
    case SeverityCritical: return "#ff6600"
    case SeverityExpired:  return "#cc0000"
    }
    return "#888888"
}

type Notifier interface {
    Send(ctx context.Context, msg Message) error
    Name() string
}
```

## Task 3 — Slack Notifier (internal/notify/slack.go)
```go
package notify

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
)

type SlackNotifier struct {
    webhookURL string
    client     *http.Client
}

func NewSlack(webhookURL string) *SlackNotifier {
    return &SlackNotifier{webhookURL: webhookURL, client: &http.Client{Timeout: 10 * 1e9}}
}

func (s *SlackNotifier) Name() string { return "slack" }

func (s *SlackNotifier) Send(ctx context.Context, msg Message) error {
    payload := map[string]any{
        "attachments": []map[string]any{{
            "color": msg.ColorHex(),
            "blocks": []map[string]any{
                {"type": "header", "text": map[string]any{"type": "plain_text", "text": msg.SeverityEmoji() + " " + msg.Title}},
                {"type": "section", "fields": []map[string]any{
                    {"type": "mrkdwn", "text": "*Asset:*\n" + msg.AssetName},
                    {"type": "mrkdwn", "text": "*Type:*\n" + msg.AssetType},
                    {"type": "mrkdwn", "text": fmt.Sprintf("*Days left:*\n%d", msg.DaysLeft)},
                    {"type": "mrkdwn", "text": "*Expires:*\n" + msg.ExpiresAt.Format("2006-01-02")},
                }},
            },
        }},
    }
    return postJSON(ctx, s.client, s.webhookURL, payload)
}
```

## Task 4 — Mattermost Notifier (internal/notify/mattermost.go)
```go
package notify

import (
    "context"
    "fmt"
    "net/http"
)

type MattermostNotifier struct {
    webhookURL string
    client     *http.Client
}

func NewMattermost(webhookURL string) *MattermostNotifier {
    return &MattermostNotifier{webhookURL: webhookURL, client: &http.Client{Timeout: 10 * 1e9}}
}

func (m *MattermostNotifier) Name() string { return "mattermost" }

func (m *MattermostNotifier) Send(ctx context.Context, msg Message) error {
    text := fmt.Sprintf(
        "%s **%s**\n**Asset:** %s | **Type:** %s | **Days left:** %d | **Expires:** %s",
        msg.SeverityEmoji(), msg.Title,
        msg.AssetName, msg.AssetType, msg.DaysLeft,
        msg.ExpiresAt.Format("2006-01-02"),
    )
    return postJSON(ctx, m.client, m.webhookURL, map[string]any{"text": text})
}
```

## Task 5 — Generic Webhook (internal/notify/webhook.go)
```go
package notify

import (
    "context"
    "net/http"
)

type WebhookNotifier struct {
    url    string
    client *http.Client
}

func NewWebhook(url string) *WebhookNotifier {
    return &WebhookNotifier{url: url, client: &http.Client{Timeout: 10 * 1e9}}
}

func (w *WebhookNotifier) Name() string { return "webhook" }

func (w *WebhookNotifier) Send(ctx context.Context, msg Message) error {
    return postJSONWithHeader(ctx, w.client, w.url, msg, map[string]string{
        "X-VaultWatch-Severity": string(msg.Severity),
    })
}
```

## Task 6 — HTTP helpers (internal/notify/http.go)
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

func postJSON(ctx context.Context, client *http.Client, url string, payload any) error {
    return postJSONWithHeader(ctx, client, url, payload, nil)
}

func postJSONWithHeader(ctx context.Context, client *http.Client, url string, payload any, headers map[string]string) error {
    body, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("notify.postJSON marshal: %w", err)
    }

    req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
    if err != nil {
        return fmt.Errorf("notify.postJSON request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")
    for k, v := range headers {
        req.Header.Set(k, v)
    }

    resp, err := client.Do(req)
    if err != nil {
        // Retry once after 2s
        time.Sleep(2 * time.Second)
        resp, err = client.Do(req)
        if err != nil {
            return fmt.Errorf("notify.postJSON send: %w", err)
        }
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 500 {
        // Retry once
        time.Sleep(2 * time.Second)
        resp2, err2 := client.Do(req)
        if err2 == nil {
            defer resp2.Body.Close()
            if resp2.StatusCode < 400 {
                return nil
            }
        }
        return fmt.Errorf("notify.postJSON: server error %d", resp.StatusCode)
    }
    return nil
}
```

## Task 7 — Multi Notifier (internal/notify/multi.go)
```go
package notify

import (
    "context"
    "fmt"
    "strings"
)

type MultiNotifier struct {
    notifiers []Notifier
}

func NewMulti(notifiers ...Notifier) *MultiNotifier {
    return &MultiNotifier{notifiers: notifiers}
}

func (m *MultiNotifier) Send(ctx context.Context, msg Message) error {
    var errs []string
    for _, n := range m.notifiers {
        if err := n.Send(ctx, msg); err != nil {
            errs = append(errs, fmt.Sprintf("%s: %v", n.Name(), err))
        }
    }
    if len(errs) > 0 {
        return fmt.Errorf("notify.MultiNotifier errors: %s", strings.Join(errs, "; "))
    }
    return nil
}

func (m *MultiNotifier) Names() []string {
    var names []string
    for _, n := range m.notifiers {
        names = append(names, n.Name())
    }
    return names
}
```

## Task 8 — Expiry Checker (internal/expiry/checker.go)
```go
package expiry

import (
    "context"
    "sort"
    "time"
    "vaultwatch/internal/metrics"
    "vaultwatch/internal/model"
    "vaultwatch/internal/notify"
    "vaultwatch/internal/store"
    "github.com/rs/zerolog/log"
    prom "github.com/prometheus/client_golang/prometheus"
)

type Checker struct {
    store     store.Store
    notifier  *notify.MultiNotifier
    warnDays  []int
    interval  time.Duration
    cancel    context.CancelFunc
    done      chan struct{}
}

func New(s store.Store, n *notify.MultiNotifier, warnDays []int, interval time.Duration) *Checker {
    sort.Sort(sort.Reverse(sort.IntSlice(warnDays))) // descending
    return &Checker{
        store:    s,
        notifier: n,
        warnDays: warnDays,
        interval: interval,
        done:     make(chan struct{}),
    }
}

func (c *Checker) Start(ctx context.Context) {
    ctx, c.cancel = context.WithCancel(ctx)
    go func() {
        defer close(c.done)
        c.run(ctx)                     // run immediately on start
        ticker := time.NewTicker(c.interval)
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                c.run(ctx)
            }
        }
    }()
}

func (c *Checker) Stop() {
    if c.cancel != nil { c.cancel() }
    <-c.done
}

func (c *Checker) run(ctx context.Context) {
    timer := prom.NewTimer(metrics.CheckDuration)
    defer timer.ObserveDuration()

    assets, err := c.store.ListAllActive(ctx)
    if err != nil {
        log.Error().Err(err).Msg("expiry.Checker: failed to list assets")
        return
    }

    metrics.UpdateMetrics(assets)

    for _, a := range assets {
        c.checkAsset(ctx, a)
    }
}

func (c *Checker) checkAsset(ctx context.Context, a *model.Asset) {
    days := a.DaysUntilExpiry()

    // Find the matching warn threshold
    threshold := 0
    for _, d := range c.warnDays {
        if days <= d {
            threshold = d
        }
    }
    if threshold == 0 && days > 0 {
        return // not in any warning window
    }

    // Don't spam — check if already notified in last 23h
    already, err := c.store.WasNotifiedRecently(ctx, a.ID, threshold, 23*time.Hour)
    if err != nil || already {
        return
    }

    severity := notify.SeverityWarning
    if days <= 0  { severity = notify.SeverityExpired }
    if days <= 7  { severity = notify.SeverityCritical }

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

    if err := c.notifier.Send(ctx, msg); err != nil {
        log.Error().Err(err).Str("asset", a.Name).Msg("expiry.Checker: notification failed")
        return
    }

    for _, name := range c.notifier.Names() {
        metrics.NotificationsSent.WithLabelValues(name).Inc()
        _ = c.store.LogNotification(ctx, a.ID, threshold, name)
    }

    log.Info().Str("asset", a.Name).Int("days_left", days).Str("severity", string(severity)).Msg("expiry notification sent")
}
```

## Task 9 — Wire into main.go

Update main.go to:
1. Initialize the store (NewPostgres)
2. Build notifiers from config (only add non-empty webhook URLs)
3. Create MultiNotifier
4. Create Checker and call checker.Start(ctx)
5. Call checker.Stop() in the shutdown sequence after HTTP server stops
6. Pass store to server.New()

## Final check
Run `go build ./...` — fix all errors before finishing.
