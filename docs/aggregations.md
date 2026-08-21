# Aggregations

[← Docs index](./README.md)

`Aggregate` returns an `AggregationResults` map (name → result). Each
aggregation type has a matching result struct and a typed accessor that mirrors
PHP's `AggregationResultCollection::get($name)`.

```go
limit := 5
c := shopware.NewCriteria().
	AddAggregation(shopware.TermsAggregation("per_manufacturer", "manufacturerId", &limit, nil, nil)).
	AddAggregation(shopware.StatsAggregation("price_stats", "price"))

aggs, err := products.Aggregate(ctx, c)

stats, _ := aggs.GetStats("price_stats")
// stats.Avg, stats.Sum  -> *float64
// stats.Min, stats.Max  -> shopware.Numeric (see below)

terms, _ := aggs.GetTerms("per_manufacturer")
for _, b := range terms.Buckets {
	fmt.Println(b.Key, b.Count)
}
```

Accessors: `GetTerms`, `GetDateHistogram`, `GetStats`, `GetAvg`, `GetSum`,
`GetMin`, `GetMax`, `GetCount`, `GetEntity`. The same results are available on a
`Search` result via `result.Aggregations`.

## Numeric values

Shopware serializes `min`/`max` (and stats `min`/`max`) as either a number or a
string (decimal DB columns come back as `"1.5400"`), matching the PHP `mixed`
type. `Numeric` preserves both:

```go
min, _ := aggs.GetMin("lowest_price")
min.Min.String()        // "1.5400"
f, _ := min.Min.Float()  // 1.54
```

## Nested aggregations

A sub-aggregation is merged into each bucket under its own name. `Bucket.NestedAs`
decodes it:

```go
terms, _ := aggs.GetTerms("per_active")
for _, b := range terms.Buckets {
	var avgStock shopware.AvgResult
	if err := b.NestedAs(&avgStock); err == nil {
		fmt.Println(b.Key, avgStock.Avg)
	}
}
```

## Custom shapes

For a shape these structs don't cover, decode the whole payload yourself with
the generic `AggregateAs[A]`:

```go
type Aggs struct {
	PerManufacturer shopware.TermsResult `json:"per_manufacturer"`
}
typed, err := products.AggregateAs[Aggs](ctx, c)
```

## See also

- [Criteria builder](./criteria.md) — the aggregation constructors.
- [Entities & repositories](./entities.md) — `Aggregate` and `Search`.
