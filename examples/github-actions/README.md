# GitHub Actions: k6 Performance Gate

This directory contains a GitHub Actions workflow that runs k6 load tests on every push to `main`, stores the result as a baseline artifact, and compares subsequent pull requests against that baseline using `k6-compare`. Any regression beyond the configured thresholds causes the workflow to fail, blocking the merge.

## What the workflow does

1. **On push to `main`** — the `k6-baseline` job runs the k6 test and uploads `summary.json` as a GitHub Actions artifact named `k6-baseline-summary`.
2. **On pull requests** — the `k6-regression-gate` job runs the k6 test on the PR branch, downloads the baseline artifact, installs `k6-compare`, and runs the comparison. The workflow passes or fails based on the `k6-compare` exit code.

Because GitHub Actions treats any non-zero exit code as a workflow failure, no explicit conditional check is needed — the `k6-compare` command itself gates the merge.

## Prerequisites

- **A k6 test script** at `examples/load-test.js` (or update the `run` step to point to your script).
- **Go available on the runner** — the `ubuntu-latest` runner includes Go, which is needed for `go install github.com/jwcastillo/k6-compare@latest`.
- **Bootstrap push** — the baseline artifact must exist from a prior main-branch run. Push to `main` once before opening PRs to generate the first baseline.

## Exit code reference

| Exit Code | Meaning | Workflow Result |
|-----------|---------|----------------|
| 0 | No regression detected | Pass — PR can merge |
| 3 | Regression detected (threshold breached) | Fail — PR blocked |
| 5 | Load-model mismatch between runs | Fail — add `--force` or use `--assume-*` flags |
| 1 | Parse or IO error (bad summary file) | Fail — check file paths |

## Customizing thresholds

By default, `k6-compare` gates on: latency percentiles +10%, error rate +50% relative, and RPS -10%. All thresholds are overridable via flags in the `Gate regression` step.

```yaml
- name: Gate regression
  run: k6-compare --threshold-p95 15 --threshold-rps 5 baseline/summary.json current-summary.json
```

Common overrides:

```bash
# Tighten p95 to 5%
k6-compare --threshold-p95 5 baseline.json current.json

# Relax error rate to 100% relative (allow doubling)
k6-compare --threshold-error-rate 100 baseline.json current.json

# Set threshold for custom metrics
k6-compare --threshold-default 20 baseline.json current.json

# Report only — no gating (informational PR comment use case)
k6-compare --no-default-thresholds baseline.json current.json
```

## Handling load-model mismatches (exit code 5)

If you know both runs use a closed-model executor (`constant-vus`, `ramping-vus`), suppress the heuristic:

```yaml
- name: Gate regression
  run: k6-compare --assume-closed-model baseline/summary.json current-summary.json
```

If you want the regression report regardless of model mismatch:

```yaml
- name: Gate regression
  run: k6-compare --force baseline/summary.json current-summary.json
```

See [DECISIONS.md](../../DECISIONS.md#4-load-model-heuristic-and-its-limitation) for a full explanation of the heuristic and its known false positives.

## Bootstrapping note

The `k6-baseline` job only runs on pushes to `main`. The first time you add this workflow, you must push to `main` once to generate the baseline artifact before any PR can be successfully gated. Subsequent PRs will download the artifact from the most recent main-branch run.

If a PR arrives before a baseline exists, the `actions/download-artifact` step will fail with "artifact not found." This is expected — push to main first to bootstrap.
