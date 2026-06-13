# Examples

Ready-to-run artifacts that demonstrate `k6-compare`.

```
examples/
├── scripts/
│   ├── smoke.js          # closed model (constant-vus) + handleSummary -> summary.json
│   └── arrival-rate.js   # open model (constant-arrival-rate)
├── compare-demo/
│   ├── baseline.json     # a healthy k6 summary (reference)
│   └── current.json      # the same run with a deliberate regression injected
└── github-actions/
    ├── compare.yml       # CI: run k6 -> export summary -> gate with k6-compare
    └── README.md
```

## 1. Demonstrate `k6-compare` without running k6

The two summaries in `compare-demo/` are pre-built so the comparison demonstrates
itself. `current.json` carries an injected regression:

| Metric     | baseline | current  | change   |
|------------|----------|----------|----------|
| p99        | 61.30 ms | 78.51 ms | ▲ +28.1% |
| p99.9      | 148.0 ms | 205.3 ms | ▲ +38.7% |
| RPS        | 2973.85  | 2710.95  | ▼ -8.8%  |
| error rate | 0.12%    | 0.38%    | ▲ +217%  |

```bash
k6-compare examples/compare-demo/baseline.json examples/compare-demo/current.json \
  --threshold-p99 10 --threshold-rps 10 --threshold-error-rate 50
echo "exit: $?"   # expected: 3 (regressions detected)
```

Neither summary contains `dropped_iterations`, so the load-model heuristic
reports both as `unknown` and the comparison does **not** trigger exit 5. The
heuristic only ever *proves a run open* (when `dropped_iterations > 0`); it never
asserts closed. To see a load-model mismatch (exit 5), generate an open-model
summary with `arrival-rate.js` and compare it under a closed-model assumption —
the open evidence then contradicts the declaration:

```bash
k6 run examples/scripts/arrival-rate.js   # emits dropped_iterations -> detected open
k6-compare --assume-closed-model examples/compare-demo/baseline.json summary.json
echo "exit: $?"   # expected: 5 (load-model mismatch); add --force to downgrade to a warning
```

## 2. Generate real summaries with k6

```bash
# Closed model -> summary.json
k6 run examples/scripts/smoke.js

# Open model -> summary.json (overwrites; rename if you want to keep both)
k6 run examples/scripts/arrival-rate.js
```

Both scripts set `summaryTrendStats` with `p(99)` and `p(99.9)` — this is
**required**, because by default k6 omits those tail percentiles from the
summary and `k6-compare` cannot evaluate them.

## 3. Regression gate in CI

[`github-actions/compare.yml`](github-actions/compare.yml) runs on every PR: it
installs k6 and `k6-compare`, runs the smoke test, and compares against the
versioned baseline. If a regression is detected (exit 3), the job fails and
blocks the merge. See [github-actions/README.md](github-actions/README.md) for
details on the versioned-baseline workflow and threshold customization.
