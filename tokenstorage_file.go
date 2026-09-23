package shopware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FileTokenStorage is a TokenStorage that persists OAuth tokens on disk, one
// file per storage key. The filename is the hex-encoded SHA-256 hash of the
// key (plus a ".json" suffix), so neither the client ID, username, nor any
// other key material ever appears in the directory listing, and keys with
// characters that are unsafe for filenames (or path separators) cannot escape
// the directory.
//
// Because the key is hashed, distinct principals never share a file — the same
// guarantee InMemoryTokenStorage gives. But note the default storage key only
// identifies the principal (e.g. "integration:<client-id>"), not the shop. If
// a single directory is shared by clients talking to *different* shops with
// the same principal, scope each client with NewScopedTokenStorage (or set
// Config.TokenStorageKey explicitly); otherwise they will share one file and
// one token.
//
// Tokens survive process restarts and are shared between processes on the same
// machine. Writes are atomic (write-temp-then-rename) and files are created
// with mode 0600. The directory is created with mode 0700 when missing. Like
// all TokenStorage implementations, expired tokens are treated as absent.
//
// A FileTokenStorage is safe for concurrent use.
type FileTokenStorage struct {
	dir string

	// mu serializes in-process access so concurrent Set/Delete/Get on the same
	// key do not interleave temp-file creation awkwardly. Cross-process safety
	// comes from atomic renames on Set and atomic reads on Get.
	mu sync.Mutex
}

// NewFileTokenStorage creates a FileTokenStorage rooted at dir, creating the
// directory (with mode 0700) when it does not exist. It returns an error when
// dir is empty or the directory cannot be created.
func NewFileTokenStorage(dir string) (*FileTokenStorage, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("file token storage: directory must not be empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("file token storage: create directory %q: %w", dir, err)
	}
	return &FileTokenStorage{dir: dir}, nil
}

// Dir returns the directory tokens are stored in.
func (s *FileTokenStorage) Dir() string { return s.dir }

// PathForKey returns the file backing key. It is exposed for debugging and
// tooling (e.g. listing or pruning cached tokens); callers should otherwise
// treat the layout as opaque.
func (s *FileTokenStorage) PathForKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}

type fileTokenEntry struct {
	Token  string    `json:"token"`
	Expiry time.Time `json:"expiry"`
}

// Get returns the cached token for key, or an empty token when no valid
// (unexpired) token exists. Missing, expired, and unreadable/corrupt files all
// report a miss; corrupt and expired files are removed on a best-effort basis
// so the next request fetches (and stores) a fresh token.
func (s *FileTokenStorage) Get(ctx context.Context, key string) (string, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return "", time.Time{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.PathForKey(key)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", time.Time{}, nil
		}
		return "", time.Time{}, fmt.Errorf("file token storage: read %q: %w", path, err)
	}

	var entry fileTokenEntry
	if err := json.Unmarshal(data, &entry); err != nil || entry.Token == "" {
		// Corrupt or empty entry: self-heal by dropping the file and
		// reporting a miss so the client fetches a fresh token.
		_ = os.Remove(path)
		if err != nil {
			// Still surface nothing: a corrupt cache must not hard-fail
			// requests when a fresh token can be obtained.
			return "", time.Time{}, nil
		}
		return "", time.Time{}, nil
	}

	if time.Now().After(entry.Expiry) {
		_ = os.Remove(path)
		return "", time.Time{}, nil
	}
	return entry.Token, entry.Expiry, nil
}

// Set stores token for key with its expiry, atomically and with mode 0600.
func (s *FileTokenStorage) Set(ctx context.Context, key, token string, expiry time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("file token storage: create directory %q: %w", s.dir, err)
	}

	data, err := json.Marshal(fileTokenEntry{Token: token, Expiry: expiry})
	if err != nil {
		return fmt.Errorf("file token storage: encode token: %w", err)
	}

	tmp, err := os.CreateTemp(s.dir, ".tmp-*.json")
	if err != nil {
		return fmt.Errorf("file token storage: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	// Best effort cleanup on failure; a successful rename removes the temp
	// name from the directory anyway.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("file token storage: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("file token storage: close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("file token storage: chmod temp file: %w", err)
	}
	path := s.PathForKey(key)
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("file token storage: persist token: %w", err)
	}
	return nil
}

// Delete removes the token for key. A missing file is not an error.
func (s *FileTokenStorage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(s.PathForKey(key)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("file token storage: delete token: %w", err)
	}
	return nil
}

// DefaultFileTokenStorageDir returns a user-private default directory for file
// token storage: <os.UserCacheDir()>/go-shopware-http-client/tokens. Callers
// typically pass it to NewFileTokenStorage.
func DefaultFileTokenStorageDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("file token storage: user cache dir: %w", err)
	}
	return filepath.Join(base, "go-shopware-http-client", "tokens"), nil
}

// ScopedTokenStorage prefixes every key with a fixed scope before delegating
// to its parent storage. Use it to namespace one shared backend — e.g. a
// FileTokenStorage directory — per shop:
//
//	fileStore, err := shopware.NewFileTokenStorage(dir)
//	storage := shopware.NewScopedTokenStorage(fileStore, "https://shop.example.com")
//	client := shopware.NewClient(shopware.Config{
//		BaseURL:      "https://shop.example.com",
//		Credentials:  shopware.NewIntegrationCredentials(id, secret),
//		TokenStorage: storage,
//	})
//
// The default storage key only identifies the principal
// ("integration:<client-id>", "password:<username>", "pkce:<client-id>"), so
// without scoping, clients with the same principal against different shops
// would share one cached token. ScopedTokenStorage makes the effective key
// "<scope>\x00<key>", which FileTokenStorage then hashes into the filename —
// effectively a hash of shop URL + client-id/username.
//
// A ScopedTokenStorage is safe for concurrent use when its parent is.
type ScopedTokenStorage struct {
	parent TokenStorage
	scope  string
}

// NewScopedTokenStorage wraps parent so every key is namespaced under scope.
// Scope is typically the shop's BaseURL (any non-empty string works).
func NewScopedTokenStorage(parent TokenStorage, scope string) *ScopedTokenStorage {
	return &ScopedTokenStorage{parent: parent, scope: scope}
}

// Scope returns the configured scope.
func (s *ScopedTokenStorage) Scope() string { return s.scope }

// Unwrap returns the parent storage.
func (s *ScopedTokenStorage) Unwrap() TokenStorage { return s.parent }

// scopedKey joins scope and key with a NUL separator so "ab"+"c" and "a"+"bc"
// can never produce the same effective key.
func (s *ScopedTokenStorage) scopedKey(key string) string {
	return s.scope + "\x00" + key
}

// Get retrieves the token for the scoped key.
func (s *ScopedTokenStorage) Get(ctx context.Context, key string) (string, time.Time, error) {
	return s.parent.Get(ctx, s.scopedKey(key))
}

// Set stores the token under the scoped key.
func (s *ScopedTokenStorage) Set(ctx context.Context, key, token string, expiry time.Time) error {
	return s.parent.Set(ctx, s.scopedKey(key), token, expiry)
}

// Delete removes the token stored under the scoped key.
func (s *ScopedTokenStorage) Delete(ctx context.Context, key string) error {
	return s.parent.Delete(ctx, s.scopedKey(key))
}
