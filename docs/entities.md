# Entities & repositories

[← Docs index](./README.md)

`EntityRepository[T]` is a typed wrapper over a single DAL entity. Create one
per entity; the entity name uses snake_case as in the DAL (`sales_channel`,
`product_manufacturer`, …) and is mapped to the dashed API route automatically.

```go
type SalesChannel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

channels := shopware.NewRepository[SalesChannel](client, "sales_channel")
```

## Searching

```go
result, err := channels.Search(ctx, shopware.NewCriteria())
// result.Total           -> int
// result.Data            -> []SalesChannel
// result.First()         -> *SalesChannel (nil if empty)
// result.Aggregations    -> AggregationResults (see Aggregations doc)

// Just the IDs (single-column primary key):
ids, err := channels.SearchIDs(ctx, shopware.NewCriteria())
```

Every repository operation takes optional [request options](#request-options)
for the DAL context (language, version, ...) as trailing arguments.

Build the query with the [Criteria builder](./criteria.md).

### Mapping entities

m:n join tables (e.g. `product_category`) have a composite primary key, so
`/search-ids` returns objects rather than strings. Decode them with the generic
`SearchIDsAs`:

```go
type ProductCategory struct {
	ProductID  string `json:"productId"`
	CategoryID string `json:"categoryId"`
}

mapping := shopware.NewRepository[ProductCategory](client, "product_category")
pairs, err := mapping.SearchIDsAs[ProductCategory](ctx, shopware.NewCriteria())
// pairs -> []ProductCategory
```

`SearchIDs` is shorthand for `SearchIDsAs[string]`.

## Writing data (upsert / delete)

Writes go through the DAL `_action/sync` endpoint.

```go
// Create or update.
err := products.Upsert(ctx, []Product{
	{ID: shopware.UUID(), Name: "New Product"},
})

// Delete by id.
err = products.Delete(ctx, []map[string]any{
	{"id": "0fa91ce3e96a4bc2be4bd9ce752c3425"},
})

// Delete everything matching a filter.
err = products.DeleteByFilters(ctx, []shopware.Filter{
	shopware.Equals("active", false),
})
```

`shopware.UUID()` returns a Shopware-style ID (a UUIDv4 with the dashes
stripped). Well-known IDs are exported as constants
(`DefaultSystemLanguageID`, `DefaultLiveVersionID`, `DefaultSystemCurrencyID`,
the sales-channel type IDs, …).

For multi-entity transactional writes, drop down to the `SyncService`:

```go
sync := shopware.NewSyncService(client)
err := sync.Sync(ctx, []shopware.SyncOperation{
	shopware.NewSyncOperation("create-product", "product", "upsert", []any{product}, nil),
	shopware.NewSyncOperation("clean-tags", "tag", "delete", nil, []shopware.Filter{
		shopware.Equals("name", "obsolete"),
	}),
})
```

## Request options

Every repository operation (`Search`, `SearchIDs`, `Aggregate`, `Upsert`,
`Delete`, `DeleteByFilters`, `Sync`) accepts trailing `RequestOption`s that set
the DAL context headers. With no options, no context headers are sent.

```go
result, err := products.Search(ctx, shopware.NewCriteria(),
	shopware.WithLanguage(shopware.DefaultSystemLanguageID),
	shopware.WithVersion(shopware.DefaultLiveVersionID),
	shopware.WithInheritance(true),
)
```

Available options:

| Option | Header |
| --- | --- |
| `WithLanguage(id)` | `sw-language-id` |
| `WithVersion(id)` | `sw-version-id` |
| `WithInheritance(bool)` | `sw-inheritance` |
| `WithSkipTriggerFlows(bool)` | `sw-skip-trigger-flow` |
| `WithIndexingSkip(value)` | `indexing-skip` |
| `WithIndexingBehaviour(value)` | `indexing-behavior` |
| `WithHeader(key, value)` | any header (escape hatch) |

Options are plain values, so a reused context is just a saved option:

```go
deDE := shopware.WithLanguage(deDELanguageID)
products.Search(ctx, c1, deDE)
products.Search(ctx, c2, deDE)
```

## See also

- [Criteria builder](./criteria.md) — filters, sorting, associations, includes.
- [Aggregations](./aggregations.md) — typed aggregation results.
