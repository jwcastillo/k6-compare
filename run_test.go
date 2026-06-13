package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/require"
)

// testBinaryPath holds the path to the built binary used by the smoke test.
var testBinaryPath string

// TestMain builds the binary before running tests, then removes it after.
func TestMain(m *testing.M) {
	color.NoColor = true

	// Build the binary for smoke tests.
	tmp, err := os.CreateTemp("", "k6-compare-test-*")
	if err != nil {
		panic("failed to create temp file for binary: " + err.Error())
	}
	tmp.Close()
	testBinaryPath = tmp.Name()

	out, err := exec.Command("go", "build", "-o", testBinaryPath, ".").CombinedOutput()
	if err != nil {
		panic("failed to build k6-compare binary: " + err.Error() + "\n" + string(out))
	}

	code := m.Run()

	os.Remove(testBinaryPath)
	os.Exit(code)
}

// baselineNoRegression and currentRegression are relative to the package dir (cmd/k6-compare/).
const (
	fixtureDir            = "internal/regression/testdata"
	fixtureBaseline       = fixtureDir + "/baseline_no_regression.json"
	fixtureCurrent        = fixtureDir + "/current_regression.json"
	fixtureSame           = fixtureDir + "/baseline_no_regression.json"
	fixtureBaselineClosed = fixtureDir + "/baseline_closed.json"
	fixtureCurrentOpen    = fixtureDir + "/current_open.json"
)

func discard() (io.Writer, io.Writer) {
	return io.Discard, io.Discard
}

// TestRun_ExitCodes tests the run() function exit code logic.
func TestRun_ExitCodes(t *testing.T) {
	color.NoColor = true

	tests := []struct {
		name     string
		args     []string
		flags    Flags
		wantCode int
	}{
		{
			name:     "regression detected exits 3",
			args:     []string{fixtureBaseline, fixtureCurrent},
			flags:    Flags{},
			wantCode: 3,
		},
		{
			name:     "no regression exits 0",
			args:     []string{fixtureBaseline, fixtureSame},
			flags:    Flags{},
			wantCode: 0,
		},
		{
			name:     "no-default-thresholds with regression exits 0",
			args:     []string{fixtureBaseline, fixtureCurrent},
			flags:    Flags{NoDefaultThresholds: true},
			wantCode: 0,
		},
		{
			name:     "explicit threshold wide enough exits 0",
			args:     []string{fixtureBaseline, fixtureCurrent},
			flags:    Flags{ThresholdP95: 20.0},
			wantCode: 0,
		},
		{
			name:     "nonexistent baseline exits 1",
			args:     []string{"nonexistent_file_xyz.json", fixtureBaseline},
			flags:    Flags{},
			wantCode: 1,
		},
		{
			name:     "both args missing exits 1",
			args:     []string{"nonexistent_a.json", "nonexistent_b.json"},
			flags:    Flags{},
			wantCode: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := discard()
			got := run(tt.args, tt.flags, stdout, stderr)
			require.Equal(t, tt.wantCode, got, "exit code mismatch")
		})
	}
}

// TestRun_AssumeFlags tests the --assume-open-model and --assume-closed-model flags.
func TestRun_AssumeFlags(t *testing.T) {
	color.NoColor = true

	t.Run("assume-open-model skips heuristic, result by thresholds", func(t *testing.T) {
		stdout, stderr := discard()
		flags := Flags{AssumeOpenModel: true}
		got := run([]string{fixtureBaseline, fixtureCurrent}, flags, stdout, stderr)
		// Same as default — regressions exist, so should exit 3
		require.Equal(t, 3, got)
	})

	t.Run("assume-closed-model skips heuristic, result by thresholds", func(t *testing.T) {
		stdout, stderr := discard()
		flags := Flags{AssumeClosedModel: true}
		got := run([]string{fixtureBaseline, fixtureCurrent}, flags, stdout, stderr)
		// Same as default — regressions exist, so should exit 3
		require.Equal(t, 3, got)
	})

	t.Run("assume-open-model no-default-thresholds exits 0", func(t *testing.T) {
		stdout, stderr := discard()
		flags := Flags{AssumeOpenModel: true, NoDefaultThresholds: true}
		got := run([]string{fixtureBaseline, fixtureCurrent}, flags, stdout, stderr)
		require.Equal(t, 0, got)
	})
}

// TestRun_ModelMismatch tests exit 5 on model mismatch and --force downgrade.
func TestRun_ModelMismatch(t *testing.T) {
	color.NoColor = true

	// Mismatch paths (D-08):
	//  (a) pure heuristic: requires both sides conclusive — unreachable today
	//      because classify() only returns Open or Unknown.
	//  (b) declaration contradicted by positive evidence: --assume-closed-model
	//      while a run's heuristic proves Open (dropped_iterations > 0) → exit 5
	//      unless --force. --assume-open-model can never be contradicted (the
	//      heuristic cannot prove Closed).
	t.Run("assume-closed contradicted by open evidence exits 5", func(t *testing.T) {
		var stderr bytes.Buffer
		flags := Flags{AssumeClosedModel: true}
		got := run([]string{fixtureBaselineClosed, fixtureCurrentOpen}, flags, io.Discard, &stderr)
		require.Equal(t, 5, got)
		require.Contains(t, stderr.String(), "mismatch")
	})

	t.Run("assume-closed contradiction with --force downgrades to thresholds", func(t *testing.T) {
		var stderr bytes.Buffer
		flags := Flags{AssumeClosedModel: true, Force: true, NoDefaultThresholds: true}
		got := run([]string{fixtureBaselineClosed, fixtureCurrentOpen}, flags, io.Discard, &stderr)
		require.Equal(t, 0, got)
		require.Contains(t, stderr.String(), "mismatch")
	})

	t.Run("assume-open cannot be contradicted, exits by thresholds", func(t *testing.T) {
		stdout, stderr := discard()
		flags := Flags{AssumeOpenModel: true, NoDefaultThresholds: true}
		got := run([]string{fixtureBaselineClosed, fixtureCurrentOpen}, flags, stdout, stderr)
		require.Equal(t, 0, got)
	})

	t.Run("no mismatch with standard fixtures exits by thresholds", func(t *testing.T) {
		stdout, stderr := discard()
		flags := Flags{Force: true} // --force with no mismatch = no effect
		got := run([]string{fixtureBaseline, fixtureCurrent}, flags, stdout, stderr)
		require.Equal(t, 3, got) // regressions still detected
	})

	t.Run("no mismatch with no-default-thresholds exits 0", func(t *testing.T) {
		stdout, stderr := discard()
		flags := Flags{Force: true, NoDefaultThresholds: true}
		got := run([]string{fixtureBaseline, fixtureCurrent}, flags, stdout, stderr)
		require.Equal(t, 0, got)
	})
}

// TestRun_TableOutput tests that the human table output contains expected strings.
func TestRun_TableOutput(t *testing.T) {
	color.NoColor = true

	t.Run("table contains FAIL for regression", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		flags := Flags{}
		code := run([]string{fixtureBaseline, fixtureCurrent}, flags, &stdout, &stderr)
		require.Equal(t, 3, code)
		out := stdout.String()
		require.Contains(t, out, "FAIL", "table should contain FAIL for regressed metrics")
	})

	t.Run("table contains PASS for no-regression", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		flags := Flags{}
		code := run([]string{fixtureBaseline, fixtureSame}, flags, &stdout, &stderr)
		require.Equal(t, 0, code)
		out := stdout.String()
		require.Contains(t, out, "PASS", "table should contain PASS for non-regressed metrics")
	})

	t.Run("table contains triangle indicator for regressed metric", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		flags := Flags{}
		code := run([]string{fixtureBaseline, fixtureCurrent}, flags, &stdout, &stderr)
		require.Equal(t, 3, code)
		out := stdout.String()
		// The up-triangle ▲ is used for regressions (worse latency = higher value = LowerIsBetter)
		require.Contains(t, out, "▲", "table should contain ▲ for worse metric")
	})
}

// TestRun_JSONOutput tests that --json flag produces valid JSON with the D-13 schema.
func TestRun_JSONOutput(t *testing.T) {
	color.NoColor = true

	t.Run("json output has schema_version 1", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		flags := Flags{JSON: true}
		code := run([]string{fixtureBaseline, fixtureCurrent}, flags, &stdout, &stderr)
		require.Equal(t, 3, code)

		var report JSONReport
		err := json.Unmarshal(stdout.Bytes(), &report)
		require.NoError(t, err, "JSON output should be valid")
		require.Equal(t, "1", report.SchemaVersion)
	})

	t.Run("json output has exit_code matching return value", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		flags := Flags{JSON: true}
		code := run([]string{fixtureBaseline, fixtureCurrent}, flags, &stdout, &stderr)

		var report JSONReport
		err := json.Unmarshal(stdout.Bytes(), &report)
		require.NoError(t, err)
		require.Equal(t, code, report.ExitCode)
	})

	t.Run("json output has all required fields", func(t *testing.T) {
		var stdout bytes.Buffer
		flags := Flags{JSON: true}
		run([]string{fixtureBaseline, fixtureCurrent}, flags, &stdout, io.Discard)

		var raw map[string]interface{}
		err := json.Unmarshal(stdout.Bytes(), &raw)
		require.NoError(t, err)

		requiredFields := []string{
			"schema_version", "baseline_file", "current_file",
			"load_model", "metrics", "regressions_count", "exit_code",
		}
		for _, f := range requiredFields {
			require.Contains(t, raw, f, "JSON output missing field: "+f)
		}
	})

	t.Run("json metrics have direction field", func(t *testing.T) {
		var stdout bytes.Buffer
		flags := Flags{JSON: true}
		run([]string{fixtureBaseline, fixtureCurrent}, flags, &stdout, io.Discard)

		var report JSONReport
		err := json.Unmarshal(stdout.Bytes(), &report)
		require.NoError(t, err)
		require.NotEmpty(t, report.Metrics, "metrics should not be empty")

		for _, m := range report.Metrics {
			require.True(t, m.Direction == "lower_is_better" || m.Direction == "higher_is_better",
				"direction must be lower_is_better or higher_is_better, got: %s", m.Direction)
		}
	})
}

// TestRun_WarningsToStderr tests that warnings are routed to stderr, not stdout.
func TestRun_WarningsToStderr(t *testing.T) {
	color.NoColor = true

	t.Run("stdout is clean when no json flag", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		flags := Flags{}
		run([]string{fixtureBaseline, fixtureCurrent}, flags, &stdout, &stderr)
		// stdout should contain only the table
		// warnings like "WARNING:" should NOT appear on stdout
		stdoutStr := stdout.String()
		require.NotContains(t, stdoutStr, "WARNING:", "warnings should go to stderr, not stdout")
	})
}

// TestBinaryExitCode is a smoke test that builds the binary and checks exit codes via os/exec.
func TestBinaryExitCode(t *testing.T) {
	if testBinaryPath == "" {
		t.Skip("binary path not set")
	}

	t.Run("binary exits 3 on regression", func(t *testing.T) {
		cmd := exec.Command(testBinaryPath, fixtureBaseline, fixtureCurrent)
		err := cmd.Run()
		exitErr, ok := err.(*exec.ExitError)
		require.True(t, ok, "expected ExitError, got: %v", err)
		require.Equal(t, 3, exitErr.ExitCode())
	})

	t.Run("binary exits 0 on no-regression", func(t *testing.T) {
		cmd := exec.Command(testBinaryPath, fixtureBaseline, fixtureSame)
		err := cmd.Run()
		require.NoError(t, err, "expected exit 0, got error: %v", err)
	})

	t.Run("binary exits 1 on nonexistent file", func(t *testing.T) {
		cmd := exec.Command(testBinaryPath, "nonexistent.json", fixtureBaseline)
		err := cmd.Run()
		exitErr, ok := err.(*exec.ExitError)
		require.True(t, ok, "expected ExitError, got: %v", err)
		require.Equal(t, 1, exitErr.ExitCode())
	})

	t.Run("binary exits 1 on wrong arg count", func(t *testing.T) {
		cmd := exec.Command(testBinaryPath, fixtureBaseline)
		err := cmd.Run()
		exitErr, ok := err.(*exec.ExitError)
		require.True(t, ok, "expected ExitError, got: %v", err)
		require.Equal(t, 1, exitErr.ExitCode())
	})
}
