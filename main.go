package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"vaultwatch/config"
	"vaultwatch/internal/auth"
	"vaultwatch/internal/expiry"
	"vaultwatch/internal/handler"
	"vaultwatch/internal/notify"
	"vaultwatch/internal/server"
	"vaultwatch/internal/store"
)

func main() {
	// Logger
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Config
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	// Validate JWT secret (Fix 3.1)
	if cfg.JWTSecret == "" {
		log.Fatal().Msg("KEYWATCHER_JWT_SECRET is required and must not be empty")
	}
	if len(cfg.JWTSecret) < 32 {
		log.Fatal().Msg("KEYWATCHER_JWT_SECRET must be at least 32 characters")
	}

	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// Database
	dbCtx, dbCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer dbCancel()

	s, err := store.NewPostgres(dbCtx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer s.Close()

	// Auth providers
	var oidcAuth *auth.OIDCAuthenticator
	var ldapAuth *auth.LDAPAuthenticator

	// Initialize OIDC if enabled
	if contains(cfg.AuthMethods, "oidc") {
		oidcCtx, oidcCancel := context.WithTimeout(context.Background(), 10*time.Second)
		oidcAuth, err = auth.NewOIDCAuthenticator(oidcCtx, auth.OIDCConfig{
			Issuer:       cfg.OIDC.Issuer,
			ClientID:     cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret,
			RedirectURL:  cfg.OIDC.RedirectURL,
		})
		oidcCancel()
		if err != nil {
			log.Warn().Err(err).Msg("OIDC not available")
		}
	}

	// Initialize LDAP if enabled
	if contains(cfg.AuthMethods, "ldap") {
		ldapAuth = auth.NewLDAPAuthenticator(auth.LDAPConfig{
			URL:          cfg.LDAP.URL,
			BindDN:       cfg.LDAP.BindDN,
			BindPassword: cfg.LDAP.BindPassword,
			BaseDN:       cfg.LDAP.BaseDN,
			UserFilter:   cfg.LDAP.UserFilter,
		})
	}

	// Build notifiers from config (only add non-empty webhook URLs)
	var notifiers []notify.Notifier
	if cfg.Notify.SlackWebhook != "" {
		notifiers = append(notifiers, notify.NewSlack(cfg.Notify.SlackWebhook))
	}
	if cfg.Notify.MattermostWebhook != "" {
		notifiers = append(notifiers, notify.NewMattermost(cfg.Notify.MattermostWebhook))
	}
	if cfg.Notify.GenericWebhook != "" {
		notifiers = append(notifiers, notify.NewWebhook(cfg.Notify.GenericWebhook))
	}

	// Create MultiNotifier
	multiNotifier := notify.NewMulti(notifiers...)

	// Create and start the expiry checker
	checker := expiry.New(s, multiNotifier, cfg.WarnDays, cfg.CheckInterval)
	ctx := context.Background()
	checker.Start(ctx)

	// Handlers
	authHandler := handler.NewAuthHandler(s, cfg, oidcAuth, ldapAuth)
	assetHandler := handler.NewAssetHandler(s)
	settingsHandler := handler.NewSettingsHandler(s)
	silenceHandler := handler.NewSilenceHandler(s)

	// Server
	srv := server.New(cfg, s, authHandler, assetHandler, settingsHandler, silenceHandler)

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      srv.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start
	go func() {
		log.Info().Interface("config", cfg.SafeLog()).Msg("keywatcher starting")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("shutdown error")
	}

	// Stop the expiry checker
	checker.Stop()

	log.Info().Msg("goodbye")
}

// contains checks if a slice contains a value
func contains(slice []string, item string) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}
