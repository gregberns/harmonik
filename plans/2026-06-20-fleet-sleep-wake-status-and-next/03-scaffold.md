# Fleet state model + ZFC wind-down — scaffold & locked decisions

**Date:** 2026-06-20 · **Codename:** `fleet-state` · **Supersedes:** the deferred "policy layer" of epic `hk-rl4b` (mechanism from hk-rl4b is kept; its auto-park *decision* is removed).
**Reads with:** `./01-...`/`README.md` (status quo) and `./02-zfc-redesign-synthesis.md` (the analysis). This doc is the actionable scaffold.

---

## Locked decisions (operator, 2026-06-20)

1. **Demote the oracle — YES.** Keep its facts as a tool; delete the auto-park decision. (Fork 1 ✓)
2. **No auto-drain.** Go NEVER initiates sleep on its own — not even on mechanical drain. The captain (LLM) always initiates; Go validates + executes. (Fork 2 ✓)
3. **Checker = context-into-state, not a constant agent.** Don't build a constantly-polling checker. Instead **capture context size INTO system state** (per-session AND per-subagent), so agents can read: (a) context too big, (b) context not changing (stuck), (c) repeating-pattern detection on recent messages (cheap Haiku pass = "same thing over and over" loop detection). The existing ctx-watchdog (~30-min, context-focused) stays but should *read state* rather than eyeball. The sentinel flywheel governor is **not** relied on (unused, influence TBD). (Fork 3 ✓)
4. **Defer the full reconcile loop** (DESIRED-state controller) — Phase 4, on hold. (Fork 4 ✓)
5. **Drop "quiesce"** from all operator-facing vocabulary and docs — the operator does not want to remember the term. Internal `QuiesceArbiter`/`quiesce.go` rename is optional low-pri cleanup (with auto-park gone it's just the sleep/wake plumbing — candidate name: `WindDownCoordinator`). (Fork 5 ✓)

**Vocabulary (no "quiesce"):** **SLEEP** (operator, end of day) · **PARK** (interrupt-and-hold) · **STOP** (one crew, killed) · **TEARDOWN** (irreversible) · resting state is just **"asleep"/"at rest."**

**Standing emphasis from operator:** (a) *really* nail the system model — run a deliberate multi-agent modeling pass, don't hand-wave it; (b) **specs MUST be updated** — every phase that changes a contract ships a spec delta, not just code.

---

## Two threads folded in this round

- **Richer oracle facts + the `br ready` problem.** The fact-tool should know about work that's *in progress* and *lined up*, not just empty `br ready`. And `br ready` itself "has consistently been an issue" — a focused investigation is running (failure modes + the one-line rule + a categorized fact inventory: ready / in-progress / lined-up / generative). Its output feeds Phase 1's `GatherDrainFacts` and Phase 2's state model.
- **Cognition observability in state.** Context-size (session + subagents) + staleness + loop-pattern signals become first-class fields in the system-state snapshot (Phase 2), replacing the "separate checker agent" idea.

---

## Phase breakdown (build focus = 0–2; spec-only = 3; hold = 4)

### Phase 0 — Safety prerequisites (build now; small; unblocks trust)
Independent of the redesign; any wind-down is unsafe without them.
- **P0-a** ctx-watchdog (and every restart authority) skips a session with a live `.sleeping.*` marker.
- **P0-b** wake pane resolution via `ResolveTmuxTarget` + liveness probe (kill the hard-coded `…-captain:0.0`).
- **P0-c** daemon-startup reconcile of orphaned `.sleeping.*` markers (+ re-seed the failsafe).
- **P0-d** add `source` + `level` fields to the sleep marker (operator PARK outranks a stray `queue submit` wake).
- **P0-e** `IsSleeping` fail-open-on-empty-sessionID fix (`gates.go:155`), interacts with SID-flips-on-/clear.

### Phase 1 — Stop the dumb stuff: demote the oracle (build now; ~small)
- **P1-a** reshape `GenuineDrain` → `GatherDrainFacts`: emit a typed **fact bundle** (ready / in-progress / lined-up — per the br-investigation inventory), drop `DrainState{DRAINED}` as a control signal; keep `UNSURE`-on-read-error as a read-quality flag. Preserve all 5 false-negative defenses.
- **P1-b** delete the auto-park tick (`quiesce.go:282–295`) + unwire `SetDrain` / `daemon.go:1769–1785`. Keep the 4h failsafe + event-reflex wake.
- **P1-c** re-cast non-force `harmonik sleep` as **veto-on-execute**: re-run the facts, refuse if work would be stranded; `--force` stays the override.
- **P1-SPEC** update `specs/operator-nfr.md` ON-008/ON-010 ("sleep is LLM-initiated; daemon refuses to execute a sleep that strands work") + fix the 4 bound spec-audit tests.

### Phase 2 — Model the system (build now; the KEYSTONE — design-first)
- **P2-DESIGN** deliberate multi-agent **system-model design pass** → produces the normative `specs/system-state.md`. Must nail: the actual-state envelope, the state labels (PROCESSING / WAITING / DRAINING / INACTIVE), how per-session lifecycle FSM + queues roll up, and the cognition-observability fields. *This is the "really think about it" step the operator called out.*
- **P2-a** `harmonik state [--json]` — the aggregator command (union of existing readers; `captain-boot-digest.sh` already does ~half). The "captain prints the system status" primitive + the home for `GatherDrainFacts`.
- **P2-b** the system-state **fold** (~50 lines over existing per-session FSM + `QueueStore` + `RunRegistry`) → the labels, and the label gates which polls run (INACTIVE ⇒ stop run-watchers/gauges).
- **P2-c** **context-into-state**: capture per-session + per-subagent context size; derive too-big / not-changing / repeating-pattern (Haiku) signals; surface in the snapshot. Watchdog reads this instead of eyeballing.
- **P2-SPEC** `specs/system-state.md` (NORMATIVE) — ships with the design pass, not after.

### Phase 3 — Workflows + teardown (SPEC ONLY now; build later)
- **P3-SPEC** extend `specs/park-resume-protocol.md`: the vocabulary, the hard↔soft **`--level` enum** (L0 abandon / L1 drain / L2 handoff / L3 finish-lane), and the W1 (operator sleep) / W3 (semantic no-work park) / W4 (crew context-pressure handoff) / teardown-as-transition sequences. Build is deferred until the spec is signed off.

### Phase 4 — DESIRED-state reconcile loop (HOLD)
- **P4** the full Kubernetes-style actual↔desired reconcile controller (LLM authors desired, Go validates + executes the diff). On hold; revisit only if Phase 2 proves the fact snapshot insufficient. Its own kerf work.

---

## Sequencing & dependencies

```
P0 (safety) ──────────────┐  (independent, can land anytime)
P1 (demote oracle) ───────┼──► P2 (model: state cmd + fold + ctx-in-state)
        P1-a facts ───────┘        ▲
                                   └── P2-DESIGN feeds P2-a/b/c and emits specs/system-state.md
P3-SPEC (park/level spec) ── independent, can be drafted in parallel
P4 ── HOLD
```
- Hard dep: **P2-a/b** consume **P1-a**'s `GatherDrainFacts`.
- **P2-DESIGN gates P2 build** — no Phase 2 code lands before `specs/system-state.md` exists and is reviewed.
- P0 and P3-SPEC are independent and parallelizable.

## Spec deltas (operator emphasis: these are first-class, not afterthoughts)
- `specs/operator-nfr.md` — ON-008/ON-010 reworded (P1-SPEC).
- `specs/system-state.md` — NEW, normative (P2-SPEC).
- `specs/park-resume-protocol.md` — extended with vocabulary + level enum + workflow sequences (P3-SPEC).

## Bead map (created 2026-06-20, codename:fleet-state)

Umbrella epic **hk-up4b**.

| Phase | Bead | Item |
|---|---|---|
| QUICK-FIX | **hk-5kn3** | `br ready` truncation: digest builder `--limit 0` + fix crew/beads skill docs (LIVE bug) |
| P0 | hk-jxcx | ctx-watchdog skips `.sleeping.*` sessions |
| P0 | hk-fv40 | wake pane resolution via ResolveTmuxTarget + probe |
| P0 | hk-x03v | daemon-startup orphaned-marker reconcile |
| P0 | hk-caaf | marker `source`+`level` fields |
| P0 | hk-uord | `IsSleeping` fail-open fix |
| P1 | hk-pfr4 | reshape → `GatherDrainFacts` / `Snapshot()` (fact bundle) |
| P1 | hk-kj7d | delete auto-park tick + unwire |
| P1 | hk-zqb3 | non-force `sleep` = veto-on-execute |
| P1 | hk-9mdz | SPEC: operator-nfr ON-008/ON-010 reword + tests |
| P2 | hk-9fvk | **DESIGN pass** → specs/system-state.md (gates P2 build) |
| P2 | hk-gv04 | `harmonik state [--json]` aggregator (dep: hk-pfr4, hk-9fvk) |
| P2 | hk-w6q7 | system-state fold + poll-gating (dep: hk-pfr4, hk-9fvk) |
| P2 | hk-jay1 | context-into-state (session+subagent; loop/stale signals) (dep: hk-9fvk) |
| P2 | hk-8lne | SPEC: specs/system-state.md (normative) |
| P3 | hk-wrjv | SPEC-ONLY: park-resume-protocol + `--level` enum + workflows |
| P4 | hk-cyec | reconcile loop — **HOLD** |

## `br ready` findings (investigation, 2026-06-20) — folded into hk-5kn3 + hk-pfr4

- **Root cause = config + doc-rot + one live code gap**, NOT a daemon-dispatch bug. The daemon's own dispatch only needs ready[0], so its default-paginated `Ready()` is harmless there.
- **The live bug:** `internal/digest/builder.go:358-365` calls bare `br ready` (capped at 20) → the boot digest's ready list is silently truncated and agents trust it. Skill docs (`beads-cli/SKILL.md`, `crew-launch/SKILL.md:291`) teach the truncating form; only the captain SKILL teaches `--limit 0`. → hk-5kn3.
- **Correct-but-confusing semantics (handle by RULE, not code):** epic-dep gating, assignee wedging, `needs-attention` exclusion, and `in_progress`/`draft`/`deferred` absence are all *correct* — `br ready` reports "dispatchable right now," a strict subset of "is there work."
- **Canonical rule to bake into skills:** *`br ready` = dispatchable-now, not is-there-work; always `--limit 0`; never read empty as drained without also checking in-progress + blocked-by-open-epic + paused/failed queues.*
- **Fact-tool implication (hk-pfr4):** the snapshot must emit per-axis **counts + lists** (stop short-circuiting at the first sign of work), add the 3 buckets the oracle drops (in_progress, standalone-blocked, needs-attention/draft/deferred), and flag childless open epics as `needs_decomposition` — the one generative category it hands to the captain rather than scoring.

## Out of scope (operator-declared)
- Captain that has lost its way after many /clears (token-burn from a confused captain).
- A crew stuck at ~200k with a forever-wake monitor. (Phase 2 context-into-state *observes* these, but auto-remediation is not in this initiative.)
