package pkce

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shopwareLabs/go-shopware-http-client/internal/assert"
)

func TestFileStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	assert.NoError(t, err)

	missing, err := store.Load(ctx, "https://shop.example.com")
	assert.NoError(t, err)
	assert.Nil(t, missing)

	want := &Tokens{
		BaseURL: "https://shop.example.com", ClientID: "shopware-cli",
		AccessToken: "at", RefreshToken: "rt", Expiry: time.Now().Add(time.Hour).Round(time.Second),
	}
	assert.NoError(t, store.Save(ctx, want))

	got, err := store.Load(ctx, "https://shop.example.com/")
	assert.NoError(t, err)
	assert.Equal(t, want.RefreshToken, got.RefreshToken)
	assert.Equal(t, want.AccessToken, got.AccessToken)
	assert.True(t, want.Expiry.Equal(got.Expiry))

	info, err := os.Stat(store.PathForBaseURL(want.BaseURL))
	assert.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	other, err := store.Load(ctx, "https://other.example.com")
	assert.NoError(t, err)
	assert.Nil(t, other, "sessions are per shop")

	assert.NoError(t, store.Delete(ctx, want.BaseURL))
	gone, err := store.Load(ctx, want.BaseURL)
	assert.NoError(t, err)
	assert.Nil(t, gone)
	assert.NoError(t, store.Delete(ctx, want.BaseURL), "deleting a missing session is fine")
}

func TestNewFileStoreRejectsEmptyDir(t *testing.T) {
	_, err := NewFileStore(" ")
	assert.Error(t, err)
}
