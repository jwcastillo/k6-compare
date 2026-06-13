package loadmodel

import "github.com/jwcastillo/k6-compare/internal/summary"

// classify applies the dropped_iterations heuristic to a single summary.
//
// Rules (from RESEARCH.md §D-07, D-08):
//   - dropped_iterations key absent              → ModelUnknown (inconclusive)
//   - dropped_iterations.Values["count"] == 0   → ModelUnknown (inconclusive)
//   - dropped_iterations.Values["count"]  > 0   → ModelOpen    (arrival-rate executor)
//
// ModelClosed is NEVER returned here; the heuristic cannot prove a run is closed.
func classify(s summary.ParsedSummary) Model {
	entry, ok := s.Metrics["dropped_iterations"]
	if !ok {
		return ModelUnknown
	}
	if entry.Values["count"] == 0 {
		return ModelUnknown
	}
	return ModelOpen
}

// Detect classifies both summaries and returns a ModelComparison.
//
// Mismatch semantics (D-09): Mismatch is true only when both sides are
// conclusive (neither Unknown) AND they differ.  With the pure heuristic this
// is never true because classify() only returns Open or Unknown.  Mismatch
// becomes true when callers inject --assume-open-model / --assume-closed-model
// flags before or after calling Detect.
//
// The Forced field is NOT set here; callers set it after checking the --force
// flag.
func Detect(a, b summary.ParsedSummary) ModelComparison {
	ma := classify(a)
	mb := classify(b)
	mismatch := ma != mb && ma != ModelUnknown && mb != ModelUnknown
	return ModelComparison{
		Baseline: ma,
		Current:  mb,
		Mismatch: mismatch,
	}
}
