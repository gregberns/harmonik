# Lens 5 — ZFC purity + the normative spec shape for `specs/system-state.md`

**Codename:** `fleet-state` · **Bead:** hk-9fvk (P2-DESIGN pass) → emits hk-8lne (P2-SPEC).
**Date:** 2026-06-20 · **Lens role:** ZFC invariant enforcement + house-style spec scaffold.

**Grounded in:**
- `docs/concepts/zero-framework-cognition.md` (the four-step flow §21–26; allowed-vs-forbidden §28–32; the harmonik resolution §51–56; the over-application warning §65).
- `plans/2026-06-20-fleet-sleep-wake-status-and-next/03-scaffold.md` (locked decisions 1–5; vocabulary; the fact-bundle/axes from the `br ready` investigation §103–113).
- `plans/2026-06-20-fleet-sleep-wake-status-and-next/02-zfc-redesign-synthesis.md` (the demote-not-delete verdict; the one-directional veto §42–45; the decision-locus map §50–60).
- `specs/operator-nfr.md` (ID convention `ON-NNN`, `ON-INV-NNN` invariants, the `Tags:`/`Axes:` per-requirement footer, front-matter shape, §10 conformance/sensors).
- `specs/park-resume-protocol.md` (contract-shape sibling; §6 Invariants list style; vocabulary the new spec must keep consistent).
- `internal/daemon/draindetect.go` (the live fact axes + the five false-negative defenses the snapshot must preserve verbatim).

---

## Part 1 — ZFC invariant list (normative MUSTs + the code-level catch)

These are the invariants that keep the Go side **facts-only**. Each is written as it should appear in the spec (`SS-INV-NNN`), with the violation a reviewer/test can mechanically catch.

### SS-INV-001 — The snapshot reports facts; it never decides sleep/wake/park
The system-state snapshot (`harmonik state`, `Snapshot()`, the `GatherDrainFacts` fact bundle) MUST be a pure **Gather + structural-fold** operation. It MUST NOT contain, return, or persist any field whose value is a wind-down *decision* — no `should_sleep`, `recommend_park`, `wind_down: bool`, or equivalent. The four state labels (SS-002 below) are **observations** of current activity, never directives.
- **Catch:** grep the snapshot struct + its JSON tags for decision-verbs (`should_`, `recommend_`, `auto_`, `_decision`, `wind_down`). A field matching is a BLOCK. A unit test asserts the snapshot type has no boolean/enum field that an actuator (`sleep`/`wake`/`park`) consumes as its *trigger* — actuators take the snapshot as *evidence to validate against*, never as a command.

### SS-INV-002 — Work is never collapsed to a single DRAINED boolean
The snapshot MUST emit **all work axes** as **counts + lists** (ready, in-progress, lined-up/blocked-by-open-epic, paused/failed queues, needs-attention/draft/deferred, childless-open-epics). It MUST NOT short-circuit at the first sign of work, and MUST NOT expose a single `drained: bool` (or `DrainState{DRAINED}` re-used as a control signal). `UNSURE`-on-read-error survives only as a **read-quality flag**, never as a license to act.
- **Catch:** the existing `DrainState{DRAINED}` constant (`draindetect.go:67`) MUST NOT appear in the snapshot's public output type or in any actuator call-site as a gate. Test: assert the fact bundle returns per-axis counts even when every axis is non-empty (no early `return DRAINED`). A reviewer flag fires if a caller does `if snapshot.Drained { sleep() }`.

### SS-INV-003 — Generative categories are flagged for the captain, never scored or auto-acted
Childless open epics (`needs_decomposition`) and any other **generative/latent** work category MUST appear in the snapshot as a **flagged, enumerated fact** (count + epic IDs). The snapshot MUST NOT score, rank, prioritize, or auto-decompose them, and MUST NOT let their *absence* license sleep. They are handed to the captain LLM, which alone owns the judgment "this latent work means don't wind down."
- **Catch:** the `needs_decomposition` axis is a list of IDs + a count — no `priority`, `score`, or `urgency` field on it. Test: a project with only childless-open-epics (zero ready beads) produces a snapshot whose work-axes are non-empty (epics flagged), so a downstream `sleep` veto (SS-INV-005) still has grounds to refuse. This is the ZFC:32 "completion-detection-via-heuristics" ban applied to the generative case.

### SS-INV-004 — No Go auto-drain: the captain always initiates sleep; Go validates + executes
No Go code path MAY *initiate* a wind-down (SLEEP/PARK) from the snapshot. There MUST be no timer, tick, or reflex that reads the snapshot and calls an actuator. The captain LLM initiates; Go's role is **veto-on-execute** (re-gather facts, refuse if work would be stranded) + mechanical execution. The 4h max-sleep failsafe and the event-reflex *wake* survive (they are deterministic policy/reflex per ZFC:29, not completion-decisions).
- **Catch:** the auto-park tick (`quiesce.go:282–295`) and the `SetDrain`/`daemon.go:1769–1785` wiring MUST be deleted (P1-b). Test: grep for any goroutine/`time.Ticker` whose body calls `parkAllSessions`/`parkSession` *without* a CLI-`sleep`/operator-command on the stack. The only callers of the park actuator are (a) the `harmonik sleep` command path and (b) the 4h failsafe — nothing reads the snapshot and decides.

### SS-INV-005 — The veto is one-directional (facts may refuse sleep, never command it)
The fact bundle MAY **veto** a sleep (`harmonik sleep` non-force re-gathers and refuses if any axis shows dispatchable/in-flight work). The fact bundle MUST NEVER **command** a sleep. This asymmetry is what keeps Go facts-only while still closing the dead-fleet-on-ready-beads wound.
- **Catch:** the veto lives only inside the `sleep` execute path as a guard that can return "refused"; it has no path that *calls* `sleep`. Test: assert `GatherDrainFacts` has zero call-sites that invoke an actuator; its only consumers are (a) `harmonik state` output and (b) the `sleep` veto-guard. (Preserves all five false-negative defenses from `draindetect.go:35–47` verbatim — they are read-correctness, not judgment.)

### SS-INV-006 — Vocabulary: "quiesce" is absent from the operator-facing surface
The spec, the CLI output, the snapshot field names, and all operator-facing copy MUST use **SLEEP / PARK / STOP / TEARDOWN**; the resting state is **"asleep" / "at rest."** The word **"quiesce"** MUST NOT appear in any operator-facing string, JSON field, event, or doc. (Internal `QuiesceArbiter`/`quiesce.go` symbol names are out of scope for this invariant — they're a low-pri rename, candidate `WindDownCoordinator`.)
- **Catch:** grep `specs/system-state.md`, the snapshot JSON tags, and `cmd/harmonik/*.go` operator-facing output strings for `quiesce`/`Quiesce` — zero hits in operator-facing positions. A reviewer flag fires on any new operator-facing use.

### SS-INV-007 — The snapshot is observation-only (no mutation, no in-flight effect)
`harmonik state` / `Snapshot()` MUST NOT mutate daemon state and MUST NOT abort/pause/resume any in-flight run. It is a read surface, exactly like `harmonik subscribe` (cf. `operator-nfr.md` §4 on subscribe's ON-INV-006 exemption-by-construction).
- **Catch:** the snapshot path takes only read locks / read-only adapters; test asserts no write to `.harmonik/` and no actuator call during a `state` invocation. Mirrors ON-INV-006's "no new control surface" discipline.

> **Tie-back to ZFC:** SS-INV-001/002/003 enforce the **Gather** step purity (no interpretation, ZFC:24); SS-INV-004/005 enforce that **Decide** lives only in the model (ZFC:32 completion-detection ban); SS-INV-007 enforces **smart endpoints / dumb pipes** (ZFC:34–35). The snapshot is the highest-value "bone" (synthesis §62): Gather made consistent, never Decide.

---

## Part 2 — ACTUAL-state vs DESIRED-state boundary

**Locked (scaffold decision 4 + §6 scope fork):** model **ACTUAL state now**; **DESIRED state is a forward-looking stub only** (the full reconcile loop is Phase 4, on HOLD — bead hk-cyec).

What the spec says about each:

- **ACTUAL state — fully normative now.** The state envelope, the four labels, the fact-bundle axes, and the cognition-observability fields are all ACTUAL-state: a pure read of "what is true right now" (live sessions, queue statuses, run registry, per-session/subagent context sizes, the drain fact bundle). This is the entire normative body of v1.0 of the spec.

- **DESIRED state — a NAMED PLACEHOLDER SECTION, explicitly NOT modeled in v1.0.** The spec MUST carry a `§N. Desired state (Phase 4 — HOLD, not modeled in v1.0)` section that:
  1. **Names** the concept (a desired-state document: which sessions/crews/queues *should* exist; per-session `state ∈ {ready, suspended, torn-down}`) so future work has a stable anchor and downstream specs have something to cite.
  2. States the **boundary rule normatively**: *the ACTUAL-state snapshot of this spec MUST NOT embed, infer, or persist any desired-state field. Desired state, when introduced, is authored by the captain LLM (cognition) and validated structurally by Go (name regex + referential integrity only — no merit judgment); it is its own kerf work (hk-cyec) and does NOT alter the ACTUAL-state contract.*
  3. Records the **HOLD condition**: revisited only if Phase 2 proves the ACTUAL fact snapshot insufficient.
  - This keeps the ZFC reconcile-loop temptation (synthesis §120, Lens D/E's "framework intelligence in prompt-land" warning, ZFC:65 over-application) **out of scope by construction** — the stub forbids it from leaking into v1.0 rather than leaving silence that invites it.

---

## Part 3 — Proposed section structure of `specs/system-state.md`

Matches `operator-nfr.md` / `park-resume-protocol.md` house style: YAML front-matter block, numbered `##` sections, a Glossary, a `## 4 Normative requirements` body with `#### SS-NNN —` headed requirements each carrying a `Tags:` footer (and `Axes:` where it does I/O), a `## N Invariants` block (`SS-INV-NNN`), a conformance/sensor section, and a changelog.

```
---
title: System State Model
spec-id: system-state
requirement-prefix: SS
spec-category: foundation-cross-cutting
status: draft
spec-shape: requirements-first          # (model + normative predicates; cf. operator-nfr)
version: 1.0.0
spec-template-version: 1.1
owner: fleet-state-author
depends-on:
  - operator-nfr            # ON-008/ON-010 sleep-is-LLM-initiated (P1-SPEC)
  - park-resume-protocol    # PARK/wake session-side contract + vocabulary
  - process-lifecycle       # daemon status; live-session enumeration
  - queue-model             # QueueStore statuses
  - execution-model         # RunState / in_flight(run); the per-session lifecycle FSM
  - beads-integration       # the work-axes (ready/in-progress/blocked/needs-attention)
---

## 1. Purpose
   The normative model of harmonik's ACTUAL fleet state: a typed, facts-only
   snapshot the captain LLM reads to decide wind-down. Establishes that the
   snapshot Gathers + folds, and NEVER decides.

## 2. Scope
   2.1 In scope: the ACTUAL-state envelope; the four activity labels + predicates;
       the fact-bundle work-axes; the cognition-observability fields; the
       observation-only invariant; the desired-state placeholder.
   2.2 Out of scope: the DESIRED-state model + reconcile loop (§N stub → Phase 4);
       the wind-down *workflows/levels* (owned by park-resume-protocol, P3-SPEC);
       auto-remediation of a lost captain / stuck crew (operator-declared OOS).

## 3. Glossary
   ACTUAL state · work-axis · activity label · the four labels · fact bundle ·
   cognition-observability · veto-on-execute · at-rest. (Reuse in_flight(run)
   from execution-model; do NOT redefine.) Vocabulary table: SLEEP/PARK/STOP/
   TEARDOWN/at-rest — and the explicit note that "quiesce" is retired from the
   operator surface (SS-INV-006).

## 4. Normative requirements

   ### 4.1 The state envelope (top-level snapshot shape)
     SS-001  — `harmonik state [--json]` emits a single typed StateSnapshot:
               { schema_version, captured_at, daemon_status, activity_label,
                 sessions[], queues[], runs[], work_axes{...}, cognition{...},
                 read_quality }. Counts+lists, never a single boolean. [Tags: mechanism]
     SS-002  — The snapshot is a fold over EXISTING readers (per-session lifecycle
               FSM, QueueStore, RunRegistry, crew registry, keeper gauge); it adds
               no new persistent store. [Tags: mechanism]

   ### 4.2 The four activity labels (normative predicates)
     SS-003  — PROCESSING ≡ ≥1 run satisfies in_flight(run) [per execution-model].
     SS-004  — WAITING    ≡ no in_flight run, but ≥1 work-axis non-empty
               (ready / lined-up / needs_decomposition) — work exists, none dispatching.
     SS-005  — DRAINING   ≡ ≥1 run in_flight AND every queue status ∈ paused/closing
               (finishing current work, accepting no new).
     SS-006  — INACTIVE   ≡ no in_flight run AND every work-axis empty AND read_quality=OK.
               INACTIVE is an OBSERVATION, not a sleep command (SS-INV-001).
     SS-007  — The label gates which POLLS Go runs (INACTIVE ⇒ stop run-watchers/
               gauges; PROCESSING ⇒ all armed). This is mechanism (a deterministic
               poll-on/off rule), not a wind-down decision. [Tags: mechanism]
               (NB labels are ACTIVITY observations; the operator verbs
               SLEEP/PARK/STOP/TEARDOWN are a SEPARATE axis — keep distinct.)

   ### 4.3 The fact-bundle work-axes (the demoted oracle, as a tool)
     SS-008  — work_axes MUST carry per-axis {count, ids[], reason}: ready,
               in_progress, lined_up (blocked-by-open-epic), paused_or_failed_queues,
               needs_attention (incl. draft/deferred), needs_decomposition
               (childless-open-epics). Preserves the five false-negative defenses
               of draindetect.go verbatim. [Tags: mechanism]
     SS-009  — needs_decomposition is FLAGGED only — no score/rank/priority field;
               handed to the captain (SS-INV-003). [Tags: mechanism]
     SS-010  — read_quality ∈ {OK, UNSURE}; UNSURE on any read error. UNSURE keeps
               the fleet AWAKE (a read-quality flag, never a license to act). [Tags: mechanism]

   ### 4.4 Cognition-observability fields
     SS-011  — cognition MUST carry, per session AND per subagent: context_size
               (tokens / % of window), context_delta (changed since last sample?),
               loop_signal (cheap Haiku "same thing repeating" pass result, with
               its model + sampled-window provenance). [Tags: mechanism]
     SS-012  — These are FACTS surfaced for a reader (the ctx-watchdog reads state
               instead of eyeballing); the snapshot MUST NOT itself restart, clear,
               or remediate a session on them (auto-remediation is OOS). [Tags: mechanism]
     SS-013  — The loop_signal is a reported observation, not a gate: no Go path
               MAY auto-act on loop_signal (it informs the captain/watchdog). [Tags: mechanism]

   ### 4.5 The wind-down actuator boundary (veto-on-execute)
     SS-014  — The snapshot is read-only; actuators (sleep/wake/park/teardown) are
               the ONLY mutators and live in the command surface, not here.
     SS-015  — Non-force `harmonik sleep` MUST re-gather work_axes and REFUSE
               (veto) if any dispatchable/in-flight axis is non-empty; `--force`
               overrides. The facts may veto, never command (SS-INV-005).
               (Cross-ref operator-nfr ON-008/ON-010, reworded in P1-SPEC.) [Tags: mechanism]

## 5. Invariants
   SS-INV-001 … SS-INV-007  (Part 1 above, verbatim, each with its Sensor).

## 6. Desired state (Phase 4 — HOLD; NOT modeled in v1.0)
   The named placeholder per Part 2: concept named, boundary rule stated
   normatively (ACTUAL snapshot MUST NOT embed desired-state), HOLD condition
   recorded, forward-pointer to hk-cyec.

## 7. Conformance & sensors
   Mirror operator-nfr §10: per-label predicate tests; the no-decision-field
   grep sensor (SS-INV-001); the no-DRAINED-boolean sensor (SS-INV-002); the
   generative-only-still-vetoes sensor (SS-INV-003); the no-auto-park-tick grep
   (SS-INV-004); the GatherDrainFacts-has-no-actuator-callsite test (SS-INV-005);
   the no-"quiesce"-in-operator-surface grep (SS-INV-006); the
   state-is-read-only test (SS-INV-007).

## 8. Bead references
   hk-9fvk (design pass) · hk-8lne (this spec) · hk-gv04 (state cmd) ·
   hk-w6q7 (fold + poll-gating) · hk-jay1 (context-into-state) · hk-pfr4
   (GatherDrainFacts) · hk-cyec (desired-state reconcile — HOLD).

## 9. Changelog
   (operator-nfr-style dated/version/author rows.)
```

---

## Part 4 — Spec IDs / numbering convention

Looking at `operator-nfr.md`: it declares `requirement-prefix: ON` in front-matter; topical requirements are `ON-NNN` (zero-padded 3-digit, with `a`/`b` sub-letters for in-place additions, e.g. `ON-004a`); invariants are a reserved `ON-INV-NNN` band; the envelope block uses a reserved `ON-ENV-NNN` band; **retired IDs are never reused**; every requirement carries a `Tags:` footer and an `Axes:` footer when it does I/O or state mutation; a `## Changelog` records every ID add/retire. `park-resume-protocol.md` (contract-shape) uses a plainer `## N Invariants` numbered list and `R-C4.11`-style refs.

**Proposed for `system-state.md`:**
- **`requirement-prefix: SS`** — topical requirements `SS-NNN` (3-digit, sub-letter `a/b` for in-place additions).
- **`SS-INV-NNN`** — reserved invariant band (the seven ZFC-purity invariants).
- **`SS-ENV-NNN`** — reserved (foundation-cross-cutting; envelope optional/voluntary like ON's, only if the spec ends up emitting cross-subsystem events; likely deferred).
- Per-requirement `Tags: mechanism` footer (the whole spec is mechanism — there is **no** `cognition`-tagged requirement here, which is itself the ZFC proof: a facts-only spec). `Axes:` only on SS-015 (the veto path does I/O + a refuse-or-execute branch: `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent`).
- IDs frozen once published; retirements logged in §9, never reused.

---

## Locked-decision risk flags (Phase 1 / Phase 2 design)

1. **`DrainState{DRAINED}` leaking into the snapshot.** P1-a reshapes `GenuineDrain → GatherDrainFacts` but the constant still exists in `draindetect.go:67`. **Risk:** if `Snapshot()` re-exposes a `Drained bool` or re-uses `DrainState` as a public field, it silently violates SS-INV-002 (single-boolean collapse) and re-opens the very Go-decides path the redesign removes. *Mitigation:* SS-INV-002's sensor must assert the constant is not in the public output type.

2. **The INACTIVE label being read as "therefore sleep."** SS-006 makes INACTIVE a legitimate observation. **Risk:** a P2-b implementer wires `label == INACTIVE` to an actuator (poll-gating is fine; auto-park is not). The line is thin — *poll-on/off is mechanism, park is decision.* SS-007's wording + SS-INV-004's no-auto-park-tick sensor must hold this line explicitly, or Phase 2 quietly re-introduces auto-drain under a new name.

3. **Cognition-observability sliding into auto-remediation.** SS-011/012/013 are the "context-into-state" fields. **Risk:** the ctx-watchdog (which P2-c says should *read* state) keeps its eyeball-and-restart authority and now restarts on `loop_signal`/`context_size` — that's a Go heuristic making a stuck-ness *decision* (ZFC:32, synthesis §58). The scaffold marks auto-remediation OOS; SS-012/013 must state it normatively so the watchdog stays a reader.

4. **The Haiku loop-detection pass is itself cognition — keep it OUT of the Go fold.** SS-011's `loop_signal` is produced by an LLM (Haiku), not by Go pattern-matching. **Risk:** an implementer "optimizes" it into a Go regex/substring loop-detector — exactly ZFC:32's keyword-completion anti-pattern. The spec MUST require `loop_signal` come from a model call (with provenance), never a Go heuristic; the snapshot merely *carries* the reported result.

5. **"Quiesce" still in shipped surfaces.** `park-resume-protocol.md` v1.0 still says "QuiesceArbiter" and "quiesced" throughout, and the live sleep marker/event vocabulary uses it. **Risk:** the new spec is clean but the sibling specs/CLI keep the word, so the operator still meets it. SS-INV-006 covers `system-state.md`; flag that the **P3-SPEC park-resume rewrite must also scrub operator-facing "quiesce"** for decision 5 to actually hold fleet-wide (internal symbol rename stays low-pri).
