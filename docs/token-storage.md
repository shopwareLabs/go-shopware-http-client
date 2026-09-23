# Token storage

[← Docs index](./README.md)

By default the client caches the OAuth token in-process for the lifetime of the
`Client`. Swap the backend via `Config.TokenStorage`:

- `NewInMemoryTokenStorage()` — process-local cache (the default when
  `TokenStorage` is nil). Tokens are lost on restart and not shared across
  processes.
- `NewFileTokenStorage(dir)` — disk cache ([see below](#file-storage)).
  Tokens survive restarts and are shared between processes on the same machine.
- `NewNoOpTokenStorage()` — no caching; every request fetches a fresh token.
  Suitable for tests or callers that manage tokens elsewhere.

```go
// Default: process-local cache (same as not setting it).
shopware.Config{ TokenStorage: shopware.NewInMemoryTokenStorage() }

// No caching — fetch a fresh token on every request (tests / low traffic).
shopware.Config{ TokenStorage: shopware.NewNoOpTokenStorage() }
```

## Storage keys

Tokens are keyed by the credentials' identity by default (e.g.
`integration:<client-id>` vs `password:<username>`), so distinct principals
never share a cached token.

The default key identifies only the principal, not the shop. When several
clients share one storage but talk to **different shops** with the same
principal, scope each client — otherwise they share one cached token:

```go
// Option 1: scope any backend (file, in-memory, Redis) by shop.
shopware.Config{
	TokenStorage: shopware.NewScopedTokenStorage(sharedStore, shopURL),
}

// Option 2: set an explicit key per shop.
shopware.Config{
	TokenStorage:    sharedStore,
	TokenStorageKey: "shop-" + shopID,
}
```

## File storage

`NewFileTokenStorage(dir)` persists one file per storage key. The filename is
the hex-encoded SHA-256 hash of the (possibly scoped) key (`<sha256>.json`),
so client IDs and usernames never appear in the directory listing, and keys
containing path separators cannot escape the directory.

- Files are written atomically (temp file + rename) with mode `0600`; the
  directory is created with mode `0700`.
- Missing, expired, and corrupt files all read as "no cached token", so the
  client simply fetches a fresh one. Expired and corrupt files are removed on
  a best-effort basis.
- `DefaultFileTokenStorageDir()` returns a user-private default directory
  (`<os.UserCacheDir()>/go-shopware-http-client/tokens`) to pass to
  `NewFileTokenStorage`.

```go
dir, err := shopware.DefaultFileTokenStorageDir()
if err != nil {
	log.Fatal(err)
}
store, err := shopware.NewFileTokenStorage(dir)
if err != nil {
	log.Fatal(err)
}

shopURL := "https://my-shop.example.com"
client := shopware.NewClient(shopware.Config{
	BaseURL:      shopURL,
	ClientID:     "CLIENT_ID",
	ClientSecret: "CLIENT_SECRET",
	// Scope by shop so one shared directory stays correct if the same
	// principal is ever used against multiple shops.
	TokenStorage: shopware.NewScopedTokenStorage(store, shopURL),
})
```

## Custom backends

Implement `shopware.TokenStorage` for anything else (Redis, keychain, ...):

```go
type TokenStorage interface {
	Get(ctx context.Context, key string) (token string, expiry time.Time, err error)
	Set(ctx context.Context, key string, token string, expiry time.Time) error
	Delete(ctx context.Context, key string) error
}
```

Treat expired tokens as absent: `Get` must return an empty token once the
stored expiry has passed, prompting the client to fetch a fresh one.

## Expiry

The client stores each token's real expiry and applies a 30s safety margin when
reading it back, so a token is never used in the window where the server might
already reject it.

## See also

- [Authentication](./authentication.md) — how the cached token is obtained.
- [Concurrency](./concurrency.md) — concurrent token fetches are collapsed.
