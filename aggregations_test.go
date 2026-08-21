package shopware

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aggregationsFromJSON decodes a raw aggregations object as the repository
// would, so accessor behavior can be tested without a server.
func aggregationsFromJSON(t *testing.T, raw string) AggregationResults {
	t.Helper()
	var a AggregationResults
	require.NoError(t, json.Unmarshal([]byte(raw), &a))
	return a
}

func TestAggregationMetricResults(t *testing.T) {
	// Shapes captured live from Shopware 6.7.
	a := aggregationsFromJSON(t, `{
		"a":{"name":"a","avg":81.1904,"apiAlias":"a_aggregation"},
		"sm":{"name":"sm","sum":127063.0,"apiAlias":"sm_aggregation"},
		"c":{"name":"c","count":1565,"apiAlias":"c_aggregation"},
		"mn":{"name":"mn","min":"1.5400","apiAlias":"mn_aggregation"},
		"mx":{"name":"mx","max":88888,"apiAlias":"mx_aggregation"},
		"s":{"name":"s","min":"1.5400","max":"999.3400","avg":495.95,"sum":776174.07,"apiAlias":"s_aggregation"}
	}`)

	avg, err := a.GetAvg("a")
	require.NoError(t, err)
	assert.InDelta(t, 81.1904, avg.Avg, 1e-6)

	sum, err := a.GetSum("sm")
	require.NoError(t, err)
	assert.InDelta(t, 127063.0, sum.Sum, 1e-6)

	count, err := a.GetCount("c")
	require.NoError(t, err)
	assert.Equal(t, 1565, count.Count)

	// min came back as a decimal string -> Numeric handles both forms.
	minResult, err := a.GetMin("mn")
	require.NoError(t, err)
	assert.Equal(t, "1.5400", minResult.Min.String())
	f, err := minResult.Min.Float()
	require.NoError(t, err)
	assert.InDelta(t, 1.54, f, 1e-6)

	// max came back as a bare number here.
	maxResult, err := a.GetMax("mx")
	require.NoError(t, err)
	mf, err := maxResult.Max.Float()
	require.NoError(t, err)
	assert.InDelta(t, 88888, mf, 1e-6)

	stats, err := a.GetStats("s")
	require.NoError(t, err)
	assert.Equal(t, "1.5400", stats.Min.String())
	require.NotNil(t, stats.Avg)
	assert.InDelta(t, 495.95, *stats.Avg, 1e-6)
	require.NotNil(t, stats.Sum)
	assert.InDelta(t, 776174.07, *stats.Sum, 1e-6)
}

func TestAggregationTermsWithNested(t *testing.T) {
	// Terms bucket with a nested avg, merged in under "avg_stock" (live shape).
	a := aggregationsFromJSON(t, `{
		"t":{"name":"t","apiAlias":"t_aggregation","buckets":[
			{"key":"1","count":1565,"apiAlias":"aggregation_bucket",
			 "avg_stock":{"extensions":[],"name":"avg_stock","avg":81.1904}}
		]}
	}`)

	terms, err := a.GetTerms("t")
	require.NoError(t, err)
	require.Len(t, terms.Buckets, 1)

	b := terms.Buckets[0]
	assert.Equal(t, "1", b.Key)
	assert.Equal(t, 1565, b.Count)

	var nested AvgResult
	require.NoError(t, b.NestedAs(&nested))
	assert.Equal(t, "avg_stock", nested.Name)
	assert.InDelta(t, 81.1904, nested.Avg, 1e-6)
}

func TestAggregationDateHistogram(t *testing.T) {
	a := aggregationsFromJSON(t, `{
		"h":{"name":"h","apiAlias":"h_aggregation","buckets":[
			{"key":"2026-06-01 00:00:00","count":1565,"apiAlias":"aggregation_bucket"}
		]}
	}`)

	hist, err := a.GetDateHistogram("h")
	require.NoError(t, err)
	require.Len(t, hist.Buckets, 1)
	assert.Equal(t, "2026-06-01 00:00:00", hist.Buckets[0].Key)
	assert.Equal(t, 1565, hist.Buckets[0].Count)
	assert.Empty(t, hist.Buckets[0].Nested)
}

func TestAggregationResultsHelpers(t *testing.T) {
	a := aggregationsFromJSON(t, `{"a":{"name":"a","avg":1}}`)
	assert.True(t, a.Has("a"))
	assert.False(t, a.Has("missing"))
	assert.Equal(t, []string{"a"}, a.Names())

	_, err := a.GetAvg("missing")
	assert.Error(t, err)
}

func TestRepositoryAggregateReturnsTyped(t *testing.T) {
	srv := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/search/product", r.URL.Path)
		_, _ = w.Write([]byte(`{"total":1,"data":[],"aggregations":{
			"by_active":{"name":"by_active","buckets":[{"key":"1","count":1565,"apiAlias":"aggregation_bucket"}]}
		}}`))
	})
	defer srv.Close()

	repo := NewRepository[product](newClient(srv.URL), "product")
	aggs, err := repo.Aggregate(context.Background(),
		NewCriteria().AddAggregation(TermsAggregation("by_active", "active", nil, nil, nil)))
	require.NoError(t, err)

	terms, err := aggs.GetTerms("by_active")
	require.NoError(t, err)
	require.Len(t, terms.Buckets, 1)
	assert.Equal(t, "1", terms.Buckets[0].Key)
}
