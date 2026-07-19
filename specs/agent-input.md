# Agent Input Substrate

```yaml
---
title: Agent Input Substrate
spec-id: agent-input
requirement-prefix: AIS  # NOTE: prefix AIS MUST be reserved in specs/_registry.yaml in the same
                         # commit that lands this spec (registry lint rule). Both AIS and the
                         # spec-id `agent-input` are free as of 2026-07-14.
status: draft
spec-shape: requirements-first
spec-category: runtime-subsystem
version: 0.1.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-07-14
depends-on:
  - replay-substrate
  - process-lifecycle
  - handler-contract
  - session-keeper
  - event-model
  - workspace-model
  - claude-hook-bridge
---
```

## 1. Purpose

This spec defines the normative contract for the harmonik **agent-input substrate** — the typed input verb the process-spawn seam exposes so the daemon can deliver input to a spawned agent and receive a first-class acceptance signal, replacing the out-of-band tmux paste-injection mechanism whose only "ack" is a process exit code that does not mean the agent accepted the input.

It is normative for: the consumer-owned `InputPort` and its two-method surface; the `Ack` acceptance contract (delivery outcome + monotonic sequence + protocol token) and its dual synchronous-return-plus-emitted-event delivery; the bounded-liveness (output-or-stale) guarantee on every input submission — whose ack signal for the tmux/Claude path is SOURCED from the Claude-hook-bridge ([claude-hook-bridge.md]: `outcome_emitted` on `Stop`, `agent_ready` on `SessionStart` start/resume), not from pane-scraping and not from a Claude wire protocol; the structured-protocol driver — the Codex app-server driver, a second `Substrate` implementation hosted on the [replay-substrate.md §4] seam (a `codexwire` codec, a flat `Event`/`Action` vocabulary, and a pure `Step`); the structured driver's direct-child-stdio ownership and its observation-only tmux boundary (the tmux paste path remains Claude's first-class input transport, not observation-only); the handoff done-gate (`outcome_emitted` + artifact-present); and the best-effort live capture tee. It is the load-bearing home for input-acceptance correctness, which has no normative home in `specs/` today.

## 2. Scope

### 2.1 In scope

- The consumer-owned `InputPort` (`SubmitInput`, `CloseInput`), the widening of `SubstrateSession` to satisfy it structurally, and the retirement of the six type-asserted side-interfaces and the no-op adapter (AIS-001) plus the machine-enforced handler↔tmux boundary (AIS-002).
- The `Ack` contract — delivery outcome (`Delivered`/`Rejected`), monotonic input sequence, protocol acceptance token — and its dual synchronous-return + emitted-event delivery, composed as a front-stop in front of the existing async watchdogs; the async positive-acceptance signal for the tmux/Claude path is SOURCED from the Claude-hook-bridge (`outcome_emitted`/`agent_ready`), not pane-scraping (AIS-003, AIS-004, AIS-005).
- The structured-protocol driver — the Codex app-server driver — as the second `substrate.Run[E,A]` instantiation: the `codexwire` codec + method registry, the flat `Event`/`Action` vocabulary, the pure `Step`, and the type-alias seam instantiation (AIS-006). The codec is proven from the Codex app-server, which is subscription-compatible and DONE (AIS-007 RETIRED; AIS-008 RETIRED — see §4.3).
- Process ownership and stdio disposition: the driver owns the child's stdin/stdout pipes directly, and the `StdinDevNull` disposition under a real input protocol (AIS-009, AIS-010).
- The structured-driver observation-only tmux boundary (the tmux paste path stays Claude's first-class input; `capture-pane` is a human observation window only, never an ack) and the deletion boundary + keeper/CLI carve-out (AIS-011, AIS-012).
- The handoff done-gate for the tmux/Claude path — `outcome_emitted` (`Stop`) AND nonce'd-artifact-present-and-fresh, with the completed-vs-pending-question discrimination RESOLVED conservatively (no real-time discriminator; unmet gate falls through to the bounded-liveness timeout) (AIS-018, OQ-AIS-006).
- The pipes-only best-effort capture tee and the consumer-side persistence/redaction/corpus-layout/retention contract (AIS-013, AIS-014).
- The substrate-selection axis and twin-blindness, and the preservation of the remote seam (AIS-015, AIS-016).
- The WAL-guard adapt-not-delete fold-in (AIS-017).
- The two invariants: bounded liveness output-or-stale (AIS-INV-001) and capture-never-aborts-the-run (AIS-INV-002).

### 2.2 Out of scope

- The record→replay seam itself (`internal/substrate`, `EventSource[E]`/`Effector[A]`/`Run`, the replay `Twin`, the four fault modes, the `ClockPort` type) — owned by [replay-substrate.md §4]; this spec instantiates them.
- The daemon-run reactor (`runexec` `Step`) and its consumption of the ack contract — a CONSUMER of this spec, owned by the M3 work (see AIS-004 and §11).
- The remote/transport rebuild — a CONSUMER, owned by the M4 remote work; this spec only preserves the seam (AIS-016).
- The C5 acceptance harness/DoD (fault matrix, N-consecutive gate, forbidigo/depguard/grep gates) — its taxonomy is owned by the sibling harness-acceptance design and lands as DoD, not normative prose here; this spec supplies only the conformance obligation of AIS-INV-001 (§10).
- The C6 deletion of the retired input stack — gated behind the C5 bake window; DoD/teardown, not normative prose here (AIS-012).
- The keeper restart-cycle contract and its PL-021d paste path — owned by [session-keeper.md §4]; this spec records the carve-out and amends the cross-reference, and does not rebuild the keeper side (AIS-012).
- The event REGISTRATION mechanics (`EventType` consts, `mustRegister`, `PayloadCompatEntry`) for the `agent_input_*` events — owned by [event-model.md §8 (registration + taxonomy) + §6.3 (payloads)]; this spec owns only the *when* and the acceptance semantics.
- Codex-specific process lifecycle — the WAL-guard's post-M2 home is codex process lifecycle proper, not this agent-input driver spec (AIS-017).

#### AIS-000 — Disambiguation: this spec sits on the process-spawn "substrate" sense

This spec's "substrate" MUST be read as the **process-spawn seam** — `internal/handler.Substrate` / the [process-lifecycle.md §4 PL-021b] Substrate seam — which this spec extends with a typed input verb. It MUST NOT be conflated with the three other normative "substrate" senses in the spec tree: (1) the record→replay seam (`internal/substrate`, [replay-substrate.md]; see [replay-substrate.md §4.10 RS-023]) — which this spec *hosts the driver on* but is not *about*; (2) the cognition session substrate (the CL-015 / CL-024 "substrate teardown"); (3) the transport/production substrate ([credential-isolation.md §2.2] "LLM transport substrate", pi-harness PI-069). The `agent-input` spec-file name and the `AIS` prefix satisfy this disambiguation by construction; those specs keep their own "substrate" meanings intact and are not edited by this spec.

Tags: mechanism

> PLANNER-RECONCILE: the final filename/prefix (`agent-input.md`/`AIS` vs `agent-input-protocol.md`/`AIP`) is a small registry call. This draft proceeds with `agent-input.md`/`AIS`; the reservation lands in `specs/_registry.yaml` in the same commit.

## 3. Glossary

- **process-spawn seam** — `internal/handler.Substrate` (`SpawnWindow`) plus the `SubstrateSession` handle it returns; the boundary at which the daemon obtains and operates a spawned agent process without knowing whether it is tmux-hosted or driver-hosted. Distinct from the record→replay seam (AIS-000). (see §4.1)
- **InputPort** — the consumer-owned narrow port (`SubmitInput`, `CloseInput`) a session handle satisfies structurally; the typed input verb this spec introduces on the process-spawn seam. (see §4.1, §6.1)
- **Ack** — the signal `SubmitInput` returns: a delivery outcome, a driver-internal monotonic input sequence id, and an optional protocol-level acceptance token. (see §4.2, §6.2)
- **delivery outcome** — one of `Delivered` (the input was handed to the driver; the acceptance verdict arrives asynchronously as `agent_input_acked`) or `Rejected` (a protocol-level refusal — structured drivers only; tmux cannot produce one). Positive acceptance is NOT an outcome value — it is the async `agent_input_acked` event, uniform across tmux and the structured driver. (see §4.2, §8)
- **structured driver** — the second `substrate.Run[E,A]` instantiation: the **Codex app-server driver**, which speaks a real bidirectional protocol to the agent child over stdio — a `codexwire` codec + a flat `Event`/`Action` vocabulary + a pure `Step`. Proven and subscription-compatible; the whole agent-input architecture was modeled off it. It is NOT a claude driver — Claude runs first-class on the tmux path (§4.5). (see §4.3, §7)
- **codexwire codec** — the structured driver's stateful codec with a single method registry, an `Extra map[string]json.RawMessage` on every payload, and unknown-method-as-Raw passthrough. (see §4.3, §6.5)
- **front-stop** — the composition in which the synchronous `Ack` is an *earlier* positive acceptance signal placed IN FRONT of the pre-existing async watchdogs (`agent_heartbeat`, commit-poll), which it does NOT remove or weaken. (see §4.2)
- **capture tee** — the pipes-only best-effort `MultiWriter` splice that copies the driver↔child byte stream through to its consumer while teeing a byte-identical copy to a caller-supplied writer; owns no file format. (see §4.6, [replay-substrate.md §4.7 RS-016])
- **twin-blind** — the property that the structured driver cannot tell whether it is speaking to a real agent or a `harmonik-twin-codex` speaking the same NDJSON wire; the L2/L3 integration double substitutes at the wire, not via a test branch. (see §4.7)
- **bounded-liveness (output-or-stale)** — the property that every `SubmitInput` reaches exactly one terminal — an `Ack` or an emitted `agent_input_stale` event — within a bounded window; silence is forbidden. (see §5, AIS-INV-001)

## 4. Normative requirements

### 4.1 The seam input port and the retirements

#### AIS-001 — The `InputPort`

The system MUST declare a new consumer-owned narrow `InputPort` handler-side, following the [replay-substrate.md §4.1 RS-004] port idiom (a 1–3-method interface satisfied structurally, with no `Result`/`Option`/`Either` container in its surface). The port has exactly two methods:

- `SubmitInput(ctx, InputRequest) (Ack, error)` — delivers one input submission and MUST block until ack-or-stale within the bounded window of AIS-INV-001.
- `CloseInput(ctx) error` — signals end-of-input; it replaces, and MUST retire, the `substrateSessionAdapter.CloseStdin` no-op.

`SubstrateSession` MUST be widened so a session MAY satisfy `InputPort`; the daemon MUST obtain the port by structural assertion at the process-spawn seam. The six type-asserted side-interfaces (`enterSender`, `paneCapturer`, `quitSender`, `paneOutputSizer`, `paneLivenessChecker`, `commandRunnerProvider`) and the no-op `substrateSessionAdapter.SendInput` / `CloseStdin` MUST be retired as the input mechanism. The single-method `Substrate.SpawnWindow` MUST remain intact.

Tags: mechanism

> PLANNER-RECONCILE: whether the input verb lives on `SubstrateSession` directly vs a separately-asserted `InputPort` is a small placement call; this draft picks the separate narrow port. The interim tmux impl during the bake window satisfies `InputPort` via the existing paste path returning a `Delivered` ack whose acceptance is confirmed asynchronously by `agent_input_acked` on observed output (AIS-003), so both impls are swappable at one seam.

#### AIS-002 — The handler↔tmux boundary is a machine rule

The handler↔tmux boundary MUST be a machine-enforced depguard deny (the [replay-substrate.md §4.1 RS-005] leaf style) stating that `internal/handler` does not import `internal/lifecycle/tmux`, landed WITH the seam change. It MUST replace today's doc-comment-only "boundary," which is not machine-checkable. Once the C3/C6 boundary lands, the rule becomes import-expressible against the driver packages as well.

Tags: mechanism

### 4.2 The Ack

#### AIS-003 — `Ack` contents

`Ack` MUST carry exactly:

- (a) a **delivery outcome** — either `Delivered` (the input was handed to the driver; the acceptance verdict arrives asynchronously) or `Rejected` (a protocol-level refusal — structured drivers only; tmux cannot produce one). There is NO acceptance "class" and no capability hierarchy; the two input methods (tmux paste-driven; the structured Codex app-server driver) are PEERS, not tiers.
- (b) a driver-internal **monotonic input sequence id** (codec-owned per [replay-substrate.md §4.3 RS-008]; any per-submission sequence state lives inside the codec, never on the port surface);
- (c) an OPTIONAL **protocol-level acceptance token** — the turn id the input opened, when the wire protocol supplies one; absent otherwise.

Positive acceptance is NOT an `Ack` outcome value. It is the async `agent_input_acked` event — uniform across tmux and the structured driver — but the two peer paths SOURCE that signal differently:

- **tmux/Claude path — the ack signal is the Claude-hook-bridge event, NOT the pane.** The positive-acceptance signal is a structured Claude-hook-bridge event on the daemon bus ([claude-hook-bridge.md §4.5 CHB-013, §4.7 CHB-018]): **`outcome_emitted`** (the `Stop` hook — a turn completion) or **`agent_ready`** (the `SessionStart` hook, which the bridge synthesizes on BOTH a fresh start and a post-`/clear` resume; the relay does not distinguish the two — see `internal/hookrelay/hookrelay.go` `buildSessionStartMessage` / `buildStopMessage`). `agent_input_acked` is emitted when that expected hook signal is observed. It MUST NOT be derived from a `capture-pane` scrape (pane-scraping as an ack is DROPPED, AIS-011) and MUST NOT come from a Claude wire protocol (there is no structured Claude input driver — Claude runs first-class on tmux, §4.3/§4.5). Claude's transcript-tail (the keeper's `transcript_turn` source) is retained ONLY as SECONDARY corroboration, never as the primary ack.
- **structured (Codex) driver path — the ack signal is the wire protocol.** The positive signal is the codec's observed `InputAcked`/turn observation on the app-server wire (§7.1).

The interim tmux/paste implementation active during the bake window MUST return `Delivered` for a successful write; its acceptance is confirmed asynchronously by `agent_input_acked` when the expected hook signal (`outcome_emitted` / `agent_ready`) arrives, or reaches `agent_input_stale` on the bounded-liveness timeout (AIS-INV-001). It MUST NOT synthesize a positive acceptance it did not observe. `Rejected` MUST be returned only on a protocol-level refusal (which tmux cannot produce).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

#### AIS-004 — Dual signal: synchronous return AND emitted event

`SubmitInput` MUST return the `Ack` synchronously to the caller AND the driver MUST emit a durable `agent_input_acked`-class event on the event stream (and an `agent_input_stale`-class event on the timeout terminal per AIS-INV-001). The dual delivery MUST let an async observer see the ack on the event stream without compile-coupling this subsystem to any consumer's `State` type. The `agent_input_*` event payload STRUCTURES and their registration are owned by [event-model.md §8 (registration + taxonomy) + §6.3 (payloads)]; this requirement owns the *when* and the dual-delivery obligation.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

> PLANNER-RECONCILE (headline, contract-ownership direction): the planner brief stated M3 owns and defines this contract FIRST; the repo task graph (five concordant TASKS.md citations) states M2 owns it. This draft follows the repo — **M2 owns** the InputPort/Ack/bounded-liveness contract, and M3 (`runexec`) and M4 (remote) are consumers. The dual synchronous-return + emitted-event shape is direction-agnostic and survives either reconciliation. Confirm the ownership direction before either work implements.

#### AIS-005 — Front-stop, not replacement

The synchronous `Ack` MUST be an earlier positive acceptance signal placed IN FRONT of the pre-existing async watchdogs (`agent_heartbeat` per [handler-contract.md HC-057], the commit-poll). It MUST NOT remove or weaken those watchdogs. Dissolving the kill-ladder watchdog is a downstream consumer's concern (the M2→M3 cut line); this spec states the front-stop composition and does not rebuild the watchdog in the consumer.

Tags: mechanism

### 4.3 The structured-protocol driver

#### AIS-006 — Second `Substrate` impl over `substrate.Run[E,A]`

The structured driver is the **Codex app-server driver** — proven, subscription-compatible, and DONE; the whole agent-input architecture was modeled off it. It is NOT a claude driver (there is no structured claude driver — Claude runs first-class on the tmux path, §4.5). It is hosted on the [replay-substrate.md §4] seam as the codex quartet:

- a `codexwire` **codec** with a single method registry, an `Extra map[string]json.RawMessage` on every payload, and unknown-method-as-Raw passthrough (the [replay-substrate.md §4.5 RS-014] two-layer decode discipline: strict outer envelope, tolerant inner payload);
- a flat **`Event` / `Action`** vocabulary — `omitempty`, JSON-round-trippable structs, no vertical vocabulary leaking into the seam;
- a pure **`Step(ev) []Action`** — no goroutines, no I/O, returns `nil` (not an empty slice) when there is no action;
- a **`Run`** one-liner over `substrate.Run(ctx, src, Step, eff)`, timed exclusively via [replay-substrate.md §4.6 RS-015] `ClockPort` (never wall-clock).

Seam instantiations MUST use type aliases (`=`), not defined types, per the type-alias re-instantiation clause of [replay-substrate.md §4.9 RS-021]. The codex app-server protocol is proven (subscription-confirmed): an `initialize` handshake → a `turn/start` input frame → streamed `*/delta` → a `turn/completed` terminal, with steer/interrupt.

Tags: mechanism

#### AIS-007 — RETIRED per COORD c019: codec is proven from Codex; no claude capture spike

RETIRED per COORD c019 (2026-07-14, operator-ratified). The structured driver is the Codex app-server driver, whose input framing is already proven from the codex app-server — there is no speculative "claude headless" capture spike to gate a codec freeze. The corpus-first discipline (record real wire, freeze the codec against it) still applies to the codex codec as ordinary practice, but it is not gated on proving an unproven claude input side.

Tags: mechanism

#### AIS-008 — RETIRED per COORD c019: Codex is subscription-confirmed; no billing gate

RETIRED per COORD c019 (2026-07-14, operator-ratified). The billing gate existed to check whether a speculative claude headless `stream-json` mode would bill the Max subscription. The structured driver is the Codex app-server driver, which is already proven subscription-compatible — the gate is moot.

Tags: mechanism

### 4.4 Process ownership and stdio

#### AIS-009 — The driver owns the child's stdio directly

The structured driver MUST own the child's stdin and stdout pipes directly (the capture tee tees both), following the codex app-server pattern in which the daemon parses the child's stdout for protocol events. A real bidirectional protocol is incompatible with tmux owning the pty while the daemon reads the child's stdout; therefore the production input path MUST NOT route the child's stdio through a tmux pane. The driver reuses the existing house machinery — the `handler.Session` StdinPipe + `SendInput`/`CloseStdin` shape (the daemon's native NDJSON-to-child-stdin path) and the `apptap` pipe splice.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

#### AIS-010 — `SubstrateSpawn.StdinDevNull` disposition

The meaning of `SubstrateSpawn.StdinDevNull` under a real input protocol MUST be respecified. The structured driver keeps the child's stdin OPEN and driver-owned per harness; the flag MUST NOT force `< /dev/null` on a harness that speaks a real input protocol. The paste-harness case that today FORBIDS an open stdin is retired together with the tmux input stack (AIS-001); the codex `< /dev/null` case remains valid where no bidirectional input protocol is in use.

Tags: mechanism

### 4.5 Observation-only tmux boundary

#### AIS-011 — tmux is observation-only on the structured-driver input path

On the **structured (Codex app-server) driver's** input path the system MUST NOT issue `load-buffer`, `paste-buffer`, or `send-keys` — the codex driver owns the child's stdio directly (AIS-009), so tmux is not on that input path at all. At most `capture-pane` survives as a human-facing read/observation window. Observability of a structured-driver run MUST be satisfied by the C4 capture tee (tail the recorded wire) plus an OPTIONAL tmux observation window fed the captured stream. This is a boundary statement scoped to the structured driver; the [process-lifecycle.md §4 PL-021d] demotion detail is owned by the sibling process-lifecycle change. This does NOT demote the **tmux paste-driven path for Claude**, which per COORD c019 is a REAL, first-class input method (peer, not tier) — its `load-buffer`/`paste-buffer`/`send-keys` are the input mechanism, not a limitation to work around. On the Claude path the positive ACK is NOT read off the pane — it is the Claude-hook-bridge event (`outcome_emitted` / `agent_ready`, AIS-003). On BOTH paths `capture-pane` survives ONLY as a human-facing observation window and MUST NEVER be read as an acceptance/liveness SIGNAL; pane-scraping as an ack (today's `injectAndVerifySeed` `capture-pane` + `strings.Contains` heuristic) is DROPPED.

Tags: mechanism

> PLANNER-RECONCILE (locked-decision boundary, "tmux inspectability required"): per COORD c019, D5 (tmux inspectability) holds LITERALLY for Claude, which runs interactive-in-tmux; only the structured Codex driver observes via the capture tee (it owns raw stdio, not a TUI pane). No reinterpretation of the locked decision is needed for the Claude path.

#### AIS-012 — Deletion boundary and keeper/CLI carve-out

Write-verb retirement MUST be scoped to the **daemon-RUN input path**. The system MUST NOT delete the [process-lifecycle.md §4 PL-021d] `load-buffer` / `paste-buffer` / `send-keys` verbs that the keeper ([session-keeper.md §4.1 SK-002], off-daemon, session-id-addressed) and the CLI boot-seed/wake paths (`cmd/harmonik/{captain,crew,comms}.go`) depend on. This spec amends [session-keeper.md §4.1 SK-002]'s cross-reference rather than contradicting it; PL-021d is DEMOTED from the daemon-run input path but PRESERVED for keeper and the interactive-session-nudge use. The actual keeper reconciliation is owned by the sibling session-keeper change. Deletion of the retired input stack (C6) is gated behind the C5 bake window and is DoD/teardown, not normative prose here.

Tags: mechanism

> PLANNER-RECONCILE (keeper migrate-vs-carve-out): the ROADMAP task graph states keeper is MIGRATED to the structured driver before C6 deletes; [session-keeper.md §4.1 SK-002] (normative 2026-07-13) requires keeper's PL-021d paste, and keeper's architecture (off-daemon, session-id-addressed, no `SubstrateSession` handle) makes full migration architecturally awkward. This draft resolves the deletion hazard via carve-out + narrowed C6 scope (safe, unblocks M2) and defers migrate-vs-carve-out to the planner. Migration would add a session-id-keyed input-contract task and an SK-002 re-draft in the same motion.

### 4.6 Live capture tee

#### AIS-013 — Pipes-only, best-effort tee

The capture tee MUST be a pipes-only `MultiWriter` splice (a pipes-in constructor added to `apptap`); it MUST NOT force `Tap.Run`'s exec+Wait process ownership onto a driver that owns its own process. Per [replay-substrate.md §4.7 RS-016] the tee owns no file format and opens no file. Capture MUST be best-effort / degrade-to-uncaptured — an explicit reversal of `apptap`'s current fail-closed `MultiWriter` semantics — so a capture-disk problem MUST NOT abort a live agent run (AIS-INV-002).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=n/a; idempotency=idempotent

#### AIS-014 — Persistence, redaction, corpus layout, retention

Persistence and redaction MUST live in the CONSUMER, not in the tee. The corpus MUST land at `${workspace_path}/.harmonik/sessions/${session_id}/`, one file per direction, with a MECHANICAL `CAPTURE-LOG` ledger that is actually written per capture (the keeper `EXTRACT-LOG.md` is the executed model; a declared-but-never-written `CAPTURE-LOG.md` is the anti-pattern to avoid). Redaction MUST be a verbatim in-memory tee plus a value-pattern scrub ([handler-contract.md HC-032] style) in the persisting writer; the OUTPUT direction is the secret risk, and the INPUT direction is structurally secret-free per [handler-contract.md HC-028]. Retention MUST follow a keep-N / age-prune rule (the `brhistoryrotate` / `orphansweep` precedents); the corpus MUST NOT inherit the unrotated large-`events.jsonl` defect.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 4.7 Composition-root selection and remote seam

#### AIS-015 — Substrate-selection axis

The system MUST define a substrate-selection axis — a composition-root config/flag OR a key off the harness — that selects tmux-hosting vs structured-driver hosting, and that also works in the L2/L3 test harnesses. The structured driver MUST remain **twin-blind**: a `harmonik-twin-codex` speaking the same NDJSON wire on stdio is the L2/L3 integration double (per the twin-binaries decision), and a corpus-replay `Twin[E]` ([replay-substrate.md §4.3]) covers the unit tier. Substitution MUST be by which value is wired at the selection axis, not by a runtime test-branch (consistent with the [replay-substrate.md §4.8 RS-017] no-test-branch discipline).

Tags: mechanism

#### AIS-016 — Remote seam preserved

The new input driver MUST carry the `CommandRunner`/remote seam so remote workers do not regress: every retired side-interface has a `…Via(runner)` remote-SSH twin, and the driver MUST keep the seam remote-capable. The transport rebuild itself is a downstream consumer's concern (M4); this spec preserves the seam and does not rebuild the transport.

Tags: mechanism

### 4.8 WAL-guard fold-in

#### AIS-017 — Adapt, don't delete

The WAL-guard MUST be adapted, not deleted. It compensates for ungraceful SIGKILL of a stateful codex child (a leftover `state_*.sqlite-wal` corrupts the next launch), NOT for lost input; a real input ack does not by itself make it redundant. The structured driver MUST reduce the need via (a) graceful turn-termination (`turn/interrupt`) instead of SIGKILL and (b) positive fast-fail (no handshake within the bound → a structured launch-failure event instead of a silent exit-0). A boot-time recovery step MUST be retained for residual SIGKILL/crash paths; the per-launch symptom-treatment MUST be removed where graceful shutdown covers it. The WAL-guard is codex-specific — its post-M2 home is codex process lifecycle proper, not this agent-input driver spec.

Tags: mechanism

### 4.9 Handoff done-gate (tmux/Claude path)

#### AIS-018 — Handoff-completion is `outcome_emitted` AND artifact-present, not `Stop`-fired-once

The `Stop` hook fires at EVERY turn-end, not only at task completion; a single `outcome_emitted` therefore does NOT by itself establish that a handoff/restart cycle is DONE. The done-gate for handoff completion on the tmux/Claude path MUST be:

> **handoff-done = `outcome_emitted` (the `Stop` hook, [claude-hook-bridge.md §4.5 CHB-013]) fired  AND  the expected artifact is present.**

The artifact check MUST be a file-existence/fingerprint test evaluated at `Stop` time (the handoff/output file written to its expected path), or an equivalent `PostToolUse`-on-`Write` hook match on that path. This exact pattern ALREADY EXISTS: for the `reviewer` phase, `buildStopMessage` reads `${workspace_path}/.harmonik/review.json` at `Stop` and maps its contents into `outcome_emitted` ([claude-hook-bridge.md §4.5 CHB-014]; `internal/hookrelay/hookrelay.go`). The handoff-done gate is the same shape — `Stop` + read/confirm the expected file — applied to the keeper's restart/handoff cycle, whose "wait for the model to finish before `/clear`" step has NO interior implementation today (it leans on a flaky `.idle` marker + transcript parse). Wiring that gate to the `Stop` hook + artifact check is what closes the resume-hang class for the restart cycle.

**Discrimination — RESOLVED (2026-07-14, operator-ratified; see OQ-AIS-006).** For the keeper restart/handoff cycle the completion artifact is exactly `HANDOFF-<agent>.md` carrying this cycle's `<!-- KEEPER:<cycleID> -->` nonce, and "present" MEANS: that file exists, contains the current cycle's nonce, and its mtime is not before the nonce-confirmation instant — the same `!mt.Before(NonceConfirmedAt)` freshness anchor `pollAwaitModelDone` already applies to the `.idle` marker (`internal/keeper/shell.go`). The change is to key the `AwaitModelDone → Clearing` edge on `outcome_emitted` (`Stop`) + that nonce'd-artifact check rather than the flaky `.idle` mtime.

The completed-vs-pending-question case does NOT get real-time discrimination logic in M2, because two facts collapse it: (a) the handoff turn is a `/session-handoff` injection, not an open-ended prompt — it is not supposed to ask a question; and (b) the operator disables the interactive/plan-question behavior at source. A `Stop` that nonetheless carries no fresh nonce'd artifact is simply NOT-done: it falls through to the bounded-liveness timeout (AIS-INV-001; the keeper's fail-open `ModelDoneTimeout`, ~60s), which recovers rather than hangs. Conservative by construction — it can over-wait on a genuine pending-question `Stop`, never falsely `/clear` one. Future hardening (tune-later, NOT in M2 scope): a `PostToolUse`-on-`Write(HANDOFF-<agent>.md)` hook would give a synchronous lag-free "written this turn" positive (the bridge relays no `PostToolUse` today — it would add one hook kind), and the already-mapped `agent_heartbeat{phase:"waiting_input"}` (from `Notification{idle_prompt|permission_prompt}`) can serve as an out-of-band pending veto. Neither is required for the M2 gate.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

## 5. Invariants

#### AIS-INV-001 — Bounded liveness: output-or-stale, never silence

For every `SubmitInput`, the call MUST reach exactly one terminal — an `Ack` (`Delivered` or `Rejected`) OR an emitted `agent_input_stale`-class event — within a bounded window `InputAckTimeout + injection overhead`. Silence is FORBIDDEN. Structurally, every `TimerFired` edge in the driver's `Step` MUST land in a state with an outgoing action, so the machine cannot wedge silently.

**On the tmux/Claude path the awaited positive signal is the Claude-hook-bridge event** (`outcome_emitted` / `agent_ready` per [claude-hook-bridge.md §4.5 CHB-013, §4.7 CHB-018], AIS-003). When no such hook signal arrives within the window, the driver MUST emit `agent_input_stale` and the daemon recovers instead of hanging. **This is the resume-hang fix:** today's failure mode is "paste, assume success, wait forever"; the bound converts a missing hook signal into a recoverable terminal (`agent_input_stale`) so the daemon recovers rather than wedging on a `/clear`-resume that never produced its expected hook event. The window MUST be measured via [replay-substrate.md §4.6 RS-015] `ClockPort`, never wall-clock. This is the M2 peer of [session-keeper.md §5 SK-INV-005]; it is phrased in this subsystem's vocabulary and MUST NOT be stated by citing a plan-only construct. The daemon-run peer of this invariant is defined by the downstream M3 consumer.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

#### AIS-INV-002 — Capture never aborts the run

A capture-writer error (full disk, slow sink, redaction-scrub failure) MUST degrade the capture to uncaptured and MUST NOT back-pressure or abort the live agent input/output stream. The live stream's liveness MUST be independent of the capture side-channel.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

## 6. Schemas and data shapes

All signatures below are the pinned surface, restated verbatim. Types in use are Go standard-library types plus the vertical's type parameters `E` (event) and `A` (action). None of these surfaces is a versioned wire format; they are in-process Go interfaces except the `agent_input_*` event payloads, whose shape is owned by [event-model.md §8 (registration + taxonomy) + §6.3 (payloads)].

### 6.1 The `InputPort` (handler-side)

```go
// Consumer-owned narrow port (RS-004 idiom): 2 methods, no Result/Option/Either.
type InputPort interface {
    // SubmitInput delivers one input submission; blocks until ack-or-stale
    // within the bounded window (AIS-INV-001).
    SubmitInput(ctx context.Context, req InputRequest) (Ack, error)
    // CloseInput signals end-of-input; replaces the CloseStdin no-op.
    CloseInput(ctx context.Context) error
}
```

`SubstrateSession` is widened so a session MAY satisfy `InputPort`; the daemon obtains it by structural assertion at the process-spawn seam (AIS-001).

### 6.2 `InputRequest` and `Ack`

```go
type InputRequest struct {
    Payload    []byte // the input bytes to deliver to the agent
    TurnIntent string `json:"turn_intent,omitempty"` // e.g. new-turn vs steer/interrupt intent
}

type DeliveryOutcome int
const (
    Delivered DeliveryOutcome = iota // input handed to the driver; acceptance verdict arrives async (agent_input_acked)
    Rejected                         // protocol-level refusal (structured drivers only; tmux cannot produce one)
)

type Ack struct {
    Outcome DeliveryOutcome // delivery outcome (AIS-003a); no acceptance "class", no capability hierarchy
    Seq     uint64          // driver-internal monotonic input sequence id, codec-owned (AIS-003b, RS-008)
    Token   string          `json:"token,omitempty"` // protocol acceptance token (turn id), when supplied (AIS-003c)
}
```

`Ack` is the in-process Go return type of `SubmitInput`; its field names (`Outcome`, `Seq`, `Token`) are the authoritative in-process surface. `Ack` is the SYNCHRONOUS delivery-handoff result; positive acceptance is decoupled from it and travels as the async `agent_input_acked` event (AIS-004). That event carries NO acceptance "class" — its existence IS the positive ack; its serialized payload field names are owned by [event-model.md §6.3] and are `input_seq` / `acceptance_token` (the wire forms of `Seq` / `Token`). `Rejected` is a synchronous `Ack` outcome (structured drivers only), not an `agent_input_acked` class. The never-confirmed case reaches the distinct `agent_input_stale` timeout terminal (AIS-INV-001). [handler-contract.md §6.1] restates the seam-level record using the serialized names.

### 6.3 The flat `Event` / `Action` vocabulary

The driver's `Event` (wire → reactor) and `Action` (reactor → effector) values are flat, `omitempty`, JSON-round-trippable structs with no seam-leaking vocabulary. They include, at minimum, the input-submission lifecycle (submit, ack, stale-timeout) and the codec-supplied transport terminals:

```
Events:  Handshake{...}  TurnStarted{turnID, at}  Delta{turnID, ...}  TurnCompleted{turnID, at}
         InputSubmitted{seq, at}  InputAcked{seq, turnID, at}  InputRejected{seq, reason, at}
         TimerFired{kind, at}  ErrorEvent{msg}  DisconnectEvent{}
         -- InputAcked is the observed positive acceptance from the wire; TimerFired.kind includes at least input_ack_timeout

Actions: WriteInput{seq, payload}  CloseInput  Emit{type, payload}
         ArmTimer{kind, d}  CancelTimer{kind}  Interrupt{turnID}
```

`ErrorEvent`/`DisconnectEvent` are supplied by the codec ([replay-substrate.md §4.3 RS-009]), not by extending the four substrate fault modes.

### 6.4 `SubstrateSpawn.StdinDevNull` disposition

`StdinDevNull` retains its codex `< /dev/null` meaning only for harnesses with no bidirectional input protocol. For a structured-driver harness it MUST be interpreted as "keep stdin open and driver-owned" (AIS-010); the paste-harness "FORBID open stdin" case is retired with the tmux input stack.

### 6.5 The `codexwire` method-registry entry shape

Each codec method-registry entry maps one wire method name to a decoder that produces a flat `Event`, preserving unmodeled fields into `Extra` and mapping an unknown method to a typed-raw skip:

```go
type MethodEntry struct {
    Method string // wire method name, e.g. "turn/completed"
    // Decode: (ev, emit, err); an unknown method returns emit=false (skip), never a crash.
    Decode func(raw json.RawMessage) (Event, bool, error)
}
// Every decoded payload carries: Extra map[string]json.RawMessage (unmodeled fields preserved).
```

### 6.6 Schema evolution

The in-process interfaces (`InputPort`, `Ack`, `InputRequest`, `Event`/`Action`) are not versioned wire formats. The `agent_input_acked` / `agent_input_stale` event payloads are versioned per [event-model.md §8 (registration + taxonomy) + §6.3 (payloads)] with the N-1 readable contract of [operator-nfr.md §4.5]. The codex app-server wire framing is already proven (subscription-confirmed); the codec is frozen against the real codex wire as ordinary corpus-first practice (AIS-007 RETIRED — there is no speculative claude capture spike).

## 7. Protocols and state machines

There are two machines: the codex app-server wire protocol (proven) and the driver `Step` state machine (bounded-liveness-bearing).

### 7.1 The codex app-server wire protocol (proven)

The structured driver speaks the proven, subscription-confirmed codex app-server turn-based framing:

```
initialize handshake        -- capability negotiation; the driver confirms input support
  → turn/start input frame  -- carries one InputRequest.Payload; opens a turn
    → streamed */delta       -- assistant output deltas for the open turn
  → turn/completed terminal -- the turn's terminal; supplies the acceptance token
steer / interrupt            -- mid-turn input or turn cancellation (Interrupt{turnID})
```

This framing is proven from the codex app-server; the codec is frozen against the real codex wire as ordinary corpus-first practice (AIS-007 RETIRED — no speculative claude capture spike).

### 7.2 The driver `Step` state machine

The pure `Step(Event) []Action` machine carries the bounded-liveness property (AIS-INV-001). Every `TimerFired` edge lands in a state with an outgoing action:

| State | Event | Action(s) | To |
|---|---|---|---|
| `Idle` | `InputSubmitted{seq}` | `WriteInput{seq}`, `Emit(agent_input_submitted)`, `ArmTimer(input_ack_timeout)` | `AwaitingAck` |
| `AwaitingAck` | `InputAcked{seq, turnID}` | `Emit(agent_input_acked{token=turnID})` (its existence IS the positive ack — no class), `CancelTimer(input_ack_timeout)` | `Idle` |
| `AwaitingAck` | `InputRejected{seq}` | `CancelTimer(input_ack_timeout)` — resolves the sync `Ack{Rejected}` (structured driver only; no positive event) | `Idle` |
| `AwaitingAck` | `TimerFired(input_ack_timeout)` | `Emit(agent_input_stale{seq})` | `Idle` |
| `AwaitingAck` | `ErrorEvent` / `DisconnectEvent` | `Emit(agent_input_stale{seq, reason})` | `Idle` |
| any | `TurnCompleted` / `Delta` | (output routing; capture tee) | (unchanged) |

The interim tmux/paste impl reaches `AwaitingAck`; a successful write resolves the sync `Ack{Delivered}`, and its positive acceptance is the async `agent_input_acked` emitted when the expected Claude-hook-bridge signal (`outcome_emitted` / `agent_ready`, AIS-003) is observed on the bus (or `agent_input_stale` on the AIS-INV-001 timeout) — it does not synthesize a positive acceptance it did not observe. No `TimerFired` edge may land in a silence-only state (AIS-INV-001).

## 8. Error and failure taxonomy

The acceptance/failure classes and their emitted events:

| Outcome / mode | Meaning | Emitted signal |
|---|---|---|
| `Delivered` (sync) | Input handed to the driver; the acceptance verdict arrives asynchronously. Uniform across tmux and the structured driver. | `Ack{Delivered}` returned synchronously. |
| positive acceptance (async) | An observed turn/output confirms acceptance — on the **tmux/Claude path** sourced from the Claude-hook-bridge `outcome_emitted`/`agent_ready` event (AIS-003), on the **structured driver** from the wire `InputAcked`. NOT an `Ack` class — the event's existence IS the ack. | `agent_input_acked` event (no class; carries `input_seq`/`acceptance_token`). |
| `Rejected` (sync) | Protocol-level refusal of the input (structured driver only; tmux cannot produce one). | `Ack{Rejected}` returned synchronously; no positive event. |
| stale (timeout terminal) | No positive ack within the bounded window (the never-confirmed wedge / resume-hang fix). | `agent_input_stale`-class event (AIS-INV-001). |
| launch-failure | No handshake within the bound. | Structured launch-failure event (AIS-017 fast-fail), not a silent exit-0. |
| disconnect | Child stdout closed mid-turn. | Codec `DisconnectEvent()` → `agent_input_stale`. |
| transport error | Fatal decode/transport failure. | Codec `ErrorEvent(msg)` → `agent_input_stale`. |

`ErrorEvent` and `DisconnectEvent` are supplied by the codec ([replay-substrate.md §4.3 RS-009]); they are NOT new substrate fault modes. The four vertical-neutral fault modes and their terminal-never-silence property are closed by [replay-substrate.md §4.4 RS-012] and MUST NOT be extended here.

## 9. Cross-references

### 9.1 Depends on

- **[replay-substrate.md §4.1 RS-004]** — the consumer-owned narrow-port idiom `InputPort` follows (AIS-001).
- **[replay-substrate.md §4.1 RS-005]** — the depguard leaf-rule style the handler↔tmux boundary follows (AIS-002).
- **[replay-substrate.md §4.3 RS-008 / RS-009]** — the codec-owned sequence state (AIS-003), and the codec-supplied `ErrorEvent`/`DisconnectEvent` terminals (§8).
- **[replay-substrate.md §4.4 RS-012]** — the four vertical-neutral fault modes, closed and not extended by this spec (§8).
- **[replay-substrate.md §4.7 RS-016]** — the capture-tee-owns-no-file-format contract the tee follows (AIS-013).
- **[replay-substrate.md §4.8 RS-018]** — the per-vertical minimum-artifact list and no-test-branch substitution the harness follows (AIS-015; §10).
- **[replay-substrate.md §4.8 RS-020]** — the replay-vs-baseline acceptance shape the conformance obligation follows (§10).
- **[replay-substrate.md §4.9 RS-021]** — the type-alias re-instantiation clause (applied here to the codex structured-driver instantiation, AIS-006).
- **[replay-substrate.md §4.10 RS-023]** — the disambiguation cited by AIS-000.
- **[process-lifecycle.md §4 PL-021b]** — the process-spawn Substrate seam this spec extends with a typed input verb (AIS-000, AIS-001).
- **[process-lifecycle.md §4 PL-021d]** — the `load-buffer`/`paste-buffer` write discipline demoted from the daemon-run path but preserved for keeper/CLI (AIS-011, AIS-012).
- **[handler-contract.md HC-054 / HC-056 / HC-057 / HC-069 / HC-070 / HC-071 / HC-INV-008]** — the seam input-method-and-ack family the `InputPort`/`Ack` widening lands against: HC-069 (InputPort verb), HC-070 (Ack contents + emitted event), HC-071 (machine-enforced seam inversion), HC-INV-008 (bounded input liveness); HC-057 is the `agent_heartbeat` watchdog the front-stop composes in front of (AIS-005). (NOTE: the design doc's provisional HC-058/059/060 numbers were stale — those IDs are already live in handler-contract §4.2a — so the seam-input family landed as HC-069/070/071 per ID-FREEZE.)
- **[handler-contract.md HC-028 / HC-032]** — the input-is-structurally-secret-free property and the value-pattern scrub the persisting writer follows (AIS-014).
- **[session-keeper.md §4.1 SK-002]** — the keeper `PanePort.Inject` PL-021d dependency this spec carves out and whose cross-reference it amends (AIS-012).
- **[session-keeper.md §4.10 SK-021 / §5 SK-INV-005]** — the bounded-liveness (SR9) peer AIS-INV-001 mirrors in this subsystem's vocabulary.
- **[event-model.md §8 (registration + taxonomy) + §6.3 (payloads)]** — the registration and payload shapes for the `agent_input_acked` / `agent_input_stale` events (AIS-004); this spec owns only the *when*.
- **[claude-hook-bridge.md §4.5 CHB-013 / §4.7 CHB-018 / CHB-020 / §4.5 CHB-014]** — the NORMATIVE ack-signal source for the tmux/Claude path: `agent_ready` (`SessionStart`, fresh start AND post-`/clear` resume) and `outcome_emitted` (`Stop`, turn completion) are the positive-acceptance signals AIS-003 / AIS-INV-001 source (never a `capture-pane` scrape, never a Claude wire protocol); and the `Stop` + `.harmonik/review.json`-read (`buildStopMessage`, CHB-014) is the done-gate precedent AIS-018 cites.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand by walking every spec's `depends-on` list; they are not stored here. The M3 `runexec` reactor and the M4 remote transport are the consumers of this spec's InputPort/Ack/bounded-liveness contract (AIS-004, AIS-016).

### 9.3 Co-references (read-only consumption)

- **[operator-nfr.md §4.5]** — the N-1 readable schema-compatibility window applied to the `agent_input_*` payloads (§6.6); no reverse dependency.

## 10. Conformance

### 10.1 Conformance profiles

- **Core agent-input (this phase)** — all of AIS-000…AIS-017 (with AIS-007/AIS-008 RETIRED per COORD c019) and AIS-INV-001…AIS-INV-002 are in force; an implementation conforms when each passes. The interim tmux/paste impl conforms only if it returns `Delivered` (never a synthesized positive acceptance it did not observe) and its acceptance reaches a bounded-liveness terminal — `agent_input_acked` on observed output or `agent_input_stale` on timeout (AIS-003, AIS-INV-001).
- **Consumer obligations (deferred, other works)** — the daemon-run peer of AIS-INV-001 (M3), the remote transport rebuild (M4), and the C6 deletion of the retired input stack are not part of this spec's core conformance claim.

### 10.2 Test-surface obligations

- **AIS-INV-001 is the load-bearing conformance obligation.** Its output-or-stale property MUST be proven by a `FakeClock` virtual-time fault-injection test across the four [replay-substrate.md §4.4 RS-012] fault modes, an N=10 consecutive-clean-cycles gate, and an out-of-band `jq`/`grep` oracle (the harness MUST NOT grade itself, per [replay-substrate.md §4.8 RS-020]).
- **SC6 "zero sleeps / zero capture-pane scraping" is a DoD gate**: a forbidigo path-scoped ban of `time.Sleep` AND `time.After`/`time.NewTimer` in the driver packages, a depguard deny of driver → `internal/lifecycle/tmux` (import-expressible once C3/C6 land, AIS-002), and a grep gate for `capture-pane` in the driver path.
- The C5 harness taxonomy (tier files, fault matrix, canary, Makefile pair, env gate) is owned by the sibling harness-acceptance design and lands as DoD; this spec supplies only the AIS-INV-001 conformance obligation above.

### 10.3 Excluded conformance claims

- This spec grants no conformance over the concrete codex app-server wire framing (proven from Codex; AIS-007 RETIRED), nor over the codec's per-method decoders (vertical detail).
- It grants no conformance over the event registration or payload schemas for `agent_input_*` (owned by [event-model.md §8 (registration + taxonomy) + §6.3 (payloads)]).
- It grants no conformance over the keeper restart-cycle contract or its PL-021d paste path (owned by [session-keeper.md §4]).
- It grants no conformance over the C5 harness DoD or the C6 deletion teardown (sibling works).

## 11. Open questions

#### OQ-AIS-001 — RESOLVED/RETIRED per COORD c019: no structured claude driver

RETIRED per COORD c019 (operator-ratified). This question assumed a speculative claude headless `--input-format stream-json` input side that had to be proven. Per c019 there is NO structured claude driver — Claude runs first-class on tmux by design; the structured driver is the Codex app-server driver, which is proven and subscription-compatible. There is nothing to prove and no capture-spike gate.

#### OQ-AIS-002 — RESOLVED/RETIRED per COORD c019: Codex is subscription-confirmed

RETIRED per COORD c019 (operator-ratified). The billing question was scoped to a speculative claude headless `stream-json` mode. The structured driver is the Codex app-server driver, already confirmed to run on the subscription; there is no billing gate to clear.

#### OQ-AIS-003 — RESOLVED per COORD c019: D5 tmux-inspectability holds literally for Claude

RESOLVED per COORD c019 (operator-ratified). D5 (tmux inspectability) holds LITERALLY for Claude, which runs interactive-in-tmux. Only the structured Codex driver — which owns raw stdio, not a TUI pane — observes via the capture tee. No reinterpretation of the locked decision is required, and the process need not be hosted under tmux for the structured-driver path.

#### OQ-AIS-004 — Keeper: migrate to the structured driver, or carve out?

Question: Does the planner require keeper migrated to the structured driver before C6 deletes (the ROADMAP's stated intent), or is the carve-out (this draft's resolution) accepted?
Owner: foundation-author / planner.
Blocks: the C6 deletion scope (AIS-012) and any SK-002 re-draft.
Default-if-unresolved: carve-out + narrowed C6 scope (safe, unblocks M2).

> PLANNER-RECONCILE: migration would add a session-id-keyed input-contract task and an SK-002 re-draft in the same motion; carve-out defers that. Planner's call.

#### OQ-AIS-006 — Handoff done-gate: distinguish a completed `Stop` from a pending-question `Stop` — RESOLVED (2026-07-14, operator-ratified)

Question: For the AIS-018 handoff done-gate, what is the precise rule that tells a `Stop`-that-completed-the-handoff apart from a `Stop`-that-paused-to-ask-the-operator?

**Resolution.** Gate on `outcome_emitted` (`Stop`) + a nonce'd-artifact-present-and-fresh check; do NOT build real-time completed-vs-pending discrimination in M2.
- **Completion artifact (keeper cycle):** `HANDOFF-<agent>.md` exists, carries this cycle's `<!-- KEEPER:<cycleID> -->` nonce, and `mtime` is not before `NonceConfirmedAt` — reusing the `!mt.Before(...)` freshness anchor `pollAwaitModelDone` already applies to `.idle` (`internal/keeper/shell.go`). Key the `AwaitModelDone → Clearing` edge on `Stop` + this check instead of the `.idle` mtime.
- **Pending-question `Stop`:** no dedicated signal is needed. The handoff turn is a `/session-handoff` injection (not supposed to ask), and the operator disables interactive/plan-question behavior at source. Any `Stop` lacking the fresh nonce'd artifact is treated as NOT-done and falls through to the bounded-liveness timeout (AIS-INV-001; keeper `ModelDoneTimeout` ~60s, fail-open). Conservative by construction: may over-wait, never false-`/clear`.
- **Future hardening (tune-later, out of M2 scope):** a `PostToolUse`-on-`Write(HANDOFF-<agent>.md)` hook (bridge relays none today; +1 hook kind) for a synchronous positive; and the already-mapped `agent_heartbeat{phase:"waiting_input"}` (from `Notification{idle_prompt|permission_prompt}`) as an out-of-band pending veto. Neither required now.

Owner: foundation-author / M2 implementer. Does NOT block the ack-signal-source clauses (AIS-003/AIS-INV-001), which were already settled.

#### OQ-AIS-005 — Final spec filename and requirement prefix

Question: Is the spec `agent-input.md` / `AIS`, or `agent-input-protocol.md` / `AIP`?
Owner: foundation-author.
Blocks: the `specs/_registry.yaml` reservation (must land in the same commit).
Default-if-unresolved: `agent-input.md` / `AIS`.

> PLANNER-RECONCILE: small registry call; pick one and reserve it.

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-07-14 | 0.1.0 | foundation-author | Initial draft (codename: agent-input-substrate). The typed process-spawn input verb: the consumer-owned `InputPort` and the retirement of the six side-interfaces + no-op adapter (AIS-001) and the machine-enforced handler↔tmux boundary (AIS-002); the `Ack` contract — delivery-outcome (`Delivered`/`Rejected`)/seq/token — and its dual synchronous-return + emitted-event delivery composed as a front-stop (AIS-003…AIS-005); the structured-protocol driver (the Codex app-server driver) over `substrate.Run[E,A]` (AIS-006; AIS-007/AIS-008 RETIRED per COORD c019 — Codex proven & subscription-confirmed, no claude capture-spike/billing gate); direct-child-stdio ownership and `StdinDevNull` disposition (AIS-009, AIS-010); the observation-only tmux boundary and keeper/CLI carve-out (AIS-011, AIS-012); the pipes-only best-effort capture tee and consumer-side persistence/redaction/retention (AIS-013, AIS-014); the substrate-selection axis + twin-blindness and the preserved remote seam (AIS-015, AIS-016); the WAL-guard adapt-not-delete fold-in (AIS-017); bounded-liveness output-or-stale (AIS-INV-001) and capture-never-aborts-the-run (AIS-INV-002); the AIS-000 process-spawn "substrate" disambiguation; the wire-protocol and driver-`Step` state machines; the acceptance/failure taxonomy; and the five carried PLANNER-RECONCILE open questions (OQ-AIS-001…OQ-AIS-005). |
| 2026-07-14 | 0.1.0 | foundation-author | **Hook-sourced-ack addendum (COORD c021, operator-ratified; design note `plans/2026-07-13-code-revamp/M2-RESCOPE-hook-sourced-ack.md`).** Made the tmux/Claude path's ack-signal SOURCE explicit and correct: the async positive acceptance is the Claude-hook-bridge event (`outcome_emitted` on `Stop`, `agent_ready` on `SessionStart` start/resume — verified against `internal/hookrelay/hookrelay.go`), NOT a `capture-pane` scrape and NOT a Claude wire protocol (AIS-003); `agent_input_stale` fires when that expected hook signal does not arrive in-bound — the resume-hang fix (AIS-INV-001). Added **AIS-018** (§4.9) handoff done-gate: `outcome_emitted` (`Stop`) AND expected-artifact-present, citing the `buildStopMessage`→`.harmonik/review.json` precedent (CHB-014), with the completed-vs-pending-question discrimination flagged OPEN (OQ-AIS-006, NOT solved). Corrected the tmux-boundary framing so "observation-only" scopes to the structured (Codex) path only — the tmux paste path is Claude's first-class input transport, `capture-pane` is a human observation window never read as a signal, pane-scraping-as-ack dropped (AIS-011). Downgraded transcript-tail to secondary corroboration only. Added `claude-hook-bridge` to `depends-on` and a normative §9.1 cross-ref. No AIS-nnn IDs renumbered; the binary `Delivered`/`Rejected` + async acked/stale model is unchanged. |
