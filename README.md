# go-shopware-http-client

[![CI](https://github.com/shopwareLabs/go-shopware-http-client/actions/workflows/ci.yml/badge.svg)](https://github.com/shopwareLabs/go-shopware-http-client/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/shopwareLabs/go-shopware-http-client.svg)](https://pkg.go.dev/github.com/shopwareLabs/go-shopware-http-client)

A standalone Go client for the [Shopware](https://www.shopware.com/) Admin API,
with a typed Data Abstraction Layer (DAL) on top.

It is self-contained — outside the standard library (which provides the
`uuid` package) it depends only on `golang.org/x/sync` and
`github.com/shyim/go-version`. Everything
application-specific (HTTP transport, instrumentation, SSRF protection, extra
headers, token persistence) is **injected** rather than imported, so the package
can be vendored or split into its own module without dragging the rest of an
application along.

```go
import "github.com/shopwareLabs/go-shopware-http-client"
```

The import path ends in `go-shopware-http-client`, but the package is named
`shopware`, so it is used as `shopware.NewClient(...)`.

## Quick start

```go
ctx := context.Background()

client := shopware.NewClient(shopware.Config{
	BaseURL:     "https://my-shop.example.com",
	Credentials: shopware.NewIntegrationCredentials("CLIENT_ID", "CLIENT_SECRET"),
})

type Product struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

products := shopware.NewRepository[Product](client, "product")

result, err := products.Search(ctx,
	shopware.NewCriteria().
		SetLimit(10).
		AddFilter(shopware.Equals("active", true)),
)
if err != nil {
	panic(err)
}

fmt.Printf("found %d products\n", result.Total)
for _, p := range result.Data {
	fmt.Println(p.ID, p.Name)
}
```

The client authenticates lazily on the first request, caches the OAuth token,
and refreshes it transparently — you never manage tokens by hand.

## Documentation

Full guides live in [`docs/`](./docs/README.md):

- [Authentication](./docs/authentication.md) — integration, admin user (username/password), PKCE (browser login), custom grants.
- [Raw requests](./docs/raw-requests.md) — the HTTP verbs and `Response`.
- [Entities & repositories](./docs/entities.md) — typed search, ids, mapping entities, sync, DAL context.
- [Criteria builder](./docs/criteria.md) — filters, sorting, associations, includes.
- [Aggregations](./docs/aggregations.md) — typed aggregation results.
- [Extension manager](./docs/extensions.md) — `/_action/extension/*` helpers (sub-package).
- [Media](./docs/media.md) — uploads (file / URL) and media folders (sub-package).
- [Token storage](./docs/token-storage.md) — in-memory, file, and no-op backends, scoping, custom stores.
- [Custom HTTP client & headers](./docs/http-client.md) — transports, global headers.
- [Error handling](./docs/error-handling.md) — `APIError`, the 401 retry.
- [Concurrency](./docs/concurrency.md) — safe concurrent use.

Runnable, compile-checked snippets are in [`example_test.go`](./example_test.go).
A full PKCE browser-login CLI demo lives in [`examples/pkce-cli`](./examples/pkce-cli/).
