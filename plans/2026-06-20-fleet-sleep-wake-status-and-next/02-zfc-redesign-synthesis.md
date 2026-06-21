# Fleet wind-down — ZFC realignment & system-state model (synthesis)

**Date:** 2026-06-20
**Supersedes the direction in:** `./README.md` (which was "harden the shipped sleep feature"). This doc reframes the work around Zero Framework Cognition after operator feedback.
**Inputs:** 5 parallel agent passes (explorer + lenses A–E) + direct code reads of `draindetect.go`, `quiesce.go`, `daemon.go`, ZFC.

---

## 0. The reframe

The original framing was "harden a sleep feature." The operator's feedback changes it to three things:
1. **Stop doing the dumb thing** — a deterministic "oracle" is making a decision it shouldn't.
2. **Model the system** as state (actual + desired), so we can reason about park/sleep/teardown coherently.
3. **Re-ground in Zero Framework Cognition** (`docs/concepts/zero-framework-cognition.md`): LLMs make decisions; deterministic code is the pipes + the structure/tools that make LLM behavior consistent. "A couple places we add bones."

This is not a token-burn patch anymore. It's a **decision-locus correction** plus a **state model**, with token-burn as the first beneficiary.

---

## 1. The ZFC resolution (the principle, applied)

ZFC's own line (zero-framework-cognition.md:51–56):
- **Mechanism** (allowed in Go): "if state == X and event == Y → transition to Z." Gather I/O, validate structure, execute transitions, enforce deterministic policy.
- **Cognition** (must be the model): "what should happen next in this ambiguous situation?" — and line 32 explicitly bans **"completion detection via heuristics"** from the framework.

**Where the oracle sits — the sharpened verdict:** the oracle's *fact-gathering* (count ready beads, in-flight runs, paused queues, blocked epic-children, the `kerf next` lie) is **Gather — ZFC-legal**. But the step `DRAINED → park everything` (`quiesce.go:282–295`) is **completion-detection-as-decision — the violation.**

The decisive argument (why the operator is right that Go shouldn't decide): **an empty `br ready` is a fact, but "therefore sleep" is a judgment** — because *no dispatchable bead ≠ nothing to do*. The captain's job includes **generating** work (decomposing an epic, planning the next lane) that is not yet a ready bead. The oracle is structurally blind to that latent/generative work. So the decision to wind down requires understanding the backlog — cognition — even though the facts under it are deterministic.

---

## 2. What five independent agents converged on

This convergence is the strongest signal in the analysis — three lenses that did not see each other's output landed on the same design:

> **Demote the oracle, don't delete it. Keep its facts as a tool; move the *decision* to the captain; keep a deterministic guardrail that can VETO an unsafe sleep but never COMMAND one.**

- **Lens A (decision-locus):** "the oracle becomes a *lens, not a judge*." Delete the auto-park tick; keep the facts as `harmonik status --json`; the captain decides; non-force `sleep` re-checks the facts and *refuses to execute* against work (policy enforcement = ZFC-legal).
- **Lens E (adversarial):** "demote, don't delete — deleting `draindetect.go` re-ships the exact dead-fleet-on-ready-beads wound the five defenses were built to close." Keep `HAS_WORK`/`UNSURE` as a Go veto; the only real violation is ~14 lines.
- **Lens D (workflows):** the one legitimate LLM-owned park decision is **"there's ready work but it's all operator-blocked, so I'll park"** (W3) — with the Go facts as a backstop. Mechanical drain stays a fact, not an auto-action.

The **one-directional veto** is the crux that makes all of this safe and ZFC-clean:
- Facts may **veto** sleep (dispatchable/in-flight work exists → `sleep` refuses) → prevents the dead-fleet failure.
- Facts may **never command** sleep → only the captain decides, because only it knows about generative/latent work.

---

## 3. Decision-locus map (Lens A, condensed)

| Capability | Locus | Why |
|---|---|---|
| Gather drain *facts* (ready/in-flight/paused/blocked) | **Go** | Pure I/O + structural scan. The 5 false-negative defenses = correctness of the read, not judgment. |
| Decide "no meaningful work → wind down" | **LLM** | Completion detection (ZFC:32). Requires understanding the backlog + generative work. |
| Wake on new-work *event* | **Go reflex** | Deterministic rule (event → nudge), not judgment. |
| Wake — *semantic* "operator's back, re-plan" | **LLM** | Judgment. |
| Park / write marker / stop monitors | **Go** | State transition + I/O (dumb pipe once decided). |
| Tear down a crew (the act) | **Go** | Mechanical RPC. |
| Decide a crew *should* be torn down (done/stuck) | **LLM** | Judgment — never a Go stuck-ness heuristic. |
| Know live state of every session/queue/run | **Go** | Pure enumeration — this is the under-built part (no captain-session liveness API yet). |
| Max-sleep 4h failsafe | **Go** | Policy enforcement of a deterministic rule (ZFC:29). |

**The three legitimate "bones"** (deterministic structure that makes the LLM consistent, without deciding for it):
1. `harmonik status --json` / `harmonik state` — a typed **fact snapshot** the LLM reads (Gather, made consistent). Highest-value bone.
2. `sleep` / `wake` / `teardown` command surface as the **only** actuator the LLM calls (smart endpoints, dumb pipes).
3. Deterministic **guardrails the LLM opts into** — the gated-sleep fact-recheck + the max-sleep failsafe.

Everything labeled a "bone" passes the test: it's Gather, Validate, or Execute — never Decide.

---

## 4. The oracle: concrete demote plan (from explorer wiring + A + E)

Wiring is already cleanly separable (explorer map):
- **Decision footprint to remove:** `GenuineDrain` consumed as control at `quiesce.go:288` (auto-park tick) + the `SetDrain`/`cfg.Drain` field + the daemon wiring `daemon.go:1769–1785`. Plus the non-force `sleep` gate at `quiesce.go:535` is **kept but re-cast** (see below).
- **Plumbing to keep verbatim** (already exercised by `sleep --force` + wake): `parkAllSessions`, `parkSession`, markers, `nudgePane`, `WakeCh`, `handleQueueSubmit`, `handleEpicCompleted`, `executeWake`, `HandleDaemonWake`, the **4h failsafe** (runs even when `Drain == nil`).

**Steps:**
1. Rename/reshape `GenuineDrain` → `GatherDrainFacts`: output a **fact bundle** (counts + ids + reasons per axis), drop `DrainState{DRAINED}` *as a control signal*. Keep `UNSURE`-on-read-error as a read-quality flag. The five defenses survive verbatim.
2. Surface it as `harmonik state --json` (Gather). The captain — or an occasional checker — reads it.
3. **Delete the autonomous auto-park tick** (`quiesce.go:282–295`). Go never *initiates* sleep.
4. Re-cast non-force `harmonik sleep` as the **veto-on-execute**: it re-runs `GatherDrainFacts` and *refuses* if any axis shows dispatchable/in-flight work. The LLM *chose* to sleep; Go *validates the choice is safe* before executing. `--force` stays the operator override.
5. **Spec change (honest correction):** `specs/operator-nfr.md` ON-008/ON-010 currently *require* sleep be "drain-gated" by the daemon, and 4 spec-audit tests bind to that. The veto-on-execute satisfies "don't sleep over work" without the daemon *deciding* to sleep — update ON-008's wording from "daemon auto-sleeps when drained" to "sleep is LLM-initiated; the daemon refuses to execute a sleep that would strand work."

**Net deletion is tiny (~14 lines + wiring); the valuable code is preserved as a tool.**

---

## 5. Vocabulary + workflows + the hard/soft spectrum (Lens D)

**Vocabulary** (disambiguated on 4 axes: reversible? pane alive? polls? in-flight work?):
- **QUIESCE** — automatic rest (today's mislabeled "auto-sleep"). *Under this redesign, QUIESCE stops being Go-initiated* — it becomes "the captain noticed drain and parked." Reserve the word for the resting *state*, not a Go decision.
- **SLEEP** — operator "done for the day"; whole fleet, work-finishing.
- **PARK** — interrupt-and-hold at the nearest safe point (work not guaranteed to finish).
- **STOP** — one crew, pane killed.
- **TEARDOWN** — irreversible fleet shutdown.

**Renaming decision flagged:** today the daemon auto-behavior *and* the CLI verb are both "sleep." Split them.

**Hard↔soft = a `--level` enum on every wind-down verb, not a separate feature:**
- **L0 abandon** (kill mid-run, bead → open) · **L1 drain** (current run commits, queue paused, then stop) · **L2 handoff** (run finishes + crew writes `/session-handoff` + captain integrates + stop) · **L3 finish-lane** (run to natural empty, park not kill).
- Choosing the level = **LLM**; executing it = **Go**.
- `crew stop` is a **pure hard kill today** — soft-stop is an LLM procedure that runs *before* `crew stop`.

**Workflows (W1–W7):** operator SLEEP (L2), captain semantic NO-WORK park (W3, the one LLM-owned park, L3), crew context-pressure handoff (W4, L2→L0 self-stop), operator PARK (W5, L1), TEARDOWN (W6, L0), WAKE/RESUME (W7, first-class — owns the wake-reliability gaps).

---

## 6. System-state machine (Lens C) — and the scope fork

**Not greenfield.** The leaves already ship: the per-session lifecycle FSM (`internal/handlercontract/lifecycle/`, attached to every run via `RunHandle.machine`), `QueueStore`, `RunRegistry`, the crew registry, the keeper gauge/`.sid`/`.managed` readers, `supervise status`. `scripts/captain-boot-digest.sh` already fuses ~half of "actual state."

**Net-new, all cognition-free:**
1. `harmonik state [--json]` — union of existing readers (= the "captain prints status" primitive + the oracle's fact snapshot from §4).
2. DESIRED-state schema (which sessions/crews/queues should exist; session `state ∈ {ready, suspended, torn-down}`) + a **structural** Go validator (name regex, referential integrity — no merit judgment).
3. A ~50-line **fold** rolling per-session FSMs + queue statuses into system labels **PROCESSING / WAITING / DRAINING / INACTIVE** — and the label tells Go *which polls to run* (the operator's original insight: INACTIVE ⇒ stop run-watchers/gauges; PROCESSING ⇒ all armed).
4. **Teardown as a first-class transition** — the FSM already has `Ready→Terminating→Terminated` (+`→Failed` escalation); wrap with a request + a Go verify-stopped gate (`tmux has-session`==false, run `Wait` returned). Exactly "request → tear down → check when done."

**The reconcile loop = the 4 ZFC steps at fleet scale:** Go gathers actual + computes the structural diff → captain judges "is this divergence a problem? what's desired?" → Go validates the desired doc's shape → Go executes the diff verb-by-verb. The git analogy holds: Go shows the diff, the model resolves it.

**The scope fork (honest tension):** Lens C shows the machine is mostly assembly of existing parts. Lenses D and E warn that the LLM *reconcile judgment* can still rot into "framework intelligence in prompt-land" (ZFC:45) if over-built. The disagreement is about **how much to build now**, not whether the pieces are sound.

---

## 7. Safety prerequisites (independent of the redesign — must-fix either way)

These block *any* trustworthy wind-down and are not controversial:
- **ctx-watchdog is sleep-blind** — the Sonnet 300K governor will force-restart a parked crew, un-sleeping it and billing tokens. Teach it (and every restart authority) to skip a session with a live `.sleeping.*` marker.
- **Captain wake-pane mis-resolution** — wake targets a hard-coded `…-captain:0.0`; the live captain is sometimes bare `captain`. Resolve via `ResolveTmuxTarget` + liveness probe.
- **Daemon-restart orphans markers** — no startup reconcile of `.sleeping.*`; a daemon death mid-sleep can strand sessions. Add a boot-time marker reconcile.
- **Marker needs `source` + `level` fields** — so an operator PARK isn't auto-woken by a stray `queue submit` (operator intent outranks the drain reflex).
- `IsSleeping` is fail-open on empty sessionID (`gates.go:155`) — interacts with the known "SID flips on /clear" drift.

---

## 8. Open decision forks for the operator

1. **Oracle: delete vs demote.** Recommendation: **demote** (§4) — keep the 5 hard-won defenses as a fact-tool + a one-directional veto, delete only the ~14-line auto-park decision. (Your "revert it" instinct is satisfied on the *decision*; the *facts* are kept as a tool, which is the ZFC-correct reading.)
2. **Auto-quiesce: should Go EVER initiate sleep on mechanical drain?** Lens D said keep a Go auto-quiesce on true drain; A/E + the generative-work argument say **no — Go never initiates, captain always does.** Recommendation: **no auto-initiate.** Accept that a dead/lost captain won't self-sleep (your explicit out-of-scope case; keeper/watchdog handle runaway tokens separately).
3. **Checker agents (you said you started these).** Lens E argues against *periodic* checkers ("burn tokens to decide not to burn tokens") — but that's the worst-case framing. A checker that runs **infrequently / event-triggered** and reads the **cheap Go `harmonik state` snapshot** is viable and matches your direction. The **sentinel flywheel governor** (`internal/sentinel/governor.go` + `adversary.go`, built but **0 callers**) is exactly this pattern (Go governor → spawns a fresh-context LLM adversary on a trip) and is the natural substrate. Fork: revive the sentinel governor as the checker, or keep checking on the captain's own health tick only?
4. **State-machine scope:** build `harmonik state` + the fold + teardown **now** (small, directly serves wind-down), and defer the full DESIRED/reconcile loop to its own kerf work — vs. spec the whole reconcile model up front.
5. **Vocabulary/rename** (quiesce vs sleep) — touches a shipped CLI; low-risk but worth a nod.

---

## 9. Recommended sequencing (matches the operator's lean)

> "Stop doing dumb stuff (oracle), then model the system, then replicate that model internally."

- **Phase 0 — Safety prerequisites (§7).** Independent, must-fix, unblocks trust. Small.
- **Phase 1 — Stop the dumb stuff.** Demote the oracle (§4): delete the auto-park tick, reshape to `GatherDrainFacts`, re-cast non-force `sleep` as veto-on-execute, fix the ON-008 spec. This is the "stop doing dumb stuff" step and it's genuinely small (~14 lines + wiring + spec).
- **Phase 2 — Model the system.** `harmonik state [--json]` (the fact snapshot + live-session picture) + the system-state fold (§6.1, §6.3). This is the "print the system status" capability and it feeds both the captain's sleep decision and the keeper/watchdog.
- **Phase 3 — Workflows + teardown.** Implement the `--level` spectrum, the W1/W3/W4 sequences, teardown-as-transition (§5, §6.4). Spec first (`specs/park-resume-protocol.md` extension + a new `specs/system-state.md`).
- **Phase 4 (optional, gated on Phase 2 value) — Desired-state reconcile loop.** Only if Phase 2 shows the fact snapshot isn't enough. Its own kerf work, judged on its own merit (per E's warning).

**Decline for now:** the fully-LLM-autonomous *daemon-driven* sleep policy, and any *constantly-polling* checker.

---

## Appendix — evidence map

- ZFC: `docs/concepts/zero-framework-cognition.md` (lines 21–26 four-step, 29 allowed, 32/41/45 anti-patterns, 51–56 the resolution, 65 over-application warning).
- Oracle decision (remove): `internal/daemon/quiesce.go:282–295` (auto-park tick), `:288`/`:535` (verdict-as-control), `daemon.go:1769–1785` (wiring).
- Oracle facts (keep as tool): `internal/daemon/draindetect.go` + `draindetect_epic.go`; 14 tests in `draindetect_test.go`.
- Plumbing (keep): `quiesce.go` park/wake/markers/failsafe; `cmd/harmonik/sleepwake.go`; keeper gate `internal/keeper/gates.go:155`.
- State-machine leaves (leverage): `internal/handlercontract/lifecycle/{types,table,machine}.go`, `QueueStore`, `RunRegistry`, `internal/crew/registry.go`, `internal/keeper/{gauge,tmuxresolve}.go`, `scripts/captain-boot-digest.sh`.
- Checker substrate (unwired): `internal/sentinel/governor.go` + `adversary.go` (epic `codename:flywheel`); live alert-only `scripts/ops-monitor-check.sh`.
- Spec touch-points: `specs/operator-nfr.md` (ON-008/ON-010), `specs/park-resume-protocol.md`, suggested new `specs/system-state.md`.
- Safety prereqs: `scripts/ctx-watchdog-launch.sh` (sleep-blind), `quiesce.go:304` (wake pane target), startup marker-reconcile (absent), marker `source`/`level` fields (absent).
