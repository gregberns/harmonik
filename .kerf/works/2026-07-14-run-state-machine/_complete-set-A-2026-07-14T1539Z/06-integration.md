# 06 — Integration (Pass 6): corpus cross-reference, contradiction & terminology check

> Verifies the Pass-5 draft (`run-state-machine.md` / RX-001..020,
> RX-INV-001..005) plugs into the live `specs/*.md` corpus. Every check
> grep/read-verified against the working tree 2026-07-14. Mirrors the P1
> integration format.

## 1. Cross-reference checks

| # | Link | Claim | Verified against | Verdict |
|---|---|---|---|---|
| X1 | RX-004 → replay-substrate RS-001/RS-002 | seam + free-function Run is the composition boundary | `replay-substrate.md:72` RS-001, `:78` RS-002 | ✅ |
| X2 | RX-007/RX-017 → RS-015 ClockPort | ClockPort required by reference, never redefined | `replay-substrate.md:175` RS-015 (+`clock.go:10` impl) | ✅ single owner |
| X3 | §6 L2 fault matrix → RS-012 | four vertical-neutral fault modes + never-silence | `replay-substrate.md:146` RS-012 (+ RS-INV-003 at `:37`) | ✅ |
| X4 | §6 tiers → RS-017..020 | L0–L3 taxonomy + measurement standard | `replay-substrate.md:41` | ✅ |
| X5 | RX-INV-003 → session-keeper SK-INV-005/SK-015 | the template invariant + its bounded window | `session-keeper.md:264–:266`, `:191` | ✅ wording transposed, structure identical |
| X6 | RX-001/RX-007 → SK-009/SK-010 | pure-Step + timers-as-events lineage | `session-keeper.md:134`, `:140` | ✅ informative, no reverse dep |
| X7 | RX-015 → execution-model EM-053/EM-054/EM-INV-005 | reopen path, working-tree refresh, rollback backstop preserved in the commit phase | `execution-model.md:944` EM-053, `:963` EM-054, EM-INV-005 cited at merge rollback comments (`workloop.go:6902–:6905`) | ✅ |
| X8 | RX-020 → handler-contract HC-065 | lifecycle FSM ownership unmoved | `handler-contract.md:957` HC-065 transition table | ✅ |
| X9 | LaunchPort → process-lifecycle PL-021b | substrate spawn seam wrapped, not rebuilt | `process-lifecycle.md:728` PL-021b | ✅ |
| X10 | RX-018 → event-model | `agent_ready_timeout` / `run_stale` exist registered; NO new types needed | `internal/core/eventtype.go:282` (agent_ready_timeout), `:872` (run_stale, §8.12.1); both in the registry cohort | ✅ — note: `agent_ready_timeout` is code-registered; its event-model §8 section anchor is thin (see C3) |
| X11 | RX prefix collision | RX unused anywhere in specs/ | `grep -rn "RX-" specs/*.md` → zero hits; `_registry.yaml` has no RX | ✅ free |
| X12 | RX-012 member (b) → hk-zguy6 test net | escape-check suite must stay green unmodified | `internal/daemon/escapedetect_hkooexj_test.go:252–:315,:374–:497` exists | ✅ carried into tasks |
| X13 | depguard pre-authorization | the runexec rule shape is pre-agreed | `plans/2026-07-13-code-revamp/track-c-enforcement.md` §2.2 (rule block) + TASKS.md TC-6 | ✅ mergeq rule is NEW (same pattern) — flagged in §2 |
| X14 | M2 edge | RX-009..011 is the M3-4→M2-1 contract ROADMAP names | `ROADMAP.md` phase-map note ("the only M3→M2 dependency is M3-4 → M2-1"); M2 decompose C1 open questions (ack shape, sync-vs-event, SR9 analog) are each answered by RX-009/RX-011/RX-INV-001 | ✅ M2's three C1 open questions all have a normative answer to consume |

**Tally: 14 checks — ✅ 14, ⚠️ 0, ❌ 0.**

## 2. Required / recommended integration edits

1. **REQUIRED at finalize:** `specs/_registry.yaml` gains
   `RX: {spec-id: run-state-machine, reserved: <date>, status: draft}` in the
   same commit as the spec (registry lint).
2. **RECOMMENDED (P1 R5 pattern, non-blocking):** one-line reciprocal pointers —
   `replay-substrate.md` §9: "run-state-machine.md (RX) — the daemon run
   vertical, third instantiation of the seam"; `session-keeper.md` §9:
   "RX-INV-003 is the daemon peer of SK-INV-005". Pure additive.
3. **REQUIRED alongside code:** `.golangci.yml` gains the TC-6 runexec rule
   (allow trimmed to `$gostd`+core+substrate+self per 00b R8) AND a new
   parallel `mergeq` rule (`deny: internal/daemon`) — the mergeq rule was NOT in
   TC-6's pre-authorization and is called out as new (same "applied per carve"
   pattern track-c §2.2 prescribes). `coverage.baseline` seeds entries for both
   packages at measured value (TC-6).

## 3. Contradictions checked

**C1 — RX-015 vs problem-space/census DoD wording ("push outside any lock").**
Resolved-with-supersession: RX-015 states the deviation and its grounds
(rollback pairing MF §2; hk-zguy6 window) IN the normative text; 05-changelog
records it; the final report flags it to the planner. Not silent drift.

**C2 — Two normative state machines in one daemon (RX-005/006 vs HC-064/065).**
No conflict: HC-064/065 is the per-SESSION lifecycle FSM
(Spawning…Terminated) owned by handler-contract; RX machines are the per-RUN /
per-DISPATCH orchestration layer that DRIVES it via the terminal action
(RX-020 states the projection relationship explicitly). Terminology kept
distinct: "lifecycle machine" (HC) vs "run/dispatch machine" (RX).

**C3 — event-model thin anchor for `agent_ready_timeout`.** The type is
registered in code (`eventtype.go:282`) and RX-018 reuses it; the event-model
spec's §8 section for it is not explicitly numbered in the grep. NOT a blocker
(RX registers nothing); noted so a future EV tidy can add the section anchor.
No RX text change needed.

**C4 — stall-sentinel overlap (reconciliation.md §4a).** RX-INV-003 deliberately
reuses `run_stale`/failure-class vocabulary rather than defining new stall
semantics; the stall-sentinel work (status `tasks`) owns stall signatures. The
spec's §2.2 out-of-scope line covers this. No conflict.

**C5 — reap overlap (reconciliation.md §4b).** The orphan-run/boot-reconcile
recovery side stays with `reap`; RX-006's shutdown-drain edge + QM-002a
dispatched-item recovery semantics are unchanged (the wrapper still leaves
items `dispatched` on ctx-cancel, RF §1). No conflict.

**C6 — "queue" terminology.** Three senses now live in the corpus: work queues
(queue-model QM), the keeper's none, and the NEW merge queue. The spec always
says "merge queue"/"exclusion domain" and never bare "queue"; `internal/mergeq`
avoids the `internal/queue` package namespace. Documented-acceptable.

## 4. Terminology consistency

- **reactor / Step / machine** — matches RS glossary ("reactor Step",
  vertical-owned) and SK usage; RX uses "machine" for the two cores. ✅
- **port** — RS-004's "consumer-owned narrow interface" sense; RX-016 uses it
  identically. ✅
- **dispatch** — RX defines it (glossary §3) distinctly from harmonik-dispatch
  (the CLI skill) and queue dispatch; the spec's usage is internally uniform. ✅
- **critical section / exclusion domain** — defined §3, used consistently in
  RX-012..015 and mergeq design. ✅
- **resume** — anchored to the `implementer_resumed` event boundary. ✅

## 5. Changelog accuracy

`05-changelog.md` verified: 20 requirements + 5 invariants matches the draft
count (grep: RX-001..RX-020 present, contiguous; RX-INV-001..005); "zero
event-model amendment" consistent with RX-018 and every design doc (00b R7);
landing order (registry same-commit; RS/SK already landed) verified —
`_registry.yaml:34–:35` shows RS + SK reserved, both spec files exist.

## 6. Verdict

**PASS.** No contradictions; one supersession explicitly recorded (C1); three
integration edits carried into Tasks (registry reservation; optional reciprocal
pointers; depguard + coverage-baseline edits incl. the NEW mergeq rule).
