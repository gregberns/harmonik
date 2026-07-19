# Research Review — run-state-machine (M3)

> Independent-reviewer sub-agent (fresh context), signoffs waived (2026-07-14).
> Inputs: 02-components.md, decompose-review.md (4 carried obligations),
> all six 03-research/c*/findings.md. Reviewer spot-checked 8 load-bearing
> file:line claims against the working tree on phase1-session-restart-substrate.

## Verdict: APPROVE — advance to `change-design`

## Spot-checks (all exact against the tree)
- C2 FF-check `:6755`, non-CAS `update-ref` `:6821`, `mergeMu` `:384`.
- C5 `resumeReadyFallbackGrace = 2*time.Second` `:110`; resume branch `:250/:252`.
- C1 `ClockPort` (Now/Since/NewTicker/Sleep) clock.go:10.
- C3 `workLoopDeps` `:182-:942`, **exactly 85 fields** (942 = sandboxCfg).
- C4 keeper `Step` :234 / `Run` :251 / `stepCycle` :280; `substrate.Run[E,A]` seam.go:27.
- C6 workloop.go = 8184 lines. No claimed line was off.

## Per-component completeness (questions + evidence + patterns + risks)
- C1 PASS — 38+8 time-site census, named-timeout table w/ values, ClockPort/FakeClock API, D4/D11 template, four disjoint test seams named.
- C2 PASS — full LOCK/SPEC/FREE walkthrough (steps 0-7), explicit critical-section window, all mergeMu sites incl. both non-merge uses, worktreeCreateMu nesting, fairness.
- C3 PASS — exhaustive 85-field census (RUN/SG/MAINT/OTHER/DEAD), 8-port map, nil inventory, by-value analysis, 2 dead fields.
- C4 PASS — keeper template signatures, five genericization gaps, grounded state list, review-loop/DOT nesting, hclifecycle projection, M2 contract.
- C5 PASS — end-to-end resume mechanics + wedge point, 12-detector miss table, SR9 template, two-tier bound, event gaps, fault machinery.
- C6 PASS — four blocks + two merge-less closes, divergence table, exit-0 non-redundancy, shutdown-drain, emitDone census (45 sites), reopen/close census.

## Carried obligations
- (a) re-verify every file:line vs current tree — YES.
- (b) C2 critical-section via mergeRunBranchToMain walkthrough — YES (window = FF-check->update-ref->[fmt land]->push+rollback->restore->reset; build/rebase/fmt hoist out speculatively).
- (c) C3 workLoopDeps census fixing the cut line — YES (+2 dossier corrections, 2 dead-field deletions).
- (d) C5 grounded in real resume mechanics + detector miss-accounting — YES (root cause: daemon heartbeat != agent liveness).
- C4 M2 input/ack contract pinned — YES (dedicated section: input vocabulary, awaited-event ack, per-run monotonic seq, shell-owned backpressure; converges with M2-1 Submit(ctx,payload)(Ack,error)).

## Material gaps
None that block design. Each file's PLANNER-RECONCILE raises a scope-enlarging design
call (C1 five-file clock scope; C2 push-stays-in-window bounded by M4; C3 eight ports vs
six; C4 circular M3-4<->M2-1 handled self-contained; C5 heartbeat-semantics split as the
one sanctioned divergence) — escalations WITH the answer attached, not unresolved blockers.
Bar "no unresolved blocker prevents writing the change design" is met.
