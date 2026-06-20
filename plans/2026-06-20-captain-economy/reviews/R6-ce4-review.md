# R6 — CE4 review (hk-ayvx): offload captain /loop 12m deterministic checks to ops-monitor

**Commit:** f666ceee  •  **Base:** predates CE5/CE6 — reviewed per-file from the bead commit, not the two-dot range.
**Verdict: REQUEST_CHANGES** — one BLOCKING correctness defect in the review-gate join (false-positive storm that degrades into an effective false-negative via alert fatigue).

Files changed (scope confirmed clean): `scripts/ops-monitor-check.sh` (+146), `.claude/skills/captain/STARTUP.md` (+13), `cmd/harmonik/assets/skills/captain/STARTUP.md` (+13, byte-identical mirror — both blobs sha `349d0525`), `test/exploratory/ops_monitor_check_test.sh` (+183). **Zero .go files.**

---

## 1. Review-gate correctness (MOST IMPORTANT) — BLOCKING

The join mechanics are individually correct:
- Field locations verified against the live log: `run_completed` and `reviewer_verdict` both carry `run_id` at top level AND in `payload` (identical values). The script's `_payload.get('run_id') or _ev.get('run_id')` (and the reverse for verdict) resolve correctly. `timestamp_wall` top-level parse is correct.
- (b) 180s grace correctly skips in-flight runs (`ts_epoch - _cts < grace → continue`). Verified by Test 12.
- (c) The broken top-level `workflow_mode` grep is gone — replaced by the run_id↔verdict join. Confirmed.
- (d) Malformed lines: per-line `try/except: pass` + payload-string re-parse guarded — a partial tail line can't crash the pass. OK.

**BLOCKING defect — the join over-flags (`scripts/ops-monitor-check.sh:286-296`, severity HIGH).** The check flags ANY `run_completed` run_id lacking a `reviewer_verdict` as `review-bypass`, with NO filter on workflow type. But the daemon has a legitimate non-reviewed close path: the MVH twin-blind **`auto-close: exit=0`** branch (`internal/daemon/workloop.go:3811`) merges to main and closes the bead with **no reviewer at all** — by design, not a bypass. Evidence from the live `.harmonik/events/events.jsonl`:
- 681 `run_completed` run_ids; 489 have a matching verdict; **192 do not.**
- Of the 192 unmatched: **180 are `auto-close: exit=0`**, plus `noChange-subsumed`, `pre-dispatch-subsumed`, `agent_completed` — all legitimately review-less.
- Simulating the exact script logic on the live log's last-10 window flags **2 run_ids** as `review-bypass` (both `noChange-subsumed: bead found in main`).

So on normal operation the script fires **IMMEDIATE** `review-bypass` comms alerts to the operator for runs that never had a review gate. This is a false-positive generator. Per build-practices the danger is second-order: a signal that cries wolf on every legacy/no-change close trains the operator to ignore `review-bypass`, which then masks a REAL DOT/review-loop bypass → **effective false-negative on the load-bearing safety check.** That is exactly the failure mode this bead exists to prevent.

Caveat — partly a spec defect: I4-busywork.md:41/92 specifies the check as "per `run_completed.run_id`, assert a matching `reviewer_verdict` exists" with no workflow carve-out, so the implementer followed the written spec. But the live-log evidence was available, and the check as built is provably wrong against real data.

**Sub-finding (MEDIUM):** 3 unmatched run_completeds have summary `dot: reached terminal node "close"`. A DOT run reaching terminal with no same-run_id verdict is either a real bypass OR the verdict landed under a different run_id than the terminal run_completed. The join assumes verdict.run_id == terminal run_completed.run_id; this should be confirmed for multi-iteration DOT cascades before the check is trusted as authoritative.

**Required fix:** gate the bypass flag to runs that were actually supposed to be reviewed — e.g. only flag run_ids whose run also emitted a reviewer node event (`reviewer_launched`) but no `reviewer_verdict`, or exclude the auto-close/subsumed terminal summaries. A run with no reviewer node was never gated and cannot be "bypassed."

---

## 2. No dropped captain responsibility — PASS

Old 8 checks → new mapping, cross-checked line by line in STARTUP.md:
- (1) daemon-up → `daemon-up` flag → rebuild+restart. (2) paused-queues → `paused-queues` flag → resume. (3) crew-fresh → `crew-fresh` flag → capture-pane/reconcile. (4) drain comms → kept as step (B), still captain's. (5) backlog-pull → `backlog-ready` flag → STAFF (decision explicitly retained on captain). (6) lull-deploy → `lull` flag → deploy+verify. (7) quality-check → `review-gate` flag → escalate to operator. (8) self-audit/stalled-initiative → kept as step (D), still captain's.
All four JUDGMENT slices (staff, lull-deploy, review-bypass escalate, stalled-initiative) remain the captain's and are flag-triggered. No deterministic check silently dropped. Stale/missing latest.json falls back to manual checks (1)-(3). Good.

## 3. latest.json schema bump 1→2 — PASS

Additive only: existing fields retained, new `ready_count`/`backlog_ready`/`review_bypass_run_ids`/`checks` added. No programmatic consumer keys on `schema_version` — `latest.json` is read only by the Opus captain (human-read jq in STARTUP.md). The unrelated `.harmonik/crew/missions/ops-monitor.md` frontmatter `schema_version:1` is a different schema. No consumer breaks.

## 4. Pre-existing-failure claim — VERIFIED TRUE

Zero .go files in f666ceee (confirmed). `TestNoProgress_ApprovedAndDone_Completes_hk8ps7q` lives in `internal/daemon/dot_approved_done_completes_hk8ps7q_test.go` — a DOT no-progress-detector regression test, unrelated to the ops-monitor bash. Ran it in this worktree (no Go changes): it FAILS (`no-progress detected at iteration 2`). Since CE4 touched no Go, the failure is definitionally not a CE4 regression — it is a genuine pre-existing DOT-engine regression that should be filed separately.

## 5. Scope + sync — PASS (one nit)

Only the four declared files changed. STARTUP.md mirror byte-identical. bash -n clean; embed-sync go test green; bash suite 0 failures.
NIT: commit claims "82/82 green"; the test runner actually tallies **50 passed / 0 failed** across 9 test cases. Cosmetic overstatement, not a defect.

---

## Defect summary

| # | File:line | Severity | Issue |
|---|---|---|---|
| 1 | scripts/ops-monitor-check.sh:286-296 | **BLOCKING** | review-gate flags every non-reviewed close (auto-close/noChange/subsumed) as `review-bypass`; 180/192 live unmatched runs are legitimate → IMMEDIATE-alert storm → alert fatigue masks real bypass |
| 2 | scripts/ops-monitor-check.sh:286-296 | MEDIUM | join assumes verdict.run_id == terminal run_completed.run_id; unconfirmed for multi-iteration DOT cascades (3 live `dot: reached terminal node close` unmatched) |
| 3 | test/exploratory/ops_monitor_check_test.sh:479 | MEDIUM | Test 10 enshrines the over-flag behavior; no test for auto-close/noChange suppression |
| 4 | commit message | NIT | "82/82" vs actual 50/0 |

**Verdict: REQUEST_CHANGES.** The deterministic-offload mechanics, schema bump, captain-tick rewrite, mirror sync, and pre-existing-failure attribution are all sound. The single blocking issue is that the load-bearing review-gate check over-fires on the daemon's legitimate non-reviewed close paths — fix the workflow-type gate before merge.
