package shopware

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureTokenBody returns a server that records the JSON body sent to the
// token endpoint and issues a token, so credential request shapes can be
// asserted.
func captureTokenBody(t *testing.T, captured *map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/oauth/token" {
			body, _ := io.ReadAll(r.Body)
			require.NoError(t, json.Unmarshal(body, captured))
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
	require.NoError(t, c.Authenticate(context.Background()))

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
	require.NoError(t, c.Authenticate(context.Background()))

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
	require.NoError(t, c.Authenticate(context.Background()))

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
