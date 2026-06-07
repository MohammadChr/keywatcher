package metrics

import (
	"time"
	"keywatcher/internal/model"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	AssetExpiryDays = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "keywatcher_asset_expiry_days",
		Help: "Days until asset expiry. Negative means already expired.",
	}, []string{"name", "type", "env"})

	AssetsTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "keywatcher_assets_total",
		Help: "Total assets by type and status.",
	}, []string{"type", "status"})

	NotificationsSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "keywatcher_notifications_sent_total",
		Help: "Notifications sent per channel.",
	}, []string{"channel"})

	CheckDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "keywatcher_check_duration_seconds",
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
		if env == "" {
			env = "unknown"
		}

		days := float64(time.Until(a.ExpiresAt).Hours()) / 24
		AssetExpiryDays.WithLabelValues(a.Name, string(a.Type), env).Set(days)

		status := a.Status()
		t := string(a.Type)
		if counts[t] == nil {
			counts[t] = map[string]float64{}
		}
		counts[t][status]++
	}

	for assetType, statuses := range counts {
		for status, count := range statuses {
			AssetsTotal.WithLabelValues(assetType, status).Set(count)
		}
	}
}
