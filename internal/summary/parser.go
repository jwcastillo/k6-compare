// Package summary provides format-agnostic parsing for k6 summary JSON files.
// Parse() accepts both the legacy handleSummary()/--summary-export format and the
// new machine-readable v1.0.0 format, normalising both into ParsedSummary.
package summary

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Parse detects the k6 summary format and returns a normalised ParsedSummary.
// It accepts both the legacy handleSummary()/--summary-export format and the
// new machine-readable v1.0.0 format, discriminated by the root "version" key.
//
// Error semantics:
//   - empty input          → "summary: invalid JSON: empty input"
//   - JSON null            → "summary: document is null"
//   - truncated JSON       → wraps json.Unmarshal error
//   - missing new-format required field "config" → "summary: new format v1.0.0: missing required field 'config'"
//   - valid JSON with no recognised fields → ParsedSummary{} with nil error
func Parse(data []byte) (ParsedSummary, error) {
	if len(data) == 0 {
		return ParsedSummary{}, fmt.Errorf("summary: invalid JSON: empty input")
	}

	// Fast-path check for JSON null (must be done before the probe unmarshal,
	// because json.Unmarshal("null", &struct{}{}) succeeds with no error).
	if string(bytes.TrimSpace(data)) == "null" {
		return ParsedSummary{}, fmt.Errorf("summary: document is null")
	}

	// Probe for the "version" key to determine format. We unmarshal into a
	// minimal struct first — this also validates that the input is well-formed JSON.
	var probe struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return ParsedSummary{}, fmt.Errorf("summary: invalid JSON: %w", err)
	}

	if probe.Version != "" {
		return parseNewFormat(data)
	}
	return parseLegacy(data)
}

// ─── Legacy format ────────────────────────────────────────────────────────────

// legacySummary is the unmarshal target for the legacy format.
// Both the handleSummary() and --summary-export variants share this outer shape.
type legacySummary struct {
	State struct {
		TestRunDurationMs float64 `json:"testRunDurationMs"`
	} `json:"state"`
	Metrics map[string]json.RawMessage `json:"metrics"`
}

// legacyMetricHandleSummary is a handleSummary() metric entry with type/contains/values wrappers.
type legacyMetricHandleSummary struct {
	Type     string             `json:"type"`
	Contains string             `json:"contains"`
	Values   map[string]float64 `json:"values"`
}

// parseLegacy handles both the handleSummary variant (metrics have "type"/"contains"/"values"
// wrappers) and the --summary-export variant (metrics have flat inline values).
//
// Detection: if the first metric entry has a "type" JSON key → handleSummary variant.
// Otherwise → export variant.
func parseLegacy(data []byte) (ParsedSummary, error) {
	var raw legacySummary
	if err := json.Unmarshal(data, &raw); err != nil {
		return ParsedSummary{}, fmt.Errorf("summary: invalid JSON: %w", err)
	}

	result := ParsedSummary{
		Metrics: make(map[string]MetricEntry, len(raw.Metrics)),
	}
	result.DurationSec = raw.State.TestRunDurationMs / 1000.0

	for name, metricRaw := range raw.Metrics {
		entry, err := parseLegacyMetric(metricRaw)
		if err != nil {
			// Skip metrics that cannot be parsed; be lenient.
			continue
		}
		result.Metrics[name] = entry
	}

	return result, nil
}

// isHandleSummaryVariant peeks at the raw metric JSON to determine if it uses
// the handleSummary style (has a "type" field at the top level).
func isHandleSummaryVariant(raw json.RawMessage) bool {
	return bytes.Contains(raw, []byte(`"type"`))
}

// parseLegacyMetric dispatches to the appropriate variant parser.
func parseLegacyMetric(raw json.RawMessage) (MetricEntry, error) {
	if isHandleSummaryVariant(raw) {
		return parseLegacyHandleSummaryMetric(raw)
	}
	return parseLegacyExportMetric(raw)
}

// parseLegacyHandleSummaryMetric parses a handleSummary metric entry that has
// "type", "contains", and "values" wrapper fields.
func parseLegacyHandleSummaryMetric(raw json.RawMessage) (MetricEntry, error) {
	var m legacyMetricHandleSummary
	if err := json.Unmarshal(raw, &m); err != nil {
		return MetricEntry{}, err
	}
	return MetricEntry{
		Type:     MetricType(m.Type),
		Contains: m.Contains,
		Values:   m.Values,
	}, nil
}

// parseLegacyExportMetric parses a --summary-export metric entry where values
// are inlined at the top level of the metric JSON object.
//
// Type detection heuristic (from spec):
//   - has "value" AND "passes"        → rate
//   - has "count"                     → counter
//   - has "avg" OR "med"              → trend
//   - has "value" AND "min" AND NOT "passes" → gauge
//
// Non-numeric fields (like "thresholds") are silently skipped.
func parseLegacyExportMetric(raw json.RawMessage) (MetricEntry, error) {
	var flatRaw map[string]json.RawMessage
	if err := json.Unmarshal(raw, &flatRaw); err != nil {
		return MetricEntry{}, err
	}

	values := make(map[string]float64)
	for k, v := range flatRaw {
		if k == "thresholds" {
			continue
		}
		var f float64
		if err := json.Unmarshal(v, &f); err == nil {
			values[k] = f
		}
	}

	// Determine metric type from key patterns.
	_, hasValue := values["value"]
	_, hasPasses := values["passes"]
	_, hasCount := values["count"]
	_, hasAvg := values["avg"]
	_, hasMed := values["med"]
	_, hasMin := values["min"]

	var mtype MetricType
	var contains string
	switch {
	case hasValue && hasPasses:
		mtype = MetricTypeRate
		// Normalise: export format has "value" (= rate), "passes", "fails".
		// Rename "value" → "rate" for uniform interface.
		if v, ok := values["value"]; ok {
			values["rate"] = v
			delete(values, "value")
		}
	case hasCount:
		mtype = MetricTypeCounter
	case hasAvg || hasMed:
		mtype = MetricTypeTrend
		contains = "time"
	case hasValue && hasMin && !hasPasses:
		mtype = MetricTypeGauge
	}

	return MetricEntry{
		Type:     mtype,
		Contains: contains,
		Values:   values,
	}, nil
}

// ─── New machine-readable format (v1.0.0) ────────────────────────────────────

// newSummary is the unmarshal target for the new machine-readable format.
type newSummary struct {
	Version string `json:"version"`
	Config  *struct {
		Duration float64 `json:"duration"`
	} `json:"config"`
	Results struct {
		Metrics []newMetric `json:"metrics"`
	} `json:"results"`
}

// newMetric is one element from results.metrics in the new format.
// Values is typed as any so that json.Unmarshal produces map[string]interface{},
// which lets extractMapFloat64 preserve all keys including custom percentiles.
type newMetric struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Contains string `json:"contains"`
	Values   any    `json:"values"`
}

// newRateValues maps the new format rate metric's wire fields.
type newRateValues struct {
	Matches int64   `json:"matches"`
	Total   int64   `json:"total"`
	Rate    float64 `json:"rate"`
}

// newCounterValues maps the new format counter metric's wire fields.
type newCounterValues struct {
	Count float64 `json:"count"`
}

// parseNewFormat handles the v1.0.0 machine-readable summary format.
func parseNewFormat(data []byte) (ParsedSummary, error) {
	var raw newSummary
	if err := json.Unmarshal(data, &raw); err != nil {
		return ParsedSummary{}, fmt.Errorf("summary: invalid JSON: %w", err)
	}

	if raw.Config == nil {
		return ParsedSummary{}, fmt.Errorf("summary: new format v1.0.0: missing required field 'config'")
	}

	durationSec := raw.Config.Duration
	result := ParsedSummary{
		DurationSec: durationSec,
		Metrics:     make(map[string]MetricEntry, len(raw.Results.Metrics)),
	}

	for _, m := range raw.Results.Metrics {
		entry, err := parseNewMetric(m, durationSec)
		if err != nil {
			continue
		}
		result.Metrics[m.Name] = entry
	}

	return result, nil
}

// parseNewMetric normalises a single new-format metric into a MetricEntry.
func parseNewMetric(m newMetric, durationSec float64) (MetricEntry, error) {
	mtype := MetricType(m.Type)
	entry := MetricEntry{
		Type:     mtype,
		Contains: m.Contains,
	}

	switch mtype {
	case MetricTypeRate:
		// Re-unmarshal Values into the rate-specific struct to get typed fields.
		rawBytes, err := json.Marshal(m.Values)
		if err != nil {
			return MetricEntry{}, err
		}
		var rv newRateValues
		if err := json.Unmarshal(rawBytes, &rv); err != nil {
			return MetricEntry{}, err
		}
		entry.Values = map[string]float64{
			"passes": float64(rv.Matches),
			"fails":  float64(rv.Total - rv.Matches),
			"rate":   rv.Rate,
		}

	case MetricTypeCounter:
		rawBytes, err := json.Marshal(m.Values)
		if err != nil {
			return MetricEntry{}, err
		}
		var cv newCounterValues
		if err := json.Unmarshal(rawBytes, &cv); err != nil {
			return MetricEntry{}, err
		}
		synthesisedRate := 0.0
		if durationSec > 0 {
			synthesisedRate = cv.Count / durationSec
		}
		entry.Values = map[string]float64{
			"count": cv.Count,
			"rate":  synthesisedRate,
		}

	default:
		// Trend, Gauge, and unknown types: extract all numeric values from the
		// map[string]interface{} that json.Unmarshal produces when target is any.
		entry.Values = extractMapFloat64(m.Values)
	}

	return entry, nil
}

// extractMapFloat64 coerces a map[string]interface{} (from json.Unmarshal into any)
// to map[string]float64, handling both float64 and json.Number types.
// Non-numeric values are silently skipped.
func extractMapFloat64(raw any) map[string]float64 {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	result := make(map[string]float64, len(m))
	for k, v := range m {
		switch n := v.(type) {
		case float64:
			result[k] = n
		case json.Number:
			f, err := n.Float64()
			if err == nil {
				result[k] = f
			}
		}
	}
	return result
}
