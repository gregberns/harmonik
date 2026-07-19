# Daemon run corpus extraction ledger

- **Source:** frozen baseline `testdata/daemon-runs/baseline-2026-07-14/source/events.jsonl` (read-only)
- **Baseline date:** 2026-07-14
- **Extractor:** `scripts/extract-run-corpus.py` (sha256 `39d61b2f0bf6b6d1738794606977669d2b7429ebc732489bb99ca4344d770e56`)
- **Join key:** payload `run_id` (run-lifecycle track); events EventID-sorted (D9)
- **Strata:** single | review-loop-resume | dot | merge-failure | run-stale | hung-relaunch

## Counts

```json
{
  "count": 6,
  "failure_class": 2,
  "resumed": 2,
  "strata": {
    "dot": 1,
    "hung-relaunch": 1,
    "merge-failure": 1,
    "review-loop-resume": 1,
    "run-stale": 1,
    "single": 1
  },
  "terminals": {
    "agent_ready_timeout": 1,
    "review_loop_cycle_complete": 1,
    "run_completed": 2,
    "run_failed": 1,
    "run_stale": 1
  },
  "terminated": 4,
  "unterminated": 0
}
```
