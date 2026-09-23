package shopware

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopwareLabs/go-shopware-http-client/internal/assert"
)

// newTestServer returns a server that issues tokens at /api/oauth/token and
// delegates everything else to handler.
func newTestServer(t *testing.T, tokenCount *atomic.Int32, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/oauth/token" {
			if tokenCount != nil {
				tokenCount.Add(1)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.MarshalWrite(w, map[string]any{
				"access_token": "token-abc",
				"token_type":   "Bearer",
				"expires_in":   600,
			})
			return
		}
		handler(w, r)
	}))
}

func TestClientGetAuthenticatesAndSendsBearer(t *testing.T) {
	var tokens atomic.Int32
	srv := newTestServer(t, &tokens, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token-abc", r.Header.Get("Authorization"))
		assert.Equal(t, "custom", r.Header.Get("X-Custom"))
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	defer srv.Close()

	c := NewClient(Config{
		BaseURL:      srv.URL,
		ClientID:     "id",
		ClientSecret: "secret",
		Headers:      map[string]string{"X-Custom": "custom"},
	})

	resp, err := c.Get(context.Background(), "/_info/config")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{"ok":true}`, string(resp.Body))
}

func TestClientCachesToken(t *testing.T) {
	var tokens atomic.Int32
	srv := newTestServer(t, &tokens, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"})

	for range 3 {
		_, err := c.Get(context.Background(), "/anything")
		assert.NoError(t, err)
	}
	assert.Equal(t, int32(1), tokens.Load(), "token should be fetched once and cached")
}

func TestClientNoOpStorageRefetchesEachRequest(t *testing.T) {
	var tokens atomic.Int32
	srv := newTestServer(t, &tokens, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	defer srv.Close()

	c := NewClient(Config{
		BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret",
		TokenStorage: NewNoOpTokenStorage(),
	})

	for range 3 {
		_, err := c.Get(context.Background(), "/anything")
		assert.NoError(t, err)
	}
	assert.Equal(t, int32(3), tokens.Load(), "NoOp storage forces a fresh token every request")
}

func TestClientUsesPreSeededTokenFromStorage(t *testing.T) {
	var tokens atomic.Int32
	srv := newTestServer(t, &tokens, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer seeded", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{}`))
	})
	defer srv.Close()

	storage := NewInMemoryTokenStorage()
	assert.NoError(t, storage.Set(context.Background(), "shop-key", "seeded", time.Now().Add(time.Hour)))

	c := NewClient(Config{
		BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret",
		TokenStorage:    storage,
		TokenStorageKey: "shop-key",
	})

	_, err := c.Get(context.Background(), "/anything")
	assert.NoError(t, err)
	assert.Equal(t, int32(0), tokens.Load(), "valid pre-seeded token avoids the token endpoint")
}

func TestClientRetriesOnceOn401(t *testing.T) {
	var tokens atomic.Int32
	var calls atomic.Int32
	srv := newTestServer(t, &tokens, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"})

	resp, err := c.Get(context.Background(), "/anything")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(2), calls.Load(), "request retried once after 401")
	assert.Equal(t, int32(2), tokens.Load(), "token re-fetched after invalidation")
}

func TestClientReturnsAPIErrorWithDetail(t *testing.T) {
	srv := newTestServer(t, nil, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"detail":"Invalid criteria"}]}`))
	})
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"})

	_, err := c.Get(context.Background(), "/anything")
	assert.Error(t, err)

	var apiErr *APIError
	assert.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, "Invalid criteria", apiErr.Detail)
	assert.Contains(t, apiErr.Error(), "Invalid criteria")
}

func TestClientTokenFailurePropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, ClientID: "id", ClientSecret: "bad"})
	err := c.Authenticate(context.Background())
	assert.Error(t, err)

	var apiErr *APIError
	assert.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
}

func TestTokenResponseDecodeErrorShowsResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("hello from a proxy"))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, ClientID: "i", ClientSecret: "s"})
	err := c.Authenticate(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), `status 200, content-type "text/html", body "hello from a proxy"`)
}

func TestTokenResponseWithoutAccessTokenFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"token_type":"Bearer"}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, ClientID: "i", ClientSecret: "s"})
	err := c.Authenticate(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "has no access_token")
}
