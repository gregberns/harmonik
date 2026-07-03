# Research — A3: `specs/operator-nfr.md` (pause/resume command verb + `operator_pause_status` producer)

**Component requirement (from 02-components.md):** define the agent-callable pause/resume command verb and its producer of `operator_pause_status`/`operator_resuming`, mapped onto the EXISTING ON pause/resume state machine (not a parallel one). Maps to G3 (S9). A3 is the upstream producer; A1/A2/A4 depend on it.

All anchors re-verified against the live tree on 2026-05-31.

## Research Questions

1. Does ON already define the pause/resume state machine and the `operator_pause_status` event, or is the producer side genuinely undefined?
2. What is reserved-but-unwired in the code (`CommandPause`), and what does its spec-ref point to?
3. Is the producer's transport already constrained by the spec (CLI -> Unix-socket RPC vs signal), resolving OQ2?
4. What re-emit / paired-phase discipline must the producer honor so it does not invent a new event type (C4)?
5. Is there an "agent-callable" requirement already, or must A3 add the explicit "Pi can issue without a human" obligation?

## Findings

### F1 — The ON pause/resume STATE MACHINE and the `operator_pause_status` event ALREADY exist and are fully normative; what is missing is the COMMAND VERB that triggers them from an agent

This is the pivotal finding: the assessment's "operator-nfr possibly touched" hedge resolves to **definitely touched, but lighter than feared** — the hard part (the state machine + event semantics) is already specced; A3 adds the *entry point*.

- **State machine** (`operator-nfr.md:241`, ON-011): *"States are `running`, `pausing`, `paused`, `resuming`, `stopped` ... and `upgrading`. ... Transitions and emitted events are normative per §7.1."*
- **Pause respects between-task invariant** (`:211`, ON-008): a `pause` issued while `ready` MUST NOT interrupt in-flight runs; transition to `pausing`, drain via ON-027's eight steps, then `paused`. The `pausing -> paused` gate is drain-completion.
- **Event emission per transition** (`:256`, ON-013): *"The daemon MUST emit one typed event per operator-control state transition"* — `operator_pause_status{status: pausing}` on `running -> pausing` (`:258`), `operator_pause_status{status: paused}` on `pausing -> paused` after all ON-027 drain steps (`:259`), `operator_resuming` on `paused -> resuming` (`:260`).
- **Events produced** declared in the envelope block (`:95-98`): `operator_pause_status` (paired-phase), `operator_resuming`.

**So the PRODUCER OBLIGATIONS already exist as spec text.** The genuine A3 gap is narrower than "define the producer": it is **(a) wire a `pause`/`resume` COMMAND VERB that an agent (Pi) can issue, and (b) state that issuing it is what drives the already-specced `running -> pausing -> paused` transition (and thus the already-specced emission).** A3 is connecting an existing command-code stub to an existing state machine, plus adding the agent-callable obligation.

### F2 — `CommandPause` is reserved in code and its spec-ref already points at ON §4.3 ON-007..010 — but it is unwired and `supervise` exposes no pause verb

- **`internal/operatornfr/commandcodes.go:34`** (verified): `CommandPause CommandName = "pause"` with comment *"`harmonik pause` requests the daemon to enter the paused state after completing in-flight runs. Spec ref: operator-nfr.md §4.3 ON-007 – ON-010."* So the code *already names* `harmonik pause` (top-level) AND cites the ON requirements — but the constant is reserved-but-unwired (no command path reaches it).
- **`cmd/harmonik/supervise_cmd.go`** (verified): verb switch is `start/stop/status/attach/restart/logs/_shim`; default-case error lists exactly those — **no `pause`/`resume`.**

**Naming tension (OQ1, cross-cutting with A2/F4):** the code constant says `harmonik pause` (top-level); CL-080 (cognition-loop `:163`) says `harmonik supervise pause/resume`. A3 must pick the canonical form. **Recommendation: canonical = `harmonik supervise pause/resume`** (CL-080's form, which PL §4.9 + ON §4.3 already reference and which keeps all daemon-lifecycle verbs under one `supervise` umbrella), with the bare `harmonik pause` retained only as the existing `CommandName` wire value the RPC carries. This avoids a parallel surface (C4/N3) and matches the existing CL-080 + CL-090 wording. A3 should reconcile the `commandcodes.go:34` comment to the canonical CLI form.

### F3 — Transport is already constrained toward CLI -> Unix-socket RPC; OQ2 resolves to RPC, not signal

ON-013a (`:275`) enumerates the operator-command-dispatch goroutines that MUST install a panic barrier: *"the goroutine handling `pause`, `stop`, `upgrade`, `attach`, and the `queue-submit`/`queue-append`/`queue-status`/`queue-dry-run` JSON-RPC methods per [process-lifecycle.md §4.1 PL-003a]."* This **groups `pause` with the JSON-RPC queue methods** — i.e. the spec already frames `pause` as a daemon-socket RPC command on the same PL-003a transport as `queue-submit`. This resolves OQ2 in favor of **CLI -> daemon Unix-socket RPC (PL-003a)**, consistent with the queue verbs and the agent-callable path (an agent shells `harmonik supervise pause`, the CLI frames a PL-003a RPC to the daemon). The signal alternative is dispreferred — signals can't carry the `pause_reason` discriminator (ON-012) and don't fit the agent-callable, structured request shape.

### F4 — The producer MUST reuse the existing paired-phase `operator_pause_status` event with the `pause_reason` discriminator — inventing a new event type is explicitly retired

- **Paired-phase rule** (`:256-259`, ON-013): pause's `pausing`/`paused` are two phases of ONE event type `operator_pause_status` with `status ∈ {pausing, paused}`; *"emitted on each status transition, forbidden as keepalive re-emission"* (re-emit-once discipline). The note (queue-model `:335`) confirms `operator_pausing`/`operator_paused`/`operator_stopping` *"do not exist as Go EventTypes; the consolidated `operator_pause_status` with a `status` enum covers pausing and paused phases."*
- **Discriminator** (`:250`, ON-012): improvement-pause and operator-pause share the identical state table; the `pause_reason ∈ {operator, improvement}` field on `operator_pause_status` distinguishes them; *"The earlier framing of separate `improvement-pausing`/`improvement-paused` states is retired."*

**Implication (C4 satisfied by construction):** the A3 producer emits the *existing* `operator_pause_status` with `pause_reason=operator` — it invents **no new event type and no new state**. The agent-callable `pause` is just another origin tagged `pause_reason=operator`, structurally identical to the improvement-pause path. This is the cleanest possible landing and is exactly what the decomposition's constraint C4/N3 demands.

### F5 — There is no explicit "agent-callable" obligation yet; A3 must add it, and ON-027/ON-010 carve-outs are inherited unchanged

- ON-007..010 describe pause as an *operator* command; nothing states a *non-human agent* (Pi) may issue it. A3's net-new normative bit is the **explicit agent-callable requirement**: Pi can issue `harmonik supervise pause`/`resume` without a human, over the PL-003a RPC, and it drives the same state machine. This is the actual S9 closure.
- **Drain ordering inherited, not changed (C4):** ON-027 (`:441`, eight drain steps, `:463` strictly-sequential single-goroutine + per-step durable marking) and ON-010 (`:235`, reconciliation carve-out: pause issued during `reconciling` is queued and applied at the boundary) are **inherited** by the producer. The producer triggers the transition; the drain machinery is unchanged. A3 must state "emit into ON-027, do not alter ON-027" (matches QM-054's "drain pseudocode owned by ON-027, not duplicated here").
- **Single source of pause truth:** the `operator_pause_status` the A3 producer emits is what BOTH the queue consumer (A4/QM-054) AND the br-ready fallback gate (A1) observe. A3 owns the producer; A1 and A4 are read-side consumers of its output.

## Patterns to Follow

- **Wire, don't invent.** The state machine (ON-011), the events (ON-013), the drain (ON-027), and the discriminator (ON-012) all exist. A3 adds the command verb + the agent-callable obligation and connects them to the existing machine. No new event type, no new state (C4, N3).
- **Canonical verb = `harmonik supervise pause/resume`** (CL-080 form); reconcile `commandcodes.go:34`'s `harmonik pause` comment and keep `pause`/`resume` as the RPC `CommandName` wire values.
- **Transport = PL-003a Unix-socket RPC** (OQ2 resolved), co-located with the queue verbs per ON-013a; defer transport framing to PL-003a, do not duplicate.
- **Emit `operator_pause_status{status, pause_reason=operator}`** reusing the paired-phase + re-emit-once discipline (ON-013); `operator_resuming` on resume.
- **Reference ON-027/ON-010, don't modify** (matches QM-054's ownership boundary).
- **Conformance scenario** (SC3) attaches to the new command requirement: "Pi issues pause -> no new `run_started` while in-flight runs reach terminal -> resume -> dispatch resumes" — follows ON §10/§4 prose-obligation style.

## Risks / Conflicts

- **R1 (naming, OQ1) — needs a decision the change-design must record.** Code constant says `harmonik pause` (top-level), CL-080 says `harmonik supervise pause/resume`. Recommendation above is `supervise pause/resume` canonical + bare `pause`/`resume` as wire `CommandName`. This is the one cross-cutting decision (touches A2 CL-080 and the `commandcodes.go` comment); change-design must pick it once and propagate. Not a blocker — the lean is clear.
- **R2 (improvement-pause coexistence).** ON-012 already routes improvement-pause through the same state table with `pause_reason=improvement`. The agent-callable operator pause (`pause_reason=operator`) must not collide with an in-flight improvement-pause. The state machine is shared by design, so coexistence is handled — but change-design should note the `pause_reason` tagging at the new producer's emission site (the emission site "is responsible for tagging the pause reason," ON-013 `:258`).
- **R3 (drain-summary payload coordination).** ON-013 (`:259`) carries a cross-spec coordination *request to EV* to extend §8.7.6 with `drain_summary?`. The A3 producer's `status=paused` emission is expected to include it. If EV has not yet landed that field, the producer emission may need a graceful-degradation note. Verify event-model §8.7.6 state before finalizing; low risk (optional field).
- **R4 (queue-method-method vs operator-command surface).** ON-013a groups `pause` with `queue-submit` etc. as PL-003a RPC methods, but pause is an *operator-control* command (ON §4.3) while queue verbs are *queue-model* methods. They share transport, not ownership. Change-design must keep the verb's ownership in ON (operator control) while reusing the PL-003a transport — do not let the queue-model own the pause verb.
- **R5 (no blocker).** With OQ1 leaning `supervise pause/resume` and OQ2 resolved to RPC, no open question prevents writing the A3 change design. A3 is the upstream piece (A1/A2/A4 depend on it); it should be authored first per the decomposition order.
