package media

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	shopware "github.com/shyim/go-shopware-http-client"
	"github.com/shyim/go-shopware-http-client/internal/assert"
)

// syncOp is the shape of a /_action/sync operation, used to inspect upserts and
// deletes the manager issues.
type syncOp struct {
	Entity   string           `json:"entity"`
	Action   string           `json:"action"`
	Payload  []map[string]any `json:"payload"`
	Criteria []map[string]any `json:"criteria"`
}

func testServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/oauth/token" {
			_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer","expires_in":600}`))
			return
		}
		handler(w, r)
	}))
}

func newManager(url string) *Manager {
	return NewManager(shopware.NewClient(shopware.Config{
		BaseURL: url, ClientID: "i", ClientSecret: "s",
	}))
}

func decodeSync(t *testing.T, body io.Reader) []syncOp {
	t.Helper()
	var ops []syncOp
	assert.NoError(t, json.UnmarshalRead(body, &ops))
	return ops
}

func TestUpload(t *testing.T) {
	var syncOps []syncOp
	var uploadPath, uploadBody, uploadContentType string
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/_action/sync":
			syncOps = decodeSync(t, r.Body)
			_, _ = w.Write([]byte(`{}`))
		case strings.HasPrefix(r.URL.Path, "/api/_action/media/"):
			uploadPath = r.URL.Path
			uploadContentType = r.Header.Get("Content-Type")
			b, _ := io.ReadAll(r.Body)
			uploadBody = string(b)
			// echo back the query so we can assert it
			assert.Equal(t, "png", r.URL.Query().Get("extension"))
			assert.Equal(t, "logo", r.URL.Query().Get("fileName"))
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	id, err := newManager(srv.URL).Upload(context.Background(),
		strings.NewReader("PNGDATA"),
		UploadOptions{FileName: "logo.png", ContentType: "image/png", Private: true})
	assert.NoError(t, err)
	assert.Len(t, id, 32, "media id is a stripped uuid")

	// The media entity was upserted first, carrying our id and private flag.
	assert.Len(t, syncOps, 1)
	assert.Equal(t, "media", syncOps[0].Entity)
	assert.Equal(t, "upsert", syncOps[0].Action)
	assert.Len(t, syncOps[0].Payload, 1)
	assert.Equal(t, id, syncOps[0].Payload[0]["id"])
	assert.Equal(t, true, syncOps[0].Payload[0]["private"])

	// Then the binary was uploaded to the right path with the content type.
	assert.Equal(t, "/api/_action/media/"+id+"/upload", uploadPath)
	assert.Equal(t, "PNGDATA", uploadBody)
	assert.Equal(t, "image/png", uploadContentType)
}

func TestUploadRollsBackOnUploadFailure(t *testing.T) {
	var actions []string
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/_action/sync":
			for _, op := range decodeSync(t, r.Body) {
				actions = append(actions, op.Action)
			}
			_, _ = w.Write([]byte(`{}`))
		case strings.HasPrefix(r.URL.Path, "/api/_action/media/"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":[{"detail":"bad file"}]}`))
		}
	})
	defer srv.Close()

	_, err := newManager(srv.URL).Upload(context.Background(),
		strings.NewReader("x"), UploadOptions{FileName: "a.png"})
	assert.Error(t, err)

	// upsert (create) followed by delete (rollback).
	assert.Equal(t, []string{"upsert", "delete"}, actions)
}

func TestUploadByURL(t *testing.T) {
	var uploadBody map[string]any
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/_action/sync":
			_, _ = w.Write([]byte(`{}`))
		case strings.HasPrefix(r.URL.Path, "/api/_action/media/"):
			assert.NoError(t, json.UnmarshalRead(r.Body, &uploadBody))
			_, _ = w.Write([]byte(`{}`))
		}
	})
	defer srv.Close()

	id, err := newManager(srv.URL).UploadByURL(context.Background(), UploadURLOptions{
		FileName: "image.jpg",
		URL:      "https://example.com/image.jpg",
	})
	assert.NoError(t, err)
	assert.Len(t, id, 32)

	assert.Equal(t, "image", uploadBody["fileName"])
	assert.Equal(t, "jpg", uploadBody["extension"])
	assert.Equal(t, "https://example.com/image.jpg", uploadBody["url"])
}

func TestInvalidFileNameDoesNotCreateMedia(t *testing.T) {
	called := false
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	})
	defer srv.Close()

	for _, name := range []string{"noext", ".png", "trailingdot."} {
		_, err := newManager(srv.URL).Upload(context.Background(),
			strings.NewReader("x"), UploadOptions{FileName: name})
		assert.Error(t, err, "file name %q must be rejected", name)
	}
	assert.False(t, called, "no request is made when the file name is invalid")
}

func TestDefaultFolderByEntity(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/search/media-default-folder", r.URL.Path)
		_, _ = w.Write([]byte(`{"total":1,"data":[{"folder":{"id":"folder-123"}}]}`))
	})
	defer srv.Close()

	id, err := newManager(srv.URL).DefaultFolderByEntity(context.Background(), "product")
	assert.NoError(t, err)
	assert.Equal(t, "folder-123", id)
}

func TestFolderByNameNotFound(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/search/media-folder", r.URL.Path)
		_, _ = w.Write([]byte(`{"total":0,"data":[]}`))
	})
	defer srv.Close()

	id, err := newManager(srv.URL).FolderByName(context.Background(), "Missing")
	assert.NoError(t, err)
	assert.Empty(t, id)
}

func TestCreateFolder(t *testing.T) {
	var op syncOp
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		ops := decodeSync(t, r.Body)
		assert.Len(t, ops, 1)
		op = ops[0]
		_, _ = w.Write([]byte(`{}`))
	})
	defer srv.Close()

	id, err := newManager(srv.URL).CreateFolder(context.Background(), "Products",
		CreateFolderOptions{ParentID: "parent-1"})
	assert.NoError(t, err)
	assert.Len(t, id, 32)

	assert.Equal(t, "media_folder", op.Entity)
	assert.Equal(t, "upsert", op.Action)
	assert.Equal(t, "Products", op.Payload[0]["name"])
	assert.Equal(t, "parent-1", op.Payload[0]["parentId"])
}

func TestCreateFolderWithoutParentSendsNull(t *testing.T) {
	var op syncOp
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		op = decodeSync(t, r.Body)[0]
		_, _ = w.Write([]byte(`{}`))
	})
	defer srv.Close()

	_, err := newManager(srv.URL).CreateFolder(context.Background(), "Root", CreateFolderOptions{})
	assert.NoError(t, err)
	assert.Nil(t, op.Payload[0]["parentId"], "empty parent serializes as null")
}
