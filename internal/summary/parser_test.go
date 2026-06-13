package summary

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper: readFixture reads a testdata JSON file and fails the test if not found.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err, "reading testdata/%s", name)
	return data
}

// withinDelta asserts that got is within delta of want.
func withinDelta(t *testing.T, want, got float64, delta float64, msgAndArgs ...interface{}) {
	t.Helper()
	assert.InDelta(t, want, got, delta, msgAndArgs...)
}

// ─── PARSE-01: Format detection ──────────────────────────────────────────────

// TestParseFormatDetectionLegacy verifies that a legacy handleSummary file is
// detected and parsed without error, returning DurationSec = 1.0.
func TestParseFormatDetectionLegacy(t *testing.T) {
	data := readFixture(t, "legacy_handle_summary.json")
	got, err := Parse(data)
	require.NoError(t, err)
	withinDelta(t, 1.0, got.DurationSec, 0.001)
}

// TestParseFormatDetectionNew verifies that a new-format (version 1.0.0) file is
// detected and parsed without error, returning DurationSec = 1.0.
func TestParseFormatDetectionNew(t *testing.T) {
	data := readFixture(t, "new_format_v1.json")
	got, err := Parse(data)
	require.NoError(t, err)
	withinDelta(t, 1.0, got.DurationSec, 0.001)
}

// ─── PARSE-06: Duration extraction ───────────────────────────────────────────

// TestParseDurationLegacy checks that state.testRunDurationMs is converted to
// seconds correctly (1000 ms → 1.0 s).
func TestParseDurationLegacy(t *testing.T) {
	data := readFixture(t, "legacy_handle_summary.json")
	got, err := Parse(data)
	require.NoError(t, err)
	withinDelta(t, 1.0, got.DurationSec, 0.001, "DurationSec should be 1.0 (1000ms / 1000)")
}

// TestParseDurationNew checks that config.duration (already in seconds) is used
// directly.
func TestParseDurationNew(t *testing.T) {
	data := readFixture(t, "new_format_v1.json")
	got, err := Parse(data)
	require.NoError(t, err)
	withinDelta(t, 1.0, got.DurationSec, 0.001, "DurationSec should equal config.duration = 1.0")
}

// TestParseDurationExportFormat checks that the --summary-export format (which
// has no state block) results in DurationSec = 0.
func TestParseDurationExportFormat(t *testing.T) {
	data := readFixture(t, "legacy_export.json")
	got, err := Parse(data)
	require.NoError(t, err)
	withinDelta(t, 0.0, got.DurationSec, 0.001, "DurationSec should be 0 (no state block in export format)")
}

// ─── PARSE-03: Rate metric normalization ─────────────────────────────────────

// TestParseRateLegacyNormalization checks that the legacy handleSummary rate
// metric "checks" has its passes/fails/rate values normalised correctly.
func TestParseRateLegacyNormalization(t *testing.T) {
	data := readFixture(t, "legacy_handle_summary.json")
	got, err := Parse(data)
	require.NoError(t, err)

	entry, ok := got.Metrics["checks"]
	require.True(t, ok, "Metrics map must contain 'checks'")
	assert.Equal(t, MetricTypeRate, entry.Type)
	withinDelta(t, 45.0, entry.Values["passes"], 0.001)
	withinDelta(t, 15.0, entry.Values["fails"], 0.001)
	withinDelta(t, 0.75, entry.Values["rate"], 0.001)
}

// TestParseRateNewFormatNormalization checks that the new format rate metric
// normalises matches → passes; (total - matches) → fails.
func TestParseRateNewFormatNormalization(t *testing.T) {
	data := readFixture(t, "new_format_v1.json")
	got, err := Parse(data)
	require.NoError(t, err)

	entry, ok := got.Metrics["checks"]
	require.True(t, ok, "Metrics map must contain 'checks'")
	assert.Equal(t, MetricTypeRate, entry.Type)
	withinDelta(t, 45.0, entry.Values["passes"], 0.001, "passes = matches = 45")
	withinDelta(t, 15.0, entry.Values["fails"], 0.001, "fails = total - matches = 60 - 45 = 15")
	withinDelta(t, 0.75, entry.Values["rate"], 0.001)
}

// TestParseCounterRateSynthesis checks that the new format counter metric has
// its rate synthesised as count / durationSec.
func TestParseCounterRateSynthesis(t *testing.T) {
	data := readFixture(t, "new_format_v1.json")
	got, err := Parse(data)
	require.NoError(t, err)

	entry, ok := got.Metrics["http_reqs"]
	require.True(t, ok, "Metrics map must contain 'http_reqs'")
	assert.Equal(t, MetricTypeCounter, entry.Type)
	withinDelta(t, 3.0, entry.Values["count"], 0.001)
	withinDelta(t, 3.0, entry.Values["rate"], 0.001, "rate = count / durationSec = 3 / 1.0 = 3.0")
}

// ─── PARSE-02: Trend metric custom stats ─────────────────────────────────────

// TestParseTrendCustomStats checks that when summaryTrendStats is ["avg","p(99.9)"],
// only those two keys are present and p(99.9) value is preserved.
func TestParseTrendCustomStats(t *testing.T) {
	data := readFixture(t, "legacy_custom_stats.json")
	got, err := Parse(data)
	require.NoError(t, err)

	entry, ok := got.Metrics["my_trend"]
	require.True(t, ok, "Metrics map must contain 'my_trend'")
	assert.Equal(t, MetricTypeTrend, entry.Type)
	withinDelta(t, 19.99, entry.Values["p(99.9)"], 0.001, "p(99.9) must be preserved")
	assert.NotContains(t, entry.Values, "p(90)", "p(90) should not be present (not in summaryTrendStats)")
	assert.NotContains(t, entry.Values, "p(95)", "p(95) should not be present (not in summaryTrendStats)")
}

// TestParseTrendDefaultStats checks that with the default summaryTrendStats the
// trend values including p(99), avg and count are all present.
func TestParseTrendDefaultStats(t *testing.T) {
	data := readFixture(t, "legacy_handle_summary.json")
	got, err := Parse(data)
	require.NoError(t, err)

	entry, ok := got.Metrics["my_trend"]
	require.True(t, ok, "Metrics map must contain 'my_trend'")
	withinDelta(t, 19.9, entry.Values["p(99)"], 0.001)
	withinDelta(t, 15.0, entry.Values["avg"], 0.001)
	withinDelta(t, 3.0, entry.Values["count"], 0.001)
}

// TestParseTrendNewFormatCustomStats checks that p(99) survives parsing of the
// new format (must NOT be silently dropped by a fixed struct).
func TestParseTrendNewFormatCustomStats(t *testing.T) {
	data := readFixture(t, "new_format_v1.json")
	got, err := Parse(data)
	require.NoError(t, err)

	entry, ok := got.Metrics["http_req_duration"]
	require.True(t, ok, "Metrics map must contain 'http_req_duration'")
	withinDelta(t, 19.9, entry.Values["p(99)"], 0.001, "p(99) must be preserved in new format")
}

// ─── PARSE-04: Sub-metric keys ───────────────────────────────────────────────

// TestParseSubMetric checks that sub-metric keys (with tag suffix) are
// preserved verbatim in the Metrics map.
func TestParseSubMetric(t *testing.T) {
	data := readFixture(t, "legacy_with_submetric.json")
	got, err := Parse(data)
	require.NoError(t, err)

	key := "http_req_duration{expected_response:true}"
	entry, ok := got.Metrics[key]
	require.True(t, ok, "Metrics map must contain %q", key)
	assert.Equal(t, MetricTypeTrend, entry.Type)
	withinDelta(t, 14.0, entry.Values["avg"], 0.001)
}

// ─── PARSE-05: Error handling ─────────────────────────────────────────────────

// TestParseMalformedEmpty checks that empty input returns a non-nil error
// containing "invalid JSON".
func TestParseMalformedEmpty(t *testing.T) {
	_, err := Parse([]byte{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON")
}

// TestParseMalformedNull checks that "null" JSON returns a non-nil error
// containing "document is null".
func TestParseMalformedNull(t *testing.T) {
	_, err := Parse([]byte("null"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "document is null")
}

// TestParseMalformedTruncated checks that truncated JSON returns a non-nil error
// without panicking.
func TestParseMalformedTruncated(t *testing.T) {
	_, err := Parse([]byte(`{"version": "1.0.0", "config":`))
	require.Error(t, err)
}

// TestParseNoPanic checks that Parse() never panics on adversarial input.
func TestParseNoPanic(t *testing.T) {
	longStr := strings.Repeat("a", 10000)
	inputs := []struct {
		name  string
		input []byte
	}{
		{"empty", []byte{}},
		{"null", []byte("null")},
		{"empty_object", []byte("{}")},
		{"array", []byte("[]")},
		{"bad_value", []byte(`{"x":}`)},
		{"version_int", []byte(`{"version":1}`)},
		{"metrics_null", []byte(`{"metrics":null}`)},
		{"binary_garbage", []byte{0x01, 0x02, 0x03, 0xff, 0xfe, 'g', 'a', 'r', 'b', 'a', 'g', 'e'}},
		{"long_string", []byte(`{"key":"` + longStr + `"}`)},
		{"unicode_edge", []byte(`{" ":"\ud800"}`)},
	}

	for _, tc := range inputs {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Parse() panicked on input %q: %v", tc.name, r)
				}
			}()
			// We don't care about the error; we just must not panic.
			_, _ = Parse(tc.input)
		})
	}
}
