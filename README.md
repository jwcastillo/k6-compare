# k6-compare

Detect performance regressions between two [k6](https://k6.io) runs and gate
your CI with deterministic exit codes.

k6 can fail a run against its own thresholds, but it cannot compare **two**
runs. `k6-compare` closes that gap: feed it a baseline summary and a current
summary, and it tells you — with an exit code your pipeline can act on —
whether performance got worse.

## Install

```bash
go install github.com/jwcastillo/k6-compare@latest
```

Requires Go 1.25+.

## Usage

Export a summary from each k6 run, then compare:

```bash
# produce summaries (either flag works; handleSummary() JSON also supported)
k6 run --summary-export baseline.json script.js   # on main
k6 run --summary-export current.json script.js    # on your branch

k6-compare baseline.json current.json
```

```
┌───────────────────────────┬──────────┬──────────┬──────────┬───────────┬────────┐
│          METRIC           │ BASELINE │ CURRENT  │  CHANGE  │ THRESHOLD │ STATUS │
├───────────────────────────┼──────────┼──────────┼──────────┼───────────┼────────┤
│ http_req_duration p(90)   │ 140.00ms │ 149.00ms │ ▲ +6.4%  │ +10%      │ PASS   │
│ http_req_duration p(95)   │ 160.00ms │ 184.00ms │ ▲ +15.0% │ +10%      │ FAIL   │
│ http_req_duration p(99)   │ 190.00ms │ 195.00ms │ ▲ +2.6%  │ +10%      │ PASS   │
│ http_req_failed rate      │ 0.50%    │ 0.65%    │ ▲ +30.0% │ +50%      │ PASS   │
│ http_reqs rate            │ 100.00   │ 100.00   │ =        │ -10%      │ PASS   │
│ iteration_duration p(95)  │ 170.00ms │ 174.00ms │ ▲ +2.4%  │ +10%      │ PASS   │
└───────────────────────────┴──────────┴──────────┴──────────┴───────────┴────────┘

Regressions: 1
```

Exit code `3` → your CI fails the merge.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0`  | No regressions detected |
| `3`  | At least one metric regressed beyond its threshold |
| `5`  | Load-model mismatch between the two runs (without `--force`) |
| `1`  | Parse, I/O, or usage error |

## Thresholds

Out of the box, sensible defaults gate the comparison: latency percentiles
**+10%**, error rate **+50%** (relative), requests/second **−10%**. Every
default is overridable per metric, in percent:

```bash
k6-compare baseline.json current.json \
  --threshold-p95 5 \
  --threshold-p99 10 \
  --threshold-error-rate 25 \
  --threshold-rps 15
```

| Flag | Gates | Direction |
|------|-------|-----------|
| `--threshold-p50/-p90/-p95/-p99/-p999` | `http_req_duration` percentiles | regress on increase |
| `--threshold-iteration-duration` | `iteration_duration` p(95) | regress on increase |
| `--threshold-error-rate` | `http_req_failed` rate | regress on increase |
| `--threshold-rps` | `http_reqs` rate | regress on decrease |
| `--threshold-default N` | every custom metric present in both files | metric-type aware |
| `--no-default-thresholds` | report-only mode — only explicit flags gate | — |

Custom Trend/Rate/Counter metrics present in both summaries are always
**compared and reported** automatically; they only **gate** when
`--threshold-default` is set.

## Load-Model Safety

Comparing an open-model run (arrival-rate executors) against a closed-model
run (VU executors) produces meaningless deltas. k6 summaries don't record the
executor, so detection is evidence-based:

- `--assume-open-model` / `--assume-closed-model` declare what both runs
  should be. If a run's metrics contradict the declaration (e.g. declared
  closed but `dropped_iterations > 0` proves open), the mismatch exits `5`.
- `--force` downgrades the mismatch to a warning and compares anyway.

See [DECISIONS.md](DECISIONS.md) for the full design rationale.

## Machine-Readable Output

`--json` replaces the table with a stable schema for downstream tooling:

```bash
k6-compare --json baseline.json current.json | jq '.exit_code, .regressions_count'
```

Fields: `schema_version`, `baseline_file`, `current_file`, `load_model`
(baseline/current/mismatch/forced), `metrics[]` (name, type, stat, baseline,
current, change_pct, direction, threshold_pct, regressed, gated),
`regressions_count`, `exit_code`.

## Supported Summary Formats

Both k6 summary JSON formats are parsed transparently:

- the classic format from `k6 run --summary-export` and `handleSummary()` data
- the machine-readable schema v1.0.0 (`--new-machine-readable-summary`)

Custom `summaryTrendStats` percentiles (e.g. `p(99.9)`) survive parsing, and
sub-metrics like `http_req_duration{expected_response:true}` are preserved.

## CI Integration

A ready-to-use GitHub Actions workflow that runs k6, exports the summary, and
gates the merge on the exit code lives in
[`examples/github-actions/`](examples/github-actions/).

## Running the Tests

```bash
go test -race ./...
```

Covers regression detection, no-regression, load-model mismatch (exit 5 and
`--force`), custom metrics, malformed summaries, non-default
`summaryTrendStats`, and both summary formats — including end-to-end exit-code
tests against the built binary.

## License

Dual-licensed under [MIT](LICENSE-MIT) or [Apache-2.0](LICENSE-APACHE), at
your option.
