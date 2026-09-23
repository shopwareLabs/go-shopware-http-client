# Authentication

[← Docs index](./README.md)

The grant type is selected by the `Credentials` you pass in `Config`. This
mirrors how a Shopware integration and the admin SPA authenticate differently
against the same `/api/oauth/token` endpoint.

The client authenticates lazily on the first request, caches the token, and
refreshes it transparently — you never manage tokens by hand. Call
`client.Authenticate(ctx)` if you want to verify the credentials up front.

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

The flow has two phases:

1. **Interactive login** — the `pkce` sub-package opens the browser, the user
   approves access, and the app receives an access + refresh token.
2. **Ongoing use** — the refresh token is stored in
   `RefreshTokenCredentials` so the client can transparently refresh the access
   token on subsequent runs.

### Phase 1: Interactive login

```go
import (
	"context"
	"log"

	"github.com/shopwareLabs/go-shopware-http-client"
	"github.com/shopwareLabs/go-shopware-http-client/pkce"
)

func main() {
	tokens, err := pkce.Login(context.Background(), pkce.Config{
		BaseURL: "https://my-shop.example.com",
	})
	if err != nil {
		log.Fatal(err)
	}

	// tokens.RefreshToken should be persisted (e.g., to a file or keychain)
	// so the user doesn't have to log in again.
}
```

`pkce.Login` handles the entire flow:
- Generates a PKCE code verifier and S256 challenge
- Opens the browser to the Shopware authorization page
- Listens on a loopback address for the callback
- Exchanges the authorization code for tokens

### Phase 2: Using the tokens

After the interactive login, create a client with `RefreshTokenCredentials`:

```go
creds := shopware.NewRefreshTokenCredentials(tokens.ClientID, tokens.RefreshToken)
client := shopware.NewClient(shopware.Config{
	BaseURL:     tokens.BaseURL,
	Credentials: creds,
})

// Seed the access token so the first API call doesn't need a refresh.
if err := client.SetAccessToken(ctx, tokens.AccessToken, tokens.Expiry); err != nil {
	log.Fatal(err)
}

// The client now works like any other — it refreshes the token transparently
// when it expires, and the refresh token is automatically rotated.
//
// Pair this with `NewFileTokenStorage` (see Token storage) so the access
// token is also cached on disk across CLI runs — only the refresh token
// itself still needs persisting (file or keychain) between logins.
```

### Refresh token rotation

`RefreshTokenCredentials` supports refresh token rotation: when the server
returns a new `refresh_token` in the token response, the client automatically
updates the stored value. Persist `creds.RefreshToken()` after use to keep the
rotated token for future runs.

### Customizing the PKCE flow

```go
tokens, err := pkce.Login(ctx, pkce.Config{
	BaseURL:      "https://my-shop.example.com",
	ClientID:     "my-custom-cli",    // default: "shopware-cli"
	Scope:        "write",            // default: "write"
	CallbackHost: "127.0.0.1",        // default: "127.0.0.1"
	CallbackPort: 0,                  // 0 = OS-assigned random port
	Timeout:      10 * time.Minute,   // default: 10 minutes
	OpenURL: func(u string) error {   // custom browser opener
		return open.Run(u)
	},
	HTTPClient: &http.Client{         // custom HTTP client for token exchange
		Timeout: 30 * time.Second,
	},
})
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
