package extension

import (
	"context"
	"encoding/json/v2"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	shopware "github.com/shopwareLabs/go-shopware-http-client"
	"github.com/shopwareLabs/go-shopware-http-client/internal/assert"
)

// testServer issues tokens and serves /_info/config with the given version,
// delegating the rest to handler.
func testServer(t *testing.T, version string, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/oauth/token":
			_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer","expires_in":600}`))
		case "/api/_info/config":
			_, _ = w.Write([]byte(`{"version":"` + version + `"}`))
		default:
			handler(w, r)
		}
	}))
}

func newManager(url string) *Manager {
	return NewManager(shopware.NewClient(shopware.Config{
		BaseURL: url, ClientID: "i", ClientSecret: "s",
	}))
}

func TestListAvailable(t *testing.T) {
	srv := testServer(t, "6.7.0.0", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/_action/extension/installed", r.URL.Path)
		_, _ = w.Write([]byte(`[
			{"name":"SwagA","version":"1.0.0","latestVersion":"1.1.0","type":"plugin","active":true},
			{"name":"SwagB","version":"2.0.0","latestVersion":"2.0.0","type":"app"}
		]`))
	})
	defer srv.Close()

	list, err := newManager(srv.URL).ListAvailable(context.Background())
	assert.NoError(t, err)
	assert.Len(t, list, 2)

	a := list.GetByName("SwagA")
	assert.NotNil(t, a)
	assert.True(t, a.IsPlugin())
	assert.True(t, a.IsUpdatable())

	updatable := list.FilterByUpdatable()
	assert.Len(t, updatable, 1)
	assert.Equal(t, "SwagA", updatable[0].Name)
}

func TestLifecycleMethodsAndPaths(t *testing.T) {
	cases := []struct {
		name       string
		call       func(m *Manager) error
		wantMethod string
		wantPath   string
	}{
		{"install", func(m *Manager) error { return m.Install(ctx(), "plugin", "Foo") }, "POST", "/api/_action/extension/install/plugin/Foo"},
		{"uninstall", func(m *Manager) error { return m.Uninstall(ctx(), "plugin", "Foo") }, "POST", "/api/_action/extension/uninstall/plugin/Foo"},
		{"update", func(m *Manager) error { return m.Update(ctx(), "plugin", "Foo") }, "POST", "/api/_action/extension/update/plugin/Foo"},
		{"activate", func(m *Manager) error { return m.Activate(ctx(), "plugin", "Foo") }, "PUT", "/api/_action/extension/activate/plugin/Foo"},
		{"deactivate", func(m *Manager) error { return m.Deactivate(ctx(), "plugin", "Foo") }, "PUT", "/api/_action/extension/deactivate/plugin/Foo"},
		{"download", func(m *Manager) error { return m.Download(ctx(), "Foo") }, "POST", "/api/_action/extension/download/Foo"},
		{"refresh", func(m *Manager) error { return m.Refresh(ctx()) }, "POST", "/api/_action/extension/refresh"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotMethod, gotPath string
			srv := testServer(t, "6.7.0.0", func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				_, _ = w.Write([]byte(`{}`))
			})
			defer srv.Close()

			assert.NoError(t, tc.call(newManager(srv.URL)))
			assert.Equal(t, tc.wantMethod, gotMethod)
			assert.Equal(t, tc.wantPath, gotPath)
		})
	}
}

func TestRemoveVersionGate(t *testing.T) {
	cases := []struct {
		version    string
		wantMethod string
	}{
		{"6.6.10.2", "POST"},
		{"6.7.0.0", "POST"},
		{"6.6.10.1", "DELETE"},
		{"6.5.0.0", "DELETE"},
	}

	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			var gotMethod string
			srv := testServer(t, tc.version, func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				_, _ = w.Write([]byte(`{}`))
			})
			defer srv.Close()

			assert.NoError(t, newManager(srv.URL).Remove(context.Background(), "plugin", "Foo"))
			assert.Equal(t, tc.wantMethod, gotMethod)
		})
	}
}

func TestUploadMultipart(t *testing.T) {
	var gotPath, gotContentType string
	var gotFile string
	srv := testServer(t, "6.7.0.0", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")

		_, params, err := mime.ParseMediaType(gotContentType)
		assert.NoError(t, err)
		mr := multipartReader(t, r.Body, params["boundary"])
		gotFile = mr
		_, _ = w.Write([]byte(`{}`))
	})
	defer srv.Close()

	err := newManager(srv.URL).Upload(context.Background(), strings.NewReader("ZIPDATA"))
	assert.NoError(t, err)

	assert.Equal(t, "/api/_action/extension/upload", gotPath)
	assert.True(t, strings.HasPrefix(gotContentType, "multipart/form-data"))
	assert.Equal(t, "ZIPDATA", gotFile)
}

func TestUploadUpdateToCloudIncludesMediaField(t *testing.T) {
	var fields map[string]string
	var file string
	srv := testServer(t, "6.7.0.0", func(w http.ResponseWriter, r *http.Request) {
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		assert.NoError(t, err)
		fields, file = multipartAll(t, r.Body, params["boundary"])
		_, _ = w.Write([]byte(`{}`))
	})
	defer srv.Close()

	err := newManager(srv.URL).UploadUpdateToCloud(context.Background(), "SwagFoo", strings.NewReader("ZIP"))
	assert.NoError(t, err)
	assert.Equal(t, "SwagFoo", fields["media"])
	assert.Equal(t, "ZIP", file)
}

func TestDetailStatusAndDate(t *testing.T) {
	raw := `{"name":"X","version":"1.0.0","latestVersion":"1.2.0","active":true,
		"installedAt":{"date":"2026-01-01 00:00:00.000000","timezone_type":3,"timezone":"UTC"}}`
	var d Detail
	assert.NoError(t, json.Unmarshal([]byte(raw), &d))

	assert.NotNil(t, d.InstalledAt)
	assert.Equal(t, "UTC", d.InstalledAt.Timezone)
	assert.Contains(t, d.Status(), "update available to 1.2.0")
}

// --- helpers ---

func ctx() context.Context { return context.Background() }

// multipartReader returns the "file" part's body.
func multipartReader(t *testing.T, body io.Reader, boundary string) string {
	t.Helper()
	_, file := multipartAll(t, body, boundary)
	return file
}

// multipartAll returns the non-file form fields and the "file" part body.
func multipartAll(t *testing.T, body io.Reader, boundary string) (map[string]string, string) {
	t.Helper()
	fields := map[string]string{}
	var file string

	mr := multipart.NewReader(body, boundary)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		assert.NoError(t, err)
		data, err := io.ReadAll(part)
		assert.NoError(t, err)
		if part.FormName() == "file" {
			file = string(data)
		} else {
			fields[part.FormName()] = string(data)
		}
	}
	return fields, file
}
