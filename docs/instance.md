# Instance

[← Docs index](./README.md)

The `instance` sub-package wraps endpoints that act on the shop as a whole
rather than on a single entity or extension.

```go
import (
	shopware "github.com/shopwareLabs/go-shopware-http-client"
	"github.com/shopwareLabs/go-shopware-http-client/instance"
)

shop := instance.NewManager(client)
```

## Clearing the cache

```go
err := shop.ClearCache(ctx) // DELETE /_action/cache
```

## See also

- [Raw requests](./raw-requests.md) — for instance endpoints without a helper yet.
