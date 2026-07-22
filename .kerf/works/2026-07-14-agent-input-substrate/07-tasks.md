# 07 — Tasks: `agent-input-substrate` (M2)

> Pass 7 (Tasks). Breaks the drafted spec set (`05-spec-drafts/`, changelog `05-changelog.md`) into
> executable implementation tasks. Each task: what to build · spec refs (file + ID) · deliverables
> (files) · acceptance (testable) · deps. Implementation-agnostic — concrete enough to execute with
> no new design decisions. Subordinate to `04-design/00-decisions.md` (D0–D10). Codename:
> `codename:2026-07-14-agent-input-substrate`.

## Settled naming (resolves the three "small Tasks-pass calls")

> **REFRAMED per COORD c019 (2026-07-14):** the structured driver is the **Codex app-server driver**,
> NOT a claude driver — it is proven, subscription-compatible, and DONE; the whole M2 architecture was
> modeled off it. Claude's input path is tmux (first-class, by design, needs no new codec). The vertical
> stem is therefore **`codex`**, not `claude`. The `claude*` package/dir/script tokens written in the
> task bodies below (`claudewire`/`claudereactor`/`claudedriver`, `internal/claudetest`/`claudetwin`,
> `testdata/claude/`, `scripts/claude-*`, `test-claude-*`) are a stale naming from before c019 — they
> denote the **codex structured-driver vertical** and are flagged for a mechanical `claude*`→`codex*`
> rename at implementation. There is NO new claude structured driver to build.

- **Spec (D10):** file `specs/agent-input.md`, prefix **AIS** — reserved this pass (T1).
- **Driver vertical stem = `codex`** (per c019; the proven Codex app-server vertical —
  `codexwire`/`codexreactor`/`codexdigitaltwin`): the codec, `Event`/`Action`/`Step`, and `Substrate`
  impl are the codex structured driver. (Task bodies below still carry the pre-c019 `claude*` tokens;
  read them as the codex vertical per the note above.)
- **Harness stem = `codex`:** the L0–L3 harness + twin + corpus dir + scripts ride the codex vertical.
  One stem across spec prefix (AIS), wire vertical, corpus dir, twin, and Makefile targets.
- **SC6 gate home = pre-deploy Makefile pair** (codex-vertical, keeper precedent), with N=10 wired into
  the pre-deploy step — this IS the C6-deletion gate, so it blocks a deploy, not merely a CI branch check.

## PLANNER-RECONCILE gates (block real coding until cleared)

- **G1 — OQ-AIS-001 (input side unproven). RETIRED per COORD c019 (2026-07-14) — no longer gates.**
  The premise (a speculative claude `--input-format stream-json` structured driver whose input side is
  unproven) is void: the structured driver is the **Codex app-server**, already proven and DONE. There
  is no claude structured driver to prove and no claude codec to freeze. T5 freezes the codec from the
  proven codex protocol, not a claude spike. (Label kept as a retired slot so DAG references resolve.)
- **G2 — OQ-AIS-002 (billing). RETIRED per COORD c019 (2026-07-14) — no longer gates.** The billing
  concern (does headless claude `stream-json` bill the Max subscription vs API credit?) is moot: the
  Codex app-server structured driver is subscription-confirmed. No operator billing sign-off gates T6.
  (Label kept as a retired slot so DAG references resolve.)
- **OQ-AIS-003 (D5 inspectability). REFRAMED per c019.** D5 holds LITERALLY for Claude — it runs
  interactive-in-tmux (first-class), so tmux inspectability (`project.yaml:26`) is satisfied directly,
  no fallback needed. Only the **Codex structured driver** observes via the capture-tee (it owns the
  process directly). Not a blocking open question for the claude/tmux path; affects the codex T6/T8
  observation shape only.
- **OQ-AIS-004 (D6 migrate-vs-carve-out).** Drafts do carve-out (keeper keeps PL-021d). If the planner
  insists on full keeper migration, SK-021 becomes an in-scope task and SK-002 re-drafts. Surface;
  drafts proceed carve-out.
- **D0 direction (OQ-AIS-005).** M2 owns the input/ack contract (repo TASKS.md, 5 citations); the
  planner brief said the reverse. Dual sync-return + emitted-event is direction-agnostic, so this does
  NOT block — but confirm before M3 implements against it.

---

## Tasks

### T0 — Capture spike + input corpus (gates codec freeze) — RETIRED/REFRAMED per COORD c019
- **RETIRED per c019 (2026-07-14):** there is NO claude capture spike. The premise — an apptap tee over
  a "real claude headless session" pinning `--input-format stream-json` / Agent-SDK-stdin framing — is
  void: the structured driver is the **Codex app-server**, and its wire codec/framing is already proven
  from the existing codex vertical. No `--input-format stream-json` spike, no billing evidence to
  gather (Codex is subscription-confirmed).
- **What remains (if anything):** the T5 codec is frozen directly from the proven codex app-server
  protocol; any corpus needed by T5/T9 comes from the codex vertical's existing captures, not a new
  claude spike. (Task ID slot kept so downstream deps resolve.)
- **Spec:** AIS-007 (was corpus-first + spike gate) — the spike gate is void; codec framing is proven.
- **Deps:** none. **No longer blocked by G1; produces no G2 billing evidence (both retired).**
  Downstream: **T5 no longer waits on a claude capture spike** — it freezes the codec from the codex
  protocol.

### T1 — Land the AIS spec + registry reservation (spec-first)
- **Build:** copy drafted `specs/agent-input.md` into `specs/`; reserve `AIS` in `specs/_registry.yaml`
  **same commit** (registry lint rule); land the drafted amendments to the five modified specs.
- **Spec:** the whole drafted set — AIS-000…017, AIS-INV-001/002; HC 0.7.0 (HC-069/070/071,
  HC-INV-007 carve-out, HC-INV-008); event-model 0.7.1 (§8.21, §6.3); execution-model 0.9.2 (EM-015d);
  process-lifecycle 0.5.5 (PL-021b subclause, PL-021d demotion); session-keeper 0.2.0 (SK-002
  carve-out, §4.10/SK-021).
- **Deliverables:** `specs/agent-input.md` (new), `specs/_registry.yaml`, `specs/handler-contract.md`,
  `specs/event-model.md`, `specs/execution-model.md`, `specs/process-lifecycle.md`,
  `specs/session-keeper.md`.
- **Acceptance:** registry lint passes (AIS reserved, agent-input present same commit); every `[file
  §ID]` cross-ref in the six files resolves (F4/F6/F7/F8/F9/F10 fixes present); version bumps match
  front-matter (HC 0.7.0, PL 0.5.5, SK 0.2.0, event-model 0.7.1, execution-model 0.9.2, agent-input
  0.1.0); no `§AIS` anchors remain (AIS is a prefix).
- **Deps:** none.

### T2 — InputPort + Ack seam + retirements + depguard
- **Build:** `InputPort{ SubmitInput(ctx, InputRequest)(Ack,error); CloseInput(ctx) error }` (RS-004
  narrow port, structurally satisfied); widen `SubstrateSession` so a session MAY satisfy it; obtain by
  structural assertion at the seam. Retire the six type-asserted side-interfaces (`enterSender`,
  `paneCapturer`, `quitSender`, `paneOutputSizer`, `paneLivenessChecker`, `commandRunnerProvider`) and
  the no-op `substrateSessionAdapter.SendInput`/`CloseStdin` (`internal/handler/substrate.go:140/:173`).
  `Ack{Outcome(Delivered|Rejected), Seq, Token}` — the ack carries a delivery outcome (`Delivered` =
  input handed to the driver, acceptance verdict arrives async; `Rejected` = protocol-level refusal,
  structured drivers only), NOT an acceptance capability class. Land the REAL handler↛tmux depguard
  **deny**.
- **Spec:** AIS-001/002/003; HC-069 (verb + retirements + StdinDevNull split), HC-070 (Ack contents +
  front-stop composition), HC-071/AIS-002 (machine-enforced depguard deny).
- **Deliverables:** `internal/handler/substrate.go` (InputPort, Ack, InputRequest; deletions),
  `internal/handler/pasteinject.go` (side-interface call sites cut over — not yet deleted, C6/T11
  owns deletion), `.golangci.yml` (depguard component-matrix entry: handler → `internal/lifecycle/tmux`
  deny).
- **Acceptance:** contract type-checks with NO driver present (port declared handler-side, supplied
  daemon-side — depguard inversion preserved); the six side-interfaces and the two no-ops are gone;
  interim tmux path satisfies `InputPort` returning an explicit `Delivered` ack (its acceptance is
  confirmed async via `agent_input_acked` when the expected Claude-hook-bridge signal
  (`outcome_emitted`/`agent_ready` per [claude-hook-bridge.md CHB-013/CHB-018]) is observed — never a
  `capture-pane` scrape — or reaches `agent_input_stale` on the bounded-liveness timeout), not a silent
  no-op;
  `golangci-lint run` fails on a synthetic handler→tmux import.
- **Deps:** T1.

### T3 — event-model §8.21 registration
- **Build:** register the two cross-bus events via `mustRegister` + `PayloadCompatEntry{…v1}`; payload
  structs `core.AgentInputAckedPayload` (`run_id`/`class`/`input_seq`/`acceptance_token?`/`session_id?`/
  `acked_at`) and `core.AgentInputStalePayload` (`run_id`/`input_seq`/`session_id?`/`timed_out_at`/
  `window`). Wire EV-050 cohort-guard carve-out + §8.9(b) evidence.
- **Spec:** event-model §8.21 (8.21.1 `agent_input_acked` class O, 8.21.2 `agent_input_stale` class O),
  §6.3 payloads; AIS-004.
- **Deliverables:** `internal/core/` payload structs + registration (mirror the §8.20 keeper-interior
  pattern), event registry test.
- **Acceptance:** both event types resolve through `mustRegister` (registry test green); N-1 readable
  compat via `PayloadCompatEntry` present (operator-nfr §4.5); acked payload documented idempotent on
  `input_seq`; both types are excluded from HC-INV-007 sole-publisher scope (F1 carve-out honored).
- **Deps:** T1.

### T4 — Bounded-liveness in the driver Step (ClockPort)
- **Build:** encode AIS-INV-001/HC-INV-008 into the reactor Step: every `SubmitInput` reaches exactly
  one terminal — an `Ack` (`Delivered`/`Rejected`) OR emitted `agent_input_stale` — within
  `InputAckTimeout + injection overhead`; **silence FORBIDDEN**; every `TimerFired`/timeout edge lands
  in a state with an outgoing action (no silent wedge). Window measured via `ClockPort`, never
  wall-clock.
- **Spec:** AIS-INV-001, HC-INV-008; D3.
- **Deliverables:** the Step's timeout/terminal transitions in `internal/claudereactor/` (co-developed
  with T5's Step; T4 owns the liveness property + its transitions).
- **Acceptance:** state-machine review shows no `TimerFired` edge without an outgoing action;
  the timeout path emits `agent_input_stale`; no path leaves `SubmitInput` pending with no terminal;
  timing is ClockPort-injected (verifiable under FakeClock — the executable oracle is T9).
- **Deps:** T2.

### T5 — structured-driver codec + reactor (Event/Action/Step)
> Vertical stem is **codex** per c019 (the `claude*` package tokens below are the pre-c019 naming for
> the codex structured-driver vertical — see "Settled naming"). This is the Codex app-server codec, not
> a claude codec.
- **Build:** the structured-driver codec (frozen from the **proven Codex app-server protocol** — no
  claude spike; the framing is already proven from the existing codex vertical) + single **method
  registry** + `Extra` passthrough for unmodeled fields + flat structs + **pure** `Step` + a `Run`
  one-liner over `substrate.Run[E,A]`. Mirror the codex quartet exactly. Apply the type-alias
  re-instantiation clause of RS-021 (codex-scoped in RS-021's body; applied to the codex
  structured-driver instantiation) so the existing codex packages stay green.
- **Spec:** AIS-006; RS-021 (type-alias clause per F8 relabel).
- **Deliverables:** `internal/claudewire/` (codec, frame→method table, `Extra`) [codex vertical per
  c019], `internal/claudereactor/` (Event/Action vocab, pure Step, `Run` over `substrate.Run[E,A]`).
- **Acceptance:** every corpus frame parses to a known method with zero `FrameKindRaw`; unmodeled
  fields survive round-trip via `Extra`; Step is pure (no IO, deterministic given (state, event));
  `Run` composes `substrate.Run[E,A]` with no bespoke loop; codex packages still compile+pass.
- **Deps:** T2 (port/Ack types). **No longer waits on a T0 claude spike / G1 (both retired per c019);**
  the codec freezes from the proven codex protocol.

### T6 — The structured driver (second Substrate impl)
> The structured driver is the **Codex app-server driver** per c019 (`claudedriver` is the pre-c019
> token for the codex vertical — see "Settled naming"). NOT a claude driver; Claude runs on tmux.
- **Build:** `claudedriver` [codex structured-driver vertical per c019] — a second `handler.Substrate`
  impl. Direct-child stdio ownership
  (driver-held stdin + stdout pipes; apptap tees both). Composition-root selection axis alongside tmux
  (`workloop.go` injection point), twin-blind (driver selected at the root, tests never see the axis).
  Preserve the remote seam.
- **Spec:** AIS-009/010 (direct-stdio ownership, StdinDevNull split), AIS-015 (substrate-selection axis
  + twin-blind), AIS-016 (remote seam preserved); D5.
- **Deliverables:** `internal/claudedriver/` (Substrate impl over T5 reactor), composition-root wiring
  in `internal/daemon/` (selection axis), stdin/stdout pipe ownership.
- **Acceptance:** driver built and tested in isolation before replacing anything; swappable at one seam
  (both tmux and claudedriver satisfy `Substrate`/`InputPort`); the driver owns child stdio (daemon
  reads child stdout for protocol events); selection axis is composition-root-only (no test observes
  it); remote seam unbroken (M4 hard-prereq intact).
- **Deps:** T5, T4. **No longer blocked by G2 (billing gate retired per c019 — the codex structured
  driver is subscription-confirmed).**

### T7 — Live capture tee
- **Build:** refactor apptap to a **pipes-only** `MultiWriter` splice (add a pipes-in constructor; do
  NOT force `Tap.Run`'s exec+Wait ownership onto the driver). **Best-effort / degrade-to-uncaptured**
  (explicit reversal of the fail-closed default). Persistence + redaction + retention live in the
  **consumer**, not the tee (RS-016). Redaction: verbatim in-memory tee + HC-032-style value-pattern
  scrub in the persisting writer (OUTPUT is the secret risk; INPUT structurally secret-free per HC-028).
- **Spec:** AIS-013/014, AIS-INV-002 (capture never aborts the run); D8.
- **Deliverables:** `internal/apptap/` (pipes-in constructor, best-effort semantics), a consumer-side
  persisting writer with redaction + keep-N/age-prune retention (`brhistoryrotate.go`/`orphansweep.go`
  precedents), corpus lands at `${workspace_path}/.harmonik/sessions/${session_id}/` per WM §4.7.
- **Acceptance:** a capture-disk fault (e.g. write error / full disk) does NOT abort the live run
  (AIS-INV-002 test); captured stream is byte-verbatim in-memory; persisting writer scrubs OUTPUT
  secrets; retention rotates (no unbounded growth — must NOT inherit the 89.5 MB `events.jsonl`
  defect); CAPTURE-LOG emitted mechanically.
- **Deps:** T6.

### T8 — Observation-only tmux + PL-021d demotion wiring + EM-015d migration
- **Build:** move the tmux write path OFF the daemon-run input path (input goes through
  `SubmitInput`); wire EM-015d-RIA/RFD review-loop delivery to `SubmitInput`→`Ack`; preserve keeper +
  interactive-CLI paste (PL-021d demoted-not-deleted) and `capture-pane` observation.
- **Spec:** AIS-011/012 (observation-only boundary, deletion boundary); PL-021b subclause + PL-021d
  demotion; EM-015d-RFD step 2 + EM-015d-RIA intro/step 3 migration; SK-002 carve-out.
- **Deliverables:** `internal/lifecycle/tmux/` (write verbs no longer on the daemon-run input path;
  `capture-pane` retained), review-loop delivery cutover in the daemon run path,
  `cmd/harmonik/{captain,crew,comms}.go` keeper/nudge paste preserved.
- **Acceptance:** a daemon run delivers input via `SubmitInput` (no tmux `send-keys`/`paste-buffer` on
  that path); the EM-015d implementer-resume + reviewer-start instructions arrive via AIS with an Ack;
  keeper's `PanePort.Inject` and CLI nudge paste still work (PL-021d survives for those); `capture-pane`
  observation still available; WHAT/control-flow/ordering of EM-015d unchanged.
- **Deps:** T6.

### T9 — L0–L3 harness + fault matrix + N=10 + coverage floor + SC6 gates
- **Build:** per `harness-acceptance-design.md`. `internal/claudetest/` (`l0_wire`, `l1_contract`,
  `l2_integration`, `l2_fault_matrix`, `l3_live`, `canary`, `metrics_export`, `helpers`) +
  `internal/claudetwin/` (`codec`, `synthesizer`, `twin` re-exporting substrate fault types as aliases).
  Model the driver's ack/response stream as replayed E; map paste-discard/stall/partial/double-submit →
  `FaultDropAfter`/`FaultStall`/`FaultTruncate`/`FaultDup`; codec supplies sentinel
  `twin_transport_error`/`twin_disconnected` (NEVER new fault modes — RS-012 closed). FakeClock
  output-or-stale oracle; N=10 script; SC6 gate trio.
- **Spec:** AIS-INV-001 conformance oracle; DoD per M2-5 card (not spec prose); SC6.
- **Deliverables:** the two packages above; `scripts/claude-oracle-n10.sh`,
  `scripts/claude-coverage-gate.sh` + `.baseline`, `scripts/claude-metrics.sh`; `.golangci.yml`
  forbidigo path-scoped ban of `^time\.Sleep$` AND `time.After`/`time.NewTimer` in the driver packages
  (FakeClock spin carved out); depguard deny driver → `internal/lifecycle/tmux`; a `capture-pane` grep
  gate; Makefile `test-claude-l012` / `test-claude-live` pair.
- **Acceptance:** fault matrix `4 modes × strata × every EventN` at **100%**, per-cell single-terminal
  (acked-event XOR stale) within the bounded VIRTUAL window; entry-foreclosed cells assert the
  no-cycle/no-submit shape; the FakeClock oracle proves stale fires past the window (BlockUntil→Advance,
  no advance-before-arm race); N=10 green with zero flakes; per-FILE coverage ratchet met; metrics
  recomputed from raw artifacts via jq/grep (never the driver's own report — D13); forbidigo fails on
  a planted `time.After` in the driver path; depguard fails on a driver→tmux import; grep gate fails on
  a planted `capture-pane`. This bundle is the **acceptance oracle for T11**.
- **Deps:** T6, T7.

### T10 — WAL-guard adapt (C7)
- **Build:** adapt (don't delete) the codex WAL-guard. Reduce need via graceful turn-interrupt
  (`turn/interrupt` instead of SIGKILL) + positive fast-fail (no handshake within bound → structured
  launch-failure event, not the 8s silent exit-0). Retain a boot-time recovery step for residual
  SIGKILL/crash paths; remove per-launch symptom-treatment where graceful shutdown covers it.
- **Spec:** AIS-017; D7.
- **Deliverables:** graceful turn-interrupt + fast-fail in `internal/claudedriver`/reactor; boot-time
  recovery retained (its post-M2 home is codex process lifecycle, not the claude driver).
- **Acceptance:** graceful interrupt path exercised (no SIGKILL on normal turn-end); a handshake-timeout
  emits a structured launch-failure event (not silent exit-0); boot-time recovery still handles a
  simulated residual crash; per-launch symptom-treatment removed where graceful shutdown covers it.
- **Deps:** T6. (Off critical path — rides C2/C5.)

### T11 — C6 deletion (post-bake, gated)
- **Build:** delete `pasteinject.go` and the input portions of `tmuxsubstrate.go`
  (`WriteLastPane`, input-only write verbs). PRESERVE: keeper/CLI paste verbs
  (`load-buffer`/`paste-buffer`/`send-keys` for keeper + nudge), spawn verbs (`tmux new-window`,
  `crewstart.go`), and `capture-pane`.
- **Spec:** AIS-012 deletion boundary; PL-021d C6 note; SK-002 carve-out (keeper EXCLUDED).
- **Deliverables:** deletion of `internal/handler/pasteinject.go` (~2,633 LOC) + input portions of
  `internal/daemon/tmuxsubstrate.go` (~2,735 LOC); write verbs kept for keeper/spawn.
- **Acceptance:** T9 bundle green (N=10 + 100% matrix + oracle) AND bake window elapsed with the ack
  contract holding; after deletion the build is green; keeper input, CLI nudge, spawn, and
  `capture-pane` all still function; the escape hatch is gone only after the gate passes (irreversible
  teardown checkpoint).
- **Deps:** T9 + defined bake window.

### T12 — WM §4.7 capture-corpus layout enumeration (integration F5) — DEFERRED, non-blocking
- **Build:** amend workspace-model §4.7 session-dir layout to enumerate the capture-corpus files +
  CAPTURE-LOG. Non-blocking: AIS relies on the existing dir (read-only), it does not require WM to list
  every file.
- **Deliverables:** `specs/workspace-model.md` §4.7 amendment.
- **Acceptance:** §4.7 lists the per-direction corpus files + CAPTURE-LOG. **Deps:** T7 (uses the real
  file set). Deferred — does not block work close.

### T14 — Handoff done-gate wired to the Stop hook + artifact check (tmux/Claude path)
> The "even a whole phase is worth it" piece per the M2 re-scope design note
> (`plans/2026-07-13-code-revamp/M2-RESCOPE-hook-sourced-ack.md`); scoped as its OWN task, NOT folded
> into T8.
- **Build:** realize **AIS-018** — wire the keeper/restart cycle's "model done" gate to the
  Claude-hook-bridge `Stop` (`outcome_emitted`) event + an expected-artifact-presence check
  (file-existence/fingerprint, or a `PostToolUse`-on-`Write` match). Consume the bus `outcome_emitted`
  (mirror the reviewer-phase `buildStopMessage`→`.harmonik/review.json` precedent, CHB-014) rather than
  the flaky `.idle` marker + transcript parse. Scope the completed-vs-pending-question discrimination
  (OQ-AIS-006) EXPLICITLY.
- **Spec:** AIS-018 (§4.9); [claude-hook-bridge.md §4.5 CHB-013/CHB-014, §4.7 CHB-020]; session-keeper
  SK-014 cross-ref note.
- **Deliverables:** the done-gate in the keeper/restart cycle (`internal/keeper/`) consuming
  `outcome_emitted` + the artifact check; resolves PLAN.md problem #6's unimplemented SR4 interior.
- **Acceptance:** a handoff completes ONLY when `outcome_emitted` (`Stop`) fired AND the expected
  artifact is present; a `Stop` without the expected artifact does NOT satisfy the gate (falls through
  to the AIS-INV-001 bounded-liveness timeout); the completed-vs-pending-question rule is documented
  (OQ-AIS-006 resolved, or its conservative default applied); no reliance on pane-scraping or a bare
  `.idle` marker as the sole signal.
- **Deps:** T1 (spec), T3 (event registration — needs the bus events). Coordinates with T8; off the T6
  critical path.

### T13 — CHB-028 delivery-wording reconciliation (integration F11) — DEFERRED to M3/claude-driver
- **Build:** reconcile claude-hook-bridge CHB-028 `agent-task.md` task-delivery wording (currently
  "under the tmux substrate per PL-021b") with the AIS structured input path. The ARTIFACT contract
  survives; only the delivery instruction changes.
- **Deliverables:** `specs/claude-hook-bridge.md` CHB-028 wording amendment.
- **Acceptance:** CHB-028 delivery wording cites the AIS input path, not PL-021b paste. **Deps:** best
  done alongside the M3 consumer. Deferred — does not block work close.

---

## Test tasks (MANDATORY close-gate)

### T-scenario — hk-1cjy5
`scenario: agent-input.md — structured-driver input ack end-to-end (twin)`
(labels `scenario-test`, `codename:2026-07-14-agent-input-substrate`).
- **Acceptance:** a twin-driven end-to-end run submits input and observes a positive acceptance — the
  async `agent_input_acked` event (plus the sync `Delivered` ack return) — asserted out-of-band.
  **Deps:** T6, T9.

### T-explore — hk-1r5jt
`explore: agent-input.md — operator drives a daemon run on the structured input driver`
(labels `exploratory-test`, same codename).
- **Acceptance:** an operator drives a real daemon run on `claudedriver`; input is delivered and acked
  through `SubmitInput`; observation via capture-tee (+ optional pane). **Deps:** T8.

> **CLOSE RULE (normative):** neither this work NOR any of its implementation beads (T1–T11) may close
> until BOTH test beads (hk-1cjy5, hk-1r5jt) are closed. Filed in `05-changelog.md` §"Validation".

---

## Dependency DAG

> **Per c019:** G1, G2, and the T0 claude capture spike are RETIRED (the Codex app-server structured
> driver is proven + subscription-confirmed). T5 no longer waits on a G1/T0 claude spike; T6 no longer
> waits on a G2 billing gate. The edges below are shown struck-through to keep the DAG readable.

```
[G1 RETIRED] ╌╌▶ [T0 RETIRED] ╌╌▶ T5 ─▶ T6 ─┬─▶ T7 ─┐
                                 ▲           ├─▶ T8 ──┼─▶ (T-explore)
T1 ─┬─▶ T2 ─┬─▶ T4 ───────────────┘          ├─▶ T10  │
    │       └───────────────────────────────┘         │
    ├─▶ T3                                   T9 ◀───────┘ (T7)
    └─(spec)                                 T9 ◀── T6
[G2 RETIRED] ╌╌▶ T6 (was commit gate; no longer blocks)
T9 ─▶ T11 (+bake)
T6,T9 ─▶ T-scenario
T7 ─▶ T12 (deferred)   M3 ─▶ T13 (deferred)
```

Edges (parent → child): T1→{T2,T3}; T2→T4; T2→T5; T5→T6; T4→T6; T6→{T7,T8,T10}; T6,T7→T9;
T9→T11(+bake); T6,T9→T-scenario; T8→T-explore; T7→T12; M3→T13; {T1,T3}→T14 (coordinates with T8, off
the T6 critical path). (Retired per c019: G1→T0, T0→T5, G2→T6.) **No cycles.**

---

## Parallelization plan

- **Wave A (spec, parallel):** **T1** (spec land). T1 has no deps. (T0 is RETIRED per c019 — no claude
  capture spike; G1 retired, so nothing gates a spike.)
- **Wave B (after T1):** **T2** ∥ **T3**. T3 (event registration) is independent of the seam once the
  spec lands.
- **Wave C (after T2):** **T4** (bounded-liveness transitions) — feeds T5/T6.
- **Wave D (after T2):** **T5** (codec + reactor) — needs the port types; the codec freezes from the
  proven codex protocol (no T0 spike / G1 per c019).
- **Wave E (after T5 + T4):** **T6** (driver). Single-threaded critical node. (No G2 billing gate per
  c019 — codex is subscription-confirmed.)
- **Wave F (after T6, fully parallel):** **T7** (capture tee) ∥ **T8** (tmux demotion/EM-015d) ∥
  **T10** (WAL-guard). Independent surfaces.
- **Wave G (after T6 + T7):** **T9** (harness). Can start L0/L1 as soon as T5 lands; the fault matrix
  + capture-fed corpus need T6/T7.
- **Wave H (after T9 + bake):** **T11** (deletion). Then **T-scenario** (T6+T9) and **T-explore** (T8)
  before work close.
- **Deferred, any time:** **T12** (after T7) ∥ **T13** (with M3) — neither gates work close.

---

## Critical path

`T1 → T2 → T5 → T6 → T9 → (bake) → T11`, with T-scenario/T-explore as the mandatory close-gate
after T9/T8. T6 (the driver) is the single largest node (replaces ~5.4k LOC of live IO) and the
convergence point — everything downstream (T7/T8/T9/T10) fans out from it. **Per c019 the two former
external gates are gone:** G1 (claude codec-freeze spike) and G2 (billing) are RETIRED — the Codex
app-server structured driver is proven and subscription-confirmed, so T5 freezes the codec from the
proven codex protocol and T6 commits without a billing sign-off. T3/T4 run off the critical path;
T7/T8/T10 parallelize after T6; T12/T13 are off-path deferrals.
