// Package loadmodel classifies k6 summary runs as open-model (arrival-rate),
// closed-model (VU-based), or unknown, using the dropped_iterations heuristic.
// It also detects mismatches between two runs.
//
// Note: the heuristic cannot confirm a run is closed — it can only detect
// "probably open" (dropped_iterations > 0) or "inconclusive" (absent/zero).
// ModelClosed is provided as a constant for callers that set it via --assume flags;
// classify() itself never returns ModelClosed.
package loadmodel

// Model represents the detected load model of a k6 test run.
type Model int

const (
	// ModelUnknown means the heuristic is inconclusive:
	// dropped_iterations was absent or its count was zero.
	ModelUnknown Model = iota

	// ModelOpen means the run was probably using an arrival-rate executor:
	// dropped_iterations was present and count > 0.
	ModelOpen

	// ModelClosed is provided for callers that override via --assume-closed-model.
	// The classify() heuristic never returns this value.
	ModelClosed
)

// String returns the lowercase display name for the model.
func (m Model) String() string {
	switch m {
	case ModelOpen:
		return "open"
	case ModelClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// ModelComparison holds the result of comparing the load models of two runs.
type ModelComparison struct {
	// Baseline is the classified model of the baseline run.
	Baseline Model
	// Current is the classified model of the current run.
	Current Model
	// Mismatch is true when both sides are conclusive (non-Unknown) and differ.
	// With pure heuristics this is never true (heuristic returns Open or Unknown only).
	// It can be true when callers inject --assume flags before or after Detect().
	Mismatch bool
	// Forced is set by the caller after checking the --force flag; it is not
	// computed by Detect().
	Forced bool
}
