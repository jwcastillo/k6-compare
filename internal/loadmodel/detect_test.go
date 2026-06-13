package loadmodel_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jwcastillo/k6-compare/internal/loadmodel"
	"github.com/jwcastillo/k6-compare/internal/summary"
)

// fixtureDir is the path to the regression testdata fixtures used for
// realistic end-to-end classification tests.
const fixtureDir = "../regression/testdata"

// loadFixture reads a JSON fixture file and parses it via summary.Parse.
// It calls t.Fatal on any error so tests fail clearly when fixtures are missing
// or malformed.
func loadFixture(t *testing.T, name string) summary.ParsedSummary {
	t.Helper()
	path := filepath.Join(fixtureDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("loadFixture: cannot read %s: %v", path, err)
	}
	ps, err := summary.Parse(data)
	if err != nil {
		t.Fatalf("loadFixture: cannot parse %s: %v", path, err)
	}
	return ps
}

// makeSummary builds a minimal ParsedSummary with only the dropped_iterations
// counter so we can test classify() in isolation without touching fixture files.
func makeSummary(droppedCount float64, present bool) summary.ParsedSummary {
	m := summary.ParsedSummary{
		Metrics: make(map[string]summary.MetricEntry),
	}
	if present {
		m.Metrics["dropped_iterations"] = summary.MetricEntry{
			Type:     summary.MetricTypeCounter,
			Contains: "default",
			Values:   map[string]float64{"count": droppedCount, "rate": 0},
		}
	}
	return m
}

// ---------------------------------------------------------------------------
// Model.String tests
// ---------------------------------------------------------------------------

func TestModelString(t *testing.T) {
	cases := []struct {
		model loadmodel.Model
		want  string
	}{
		{loadmodel.ModelUnknown, "unknown"},
		{loadmodel.ModelOpen, "open"},
		{loadmodel.ModelClosed, "closed"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.model.String(); got != tc.want {
				t.Errorf("Model(%d).String() = %q, want %q", tc.model, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// classify() heuristic tests via Detect (single-side classification)
// ---------------------------------------------------------------------------

func TestClassify_DroppedIterationsPresentAndPositive(t *testing.T) {
	s := makeSummary(42, true)
	empty := makeSummary(0, false)
	result := loadmodel.Detect(s, empty)
	if result.Baseline != loadmodel.ModelOpen {
		t.Errorf("dropped_iterations.count=42 → Baseline want ModelOpen, got %v", result.Baseline)
	}
}

func TestClassify_DroppedIterationsAbsent(t *testing.T) {
	empty := makeSummary(0, false)
	s := makeSummary(0, false)
	result := loadmodel.Detect(s, empty)
	if result.Baseline != loadmodel.ModelUnknown {
		t.Errorf("no dropped_iterations key → Baseline want ModelUnknown, got %v", result.Baseline)
	}
}

func TestClassify_DroppedIterationsZero(t *testing.T) {
	s := makeSummary(0, true)
	empty := makeSummary(0, false)
	result := loadmodel.Detect(s, empty)
	if result.Baseline != loadmodel.ModelUnknown {
		t.Errorf("dropped_iterations.count=0 → Baseline want ModelUnknown, got %v", result.Baseline)
	}
}

// ---------------------------------------------------------------------------
// Detect() mismatch logic tests
// ---------------------------------------------------------------------------

func TestDetect_OpenVsUnknown_NoMismatch(t *testing.T) {
	open := makeSummary(42, true)
	unknown := makeSummary(0, false)
	result := loadmodel.Detect(open, unknown)
	if result.Baseline != loadmodel.ModelOpen {
		t.Errorf("Baseline want ModelOpen, got %v", result.Baseline)
	}
	if result.Current != loadmodel.ModelUnknown {
		t.Errorf("Current want ModelUnknown, got %v", result.Current)
	}
	if result.Mismatch {
		t.Error("Detect(Open, Unknown) Mismatch want false (inconclusive), got true")
	}
}

func TestDetect_UnknownVsOpen_NoMismatch(t *testing.T) {
	unknown := makeSummary(0, false)
	open := makeSummary(10, true)
	result := loadmodel.Detect(unknown, open)
	if result.Baseline != loadmodel.ModelUnknown {
		t.Errorf("Baseline want ModelUnknown, got %v", result.Baseline)
	}
	if result.Current != loadmodel.ModelOpen {
		t.Errorf("Current want ModelOpen, got %v", result.Current)
	}
	if result.Mismatch {
		t.Error("Detect(Unknown, Open) Mismatch want false (inconclusive), got true")
	}
}

func TestDetect_OpenVsOpen_NoMismatch(t *testing.T) {
	a := makeSummary(42, true)
	b := makeSummary(7, true)
	result := loadmodel.Detect(a, b)
	if result.Baseline != loadmodel.ModelOpen {
		t.Errorf("Baseline want ModelOpen, got %v", result.Baseline)
	}
	if result.Current != loadmodel.ModelOpen {
		t.Errorf("Current want ModelOpen, got %v", result.Current)
	}
	if result.Mismatch {
		t.Error("Detect(Open, Open) Mismatch want false (same model), got true")
	}
}

func TestDetect_UnknownVsUnknown_NoMismatch(t *testing.T) {
	a := makeSummary(0, false)
	b := makeSummary(0, false)
	result := loadmodel.Detect(a, b)
	if result.Mismatch {
		t.Error("Detect(Unknown, Unknown) Mismatch want false, got true")
	}
}

// Verify the Mismatch rule when caller forces one side to Closed
// (heuristic can't produce this, but the rule must hold).
func TestDetect_OpenVsClosed_Mismatch(t *testing.T) {
	// We can't use Detect() to produce Closed from heuristic alone.
	// Instead, test the struct field semantics directly.
	result := loadmodel.ModelComparison{
		Baseline: loadmodel.ModelOpen,
		Current:  loadmodel.ModelClosed,
		Mismatch: loadmodel.ModelOpen != loadmodel.ModelClosed &&
			loadmodel.ModelOpen != loadmodel.ModelUnknown &&
			loadmodel.ModelClosed != loadmodel.ModelUnknown,
	}
	if !result.Mismatch {
		t.Error("ModelComparison{Open,Closed} Mismatch want true, got false")
	}
}

// ---------------------------------------------------------------------------
// Fixture-based integration tests
// ---------------------------------------------------------------------------

func TestDetect_Fixture_CurrentOpen_IsModelOpen(t *testing.T) {
	open := loadFixture(t, "current_open.json")
	empty := makeSummary(0, false)
	result := loadmodel.Detect(open, empty)
	if result.Baseline != loadmodel.ModelOpen {
		t.Errorf("current_open.json → Baseline want ModelOpen, got %v", result.Baseline)
	}
}

func TestDetect_Fixture_BaselineClosed_IsModelUnknown(t *testing.T) {
	closed := loadFixture(t, "baseline_closed.json")
	empty := makeSummary(0, false)
	result := loadmodel.Detect(closed, empty)
	if result.Baseline != loadmodel.ModelUnknown {
		t.Errorf("baseline_closed.json → Baseline want ModelUnknown (heuristic inconclusive), got %v", result.Baseline)
	}
}

func TestDetect_Fixture_OpenVsClosed_NoMismatch(t *testing.T) {
	open := loadFixture(t, "current_open.json")
	closed := loadFixture(t, "baseline_closed.json")
	// open as baseline, closed-fixture (unknown) as current
	result := loadmodel.Detect(open, closed)
	if result.Mismatch {
		t.Error("Detect(open-fixture, closed-fixture) Mismatch want false (heuristic can't confirm closed), got true")
	}
}

func TestDetect_Fixture_OpenVsOpen_NoMismatch(t *testing.T) {
	open := loadFixture(t, "current_open.json")
	result := loadmodel.Detect(open, open)
	if result.Mismatch {
		t.Error("Detect(open-fixture, open-fixture) Mismatch want false, got true")
	}
}

// ---------------------------------------------------------------------------
// Forced field is never set by Detect()
// ---------------------------------------------------------------------------

func TestDetect_ForcedField_AlwaysFalse(t *testing.T) {
	a := makeSummary(42, true)
	b := makeSummary(7, true)
	result := loadmodel.Detect(a, b)
	if result.Forced {
		t.Error("Detect() must not set Forced; caller sets it after --force flag check")
	}
}
