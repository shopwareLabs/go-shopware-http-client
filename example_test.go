package shopware_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/shopwareLabs/go-shopware-http-client"
)

type exampleProduct struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// These examples are compile-checked documentation: they mirror the snippets in
// README.md so the README cannot drift from the actual API. They are not run
// (no "// Output:" line) because they would require a live shop.

func ExampleNewClient_integration() {
	client := shopware.NewClient(shopware.Config{
		BaseURL:     "https://my-shop.example.com",
		Credentials: shopware.NewIntegrationCredentials("CLIENT_ID", "CLIENT_SECRET"),
	})
	_ = client.Authenticate(context.Background())
}

func ExampleNewClient_password() {
	_ = shopware.NewClient(shopware.Config{
		BaseURL:     "https://my-shop.example.com",
		Credentials: shopware.NewPasswordCredentials("admin", "shopware"),
	})
}

func ExampleNewClient_customHTTPClientAndStorage() {
	_ = shopware.NewClient(shopware.Config{
		BaseURL:      "https://my-shop.example.com",
		ClientID:     "CLIENT_ID",
		ClientSecret: "CLIENT_SECRET",
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
		TokenStorage: shopware.NewInMemoryTokenStorage(),
		Headers:      map[string]string{"x-internal-proxy-token": "secret"},
	})
}

func ExampleEntityRepository_Search() {
	ctx := context.Background()
	client := shopware.NewClient(shopware.Config{
		BaseURL:     "https://my-shop.example.com",
		Credentials: shopware.NewIntegrationCredentials("id", "secret"),
	})

	products := shopware.NewRepository[exampleProduct](client, "product")

	result, err := products.Search(ctx,
		shopware.NewCriteria().
			SetLimit(10).
			AddFilter(shopware.Equals("active", true)).
			AddSorting(shopware.Sort("createdAt", "DESC")),
	)
	if err != nil {
		return
	}
	fmt.Println(result.Total, result.First())
}

func ExampleEntityRepository_Upsert() {
	ctx := context.Background()
	client := shopware.NewClient(shopware.Config{BaseURL: "https://x", ClientID: "i", ClientSecret: "s"})
	products := shopware.NewRepository[exampleProduct](client, "product")

	_ = products.Upsert(ctx, []exampleProduct{
		{ID: shopware.UUID(), Name: "New Product"},
	})

	_ = products.DeleteByFilters(ctx, []shopware.Filter{
		shopware.Equals("active", false),
	})
}

func ExampleEntityRepository_SearchIDsAs() {
	ctx := context.Background()
	client := shopware.NewClient(shopware.Config{BaseURL: "https://x", ClientID: "i", ClientSecret: "s"})

	type productCategory struct {
		ProductID  string `json:"productId"`
		CategoryID string `json:"categoryId"`
	}
	mapping := shopware.NewRepository[productCategory](client, "product_category")

	pairs, err := mapping.SearchIDsAs[productCategory](ctx, shopware.NewCriteria())
	if err != nil {
		return
	}
	fmt.Println(len(pairs))
}

func ExampleEntityRepository_Aggregate() {
	ctx := context.Background()
	client := shopware.NewClient(shopware.Config{BaseURL: "https://x", ClientID: "i", ClientSecret: "s"})
	products := shopware.NewRepository[exampleProduct](client, "product")

	aggs, err := products.Aggregate(ctx,
		shopware.NewCriteria().
			AddAggregation(shopware.TermsAggregation("by_active", "active", nil, nil, nil)).
			AddAggregation(shopware.StatsAggregation("price_stats", "price")))
	if err != nil {
		return
	}

	terms, _ := aggs.GetTerms("by_active")
	for _, b := range terms.Buckets {
		fmt.Println(b.Key, b.Count)
	}

	stats, _ := aggs.GetStats("price_stats")
	fmt.Println(stats.Min.String(), stats.Avg)
}

func ExampleEntityRepository_AggregateAs() {
	ctx := context.Background()
	client := shopware.NewClient(shopware.Config{BaseURL: "https://x", ClientID: "i", ClientSecret: "s"})
	products := shopware.NewRepository[exampleProduct](client, "product")

	type aggs struct {
		ByActive shopware.TermsResult `json:"by_active"`
	}

	got, err := products.AggregateAs[aggs](ctx,
		shopware.NewCriteria().AddAggregation(shopware.TermsAggregation("by_active", "active", nil, nil, nil)))
	if err != nil {
		return
	}
	fmt.Println(len(got.ByActive.Buckets))
}

func ExampleCriteria_nestedAssociations() {
	c := shopware.NewCriteria()
	c.AddAssociation("categories")
	c.GetAssociation("manufacturer.media").
		AddFilter(shopware.Equals("private", false))
	_ = c
}

func ExampleAPIError() {
	ctx := context.Background()
	client := shopware.NewClient(shopware.Config{BaseURL: "https://x", ClientID: "i", ClientSecret: "s"})

	_, err := client.Get(ctx, "/search/does-not-exist")
	var apiErr *shopware.APIError
	if errors.As(err, &apiErr) {
		fmt.Println(apiErr.StatusCode, apiErr.Detail)
	}
}
