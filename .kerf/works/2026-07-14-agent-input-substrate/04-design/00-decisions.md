# 04-design / 00-decisions — cross-cutting design decisions (M2 agent-input-substrate)

> Pass 4 (Change Design), shared decisions spine. Every per-area design file
> (`04-design/*-design.md`) is subordinate to these decisions. Modeled on P1's
> `04-design/00-decisions.md`. Grounded in `03-research/{seam-contract,driver,capture-tee,harness}/
> findings.md` and TASKS.md/ROADMAP.md of `plans/2026-07-13-code-revamp/`.
> `PLANNER-RECONCILE:` tags mark best-judgment calls the planner must ratify.

---

## D0 — Contract-ownership direction (M2-1 owns, M3 consumes) — HEADLINE

**Decision.** M2-1 **OWNS AND DEFINES** the seam input/ack contract (the InputPort, the `Ack`
type, the bounded-liveness/output-or-stale guarantee). M3-4 (the `runexec` reactor) and M4 (remote)
are **CONSUMERS** of it. M2 is designed self-contained — it does NOT wait on, and does NOT guess,
any M3 artifact.

**Evidence.** TASKS.md is unambiguous and internally consistent across five citations: `:77` ("only
M3-4 needs M2-1 (seam contract)"), `:85` (M3-4 dependency list includes "M2-1 (seam input/ack
contract)"), `:97` (M2-1 = "Seam input method + ACK contract"), `:137`/`:138` (M4 hard-prereq =
"M2-1 (seam input/ack contract)"). The dependency arrow points M3→M2 and M4→M2.

**PLANNER-RECONCILE:** The planner's task brief stated the OPPOSITE direction — "M2-1 consumes the
reactor `Step` input/ack contract that the M3 work (C4) defines — M3 owns and defines it FIRST." The
repo's own task graph (5 concordant citations above) contradicts that brief. This design follows the
REPO: M2 owns the contract. If the planner intended M3 to own it, the whole M2/M3 split must be
re-cut. **This design proceeds M2-as-owner; confirm before either work implements.** Consequence of
the resolution: M2's ack is emitted as BOTH a synchronous return (for the caller) AND a durable
`agent_input_acked`-class event (so M3's future `runexec` Step can observe the ack on its own event
stream without compile-coupling M2 to M3's State type). This composition is direction-agnostic and
survives either reconciliation.

---

## D1 — Input port placement & shape

**Decision.** Introduce a **new consumer-owned narrow `InputPort`** (RS-004 idiom: 1–3 methods,
satisfied structurally, no Result/Option/Either) that the structured driver's session handle
satisfies. `SubstrateSession` is widened so a session MAY satisfy `InputPort`; the daemon obtains it
by structural assertion at the seam (replacing the six type-asserted side-interfaces
`enterSender`/`paneCapturer`/`quitSender`/`paneOutputSizer`/`paneLivenessChecker`/
`commandRunnerProvider`, and the no-op `substrateSessionAdapter.SendInput`/`CloseStdin`,
`internal/handler/substrate.go:140/:173`). Shape:

```
InputPort interface {
    SubmitInput(ctx, InputRequest) (Ack, error)   // blocks until ack-or-stale (D3)
    CloseInput(ctx) error                          // end-of-input, replaces CloseStdin no-op
}
```

**Rationale.** Preserves the depguard inversion (handler declares the port, daemon supplies the
driver — `substrate.go:5`); mirrors the proven keeper `ports.go` port+fn-adapter migration idiom
(seam-contract findings Q4); keeps the single-method `Substrate.SpawnWindow` intact while giving the
session a real, ack-returning input verb. The interim tmux impl during the bake window satisfies
`InputPort` via the existing paste path returning an explicitly **degraded** ack (D2), so both impls
are swappable at one seam.

**PLANNER-RECONCILE:** whether the input method lives on `SubstrateSession` directly vs a separately
asserted `InputPort` is a small placement call; this design picks the separate narrow port. Also:
land a REAL depguard rule (RS-005 leaf style) with the seam — the current handler↔tmux "boundary" is
doc-comment-only, not machine-enforced (seam-contract Q1).

---

## D2 — Ack contents & synchronous-return + emitted-event

**Decision.** `Ack` carries: (a) an **acceptance class** — `Accepted` | `Rejected` | `Degraded`;
(b) a **monotonic input sequence id** (driver-internal, per RS-008 codec-owned); (c) a
**protocol-level acceptance token** (e.g. the turn id the input opened, when the wire protocol
supplies one). `SubmitInput` is a **synchronous return** that blocks until ack-or-stale within the
bounded window (D3). In addition, the driver's internal reactor emits a durable
`agent_input_acked` (and `agent_input_stale`) event so async observers (M3's `runexec`, the replay
harness) see the ack on the event stream. The pre-existing async signals (`agent_heartbeat`
HC-057, commit-poll) are a **front-stop composition**: the synchronous ack is an earlier, positive
acceptance signal; it does NOT replace the heartbeat/commit watchdogs (M2 non-goal §"NOT the
hook-bridge transport"). `Degraded` = "written, but acceptance could not be positively confirmed"
(the interim tmux/paste impl always returns `Degraded`).

**Rationale.** exit-0 stops being the only signal (SC1/SC2); the dual sync-return + event shape is
direction-agnostic per D0; `Degraded` gives the bake window an honest interim state instead of the
current silent no-op.

---

## D3 — Bounded liveness (output-or-stale) — M2's SR9 analog

**Decision.** New invariant **AIS-INV-001**: for every `SubmitInput`, the call reaches exactly one
terminal — an `Ack{Accepted|Rejected|Degraded}` OR an emitted `agent_input_stale`-class event —
within a bounded window `InputAckTimeout + injection overhead`; **silence is FORBIDDEN**; every
`TimerFired` edge in the driver's Step lands in a state with an outgoing action, so the machine
cannot wedge silently. Timeout is measured via `ClockPort` (never wall-clock). This is the M2
peer of SK-015/SK-INV-005 (SR9); phrased in M2's vocabulary, NOT by citing STEP-0a (which lives
only in plans, not specs — harness/seam-contract Q5). M3-5 will define the daemon-run peer.

**Rationale.** The census names the exact risk (PLAN.md:205-208): "a wrong ack/heartbeat contract
re-imports the resume-hang on a substrate whose escape hatch is deleted." The bounded-liveness
clause + the C5 fault harness are the guard that lets C6 delete the escape hatch.

---

## D4 — Wire protocol (claude headless) — corpus-first, spike-gated

**Decision.** Design the driver protocol-abstractly: a `claudewire`-shaped codec + single method
registry + `Event`/`Action` vocabulary + pure `Step`, exactly mirroring the codex quartet
(`codexwire`→`codexreactor`→`codexdigitaltwin`, driver findings Q1). Design AGAINST the assumption
that claude supports **NDJSON stdin input** (`--input-format stream-json` or Agent SDK stdin) with a
turn-based request/response shape analogous to codex app-server (an `initialize`-like handshake, a
`turn/start`-like input frame, streamed `*/delta` + a `turn/completed`-like terminal). The FIRST
implementation task is a **T0 capture spike** (apptap tee over a real claude headless session) to
pin the actual input framing before the codec is frozen — corpus-first, mirroring codex T0.

**KNOWN (repo-proven):** `--output-format stream-json --include-hook-events` exists but was
deliberately unadopted at MVH (claude-hook-bridge.md:684, driver Q2); codex app-server proves the
JSONL JSON-RPC-2.0-over-stdio shape; claude programmatic drive via `-p --resume <id>` /
caller-minted `--session-id`.

**PLANNER-RECONCILE (two, both external-verification):**
1. **The claude input side is UNPROVEN in-repo** — `--input-format stream-json` (bidirectional
   persistent-stdin NDJSON) appears NOWHERE in the tree (no doc, spec, or capture). The design
   assumes it exists with a codex-analogous shape; the T0 spike must confirm framing + steer/interrupt
   support before codec freeze.
2. **Billing** — the project's strongest historical constraint (subscription-not-API billing;
   c2-spec.md:104-109 rejected the Agent SDK sessions HTTP API on exactly this ground) has never been
   evaluated against headless stream-json mode. Confirm headless stream-json bills the Max
   subscription, not the API credit pool, before committing C2.

---

## D5 — Process ownership vs "tmux inspectability required" (LOCKED decision)

**Decision.** The structured driver **owns the child's stdio directly** (driver-held stdin +
stdout pipes, apptap tees both — the codex app-server pattern where the daemon parses the child's
stdout). This is required for a real bidirectional protocol: tmux owning the pty is incompatible
with the daemon reading the child's stdout for protocol events. Inspectability is satisfied by (a)
the C4 capture tee (tail the recorded wire) and (b) an OPTIONAL tmux observation window fed the
captured stream — NOT by tmux owning the process.

**PLANNER-RECONCILE (locked-decision boundary):** `.harmonik/context/project.yaml:26` locks "tmux
inspectability required." A headless stream-json claude is NOT a TUI — a tmux pane running it shows
raw NDJSON, so "inspectability" must reinterpret from "watch the TUI in tmux" to "tail the captured
wire (+ optional observation pane)." C3's open question "is the tmux window even retained?" edges
toward this locked decision. This design does NOT reopen the decision — it reinterprets
"inspectability" as capture-tee-backed. **Operator must confirm** a capture-tee-backed observation
window satisfies "tmux inspectability required" before design lock; if the operator requires the
process literally hosted under tmux, a FIFO-stdin + side-pipe-stdout hosting variant is the
fallback (heavier; the codex precedent argues against it).

---

## D6 — Keeper: carve-out now + migration path (A11 hazard resolution)

**Decision.** **Carve-out, not immediate migration.** Keeper keeps its PL-021d paste path
(`internal/keeper/injector.go`, SK-002 stands). Therefore C3/C6's deletion of tmux write verbs is
**scoped to the daemon-RUN input path** and MUST NOT delete the `load-buffer`/`paste-buffer`/
`send-keys` verbs keeper depends on. PL-021d is **DEMOTED** (removed from the daemon run input
path, superseded by the new input-protocol spec for runs) but **PRESERVED** for keeper + the
interactive-session-nudge use (`cmd/harmonik/{captain,crew,comms}.go` boot-seed/wake, seam-contract
Q3). The new spec explicitly records this carve-out and amends SK-002's cross-reference rather than
contradicting it. A migration path is defined but deferred: route keeper through a **session-id-keyed
input port** (a leaf package keeper may import — keeper has no `SubstrateSession` handle, is
off-daemon, depguard-barred from daemon) OR a daemon RPC.

**PLANNER-RECONCILE:** TASKS.md M2-3/M2-6 state the dependency as "**keeper migrated to the M2-2
structured driver**" before C6 deletes — i.e. the ROADMAP's stated intent is MIGRATE. But SK-002
(freshly normative 2026-07-13, one day before this work) requires keeper's `PanePort.Inject` to
follow PL-021d paste, and keeper's architecture (off-daemon, session-id-addressed, no session handle,
sends slash-commands to interactive panes it did not spawn) makes full migration to the daemon-side
`SubstrateSession`-keyed driver architecturally awkward. This design resolves the A11 deletion hazard
via **carve-out + narrowed C6 scope** (which is safe and unblocks M2) and defers migrate-vs-carve-out
as the planner's call. If the planner insists on migration, it becomes a sizeable added task
(session-id-keyed input contract) and SK-002 must be re-drafted in the same motion.

---

## D7 — WAL-guard (C7): adapt, don't delete

**Decision.** **Adapt.** The 380-line `codexwalguard.go` (driver Q5) compensates for ungraceful
SIGKILL of a stateful CODEX child (leftover `$CODEX_HOME/state_*.sqlite-wal` corrupts the next
launch), NOT for lost input — so a real input ack does not by itself make it redundant. The
structured driver reduces the need by (a) graceful turn-termination via protocol (`turn/interrupt`)
instead of SIGKILL, (b) positive fast-fail detection (no handshake within bound → structured
launch-failure event instead of the 8s silent exit-0). Retain a **boot-time recovery step** for the
residual SIGKILL/crash paths; remove the per-launch symptom-treatment where graceful shutdown
covers it. C7 is codex-specific and claude-irrelevant — its post-M2 home is wherever codex process
lifecycle lands, NOT the claude driver. Off critical path (rides C2/C5).

---

## D8 — Capture tee (C4) shape

**Decision.** Refactor the tee to a **pipes-only `MultiWriter` splice** (add a pipes-in constructor;
do NOT force `Tap.Run`'s exec+Wait ownership onto a driver that owns its own process — capture-tee
Q1). Per RS-016 the tee owns no file format and opens no file; **persistence + redaction live in the
consumer.** Capture writer is **best-effort / degrade-to-uncaptured** (an explicit reversal of
apptap's current fail-closed MultiWriter semantics) — a capture-disk problem MUST NOT abort a live
agent run. Redaction: verbatim in-memory tee + an HC-032-style value-pattern scrub in the persisting
writer (the OUTPUT direction is the secret risk; INPUT is structurally secret-free per HC-028).
Corpus lands at `${workspace_path}/.harmonik/sessions/${session_id}/` (WM §4.7), one file per
direction, with a **mechanical** `CAPTURE-LOG` ledger (keeper's `EXTRACT-LOG.md` is the executed
model; codex's declared-but-never-written `CAPTURE-LOG.md` is the anti-pattern). Retention via the
keep-N (`brhistoryrotate.go:48`) / age-prune (`orphansweep.go:881`) precedents — M2 must NOT inherit
the unrotated-89.5 MB `events.jsonl` defect.

---

## D9 — Harness (C5) taxonomy

**Decision.** Copy the keepertest package shape wholesale: `internal/<v>test/`
(l0_wire, l1_contract, l2_integration, l2_fault_matrix, l3_live, canary) + `internal/<v>twin/`
(codec + synthesizer, re-exporting substrate fault types as aliases). Fault matrix = 4 modes ×
strata × EventN positions, 100% required, per-cell uniform assertion (one terminal, bounded VIRTUAL
window, never-silence via three converters), entry-foreclosed cells assert the no-cycle shape.
Bounded-liveness oracle = D3's output-or-stale in FakeClock virtual time. N-consecutive gate (N=10)
via a script mirroring `keeper-oracle-n10.sh`. **Sentinel-event seam-gap move** for the ack-direction
mismatch: model the driver's ack/response stream as the replayed event type E; paste-discard/stall/
partial/double-submit map to FaultDropAfter/Stall/Truncate/Dup; the codec supplies protocol-specific
`ErrorEvent`/`DisconnectEvent` — do NOT extend the reactor vocab or the four substrate modes (RS-012
is closed). SC6 enforcement ("zero sleeps / zero capture-pane scraping"): forbidigo path-scoped ban
of `time.Sleep` AND `time.After`/`time.NewTimer` in the driver packages (a literal-Sleep ban alone is
insufficient — the live path's blind waits are `time.After`-in-`select`), plus a depguard deny of
driver→`internal/lifecycle/tmux` (once C3/C6 land, import-expressible), plus a grep gate for
`capture-pane` in the driver path. Acceptance = RS-020's four-part shape (N-green + 100% matrix +
out-of-band jq/grep oracle, never self-grading per D13 + measured coverage floor). These are DoD
items, not spec prose (per M2-5 card).

---

## D10 — New spec: naming, prefix, template

**Decision.** New spec file `specs/agent-input.md`, requirement prefix **`AIS`**
(agent-input-substrate), reserved in `specs/_registry.yaml` in the SAME commit (registry lint rule).
Follow the RS/SK template exactly: front-matter v1.1 (title/spec-id/requirement-prefix/status:draft/
spec-shape:requirements-first/spec-category/version/spec-template-version:1.1/owner/last-updated/
depends-on), 12-section layout, `AIS-001…` requirements with `Tags:` footers, `AIS-INV-00n`
invariant series, an RS-023-style disambiguation clause (the new spec sits on the *process-spawn*
sense of "substrate" — cite RS-023), a conformance section with a baseline anchor. `depends-on`:
replay-substrate.md (RS), process-lifecycle.md (PL), handler-contract.md (HC), session-keeper.md (SK).

**PLANNER-RECONCILE:** final filename/prefix (`agent-input.md`/`AIS` vs
`agent-input-protocol.md`/`AIP`) is a small registry call — pick one and reserve it.

---

## Affected spec areas → design files (this pass)

| Design file | Spec area | Components |
|---|---|---|
| `agent-input-design.md` | NEW `specs/agent-input.md` (AIS) | C1 ack contract, C2 driver, C3 tmux-demotion clause, C4 capture reqs, C7 WAL note |
| `process-lifecycle-design.md` | `specs/process-lifecycle.md` (PL-021b/PL-021d) | C3 (demote PL-021d), C6 boundary |
| `handler-contract-design.md` | `specs/handler-contract.md` (HC-054 family) | C1 seam input method + ack |
| `session-keeper-design.md` | `specs/session-keeper.md` (SK-002) | C6 keeper carve-out reconciliation |
| `harness-acceptance-design.md` | DoD (no spec prose) + `specs/replay-substrate.md` cross-ref | C5 taxonomy + fault matrix + SC6 gates |

Note (jig): C5's harness bar is DoD, not normative spec prose (M2-5 card) — its design file documents
the acceptance/test intent and any replay-substrate cross-reference, and feeds the Tasks pass, but
produces no new normative requirements beyond AIS-INV-001's conformance obligation.
