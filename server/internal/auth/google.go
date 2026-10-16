package auth

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

// Endpoints locate an OpenID Connect provider. GoogleEndpoints is the real
// one; tests point these at a local fake.
type Endpoints struct {
	AuthURL  string
	TokenURL string
	KeysURL  string
	Issuers  []string
}

// GoogleEndpoints are Google's OpenID Connect endpoints.
var GoogleEndpoints = Endpoints{
	AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL: "https://oauth2.googleapis.com/token",
	KeysURL:  "https://www.googleapis.com/oauth2/v3/certs",
	Issuers:  []string{"https://accounts.google.com", "accounts.google.com"},
}

// Provider runs the authorization-code flow with PKCE and verifies the
// returned ID token.
type Provider struct {
	oauth   *oauth2.Config
	keys    *keySet
	issuers []string
	client  *http.Client
}

// NewProvider returns a provider for the given client credentials.
// redirectURL must be registered with the provider exactly.
func NewProvider(ep Endpoints, clientID, clientSecret, redirectURL string) *Provider {
	client := &http.Client{Timeout: 10 * time.Second}
	return &Provider{
		oauth: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     oauth2.Endpoint{AuthURL: ep.AuthURL, TokenURL: ep.TokenURL, AuthStyle: oauth2.AuthStyleInParams},
		},
		keys:    &keySet{url: ep.KeysURL, client: client},
		issuers: ep.Issuers,
		client:  client,
	}
}

// AuthCodeURL is where the browser goes to sign in. hd, when not empty, asks
// Google to offer only accounts from that domain; it is a convenience, the
// server checks the domain itself.
func (p *Provider) AuthCodeURL(state, nonce, verifier, hd string) string {
	opts := []oauth2.AuthCodeOption{
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	}
	if hd != "" {
		opts = append(opts, oauth2.SetAuthURLParam("hd", hd))
	}
	return p.oauth.AuthCodeURL(state, opts...)
}

// Exchange trades the authorization code for tokens and returns the
// verified identity from the ID token.
func (p *Provider) Exchange(ctx context.Context, code, verifier, nonce string, now time.Time) (Identity, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, p.client)
	tok, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Identity{}, fmt.Errorf("exchange code: %w", err)
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return Identity{}, fmt.Errorf("%w: token response has no id_token", errToken)
	}
	return verifyIDToken(ctx, p.keys, raw, tokenRules{
		issuers: p.issuers, audience: p.oauth.ClientID, nonce: nonce, now: now,
	})
}
