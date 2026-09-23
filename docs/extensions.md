# Extension manager

[← Docs index](./README.md)

The `extension` sub-package wraps the Shopware `/api/_action/extension/*`
endpoints: listing, lifecycle actions, and uploading extension zips. It lives in
its own package so the generic client stays dependency-free — the
version-constraint library it uses (for one version-gated route) is only pulled
in when you import `extension`.

```go
import (
	shopware "github.com/shopwareLabs/go-shopware-http-client"
	"github.com/shopwareLabs/go-shopware-http-client/extension"
)

client := shopware.NewClient(shopware.Config{
	BaseURL:     "https://my-shop.example.com",
	Credentials: shopware.NewIntegrationCredentials(id, secret),
})

mgr := extension.NewManager(client)
```

## Listing

```go
list, err := mgr.ListAvailable(ctx) // extension.List

foo := list.GetByName("SwagPayPal") // *extension.Detail or nil
for _, e := range list.FilterByUpdatable() {
	fmt.Println(e.Name, e.Version, "->", e.LatestVersion, e.Status())
}
```

## Lifecycle

```go
mgr.Refresh(ctx)                          // re-scan the extension directory
mgr.Install(ctx, "plugin", "SwagPayPal")
mgr.Activate(ctx, "plugin", "SwagPayPal")
mgr.Update(ctx, "plugin", "SwagPayPal")
mgr.Deactivate(ctx, "plugin", "SwagPayPal")
mgr.Uninstall(ctx, "plugin", "SwagPayPal")
mgr.Download(ctx, "SwagPayPal")           // download from the store
mgr.Remove(ctx, "plugin", "SwagPayPal")
```

`Remove` uses `POST` on Shopware ≥ 6.6.10.2 and `DELETE` before that. The shop
version is resolved lazily via `client.Version(ctx)` (a cached `/_info/config`
lookup), so you do not pass it in.

## Uploading

Uploads send a `multipart/form-data` body. Pass any `io.Reader` for the zip.

```go
zip, _ := os.Open("SwagPayPal.zip")
defer zip.Close()

// Works on self-managed and Shopware cloud shops alike. The name is
// needed to update an extension a cloud shop already knows.
err := mgr.Upload(ctx, "SwagPayPal", zip)
```

## See also

- [Authentication](./authentication.md) — credentials for the client.
- [Raw requests](./raw-requests.md) — `RequestRaw`, which uploads build on.
