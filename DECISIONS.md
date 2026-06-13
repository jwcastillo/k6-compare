# k6-compare Architectural Decisions

This document records the four key architectural decisions in `k6-compare` that affect how users interpret results, configure thresholds, and integrate the tool into CI pipelines. It fulfils requirement INF-03.

---

## 1. Dual-Format Parsing Strategy

k6 has produced two distinct JSON summary formats over its history, and both are in active use.

**Legacy format** — produced by `--summary-export` or a `handleSummary()` script that returns a summary object. This format has no `"version"` key at the root. Metrics are stored as a flat object where each key is a metric name and the value contains type-specific sub-fields (`avg`, `med`, `p(90)`, `p(95)`, etc. for Trend; `count`, `rate` for Counter; `rate`, `passes`, `fails` for Rate).

**New machine-readable format (v1.0.0+)** — produced by k6's built-in `--summary-export` since the introduction of the structured format, identified by `"version": "1.0.0"` at the root. Metrics live under `results.metrics[]` as an array with explicit `name`, `type`, `contains`, and `values` fields.

The parser dispatches on the presence of the `"version"` key. If present with value `"1.0.0"`, it takes the new-format path; otherwise it falls back to the legacy path. Both paths normalize their output into a single `ParsedSummary{DurationSec float64, Metrics map[string]MetricEntry}` type that all downstream comparison logic consumes.

During normalization:

- Legacy Rate metrics use the field names `matches` (passes) and `total-matches` (fails) in the raw JSON. The parser renames these to `passes` and `fails` respectively for uniform downstream access.
- New-format Counter metrics carry no `rate` field in the JSON. The parser synthesizes `rate` as `count / durationSec`, making RPS available under the same `Values["rate"]` key used by the legacy format.

This means `k6-compare` works identically regardless of which k6 version or summary method was used to generate the input files.

---

## 2. RPS Derivation

Requests-per-second is derived from the `http_reqs` metric's `rate` field, accessible as `MetricEntry.Values["rate"]`.

For the **legacy format**, the `rate` field is present directly in the raw JSON — it represents the per-second rate over the test duration as recorded by k6.

For the **new format**, there is no `rate` field in the Counter metric values. The parser synthesizes it as `count / durationSec` during parsing, so by the time `k6-compare` reads the `ParsedSummary`, the `rate` key is always populated in both formats.

`k6-compare` reads the pre-synthesized `rate` field; it does not re-derive it. This keeps comparison logic independent of format details.

**Guard:** If the baseline RPS is zero (either because the test ran for zero duration or because no HTTP requests were made), the RPS threshold comparison is skipped and a warning is emitted to stderr. This prevents spurious false-positive regressions from pathological inputs. A zero-vs-zero comparison reports `=` with no regression.

---

## 3. Regression Direction Semantics

Not all metrics regress in the same direction. `k6-compare` uses per-metric direction to determine what constitutes a regression.

The change percentage is computed as:

```
changePct = (current - baseline) / baseline * 100
```

Regression is then direction-aware:

| Metric | Direction | Regresses When |
|--------|-----------|----------------|
| `http_req_duration` (any percentile) | lower-is-better | `current > baseline` by more than threshold% |
| `http_req_failed` rate | lower-is-better | `current > baseline` by more than threshold% |
| `iteration_duration` | lower-is-better | `current > baseline` by more than threshold% |
| `http_reqs` rate (RPS) | higher-is-better | `current < baseline` by more than threshold% |
| custom Trend metrics | lower-is-better | `current > baseline` by more than threshold% |
| custom Rate metrics | lower-is-better | `current > baseline` by more than threshold% |
| custom Counter metrics (rate) | higher-is-better | `current < baseline` by more than threshold% |

**In the output table:** `▲` (red) indicates a regression (change in the wrong direction); `▼` (green) indicates an improvement; `=` indicates no measurable change.

**Status column:**

- `FAIL` — metric regressed beyond the threshold (counted toward exit code 3)
- `WARN` — metric regressed but no threshold was set (informational only)
- `PASS` — metric is within threshold
- `SKIP` — metric stat was absent from one or both summaries; not compared

---

## 4. Load-Model Heuristic and Its Limitation

k6's JSON summaries carry no executor configuration. There is no field recording whether the test used `constant-arrival-rate` (open model) or `constant-vus` (closed model). `k6-compare` uses a best-effort heuristic to detect model mismatches, because comparing an open-model run against a closed-model run can produce meaningless results — arrival-rate executors drop iterations under load, which changes latency distributions in ways unrelated to application performance.

**The heuristic:** If `dropped_iterations.count > 0` in a summary, the run is classified as *probably open-model* (arrival-rate executor). If `dropped_iterations` is absent or its count is zero, the run is *inconclusive* (unknown). The `classify()` function never returns *closed* — the heuristic can only detect positive evidence of an open model, not the absence of one.

**Mismatch detection:** A mismatch is flagged when both sides are conclusively classified AND they differ (one Open, one Closed). With the pure heuristic, both sides return either Open or Unknown, so a mismatch from the heuristic alone requires one side to be Open and the other Unknown — which the current implementation does not flag as a mismatch. Mismatch arises when `--assume-closed-model` is used: see below.

**Known limitation and false positives:** This heuristic has known false positives. The `per-vu-iterations` and `shared-iterations` executors (both closed-model) also emit `dropped_iterations` when their duration limit is reached. A closed-model run under time pressure may be misclassified as open-model, producing a spurious exit 5. Use `--assume-closed-model` to override the heuristic when you know both runs use a closed-model executor. Exact load-model detection (planned for v2 via `handleSummary` helpers) would require embedding executor config in the summary — see CMP-V2-01.

**Flag semantics and escape hatches:**

- `--assume-open-model` — Declares both runs as open-model and bypasses the heuristic entirely. Because the heuristic can never *prove* a run is closed, this declaration can never be positively contradicted. No exit 5 will be produced.

- `--assume-closed-model` — Declares both runs as closed-model. The heuristic is still consulted: if either run has `dropped_iterations.count > 0` (positive evidence of open model), the declaration is contradicted, `Mismatch` is set to `true`, and the tool exits 5 unless `--force` is also passed. This is the asymmetric case — a user declaring closed-model while the run's own data shows open-model behavior indicates a configuration mistake worth surfacing.

- `--force` — Downgrades a detected mismatch from exit 5 to a stderr warning and continues the comparison. This is useful when you understand the mismatch (e.g., a transitional CI run) and want the regression report regardless.

**Exit codes:** 0 = no regression; 3 = regression(s) detected; 5 = load-model mismatch without `--force`; 1 = parse/IO error.

---

## 5. Default Threshold Rationale

`k6-compare` applies default thresholds out of the box so it gates CI without requiring any configuration. The defaults are:

| Metric | Default Threshold | Rationale |
|--------|------------------|-----------|
| `http_req_duration` percentiles (p50/p90/p95/p99/p99.9) | +10% | A 10% p95 increase is empirically noticeable under real load. Values smaller than 10% generate noise in most environments because measurement variance alone can reach 3–5% between consecutive runs. |
| `iteration_duration` | +10% | Same reasoning as latency percentiles. |
| `http_req_failed` rate | +50% relative | Error rates are often near zero (e.g., 0.1%). A 50% relative change (0.1% → 0.15%) is large enough to be meaningful but small enough not to be hair-trigger. A fixed absolute threshold would be wrong for near-zero baselines. |
| `http_reqs` rate (RPS) | −10% | A 10% throughput drop is a reliable signal of a real bottleneck. Smaller values trigger on normal measurement noise in CI environments. |

**The `avg` statistic for `http_req_duration` is reported but not gated by default.** Average latency is sensitive to outliers and does not represent the tail experience; the percentile thresholds are more meaningful for SLO alignment.

**All defaults are overridable:**

```bash
# Override p95 threshold to 15%
k6-compare --threshold-p95 15 baseline.json current.json

# Override RPS threshold to 5%
k6-compare --threshold-rps 5 baseline.json current.json

# Set a threshold for custom metrics
k6-compare --threshold-default 20 baseline.json current.json

# Disable all defaults (report only — no gating)
k6-compare --no-default-thresholds baseline.json current.json
```

The `--no-default-thresholds` flag switches to report-only mode: the table and JSON are still produced, but exit code 3 is only triggered when an explicitly-set threshold flag is breached.

**Note on the `--threshold-p95 0` edge case:** The sentinel value `-1` is used internally to mean "use the built-in default." Passing `0` is treated as an unset flag (reverts to default) rather than "disable gating for p95." Use `--no-default-thresholds` to disable gating for all metrics.
