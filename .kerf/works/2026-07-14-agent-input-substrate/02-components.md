# 02 — Decompose: `agent-input-substrate` (M2)

> Pass 2 (decompose). Breaks M2 into buildable components. Each carries **what / why it is
> separable / rough order / key open questions for the DEFERRED design pass.** Design is held
> until P1 proves the reactor+replay seam; these open questions are the design-pass agenda,
> not answered here. Work **stops after this pass** — do not advance past `decompose`.
>
> Grounded in `01-problem-space.md` and the verified `file:line` facts therein. (The spec-jig
> stub template asks for "affected spec files"; those are enumerated in `01-problem-space.md`
> §Affected areas — the *implementation* component breakdown below is the M2 decompose the
> phase requires, and the spec surface per component is called out in each C-block.)

> **ADDENDUM (COORD c019 + c021; design note `plans/2026-07-13-code-revamp/M2-RESCOPE-hook-sourced-ack.md`).**
> C2 already carries the c019 correction (the structured driver is the **Codex app-server** driver, NOT
> a claude driver; Claude runs first-class on tmux). Two further corrections apply to this pass:
> **C3** — "Reduce the tmux path to a read/observation window; retire the write verbs" is scoped to the
> **structured (Codex) driver's** input path ONLY. On the **Claude path** tmux paste
> (`load-buffer`/`paste-buffer`/`send-keys`) is Claude's FIRST-CLASS input transport and is RETAINED;
> `capture-pane` survives as a human observation window only, NEVER as an ack signal (pane-scraping as
> an ack is dropped). **C1 ack** — the tmux/Claude path's real ack is SOURCED from the
> **Claude-hook-bridge** (`outcome_emitted` on `Stop`, `agent_ready` on `SessionStart` start/resume;
> [specs/claude-hook-bridge.md]), not from a pane observation or a Claude wire protocol; the
> handoff-completion done-gate is `outcome_emitted` + expected-artifact-present (AIS-018). See
> `05-spec-drafts/agent-input.md` (AIS-003, AIS-011, AIS-018, AIS-INV-001) for the corrected contract.

## Component map (dependency order)

```
C1 seam contract (input method + ack)  ─┬─► C2 structured-protocol driver ─┬─► C4 capture tee
                                        │                                  │
C3 observation-only tmux tap  ◄─────────┘                                  ▼
                                                        C5 replay/L0–L3 taxonomy + fault harness
                                                                           │
                                        (after bake window) ───────────────▼──► C6 delete input stack
                                                                           C7 WAL-guard fold-in (rides C2/C5)
```

---

## C1 — The seam input method + ACK contract

**What.** Widen `handler.Substrate` (or `SubstrateSession`) with a **first-class, typed input
operation that returns a real ack** — replacing the out-of-band type-asserted side-interfaces
and the no-op adapter. Concretely: retire the `substrateSessionAdapter.SendInput`/`CloseStdin`
no-ops (`internal/handler/substrate.go:140`, `:173`) in favor of a genuine
`SendInput`/`Submit(ctx, payload) (Ack, error)`-shaped method whose success means "the agent
accepted this input," not "tmux queued a buffer." Retire the reliance on `enterSender`,
`paneCapturer`, `quitSender`, `paneLivenessChecker`, `paneOutputSizer`,
`commandRunnerProvider` as the input mechanism (`pasteinject.go:187/206/236/254/280/493`).
**Spec surface:** the seam contract (PL-021b / HC-054 family) + a new input-protocol spec.

**Why separable.** It is the pure *contract* change — a Go interface + its spec. It can be
specced and type-checked before any driver exists, and it is the narrow waist every other
component plugs into. Keeping it separate preserves the depguard inversion (handler declares
the port; daemon supplies the driver — `substrate.go:5`).

**Rough order.** First. Everything else implements or consumes it.

**Open questions (design pass):**
- Method on `Substrate`, on `SubstrateSession`, or a new `InputPort` the driver satisfies
  structurally (queue-island style)? What does `Ack` carry — a sequence id, an "input
  cleared" observation, a protocol-level acceptance token?
- Is the ack a **synchronous return** or an **awaited event** on the reactor stream (P1's D11
  models timers/signals as events)? How does it compose with the existing async
  `agent_heartbeat`/commit signal — front-stop vs replacement?
- Bounded-liveness contract: what is the M2 analog of P1 SR9 / STEP-0a — "every input reaches
  ack-or-stale within a bounded window, never silence"? Where does the timeout live
  (ClockPort)?

## C2 — The structured-protocol driver (second `Substrate` impl)

**What.** A new `Substrate` implementation driving the **Codex app-server** (census
`PLAN.md:190`; proven, subscription-compatible, DONE — the whole M2 architecture was modeled
off it) — a `codexwire`-shaped wire codec + an `Event`/`Action` vocabulary + a `Step` state
machine, instantiated over the generic `Run[E,A]` loop (`internal/substrate/seam.go:27`) with
`Effector[A]` as the effect sink and `ClockPort` for all timing. This is NOT a claude driver:
Claude runs first-class on tmux (C3), by design; the structured driver is the Codex app-server
path. Injected at the daemon composition root alongside the tmux impl
(`workloop.go:489/4346`). **Spec surface:** input framing + state machine in the new spec; the
vertical instantiates `internal/substrate` (RS-prefixed seam spec from P1).

**Why separable.** It is the vertical-specific instantiation of the proven seam — a distinct
package with its own corpus, mirroring how codex is one instantiation and P1's keeper is
another. It composes against C1's port and is swappable at the composition root, so it can be
built and tested in isolation before it replaces anything.

**Rough order.** Second (needs C1's port). Largest single component — census flags replacing
~5.4k LOC of live IO substrate as "materially more than a few days" (`PLAN.md:208`).

**Open questions (design pass):**
- Wire protocol: RESOLVED per c019 — the Codex app-server protocol is proven; framing,
  method-registry shape, and which frames map to which reactor events (the `frameToEvent`-analog
  table) come from the existing codex vertical, not a speculative claude `stream-json` design.
- Generics instantiation: what concrete `E`/`A` types; does the fused `ReplayCodec[E]`
  (P1 D2) cover this protocol cleanly?
- Where does the driver own stdin/pipes vs delegate — the Codex app-server owns the process
  directly (no tmux window for the structured path).

## C3 — Observation-only tmux tap

**What.** Reduce the tmux path to a **read/observation window**: retain `capture-pane`
(`osadapter.go:512`) as a human-facing observation seam if useful; **retire the write verbs**
`load-buffer`/`paste-buffer`/`send-keys` (`osadapter.go:379/405/464/486/541`,
`tmuxsubstrate.go:2218` `WriteLastPane`). tmux no longer carries input. **Spec surface:** the
observation-only-tmux boundary clause in the new spec.

**Why separable.** It is the "demote, don't delete-yet" step — distinct from C6's hard delete.
During the bake window tmux still hosts the window for observation while C2 owns input, so this
component is the *decoupling* of write-from-read that gates the later deletion.

**Rough order.** Alongside C2 (they define the new boundary together); precedes C6.

**Open questions (design pass):**
- Does any observation value justify keeping `capture-pane` at all, or is the recorded capture
  tee (C4) the sole observation source? Is the tmux *window* even retained, or does the driver
  host the process directly?
- Which `internal/lifecycle/tmux/` pieces are input-only (deletable) vs shared with spawn/crew
  (`crewstart.go`) and remote (M4) and must survive?

## C4 — Live capture tee (apptap production wiring)

**What.** Splice `internal/apptap.Tap` (`InCapture`/`OutCapture`, protocol-agnostic,
`tap.go:48/58/63`) onto the driver's input/output stream so the production input stream is
**recorded verbatim for replay** — apptap's **first production consumer** (closes ROADMAP
orphan `ROADMAP.md:69`). **Spec surface:** capture/corpus persistence + redaction requirements.

**Why separable.** apptap is already built, green, and doc-declared protocol-agnostic; wiring
it as a production tee is an independent concern from the driver's protocol logic. It is the
bridge that makes C5's corpus real (production-captured, not spike-captured).

**Rough order.** After C2 exists to tee (needs a stream); feeds C5's corpus.

**Open questions (design pass):**
- Persistence: where/how are corpora written (rotation, retention, budget cap like
  `make capture-fixtures`)? One file per session?
- Redaction/secret-scan on the recorded stream before it lands (event-model secret-scan is the
  precedent).
- The P1 follow-up "record keeper live" (`ROADMAP.md:69`) — shared recorder infra with M2?

## C5 — Replay harness + L0–L3 taxonomy + fault injection

**What.** Instantiate `Twin[E]` + `FaultConfig` (`internal/substrate/replay.go`) over the
captured corpus and build the **L0–L3 taxonomy** (zero-token L0/L1/L2 + env-gated live L3 +
drift canary) — mirroring `internal/codextest`. Includes the **fault-injecting integration
test**: a stalled-agent injection asserting **output-or-stale, never silence** (census
`PLAN.md:197`), plus the **N-consecutive-full-runs** gate with **zero sleeps / zero
capture-pane scraping** in the path (`PLAN.md:200`). **Spec surface:** DoD/acceptance items
(harness bar), not spec prose (census `PLAN.md:197` is explicit these are DoD not spec paras).

**Why separable.** The test taxonomy is a distinct deliverable with its own DoD (the harness
bar M4 shares), built on the generic `Twin`/`FaultConfig` — separable from the driver it
exercises. It is also the acceptance oracle for C6's deletion decision.

**Rough order.** After C2 (logic to test) + C4 (corpus to replay). Gates C6.

**Open questions (design pass):**
- Which faults matter for input (drop/stall/truncate/dup map to what real failure — paste
  discard, TUI stall, partial submit)? What is the "output-or-stale" oracle concretely?
- Corpus provenance: production-captured (C4) vs a spike fixture; how large; drift-canary
  contents.
- Typed-decode adoption (P1 D6 / EV-033) — does M2's replay harness reuse the schema registry
  for invariant checks, as P1 does?

## C6 — Delete the paste-inject / tmux-write stack

**What.** After a **defined abort/rollback + bake window** (census `PLAN.md:202`), delete
`pasteinject.go` (2633 LOC), the input portions of `tmuxsubstrate.go` (2735 LOC), and the
input-only write verbs in `internal/lifecycle/tmux/`. The tmux escape hatch survives until the
bake passes. **Spec surface:** none new — this is teardown gated by C5.

**Why separable.** It is the irreversible teardown — deliberately last and behind a gate, so a
wrong ack/liveness contract on C1/C2 does not re-import the resume-hang with the escape hatch
already gone. Separating it makes the bake window an explicit checkpoint, not an afterthought.

**Rough order.** Last. Gated by C5 passing + bake window.

**Open questions (design pass):**
- Concrete abort/rollback criteria and bake-window length/metric (N clean runs? fleet trend?).
- Deletion boundary: what in `tmuxsubstrate.go` / `internal/lifecycle/tmux/` is shared with
  spawn (`crewstart.go`) and remote (M4) and must NOT be deleted here.
- **Keeper is IN the deletion boundary** (REVIEW-FINDINGS A11, added at decompose self-review
  2026-07-14): P1's shipped keeper vertical (`PanePort.Inject`, PL-021d
  `load-buffer`+`paste-buffer`) is a live consumer of the paste-inject input mechanism. C6
  MUST depend on "keeper migrated to the C2 structured driver" (or an explicit keeper
  carve-out that survives) — deleting the input path before the keeper migrates breaks the
  restart cycle. Design pass owns the migrate-vs-carve-out call.

## C7 — Fold in the codex daemon-harness WAL-guard

**What.** Absorb the codex **WAL-guard** (380 lines of symptom-treatment on the daemon
harness, census `REPORT.md:35`; homed to M2 by `ROADMAP.md:73`) into the rebuilt input path so
the guard's concern is handled by the structured protocol rather than a bolt-on.

**Why separable.** A contained clean-up that the rebuild subsumes; it rides on C2/C5 rather
than standing alone, but is tracked so it does not fall through the cracks.

**Rough order.** Rides C2/C5; not on the critical path.

**Open questions (design pass):**
- What exactly the WAL-guard compensates for, and whether the structured driver's ack makes it
  redundant (delete) or still needed (adapt).

---

## Goal → component traceability

| Goal (01) | Component(s) |
|---|---|
| G1 real input method + ack | C1 |
| G2 structured-protocol driver | C2 |
| G3 tmux observation-only | C3 |
| G4 live capture tee | C4 |
| G5 L0–L3 taxonomy + fault harness | C5 |
| G6 delete input stack | C6 |
| (spec-first / kerf) | C1 spec + all |
| WAL-guard orphan (ROADMAP) | C7 |

Every goal maps to ≥1 component; no component lacks a goal (C7 traces to a ROADMAP orphan
explicitly assigned to M2).
