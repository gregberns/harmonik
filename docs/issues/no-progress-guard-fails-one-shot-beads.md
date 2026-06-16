# No-progress guard fails correct one-shot-complete beads

## Summary
In DOT-cascade review mode, the HEAD-advancement no-progress guard in `internal/daemon/dot_cascade.go` (the `hk-togxq` check, roughly lines 338–411) fires at review iteration ≥ 2 whenever HEAD has not advanced since the prior iteration. This is correct for genuinely-stalled rework, but it structurally fails beads that are complete and correct in a single iteration, stranding finished, tested work and failing the run.

## Mechanism
1. The implementer commits complete, correct work at iteration 1.
2. The reviewer returns `REQUEST_CHANGES` carrying only advisory/nitpick feedback (nothing requiring a code change), or the work is genuinely already done.
3. At iteration 2 the implementer has nothing committable to add, so HEAD does not advance.
4. The no-progress check (iter ≥ 2 + HEAD unchanged) fires → `review_fixup_stalled` / `no_progress_detected` → the run FAILS, discarding completed work.

The existing completion exemption (`hk-8ps7q`, dot_cascade.go ~line 402: "approved-and-done is COMPLETION, not no-progress") only covers the path where the reviewer returns APPROVE with HEAD unchanged. It does NOT cover REQUEST_CHANGES with only advisory feedback on already-complete work — that still trips the guard.

## Evidence (omatic-system-plan project, 2026-06-16)
Confirmed across two languages, with and without tests (so the wedge is language- and test-agnostic):
- hk-pdn.3 (C#, CosmosDB shim): iter-1 commit `20ae1cff` complete + self-verified; failed no-progress at iter 2. Hand-salvaged to main (`143a1b2`).
- hk-pdn.15 (C#, Base.Shim): iter-1 commit `412e4420` complete WITH 17 passing xUnit tests; failed no-progress at iter 2 despite the tests.
- hk-pdn.18 (Go, twin contracts): iter-1 commit `12cba9f4` complete WITH 8 passing Go tests; failed no-progress at iter 2.
- hk-0z6.1/.2/.3/.4/.5/.6 (Markdown doc/design beads): each committed a complete artifact at iter 1, each killed at iter 2 when the reviewer returned advisory REQUEST_CHANGES with nothing committable to add. All six hand-salvaged to main.

Adding green unit tests to the C# and Go beads did NOT bypass the guard.

## Impact
Every "one-shot-completable" bead (most build, vendor, compile/grep, and doc/design work) is at risk. It blocks an entire lane from flowing through the daemon: the work completes correctly but the run is marked failed and the bead never closes, so downstream dependents never unblock. Operators are forced to hand-salvage (cherry-pick) completed commits to the target branch outside the gate.

## Proposed fix
1. Make the no-progress guard configurable / cappable per workflow (around dot_cascade.go:401). A code workflow keeps the strict guard (for code, REQUEST_CHANGES is usually real, committable rework); a doc/design or vendor workflow can disable or cap it.
2. Per-bead workflow (.dot-file) selection by label, so doc/design beads route to a doc-safe workflow and code beads keep the strict guard. (harmonik already resolves a `workflow:<mode>` label at claim time but does not select a per-bead .dot file.)
3. Broaden the completion exemption so "reviewer feedback is advisory-only AND the gate already passes" is treated as completion, not no-progress — not only the bare APPROVE path.

## Interim mitigations in use
- Hand-salvage: cherry-pick the complete iteration-1 commit to the target branch, verify green, leave the failed bead for the daemon/operator to close.
- A separate doc-safe DOT workflow that collapses the advisory review loop (APPROVE → close; advisory nitpicks non-blocking).
- Mandating minimal unit tests on code beads did NOT work (see Evidence) — a tested-but-complete bead still trips the guard.
