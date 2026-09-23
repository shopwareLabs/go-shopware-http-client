package shopware

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopwareLabs/go-shopware-http-client/internal/assert"
)

// captureTokenBody returns a server that records the JSON body sent to the
// token endpoint and issues a token, so credential request shapes can be
// asserted.
func captureTokenBody(t *testing.T, captured *map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/oauth/token" {
			body, _ := io.ReadAll(r.Body)
			assert.NoError(t, json.Unmarshal(body, captured))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer","expires_in":600}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
}

func TestIntegrationCredentialsGrant(t *testing.T) {
	var captured map[string]string
	srv := captureTokenBody(t, &captured)
	defer srv.Close()

	c := NewClient(Config{
		BaseURL:     srv.URL,
		Credentials: NewIntegrationCredentials("my-id", "my-secret"),
	})
	assert.NoError(t, c.Authenticate(context.Background()))

	assert.Equal(t, map[string]string{
		"grant_type":    "client_credentials",
		"client_id":     "my-id",
		"client_secret": "my-secret",
	}, captured)
}

func TestPasswordCredentialsGrant(t *testing.T) {
	var captured map[string]string
	srv := captureTokenBody(t, &captured)
	defer srv.Close()

	c := NewClient(Config{
		BaseURL:     srv.URL,
		Credentials: NewPasswordCredentials("admin", "shopware"),
	})
	assert.NoError(t, c.Authenticate(context.Background()))

	assert.Equal(t, map[string]string{
		"grant_type": "password",
		"client_id":  "administration",
		"scopes":     "write",
		"username":   "admin",
		"password":   "shopware",
	}, captured)
}

func TestConfigFallsBackToClientIDSecret(t *testing.T) {
	var captured map[string]string
	srv := captureTokenBody(t, &captured)
	defer srv.Close()

	// No Credentials set: ClientID/ClientSecret must be used as integration creds.
	c := NewClient(Config{BaseURL: srv.URL, ClientID: "legacy-id", ClientSecret: "legacy-secret"})
	assert.NoError(t, c.Authenticate(context.Background()))

	assert.Equal(t, "client_credentials", captured["grant_type"])
	assert.Equal(t, "legacy-id", captured["client_id"])
	assert.Equal(t, "legacy-secret", captured["client_secret"])
}

func TestCredentialsIdentityScopesTokenStorageKey(t *testing.T) {
	assert.Equal(t, "integration:abc", NewIntegrationCredentials("abc", "x").identity())
	assert.Equal(t, "password:admin", NewPasswordCredentials("admin", "x").identity())
	assert.NotEqual(t,
		NewIntegrationCredentials("abc", "x").identity(),
		NewPasswordCredentials("abc", "x").identity(),
		"different grant types for the same name must not share a cache key")
}

func TestRefreshTokenCredentialsGrant(t *testing.T) {
	var captured map[string]string
	srv := captureTokenBody(t, &captured)
	defer srv.Close()

	creds := NewRefreshTokenCredentials("shopware-cli", "initial-rt")
	c := NewClient(Config{
		BaseURL:     srv.URL,
		Credentials: creds,
	})
	assert.NoError(t, c.Authenticate(context.Background()))

	assert.Equal(t, map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     "shopware-cli",
		"refresh_token": "initial-rt",
	}, captured)
}

func TestRefreshTokenCredentialsIdentity(t *testing.T) {
	creds := NewRefreshTokenCredentials("shopware-cli", "rt")
	assert.Equal(t, "pkce:shopware-cli", creds.identity())
}

func TestRefreshTokenCredentialsRotation(t *testing.T) {
	var capturedBodies []map[string]string
	rotationCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/oauth/token" {
			body, _ := io.ReadAll(r.Body)
			var captured map[string]string
			assert.NoError(t, json.Unmarshal(body, &captured))
			capturedBodies = append(capturedBodies, captured)
			rotationCount++

			rt := fmt.Sprintf("rotated-rt-%d", rotationCount)
			w.Header().Set("Content-Type", "application/json")
			resp := fmt.Sprintf(
				`{"access_token":"tok-%d","token_type":"Bearer","expires_in":600,"refresh_token":%q}`,
				rotationCount, rt,
			)
			_, _ = w.Write([]byte(resp))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	creds := NewRefreshTokenCredentials("shopware-cli", "initial-rt")
	c := NewClient(Config{
		BaseURL:      srv.URL,
		Credentials:  creds,
		TokenStorage: NewNoOpTokenStorage(), // disable cache so each Authenticate triggers a fetch
	})

	// First fetch — server returns rotated refresh token
	assert.NoError(t, c.Authenticate(context.Background()))
	// Credentials should have been updated with the new refresh token
	assert.NotEqual(t, "initial-rt", creds.RefreshToken(), "refresh token was not rotated")

	// Second fetch — should use the rotated token
	assert.NoError(t, c.Authenticate(context.Background()))

	// Verify the second request used the rotated refresh token
	assert.Len(t, capturedBodies, 2)
	assert.Equal(t, "initial-rt", capturedBodies[0]["refresh_token"], "first request should use initial RT")
	assert.Equal(t, "rotated-rt-1", capturedBodies[1]["refresh_token"], "second request should use rotated RT")
}

func TestRefreshTokenCredentialsSetRefreshToken(t *testing.T) {
	creds := NewRefreshTokenCredentials("shopware-cli", "old-rt")
	assert.Equal(t, "old-rt", creds.RefreshToken())

	creds.SetRefreshToken("new-rt")
	assert.Equal(t, "new-rt", creds.RefreshToken())
}

func TestSetAccessTokenSeedsCache(t *testing.T) {
	var tokenEndpointHit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/oauth/token" {
			tokenEndpointHit = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"cached","token_type":"Bearer","expires_in":600}`))
	}))
	defer srv.Close()

	c := NewClient(Config{
		BaseURL:     srv.URL,
		Credentials: NewIntegrationCredentials("id", "secret"),
	})

	// Seed the cache with a pre-obtained token
	ctx := context.Background()
	assert.NoError(t, c.SetAccessToken(ctx, "seeded-token", time.Now().Add(5*time.Minute)))

	// A request should use the seeded token without hitting the token endpoint
	_, err := c.Get(ctx, "/_info/config")
	assert.NoError(t, err)
	assert.False(t, tokenEndpointHit, "token endpoint should not be called when cache is seeded")
}

func TestRefreshTokenCredentialsOnRotateRunsBeforeAccessTokenIsStored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"at","expires_in":600,"refresh_token":"rotated-rt"}`))
	}))
	defer srv.Close()

	storage := NewInMemoryTokenStorage()
	creds := NewRefreshTokenCredentials("shopware-cli", "initial-rt")
	var rotated string
	creds.OnRotate = func(ctx context.Context, refreshToken string) error {
		rotated = refreshToken
		token, _, _ := storage.Get(ctx, "pkce:shopware-cli")
		assert.Equal(t, "", token, "access token must not be stored before the rotation is persisted")
		return nil
	}

	c := NewClient(Config{BaseURL: srv.URL, Credentials: creds, TokenStorage: storage})
	assert.NoError(t, c.Authenticate(context.Background()))
	assert.Equal(t, "rotated-rt", rotated)
	assert.Equal(t, "rotated-rt", creds.RefreshToken())
}

func TestRefreshTokenCredentialsOnRotateErrorFailsRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"at","expires_in":600,"refresh_token":"rotated-rt"}`))
	}))
	defer srv.Close()

	creds := NewRefreshTokenCredentials("shopware-cli", "initial-rt")
	creds.OnRotate = func(context.Context, string) error { return errors.New("disk full") }

	c := NewClient(Config{BaseURL: srv.URL, Credentials: creds})
	err := c.Authenticate(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "persist rotated refresh token: disk full")
}
