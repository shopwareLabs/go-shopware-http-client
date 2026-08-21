package shopware

import (
	"context"
	"strings"
	"uuid"
)

// Well-known default IDs used throughout Shopware.
const (
	DefaultSystemLanguageID              = "2fbb5fe2e29a4d70aa5854ce7ce3e20b"
	DefaultLiveVersionID                 = "0fa91ce3e96a4bc2be4bd9ce752c3425"
	DefaultSystemCurrencyID              = "b7d2554b0ce847cd82f3ac9bd1c0dfca"
	DefaultSalesChannelTypeAPI           = "f183ee5650cf4bdb8a774337575067a6"
	DefaultSalesChannelTypeStorefront    = "8a243080f92e4c719546314b577cf82b"
	DefaultSalesChannelTypeProductExport = "ed535e5722134ac1aa6524f73e26881b"
)

// UUID returns a new Shopware-style UUID: a random UUID with the dashes
// stripped.
func UUID() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}

// RequestOption configures the DAL context (language, version, inheritance,
// indexing behaviour) for a single repository operation. Pass any number to the
// repository methods; with none, no context headers are sent.
//
//	repo.Search(ctx, criteria)                       // no context
//	repo.Search(ctx, criteria, WithLanguage(langID)) // with a language
type RequestOption func(*requestOptions)

type requestOptions struct {
	headers map[string]string
}

func resolveHeaders(opts []RequestOption) map[string]string {
	o := requestOptions{headers: map[string]string{}}
	for _, opt := range opts {
		opt(&o)
	}
	return o.headers
}

// WithLanguage sets the sw-language-id header.
func WithLanguage(languageID string) RequestOption {
	return WithHeader("sw-language-id", languageID)
}

// WithVersion sets the sw-version-id header.
func WithVersion(versionID string) RequestOption {
	return WithHeader("sw-version-id", versionID)
}

// WithInheritance toggles the sw-inheritance header.
func WithInheritance(inherit bool) RequestOption {
	return WithHeader("sw-inheritance", boolHeader(inherit))
}

// WithSkipTriggerFlows toggles the sw-skip-trigger-flow header.
func WithSkipTriggerFlows(skip bool) RequestOption {
	return WithHeader("sw-skip-trigger-flow", boolHeader(skip))
}

// WithIndexingSkip sets the indexing-skip header (a comma-separated list of
// indexers to skip).
func WithIndexingSkip(value string) RequestOption {
	return WithHeader("indexing-skip", value)
}

// WithIndexingBehaviour sets the indexing-behavior header.
func WithIndexingBehaviour(value string) RequestOption {
	return WithHeader("indexing-behavior", value)
}

// WithHeader sets an arbitrary request header, an escape hatch for headers not
// covered by a dedicated option.
func WithHeader(key, value string) RequestOption {
	return func(o *requestOptions) {
		o.headers[key] = value
	}
}

func boolHeader(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// SearchResult is the typed result of a DAL search. Aggregations exposes the
// typed Get* helpers (GetTerms, GetStats, ...) for any aggregations requested.
type SearchResult[T any] struct {
	Total        int                `json:"total"`
	Data         []T                `json:"data"`
	Aggregations AggregationResults `json:"aggregations"`
}

// First returns a pointer to the first result, or nil if there are none.
func (r *SearchResult[T]) First() *T {
	if len(r.Data) == 0 {
		return nil
	}
	return &r.Data[0]
}

// EntityRepository provides typed search and write access to a single entity.
// Create one with NewRepository:
//
//	products := NewRepository[Product](client, "product")
//	res, err := products.Search(ctx, NewCriteria().SetLimit(10))
type EntityRepository[T any] struct {
	client     *Client
	entityName string
}

// NewRepository returns a repository for the given entity (e.g. "product",
// "sales_channel"). The entity name uses snake_case as in the DAL.
func NewRepository[T any](client *Client, entityName string) *EntityRepository[T] {
	return &EntityRepository[T]{client: client, entityName: entityName}
}

// route converts the snake_case entity name into the dash-cased API path
// segment (e.g. "sales_channel" -> "sales-channel").
func (r *EntityRepository[T]) route() string {
	return strings.ReplaceAll(r.entityName, "_", "-")
}

// Search executes the criteria and returns the matching entities.
func (r *EntityRepository[T]) Search(ctx context.Context, criteria *Criteria, opts ...RequestOption) (*SearchResult[T], error) {
	resp, err := r.client.Request(ctx, "POST", "/search/"+r.route(), criteria.ToPayload(), resolveHeaders(opts))
	if err != nil {
		return nil, err
	}

	var result SearchResult[T]
	if err := resp.JSON(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SearchIDs executes the criteria and returns only the matching entity IDs.
//
// This is the convenience form for entities with a single-column primary key
// (the common case), where /search-ids returns a flat list of id strings. For
// mapping entities (m:n join tables such as product_category), whose primary
// key is composite, /search-ids returns objects instead — decode those into a
// struct with SearchIDsAs.
func (r *EntityRepository[T]) SearchIDs(ctx context.Context, criteria *Criteria, opts ...RequestOption) ([]string, error) {
	return r.SearchIDsAs[string](ctx, criteria, opts...)
}

// SearchIDsAs executes the criteria and returns the matching primary keys
// decoded as ID. Use it for mapping entities whose composite key /search-ids
// returns as objects:
//
//	type ProductCategory struct {
//		ProductID  string `json:"productId"`
//		CategoryID string `json:"categoryId"`
//	}
//	pairs, err := mapping.SearchIDsAs[ProductCategory](ctx, criteria)
//
// For single-column keys, ID is string and SearchIDs is the shorter spelling.
func (r *EntityRepository[T]) SearchIDsAs[ID any](ctx context.Context, criteria *Criteria, opts ...RequestOption) ([]ID, error) {
	resp, err := r.client.Request(ctx, "POST", "/search-ids/"+r.route(), criteria.ToPayload(), resolveHeaders(opts))
	if err != nil {
		return nil, err
	}

	var result struct {
		Data []ID `json:"data"`
	}
	if err := resp.JSON(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

// Aggregate executes the criteria for its aggregations only, forcing limit=1 so
// no documents are loaded. It returns the aggregations as an AggregationResults
// map; decode a named entry with its Get* helpers (GetTerms, GetStats, ...).
//
// For a fully custom result shape, use the generic AggregateAs instead.
func (r *EntityRepository[T]) Aggregate(ctx context.Context, criteria *Criteria, opts ...RequestOption) (AggregationResults, error) {
	criteria.SetLimit(1)
	resp, err := r.client.Request(ctx, "POST", "/search/"+r.route(), criteria.ToPayload(), resolveHeaders(opts))
	if err != nil {
		return nil, err
	}

	var result struct {
		Aggregations AggregationResults `json:"aggregations"`
	}
	if err := resp.JSON(&result); err != nil {
		return nil, err
	}
	return result.Aggregations, nil
}

// AggregateAs executes the criteria for its aggregations only and decodes the
// aggregation payload into A. The shape of A depends on the aggregations added
// to the criteria, not on the entity, so the type parameter lives on the method
// instead of the repository:
//
//	type Aggs struct {
//		ByActive struct {
//			Buckets []struct {
//				Key   string `json:"key"`
//				Count int    `json:"count"`
//			} `json:"buckets"`
//		} `json:"by_active"`
//	}
//	aggs, err := products.AggregateAs[Aggs](ctx, criteria)
func (r *EntityRepository[T]) AggregateAs[A any](ctx context.Context, criteria *Criteria, opts ...RequestOption) (*A, error) {
	criteria.SetLimit(1)
	resp, err := r.client.Request(ctx, "POST", "/search/"+r.route(), criteria.ToPayload(), resolveHeaders(opts))
	if err != nil {
		return nil, err
	}

	var result struct {
		Aggregations A `json:"aggregations"`
	}
	if err := resp.JSON(&result); err != nil {
		return nil, err
	}
	return &result.Aggregations, nil
}

// Upsert creates or updates the given entities in a single sync operation.
func (r *EntityRepository[T]) Upsert(ctx context.Context, payload []T, opts ...RequestOption) error {
	return NewSyncService(r.client).Sync(ctx, []SyncOperation{
		NewSyncOperation("upsert", r.entityName, "upsert", toAnySlice(payload), nil),
	}, opts...)
}

// Delete removes the given entities. Each element typically carries at least an
// "id" field.
func (r *EntityRepository[T]) Delete(ctx context.Context, payload []map[string]any, opts ...RequestOption) error {
	return NewSyncService(r.client).Sync(ctx, []SyncOperation{
		NewSyncOperation("delete", r.entityName, "delete", toAnySlice(payload), nil),
	}, opts...)
}

// DeleteByFilters removes all entities matching the given filters.
func (r *EntityRepository[T]) DeleteByFilters(ctx context.Context, filters []Filter, opts ...RequestOption) error {
	return NewSyncService(r.client).Sync(ctx, []SyncOperation{
		NewSyncOperation("delete", r.entityName, "delete", nil, filters),
	}, opts...)
}

func toAnySlice[T any](in []T) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

// SyncService performs DAL sync operations against the /_action/sync endpoint.
type SyncService struct {
	client *Client
}

// NewSyncService returns a SyncService bound to the client.
func NewSyncService(client *Client) *SyncService {
	return &SyncService{client: client}
}

// SyncOperation is a single operation within a sync request.
type SyncOperation struct {
	Key      string   `json:"key"`
	Entity   string   `json:"entity"`
	Action   string   `json:"action"`
	Payload  []any    `json:"payload"`
	Criteria []Filter `json:"criteria,omitempty"`
}

// NewSyncOperation builds a SyncOperation. action is "upsert" or "delete".
func NewSyncOperation(key, entity, action string, payload []any, criteria []Filter) SyncOperation {
	return SyncOperation{
		Key:      key,
		Entity:   entity,
		Action:   action,
		Payload:  payload,
		Criteria: criteria,
	}
}

// Sync sends the operations to the /_action/sync endpoint.
func (s *SyncService) Sync(ctx context.Context, operations []SyncOperation, opts ...RequestOption) error {
	_, err := s.client.Request(ctx, "POST", "/_action/sync", operations, resolveHeaders(opts))
	return err
}
