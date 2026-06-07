package expiry

import (
	"context"
	"sort"
	"strconv"
	"time"
	"keywatcher/internal/metrics"
	"keywatcher/internal/model"
	"keywatcher/internal/notify"
	"keywatcher/internal/store"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
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
	if c.cancel != nil {
		c.cancel()
	}
	<-c.done
}

func (c *Checker) run(ctx context.Context) {
	timer := prometheus.NewTimer(metrics.CheckDuration)
	defer timer.ObserveDuration()

	// Load current alert settings from DB
	settings, err := c.store.GetAllSettings(ctx, "alert_")
	if err != nil {
		log.Error().Err(err).Msg("checker: failed to load alert settings")
	}
	// Parse warn days from settings
	warnDays := parseWarnDays(settings["alert_warning_days"], settings["alert_critical_days"])

	// Load templates from DB
	templates := &notify.Templates{
		Warning:  settings["alert_template_warning"],
		Critical: settings["alert_template_critical"],
		Expired:  settings["alert_template_expired"],
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

func parseWarnDays(warningStr, criticalStr string) []int {
	warning, err := strconv.Atoi(warningStr)
	if err != nil || warning == 0 {
		warning = 30
	}
	critical, err := strconv.Atoi(criticalStr)
	if err != nil || critical == 0 {
		critical = 7
	}
	return []int{warning, critical, 1}
}

func (c *Checker) checkAsset(ctx context.Context, a *model.Asset, warnDays []int, templates *notify.Templates) {
	// Check if silenced
	silenced, err := c.store.IsAssetSilenced(ctx, a.ID)
	if err == nil && silenced {
		return // skip — asset is silenced
	}

	days := a.DaysUntilExpiry()

	// Find the matching warn threshold
	threshold := 0
	for _, d := range warnDays {
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
	if days <= 0 {
		severity = notify.SeverityExpired
	}
	if days <= 7 {
		severity = notify.SeverityCritical
	}

	title := "Asset expiring soon"
	if severity == notify.SeverityExpired {
		title = "Asset has expired"
	}

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
		log.Error().Err(err).Str("asset", a.Name).Msg("expiry.Checker: notification failed")
		return
	}

	for _, name := range c.notifier.Names() {
		metrics.NotificationsSent.WithLabelValues(name).Inc()
		_ = c.store.LogNotification(ctx, a.ID, threshold, name)
	}

	log.Info().Str("asset", a.Name).Int("days_left", days).Str("severity", string(severity)).Msg("expiry notification sent")
}
