package shopware

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopwareLabs/go-shopware-http-client/internal/assert"
)

func TestFileTokenStorageRoundTrip(t *testing.T) {
	s, err := NewFileTokenStorage(t.TempDir())
	assert.NoError(t, err)

	expiry := time.Now().Add(time.Hour)
	assert.NoError(t, s.Set(context.Background(), "shop-1", "tok-1", expiry))

	token, gotExpiry, err := s.Get(context.Background(), "shop-1")
	assert.NoError(t, err)
	assert.Equal(t, "tok-1", token)
	assert.WithinDuration(t, expiry, gotExpiry, time.Second)
}

func TestFileTokenStoragePersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()

	a, err := NewFileTokenStorage(dir)
	assert.NoError(t, err)
	assert.NoError(t, a.Set(context.Background(), "k", "tok", time.Now().Add(time.Hour)))

	b, err := NewFileTokenStorage(dir)
	assert.NoError(t, err)
	token, _, err := b.Get(context.Background(), "k")
	assert.NoError(t, err)
	assert.Equal(t, "tok", token)
}

func TestFileTokenStorageKeysAreIsolated(t *testing.T) {
	s, err := NewFileTokenStorage(t.TempDir())
	assert.NoError(t, err)
	assert.NoError(t, s.Set(context.Background(), "a", "tok-a", time.Now().Add(time.Hour)))

	token, _, err := s.Get(context.Background(), "b")
	assert.NoError(t, err)
	assert.Empty(t, token)
}

func TestFileTokenStorageExpiredTokenIsAbsent(t *testing.T) {
	s, err := NewFileTokenStorage(t.TempDir())
	assert.NoError(t, err)
	assert.NoError(t, s.Set(context.Background(), "k", "tok", time.Now().Add(-time.Minute)))

	token, _, err := s.Get(context.Background(), "k")
	assert.NoError(t, err)
	assert.Empty(t, token, "expired token must be reported as absent")
}

func TestFileTokenStorageDelete(t *testing.T) {
	s, err := NewFileTokenStorage(t.TempDir())
	assert.NoError(t, err)
	assert.NoError(t, s.Set(context.Background(), "k", "tok", time.Now().Add(time.Hour)))
	assert.NoError(t, s.Delete(context.Background(), "k"))

	token, _, err := s.Get(context.Background(), "k")
	assert.NoError(t, err)
	assert.Empty(t, token)

	// Deleting a missing key is not an error.
	assert.NoError(t, s.Delete(context.Background(), "k"))
}

func TestFileTokenStorageMissingKeyIsAbsent(t *testing.T) {
	s, err := NewFileTokenStorage(t.TempDir())
	assert.NoError(t, err)

	token, _, err := s.Get(context.Background(), "nope")
	assert.NoError(t, err)
	assert.Empty(t, token)
}

func TestFileTokenStorageFilenameIsHash(t *testing.T) {
	s, err := NewFileTokenStorage(t.TempDir())
	assert.NoError(t, err)

	key := "integration:super-secret-client-id/with/slashes"
	assert.NoError(t, s.Set(context.Background(), key, "tok", time.Now().Add(time.Hour)))

	path := s.PathForKey(key)
	assert.Equal(t, ".json", filepath.Ext(path))
	assert.False(t, strings.Contains(filepath.Base(path), "super-secret-client-id"),
		"filename must not leak key material, got %q", path)
	assert.False(t, strings.Contains(path, "slashes"),
		"key path separators must not escape the directory, got %q", path)

	entries, err := os.ReadDir(s.Dir())
	assert.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, filepath.Base(path), entries[0].Name())

	info, err := os.Stat(path)
	assert.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestFileTokenStorageCorruptFileIsAbsent(t *testing.T) {
	s, err := NewFileTokenStorage(t.TempDir())
	assert.NoError(t, err)
	assert.NoError(t, s.Set(context.Background(), "k", "tok", time.Now().Add(time.Hour)))
	assert.NoError(t, os.WriteFile(s.PathForKey("k"), []byte("{not json"), 0o600))

	token, _, err := s.Get(context.Background(), "k")
	assert.NoError(t, err)
	assert.Empty(t, token, "corrupt file must self-heal to a miss")
}

func TestFileTokenStorageEmptyDirErrors(t *testing.T) {
	_, err := NewFileTokenStorage("")
	assert.Error(t, err)
}

func TestFileTokenStorageConcurrentUse(t *testing.T) {
	s, err := NewFileTokenStorage(t.TempDir())
	assert.NoError(t, err)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := "k"
			if i%2 == 0 {
				_ = s.Set(context.Background(), key, "tok", time.Now().Add(time.Hour))
			} else {
				_, _, _ = s.Get(context.Background(), key)
			}
		}()
	}
	wg.Wait()
}

func TestScopedTokenStorageIsolatesScopes(t *testing.T) {
	parent := NewInMemoryTokenStorage()
	a := NewScopedTokenStorage(parent, "https://shop-a.example.com")
	b := NewScopedTokenStorage(parent, "https://shop-b.example.com")

	assert.NoError(t, a.Set(context.Background(), "key", "tok-a", time.Now().Add(time.Hour)))

	token, _, err := b.Get(context.Background(), "key")
	assert.NoError(t, err)
	assert.Empty(t, token, "different scopes must not share tokens")

	token, _, err = a.Get(context.Background(), "key")
	assert.NoError(t, err)
	assert.Equal(t, "tok-a", token)

	assert.NoError(t, a.Delete(context.Background(), "key"))
	token, _, err = a.Get(context.Background(), "key")
	assert.NoError(t, err)
	assert.Empty(t, token)
}

func TestScopedFileTokenStorageHashesScopeAndKey(t *testing.T) {
	dir := t.TempDir()
	fileStore, err := NewFileTokenStorage(dir)
	assert.NoError(t, err)

	a := NewScopedTokenStorage(fileStore, "https://shop-a.example.com")
	b := NewScopedTokenStorage(fileStore, "https://shop-b.example.com")

	assert.NoError(t, a.Set(context.Background(), "integration:id", "tok-a", time.Now().Add(time.Hour)))

	token, _, err := b.Get(context.Background(), "integration:id")
	assert.NoError(t, err)
	assert.Empty(t, token)

	token, _, err = a.Get(context.Background(), "integration:id")
	assert.NoError(t, err)
	assert.Equal(t, "tok-a", token)
}

func TestClientUsesFileTokenStorageAcrossClients(t *testing.T) {
	var tokens atomic.Int32
	srv := newTestServer(t, &tokens, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	defer srv.Close()

	dir := t.TempDir()
	newClient := func() *Client {
		fileStore, err := NewFileTokenStorage(dir)
		assert.NoError(t, err)
		return NewClient(Config{
			BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret",
			TokenStorage: fileStore,
		})
	}

	_, err := newClient().Get(context.Background(), "/anything")
	assert.NoError(t, err)
	_, err = newClient().Get(context.Background(), "/anything")
	assert.NoError(t, err)
	assert.Equal(t, int32(1), tokens.Load(), "second client process should reuse the file-cached token")
}
