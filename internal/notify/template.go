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
	if tmpl == "" {
		tmpl = DefaultTemplates.Warning
	}

	env := msg.Tags["env"]
	if env == "" {
		env = "unknown"
	}

	r := strings.NewReplacer(
		"{asset_name}", msg.AssetName,
		"{asset_type}", msg.AssetType,
		"{days_left}", fmt.Sprintf("%d", msg.DaysLeft),
		"{expires_at}", msg.ExpiresAt.Format("2006-01-02"),
		"{env}", env,
		"{severity}", string(msg.Severity),
		"{emoji}", msg.SeverityEmoji(),
	)
	return r.Replace(tmpl)
}
