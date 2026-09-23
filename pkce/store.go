package pkce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Store persists PKCE sessions, one per shop, so users stay logged in across
// runs. Implementations must be safe for concurrent use.
type Store interface {
	// Load returns the session saved for baseURL, or nil when there is none.
	Load(ctx context.Context, baseURL string) (*Tokens, error)

	// Save persists tokens, replacing any session saved for tokens.BaseURL.
	Save(ctx context.Context, tokens *Tokens) error

	// Delete removes the session for baseURL. A missing session is not an
	// error.
	Delete(ctx context.Context, baseURL string) error
}

// FileStore is a Store that keeps one JSON file per shop in a directory. The
// filename is the hex-encoded SHA-256 hash of the base URL. Writes are atomic
// (write-temp-then-rename), files are created with mode 0600 and the directory
// with mode 0700.
//
// A FileStore is safe for concurrent use within a process.
type FileStore struct {
	dir string
	mu  sync.Mutex
}

// NewFileStore creates a FileStore rooted at dir, creating the directory when
// it does not exist.
func NewFileStore(dir string) (*FileStore, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("pkce file store: directory must not be empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("pkce file store: create directory %q: %w", dir, err)
	}
	return &FileStore{dir: dir}, nil
}

// DefaultFileStoreDir returns a user-private default directory for a
// FileStore: <os.UserConfigDir()>/go-shopware-http-client/pkce. Refresh tokens
// are long-lived credentials, so they live in the config dir, not the cache.
func DefaultFileStoreDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("pkce file store: user config dir: %w", err)
	}
	return filepath.Join(base, "go-shopware-http-client", "pkce"), nil
}

// PathForBaseURL returns the file backing the session for baseURL.
func (s *FileStore) PathForBaseURL(baseURL string) string {
	sum := sha256.Sum256([]byte(normalizeBaseURL(baseURL)))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}

// Load implements Store.
func (s *FileStore) Load(ctx context.Context, baseURL string) (*Tokens, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.PathForBaseURL(baseURL))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("pkce file store: read session: %w", err)
	}

	var tokens Tokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("pkce file store: decode session: %w", err)
	}
	return &tokens, nil
}

// Save implements Store.
func (s *FileStore) Save(ctx context.Context, tokens *Tokens) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	data, err := json.Marshal(tokens)
	if err != nil {
		return fmt.Errorf("pkce file store: encode session: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("pkce file store: create directory %q: %w", s.dir, err)
	}

	tmp, err := os.CreateTemp(s.dir, ".tmp-*.json")
	if err != nil {
		return fmt.Errorf("pkce file store: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("pkce file store: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("pkce file store: close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("pkce file store: chmod temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.PathForBaseURL(tokens.BaseURL)); err != nil {
		return fmt.Errorf("pkce file store: persist session: %w", err)
	}
	return nil
}

// Delete implements Store.
func (s *FileStore) Delete(ctx context.Context, baseURL string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(s.PathForBaseURL(baseURL)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("pkce file store: delete session: %w", err)
	}
	return nil
}

func normalizeBaseURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/")
}
