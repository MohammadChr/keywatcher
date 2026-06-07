package auth

import (
	"context"
	"fmt"
	"vaultwatch/internal/model"
	"vaultwatch/internal/store"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type OIDCAuthenticator struct {
	cfg      OIDCConfig
	provider *oidc.Provider
	oauth2   *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func NewOIDCAuthenticator(ctx context.Context, cfg OIDCConfig) (*OIDCAuthenticator, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc.NewProvider: %w", err)
	}
	o := &OIDCAuthenticator{
		cfg:      cfg,
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth2: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
	}
	return o, nil
}

func (o *OIDCAuthenticator) AuthCodeURL(state, nonce string) string {
	return o.oauth2.AuthCodeURL(state, oidc.Nonce(nonce))
}

func (o *OIDCAuthenticator) Exchange(ctx context.Context, s store.Store, code string, nonce string) (*model.User, error) {
	token, err := o.oauth2.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("oidc.Exchange: %w", err)
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("oidc.Exchange: no id_token")
	}
	idToken, err := o.verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("oidc.Exchange verify: %w", err)
	}
	if idToken.Nonce != nonce {
		return nil, fmt.Errorf("oidc.Exchange: nonce mismatch")
	}
	var claims struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Subject  string `json:"sub"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("oidc.Exchange claims: %w", err)
	}
	user := &model.User{
		ID:         uuid.New(),
		Username:   claims.Name,
		Email:      claims.Email,
		AuthMethod: model.AuthMethodOIDC,
	}
	if err := s.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("oidc.Exchange upsert: %w", err)
	}
	return user, nil
}
