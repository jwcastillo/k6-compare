package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/jwcastillo/k6-compare/internal/loadmodel"
	"github.com/jwcastillo/k6-compare/internal/regression"
	"github.com/jwcastillo/k6-compare/internal/summary"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/renderer"
	"github.com/olekukonko/tablewriter/tw"
)

// Flags holds all CLI flags for the k6-compare command (per D-04).
// Float64 threshold flags use -1 as sentinel meaning "use built-in default".
type Flags struct {
	ThresholdP50               float64
	ThresholdP90               float64
	ThresholdP95               float64
	ThresholdP99               float64
	ThresholdP999              float64
	ThresholdErrorRate         float64
	ThresholdRPS               float64
	ThresholdIterationDuration float64
	ThresholdDefault           float64
	NoDefaultThresholds        bool
	AssumeOpenModel            bool
	AssumeClosedModel          bool
	Force                      bool
	JSON                       bool
}

// JSONReport is the --json output schema (D-13).
type JSONReport struct {
	SchemaVersion    string        `json:"schema_version"`
	BaselineFile     string        `json:"baseline_file"`
	CurrentFile      string        `json:"current_file"`
	LoadModel        JSONLoadModel `json:"load_model"`
	Metrics          []JSONFinding `json:"metrics"`
	RegressionsCount int           `json:"regressions_count"`
	ExitCode         int           `json:"exit_code"`
}

// JSONLoadModel holds load-model fields for the JSON report.
type JSONLoadModel struct {
	Baseline string `json:"baseline"`
	Current  string `json:"current"`
	Mismatch bool   `json:"mismatch"`
	Forced   bool   `json:"forced"`
}

// JSONFinding is a single metric finding in the JSON report.
type JSONFinding struct {
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	Stat         string  `json:"stat"`
	Baseline     float64 `json:"baseline"`
	Current      float64 `json:"current"`
	ChangePct    float64 `json:"change_pct"`
	Direction    string  `json:"direction"` // "lower_is_better" or "higher_is_better"
	ThresholdPct float64 `json:"threshold_pct"`
	Regressed    bool    `json:"regressed"`
	Gated        bool    `json:"gated"`
}

// defaultFlags returns a Flags struct with all threshold sentinels initialized to -1.
// This matches the cobra default values so that Flags{} behaves like "use built-in defaults".
func defaultFlags() Flags {
	return Flags{
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

// applyFlagDefaults sets any threshold field that is 0 (uninitialized Go zero-value)
// to -1 (the sentinel meaning "use built-in default"). This ensures that Flags{}
// behaves the same as the cobra CLI defaults.
//
// NOTE: This means --threshold-p95 0 (explicitly disabling gating for p95) must be
// expressed instead via --no-default-thresholds. The 0-value ambiguity is a known
// limitation documented in DECISIONS.md.
func applyFlagDefaults(f Flags) Flags {
	if f.ThresholdP50 == 0 {
		f.ThresholdP50 = -1
	}
	if f.ThresholdP90 == 0 {
		f.ThresholdP90 = -1
	}
	if f.ThresholdP95 == 0 {
		f.ThresholdP95 = -1
	}
	if f.ThresholdP99 == 0 {
		f.ThresholdP99 = -1
	}
	if f.ThresholdP999 == 0 {
		f.ThresholdP999 = -1
	}
	if f.ThresholdErrorRate == 0 {
		f.ThresholdErrorRate = -1
	}
	if f.ThresholdRPS == 0 {
		f.ThresholdRPS = -1
	}
	if f.ThresholdIterationDuration == 0 {
		f.ThresholdIterationDuration = -1
	}
	if f.ThresholdDefault == 0 {
		f.ThresholdDefault = -1
	}
	return f
}

// run is the primary entrypoint for CLI logic. It returns an exit code:
//
//	0 — no regressions detected
//	1 — parse/IO error or usage error
//	3 — regression(s) detected (threshold breached)
//	5 — load-model mismatch without --force
//
// All warnings are written to stderr. Table/JSON output goes to stdout.
// This indirection allows all exit-code behavior to be tested without subprocess spawning.
func run(args []string, flags Flags, stdout, stderr io.Writer) int {
	// Normalize zero-value thresholds to -1 (use built-in default), so Flags{}
	// behaves the same as the cobra CLI where defaults are set to -1.
	flags = applyFlagDefaults(flags)

	if len(args) < 2 {
		fmt.Fprintln(stderr, "error: requires exactly 2 arguments: <baseline.json> <current.json>")
		return 1
	}

	baselineFile := args[0]
	currentFile := args[1]

	// Read and parse the baseline summary.
	baselineData, err := os.ReadFile(baselineFile)
	if err != nil {
		fmt.Fprintln(stderr, "error: "+err.Error())
		return 1
	}
	baselineSummary, err := summary.Parse(baselineData)
	if err != nil {
		fmt.Fprintln(stderr, "error: "+err.Error())
		return 1
	}

	// Read and parse the current summary.
	currentData, err := os.ReadFile(currentFile)
	if err != nil {
		fmt.Fprintln(stderr, "error: "+err.Error())
		return 1
	}
	currentSummary, err := summary.Parse(currentData)
	if err != nil {
		fmt.Fprintln(stderr, "error: "+err.Error())
		return 1
	}

	// Load-model detection (D-08, D-09).
	var mc loadmodel.ModelComparison
	switch {
	case flags.AssumeOpenModel:
		// Both runs declared as open. The heuristic can never prove a run is
		// closed, so an "open" declaration cannot be positively contradicted.
		mc = loadmodel.ModelComparison{
			Baseline: loadmodel.ModelOpen,
			Current:  loadmodel.ModelOpen,
		}
	case flags.AssumeClosedModel:
		// Both runs declared as closed. D-08(b): if the heuristic positively
		// proves a run is open (dropped_iterations > 0), the declaration is
		// contradicted — positive mismatch evidence (exit 5 unless --force).
		det := loadmodel.Detect(baselineSummary, currentSummary)
		mc = loadmodel.ModelComparison{
			Baseline: loadmodel.ModelClosed,
			Current:  loadmodel.ModelClosed,
		}
		if det.Baseline == loadmodel.ModelOpen {
			mc.Baseline = loadmodel.ModelOpen
			mc.Mismatch = true
		}
		if det.Current == loadmodel.ModelOpen {
			mc.Current = loadmodel.ModelOpen
			mc.Mismatch = true
		}
	default:
		mc = loadmodel.Detect(baselineSummary, currentSummary)
	}

	if mc.Mismatch {
		warnMsg := fmt.Sprintf(
			"WARNING: load-model mismatch (baseline=%s, current=%s); use --force to override",
			mc.Baseline, mc.Current,
		)
		fmt.Fprintln(stderr, warnMsg)
		if !flags.Force {
			return 5
		}
		// --force: downgrade exit 5 to warning, continue.
		mc.Forced = true
	}

	// Build regression options from flags.
	opts := regression.Options{
		ThresholdP50:               flags.ThresholdP50,
		ThresholdP90:               flags.ThresholdP90,
		ThresholdP95:               flags.ThresholdP95,
		ThresholdP99:               flags.ThresholdP99,
		ThresholdP999:              flags.ThresholdP999,
		ThresholdErrorRate:         flags.ThresholdErrorRate,
		ThresholdRPS:               flags.ThresholdRPS,
		ThresholdIterationDuration: flags.ThresholdIterationDuration,
		ThresholdDefault:           flags.ThresholdDefault,
		NoDefaultThresholds:        flags.NoDefaultThresholds,
	}

	report := regression.Compare(baselineSummary, currentSummary, opts)

	// Emit warnings from the report to stderr (D-12).
	for _, w := range report.Warnings {
		fmt.Fprintln(stderr, "WARNING: "+w)
	}

	// Determine exit code (D-14).
	exitCode := 0
	if report.RegressionsCount > 0 {
		exitCode = 3
	}

	// Render output to stdout (D-11, D-13).
	if flags.JSON {
		renderJSON(stdout, baselineFile, currentFile, mc, report, exitCode)
	} else {
		renderTable(stdout, baselineFile, currentFile, mc, report)
	}

	return exitCode
}

// changeIndicator returns a display string for the change percentage,
// with ▲ for regression (worse direction) and ▼ for improvement (better direction).
// Color is applied when color.NoColor is false.
func changeIndicator(changePct float64, dir regression.Direction) string {
	if changePct == 0 {
		return "="
	}

	pct := changePct
	if pct < 0 {
		pct = -pct
	}

	// Determine if this change is a regression (worse) or improvement (better).
	isWorse := (dir == regression.DirectionLowerIsBetter && changePct > 0) ||
		(dir == regression.DirectionHigherIsBetter && changePct < 0)

	if isWorse {
		s := fmt.Sprintf("▲ +%.1f%%", changePct)
		if dir == regression.DirectionHigherIsBetter && changePct < 0 {
			s = fmt.Sprintf("▲ %.1f%%", changePct)
		}
		_ = pct
		return color.RedString(s)
	}
	// Improvement.
	s := fmt.Sprintf("▼ %.1f%%", changePct)
	if changePct > 0 {
		s = fmt.Sprintf("▼ +%.1f%%", changePct)
	}
	return color.GreenString(s)
}

// formatValue formats a metric value for display in the table.
// Time metrics (trend + contains time) are rendered as ms.
// Rate metrics are rendered as percentages.
// Other metrics are rendered as plain floats.
func formatValue(f regression.Finding, v float64) string {
	metricType := strings.ToLower(f.Type)
	metricName := strings.ToLower(f.Name)

	if metricType == "trend" && (strings.Contains(metricName, "duration") || strings.Contains(metricName, "time") || f.Name == "http_req_duration") {
		return fmt.Sprintf("%.2fms", v)
	}
	if metricType == "rate" {
		return fmt.Sprintf("%.2f%%", v*100)
	}
	return fmt.Sprintf("%.2f", v)
}

// renderTable renders the human-readable regression table to stdout (D-11).
func renderTable(stdout io.Writer, baselineFile, currentFile string, mc loadmodel.ModelComparison, report regression.Report) {
	// Print header info.
	fmt.Fprintf(stdout, "baseline: %s\n", baselineFile)
	fmt.Fprintf(stdout, "current:  %s\n", currentFile)
	fmt.Fprintf(stdout, "load model: baseline=%s current=%s\n\n", mc.Baseline, mc.Current)

	colorCfg := renderer.ColorizedConfig{
		Header: renderer.Tint{FG: renderer.Colors{color.FgHiWhite, color.Bold}},
	}
	table := tablewriter.NewTable(stdout,
		tablewriter.WithRenderer(renderer.NewColorized(colorCfg)),
		tablewriter.WithConfig(tablewriter.Config{
			Row: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignLeft}},
		}),
	)
	table.Header("Metric", "Baseline", "Current", "Change", "Threshold", "Status")

	for _, f := range report.Findings {
		metric := f.Name
		if f.Stat != "" && f.Stat != "n/a" {
			metric = f.Name + " " + f.Stat
		}

		var baselineStr, currentStr, changeStr string
		if f.Stat == "n/a" {
			baselineStr = "n/a"
			currentStr = "n/a"
			changeStr = "n/a"
		} else {
			baselineStr = formatValue(f, f.Baseline)
			currentStr = formatValue(f, f.Current)
			changeStr = changeIndicator(f.ChangePct, f.Direction)
		}

		var thresholdStr string
		if f.ThresholdPct > 0 {
			if f.Direction == regression.DirectionLowerIsBetter {
				thresholdStr = fmt.Sprintf("+%.0f%%", f.ThresholdPct)
			} else {
				thresholdStr = fmt.Sprintf("-%.0f%%", f.ThresholdPct)
			}
		} else {
			thresholdStr = "-"
		}

		var statusStr string
		switch {
		case f.Stat == "n/a":
			statusStr = "SKIP"
		case f.Gated:
			statusStr = color.RedString("FAIL")
		case f.Regressed:
			statusStr = color.YellowString("WARN")
		default:
			statusStr = color.GreenString("PASS")
		}

		if err := table.Append([]string{metric, baselineStr, currentStr, changeStr, thresholdStr, statusStr}); err != nil {
			// Best effort — continue on error.
			_ = err
		}
	}

	if err := table.Render(); err != nil {
		_ = err
	}

	fmt.Fprintf(stdout, "\nRegressions: %d\n", report.RegressionsCount)
}

// renderJSON renders the structured JSON report to stdout (D-13).
func renderJSON(stdout io.Writer, baselineFile, currentFile string, mc loadmodel.ModelComparison, report regression.Report, exitCode int) {
	metrics := make([]JSONFinding, 0, len(report.Findings))
	for _, f := range report.Findings {
		dirStr := "lower_is_better"
		if f.Direction == regression.DirectionHigherIsBetter {
			dirStr = "higher_is_better"
		}
		metrics = append(metrics, JSONFinding{
			Name:         f.Name,
			Type:         f.Type,
			Stat:         f.Stat,
			Baseline:     f.Baseline,
			Current:      f.Current,
			ChangePct:    f.ChangePct,
			Direction:    dirStr,
			ThresholdPct: f.ThresholdPct,
			Regressed:    f.Regressed,
			Gated:        f.Gated,
		})
	}

	jr := JSONReport{
		SchemaVersion: "1",
		BaselineFile:  baselineFile,
		CurrentFile:   currentFile,
		LoadModel: JSONLoadModel{
			Baseline: mc.Baseline.String(),
			Current:  mc.Current.String(),
			Mismatch: mc.Mismatch,
			Forced:   mc.Forced,
		},
		Metrics:          metrics,
		RegressionsCount: report.RegressionsCount,
		ExitCode:         exitCode,
	}

	data, err := json.MarshalIndent(jr, "", "  ")
	if err != nil {
		// Fallback: emit minimal valid JSON.
		fmt.Fprintln(stdout, `{"schema_version":"1","error":"marshal failed"}`)
		return
	}
	fmt.Fprintln(stdout, string(data))
}
