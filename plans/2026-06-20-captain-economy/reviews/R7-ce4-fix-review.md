# R7 — CE4 fix review (hk-ayvx): gate review-bypass on reviewer_launched

**Commits reviewed:** `799053cc` (CE4 offload, cherry-pick) + `ed2fcb8b` (the R6 fix). Branch `worktree-agent-aca396cbefef02e18`.
**Verdict: APPROVE-WITH-LIMITATION** — the fix correctly kills the 193 false positives and preserves the launched-but-no-verdict true positive; it introduces a *new, bounded* false-negative (short-circuited reviewers) that is strictly no worse than the prior state and should be captured as a follow-up bead, not block the merge.

---

## Scope — PASS
Against current `main` (`4349f061`, advanced past the stated 09c112c5) the branch carries **exactly 2 commits**. `ed2fcb8b` touches **only** `scripts/ops-monitor-check.sh` + `test/exploratory/ops_monitor_check_test.sh` (the earlier keeper/core files in a `09c112c5..` diff are below the CE4 work and already on main — not this branch). `bash -n` clean on both fix-commit files. No scope creep.

## 1. Suppression correct — PASS (empirically)
On the live 15 MB `events.jsonl`: 682 `run_completed`, **215 have no `reviewer_launched`** — 181 `auto-close`, 5 noChange/subsumed, 4 agent_completed, 25 dot-terminal-close. The new join (`reviewer_launched ∖ reviewer_verdict`) excludes all 215 by construction. Test 12b (`r-autoclose` + `r-nochange`, no reviewer) → `review-gate=ok`, no comms. Confirmed passing.

## 2. True-positive preserved — PASS
Live data: **22 run_ids emitted `reviewer_launched` but no `reviewer_verdict`** → exactly the flaggable bypass class. Test 10 (`reviewer_launched`, no verdict, past 180s grace) → IMMEDIATE `review-bypass` signal + comms. Confirmed passing.

## 3. THE ADVERSARIAL CONCERN — new false-negative is REAL but bounded
**Yes, the launched-gate misses genuine short-circuits.** Traced 3 live runs (`019e6d6e`/hk-3yz2d, `019e6d7c`/hk-ha6z1, `019e6f78`/hk-3wbff): each emitted `node_dispatch_requested node_id=reviewer` (or `review_correctness`/`review_design`), then jumped straight to `close` and `bead_closed` with **NO `reviewer_launched` and NO `reviewer_verdict`**. That is precisely the hk-2vpj "engine short-circuited reviewers" class — and the new check cannot see it, because there is no launch event to anchor on.

Crucially, **a distinguishing signal DOES exist in events.jsonl**: `node_dispatch_requested node_id=review*`. Of the 181 legitimate auto-closes, **0** request a review node; **8** run_ids request a review node yet emit neither launch nor verdict (the short-circuit set). So "legitimately review-less" vs "should-have-been-reviewed-but-skipped" is mechanically separable — the fix simply doesn't use that signal.

**Verdict on the concern: (b) acceptable documented limitation, NOT a blocking regression.** Reasoning:
- **Strictly better than the prior state.** R6 established the old check was an effective false-negative via alert-fatigue (193 false IMMEDIATE alerts burying the real one). The fix turns that into 0 false positives + 22 genuine catchable bypasses. The short-circuit case was *also missed before* (it was drowned in the storm), so this is a net improvement on every axis.
- **The ops-monitor is a heuristic captain-tick signal, not the enforcement boundary.** The DOT engine is the real review gate; hk-2vpj-class short-circuits are an engine defect to be fixed in the engine (per the documented "count reviewer_verdict per run_id" reference), not papered over by a bash monitor.
- The miss is bounded (8 historical run_ids) and detectable later by the recommended follow-up.

## Tests / quality — PASS
Actual fix suite (run in an isolated `ed2fcb8b` worktree): **87 passed, 0 failed**, matching the commit. (Note: the review worktree itself is checked out on `main`, so a naive `bash test/...` there runs the OLD suite — I verified against the real fix blobs.) Commit honestly corrects the prior "82/82" mis-tally. R6 NIT resolved.

---

## Required documentation + follow-up
The MEDIUM comment in the script documents the multi-iteration-DOT join but **does not name the short-circuit-no-launch blind spot**. Recommend:
1. **File a follow-up bead** (P2): extend review-gate to also flag run_ids with `node_dispatch_requested node_id=review*` but no `reviewer_verdict` — the empirically-validated short-circuit signal (8 live run_ids, 0 auto-close false positives). This closes the hk-2vpj-class gap in the monitor.
2. One-line script-comment addition noting the launched-gate does not catch reviewers short-circuited before launch (engine-level concern; see the follow-up bead).

Neither blocks merge. **APPROVE-WITH-LIMITATION.**
