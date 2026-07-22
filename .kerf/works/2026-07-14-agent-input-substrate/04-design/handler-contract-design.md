# 04-design / handler-contract-design — C1 seam input method + ACK (M2 agent-input-substrate)

> Pass 4 (Change Design), `specs/handler-contract.md` area. Subordinate to `04-design/00-decisions.md`
> (D0–D3 authoritative — not re-decided here). Grounds the HC amendments for C1: the seam gains a
> real typed input verb + ack. Line numbers verified against `handler-contract.md` v0.5.4 on
> `phase1-session-restart-substrate`, 2026-07-14. Design detail for the wire/driver/capture lives in
> the NEW `specs/agent-input.md` (AIS, D10); HC keeps only the **seam-contract-level** requirements.

---

## Current state

- **HC-054** (`handler-contract.md:1143-1154`) is the **observation half only**: for
  `agent_type=claude-code` under the PL-021b tmux substrate, `Session.Attach()` MUST return a live
  pty `io.Reader` (not a log tail). It says **nothing** about *input*.
- **The seam carries no input operation.** `handler.Substrate` (`substrate.go:30-38`) has exactly one
  method, `SpawnWindow`. `SubstrateSession` (`substrate.go:101-122`) is Kill/Wait/Outcome/PID/Stdout.
  The load-bearing doc contract (`substrate.go:97-98`): *"SendInput and CloseStdin are not part of
  this interface; the substrate owns the child's stdin (typically the pty managed by tmux)."*
- **The no-op adapter** (`substrate.go:129-199`): `substrateSessionAdapter.SendInput` (`:140`)
  "returns nil silently"; `CloseStdin` (`:173`) is a no-op; `Stderr()` returns nil.
- **Spec/code mismatch worth noting:** the spec's §6.1 `Session` interface *already* declares
  `SendInput(ctx, input) -> error` — "delivers input to the running agent; typed sentinel on failure"
  (`:1073`). For substrate-hosted sessions that verb is silently a no-op, so the spec's stated
  contract is **unmet at the seam** — input is smuggled out-of-band through six type-asserted
  side-interfaces (`enterSender`/`paneCapturer`/`quitSender`/`paneOutputSizer`/
  `paneLivenessChecker`/`commandRunnerProvider`, `pasteinject.go:187/206/236/254/280/493`) and the
  PL-021d tmux paste path — with **exit-0-of-tmux as the only "ack"** (which is not an ack;
  seam-contract Q5).
- **HC-056** (`:685`) agent_ready timeout — window starts at `SubstrateSpawn` return, waits for the
  relay-synthesized `agent_ready`; **HC-057** (`:712`) daemon-side heartbeat (`RunHeartbeatLoop`,
  300 s). Neither is an *input* ack; both are process-liveness signals.
- **Redaction** HC-028/031/032/034 + **HC-INV-003** (`:1010`): secrets travel only via
  `HARMONIK_SECRET_*` env (HC-028); no secret value crosses the bus or lands on disk.
- **Depguard is doc-only:** the "handler↔lifecycle/tmux forbidden" claim in `substrate.go:8` is a
  comment, NOT a machine rule — `.golangci.yml` has `handler-brcli-ban` and `handler-impls` but **no**
  handler→tmux deny (seam-contract Q1; contrast the enforced P1 leaf rule at `.golangci.yml:180-190`).

## Target state

**Numbering call — ADD new IDs, do NOT amend HC-054.** HC-054 owns *observation*; input is a distinct
concern, and the changelog enforces "HC ID FREEZE / no renumbering; additive gap-filler IDs"
(`:1472`, HC-016a/HC-026b precedent). Highest live ID is HC-057 → new IDs **HC-058, HC-059, HC-060**
+ invariant **HC-INV-008**. HC-054's body is untouched; the `SubstrateSession` stdin-ownership doc
contract (`substrate.go:97-98`) is what gets rewritten in code, tracked by HC-058.

- **HC-058 — SubstrateSession input port (seam gains a first-class typed input verb).** The seam MUST
  expose a narrow, consumer-declared `InputPort` (D1) that a session MAY satisfy structurally:
  `SubmitInput(ctx, InputRequest) (Ack, error)` and `CloseInput(ctx) error`. `SubmitInput` **blocks
  until ack-or-stale** within the bounded window (HC-INV-008). This **retires**: the six type-asserted
  side-interfaces as the input mechanism, and the no-op `substrateSessionAdapter.SendInput`/
  `CloseStdin` (`substrate.go:140/173`) — which become real delegations to the port or an explicit
  typed `ErrDeterministic("input unsupported")` when the impl does not satisfy `InputPort` (SC2). The
  interim tmux/paste impl satisfies `InputPort` during the bake window by returning an explicitly
  **Degraded** ack (below), so both impls are swappable at ONE seam. `> PLANNER-RECONCILE (D1):` the
  input method sits on a **separately asserted narrow `InputPort`**, not on `SubstrateSession`
  directly — a small placement call this design fixes; mirrors the keeper `ports.go` port+fn-adapter
  idiom (seam-contract Q4).
  **StdinDevNull disposition (`substrate.go:63-74`):** amended. Under the structured driver (D5) the
  driver owns the child's stdin directly, so the field's claude guidance ("MUST NOT be set for claude
  (pane-paste harness)") is **superseded for the structured-driver harness** — claude now reads NDJSON
  on stdin per the AIS wire spec. The `/dev/null` redirect stays a **codex/ProcessExit-harness and
  interim-tmux-impl** concern only; HC-058 records the split rather than deleting the field.

- **HC-059 — Input ack: contents + synchronous-return AND emitted event (D2).** `Ack` MUST carry:
  (a) an **acceptance class** `Accepted | Rejected | Degraded`; (b) a **monotonic input sequence id**
  (driver-internal); (c) a **protocol-level acceptance token** when the wire protocol supplies one
  (e.g. the turn id the input opened). `SubmitInput`'s return is the synchronous ack for the caller.
  In addition the driver MUST emit a durable **`agent_input_acked`** (and, on stale,
  **`agent_input_stale`**) bus event so async observers see the ack on the event stream. `Degraded` =
  "written, acceptance not positively confirmed" — the interim tmux/paste impl **always** returns
  `Degraded` (an honest interim state, replacing today's silent no-op). `> PLANNER-RECONCILE (D0):`
  the repo task graph (5 concordant TASKS.md citations) has **M2 OWN** this contract and M3 (`runexec`)
  CONSUME it; the planner brief stated the reverse. This design proceeds M2-as-owner. The dual
  sync-return + emitted-event shape is **direction-agnostic** and survives either reconciliation — M3's
  future `runexec` Step observes the ack on its own event stream with **no compile-coupling** of M2 to
  M3's State type. Confirm the ownership direction before either work implements.
  **HC-056 / HC-057 relationship — front-stop composition, NOT replacement.** HC-056 (agent_ready
  timeout) and HC-057 (heartbeat watchdog) are amended with one cross-ref clause each: the new
  synchronous ack is an *earlier, positive, per-input* acceptance signal that **composes in front of**
  the existing process-liveness signals; it does NOT gate first-work-dispatch (agent_ready still does)
  and does NOT replace the heartbeat/commit watchdogs. exit-0 stops being the only signal (SC1).

- **HC-INV-008 — bounded input liveness (seam peer of AIS-INV-001, D3).** For every `SubmitInput`, the
  call MUST reach exactly one terminal — an `Ack{Accepted|Rejected|Degraded}` OR an emitted
  `agent_input_stale`-class event — within a bounded window (`InputAckTimeout` + injection overhead)
  measured via `ClockPort` (never wall-clock). **Silence is FORBIDDEN**; every timer edge in the
  driver's Step lands in a state with an outgoing action. This is HC's seam-level statement; the
  machine-checked, per-cell home is **AIS-INV-001** in `specs/agent-input.md` (D3, D9 fault matrix).
  Phrased in M2 vocabulary — NOT by citing STEP-0a (which lives only in `plans/`, not specs).

- **HC-060 — Machine-enforced seam inversion (make the doc-only boundary REAL).** The handler declares
  `InputPort`; the daemon composition root supplies the driver. This inversion MUST be enforced by a
  real depguard deny (RS-005 leaf style) — `internal/handler` MUST NOT import `internal/lifecycle/tmux`
  — landed in `.golangci.yml` in the SAME commit as the port (currently comment-only, seam-contract
  Q1/Risk 3). `> PLANNER-RECONCILE (D1):` land the machine rule with the seam, not after.

- **Cross-reference to the AIS spec.** HC-058/059 add a Cross-refs line pointing at `specs/agent-input.md`
  as the home of the wire framing, codec, `InputRequest` schema detail, capture, and driver Step; HC
  stays at seam altitude.

## Rationale

- **D0 (M2 owns, M3 consumes):** emitting the ack as a durable `agent_input_acked` event (not only a
  return value) lets `runexec` observe acceptance on the bus without compile-coupling — the property
  that makes the ownership direction reconcilable either way.
- **Honest-probe / liveness discipline (D2/D3):** `Degraded` gives the bake window a truthful state
  instead of a silent no-op; HC-INV-008 is the guard that (with the C5 fault harness) lets C6 delete
  the tmux escape hatch without re-importing the resume-hang (census PLAN.md:205-208).
- **Redaction × C4 capture (HC-031/032/034, HC-INV-003):** the **input** direction is structurally
  secret-free — secrets travel only via `HARMONIK_SECRET_*` env per HC-028, never in `InputRequest`.
  So the seam carries no new secret-leak surface. The **output** capture is the secret risk; per D8 the
  verbatim in-memory tee is fine and the **value-pattern scrub (HC-032-style) belongs to the
  *persisting writer*** in the AIS capture spec, not the seam. HC-034's no-secret-on-disk obligation
  extends to the capture corpus by cross-ref; the mechanism lives in AIS/the persisting writer.
- **SC2 (side-interfaces retired):** collapsing six runtime type-assertions + a no-op into one narrow
  structural port + one machine rule removes the "input smuggled out-of-band" surface entirely.

## Requirements traceability

| Success criterion / decision | HC change |
|---|---|
| **SC1** real input method + ack | **HC-058** (InputPort verb) + **HC-059** (Ack contents + emitted event) |
| **SC2** side-interfaces + no-op adapter retired | **HC-058** (retire the six + no-op) + **HC-060** (real depguard boundary) |
| **D0** M2 owns, M3 consumes (event-observable ack) | **HC-059** dual sync-return + `agent_input_acked` event |
| **D1** narrow consumer-owned InputPort placement | **HC-058** (separate `InputPort`) + **HC-060** (machine inversion) |
| **D2** Ack {Accepted/Rejected/Degraded} + seq + token; front-stop | **HC-059** + HC-056/HC-057 cross-ref amendments |
| **D3** bounded input liveness (output-or-stale, never silence) | **HC-INV-008** (seam peer of AIS-INV-001) |

## Spec-mechanics checklist (for the Tasks pass)

- Add HC-058/059/060 under §4.1 (interfaces) or a new §4.x input sub-section; add HC-INV-008 to §5.
- Amend §6.1 schema: replace the smuggled `SendInput` note with the `InputPort` interface block +
  `InputRequest`/`Ack` records; amend `SubstrateSession` narrative + `SubstrateSpawn.StdinDevNull`.
- Amend HC-056 (`:707` Cross-refs) and HC-057 (`:728` Cross-ref) with the front-stop clause.
- Amend HC-054's narrative only to note it is the *observation* peer of the new *input* port (one line).
- §6.4 co-owned events: register `agent_input_acked` / `agent_input_stale` (payload schema → event-model).
- §10.1 conformance: append **HC-INV-008** to the invariant list; note HC-058/059/060 land as
  additive bridge/post-MVH amendments (as HC-054-family did — not folded into the HC-001..HC-053 range).
- §10.2: add a test-obligation bullet (ack-class matrix; bounded-liveness fault test; depguard-deny lint).
- §12 changelog: new dated row; front-matter `version` bump 0.5.4 → **0.6.0** (new IDs + new invariant),
  `last-updated` 2026-07-14, status stays `reviewed`. NO existing HC IDs renumbered.
