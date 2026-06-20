# R8 — hk-orni review: review-gate catches DOT short-circuit

**Commit reviewed:** `ef808a9f` on `worktree-agent-abf982a194ed6bd37` (single-commit review, main is moving).
**Files:** `scripts/ops-monitor-check.sh` + `test/exploratory/ops_monitor_check_test.sh` only — no STARTUP/mirror/Go/keeper/comms. `bash -n` clean on both.
**Verdict: APPROVE.**

## 1. Prefix-match precision — PASS
Match reads `_payload.get('node_id')` (correct location, per implementer) and gates on `_nid.startswith('review')`. Full-file scan of live `events.jsonl` (15 MB) of every distinct `node_dispatch_requested` node_id:
`{close, close-needs-attention, commit_gate, consolidate, finalize, implement, implementer, review, review_correctness, review_design, review_tests, reviewer, start}`.
Exactly five start with "review" — all genuine review-family nodes; **zero over-match suspects**. No non-review node_id begins with "review", so no anchoring is needed in practice. Captures the real review nodes; does not over-match.

## 2. R6 suppression PRESERVED — PASS
The change is a strict UNION (`_review_anchor_ts` seeded from `reviewer_launched_ts` then `review_requested_ts`); auto-close / noChange / subsumed runs appear in neither map, so they remain unflagged. Test 12b (auto-close + noChange, no reviewer) → review-gate=ok, no comms: **green**. No re-introduction of the 181-false-positive class.

## 3. Multi-iteration DOT clearing intact — PASS
The bypass loop joins each anchored run_id against `verdict_run_ids` by run_id (`if _rid not in verdict_run_ids`), identical to the launched-only path. A verdict on any iteration clears the run. Test 12d (review requested + verdict) → ok, confirms.

## Suite / scope
Full bash suite: **99 passed, 0 failed** (matches commit claim). New Test 12c (short-circuit true-positive → IMMEDIATE review-bypass) and 12d both green; existing launched-only true-positive + 12b suppression stay green.

No defects. APPROVE.
