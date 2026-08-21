package shopware

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"strconv"
)

// This file models the JSON produced by Shopware's DAL aggregation result
// classes (src/Core/Framework/DataAbstractionLayer/Search/AggregationResult) as
// Go structs, so callers can decode aggregations into typed values instead of
// poking at raw JSON.
//
// The top-level "aggregations" object is keyed by each aggregation's name, and
// the value's shape depends on the aggregation type requested at the call site
// (terms -> buckets, stats -> min/max/avg/sum, ...). AggregationResults holds
// that raw map; the Get* helpers decode a named entry into the matching result
// type, mirroring PHP's AggregationResultCollection::get($name).

// AggregationResults is the decoded "aggregations" object: a map from each
// aggregation's name to its raw result. Decode a named entry with the Get*
// helpers, or with Decode for a custom type.
type AggregationResults map[string]jsontext.Value

// Has reports whether an aggregation with the given name is present.
func (a AggregationResults) Has(name string) bool {
	_, ok := a[name]
	return ok
}

// Names returns the names of all aggregations present.
func (a AggregationResults) Names() []string {
	names := make([]string, 0, len(a))
	for name := range a {
		names = append(names, name)
	}
	return names
}

// Decode unmarshals the named aggregation into target. It returns an error if
// no aggregation with that name exists.
func (a AggregationResults) Decode(name string, target any) error {
	raw, ok := a[name]
	if !ok {
		return fmt.Errorf("aggregation %q not found", name)
	}
	return json.Unmarshal(raw, target)
}

// decodeAgg unmarshals the named aggregation into R via Decode.
func (a AggregationResults) decodeAgg[R any](name string) (*R, error) {
	var result R
	if err := a.Decode(name, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Numeric holds a metric value that Shopware may serialize as either a number
// or a string (decimal columns come back as strings, e.g. "1.5400"), matching
// the PHP `mixed`/`float|int|string|null` types on min/max.
type Numeric struct {
	raw jsontext.Value
}

// UnmarshalJSON stores the raw value for later, lossless interpretation.
func (n *Numeric) UnmarshalJSON(data []byte) error {
	n.raw = append(n.raw[:0], data...)
	return nil
}

// MarshalJSON renders the original value.
func (n Numeric) MarshalJSON() ([]byte, error) {
	if len(n.raw) == 0 {
		return []byte("null"), nil
	}
	return n.raw, nil
}

// IsNull reports whether the value is absent or JSON null.
func (n Numeric) IsNull() bool {
	return len(n.raw) == 0 || string(n.raw) == "null"
}

// String returns the value as a string, unquoting it if it was a JSON string.
func (n Numeric) String() string {
	if n.IsNull() {
		return ""
	}
	var s string
	if err := json.Unmarshal(n.raw, &s); err == nil {
		return s
	}
	return string(n.raw)
}

// Float returns the value as a float64, parsing it whether it arrived as a JSON
// number or a numeric string.
func (n Numeric) Float() (float64, error) {
	if n.IsNull() {
		return 0, nil
	}
	var f float64
	if err := json.Unmarshal(n.raw, &f); err == nil {
		return f, nil
	}
	return strconv.ParseFloat(n.String(), 64)
}

// --- Metric results ------------------------------------------------------

// AvgResult is the result of an avg aggregation.
type AvgResult struct {
	Name string  `json:"name"`
	Avg  float64 `json:"avg"`
}

// SumResult is the result of a sum aggregation.
type SumResult struct {
	Name string  `json:"name"`
	Sum  float64 `json:"sum"`
}

// CountResult is the result of a count aggregation.
type CountResult struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// MinResult is the result of a min aggregation. The value may be numeric or a
// numeric string.
type MinResult struct {
	Name string  `json:"name"`
	Min  Numeric `json:"min"`
}

// MaxResult is the result of a max aggregation. The value may be numeric or a
// numeric string.
type MaxResult struct {
	Name string  `json:"name"`
	Max  Numeric `json:"max"`
}

// StatsResult is the result of a stats aggregation. Min and Max may be numeric
// or numeric strings; Avg and Sum are nullable numbers.
type StatsResult struct {
	Name string   `json:"name"`
	Min  Numeric  `json:"min"`
	Max  Numeric  `json:"max"`
	Avg  *float64 `json:"avg"`
	Sum  *float64 `json:"sum"`
}

// EntityResult is the result of an entity aggregation. Entities holds the raw
// entity payloads; decode them into your entity type.
type EntityResult struct {
	Name     string           `json:"name"`
	Entities []jsontext.Value `json:"entities"`
}

// --- Bucket results ------------------------------------------------------

// Bucket is one bucket of a terms or histogram aggregation. Key identifies the
// bucket, Count is the number of documents in it, and Nested holds the nested
// sub-aggregation result (if the criteria requested one) keyed by its name —
// decode it with NestedAs.
type Bucket struct {
	Key   string `json:"key"`
	Count int    `json:"count"`

	// Nested is the sub-aggregation result merged into the bucket under its
	// own name. It is nil when the bucket has no nested aggregation.
	Nested jsontext.Value `json:"-"`
}

// reservedBucketKeys are the fixed fields of a bucket; any other key is the
// merged nested aggregation result.
var reservedBucketKeys = map[string]bool{
	"key":      true,
	"count":    true,
	"apiAlias": true,
}

// UnmarshalJSON decodes the fixed bucket fields and captures any extra key as
// the nested aggregation result.
func (b *Bucket) UnmarshalJSON(data []byte) error {
	var raw map[string]jsontext.Value
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if v, ok := raw["key"]; ok {
		if err := json.Unmarshal(v, &b.Key); err != nil {
			return err
		}
	}
	if v, ok := raw["count"]; ok {
		if err := json.Unmarshal(v, &b.Count); err != nil {
			return err
		}
	}
	for k, v := range raw {
		if !reservedBucketKeys[k] {
			b.Nested = v
			break
		}
	}
	return nil
}

// NestedAs decodes a bucket's nested sub-aggregation result into target.
func (b *Bucket) NestedAs(target any) error {
	if len(b.Nested) == 0 {
		return fmt.Errorf("bucket %q has no nested aggregation", b.Key)
	}
	return json.Unmarshal(b.Nested, target)
}

// TermsResult is the result of a terms aggregation.
type TermsResult struct {
	Name    string   `json:"name"`
	Buckets []Bucket `json:"buckets"`
}

// DateHistogramResult is the result of a histogram (date histogram)
// aggregation. Bucket keys are formatted timestamps (e.g. "2026-06-01 00:00:00").
type DateHistogramResult struct {
	Name    string   `json:"name"`
	Buckets []Bucket `json:"buckets"`
}

// --- Typed accessors -----------------------------------------------------
//
// Each mirrors AggregationResultCollection::get($name), returning the result in
// the shape matching the aggregation type you requested.

// GetAvg decodes the named aggregation as an avg result.
func (a AggregationResults) GetAvg(name string) (*AvgResult, error) {
	return a.decodeAgg[AvgResult](name)
}

// GetSum decodes the named aggregation as a sum result.
func (a AggregationResults) GetSum(name string) (*SumResult, error) {
	return a.decodeAgg[SumResult](name)
}

// GetCount decodes the named aggregation as a count result.
func (a AggregationResults) GetCount(name string) (*CountResult, error) {
	return a.decodeAgg[CountResult](name)
}

// GetMin decodes the named aggregation as a min result.
func (a AggregationResults) GetMin(name string) (*MinResult, error) {
	return a.decodeAgg[MinResult](name)
}

// GetMax decodes the named aggregation as a max result.
func (a AggregationResults) GetMax(name string) (*MaxResult, error) {
	return a.decodeAgg[MaxResult](name)
}

// GetStats decodes the named aggregation as a stats result.
func (a AggregationResults) GetStats(name string) (*StatsResult, error) {
	return a.decodeAgg[StatsResult](name)
}

// GetTerms decodes the named aggregation as a terms result.
func (a AggregationResults) GetTerms(name string) (*TermsResult, error) {
	return a.decodeAgg[TermsResult](name)
}

// GetDateHistogram decodes the named aggregation as a date histogram result.
func (a AggregationResults) GetDateHistogram(name string) (*DateHistogramResult, error) {
	return a.decodeAgg[DateHistogramResult](name)
}

// GetEntity decodes the named aggregation as an entity result.
func (a AggregationResults) GetEntity(name string) (*EntityResult, error) {
	return a.decodeAgg[EntityResult](name)
}
