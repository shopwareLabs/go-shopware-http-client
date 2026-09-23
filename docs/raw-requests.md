# Raw requests

[← Docs index](./README.md)

You do not have to use the repository layer. The client exposes the HTTP verbs
directly and returns a `*Response`. Paths are relative to `/api`.

```go
resp, err := client.Get(ctx, "/_info/config")
if err != nil {
	return err
}

var info struct {
	Version string `json:"version"`
}
if err := resp.JSON(&info); err != nil {
	return err
}
fmt.Println("Shopware version:", info.Version)
```

All verbs are available:

```go
client.Get(ctx, "/_info/config")
client.Post(ctx, "/search/product", body)
client.Put(ctx, "/some/resource", body)
client.Patch(ctx, "/some/resource", body)
client.Delete(ctx, "/some/resource", nil)

// Full control, including per-request headers:
client.Request(ctx, http.MethodPost, "/search/product", body, map[string]string{
	"sw-language-id": "2fbb5fe2e29a4d70aa5854ce7ce3e20b",
})
```

`Response` carries the status code, raw body, and headers:

```go
resp.StatusCode      // int
resp.Body            // []byte
resp.Headers         // http.Header
resp.JSON(&target)   // unmarshal helper
```

## Shop info

The shop info endpoint has a typed helper on the client:

```go
info, err := client.Info(ctx)   // GET /_info/config, cached per Client
info.Version                    // "6.7.1.0" (also via client.Version(ctx))
info.HasBundle("Storefront")
```

Operations on the shop as a whole live in the
[`instance`](./instance.md) sub-package.

## See also

- [Entities & repositories](./entities.md) — the typed layer over `/search`,
  `/search-ids` and `/_action/sync`.
- [Error handling](./error-handling.md) — how non-2xx responses surface.
