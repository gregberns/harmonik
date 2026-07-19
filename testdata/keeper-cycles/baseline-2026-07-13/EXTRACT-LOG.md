# Keeper corpus extraction ledger

- **Source:** frozen baseline `.harmonik/events/baseline-2026-07-13/events.jsonl` (read-only)
- **Baseline date:** 2026-07-13
- **Extractor:** `scripts/extract-keeper-corpus.py` (sha256 `1b8ea0abd93e5c405d7d6a305f1062a4ab375f3cf760416eb3faf27f22c1a493`)
- **Join key:** composite `(agent_name, cycle_id)` (D7); events EventID-sorted (D9)
- **Granularity:** boundary (pre-EV-U1 interior events); re-run after a new
  baseline capture to lift to interior granularity (design §1.5)

## Counts

```json
{
  "abort_reasons": {
    "handoff_timeout": 79
  },
  "aborted": 79,
  "clear_unconfirmed": 347,
  "complete": 427,
  "count": 507,
  "started": 507,
  "unterminated": 1
}
```
