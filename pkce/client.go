package pkce

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	shopware "github.com/shopwareLabs/go-shopware-http-client"
)

// NewClient returns a Client for config.BaseURL that is logged in via PKCE.
//
// It reuses the session saved in store for the shop. When there is none, or
// the shop rejects its refresh token, it runs Login (opening the browser) and
// saves the new session. From then on every token change, including rotated
// refresh tokens, is written to store as it happens, so the caller never
// persists anything by hand.
func NewClient(ctx context.Context, config Config, store Store) (*shopware.Client, error) {
	config = applyDefaults(config)
	config.BaseURL = normalizeBaseURL(config.BaseURL)

	saved, err := store.Load(ctx, config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}

	if saved != nil && saved.ClientID == config.ClientID && saved.RefreshToken != "" {
		client := newSessionClient(config, store, saved)
		err := client.Authenticate(ctx)
		if err == nil {
			return client, nil
		}
		if !isRejectedGrant(err) {
			return nil, err
		}
	}

	tokens, err := Login(ctx, config)
	if err != nil {
		return nil, err
	}
	tokens.BaseURL = config.BaseURL
	if err := store.Save(ctx, tokens); err != nil {
		return nil, fmt.Errorf("save session: %w", err)
	}
	return newSessionClient(config, store, tokens), nil
}

// isRejectedGrant reports whether the token endpoint refused the refresh
// token, which means the user has to log in again. Network errors and server
// errors are not treated as such.
func isRejectedGrant(err error) bool {
	var apiErr *shopware.APIError
	return errors.As(err, &apiErr) &&
		(apiErr.StatusCode == http.StatusBadRequest || apiErr.StatusCode == http.StatusUnauthorized)
}

func newSessionClient(config Config, store Store, tokens *Tokens) *shopware.Client {
	s := &session{store: store, tokens: *tokens}

	creds := shopware.NewRefreshTokenCredentials(tokens.ClientID, tokens.RefreshToken)
	creds.OnRotate = s.rotate

	return shopware.NewClient(shopware.Config{
		BaseURL:      config.BaseURL,
		Credentials:  creds,
		HTTPClient:   config.HTTPClient,
		UserAgent:    config.UserAgent,
		TokenStorage: s,
	})
}

// session is the Client's TokenStorage for a PKCE login. It keeps access and
// refresh token together and writes the whole session to the Store on every
// change.
type session struct {
	store Store

	mu     sync.Mutex
	tokens Tokens
}

// Get implements shopware.TokenStorage. The session holds a single token, so
// the key is ignored.
func (s *session) Get(_ context.Context, _ string) (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokens.AccessToken, s.tokens.Expiry, nil
}

// Set implements shopware.TokenStorage.
func (s *session) Set(ctx context.Context, _, token string, expiry time.Time) error {
	return s.update(ctx, func(t *Tokens) {
		t.AccessToken = token
		t.Expiry = expiry
	})
}

// Delete implements shopware.TokenStorage.
func (s *session) Delete(ctx context.Context, _ string) error {
	return s.update(ctx, func(t *Tokens) {
		t.AccessToken = ""
		t.Expiry = time.Time{}
	})
}

func (s *session) rotate(ctx context.Context, refreshToken string) error {
	return s.update(ctx, func(t *Tokens) {
		t.RefreshToken = refreshToken
	})
}

func (s *session) update(ctx context.Context, change func(*Tokens)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	change(&s.tokens)
	snapshot := s.tokens
	return s.store.Save(ctx, &snapshot)
}
