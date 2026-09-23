package instance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	shopware "github.com/shopwareLabs/go-shopware-http-client"
	"github.com/shopwareLabs/go-shopware-http-client/internal/assert"
)

func TestClearCache(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/oauth/token" {
			_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer","expires_in":600}`))
			return
		}
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	mgr := NewManager(shopware.NewClient(shopware.Config{BaseURL: srv.URL, ClientID: "i", ClientSecret: "s"}))
	assert.NoError(t, mgr.ClearCache(context.Background()))
	assert.Equal(t, http.MethodDelete, method)
	assert.Equal(t, "/api/_action/cache", path)
}
