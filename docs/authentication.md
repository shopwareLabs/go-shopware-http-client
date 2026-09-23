# Authentication

[← Docs index](./README.md)

The grant type is selected by the `Credentials` you pass in `Config`. This
mirrors how a Shopware integration and the admin SPA authenticate differently
against the same `/api/oauth/token` endpoint.

The client authenticates lazily on the first request, caches the token, and
refreshes it transparently — you never manage tokens by hand. Call
`client.Authenticate(ctx)` if you want to verify the credentials up front.

When you need the raw bearer token for something outside the client (e.g.
handing it to `curl`), `client.AccessToken(ctx)` returns a valid one, fetching
or refreshing it as needed.

## Integration (client credentials)

Use this for server-to-server access with an API integration's ID and secret.

```go
client := shopware.NewClient(shopware.Config{
	BaseURL:     "https://my-shop.example.com",
	Credentials: shopware.NewIntegrationCredentials("CLIENT_ID", "CLIENT_SECRET"),
})
```

`ClientID` / `ClientSecret` on `Config` are a shorthand for the same thing — if
you leave `Credentials` nil they are used as integration credentials:

```go
// Equivalent to the call above.
client := shopware.NewClient(shopware.Config{
	BaseURL:      "https://my-shop.example.com",
	ClientID:     "CLIENT_ID",
	ClientSecret: "CLIENT_SECRET",
})
```

## Admin user (username / password)

Use this to act as a real admin user — for example a CLI tool that logs in with
the same credentials a person types into the Shopware administration. The
`client_id` is fixed to `administration` and the `write` scope is requested,
matching the admin SPA.

```go
client := shopware.NewClient(shopware.Config{
	BaseURL:     "https://my-shop.example.com",
	Credentials: shopware.NewPasswordCredentials("admin", "shopware"),
})
```

## PKCE (browser-based login for public clients)

Shopware 6.7+ supports OAuth authorization-code grant with PKCE for public
clients (Shopware PR [#20277](https://github.com/shopware/shopware/pull/20277)).
This lets a CLI or native app authenticate on behalf of a user without storing
the user's password or requiring an integration secret.

### Logging in with `pkce.NewClient`

`pkce.NewClient` returns a ready-to-use client and keeps the user logged in
across runs:

```go
import (
	"context"
	"log"

	"github.com/shopwareLabs/go-shopware-http-client/pkce"
)

func main() {
	ctx := context.Background()

	dir, err := pkce.DefaultFileStoreDir() // <user config dir>/go-shopware-http-client/pkce
	if err != nil {
		log.Fatal(err)
	}
	store, err := pkce.NewFileStore(dir)
	if err != nil {
		log.Fatal(err)
	}

	client, err := pkce.NewClient(ctx, pkce.Config{
		BaseURL: "https://my-shop.example.com",
	}, store)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Get(ctx, "/_info/me")
	// ...
}
```

What `NewClient` does:

- **Saved session:** if the store has a session for the shop, it is reused
  without opening the browser.
- **No session, or the refresh token was rejected:** it runs the browser login
  (verifier and S256 challenge, a loopback callback server, the code exchange)
  and saves the new session.
- **While running:** the access token and the refresh token live together in
  the store. Every change is written as it happens. Shopware revokes the old
  refresh token on each refresh, so the rotated one is saved before the new
  access token is used. A crash or an early return can no longer log the user
  out.

Sessions are keyed by base URL, so one store can hold logins for many shops.
Implement `pkce.Store` (`Load`, `Save`, `Delete`) to keep sessions somewhere
else, such as the OS keychain. Call `store.Delete(ctx, baseURL)` to log out.

### Managing the tokens yourself

`pkce.Login` runs only the browser flow and returns the tokens. Pair it with
`RefreshTokenCredentials` and set `OnRotate`, which is called with each new
refresh token before the new access token is stored:

```go
tokens, err := pkce.Login(ctx, pkce.Config{BaseURL: "https://my-shop.example.com"})
if err != nil {
	log.Fatal(err)
}

creds := shopware.NewRefreshTokenCredentials(tokens.ClientID, tokens.RefreshToken)
creds.OnRotate = func(ctx context.Context, refreshToken string) error {
	return saveRefreshToken(refreshToken) // your own persistence
}

client := shopware.NewClient(shopware.Config{
	BaseURL:     tokens.BaseURL,
	Credentials: creds,
})

// Seed the access token so the first call needs no refresh round-trip.
_ = client.SetAccessToken(ctx, tokens.AccessToken, tokens.Expiry)
```

If `OnRotate` returns an error, the request that triggered the refresh fails
with it, so a token that could not be saved never goes unnoticed.

### Customizing the PKCE flow

`pkce.NewClient` and `pkce.Login` take the same `Config`:

```go
client, err := pkce.NewClient(ctx, pkce.Config{
	BaseURL:      "https://my-shop.example.com",
	ClientID:     "my-custom-cli",    // default: "shopware-cli"
	Scope:        "write",            // default: "write"
	CallbackHost: "127.0.0.1",        // default: "127.0.0.1"
	CallbackPort: 0,                  // 0 = OS-assigned random port
	Timeout:      10 * time.Minute,   // default: 10 minutes
	OpenURL: func(u string) error {   // custom browser opener
		return open.Run(u)
	},
	HTTPClient: &http.Client{         // custom HTTP client for token and API requests
		Timeout: 30 * time.Second,
	},
}, store)
```

## Custom credentials

Implement the `Credentials` interface to support any other token body. The
interface is intentionally small:

```go
type Credentials interface {
	tokenRequest() map[string]string // JSON body POSTed to /api/oauth/token
	identity() string                // stable key, used as the default token-storage key
}
```

> The interface methods are unexported, so custom implementations must live in
> this package. If you need a third grant from outside the package, open a PR
> adding it next to `IntegrationCredentials` / `PasswordCredentials`.

## See also

- [Token storage](./token-storage.md) — caching tokens, distributed backends.
- [Error handling](./error-handling.md) — the one-shot 401 re-auth retry.
- [PKCE CLI example](../examples/pkce-cli/) — a runnable demo of the full browser-login flow.
