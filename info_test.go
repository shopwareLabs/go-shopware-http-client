package shopware

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/shopwareLabs/go-shopware-http-client/internal/assert"
)

func TestInfoDecodesAndCaches(t *testing.T) {
	var configCalls atomic.Int32
	srv := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		configCalls.Add(1)
		assert.Equal(t, "/api/_info/config", r.URL.Path)
		_, _ = w.Write([]byte(`{"version":"6.7.1.0","versionRevision":"abc","bundles":{"SaasRufus":{"css":["a.css"],"js":["a.js"]}}}`))
	})
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, ClientID: "i", ClientSecret: "s"})

	info, err := c.Info(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "6.7.1.0", info.Version)
	assert.Equal(t, "abc", info.VersionRevision)
	assert.Equal(t, []string{"a.js"}, info.Bundles["SaasRufus"].JS)
	assert.True(t, info.HasBundle("SaasRufus"))
	assert.False(t, info.HasBundle("Storefront"))

	v, err := c.Version(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "6.7.1.0", v)
	assert.Equal(t, int32(1), configCalls.Load(), "Info and Version share one fetch")
}

func TestAccessTokenReturnsCachedToken(t *testing.T) {
	var tokenCalls atomic.Int32
	srv := newTestServer(t, &tokenCalls, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, ClientID: "i", ClientSecret: "s"})

	for range 2 {
		token, err := c.AccessToken(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, "token-abc", token)
	}
	_, err := c.Get(context.Background(), "/anything")
	assert.NoError(t, err)
	assert.Equal(t, int32(1), tokenCalls.Load())
}

func TestClearCache(t *testing.T) {
	var method, path string
	srv := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, ClientID: "i", ClientSecret: "s"})
	assert.NoError(t, c.ClearCache(context.Background()))
	assert.Equal(t, http.MethodDelete, method)
	assert.Equal(t, "/api/_action/cache", path)
}
