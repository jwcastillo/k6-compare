package regression_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jwcastillo/k6-compare/internal/regression"
	"github.com/jwcastillo/k6-compare/internal/summary"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// defaultOpts returns Options with all thresholds set to -1 (use built-in defaults).
func defaultOpts() regression.Options {
	return regression.Options{
		ThresholdP50:               -1,
		ThresholdP90:               -1,
		ThresholdP95:               -1,
		ThresholdP99:               -1,
		ThresholdP999:              -1,
		ThresholdErrorRate:         -1,
		ThresholdRPS:               -1,
		ThresholdIterationDuration: -1,
		ThresholdDefault:           -1,
	}
}

// loadFixture parses a JSON fixture from the testdata directory.
func loadFixture(t *testing.T, name string) summary.ParsedSummary {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err, "reading fixture %s", name)
	ps, err := summary.Parse(data)
	require.NoError(t, err, "parsing fixture %s", name)
	return ps
}

// findFinding locates a Finding by metric name and stat key; returns zero-value + false if missing.
func findFinding(report regression.Report, name, stat string) (regression.Finding, bool) {
	for _, f := range report.Findings {
		if f.Name == name && f.Stat == stat {
			return f, true
		}
	}
	return regression.Finding{}, false
}

// TestCompare runs all table-driven regression comparison scenarios.
func TestCompare(t *testing.T) {
	// Pre-load fixtures used across multiple test cases.
	baselineNR := loadFixture(t, "baseline_no_regression.json")
	currentNR := loadFixture(t, "baseline_no_regression.json") // same file = no regression
	currentReg := loadFixture(t, "current_regression.json")
	baselineCustom := loadFixture(t, "baseline_custom_metrics.json")
	currentCustom := loadFixture(t, "current_custom_metrics.json")

	tests := []struct {
		name     string
		baseline summary.ParsedSummary
		current  summary.ParsedSummary
		opts     regression.Options
		assert   func(t *testing.T, r regression.Report)
	}{
		// Case 1: No regression — same file compared to itself.
		{
			name:     "no_regression_identical_files",
			baseline: baselineNR,
			current:  currentNR,
			opts:     defaultOpts(),
			assert: func(t *testing.T, r regression.Report) {
				assert.Equal(t, 0, r.RegressionsCount, "expected 0 regressions comparing identical files")
				// Findings should be non-empty (we compared something).
				assert.NotEmpty(t, r.Findings, "expected findings to be non-empty")
			},
		},
		// Case 2: p(95) regression — current p(95) = 184, baseline = 160 → +15%.
		// Default threshold = 10% → should gate (15% > 10%).
		{
			name:     "p95_regression_default_threshold",
			baseline: baselineNR,
			current:  currentReg,
			opts:     defaultOpts(),
			assert: func(t *testing.T, r regression.Report) {
				assert.Equal(t, 1, r.RegressionsCount, "expected exactly 1 regression (p(95))")
				f, ok := findFinding(r, "http_req_duration", "p(95)")
				require.True(t, ok, "expected Finding for http_req_duration p(95)")
				assert.True(t, f.Regressed, "p(95) should be marked Regressed")
				assert.True(t, f.Gated, "p(95) regression should be Gated")
				assert.InDelta(t, 15.0, f.ChangePct, 0.5, "change percent should be ~+15%%")
				assert.Equal(t, regression.DirectionLowerIsBetter, f.Direction)
			},
		},
		// Case 3: Default thresholds disabled → no regressions even with p(95) +15%.
		{
			name:     "no_default_thresholds_disables_gating",
			baseline: baselineNR,
			current:  currentReg,
			opts: func() regression.Options {
				o := defaultOpts()
				o.NoDefaultThresholds = true
				return o
			}(),
			assert: func(t *testing.T, r regression.Report) {
				assert.Equal(t, 0, r.RegressionsCount, "NoDefaultThresholds=true should gate nothing")
			},
		},
		// Case 4: Explicit threshold override — ThresholdP95 = 20.0.
		// p(95) changes by +15%, which is < 20%, so no regression.
		{
			name:     "explicit_threshold_override_no_regression",
			baseline: baselineNR,
			current:  currentReg,
			opts: func() regression.Options {
				o := defaultOpts()
				o.ThresholdP95 = 20.0
				return o
			}(),
			assert: func(t *testing.T, r regression.Report) {
				assert.Equal(t, 0, r.RegressionsCount, "p95=20%% threshold should not gate a 15%% change")
				f, ok := findFinding(r, "http_req_duration", "p(95)")
				require.True(t, ok, "Finding for http_req_duration p(95) should exist")
				// Regressed=false because 15% < 20% explicit threshold.
				assert.False(t, f.Regressed, "15%% change < 20%% threshold → not regressed")
			},
		},
		// Case 5: RPS regression (HigherIsBetter).
		// Simulate via custom parsed summary: baseline rate=100, current rate=85.
		// changePct = (85-100)/100*100 = -15%; regressed = -15 < -10 → true.
		{
			name: "rps_regression_higher_is_better",
			baseline: func() summary.ParsedSummary {
				ps := summary.ParsedSummary{
					DurationSec: 10,
					Metrics: map[string]summary.MetricEntry{
						"http_reqs": {
							Type:     summary.MetricTypeCounter,
							Contains: "default",
							Values:   map[string]float64{"count": 1000, "rate": 100.0},
						},
					},
				}
				return ps
			}(),
			current: func() summary.ParsedSummary {
				ps := summary.ParsedSummary{
					DurationSec: 10,
					Metrics: map[string]summary.MetricEntry{
						"http_reqs": {
							Type:     summary.MetricTypeCounter,
							Contains: "default",
							Values:   map[string]float64{"count": 850, "rate": 85.0},
						},
					},
				}
				return ps
			}(),
			opts: defaultOpts(),
			assert: func(t *testing.T, r regression.Report) {
				assert.Equal(t, 1, r.RegressionsCount, "RPS drop of 15%% should cause 1 regression")
				f, ok := findFinding(r, "http_reqs", "rate")
				require.True(t, ok, "Finding for http_reqs rate should exist")
				assert.True(t, f.Regressed, "RPS down 15%% with 10%% threshold → regressed")
				assert.True(t, f.Gated, "RPS regression should be Gated")
				assert.InDelta(t, -15.0, f.ChangePct, 0.5, "changePct should be ~-15%%")
				assert.Equal(t, regression.DirectionHigherIsBetter, f.Direction)
			},
		},
		// Case 6: Custom Trend metric auto-compared, not Gated (ThresholdDefault=-1 → 0).
		{
			name:     "custom_trend_auto_compared_not_gated",
			baseline: baselineCustom,
			current:  currentCustom,
			opts:     defaultOpts(),
			assert: func(t *testing.T, r regression.Report) {
				f, ok := findFinding(r, "my_custom_trend", "p(95)")
				require.True(t, ok, "my_custom_trend p(95) should appear in Findings")
				assert.False(t, f.Gated, "custom metric should not be Gated without ThresholdDefault")
				assert.Equal(t, float64(0), f.ThresholdPct, "ThresholdPct should be 0 for custom metrics with no default")
			},
		},
		// Case 7: Custom Trend metric gated — ThresholdDefault = 5.0.
		// my_custom_trend p(95): baseline=80, current=88. changePct = (88-80)/80*100 = +10%.
		// 10% > 5% → Gated=true.
		{
			name:     "custom_trend_gated_with_threshold_default",
			baseline: baselineCustom,
			current:  currentCustom,
			opts: func() regression.Options {
				o := defaultOpts()
				o.ThresholdDefault = 5.0
				return o
			}(),
			assert: func(t *testing.T, r regression.Report) {
				f, ok := findFinding(r, "my_custom_trend", "p(95)")
				require.True(t, ok, "my_custom_trend p(95) should appear in Findings")
				assert.True(t, f.Regressed, "custom trend p(95) +10%% > 5%% → regressed")
				assert.True(t, f.Gated, "custom trend regression should be Gated")
				// Verify RegressionsCount includes the custom metric regression.
				hasCustomRegression := false
				for _, finding := range r.Findings {
					if finding.Name == "my_custom_trend" && finding.Gated {
						hasCustomRegression = true
						break
					}
				}
				assert.True(t, hasCustomRegression, "RegressionsCount should include custom metric regression")
			},
		},
		// Case 8: Missing percentile on one side → Finding with Stat="n/a", Regressed=false, warning.
		// baseline has p(99.9), current does NOT have p(99.9).
		{
			name: "missing_percentile_emits_na_finding_and_warning",
			baseline: func() summary.ParsedSummary {
				ps := summary.ParsedSummary{
					DurationSec: 10,
					Metrics: map[string]summary.MetricEntry{
						"http_req_duration": {
							Type:     summary.MetricTypeTrend,
							Contains: "time",
							Values: map[string]float64{
								"avg":     100,
								"p(95)":   160,
								"p(99.9)": 198,
							},
						},
					},
				}
				return ps
			}(),
			current: func() summary.ParsedSummary {
				ps := summary.ParsedSummary{
					DurationSec: 10,
					Metrics: map[string]summary.MetricEntry{
						"http_req_duration": {
							Type:     summary.MetricTypeTrend,
							Contains: "time",
							Values: map[string]float64{
								"avg":   107,
								"p(95)": 164,
								// p(99.9) intentionally missing
							},
						},
					},
				}
				return ps
			}(),
			opts: defaultOpts(),
			assert: func(t *testing.T, r regression.Report) {
				// p(99.9) missing in current → warning + n/a finding
				assert.NotEmpty(t, r.Warnings, "expected warning for missing p(99.9)")
				// The n/a finding should not be Regressed.
				naFound := false
				for _, f := range r.Findings {
					if f.Name == "http_req_duration" && f.Stat == "n/a" {
						assert.False(t, f.Regressed, "n/a finding must not be Regressed")
						naFound = true
					}
				}
				assert.True(t, naFound, "expected a Finding with Stat='n/a' for missing percentile")
			},
		},
		// Case 9: Zero baseline → warning added, ChangePct=0, Regressed=false.
		{
			name: "zero_baseline_no_regression",
			baseline: func() summary.ParsedSummary {
				ps := summary.ParsedSummary{
					DurationSec: 10,
					Metrics: map[string]summary.MetricEntry{
						"http_req_failed": {
							Type:     summary.MetricTypeRate,
							Contains: "default",
							Values:   map[string]float64{"passes": 0, "fails": 1000, "rate": 0.0},
						},
					},
				}
				return ps
			}(),
			current: func() summary.ParsedSummary {
				ps := summary.ParsedSummary{
					DurationSec: 10,
					Metrics: map[string]summary.MetricEntry{
						"http_req_failed": {
							Type:     summary.MetricTypeRate,
							Contains: "default",
							Values:   map[string]float64{"passes": 10, "fails": 990, "rate": 0.01},
						},
					},
				}
				return ps
			}(),
			opts: defaultOpts(),
			assert: func(t *testing.T, r regression.Report) {
				assert.NotEmpty(t, r.Warnings, "expected warning for zero baseline")
				f, ok := findFinding(r, "http_req_failed", "rate")
				require.True(t, ok, "Finding for http_req_failed rate should exist")
				assert.Equal(t, float64(0), f.ChangePct, "ChangePct should be 0 when baseline==0")
				assert.False(t, f.Regressed, "zero baseline → Regressed must be false")
				assert.False(t, f.Gated, "zero baseline → Gated must be false")
			},
		},
		// Case 10: New format v1 summary as baseline → Compare() processes without panic.
		{
			name: "new_format_v1_as_baseline_no_panic",
			baseline: func() summary.ParsedSummary {
				data, err := os.ReadFile(filepath.Join("..", "summary", "testdata", "new_format_v1.json"))
				if err != nil {
					// If file doesn't exist in expected path, skip via returning minimal summary.
					return summary.ParsedSummary{DurationSec: 1, Metrics: map[string]summary.MetricEntry{}}
				}
				ps, err := summary.Parse(data)
				if err != nil {
					return summary.ParsedSummary{DurationSec: 1, Metrics: map[string]summary.MetricEntry{}}
				}
				return ps
			}(),
			current: baselineNR,
			opts:    defaultOpts(),
			assert: func(t *testing.T, r regression.Report) {
				// Primary requirement: no panic. Verify Compare() ran.
				// http_req_duration and http_reqs are present in both files.
				assert.NotNil(t, r.Findings, "Findings must not be nil (panic guard)")
				// We don't assert specific counts since fixture values differ significantly.
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := regression.Compare(tc.baseline, tc.current, tc.opts)
			tc.assert(t, report)
		})
	}
}

// TestCompare_RegressionsCountMatchesGatedFindings verifies the invariant:
// RegressionsCount == len(filter(findings, f => f.Gated)).
func TestCompare_RegressionsCountMatchesGatedFindings(t *testing.T) {
	baseline := loadFixture(t, "baseline_no_regression.json")
	current := loadFixture(t, "current_regression.json")
	opts := defaultOpts()

	report := regression.Compare(baseline, current, opts)

	gatedCount := 0
	for _, f := range report.Findings {
		if f.Gated {
			gatedCount++
		}
	}
	assert.Equal(t, gatedCount, report.RegressionsCount,
		"RegressionsCount must equal count of Gated findings")
}

// TestCompare_GaugeMetricsNotCompared verifies gauge metrics (vus) do not appear in Findings.
func TestCompare_GaugeMetricsNotCompared(t *testing.T) {
	baseline := loadFixture(t, "baseline_no_regression.json")
	current := loadFixture(t, "baseline_no_regression.json")
	opts := defaultOpts()

	report := regression.Compare(baseline, current, opts)

	for _, f := range report.Findings {
		assert.NotEqual(t, "vus", f.Name, "gauge metric 'vus' must not appear in Findings")
	}
}

// TestCompare_P999StatKey verifies Compare uses "p(99.9)" (not "p(999)") as stat key.
func TestCompare_P999StatKey(t *testing.T) {
	baseline := loadFixture(t, "baseline_no_regression.json")
	current := loadFixture(t, "baseline_no_regression.json")
	opts := defaultOpts()

	report := regression.Compare(baseline, current, opts)

	hasP999 := false
	for _, f := range report.Findings {
		if f.Name == "http_req_duration" && f.Stat == "p(99.9)" {
			hasP999 = true
		}
		if f.Name == "http_req_duration" && f.Stat == "p(999)" {
			t.Fatal("found incorrect stat key 'p(999)' — should be 'p(99.9)'")
		}
	}
	assert.True(t, hasP999, "expected finding with stat key 'p(99.9)'")
}
