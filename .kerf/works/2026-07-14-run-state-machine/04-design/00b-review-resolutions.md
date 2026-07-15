# 04-Design / 00b — Change-design self-review resolutions

> Signoffs waived; adversarial self-review 2026-07-14 (also saved as
> `change-design-review.md` verdict). Items an independent reviewer should
> re-challenge are marked ⚑.

**R1 — Run-machine gap (FIXED in-place):** the single-mode P17 guards (escape
check hk-zguy6, no-commit guard) had no home between Dispatching and Gating.
Added a `Guarding` state + `EvGuardsPassed`/`EvEscapeDetected`/
`EvNoCommitGuardReopen` rows; the escape check runs inside the mergeq exclusion
domain per c2-merge-queue-design §4.1. Review-loop/DOT paths do NOT pass Guarding
(parity: today only the single-mode path runs those guards, RF P17 — the guards
sit after the mode fork's early returns).

**R2 — ⚑ push-inside-the-queue deviation (M3-D5):** deliberately deviates from
problem-space §2's literal "push … outside any global daemon lock". Grounds:
rollback pairing (MF §2) + hk-zguy6. Carried to the final report as a
planner-visible FYI. The DoD-2 mechanical check is correspondingly restated
(no BUILD-class command in the critical section).

**R3 — ⚑ two machines (M3-D2):** the strongest architectural challenge point.
Defense recorded in c4-runexec-reactor-design §7.1; the fallback (one machine, dispatch as
sub-phase) remains implementable over the same vocabulary if the reviewer
overrules — the Event/Action contract (incl. M3-D11) is machine-count-agnostic.

**R4 — frozen watchdog (M3-D3):** leaves the working-segment bound at 90m.
Consistent with reproduce-behavior-first (P1 D11 precedent). The C5 headline
bound (ready-or-fail ≤ agentReadyTimeout) does not depend on it.

**R5 — census-claim correction (LF §3):** the spec motivation must state the
true current behavior (three uncoordinated bounds; correlated-event silence in
the gap), not "hangs forever". Folded into c5-resume-liveness-design §5 and the
spec draft §1.

**R6 — replay run-keyed extension:** touches `internal/replay` (P1's package).
Additive only (new file + run-state track); no change to cycle-keyed checkers or
their goldens. If P1's owner objects, the run track can live in a sibling
package importing replay's registry surface — noted as the fallback.

**R7 — zero-new-events (M3-D10) double-checked** against every design doc: the
queue logs via slog; C5 reuses agent_ready_timeout/run_stale/terminals; the two
joinability fixes add run_id to EXISTING emissions. `specs/event-model.md`
untouched. ✓

**R8 — TC-6 allow-list trim:** runexec imports only `$gostd` + core + substrate
(TC-6's provisional eventbus/queue edges dropped — the pure machines reference
neither). mergeq imports `$gostd` (+ slog) only. Recorded for the depguard task.

**Verdict: APPROVE — advance to spec-draft.** Criteria: every 02-components
open question is either settled with a cited decision (C1 a–c → M3-D4/ports §4;
C2 a–e → M3-D5/merge-queue §1–§4; C3 a–d → M3-D6/ports §1–§2; C4 a–e →
M3-D2/D3/D9/D11 + runexec §3–§5; C5 a–d → M3-D7/liveness §1; C6 a–c →
M3-D8/D9) or explicitly deferred with rationale (watchdog dissolution, DOT full
reactorization, Region A re-home, M5 remainder). No pin contradicts a locked
decision; the seam is reused not forked; parity constraints preserved.
