package shopware

import (
	"context"
	"testing"
	"time"

	"github.com/shopwareLabs/go-shopware-http-client/internal/assert"
)

func TestNoOpTokenStorageNeverCaches(t *testing.T) {
	s := NewNoOpTokenStorage()
	assert.NoError(t, s.Set(context.Background(), "k", "tok", time.Now().Add(time.Hour)))

	token, _, err := s.Get(context.Background(), "k")
	assert.NoError(t, err)
	assert.Empty(t, token)
}

func TestInMemoryTokenStorageRoundTrip(t *testing.T) {
	s := NewInMemoryTokenStorage()
	expiry := time.Now().Add(time.Hour)
	assert.NoError(t, s.Set(context.Background(), "shop-1", "tok-1", expiry))

	token, gotExpiry, err := s.Get(context.Background(), "shop-1")
	assert.NoError(t, err)
	assert.Equal(t, "tok-1", token)
	assert.WithinDuration(t, expiry, gotExpiry, time.Second)
}

func TestInMemoryTokenStorageKeysAreIsolated(t *testing.T) {
	s := NewInMemoryTokenStorage()
	assert.NoError(t, s.Set(context.Background(), "a", "tok-a", time.Now().Add(time.Hour)))

	token, _, err := s.Get(context.Background(), "b")
	assert.NoError(t, err)
	assert.Empty(t, token)
}

func TestInMemoryTokenStorageExpiredTokenIsAbsent(t *testing.T) {
	s := NewInMemoryTokenStorage()
	assert.NoError(t, s.Set(context.Background(), "k", "tok", time.Now().Add(-time.Minute)))

	token, _, err := s.Get(context.Background(), "k")
	assert.NoError(t, err)
	assert.Empty(t, token, "expired token must be reported as absent")
}

func TestInMemoryTokenStorageDelete(t *testing.T) {
	s := NewInMemoryTokenStorage()
	assert.NoError(t, s.Set(context.Background(), "k", "tok", time.Now().Add(time.Hour)))
	assert.NoError(t, s.Delete(context.Background(), "k"))

	token, _, err := s.Get(context.Background(), "k")
	assert.NoError(t, err)
	assert.Empty(t, token)
}
