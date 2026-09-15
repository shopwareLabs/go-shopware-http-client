package shopware

import (
	"encoding/json/v2"
	"testing"

	"github.com/shopwareLabs/go-shopware-http-client/internal/assert"
)

func TestCriteriaToPayloadEmpty(t *testing.T) {
	assert.Empty(t, NewCriteria().ToPayload())
}

func TestCriteriaToPayloadFull(t *testing.T) {
	limit := 5
	c := NewCriteria().
		SetLimit(10).
		SetPage(2).
		SetTerm("shirt").
		SetTotalCountMode(ExactTotalCount).
		AddFilter(Equals("active", true)).
		AddFilter(EqualsAny("id", []any{"a", "b"})).
		AddPostFilter(Range("stock", map[string]any{"gte": 1})).
		AddSorting(Sort("createdAt", "DESC")).
		AddGrouping("manufacturerId").
		AddFields("id", "name").
		AddAggregation(TermsAggregation("per_manufacturer", "manufacturerId", &limit, nil, nil)).
		AddIncludes(map[string][]string{"product": {"id", "name"}})

	payload := c.ToPayload()

	assert.Equal(t, 10, payload["limit"])
	assert.Equal(t, 2, payload["page"])
	assert.Equal(t, "shirt", payload["term"])
	assert.Equal(t, 1, payload["total-count-mode"])
	assert.Len(t, payload["filter"], 2)
	assert.Len(t, payload["post-filter"], 1)
	assert.Equal(t, []string{"id", "name"}, payload["fields"])
	assert.Equal(t, map[string][]string{"product": {"id", "name"}}, payload["includes"])
}

func TestCriteriaNestedAssociations(t *testing.T) {
	c := NewCriteria()
	// addAssociation should create the full path; getAssociation returns the leaf.
	c.GetAssociation("manufacturer.media").AddFilter(Equals("private", false))
	c.AddAssociation("categories")

	assert.True(t, c.HasAssociation("manufacturer"))
	assert.True(t, c.HasAssociation("categories"))
	// Re-fetching the same path must not duplicate the association.
	assert.Same(t, c.GetAssociation("manufacturer"), c.GetAssociation("manufacturer"))

	payload := c.ToPayload()
	assocs := payload["associations"].(map[string]any)
	assert.Contains(t, assocs, "manufacturer")
	assert.Contains(t, assocs, "categories")

	manufacturer := assocs["manufacturer"].(map[string]any)
	media := manufacturer["associations"].(map[string]any)["media"].(map[string]any)
	assert.Len(t, media["filter"], 1)
}

func TestCriteriaMarshalsAsPayload(t *testing.T) {
	c := NewCriteria().SetLimit(1).AddFilter(Equals("id", "x"))

	out, err := json.Marshal(c)
	assert.NoError(t, err)

	var decoded map[string]any
	assert.NoError(t, json.Unmarshal(out, &decoded))
	assert.Equal(t, float64(1), decoded["limit"])
	filters := decoded["filter"].([]any)
	assert.Len(t, filters, 1)
	assert.Equal(t, "equals", filters[0].(map[string]any)["type"])
}

func TestFilterConstructors(t *testing.T) {
	assert.Equal(t, Filter{"type": "contains", "field": "name", "value": "abc"}, Contains("name", "abc"))
	assert.Equal(t, Filter{"type": "prefix", "field": "name", "value": "ab"}, Prefix("name", "ab"))
	assert.Equal(t, Filter{"type": "suffix", "field": "name", "value": "bc"}, Suffix("name", "bc"))

	not := Not("AND", []Filter{Equals("a", 1)})
	assert.Equal(t, "not", not["type"])
	assert.Equal(t, "AND", not["operator"])
}

func TestSortConstructors(t *testing.T) {
	assert.Equal(t, Sorting{Field: "name", Order: "ASC"}, Sort("name", ""))
	assert.Equal(t, Sorting{Field: "name", Order: "DESC", NaturalSorting: true}, NaturalSort("name", "DESC"))
	assert.Equal(t, Sorting{Field: "lines", Order: "ASC", Type: "count"}, CountSort("lines", ""))
}

func TestSetTotalCountModeRejectsInvalid(t *testing.T) {
	c := NewCriteria().SetTotalCountMode(TotalCountMode(99))
	_, ok := c.ToPayload()["total-count-mode"]
	assert.False(t, ok, "invalid mode is dropped")
}
