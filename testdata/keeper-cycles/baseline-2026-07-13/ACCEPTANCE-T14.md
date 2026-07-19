# T14 — Acceptance Oracle Evidence Bundle

**Phase:** session-restart-substrate (Phase-1 code-revamp) · **Branch:** `phase1-session-restart-substrate`
**Run:** 2026-07-14, out-of-band (NO daemon, zero-token) on the merged branch HEAD after T0–T13 + the T11 decision.

The census Acceptance Oracle for the keeper session-restart vertical. All six conditions met and recorded.

## Reproduce
```
KEEPER_BASELINE="$PWD/.harmonik/events/baseline-2026-07-13/events.jsonl" bash scripts/keeper-metrics.sh
go test ./internal/codextest/... ./internal/codexreactor/... ./internal/codexdigitaltwin/... -count=1
go test ./internal/keepertest/ -run TestL1 -count=1
```

## Conditions

| # | Condition | Result | Evidence |
|---|-----------|--------|----------|
| 1 | N=10 consecutive green `go test` over keeper + keepertwin + keepertest | **10/10 green (73s)** | `scripts/keeper-oracle-n10.sh` (invoked by `keeper-metrics.sh`) |
| 2 | Fault matrix 100% (SR9 terminal-never-silence) | **100%** (44 cells) | `TestKeeperReplay_FaultMatrix` (T12) |
| 3 | Out-of-band jq/stat metric recompute — all 9 anchors held | **ALL MATCH** | `scripts/keeper-metrics.sh` exit 0 |
| 4 | Coverage floor on the new reactor files met | **held** (step.go 92.1% / shell.go 87.4% / ports.go 92.3%) | `scripts/keeper-coverage-gate.sh` vs `keeper-coverage-floor.baseline` |
| 5 | Codex L0–L2 still green + `git diff` shows codextest untouched (Goal-2) | **green; ZERO `_test.go` changes** on the branch | `go test ./internal/codextest/...`; `git diff --stat main...HEAD -- '**codextest/**_test.go'` empty |
| 6 | Differential green before scaffold deletion (00b R6) | **L1 green** (option B — no live scaffold; see below) | `TestL1_*` green; synthesizer table frozen (`internal/keepertwin/synthesizer.go`) |

## Baseline characterized, not regressed (the headline numbers)
- **Restart-completion:** 427/507 = **84.2%** (frozen) → 428/507 = 84.4% (new reactor, SR9-shifted +1) — did NOT drop.
- **Degraded-completion:** 347/427 = **81.3%** (frozen, the headline number to drive DOWN) → 348/428 = 81.3% (SR9-shifted) — did NOT rise. Improvement deliberately NOT asserted (this phase characterizes the baseline; SR4/model-done is the mechanism, driving it down is later work).
- **Unterminated cycles (SR9 wedge):** 1 (`kk-test|cyc-20260610T215853-000004`) → **0**. FIXED by the new reactor terminating within bound.
- SR3/SR4/SR6/SR7/SR9 invariant violations on the replayed stream: **0** (jq + `internal/replay` checkers).

## T11 note (00b R6 under the D13-scaffold-obsolete decision)
D13's live old-vs-new differential is obsolete: T7 deleted the old blocking `Cycler` (`runCycle`), not
cleanly resurrectable, and landed with the full ~55-file keeper suite green (the regression catch the
scaffold existed to provide). Per measurement-design §4's own framing (L1 golden-vs-baseline is the
"permanent net"), R6's synthesizer-table freeze binds to the green L1 corpus test + the T13 no-regress
metrics. There is no live scaffold to delete. Decision surfaced to the operator 2026-07-13; recorded in
commit `docs(keeper): T11 — freeze synthesizer table per 00b R6 (option B)`. Reversible — the literal
option-A live differential can be added as a frozen old-Cycler scaffold later if desired.

## Verdict
**ACCEPTED.** All six oracle conditions met; the baseline is characterized and not regressed; the
SR4 headline invariant (`/clear` never before model-done) is implemented and structurally enforced; the
1 unterminated wedge is fixed. The codex re-instantiation (Goal-2) holds with zero test-file changes.
