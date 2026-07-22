# 04-Design / 00b — Change-Design review resolutions (AUTHORITATIVE)

> Resolves the seven items in `change-design-review.md` (verdict: changes requested). Where this
> doc pins a contract, it is **authoritative and supersedes any divergent restatement** in the
> four component design docs; the spec-drafter (pass 5) takes the definitions here as the single
> source of truth. Items 1–4 were the blocking cross-doc contradictions / the SB-R12 gap; 5–7 are
> completeness touches. None reopens a locked decision or a pinned contract in `00-decisions.md`.

---

## R1+R2 — The four §8.20 payload structs (CANONICAL)

`internal/core/keeperevents.go`. Every struct carries `AgentName` + required `CycleID` (json
`cycle_id`, **no** omitempty) with a `Valid()` method asserting non-empty `cycle_id` (per the
`reconciliationevents` precedent, events-design §3). These definitions are canonical; both
events-design §2.2/§2.6 and session-keeper-design §4 are subordinate to them.

```go
// §8.20.1
type SessionKeeperHandoffWrittenPayload struct {
    AgentName    string `json:"agent_name"`
    CycleID      string `json:"cycle_id"`                // REQUIRED
    SessionID    string `json:"session_id,omitempty"`
    Nonce        string `json:"nonce,omitempty"`         // confirmed nonce marker (audit)
    Recovered    bool   `json:"recovered,omitempty"`     // true iff accepted via freshness recovery (not nonce)
    HandoffMtime string `json:"handoff_mtime,omitempty"` // RFC3339; carried on the recovery edge
}
// §8.20.2
type SessionKeeperModelDonePayload struct {
    AgentName string `json:"agent_name"`
    CycleID   string `json:"cycle_id"`                   // REQUIRED
    SessionID string `json:"session_id,omitempty"`
    Source    string `json:"source"`                     // REQUIRED: "idle_marker" | "transcript_turn" | "timeout"
    Degraded  bool   `json:"degraded,omitempty"`         // true iff reached via model_done_timeout
}
// §8.20.3
type SessionKeeperClearSentPayload struct {
    AgentName string `json:"agent_name"`
    CycleID   string `json:"cycle_id"`                   // REQUIRED
    SessionID string `json:"session_id,omitempty"`
    Attempt   int    `json:"attempt"`                    // 1-based; increments on defensive re-injects
}
// §8.20.4
type SessionKeeperNewSessionUpPayload struct {
    AgentName     string `json:"agent_name"`
    CycleID       string `json:"cycle_id"`               // REQUIRED
    PrevSessionID string `json:"prev_session_id"`        // REQUIRED (needed for the != check)
    NewSessionID  string `json:"new_session_id"`         // REQUIRED; Valid(): non-empty AND != PrevSessionID
}
```

**Resolution of the specific divergences the review named:**
- `HandoffWrittenPayload` is the **union**: `Nonce` (events' audit field) AND `Recovered` +
  `HandoffMtime` (session-keeper's freshness-recovery edge). Dropping the latter two would break
  the session-keeper emission that sets `recovered:true` and carries mtime on the
  `TimerFired(handoff_timeout)+fresh` edge; dropping `Nonce` loses the audit value. Keep all three
  (the recovery fields are `omitempty` — absent on the normal nonce path).
- `ModelDonePayload`: **`Source` required, `Degraded` omitempty** (session-keeper's form).
  Rationale: `Source` is always meaningful (which of the three signals fired); `Degraded=true`
  only on the timeout path, so omitempty is correct.
- `NewSessionUpPayload`: **`PrevSessionID` required (no omitempty)** — the `!=` invariant needs it
  present; it is always known at emit time (`cf.SessionID` pre-clear).
- `ClearSentPayload`: unchanged (both docs already agreed).

The events-side roundtrip-test list and the `§8.20` compat rows use exactly these structs.

---

## R3 — keeperCodec responsibility / output→input synthesis (CANONICAL)

**Adopt measurement's model. session-keeper §3e's "decode the recorded output envelope and map
output→input inline" is SUPERSEDED.** The pinned architecture:

1. **Two distinct corpora, two distinct consumers:**
   - The **recorded OUTPUT log** (`.harmonik/events/baseline-2026-07-13/events.jsonl` — the 507
     cycles of `handoff_started`/`cycle_complete`/… envelopes) is consumed ONLY by the
     `internal/replay` invariant-checker (D6): it reads via `ScanAfter`, decodes with the typed
     registry, and checks SR3/4/6/7/9 over the emitted event order. **It does not drive the
     reactor.**
   - The **keeper INPUT-event corpus** (`testdata/keeper-cycles/baseline-2026-07-13/*.jsonl`) is
     what drives the reactor under replay. It is produced at **corpus-BUILD time** by the
     `StimulusSynthesizer`, which reads each cycle's `summary.json` (derived from the output log by
     the extractor) and emits a serialized schedule of reactor **input** events (`GaugeTick`,
     `NonceObserved`, `ModelDone`, `SessionChanged`, `TimerFired`, …). The output→input synthesis
     happens **once, offline, in the extractor** — never inside the codec.
2. **`ReplayCodec[keeper.Event].DecodeLine` deserializes already-synthesized input-event lines**
   (one serialized `keeper.Event` per line) from that input corpus. It performs NO output→input
   mapping. `ErrorEvent`/`DisconnectEvent` return the keeper `restart_failed`-class input stimuli
   (session-keeper's form is otherwise correct). `substrate.Twin[keeper.Event]` then applies the
   four faults over that decoded input stream and `substrate.Run` drives the reactor — structurally
   identical to codex (corpus → codec → Twin → reactor), which is the Goal-2/8 genericity proof.
3. **The old-vs-new differential** (measurement §4): the `StimulusSynthesizer` emits ONE abstract
   schedule per cycle; a reactive-fake adapter drives the OLD `Cycler` (via `CyclerConfig` port
   state mutations, the existing `reactiveSession` shape) and the SAME schedule serialized as the
   input corpus drives the NEW reactor. Both consume the identical synthesized stimulus.

session-keeper-design §3e is rewritten to say: "`keeperCodec` deserializes synthesized input-event
lines; the output→input synthesis lives in the measurement `StimulusSynthesizer`, not the codec."
measurement-design §2 and session-keeper §3e cross-reference each other.

---

## R4 — SB-R12 (two-layer decode discipline) — added to substrate scope

The substrate spec (RS) states the **two-layer decode discipline as a normative reusable pattern**,
with the concrete framing staying vertical-side:
- **Layer 1 (strict outer):** the transport/envelope frame is parsed strictly; a malformed
  envelope is a fatal decode error (`ReplayCodec.DecodeLine` returns `err != nil`, §5.2 skip-vs-
  fatal split).
- **Layer 2 (tolerant inner):** the per-message payload is parsed tolerantly — unmodeled fields are
  **preserved and counted**, and an unknown message maps to a typed-raw value (skip, `emit=false`),
  never a crash.
- **The concrete framing is NOT pulled into substrate:** codex's `Frame`/`FrameKind`/`Extra`/
  `parseExtra` machinery (findings §3) stays in `internal/codexwire`; substrate owns only the
  *pattern statement*. This pattern is what the EV `DecodePayloadStrict` variant (events §4)
  enforces on the invariant-checker side, and what the `DecodeLine` skip-vs-fatal contract embodies
  on the replay side — making the pattern discoverable from both.

Add to substrate-design's traceability table: **SB-R12 → this clause + ReplayCodec §5.2 +
EV DecodePayloadStrict.**

---

## R5 — HC-035 / SH / ON reference-touch pointers (recorded)

Recorded here so the spec-drafter carries them (they are light spec-text cross-references; the
substance is already covered):
- **HC-035 → RS:** the substrate spec (RS) now governs the in-process-fake surface that HC-035
  disclaims (the seam + the two test doubles). At integration (pass 6), HC-035 gains a pointer to
  RS and the two are mutually discoverable. Substance covered by substrate §1.1/§4.1.
- **SH-018 / SH-INV-001 → RS:** the substrate L0–L2 zero-token tiers satisfy SH's no-test-branch
  discipline and map onto SH's cadence taxonomy. RS cross-references SH; substance covered by
  substrate §4.
- **ON-059 → SK:** the session-keeper spec (SK) cites ON-059 (the restart-now gate ladder +
  thresholds) as the adjacent normative fragment; the 11-gate ladder (session-keeper §3c/§3d) and
  ON-059 become mutually discoverable. Substance covered.

These are integration-pass edits; no design change is required now.

---

## R6 — D13 permanent-net residual (recorded for the spec)

The old-vs-new differential is a **transition scaffold** deleted when the old `runCycle` path is
deleted; the *permanent* regression net (the L1 golden-vs-`summary.json` corpus test) is
`StimulusSynthesizer`-derived, so after the scaffold is gone the differential — the only
faithfulness check on the synthesizer itself — is gone too. Therefore the spec/measurement design
requires: **the `StimulusSynthesizer` decision table is frozen and reviewed against a green
old-vs-new differential BEFORE the scaffold is deleted**, and thereafter the independent nets are
(a) the fault matrix (distinct stimulus, §5) and (b) the out-of-band jq/stat oracle (§6), neither
of which is synthesizer-derived. Add this as a stated constraint in measurement-design §4.

---

## R7 — session-keeper traceability table

Add a closing traceability table to session-keeper-design mirroring the other three docs. Mapping
(already covered inline; consolidated here as the authoritative index):

| Req | Section |
|---|---|
| SK-R1 five ports | §1 |
| SK-R2 pure Step reactor | §3 |
| SK-R3 ClockPort replaces 34 clock sites | §2 |
| SK-R4 four durable events | §4 (+ events §2 for registration) |
| SK-R5 SR4 model-done-before-clear | §5, §4 |
| SK-R6 SR9 bounded liveness | §4 (+ measurement §5) |
| SK-R7 SR3/SR6/SR7 | §4 |
| SK-R8 property tests over corpus+faults | measurement §3/§5 (+ §6 here) |
| SK-R9 behavior-parity | §6 (+ measurement §4) |
| SK-R10 baseline anchor 84%/347 | measurement §7 |
| SK-R11 PanePort cites PL-021d | §1a |

---

## Status

All seven review items resolved. R1–R4 are authoritative contract pins (this doc governs on
conflict); R5–R7 are recorded touches for integration/spec-draft. Ready for re-review and, on
approval, advance to `spec-draft`.
