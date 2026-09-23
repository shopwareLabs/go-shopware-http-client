package pkce

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopwareLabs/go-shopware-http-client/internal/assert"
)

// memStore is an in-memory Store that records every save.
type memStore struct {
	mu       sync.Mutex
	sessions map[string]Tokens
	saves    int
}

func newMemStore() *memStore { return &memStore{sessions: map[string]Tokens{}} }

func (m *memStore) Load(_ context.Context, baseURL string) (*Tokens, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.sessions[baseURL]
	if !ok {
		return nil, nil
	}
	return &t, nil
}

func (m *memStore) Save(_ context.Context, t *Tokens) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[t.BaseURL] = *t
	m.saves++
	return nil
}

func (m *memStore) Delete(_ context.Context, baseURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, baseURL)
	return nil
}

func (m *memStore) get(baseURL string) Tokens {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[baseURL]
}

// shopServer fakes the token endpoint. Authorization-code exchanges return
// "login-at"/"login-rt"; refresh grants are answered by refresh.
type shopServer struct {
	*httptest.Server
	logins    atomic.Int32
	refreshes atomic.Int32
	bearer    atomic.Value
}

func newShopServer(t *testing.T, refresh func(w http.ResponseWriter, refreshToken string)) *shopServer {
	t.Helper()
	s := &shopServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth/token" {
			s.bearer.Store(r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{}`))
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch body["grant_type"] {
		case "authorization_code":
			s.logins.Add(1)
			_, _ = w.Write([]byte(`{"access_token":"login-at","refresh_token":"login-rt","expires_in":3600}`))
		case "refresh_token":
			s.refreshes.Add(1)
			refresh(w, body["refresh_token"])
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// browser simulates the user approving the login by calling the callback.
func browser(u string) error {
	q, err := url.Parse(u)
	if err != nil {
		return err
	}
	params := q.Query()
	resp, err := http.Get(params.Get("redirect_uri") + "?code=c&state=" + url.QueryEscape(params.Get("state")))
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func noBrowser(t *testing.T) func(string) error {
	return func(string) error {
		t.Error("browser must not be opened")
		return errors.New("unexpected login")
	}
}

func TestNewClientLogsInAndSavesSession(t *testing.T) {
	srv := newShopServer(t, nil)
	store := newMemStore()

	client, err := NewClient(context.Background(), Config{BaseURL: srv.URL + "/", OpenURL: browser, Timeout: 5 * time.Second}, store)
	assert.NoError(t, err)

	saved := store.get(srv.URL)
	assert.Equal(t, "login-rt", saved.RefreshToken)
	assert.Equal(t, "login-at", saved.AccessToken)
	assert.Equal(t, "shopware-cli", saved.ClientID)

	_, err = client.Get(context.Background(), "/_info/me")
	assert.NoError(t, err)
	assert.Equal(t, "Bearer login-at", srv.bearer.Load())
	assert.Equal(t, int32(0), srv.refreshes.Load(), "the login's access token is reused")
}

func TestNewClientReusesValidSession(t *testing.T) {
	srv := newShopServer(t, nil)
	store := newMemStore()
	_ = store.Save(context.Background(), &Tokens{
		BaseURL: srv.URL, ClientID: "shopware-cli",
		AccessToken: "saved-at", RefreshToken: "saved-rt", Expiry: time.Now().Add(time.Hour),
	})

	client, err := NewClient(context.Background(), Config{BaseURL: srv.URL, OpenURL: noBrowser(t)}, store)
	assert.NoError(t, err)
	_, err = client.Get(context.Background(), "/_info/me")
	assert.NoError(t, err)

	assert.Equal(t, "Bearer saved-at", srv.bearer.Load())
	assert.Equal(t, int32(0), srv.refreshes.Load())
}

func TestNewClientPersistsRotatedRefreshToken(t *testing.T) {
	srv := newShopServer(t, func(w http.ResponseWriter, refreshToken string) {
		assert.Equal(t, "saved-rt", refreshToken)
		_, _ = w.Write([]byte(`{"access_token":"new-at","refresh_token":"new-rt","expires_in":3600}`))
	})
	store := newMemStore()
	_ = store.Save(context.Background(), &Tokens{
		BaseURL: srv.URL, ClientID: "shopware-cli",
		AccessToken: "old-at", RefreshToken: "saved-rt", Expiry: time.Now().Add(-time.Minute),
	})

	_, err := NewClient(context.Background(), Config{BaseURL: srv.URL, OpenURL: noBrowser(t)}, store)
	assert.NoError(t, err)

	saved := store.get(srv.URL)
	assert.Equal(t, "new-rt", saved.RefreshToken)
	assert.Equal(t, "new-at", saved.AccessToken)
	assert.True(t, saved.Expiry.After(time.Now()))
}

func TestNewClientLogsInAgainWhenRefreshTokenIsRejected(t *testing.T) {
	srv := newShopServer(t, func(w http.ResponseWriter, _ string) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"detail":"The refresh token is invalid."}]}`))
	})
	store := newMemStore()
	_ = store.Save(context.Background(), &Tokens{
		BaseURL: srv.URL, ClientID: "shopware-cli", RefreshToken: "revoked-rt",
	})

	_, err := NewClient(context.Background(), Config{BaseURL: srv.URL, OpenURL: browser, Timeout: 5 * time.Second}, store)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), srv.logins.Load())
	assert.Equal(t, "login-rt", store.get(srv.URL).RefreshToken)
}

func TestNewClientDoesNotLogInOnServerError(t *testing.T) {
	srv := newShopServer(t, func(w http.ResponseWriter, _ string) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	store := newMemStore()
	_ = store.Save(context.Background(), &Tokens{
		BaseURL: srv.URL, ClientID: "shopware-cli", RefreshToken: "saved-rt",
	})

	_, err := NewClient(context.Background(), Config{BaseURL: srv.URL, OpenURL: noBrowser(t)}, store)
	assert.Error(t, err)
	assert.Equal(t, "saved-rt", store.get(srv.URL).RefreshToken, "the session is kept for the next attempt")
}
