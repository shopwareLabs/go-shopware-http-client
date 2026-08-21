package shopware

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shyim/go-shopware-http-client/internal/assert"
)

func TestVersionFetchesOnceAndCaches(t *testing.T) {
	var configCalls atomic.Int32
	srv := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/_info/config" {
			configCalls.Add(1)
			_, _ = w.Write([]byte(`{"version":"6.7.1.0"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, ClientID: "i", ClientSecret: "s"})

	for range 3 {
		v, err := c.Version(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, "6.7.1.0", v)
	}
	assert.Equal(t, int32(1), configCalls.Load(), "version is fetched once and cached")
}

func TestRequestRawSendsBodyVerbatim(t *testing.T) {
	var gotBody, gotContentType string
	srv := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotContentType = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{}`))
	})
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, ClientID: "i", ClientSecret: "s"})

	_, err := c.RequestRaw(context.Background(), "POST", "/custom",
		func() (io.Reader, error) { return strings.NewReader("raw-payload"), nil },
		map[string]string{"Content-Type": "application/octet-stream"})
	assert.NoError(t, err)

	assert.Equal(t, "raw-payload", gotBody)
	assert.Equal(t, "application/octet-stream", gotContentType)
}

func TestRequestRawRebuildsBodyOn401(t *testing.T) {
	var calls atomic.Int32
	var bodies []string
	srv := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, ClientID: "i", ClientSecret: "s"})

	_, err := c.RequestRaw(context.Background(), "POST", "/custom",
		func() (io.Reader, error) { return strings.NewReader("payload"), nil }, nil)
	assert.NoError(t, err)

	assert.Len(t, bodies, 2, "request retried once after 401")
	assert.Equal(t, "payload", bodies[0])
	assert.Equal(t, "payload", bodies[1], "body factory rebuilt the body for the retry")
}
