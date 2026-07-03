# Change Design — A1: `specs/execution-model.md`

**Area:** A1 — quiet daemon (no-auto-pull / queue-only topology) + pause-gated br-ready fallback
**Maps to:** G1 (S2 quiet daemon — the incident root mechanism), G3 (S9 br-ready pause gate).
**Authoring order:** fourth (the pause-gate sub-part references A3's pause state).
**Research:** `03-research/execution-model/findings.md`.

---

## 0. One-line summary

Reconcile a **spec-vs-code contradiction** (the spec already forbids br-ready fallback; the live binary still does it) by making the fallback an explicit **opt-in** under a named startup-sealed flag, defaulting it **OFF for the flywheel topology** so a bare daemon boot dispatches zero runs (closes the incident root mechanism), and gating the fallback path — when enabled — on the **same** operator-pause state A3 produces.

## 1. Current state — what the spec says now

The contradiction the assessment flagged is real and internal to the spec-vs-code seam (research F1):

- **Spec forbids fallback (`:1395`, `:1647`, `:1759`).** §7.4 steady-state pseudocode: `IF queue IS None: idle_wait_for_queue_submission(); CONTINUE -- daemon polls submission socket; MUST NOT fall back to br ready`. §10.1 Core MVH conformance (`:1647`): *"Dispatch input MUST be the active queue per §7.4; daemon fallback to `br ready` is non-conforming."* The 0.5.0 changelog (`:1759`) records the §7.4 main-loop replacement.
- **Code DOES fall back (research F1/F3).** `internal/daemon/workloop.go:833-858`: when `queueItemIndex < 0` the comment reads "No queue active — fall back to br-ready poll", calls `deps.brAdapter.Ready(ctx)`, takes `readyRecords[0]`, and dispatches up to the EM-049 capacity gate (`workloop.go:~596`). **No flag check** sits between "no queue" and "poll br ready." This single branch is the incident root: 10 bare-daemon boots each auto-drained `br ready` into a fresh wave.
- **No quiet-topology flag exists (research F2).** Grepping the spec for `no-auto-pull` / `queue-only` returns zero hits. The only startup-sealed knob precedent is **EM-051** (`:810-814`): `--max-concurrent`, "sealed at daemon startup … MUST NOT be re-read for the lifetime of the daemon process", default stated, §10.2 conformance test. This is the model to follow.
- **Fallback has no operator-pause gate (research F4).** §7.4 gates pause in two places the fallback **misses**: `:1391` `IF should_pause_between_runs(): wait_for_resume()` (the ON §4.3 pause-between-runs check — but it depends on a real producer, which is A3's gap, so it is inert in production today); `:1396` `IF Queue.status IN {paused-by-failure, paused-by-drain, completed}: idle_wait()` (gates the **queue** path only — when `queue IS None` there is no queue status to check, so QM-054 gives **zero** protection on the fallback branch).

**What is NOT in the spec:**

1. **No named flag and no quiet-topology requirement.** The prohibition exists (`:1395`) but the *opt-in exception* (the historical single-daemon topology that legitimately wants auto-pull, constraint C3) is unspecified, and no flag selects between them.
2. **No fallback-pause gate.** When the fallback runs (under the opt-in flag), nothing in the spec says it must honor operator-pause.

## 2. Target state — what the spec should say after the change

A1 adds **two new EM requirement IDs** (fresh `EM-0NN`, no renumbering — research F-Patterns) and reconciles the §7.4 pseudocode. The change is deliberately **one-branch** at the code level (research F3): the `queueItemIndex < 0` branch.

### T1 — New requirement: no-auto-pull / queue-only daemon topology (`EM-0NN-A`, "quiet daemon")

A new requirement (proposed ID **EM-066**; final assigned at spec-draft against the live max) modeled on EM-051:

- The daemon MUST accept a startup-time **`--no-auto-pull`** flag (alias `--queue-only`; the canonical name is `--no-auto-pull`, final naming confirmed at spec-draft against hk-exd7m), a boolean **sealed at daemon startup**, MUST NOT be re-read for the daemon's lifetime (EM-051 discipline). The CLI transport defers to a `[process-lifecycle.md §4.1]` cross-reference (per the EM-051 precedent — EM names the flag + default, PL owns the flag-parsing surface).
- **When `--no-auto-pull` is set (the flywheel topology), the daemon MUST NOT fall back to `br ready`.** A bare boot with no submitted queue MUST take the `idle_wait_for_queue_submission()` branch (the existing `:1395` intent) and dispatch **zero** runs — no `run_started`, no claude spawn, no credit consumed — until a queue is submitted over the socket.
- **Default.** The flywheel/supervised topology defaults `--no-auto-pull` **ON** (quiet). The historical single-daemon topology that relies on auto-pull retains it by **explicitly NOT setting** `--no-auto-pull` (i.e. the flag's effective default is topology-scoped — see §4 Decision OQ3 and the changelog blast-radius callout). State the default explicitly so the changelog can call out the behavior flip (research R1).
- The §7.4 pseudocode is reconciled: the `queueItemIndex < 0` / `queue IS None` branch becomes a **two-way branch on the sealed flag** — `idle_wait_for_queue_submission()` when `--no-auto-pull` (default for flywheel), the `br ready` fallback **only** when the flag is unset (the opt-in historical path).

### T2 — New requirement: br-ready fallback is operator-pause-gated (`EM-0NN-B`)

A new requirement (proposed ID **EM-067**):

- When the br-ready fallback path is enabled (flag unset) AND the daemon's operator-control state is `pausing` or `paused` (per operator-nfr.md §4.3 — the state A3's ON-014a/ON-014b producer drives, surfaced via the `should_pause_between_runs()` / ON-030a marker), the fallback MUST NOT dispatch. It MUST take the idle-wait branch until `resuming`/`running`.
- **Single source of pause truth (research F4, constraint C4).** A1 does **not** define a parallel pause concept; it references A3's `operator_pause_status` / the operator-control state. The §7.4 `:1391` `should_pause_between_runs()` check is the existing hook; A1's edit ensures it gates the *fallback* branch as well as the queue branch (today it is structurally present at loop-top but the fallback branch can still reach `brAdapter.Ready` after it because the check was inert without a producer — A1 makes the gate explicit on the fallback path and notes it becomes effective once A3's producer lands).
- Reconcile the `:1396` queue-status-pause line with the new operator-pause gate so the two read as **complementary, not redundant** (research R2): `:1396` gates the *queue* path on `paused-by-drain` (QM-054); EM-067 gates the *fallback* path on operator-pause state. Together they ensure neither dispatch path runs while paused.

### T3 — Update §10.1 conformance + §10.2 test obligations (SC1)

- **§10.1 Core MVH.** The existing "daemon fallback to `br ready` is non-conforming" (`:1647`) is **refined**: fallback is non-conforming *when `--no-auto-pull` is set*; it is a conforming opt-in *only* when the flag is explicitly unset (historical topology). Add EM-066/EM-067 to the Core MVH requirement enumeration.
- **§10.2 test obligations** (prose-scenario house style keyed to requirement IDs, research F5):
  - **EM-066 (quiet daemon, SC1).** *"Boot a daemon with `--no-auto-pull`, submit no queue: verify zero `run_started` events are emitted and no claude/agent subprocess is spawned over a bounded observation window; verify the daemon sits in `idle_wait_for_queue_submission`. Boot without the flag (historical topology): verify the br-ready fallback dispatches `readyRecords[0]` as before."*
  - **EM-067 (pause-gated fallback).** *"With the fallback enabled (flag unset) and the daemon in operator-`paused` state, verify no new `run_started` is emitted from the fallback path while paused; on `resume`, verify fallback dispatch resumes."*

## 3. Rationale

- **Reconcile, don't invent (research F1).** The spec's normative posture is *already* "no auto-pull" (`:1395`, `:1647`). The live binary is simply non-conforming. A1's job is to make the prohibition's *exception* explicit (C3 historical topology), name the flag (EM-051 precedent), and make the §7.4 fallback branch reachable *only* under the opt-in flag. This keeps the change minimal and honors "don't add abstraction the user hasn't asked for."
- **One-branch behavioral change (research F3).** The entire incident root is the `queueItemIndex < 0` branch with no flag check. T1 turns that into a flag-gated two-way branch. Keeping the design at this altitude prevents over-building.
- **Default flip is the one load-bearing reversal (research R1).** Flipping the fallback off-by-default for the flywheel topology changes behavior for any bare `harmonik --project` daemon. Scoping the default to the topology (OQ3 lean) and preserving fallback under the flag for the historical user is what makes this safe; the changelog MUST call it out as a default flip.
- **Single source of pause truth (research F4, C4/N3).** Gating the fallback on A3's operator-pause state — not a new EM-local pause concept — keeps the queue path and the fallback path honoring one signal, closing the partial-pause hole (the daemon could be "paused" for the queue but still dispatch on fallback).

## 4. Decisions recorded

- **OQ3 (global vs flywheel-scoped default) → topology-scoped.** `--no-auto-pull` defaults ON for the flywheel/supervised topology; the historical single-daemon topology preserves auto-pull by not setting the flag. Preserves C3 backward-compat; the spec states the default explicitly and the changelog calls out the flip. (research R1.)
- **Flag name → `--no-auto-pull`** (canonical), `--queue-only` as alias; final confirmed against hk-exd7m at spec-draft. (research F2.)

## 5. Requirements traceability

| Goal / SC | Target state | New/changed requirement |
|---|---|---|
| G1 / S2 / SC1 — quiet daemon, zero dispatch on bare boot | T1 | **EM-066 (new)** + §7.4 pseudocode reconcile |
| G3 / S9 — br-ready fallback pause gate | T2 | **EM-067 (new)** + §7.4 `:1391`/`:1396` reconcile |
| SC1 conformance | T3 | §10.1 refine + §10.2 EM-066/EM-067 test obligations |

Every 02-components.md §A1 requirement is addressed:
- "daemon topology in which a bare boot dispatches zero runs; flag named" → T1 (EM-066).
- "contradiction between `:1395`/`:1647` and `workloop.go:833-858` resolved; fallback opt-in, off by default for flywheel, preserved for historical via flag" → T1 + T3.
- "br-ready fallback gated on operator-pause state" → T2 (EM-067).
- "conformance scenario for SC1 + pause-gated fallback" → T3.
No target lacks a driver: T1↔G1, T2↔G3, T3↔SC1.

## 6. Constraints honored

- **C3 (backward compat of the br-ready path).** The fallback is preserved as an opt-in for the historical single-daemon topology; the default flip is topology-scoped, not a removal.
- **C4 / N3 (single source of pause truth; no run-state-ownership change).** EM-067 references A3's operator-pause state; A1 defines no parallel pause concept and changes no reconciliation/run-state ownership.
- **New IDs only, no renumbering (research F-Patterns).** EM-066/EM-067 are fresh IDs; the EM changelog discipline ("No prior IDs renumbered or retired") is honored.

## 7. Risks / dependencies

- **R3 / inert-gate trap (research).** EM-067's gate is only *effective* once A3's producer (ON-014a/ON-014b) emits `operator_pause_status` in production. The 02-components.md authoring order (A3 → A1) mitigates; A1's spec text references A3's state by ID and notes the gate is structurally present + becomes effective when the producer lands. Not a blocker for the spec; flagged for the implementation sequencing in 07-tasks.
- **R2 / cross-spec ripple.** §7.4 is cross-referenced by queue-model §5 (group-advance) and EM-015f. The fallback-branch edit is *more restrictive* (low ripple risk); T2 re-resolves the `:1396` queue-status-pause line against A4 so the two pause gates read complementary. Verify at Integration.
- **Dependency:** A1 → A3 (soft; EM-067 references the operator-pause state A3 defines). A1's T1 (quiet daemon) is otherwise independent.
