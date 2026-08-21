package shopware

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type product struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestRepositorySearch(t *testing.T) {
	var path, body string
	srv := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		assert.Equal(t, "en-id", r.Header.Get("sw-language-id"))
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"total":1,"data":[{"id":"p1","name":"Shirt"}],"aggregations":{}}`))
	})
	defer srv.Close()

	repo := NewRepository[product](newClient(srv.URL), "product")
	res, err := repo.Search(context.Background(),
		NewCriteria().SetLimit(1).AddFilter(Equals("active", true)),
		WithLanguage("en-id"))
	require.NoError(t, err)

	assert.Equal(t, "/api/search/product", path)
	assert.Contains(t, body, `"active"`)
	assert.Equal(t, 1, res.Total)
	require.Len(t, res.Data, 1)
	assert.Equal(t, "Shirt", res.First().Name)
}

func TestRepositorySearchRouteDashCases(t *testing.T) {
	var path string
	srv := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"total":0,"data":[]}`))
	})
	defer srv.Close()

	repo := NewRepository[product](newClient(srv.URL), "sales_channel")
	_, err := repo.Search(context.Background(), NewCriteria())
	require.NoError(t, err)
	assert.Equal(t, "/api/search/sales-channel", path)
}

func TestRepositorySearchIDs(t *testing.T) {
	srv := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/search-ids/product", r.URL.Path)
		_, _ = w.Write([]byte(`{"data":["p1","p2"]}`))
	})
	defer srv.Close()

	repo := NewRepository[product](newClient(srv.URL), "product")
	ids, err := repo.SearchIDs(context.Background(), NewCriteria())
	require.NoError(t, err)
	assert.Equal(t, []string{"p1", "p2"}, ids)
}

func TestSearchIDsAsMappingEntity(t *testing.T) {
	srv := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/search-ids/product-category", r.URL.Path)
		_, _ = w.Write([]byte(`{"total":2,"data":[
			{"productId":"p1","categoryId":"c1"},
			{"productId":"p2","categoryId":"c1"}
		]}`))
	})
	defer srv.Close()

	type productCategory struct {
		ProductID  string `json:"productId"`
		CategoryID string `json:"categoryId"`
	}

	repo := NewRepository[productCategory](newClient(srv.URL), "product_category")
	pairs, err := repo.SearchIDsAs[productCategory](context.Background(), NewCriteria())
	require.NoError(t, err)

	require.Len(t, pairs, 2)
	assert.Equal(t, "p1", pairs[0].ProductID)
	assert.Equal(t, "c1", pairs[0].CategoryID)
}

func TestAggregateAsDecodesTyped(t *testing.T) {
	srv := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/search/product", r.URL.Path)
		_, _ = w.Write([]byte(`{"total":1,"data":[],"aggregations":{
			"by_active":{"buckets":[{"key":"1","count":1565}]}
		}}`))
	})
	defer srv.Close()

	type aggs struct {
		ByActive struct {
			Buckets []struct {
				Key   string `json:"key"`
				Count int    `json:"count"`
			} `json:"buckets"`
		} `json:"by_active"`
	}

	repo := NewRepository[product](newClient(srv.URL), "product")
	got, err := repo.AggregateAs[aggs](context.Background(),
		NewCriteria().AddAggregation(TermsAggregation("by_active", "active", nil, nil, nil)))
	require.NoError(t, err)

	require.Len(t, got.ByActive.Buckets, 1)
	assert.Equal(t, "1", got.ByActive.Buckets[0].Key)
	assert.Equal(t, 1565, got.ByActive.Buckets[0].Count)
}

func TestRepositoryUpsertSendsSyncOperation(t *testing.T) {
	var captured []SyncOperation
	srv := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/_action/sync", r.URL.Path)
		require.NoError(t, json.UnmarshalRead(r.Body, &captured))
		_, _ = w.Write([]byte(`{}`))
	})
	defer srv.Close()

	repo := NewRepository[product](newClient(srv.URL), "product")
	err := repo.Upsert(context.Background(), []product{{ID: "p1", Name: "Shirt"}})
	require.NoError(t, err)

	require.Len(t, captured, 1)
	assert.Equal(t, "upsert", captured[0].Action)
	assert.Equal(t, "product", captured[0].Entity)
	require.Len(t, captured[0].Payload, 1)
}

func TestRepositoryDeleteByFilters(t *testing.T) {
	var captured []SyncOperation
	srv := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.UnmarshalRead(r.Body, &captured))
		_, _ = w.Write([]byte(`{}`))
	})
	defer srv.Close()

	repo := NewRepository[product](newClient(srv.URL), "product")
	err := repo.DeleteByFilters(context.Background(), []Filter{Equals("active", false)})
	require.NoError(t, err)

	require.Len(t, captured, 1)
	assert.Equal(t, "delete", captured[0].Action)
	assert.Empty(t, captured[0].Payload)
	require.Len(t, captured[0].Criteria, 1)
}

func TestRequestOptionsResolveHeaders(t *testing.T) {
	headers := resolveHeaders([]RequestOption{
		WithLanguage("lang"),
		WithVersion("ver"),
		WithInheritance(true),
		WithSkipTriggerFlows(false),
		WithHeader("x-custom", "1"),
	})
	assert.Equal(t, "lang", headers["sw-language-id"])
	assert.Equal(t, "ver", headers["sw-version-id"])
	assert.Equal(t, "1", headers["sw-inheritance"])
	assert.Equal(t, "0", headers["sw-skip-trigger-flow"])
	assert.Equal(t, "1", headers["x-custom"])

	assert.Empty(t, resolveHeaders(nil), "no options send no headers")
}

func TestUUIDIsStripped(t *testing.T) {
	id := UUID()
	assert.Len(t, id, 32)
	assert.NotContains(t, id, "-")
}

func newClient(baseURL string) *Client {
	return NewClient(Config{BaseURL: baseURL, ClientID: "id", ClientSecret: "secret"})
}
