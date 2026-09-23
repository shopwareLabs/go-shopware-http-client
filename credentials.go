package shopware

import (
	"context"
	"sync"
)

// Credentials produces the OAuth token request for a Client. The Shopware Admin
// API supports two grant types, each modeled by an implementation here:
//
//   - IntegrationCredentials — client_credentials grant (integration / app).
//   - PasswordCredentials     — password grant (admin user login).
//
// Custom implementations may supply any other body the /api/oauth/token
// endpoint accepts.
type Credentials interface {
	// tokenRequest returns the JSON body POSTed to /api/oauth/token.
	tokenRequest() map[string]string

	// identity returns a stable string that uniquely identifies the principal
	// these credentials authenticate as. It is used as the default token
	// storage key so distinct principals never share a cached token.
	identity() string
}

// refreshTokenRotator is an optional extension point on Credentials.
// Implementations receive a rotated refresh_token from the token response
// before the new access token is stored.
type refreshTokenRotator interface {
	rotateRefreshToken(ctx context.Context, refreshToken string) error
}

// IntegrationCredentials authenticates as a Shopware integration using the
// client_credentials grant.
type IntegrationCredentials struct {
	ClientID     string
	ClientSecret string
}

// NewIntegrationCredentials creates IntegrationCredentials.
func NewIntegrationCredentials(clientID, clientSecret string) IntegrationCredentials {
	return IntegrationCredentials{ClientID: clientID, ClientSecret: clientSecret}
}

func (c IntegrationCredentials) tokenRequest() map[string]string {
	return map[string]string{
		"grant_type":    "client_credentials",
		"client_id":     c.ClientID,
		"client_secret": c.ClientSecret,
	}
}

func (c IntegrationCredentials) identity() string {
	return "integration:" + c.ClientID
}

// PasswordCredentials authenticates as an admin user using the password grant.
// The client_id is fixed to "administration", matching the Shopware admin SPA.
type PasswordCredentials struct {
	Username string
	Password string
}

// NewPasswordCredentials creates PasswordCredentials.
func NewPasswordCredentials(username, password string) PasswordCredentials {
	return PasswordCredentials{Username: username, Password: password}
}

func (c PasswordCredentials) tokenRequest() map[string]string {
	return map[string]string{
		"grant_type": "password",
		"client_id":  "administration",
		"scopes":     "write",
		"username":   c.Username,
		"password":   c.Password,
	}
}

func (c PasswordCredentials) identity() string {
	return "password:" + c.Username
}

// RefreshTokenCredentials authenticates using the refresh_token grant. It is
// the bridge from an interactive PKCE login (via the pkce sub-package) into
// ongoing Client use: the initial access + refresh tokens are obtained by
// browser-based authorization, then stored here so the Client can
// transparently refresh the access token without further user interaction.
//
// The refresh token is mutable (supports rotation) and thread-safe.
//
// Shopware revokes the previous refresh token on every refresh, so a rotated
// token that is not persisted logs the user out on the next run. Set OnRotate
// to persist it, or use pkce.NewClient, which does this for you.
type RefreshTokenCredentials struct {
	ClientID string

	// OnRotate, if set, is called with the new refresh token whenever the
	// server rotates it, before the new access token is used. An error is
	// returned from the request that triggered the refresh. Set it before the
	// credentials are first used.
	OnRotate func(ctx context.Context, refreshToken string) error

	mu      sync.Mutex
	refresh string
}

// NewRefreshTokenCredentials creates RefreshTokenCredentials with the given
// client ID and initial refresh token. Use the client ID that was registered
// as a public OAuth client in Shopware (e.g. "shopware-cli").
func NewRefreshTokenCredentials(clientID, refreshToken string) *RefreshTokenCredentials {
	return &RefreshTokenCredentials{
		ClientID: clientID,
		refresh:  refreshToken,
	}
}

// RefreshToken returns the current refresh token (for inspection/persistence).
func (c *RefreshTokenCredentials) RefreshToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.refresh
}

// SetRefreshToken updates the stored refresh token. Call it after a PKCE
// exchange or when the server returns a rotated refresh token.
func (c *RefreshTokenCredentials) SetRefreshToken(refreshToken string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refresh = refreshToken
}

func (c *RefreshTokenCredentials) tokenRequest() map[string]string {
	c.mu.Lock()
	refresh := c.refresh
	c.mu.Unlock()
	return map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     c.ClientID,
		"refresh_token": refresh,
	}
}

func (c *RefreshTokenCredentials) identity() string {
	return "pkce:" + c.ClientID
}

func (c *RefreshTokenCredentials) rotateRefreshToken(ctx context.Context, refreshToken string) error {
	c.SetRefreshToken(refreshToken)
	if c.OnRotate == nil {
		return nil
	}
	return c.OnRotate(ctx, refreshToken)
}
