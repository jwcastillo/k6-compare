// Package main is the entry point for the k6-compare CLI tool.
// k6-compare detects performance regressions between two k6 summary JSON files.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// buildRootCmd creates the cobra command with all flags registered (per D-04).
// stdout and stderr are injectable for testability.
func buildRootCmd(stdout, stderr io.Writer) (*cobra.Command, *Flags) {
	var flags Flags
	cmd := &cobra.Command{
		Use:           "k6-compare <baseline.json> <current.json>",
		Short:         "Detect performance regressions between two k6 summary files",
		Long: `k6-compare compares two k6 summary JSON files and reports performance regressions.

Exit codes:
  0 — no regressions detected
  1 — parse/IO/usage error
  3 — regression(s) detected (threshold breached)
  5 — load-model mismatch (use --force to override)`,
		Args:          cobra.ExactArgs(2),
		SilenceErrors: true,
		SilenceUsage:  true,
		// RunE is a no-op; exit code is handled by run() called from main().
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	// Register all threshold flags with sentinel -1.0 defaults (D-04).
	cmd.Flags().Float64Var(&flags.ThresholdP50, "threshold-p50", -1,
		"max % regression for p(50) latency (default: 10.0)")
	cmd.Flags().Float64Var(&flags.ThresholdP90, "threshold-p90", -1,
		"max % regression for p(90) latency (default: 10.0)")
	cmd.Flags().Float64Var(&flags.ThresholdP95, "threshold-p95", -1,
		"max % regression for p(95) latency (default: 10.0)")
	cmd.Flags().Float64Var(&flags.ThresholdP99, "threshold-p99", -1,
		"max % regression for p(99) latency (default: 10.0)")
	cmd.Flags().Float64Var(&flags.ThresholdP999, "threshold-p999", -1,
		"max % regression for p(99.9) latency (default: 10.0)")
	cmd.Flags().Float64Var(&flags.ThresholdErrorRate, "threshold-error-rate", -1,
		"max % relative regression for http_req_failed rate (default: 50.0)")
	cmd.Flags().Float64Var(&flags.ThresholdRPS, "threshold-rps", -1,
		"max % regression (decrease) for RPS (default: 10.0)")
	cmd.Flags().Float64Var(&flags.ThresholdIterationDuration, "threshold-iteration-duration", -1,
		"max % regression for iteration_duration (default: 10.0)")
	cmd.Flags().Float64Var(&flags.ThresholdDefault, "threshold-default", -1,
		"max % regression for custom metrics Trend/Rate/Counter (default: not gated)")
	cmd.Flags().BoolVar(&flags.NoDefaultThresholds, "no-default-thresholds", false,
		"disable built-in defaults; only explicitly set thresholds gate")
	cmd.Flags().BoolVar(&flags.AssumeOpenModel, "assume-open-model", false,
		"treat both runs as open model (arrival-rate); skip heuristic")
	cmd.Flags().BoolVar(&flags.AssumeClosedModel, "assume-closed-model", false,
		"treat both runs as closed model (VU-based); skip heuristic")
	cmd.Flags().BoolVar(&flags.Force, "force", false,
		"continue comparison on load-model mismatch (downgrade exit 5 to warning)")
	cmd.Flags().BoolVar(&flags.JSON, "json", false,
		"emit JSON report to stdout instead of human-readable table")

	return cmd, &flags
}

func main() {
	cmd, flags := buildRootCmd(os.Stdout, os.Stderr)

	// Use ExecuteC() not Execute() to avoid internal os.Exit calls (per RESEARCH.md §Anti-Patterns).
	_, err := cmd.ExecuteC()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	args := cmd.Flags().Args()
	if len(args) != 2 {
		// cobra ExactArgs should have caught this, but guard anyway.
		fmt.Fprintln(os.Stderr, "error: requires exactly 2 arguments: <baseline.json> <current.json>")
		os.Exit(1)
	}

	os.Exit(run(args, *flags, os.Stdout, os.Stderr))
}
