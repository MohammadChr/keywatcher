package config

import (
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Port           string
	LogLevel       string
	DatabaseURL    string
	AuthMethods    []string
	JWTSecret      string
	CookieSecure   bool
	MetricsToken   string
	AllowedOrigin  string

	OIDC struct {
		Issuer       string
		ClientID     string
		ClientSecret string
		RedirectURL  string
	}

	LDAP struct {
		URL          string
		BindDN       string
		BindPassword string
		BaseDN       string
		UserFilter   string
	}

	CheckInterval time.Duration
	WarnDays      []int

	Notify struct {
		SlackWebhook      string
		MattermostWebhook string
		GenericWebhook    string
	}

	MetricsPath string
}

func Load() (*Config, error) {
	viper.SetEnvPrefix("KEYWATCHER")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	viper.SetDefault("PORT", "8080")
	viper.SetDefault("LOG_LEVEL", "info")
	viper.SetDefault("CHECK_INTERVAL", "1h")
	viper.SetDefault("WARN_DAYS", "30,14,7,1")
	viper.SetDefault("METRICS_PATH", "/metrics")
	viper.SetDefault("AUTH_METHODS", "local")
	viper.SetDefault("COOKIE_SECURE", true)
	viper.SetDefault("ALLOWED_ORIGIN", "")

	cfg := &Config{
		Port:           viper.GetString("PORT"),
		LogLevel:       viper.GetString("LOG_LEVEL"),
		DatabaseURL:    viper.GetString("DB_URL"),
		JWTSecret:      viper.GetString("JWT_SECRET"),
		MetricsPath:    viper.GetString("METRICS_PATH"),
		CookieSecure:   viper.GetBool("COOKIE_SECURE"),
		MetricsToken:   viper.GetString("METRICS_TOKEN"),
		AllowedOrigin:  viper.GetString("ALLOWED_ORIGIN"),
	}

	// Parse auth methods
	methods := viper.GetString("AUTH_METHODS")
	for _, m := range strings.Split(methods, ",") {
		cfg.AuthMethods = append(cfg.AuthMethods, strings.TrimSpace(m))
	}

	// OIDC
	cfg.OIDC.Issuer = viper.GetString("OIDC_ISSUER")
	cfg.OIDC.ClientID = viper.GetString("OIDC_CLIENT_ID")
	cfg.OIDC.ClientSecret = viper.GetString("OIDC_CLIENT_SECRET")
	cfg.OIDC.RedirectURL = viper.GetString("OIDC_REDIRECT_URL")

	// LDAP
	cfg.LDAP.URL = viper.GetString("LDAP_URL")
	cfg.LDAP.BindDN = viper.GetString("LDAP_BIND_DN")
	cfg.LDAP.BindPassword = viper.GetString("LDAP_BIND_PASSWORD")
	cfg.LDAP.BaseDN = viper.GetString("LDAP_BASE_DN")
	cfg.LDAP.UserFilter = viper.GetString("LDAP_USER_FILTER")

	// Notify
	cfg.Notify.SlackWebhook = viper.GetString("NOTIFY_SLACK_WEBHOOK")
	cfg.Notify.MattermostWebhook = viper.GetString("NOTIFY_MATTERMOST_WEBHOOK")
	cfg.Notify.GenericWebhook = viper.GetString("NOTIFY_GENERIC_WEBHOOK")

	// WarnDays
	warnStr := viper.GetString("WARN_DAYS")
	for _, s := range strings.Split(warnStr, ",") {
		s = strings.TrimSpace(s)
		d, err := strconv.Atoi(s)
		if err == nil {
			cfg.WarnDays = append(cfg.WarnDays, d)
		}
	}

	// CheckInterval
	intervalStr := viper.GetString("CHECK_INTERVAL")
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		interval = 1 * time.Hour // fallback
	}
	cfg.CheckInterval = interval

	return cfg, nil
}

// SafeLog returns config suitable for logging without exposing secrets (Fix 8.1)
func (c *Config) SafeLog() map[string]interface{} {
	return map[string]interface{}{
		"port":           c.Port,
		"log_level":      c.LogLevel,
		"auth_methods":   c.AuthMethods,
		"db_url":         "****",
		"jwt_secret":     "****",
		"metrics_token":  "****",
		"cookie_secure":  c.CookieSecure,
		"allowed_origin": c.AllowedOrigin,
	}
}
