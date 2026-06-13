// Package regression provides direction-aware threshold comparison of two k6 ParsedSummary
// values, producing a Report of Findings with regression and gating status.
// This package is stdlib-only (plus internal/summary); no CLI or output dependencies.
package regression

// Direction indicates whether a metric improves as its value decreases or increases.
type Direction int

const (
	// DirectionLowerIsBetter is used for latency, error rate, and iteration_duration.
	// A regression occurs when the current value is significantly higher than the baseline.
	DirectionLowerIsBetter Direction = iota
	// DirectionHigherIsBetter is used for RPS (http_reqs.rate) and throughput counters.
	// A regression occurs when the current value is significantly lower than the baseline.
	DirectionHigherIsBetter
)

// Options controls threshold values and gating behavior for Compare().
// All threshold fields use sentinel -1.0 meaning "use built-in default".
// NoDefaultThresholds = true disables all built-in defaults (report-only mode).
type Options struct {
	// ThresholdP50 is the maximum allowed regression % for p(50). -1 = use default 10.0.
	ThresholdP50 float64
	// ThresholdP90 is the maximum allowed regression % for p(90). -1 = use default 10.0.
	ThresholdP90 float64
	// ThresholdP95 is the maximum allowed regression % for p(95). -1 = use default 10.0.
	ThresholdP95 float64
	// ThresholdP99 is the maximum allowed regression % for p(99). -1 = use default 10.0.
	ThresholdP99 float64
	// ThresholdP999 is the maximum allowed regression % for p(99.9). -1 = use default 10.0.
	// Note: the stat key is "p(99.9)" (with dot), not "p(999)".
	ThresholdP999 float64
	// ThresholdErrorRate is the maximum allowed regression % for http_req_failed rate.
	// -1 = use default 50.0.
	ThresholdErrorRate float64
	// ThresholdRPS is the maximum allowed regression % for http_reqs rate (RPS).
	// -1 = use default 10.0. Direction is HigherIsBetter.
	ThresholdRPS float64
	// ThresholdIterationDuration is the maximum allowed regression % for iteration_duration.
	// -1 = use default 10.0.
	ThresholdIterationDuration float64
	// ThresholdDefault is the threshold for custom metrics. -1 = 0 (not gated by default).
	ThresholdDefault float64
	// NoDefaultThresholds disables all built-in defaults when true.
	// All thresholds become 0 unless explicitly set (>= 0).
	NoDefaultThresholds bool
}

// Finding is one compared metric stat, produced by Compare().
type Finding struct {
	// Name is the metric name (e.g. "http_req_duration", "my_custom_trend").
	Name string
	// Type is the MetricType string: "trend", "rate", "counter", "gauge".
	Type string
	// Stat is the stat key (e.g. "p(95)", "rate", "count") or "n/a" if missing from one side.
	Stat string
	// Baseline is the baseline value for this stat.
	Baseline float64
	// Current is the current value for this stat.
	Current float64
	// ChangePct is (current-baseline)/baseline*100; 0 when baseline==0.
	ChangePct float64
	// Direction indicates whether lower or higher values are better.
	Direction Direction
	// ThresholdPct is the maximum allowed regression %. 0 = not gated.
	ThresholdPct float64
	// Regressed is true when |ChangePct| > ThresholdPct in the wrong direction.
	Regressed bool
	// Gated is true when Regressed==true AND ThresholdPct > 0.
	Gated bool
}

// Report is the full comparison result from Compare().
type Report struct {
	// Findings is the list of per-stat comparison results.
	Findings []Finding
	// RegressionsCount is the count of Gated==true findings.
	RegressionsCount int
	// Warnings is a list of non-fatal messages (missing percentile, zero baseline, etc.).
	Warnings []string
}
