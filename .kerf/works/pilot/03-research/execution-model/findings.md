# Research — A1: `specs/execution-model.md` (quiet daemon + pause-gated fallback)

**Component requirement (from 02-components.md):** define a no-auto-pull / queue-only daemon topology; reconcile the `br.Ready()` fallback with the existing "MUST NOT fall back to br ready" language; gate the fallback on operator-pause state. Maps to G1 (S2 quiet daemon) + G3 (S9 br-ready pause gate).

All anchors below were re-verified against the live tree on 2026-05-31.

## Research Questions

1. What does the spec already say about the `br ready` fallback, and where exactly does it contradict the live code?
2. Does the spec already define a quiet/queue-only topology or an operator flag for it, or is this genuinely net-new normative text?
3. What does the live workloop actually do on a bare boot (the incident mechanism), and where is the gate point?
4. Is the `br-ready` fallback path already gated on *any* pause state today, and where would an operator-pause gate attach?
5. What conformance-scenario pattern does execution-model.md use, so SC1 ("boot, submit nothing -> zero `run_started`") follows house style?

## Findings

### F1 — The spec ALREADY says "MUST NOT fall back to br ready"; the code contradicts it (the gap is reconciliation, not invention)

The contradiction the assessment flagged is real and is **internal to the spec-vs-code seam**, not a missing spec:

- **Spec side (forbids fallback):** `execution-model.md:1395` — the §7.4 steady-state pseudocode: `IF queue IS None: idle_wait_for_queue_submission(); CONTINUE -- daemon polls submission socket; MUST NOT fall back to br ready`. Reinforced at `:1647` (Core MVH conformance): *"Dispatch input MUST be the active queue per §7.4; daemon fallback to `br ready` is non-conforming."* And the 0.5.0 changelog (`:1759`): *"§7.4 main-loop replaces `ready_beads -> pick_one` block with a queue-pull block ... daemon MUST NOT fall back to `br ready`."*
- **Code side (does fall back):** `internal/daemon/workloop.go` — when `queueItemIndex < 0` the comment reads *"No queue active — fall back to br-ready poll"* and the code calls `deps.brAdapter.Ready(ctx)`, then `beadRecord = readyRecords[0]` and dispatches. (Assessment anchor `workloop.go:833-858`; verified present.)

**Implication for design:** the spec's normative posture is *already* "no auto-pull." The live binary is simply non-conforming. A1's job is **not** to invent a quiet topology from scratch — it is to (a) make the prohibition's *exception* explicit (the historical single-daemon topology that legitimately wants auto-pull, constraint C3), (b) name the operator flag selecting each topology, and (c) reconcile the §7.4 pseudocode so the fallback branch is reachable *only* under the opt-in flag. The decomposition's framing is exactly right and minimal.

### F2 — No quiet-topology flag exists in the spec yet; `--max-concurrent` (EM-051) is the model to follow

Grepping `execution-model.md` for `no-auto-pull` / `queue-only` returns **zero hits**. The only startup-sealed knob with a normative requirement is `max_concurrent` (EM-051, `:812`: *"`--max-concurrent` ... sealed at daemon startup ... MUST NOT be re-read for the lifetime of the daemon process"*). So the flag named in hk-exd7m (`--no-auto-pull` / `--queue-only`) is **net-new**, with a clear precedent for how to spec a startup-sealed daemon flag: model it on EM-051 (startup-time, sealed, named flag, default stated, §10.2 conformance test). The flag's *CLI transport* is owned by `process-lifecycle.md §4.1` per the EM-051 cross-ref; A1 names the flag + default and defers CLI-surface wording to a PL cross-reference.

### F3 — The live workloop's bare-boot behavior IS the incident mechanism; the gate point is the `queueItemIndex < 0` branch

The assessment's root cause holds against current code. Steady loop: compute `queueItemIndex`; when no active queue, take the "fall back to br-ready poll" branch, pick `readyRecords[0]`, dispatch up to the capacity gate (EM-049 at `workloop.go:~596`, verified — `if deps.runRegistry.Len() >= effectiveMax { ... continue }`). There is **no flag check** between "no queue" and "poll br ready." That single branch is where the topology gate lands: under the quiet topology `queueItemIndex < 0` must route to `idle_wait_for_queue_submission()` (the spec's existing `:1395` intent) instead of `brAdapter.Ready`. This makes the reconciliation a *one-branch* behavioral change — important for change-design altitude (don't over-build).

### F4 — The fallback path has NO operator-pause gate today; the queue-level pause (QM-054) cannot protect it

The §7.4 pseudocode gates pause in two places the fallback path **misses**:
- `:1390` `IF should_pause_between_runs(): wait_for_resume()` — the operator-nfr §4.3 pause-between-runs check at loop top. It *would* gate fallback dispatch, BUT it depends on a real producer driving it — exactly A3's gap (no producer emits `operator_pause_status` in production; `queue_operatoreventconsumer_7urls.go` is consumer-only). So the gate *exists in pseudocode* but is *inert in production*.
- `:1396` `IF Queue.status IN {paused-by-failure, paused-by-drain, completed}: idle_wait()` — gates the **queue** path on `paused-by-drain` (QM-054). The **br-ready fallback branch has no equivalent**: when `queue IS None` there is no queue status to check, so queue-level operator-pause (QM-054) gives zero protection on the fallback path. Confirms the decomposition's G3 sub-requirement: *"the br-ready fallback dispatch path is gated on operator-pause state — today only handler-pause gates it."*

**Design consequence:** A1 must state that when operator-pause state is `pausing`/`paused` (A3's producer output), the br-ready fallback (if enabled under the opt-in flag at all) MUST NOT dispatch. The single source of pause truth is A3's `operator_pause_status`; A1 *references* it (dependency A1 -> A3, soft).

### F5 — Conformance-scenario house style: §10.2 prose obligations keyed to requirement IDs

`execution-model.md §10.2` (e.g. `:1659`, `:1667`) writes conformance obligations as prose sentences keyed to requirement IDs ("Capacity-gate tests: with `max_concurrent = K` and a wave of N > K items, verify at most K runs are in-flight..."). SC1 ("boot daemon, submit nothing -> zero `run_started`") should be authored as a §10.2 obligation attached to the new quiet-topology requirement ID, in the same prose-scenario style — not a separate scenario-file format. The pause-gated-fallback scenario attaches to the gate requirement's ID.

## Patterns to Follow

- **Model the new flag on EM-051** (startup-sealed, named, default stated, §10.2 test). Do not invent a new knob-spec shape.
- **Reconcile, don't rewrite §7.4.** `:1395` already encodes the desired quiet behavior (`idle_wait_for_queue_submission`); the change is to make the *opt-in fallback* an explicit alternative branch under the flag, mark the default quiet (no fallback) for the flywheel topology, and preserve fallback under the flag for the historical single-daemon topology (C3).
- **Single source of pause truth:** reference A3's `operator_pause_status`; do not define a parallel pause concept in EM (C4/N3).
- **New requirement IDs only, no renumbering:** the EM changelog discipline (every entry ends "No prior IDs renumbered or retired") is strict; the quiet-topology requirement and the fallback-pause-gate requirement each get a fresh `EM-0NN` ID.

## Risks / Conflicts

- **R1 (default-flip blast radius, C3) — load-bearing.** Flipping the fallback off-by-default changes behavior for *any* existing bare `harmonik --project` daemon, not just the flywheel. The spec must scope the default flip to the topology (OQ3: lean topology-scoped) and explicitly preserve fallback for the historical single-daemon user via the flag, or it regresses shipped behavior. This is the one place the change reverses an effective default; change-design should state the default explicitly and the changelog must call it out.
- **R2 (cross-spec ripple).** EM §7.4 is cross-referenced by `queue-model.md §5` (group-advance) and EM-015f. The fallback-branch edit must keep `queue IS None` -> idle-wait coherent with QM-002's "in-memory authority loaded from queue.json." Low risk (the edit is *more* restrictive), but change-design should re-resolve the `:1396` queue-status-pause line against A4 so the two pause gates (queue-status pause vs operator-pause-between-runs) read as complementary, not redundant/contradictory.
- **R3 (inert-gate trap).** Gating the fallback on operator-pause is only meaningful once A3's producer exists. If A1 lands before A3, the gate references an unemitted event. The decomposition's authoring order (A3 -> A1) mitigates; flag it so change-design honors the order.
- **R4 (no blocker).** No unresolved question prevents writing the A1 change design. OQ3 has a clear lean (topology-scoped, preserves C3) and is change-design's to settle.
