# GitHub Actions: k6 Performance Gate

This directory contains a GitHub Actions workflow ([`compare.yml`](compare.yml)) that runs a k6 load test on every pull request and compares the result against a baseline **versioned in the repository** using `k6-compare`. Any regression beyond the configured thresholds causes the workflow to fail, blocking the merge.

## What the workflow does

On every pull request, the `load-gate` job:

1. Installs k6 and `k6-compare` (`go install github.com/jwcastillo/k6-compare@latest`).
2. Runs `examples/scripts/smoke.js`, which writes `summary.json` via `handleSummary`.
3. Runs `k6-compare examples/compare-demo/baseline.json summary.json` with the configured thresholds.

Because GitHub Actions treats any non-zero exit code as a workflow failure, no explicit conditional is needed — the `k6-compare` command itself gates the merge.

## Why a versioned baseline

The baseline lives in the repo at `examples/compare-demo/baseline.json`. Updating it is a deliberate, reviewable PR: when a performance change is expected and accepted, you commit the new baseline, and that commit is the explicit record of "this is the new normal."

This trades the zero-setup of an ephemeral artifact baseline for an auditable one — there is no bootstrap push required, the gate is reproducible from any checkout, and the history of the baseline file is the history of your accepted performance budget.

## Prerequisites

- **A k6 script** that emits `summary.json` via `handleSummary` (see `examples/scripts/smoke.js`). The `summaryTrendStats` must include `p(99)` and `p(99.9)` — k6 omits these tail percentiles by default, and `k6-compare` cannot gate on what is not in the summary.
- **A committed baseline** at `examples/compare-demo/baseline.json`. Generate one by running the script locally (`k6 run examples/scripts/smoke.js`) and committing the resulting `summary.json`.

## Exit code reference

| Exit Code | Meaning | Workflow Result |
|-----------|---------|----------------|
| 0 | No regression detected | Pass — PR can merge |
| 3 | Regression detected (threshold breached) | Fail — PR blocked |
| 5 | Load-model mismatch between runs | Fail — add `--force` or use `--assume-*` flags |
| 1 | Parse or IO error (bad summary file) | Fail — check file paths |

## Customizing thresholds

The `Compare against baseline` step gates on latency percentiles, error rate, and RPS. All thresholds are overridable via flags:

```bash
# Tighten p95 to 5%
k6-compare --threshold-p95 5 baseline.json summary.json

# Relax error rate to 100% relative (allow doubling)
k6-compare --threshold-error-rate 100 baseline.json summary.json

# Set a threshold for custom metrics
k6-compare --threshold-default 20 baseline.json summary.json

# Report only — no gating (informational use case)
k6-compare --no-default-thresholds baseline.json summary.json
```

## Handling load-model mismatches (exit code 5)

If you know both runs use a closed-model executor (`constant-vus`, `ramping-vus`), suppress the heuristic:

```yaml
- name: Compare against baseline
  run: k6-compare --assume-closed-model examples/compare-demo/baseline.json summary.json
```

If you want the regression report regardless of model mismatch:

```yaml
- name: Compare against baseline
  run: k6-compare --force examples/compare-demo/baseline.json summary.json
```

See [DECISIONS.md](../../DECISIONS.md#4-load-model-heuristic-and-its-limitation) for a full explanation of the heuristic and its known false positives.
