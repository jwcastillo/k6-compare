package regression

import (
	"fmt"
	"math"

	"github.com/jwcastillo/k6-compare/internal/summary"
)

// builtinMetricDef describes how to compare a single built-in k6 metric.
// The built-in metric table is the single source of truth for all default thresholds,
// directions, and stat keys — eliminating scattered if/else chains in Compare().
type builtinMetricDef struct {
	// name is the k6 metric name.
	name string
	// statKeys lists the stat keys to compare (in order). For trend metrics these are
	// percentile keys. For rate/counter metrics these are the single stat key.
	statKeys []string
	// direction indicates whether lower or higher values are better.
	direction Direction
	// defaultThreshold is the default regression threshold (percentage). 0 = report-only.
	defaultThreshold float64
	// flagThresholdForKey returns the user-specified threshold for a given stat key from
	// Options (-1 = use default). Enables per-percentile override (e.g. ThresholdP95).
	flagThresholdForKey func(key string, opts Options) float64
}

// builtinMetrics is the ordered list of built-in k6 metrics processed by Compare().
// Gauge metrics (vus, etc.) are intentionally excluded — they are instantaneous samples.
//
//nolint:gochecknoglobals // read-only data table of built-in metric definitions
var builtinMetrics = []builtinMetricDef{
	{
		name:             "http_req_duration",
		statKeys:         []string{"p(50)", "p(90)", "p(95)", "p(99)", "p(99.9)"},
		direction:        DirectionLowerIsBetter,
		defaultThreshold: 10.0,
		flagThresholdForKey: func(key string, opts Options) float64 {
			switch key {
			case "p(50)":
				return opts.ThresholdP50
			case "p(90)":
				return opts.ThresholdP90
			case "p(95)":
				return opts.ThresholdP95
			case "p(99)":
				return opts.ThresholdP99
			case "p(99.9)":
				return opts.ThresholdP999
			default:
				return -1
			}
		},
	},
	{
		name:             "http_req_failed",
		statKeys:         []string{"rate"},
		direction:        DirectionLowerIsBetter,
		defaultThreshold: 50.0,
		flagThresholdForKey: func(_ string, opts Options) float64 {
			return opts.ThresholdErrorRate
		},
	},
	{
		name:             "http_reqs",
		statKeys:         []string{"rate"},
		direction:        DirectionHigherIsBetter,
		defaultThreshold: 10.0,
		flagThresholdForKey: func(_ string, opts Options) float64 {
			return opts.ThresholdRPS
		},
	},
	{
		name:             "iteration_duration",
		statKeys:         []string{"p(95)", "avg"}, // prefer p(95); fall back to avg
		direction:        DirectionLowerIsBetter,
		defaultThreshold: 10.0,
		flagThresholdForKey: func(_ string, opts Options) float64 {
			return opts.ThresholdIterationDuration
		},
	},
	{
		name:             "iterations",
		statKeys:         []string{"count"},
		direction:        DirectionLowerIsBetter,
		defaultThreshold: 0, // report-only, no default threshold
		flagThresholdForKey: func(_ string, _ Options) float64 {
			return -1 // no flag for iterations
		},
	},
}

// builtinMetricNames is the set of built-in metric names for quick lookup.
//
//nolint:gochecknoglobals // read-only set derived from builtinMetrics
var builtinMetricNames = func() map[string]struct{} {
	m := make(map[string]struct{}, len(builtinMetrics))
	for _, bm := range builtinMetrics {
		m[bm.name] = struct{}{}
	}
	return m
}()

// effectiveThreshold returns the threshold to apply for a metric stat.
//
//	if flagValue >= 0: use flagValue (explicit override always wins per D-04)
//	if noDefaults:     return 0 (report-only per D-02)
//	otherwise:         return metricDefault (built-in default per D-01)
func effectiveThreshold(flagValue, metricDefault float64, noDefaults bool) float64 {
	if flagValue >= 0 {
		return flagValue
	}
	if noDefaults {
		return 0
	}
	return metricDefault
}

// computeChangePct returns (current-baseline)/baseline*100.
// Returns 0 when baseline == 0 (caller must add a warning).
func computeChangePct(baseline, current float64) float64 {
	if baseline == 0 {
		return 0
	}
	return (current - baseline) / baseline * 100.0
}

// isRegressed returns true when the change percentage represents a regression
// in the given direction, exceeding the threshold.
func isRegressed(changePct float64, dir Direction, threshold float64) bool {
	if threshold <= 0 {
		return false
	}
	switch dir {
	case DirectionLowerIsBetter:
		return changePct > threshold
	case DirectionHigherIsBetter:
		return changePct < -threshold
	}
	return false
}

// Compare compares baseline and current ParsedSummary values using the provided Options,
// returning a Report with Findings for each compared metric stat.
//
// Metrics compared:
//   - Built-in: http_req_duration (trend percentiles), http_req_failed (rate), http_reqs (rate/RPS),
//     iteration_duration (trend, p(95) preferred), iterations (counter count, report-only)
//   - Custom: any Trend/Rate/Counter present in both summaries and not in the built-in list
//
// Gauge metrics are never compared (they are instantaneous samples, not aggregates).
//
//nolint:funlen // linear, table-driven comparison pipeline; splitting hurts readability
func Compare(baseline, current summary.ParsedSummary, opts Options) Report {
	var findings []Finding
	var warnings []string

	// --- Process built-in metrics ---
	for _, bm := range builtinMetrics {
		baseEntry, baseOk := baseline.Metrics[bm.name]
		currEntry, currOk := current.Metrics[bm.name]

		if !baseOk && !currOk {
			continue
		}
		if !baseOk {
			warnings = append(warnings, fmt.Sprintf("metric %q present in current but missing in baseline; skipping", bm.name))
			continue
		}
		if !currOk {
			warnings = append(warnings, fmt.Sprintf("metric %q present in baseline but missing in current; skipping", bm.name))
			continue
		}

		if bm.name == "iteration_duration" {
			// Special case: prefer p(95), fall back to avg.
			findings, warnings = compareIterationDuration(bm, baseEntry, currEntry, opts, findings, warnings)
			continue
		}

		for _, key := range bm.statKeys {
			baseVal, baseHas := baseEntry.Values[key]
			currVal, currHas := currEntry.Values[key]

			flagVal := bm.flagThresholdForKey(key, opts)
			thresh := effectiveThreshold(flagVal, bm.defaultThreshold, opts.NoDefaultThresholds)

			if !baseHas && !currHas {
				continue
			}
			if !baseHas || !currHas {
				// One side missing — emit n/a finding with warning.
				missingIn := "current"
				if !baseHas {
					missingIn = "baseline"
				}
				warnings = append(warnings, fmt.Sprintf(
					"stat %q for metric %q present in %s only; skipping threshold check",
					key, bm.name, missingIn,
				))
				findings = append(findings, Finding{
					Name:         bm.name,
					Type:         string(baseEntry.Type),
					Stat:         "n/a",
					Baseline:     baseVal,
					Current:      currVal,
					ChangePct:    0,
					Direction:    bm.direction,
					ThresholdPct: thresh,
					Regressed:    false,
					Gated:        false,
				})
				continue
			}

			changePct := computeChangePct(baseVal, currVal)
			if baseVal == 0 {
				warnings = append(warnings, fmt.Sprintf(
					"baseline value for %q stat %q is 0; skipping threshold check",
					bm.name, key,
				))
			}

			regressed := isRegressed(changePct, bm.direction, thresh)
			gated := regressed && thresh > 0

			findings = append(findings, Finding{
				Name:         bm.name,
				Type:         string(baseEntry.Type),
				Stat:         key,
				Baseline:     baseVal,
				Current:      currVal,
				ChangePct:    math.Round(changePct*1000) / 1000,
				Direction:    bm.direction,
				ThresholdPct: thresh,
				Regressed:    regressed,
				Gated:        gated,
			})
		}
	}

	// --- Process custom metrics ---
	for metricName, baseEntry := range baseline.Metrics {
		// Skip built-ins.
		if _, isBuiltin := builtinMetricNames[metricName]; isBuiltin {
			continue
		}
		// Skip gauges — not meaningful for regression.
		if baseEntry.Type == summary.MetricTypeGauge {
			continue
		}

		currEntry, currOk := current.Metrics[metricName]
		if !currOk {
			continue // metric only in baseline — skip silently
		}
		if currEntry.Type == summary.MetricTypeGauge {
			continue
		}

		flagVal := opts.ThresholdDefault
		thresh := effectiveThreshold(flagVal, 0, opts.NoDefaultThresholds)

		switch baseEntry.Type {
		case summary.MetricTypeTrend:
			// Compare intersection of stat keys present in both.
			for key, baseVal := range baseEntry.Values {
				currVal, currHas := currEntry.Values[key]
				if !currHas {
					continue // Only in baseline — skip for custom metrics.
				}
				changePct := computeChangePct(baseVal, currVal)
				if baseVal == 0 {
					warnings = append(warnings, fmt.Sprintf(
						"baseline value for custom metric %q stat %q is 0; skipping threshold check",
						metricName, key,
					))
				}
				regressed := isRegressed(changePct, DirectionLowerIsBetter, thresh)
				gated := regressed && thresh > 0

				findings = append(findings, Finding{
					Name:         metricName,
					Type:         string(baseEntry.Type),
					Stat:         key,
					Baseline:     baseVal,
					Current:      currVal,
					ChangePct:    math.Round(changePct*1000) / 1000,
					Direction:    DirectionLowerIsBetter,
					ThresholdPct: thresh,
					Regressed:    regressed,
					Gated:        gated,
				})
			}

		case summary.MetricTypeRate:
			// Compare "rate" key.
			baseVal, baseHas := baseEntry.Values["rate"]
			currVal, currHas := currEntry.Values["rate"]
			if !baseHas || !currHas {
				continue
			}
			changePct := computeChangePct(baseVal, currVal)
			if baseVal == 0 {
				warnings = append(warnings, fmt.Sprintf(
					"baseline value for custom metric %q rate is 0; skipping threshold check",
					metricName,
				))
			}
			regressed := isRegressed(changePct, DirectionLowerIsBetter, thresh)
			gated := regressed && thresh > 0

			findings = append(findings, Finding{
				Name:         metricName,
				Type:         string(baseEntry.Type),
				Stat:         "rate",
				Baseline:     baseVal,
				Current:      currVal,
				ChangePct:    math.Round(changePct*1000) / 1000,
				Direction:    DirectionLowerIsBetter,
				ThresholdPct: thresh,
				Regressed:    regressed,
				Gated:        gated,
			})

		case summary.MetricTypeCounter:
			// Compare "rate" key (RPS/throughput), direction: HigherIsBetter.
			baseVal, baseHas := baseEntry.Values["rate"]
			currVal, currHas := currEntry.Values["rate"]
			if !baseHas || !currHas {
				continue
			}
			changePct := computeChangePct(baseVal, currVal)
			if baseVal == 0 {
				warnings = append(warnings, fmt.Sprintf(
					"baseline value for custom metric %q rate is 0; skipping threshold check",
					metricName,
				))
			}
			regressed := isRegressed(changePct, DirectionHigherIsBetter, thresh)
			gated := regressed && thresh > 0

			findings = append(findings, Finding{
				Name:         metricName,
				Type:         string(baseEntry.Type),
				Stat:         "rate",
				Baseline:     baseVal,
				Current:      currVal,
				ChangePct:    math.Round(changePct*1000) / 1000,
				Direction:    DirectionHigherIsBetter,
				ThresholdPct: thresh,
				Regressed:    regressed,
				Gated:        gated,
			})
		}
	}

	// Count gated regressions.
	regressionsCount := 0
	for _, f := range findings {
		if f.Gated {
			regressionsCount++
		}
	}

	return Report{
		Findings:         findings,
		RegressionsCount: regressionsCount,
		Warnings:         warnings,
	}
}

// compareIterationDuration handles the special case for iteration_duration:
// prefer p(95) if present in both, otherwise fall back to avg.
func compareIterationDuration(
	bm builtinMetricDef,
	baseEntry, currEntry summary.MetricEntry,
	opts Options,
	findings []Finding,
	warnings []string,
) ([]Finding, []string) {
	flagVal := bm.flagThresholdForKey("p(95)", opts)
	thresh := effectiveThreshold(flagVal, bm.defaultThreshold, opts.NoDefaultThresholds)

	// Try p(95) first.
	baseP95, baseHasP95 := baseEntry.Values["p(95)"]
	currP95, currHasP95 := currEntry.Values["p(95)"]

	if baseHasP95 && currHasP95 {
		changePct := computeChangePct(baseP95, currP95)
		if baseP95 == 0 {
			warnings = append(warnings, fmt.Sprintf(
				"baseline value for %q stat %q is 0; skipping threshold check",
				bm.name, "p(95)",
			))
		}
		regressed := isRegressed(changePct, bm.direction, thresh)
		gated := regressed && thresh > 0

		findings = append(findings, Finding{
			Name:         bm.name,
			Type:         string(baseEntry.Type),
			Stat:         "p(95)",
			Baseline:     baseP95,
			Current:      currP95,
			ChangePct:    math.Round(changePct*1000) / 1000,
			Direction:    bm.direction,
			ThresholdPct: thresh,
			Regressed:    regressed,
			Gated:        gated,
		})
		return findings, warnings
	}

	// Fall back to avg.
	baseAvg, baseHasAvg := baseEntry.Values["avg"]
	currAvg, currHasAvg := currEntry.Values["avg"]

	if !baseHasAvg || !currHasAvg {
		warnings = append(warnings, fmt.Sprintf(
			"metric %q has no comparable stat (no p(95) or avg in both); skipping",
			bm.name,
		))
		return findings, warnings
	}

	// Warn if one side had p(95) but not both.
	if baseHasP95 != currHasP95 {
		missingIn := "current"
		if !baseHasP95 {
			missingIn = "baseline"
		}
		warnings = append(warnings, fmt.Sprintf(
			"stat %q for metric %q present in %s only; falling back to avg",
			"p(95)", bm.name, missingIn,
		))
	}

	changePct := computeChangePct(baseAvg, currAvg)
	if baseAvg == 0 {
		warnings = append(warnings, fmt.Sprintf(
			"baseline value for %q stat %q is 0; skipping threshold check",
			bm.name, "avg",
		))
	}
	regressed := isRegressed(changePct, bm.direction, thresh)
	gated := regressed && thresh > 0

	findings = append(findings, Finding{
		Name:         bm.name,
		Type:         string(baseEntry.Type),
		Stat:         "avg",
		Baseline:     baseAvg,
		Current:      currAvg,
		ChangePct:    math.Round(changePct*1000) / 1000,
		Direction:    bm.direction,
		ThresholdPct: thresh,
		Regressed:    regressed,
		Gated:        gated,
	})

	return findings, warnings
}
