package shopware

import "encoding/json/v2"

// TotalCountMode controls how the total count of a search result is computed.
type TotalCountMode int

const (
	// NoTotalCount selects no total count. Use when no pagination is required
	// (fastest).
	NoTotalCount TotalCountMode = 0
	// ExactTotalCount selects the exact total count. Use when exact pagination
	// is required (slow).
	ExactTotalCount TotalCountMode = 1
	// PaginationTotalCount fetches limit*5+1. Use when pagination only needs a
	// "next page exists" hint (fast).
	PaginationTotalCount TotalCountMode = 2
)

// Filter is a single DAL filter (equals, range, multi, ...). Construct filters
// with the package-level helpers (Equals, EqualsAny, Contains, Range, ...).
type Filter map[string]any

// Aggregation is a single DAL aggregation. Construct with the helpers
// (TermsAggregation, SumAggregation, ...).
type Aggregation map[string]any

// Sorting describes a single sort instruction.
type Sorting struct {
	Field          string `json:"field"`
	Order          string `json:"order"`
	NaturalSorting bool   `json:"naturalSorting"`
	Type           string `json:"type,omitempty"`
}

// Query is a scored query used for relevance ranking.
type Query struct {
	Score      float64 `json:"score"`
	Query      Filter  `json:"query"`
	ScoreField string  `json:"scoreField,omitempty"`
}

type association struct {
	name     string
	criteria *Criteria
}

// Criteria is a fluent builder for Shopware DAL search and aggregation
// payloads. The zero value is not ready for use; create one with NewCriteria.
//
// All mutating methods return the receiver so calls can be chained:
//
//	c := NewCriteria().
//		SetLimit(10).
//		AddFilter(Equals("active", true)).
//		AddSorting(Sort("createdAt", "DESC")).
//		AddAssociation("media")
type Criteria struct {
	title          string
	page           *int
	limit          *int
	term           string
	ids            []string
	filters        []Filter
	postFilter     []Filter
	queries        []Query
	sortings       []Sorting
	aggregations   []Aggregation
	grouping       []string
	fields         []string
	includes       map[string][]string
	associations   []association
	totalCountMode *TotalCountMode
}

// NewCriteria creates an empty Criteria, optionally pre-filled with ids.
func NewCriteria(ids ...string) *Criteria {
	return &Criteria{ids: ids}
}

// ToPayload renders the criteria into the request body expected by the DAL
// search endpoints.
func (c *Criteria) ToPayload() map[string]any {
	params := map[string]any{}

	if len(c.ids) > 0 {
		params["ids"] = c.ids
	}
	if c.page != nil {
		params["page"] = *c.page
	}
	if c.limit != nil {
		params["limit"] = *c.limit
	}
	if c.term != "" {
		params["term"] = c.term
	}
	if len(c.queries) > 0 {
		params["query"] = c.queries
	}
	if len(c.filters) > 0 {
		params["filter"] = c.filters
	}
	if len(c.postFilter) > 0 {
		params["post-filter"] = c.postFilter
	}
	if len(c.sortings) > 0 {
		params["sort"] = c.sortings
	}
	if len(c.aggregations) > 0 {
		params["aggregations"] = c.aggregations
	}
	if len(c.grouping) > 0 {
		params["grouping"] = c.grouping
	}
	if len(c.fields) > 0 {
		params["fields"] = c.fields
	}
	if len(c.associations) > 0 {
		assoc := map[string]any{}
		for _, a := range c.associations {
			assoc[a.name] = a.criteria.ToPayload()
		}
		params["associations"] = assoc
	}
	if len(c.includes) > 0 {
		params["includes"] = c.includes
	}
	if c.totalCountMode != nil {
		params["total-count-mode"] = int(*c.totalCountMode)
	}

	return params
}

// MarshalJSON implements json.Marshaler so a Criteria can be passed directly as
// a request body.
func (c *Criteria) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.ToPayload())
}

// SetTitle sets a title shown in the search request URL, useful for debugging.
func (c *Criteria) SetTitle(title string) *Criteria {
	c.title = title
	return c
}

// SetIDs replaces the id filter.
func (c *Criteria) SetIDs(ids []string) *Criteria {
	c.ids = ids
	return c
}

// SetTotalCountMode configures how the total count is computed.
func (c *Criteria) SetTotalCountMode(mode TotalCountMode) *Criteria {
	if mode < NoTotalCount || mode > PaginationTotalCount {
		c.totalCountMode = nil
		return c
	}
	c.totalCountMode = &mode
	return c
}

// SetPage sets the 1-based result page.
func (c *Criteria) SetPage(page int) *Criteria {
	c.page = &page
	return c
}

// SetLimit sets the maximum number of results.
func (c *Criteria) SetLimit(limit int) *Criteria {
	c.limit = &limit
	return c
}

// SetTerm sets a full-text search term.
func (c *Criteria) SetTerm(term string) *Criteria {
	c.term = term
	return c
}

// AddFilter appends a filter applied to both documents and aggregations.
func (c *Criteria) AddFilter(filter Filter) *Criteria {
	c.filters = append(c.filters, filter)
	return c
}

// AddPostFilter appends a filter applied to documents but not aggregations.
func (c *Criteria) AddPostFilter(filter Filter) *Criteria {
	c.postFilter = append(c.postFilter, filter)
	return c
}

// AddSorting appends a sort instruction.
func (c *Criteria) AddSorting(sorting Sorting) *Criteria {
	c.sortings = append(c.sortings, sorting)
	return c
}

// AddQuery appends a scored query for relevance ranking.
func (c *Criteria) AddQuery(filter Filter, score float64, scoreField ...string) *Criteria {
	q := Query{Score: score, Query: filter}
	if len(scoreField) > 0 {
		q.ScoreField = scoreField[0]
	}
	c.queries = append(c.queries, q)
	return c
}

// AddGrouping groups the result by the given field.
func (c *Criteria) AddGrouping(field string) *Criteria {
	c.grouping = append(c.grouping, field)
	return c
}

// AddFields restricts the result to partial fields.
func (c *Criteria) AddFields(fields ...string) *Criteria {
	c.fields = append(c.fields, fields...)
	return c
}

// AddAggregation appends an aggregation.
func (c *Criteria) AddAggregation(aggregation Aggregation) *Criteria {
	c.aggregations = append(c.aggregations, aggregation)
	return c
}

// AddIncludes merges the given include map (entity name -> fields) into the
// criteria's includes.
func (c *Criteria) AddIncludes(includes map[string][]string) *Criteria {
	if c.includes == nil {
		c.includes = map[string][]string{}
	}
	for entity, fields := range includes {
		c.includes[entity] = append(c.includes[entity], fields...)
	}
	return c
}

// AddAssociation ensures a sub-criteria exists for each segment of the
// dot-separated path and returns the receiver for chaining.
func (c *Criteria) AddAssociation(path string) *Criteria {
	c.GetAssociation(path)
	return c
}

// GetAssociation ensures a sub-criteria exists for each segment of the
// dot-separated path and returns the Criteria of the last segment, so it can be
// further refined (filters, sortings, nested associations).
func (c *Criteria) GetAssociation(path string) *Criteria {
	current := c
	for _, part := range splitPath(path) {
		current = current.associationCriteria(part)
	}
	return current
}

// HasAssociation reports whether a direct association with the given name
// exists.
func (c *Criteria) HasAssociation(name string) bool {
	for _, a := range c.associations {
		if a.name == name {
			return true
		}
	}
	return false
}

func (c *Criteria) associationCriteria(part string) *Criteria {
	for _, a := range c.associations {
		if a.name == part {
			return a.criteria
		}
	}
	sub := NewCriteria()
	c.associations = append(c.associations, association{name: part, criteria: sub})
	return sub
}

// ResetSorting removes all sort instructions.
func (c *Criteria) ResetSorting() *Criteria {
	c.sortings = nil
	return c
}

// GetLimit returns the configured limit, or 0 if unset.
func (c *Criteria) GetLimit() int {
	if c.limit == nil {
		return 0
	}
	return *c.limit
}

// GetPage returns the configured page, or 0 if unset.
func (c *Criteria) GetPage() int {
	if c.page == nil {
		return 0
	}
	return *c.page
}

func splitPath(path string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			parts = append(parts, path[start:i])
			start = i + 1
		}
	}
	parts = append(parts, path[start:])
	return parts
}

// --- Filter constructors -------------------------------------------------

// Equals filters documents where field == value.
func Equals(field string, value any) Filter {
	return Filter{"type": "equals", "field": field, "value": value}
}

// EqualsAny filters documents where field is one of value.
func EqualsAny(field string, value []any) Filter {
	return Filter{"type": "equalsAny", "field": field, "value": value}
}

// Contains filters documents where field LIKE %value%.
func Contains(field, value string) Filter {
	return Filter{"type": "contains", "field": field, "value": value}
}

// Prefix filters documents where field LIKE value%.
func Prefix(field, value string) Filter {
	return Filter{"type": "prefix", "field": field, "value": value}
}

// Suffix filters documents where field LIKE %value.
func Suffix(field, value string) Filter {
	return Filter{"type": "suffix", "field": field, "value": value}
}

// Range filters documents where field falls within the given bounds. Valid
// parameter keys are "gt", "gte", "lt", "lte".
func Range(field string, parameters map[string]any) Filter {
	return Filter{"type": "range", "field": field, "parameters": parameters}
}

// Not negates the given queries combined with the operator ("AND"/"OR").
func Not(operator string, queries []Filter) Filter {
	return Filter{"type": "not", "operator": operator, "queries": queries}
}

// Multi combines the given queries with the operator ("AND"/"OR").
func Multi(operator string, queries []Filter) Filter {
	return Filter{"type": "multi", "operator": operator, "queries": queries}
}

// --- Sorting constructors ------------------------------------------------

// Sort sorts by field. order defaults to "ASC" when empty.
func Sort(field, order string) Sorting {
	if order == "" {
		order = "ASC"
	}
	return Sorting{Field: field, Order: order}
}

// NaturalSort sorts by field using natural ordering.
func NaturalSort(field, order string) Sorting {
	if order == "" {
		order = "ASC"
	}
	return Sorting{Field: field, Order: order, NaturalSorting: true}
}

// CountSort sorts by counting associations via field (ORDER BY COUNT(field)).
func CountSort(field, order string) Sorting {
	if order == "" {
		order = "ASC"
	}
	return Sorting{Field: field, Order: order, Type: "count"}
}

// --- Aggregation constructors --------------------------------------------

// AvgAggregation computes the average of field.
func AvgAggregation(name, field string) Aggregation {
	return Aggregation{"type": "avg", "name": name, "field": field}
}

// CountAggregation counts non-null values of field.
func CountAggregation(name, field string) Aggregation {
	return Aggregation{"type": "count", "name": name, "field": field}
}

// MaxAggregation computes the maximum of field.
func MaxAggregation(name, field string) Aggregation {
	return Aggregation{"type": "max", "name": name, "field": field}
}

// MinAggregation computes the minimum of field.
func MinAggregation(name, field string) Aggregation {
	return Aggregation{"type": "min", "name": name, "field": field}
}

// StatsAggregation computes sum/max/min/avg/count of field at once.
func StatsAggregation(name, field string) Aggregation {
	return Aggregation{"type": "stats", "name": name, "field": field}
}

// SumAggregation computes the sum of field.
func SumAggregation(name, field string) Aggregation {
	return Aggregation{"type": "sum", "name": name, "field": field}
}

// TermsAggregation buckets documents by the distinct values of field. A nil
// nested aggregation or sorting is omitted.
func TermsAggregation(name, field string, limit *int, sort *Sorting, nested Aggregation) Aggregation {
	agg := Aggregation{"type": "terms", "name": name, "field": field}
	if limit != nil {
		agg["limit"] = *limit
	}
	if sort != nil {
		agg["sort"] = *sort
	}
	if nested != nil {
		agg["aggregation"] = nested
	}
	return agg
}

// FilterAggregation applies filters before computing the nested aggregation.
func FilterAggregation(name string, filter []Filter, nested Aggregation) Aggregation {
	return Aggregation{"type": "filter", "name": name, "filter": filter, "aggregation": nested}
}

// EntityAggregation buckets documents by an associated entity.
func EntityAggregation(name, field, definition string) Aggregation {
	return Aggregation{"type": "entity", "name": name, "field": field, "definition": definition}
}

// HistogramAggregation buckets documents into date intervals. Empty/nil
// optional arguments are omitted.
func HistogramAggregation(name, field, interval, format string, nested Aggregation, timeZone string) Aggregation {
	agg := Aggregation{"type": "histogram", "name": name, "field": field}
	if interval != "" {
		agg["interval"] = interval
	}
	if format != "" {
		agg["format"] = format
	}
	if nested != nil {
		agg["aggregation"] = nested
	}
	if timeZone != "" {
		agg["timeZone"] = timeZone
	}
	return agg
}
