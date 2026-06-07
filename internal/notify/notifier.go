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
	Body      string
}

func (m Message) SeverityEmoji() string {
	switch m.Severity {
	case SeverityWarning:
		return "⚠️"
	case SeverityCritical:
		return "🚨"
	case SeverityExpired:
		return "☠️"
	}
	return "ℹ️"
}

func (m Message) ColorHex() string {
	switch m.Severity {
	case SeverityWarning:
		return "#ffcc00"
	case SeverityCritical:
		return "#ff6600"
	case SeverityExpired:
		return "#cc0000"
	}
	return "#888888"
}

type Notifier interface {
	Send(ctx context.Context, msg Message) error
	Name() string
}
