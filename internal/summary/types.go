// Package summary provides a format-agnostic parser for k6 summary JSON files.
// It normalises both the legacy handleSummary() format and the new machine-readable
// v1.0.0 format into a single ParsedSummary type.
// This package is stdlib-only and has no dependency on go.k6.io/k6/v2.
package summary

// MetricType identifies the statistical aggregation strategy of a k6 metric.
// The string values match the "type" field in both k6 summary JSON formats.
type MetricType string

const (
	// MetricTypeCounter accumulates a running total (e.g. http_reqs).
	MetricTypeCounter MetricType = "counter"
	// MetricTypeGauge tracks the current value of a quantity (e.g. vus).
	MetricTypeGauge MetricType = "gauge"
	// MetricTypeRate tracks the proportion of non-zero (true) samples (e.g. checks, http_req_failed).
	MetricTypeRate MetricType = "rate"
	// MetricTypeTrend computes distribution statistics over a stream of values (e.g. http_req_duration).
	MetricTypeTrend MetricType = "trend"
)

// MetricEntry is the normalised representation of one metric from either k6 summary format.
//
// Values semantics by type:
//
//	Counter: always has "count" and "rate" (rate synthesised for new format as count/duration).
//	Gauge:   always has "value", "min", "max".
//	Rate:    always has "passes", "fails", "rate".
//	         Legacy "passes"/"fails" are copied directly.
//	         New format "matches" → "passes"; "total-matches" → "fails".
//	Trend:   contains only the keys present in the source file (freeform subset of stats).
//	         Custom summaryTrendStats percentiles (p(99), p(99.9), etc.) are preserved.
type MetricEntry struct {
	Type     MetricType
	Contains string             // "default" | "time" | "data"
	Values   map[string]float64 // freeform; never a fixed struct — preserves custom percentiles
}

// ParsedSummary is the single normalised output of the parser, agnostic to input format.
// Callers never need to know which format was on disk.
type ParsedSummary struct {
	// DurationSec is the test run duration in seconds.
	// Source: state.testRunDurationMs (legacy, converted from ms) or config.duration (new format).
	// Zero when the source format does not include duration information (e.g. --summary-export flat format).
	DurationSec float64

	// Metrics maps full metric name to its normalised entry.
	// Sub-metric names including tag suffixes (e.g. "http_req_duration{expected_response:true}")
	// are preserved as map keys.
	Metrics map[string]MetricEntry
}
