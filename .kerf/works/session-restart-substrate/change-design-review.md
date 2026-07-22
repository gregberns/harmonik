# Change-Design Review (pass 4) — `session-restart-substrate`

> Independent reviewer verdict on `04-design/00-decisions.md` + the four component design
> docs (`substrate`, `session-keeper`, `events`, `measurement`) against `02-components.md`
> (SB-R1..R14, SK-R1..R11, EV-U1..U5) and the four `03-research/*/findings.md`. Load-bearing
> code claims spot-checked against the repo at `/Users/gb/github/harmonik`.

## Verdict: **Approved** (re-review 2026-07-13, after `00b-review-resolutions.md`)

**Original verdict was "Changes requested"; all seven items are now resolved and re-verified.**
See the "Re-review" section at the end. The coordinator added an AUTHORITATIVE addendum
`04-design/00b-review-resolutions.md` (governs on conflict; component docs subordinate on the
contested items) AND patched the literal contradictions in place. The design is strong, deeply
research-grounded, and disciplined about not gold-plating; the four high-risk calls (D1 alias
re-instantiation, D12 model-done, D8 §8.9(b) exception, D13 trace-driven Twin) are sound or
adequately argued (details below). The material below is the original review; the closing
Re-review section records the final verdict.

---

## Per-criterion assessment

**1. Each doc has current/target/rationale/traceability — MOSTLY MET.**
substrate, events, measurement each carry current-state (code-anchored), target state, rationale
(cites findings), and a closing Traceability table. **`session-keeper-design.md` has no
consolidated traceability table** — requirement mapping is inline (SK-R# tagged per section) and
in 00-decisions' D→requirement table. Minor; add a closing table for parity with the other three.

**2. Every requirement addressed by a target state — ONE GAP (SB-R12).** Full checklist below.
All SK-R1..R11 and EV-U1..U5/U1a are addressed. All SB except **SB-R12 (two-layer decode
discipline) is not addressed anywhere** in substrate-design (no mention, not in its traceability
table, not explicitly deferred). The EV side covers strict/non-strict decode (DecodePayloadStrict),
but SB-R12 is a *substrate-spec* requirement to state the two-layer decode as a normative reusable
pattern (with the concrete framing staying vertical-side); the substrate design is silent on it.

**3. No un-backed target state / no invented abstraction — MET.** The design is notably
disciplined. Every added shape has a cited justification: ReplayCodec fusion (5 leak points,
findings §1.5), RespawnPort as its own one-method port (process-lifecycle ≠ pane write, D10),
GateSnapshot (keeps Step pure, findings §1b), DecodePayloadStrict (EV-U3/D6), routing keeper
stimulus through substrate.Twin (zero keeper-specific fault code). R9 explicitly *forbids* a
generic bridge sink. No monads (Constraint 3). One minor additive-beyond-pin: events' `Report`
adds `RegisteredNeverObserved` not in the D6 pin — justified by the operator_attached precedent
(§4.6); elaboration, not contradiction.

**4. Current state accurately reflects reality — MET (spot-checked).** Verified against repo:
`specs/event-model.md` §8 headings end at §8.15 and §8.20 is free (D8); zero external importers
of `codexreactor`/`codexdigitaltwin` and zero `apptap` importers (D1 blast radius); `FileEmitter`
does open/write/close with **no `Sync()`** (D9 O-class rationale); `emitOperatorAttached` is an
empty-body no-op (events §4.6 / session-keeper §6.7); the four new event names have 0 hits. The
research's grep-verified counts (34 clock sites, 18 keeper types, 507 composite cycles, 78K
zero-run_id events) are carried faithfully.

**5. Specific enough for a spec writer — MOSTLY MET.** Exceptionally concrete (file:line, verbatim
signatures, full transition table, runnable extraction script, exact Makefile targets, jq
recompute commands). The **one under-specified seam is the keeperCodec / output→input synthesis**
(see Cross-doc finding X2): two docs describe it incompatibly, so a drafter cannot produce one
consistent answer for "what does `ReplayCodec[keeper.Event]` consume."

**6. Rationale references research findings — MET, strongly.** Every design section cites the
relevant `findings §`; decisions in 00-decisions carry evidence pointers. Best-in-class here.

**7. No contradictions across docs / vs 00-decisions — NOT MET (the headline).** The seam
contract, event names, §8.20, cycle_id, ClockPort, the 5 ports, and the Step machine are
consistent everywhere. But the **four new payload struct definitions diverge between
session-keeper-design and events-design**, and the **keeperCodec's consumed input diverges
between session-keeper-design and measurement-design**. Details in the mismatch list.

---

## Requirement-coverage checklist

| Req | Status | Addressed where |
|---|---|---|
| SB-R1 seam/Run | ✅ | substrate §1.1, §2; D1 |
| SB-R2 typed genericity | ✅ | substrate §1.3 (compile-time property); D1 |
| SB-R3 two doubles | ✅ | substrate §1.1 (doubles.go), §4.1 |
| SB-R4 replay-Twin | ✅ | substrate §5, §2.3; D2 (fused codec supersedes the 2-fn sketch — argued) |
| SB-R5 fault model | ✅ | substrate §5; D3 |
| SB-R6 L0–L3 taxonomy | ✅ | substrate §4 (+ minimum-artifact list §4.2) |
| SB-R7 codex ref-impl green | ✅ | substrate §2, §2.4 checklist |
| SB-R8 SK 2nd instantiation | ✅ | session-keeper (whole); substrate §0 |
| SB-R9 ClockPort | ✅ | substrate §3; D4 |
| SB-R10 measurement standard | ✅ | substrate §6; measurement doc; D13 |
| SB-R11 capture-tee Tap | ✅ | substrate §1.2 |
| **SB-R12 two-layer decode** | ❌ **MISSING** | not addressed in substrate-design; not deferred |
| SB-R13 port idiom / no monads | ✅ | substrate §1.3 |
| SB-R14 naming disambiguation | ✅ | D5 (RS rename), substrate §1 doc.go |
| SK-R1 five ports | ✅ | session-keeper §1; D10 |
| SK-R2 pure Step reactor | ✅ | session-keeper §3; D11 |
| SK-R3 ClockPort replaces clock sites | ✅ | session-keeper §2 (34 sites); D4 |
| SK-R4 four durable events | ✅ | session-keeper §4; events §2 |
| SK-R5 SR4 (/clear never before model-done) | ✅ | session-keeper §5, §4; D12 |
| SK-R6 SR9 bounded liveness | ✅ | session-keeper §4; measurement §5 |
| SK-R7 SR3/SR6/SR7 | ✅ | session-keeper §4 |
| SK-R8 property tests over corpus+faults | ✅ | measurement §3/§5; session-keeper §6 |
| SK-R9 behavior-parity | ✅ | session-keeper §6; D11; measurement §4 |
| SK-R10 baseline anchor | ✅ | measurement §7; D13 |
| SK-R11 PanePort cites PL-021d | ✅ | session-keeper §1a |
| EV-U1 register 4 events | ✅ | events §2; D8 |
| EV-U1a session_keeper_* naming | ✅ | events §2 (l.109) |
| EV-U2 cycle_id joinability | ✅ | events §3; D7 |
| EV-U3 adopt typed-decode | ✅ | events §4; D6 |
| EV-U4 declare ScanAfter | ✅ | events §5 |
| EV-U5 §8 drift reconcile | ✅ | events §1; D8 |
| PL (PL-021d/PL-021b) touch | ✅ | session-keeper §1a, SK-R11 |
| HC (HC-035 → SB) touch | ⚠️ | **not explicitly addressed** in substrate-design (SB is meant to "fill that gap"); left to spec-drafter |
| SH (SH-018/SH-INV-001) touch | ⚠️ | substrate §4 covers zero-token tiers but does not cross-reference SH-018/SH-INV-001 |
| ON (ON-059) touch | ⚠️ | session-keeper §3c covers the 11-gate ladder but does not cite ON-059 |

The three ⚠️ reference touches are light spec-text cross-references by nature; the *substance* is
covered (PanePort/PL, taxonomy/SH no-test-branch, gate-ladder/ON). Reasonable to carry to the
spec-drafter, but HC-035 deserves an explicit pointer since SB is the spec that now governs that
surface — see fix #5.

---

## Cross-doc consistency findings (the mismatch list)

**X1 — The four §8.20 payload structs are defined DIFFERENTLY in session-keeper-design §4 vs
events-design §2.2 (HIGH; a real contradiction).** Both docs restate the structs; events-design
also declares itself the registration owner ("session-keeper does NOT duplicate the §8.20 catalog
work") yet session-keeper re-states them with divergent fields:

- `SessionKeeperHandoffWrittenPayload`: session-keeper has `Recovered bool` + `HandoffMtime
  string`; events has `Nonce string`. **Neither is a superset of the other.** session-keeper's
  emission table *sets* `handoff_written{recovered:true}` and "carry mtime" on the freshness-
  recovery edge — so dropping those fields (as events-design does) breaks the described emission;
  events' `Nonce` (audit) is absent from session-keeper. events §2.6's field census lists
  `nonce` but neither `recovered` nor `handoff_mtime`, confirming the divergence is real, not a
  transcription slip.
- `SessionKeeperModelDonePayload`: omitempty tags are swapped — session-keeper has `Source`
  required / `Degraded,omitempty`; events has `Degraded` required / `Source,omitempty`. Affects
  roundtrip goldens and the interior-order `types` golden.
- `SessionKeeperNewSessionUpPayload`: `PrevSessionID` is `json:"prev_session_id"` (required) in
  session-keeper, `,omitempty` in events.
- `SessionKeeperClearSentPayload`: identical in both. ✅

**X2 — The `ReplayCodec[keeper.Event]` (keeperCodec) consumes different inputs in session-keeper
§3e vs measurement §2 (MEDIUM-HIGH; architectural inconsistency).**
- session-keeper §3e: `keeperCodec.DecodeLine` "json.Unmarshal the core.Event envelope" — i.e.
  it reads **recorded output events** (bus-JSONL) and maps output→input inline
  (`handoff_written→NonceObserved`, `new_session_up→SessionChanged`, …).
- measurement §2.1/§2.3: "the baseline records keeper **outputs**, not inputs … the corpus
  cannot be fed *directly* into the new Step reactor." A separate `StimulusSynthesizer` reads
  `summary.json` → produces input events → serialized as an in-memory stimulus stream → the
  keeperCodec's `DecodeLine` "decodes one **synthesized stimulus line**."

These are two incompatible seams: in one, output→input synthesis lives *inside* the codec reading
recorded envelopes; in the other, synthesis is a distinct pre-Twin component and the codec merely
deserializes already-synthesized inputs. measurement's is internally coherent (matches its
"outputs ≠ inputs" premise); session-keeper §3e contradicts that premise. A drafter/implementer
needs one answer.

**X3 — (minor) `Report` shape.** events §4.1 adds `RegisteredNeverObserved []core.EventType` to
the D6-pinned `Report`. Additive elaboration, justified by §4.6; not a contradiction, but the pin
is no longer restated verbatim. Acceptable; note it.

Everything else cross-checks clean: event names (`session_keeper_handoff_written/model_done/
clear_sent/new_session_up`), §8.20 placement, composite `(agent_name, cycle_id)`=507, ClockPort
(Now/Since/NewTicker/Sleep + Ticker), the 5 ports + RespawnPort, the Step machine
(`Idle→AwaitingHandoff→AwaitModelDone→Clearing→Briefing→{Complete|Aborted}`), the ReplayCodec
method triple, and `Replay(path, since, strict, checkers)` are identical across the docs that
touch them.

---

## Sanity-check of the four highest-risk calls

**(a) D1 alias-based codex re-instantiation keeps codextest green — SOUND.** Aliases (`=`) of
instantiated generic types preserve type identity, so composite literals (`&codexreactor.
FakeEffector{}`) and `reflect.DeepEqual([]codexreactor.Action, …)` resolve to concrete codex
types (correct Go semantics). Blast radius verified: **0 external importers** of
`codexreactor`/`codexdigitaltwin`; `Run`-as-free-function is covered by the one-line wrapper at
the three call sites. The §2.4 checklist is the right gate. Note the load-bearing risk (defined
type instead of alias) is correctly called out as a review-checklist item, not a design change.

**(b) D12 model-done `.idle`-mtime signal — SOUND.** `keeper-stop-hook.sh` fires only at
await-input boundaries; "first `mtime(.idle) ≥ t_nonce`" correctly captures "the turn that wrote
the handoff has ended" (kills the `/clear`-races-the-tail defect). Strict compare (no
`crispIdleTolerance`) is justified. The transcript backstop covers unwired Stop hooks; the ~60s
fail-open bound (< 150s backstop) prevents wedge and preserves today's clear-immediately behavior
as the degraded mode. Residual (acceptable, stated): depends on the Stop hook / transcript being
truthful; both fallbacks are in place.

**(c) D8 §8.9(b) cycle-interior exception — GENUINELY ARGUED, not asserted.** events §7.1 engages
each criterion: (a) real cross-subsystem consumer (`internal/replay`), (c) per-boundary access is
genuinely required (SR3/4/6 are orderings *between* interior milestones — a summary event cannot
express "clear_sent after model_done"), (b) reframed as the sub-lifecycle's own boundaries, (h)
four distinct single-emission milestones so no status-field merge. It contrasts with the
`tool_command_completed` deferral (failed for *no* consumer). The (b) reframe is mildly
self-serving (redefining the lifecycle to make interior events into boundaries) but is carried by
(a)+(c) being independently satisfied. Adequate.

**(d) D13 trace-driven Twin can't false-pass — ADEQUATE, with one residual gap to state.** The
core defense holds: the synthesizer maps recorded outcome → *stimulus schedule* (nonce
never/late/prompt), **not** outcome→outcome, so a correct reactor must independently run its
timeout/backstop logic to reach the terminal (a reactor that never times out shows "no terminal
within bound" = fail). The old-vs-new differential (§4) cross-checks the same synthesized
schedules against the production-proven old `Cycler`, which is a strong independent oracle, and
the out-of-band jq/stat checks (§6) never route through the replay path.
**Residual gap worth a spec note:** the differential is a *transition scaffold* deleted with old
`runCycle`; the *permanent* net is the L1 golden-vs-summary corpus test, whose inputs are
synthesizer-derived — so after deletion the only validation that the synthesizer itself is
faithful (the differential) is gone. A synthesizer + reactor sharing a blind spot could then
false-pass. Recommend the spec state that the synthesizer's decision table is frozen/reviewed
against the differential *before* the scaffold is deleted, and that the fault matrix (§5, distinct
stimulus) plus the out-of-band oracle remain the independent nets thereafter.

---

## Changes requested (numbered, actionable)

1. **Reconcile the four §8.20 payload structs to a single authoritative definition
   (`events-design.md` §2.2 is the owner; `session-keeper-design.md` §4 must match or stop
   restating).** Specifically for `SessionKeeperHandoffWrittenPayload`: decide the union — it
   almost certainly needs **`Nonce` (audit, events) AND `Recovered`+`HandoffMtime` (the
   freshness-recovery edge, session-keeper)**, since session-keeper's emission sets
   `recovered:true` and carries mtime. Update events §2.2/§2.6 and the roundtrip-test list to
   include them, or remove them from session-keeper's emission design. (Files: `events-design.md`
   §2.2 & §2.6; `session-keeper-design.md` §4.)

2. **Align the omitempty tags** on `SessionKeeperModelDonePayload` (`Source` vs `Degraded`
   required/omitempty) and `SessionKeeperNewSessionUpPayload` (`PrevSessionID`) between the two
   docs. Pick one and make both docs identical, since roundtrip/interior-order goldens assert
   exact JSON. (Files: `events-design.md` §2.2; `session-keeper-design.md` §4.)

3. **Resolve the keeperCodec responsibility split (X2).** State, in one place, whether
   `ReplayCodec[keeper.Event].DecodeLine` consumes (i) recorded `core.Event` output envelopes and
   performs output→input synthesis inline (session-keeper §3e), or (ii) already-synthesized
   stimulus lines produced by a separate `StimulusSynthesizer` from `summary.json` (measurement
   §2). measurement's model is the coherent one given "outputs ≠ inputs"; recommend adopting it
   and rewriting session-keeper §3e's `keeperCodec` description to match (codec deserializes
   synthesized inputs; synthesis lives in the measurement synthesizer). (Files:
   `session-keeper-design.md` §3e; `measurement-design.md` §2 — make them cite each other.)

4. **Address SB-R12 in `substrate-design.md`.** Add a short target-state clause: SB states the
   two-layer decode discipline (strict outer envelope / tolerant inner payload with
   unmodeled-field preserve-and-count / unknown → typed-raw-not-crash) as a normative *reusable
   pattern*, with the concrete framing staying vertical-side (codexwire's Frame/Extra machinery is
   NOT pulled into substrate — findings §3). Tie it to the ReplayCodec `DecodeLine` skip-vs-fatal
   split (§5.2) and the EV `DecodePayloadStrict` variant so the pattern is discoverable. (File:
   `substrate-design.md` — new subsection + traceability row.)

5. **Add the explicit HC-035 pointer.** In `substrate-design.md`, note that SB now governs the
   in-process-fake surface HC-035 disclaims (the seam + doubles), making the two mutually
   discoverable — this is a stated deliverable of the HC reference touch that no design doc
   currently records. (File: `substrate-design.md` §1.2 or §0.) Optionally add the SH-018/
   SH-INV-001 and ON-059 cross-reference pointers (§4 and §3c respectively), or explicitly hand
   them to the spec-drafter.

6. **State the D13 permanent-net residual (from sanity-check d).** In `measurement-design.md` §4,
   record that the synthesizer decision table must be validated against the old-vs-new
   differential *before* the scaffold is deleted, since the permanent L1 golden net is
   synthesizer-derived and the differential is the only faithfulness check on the synthesizer.
   (File: `measurement-design.md` §4.)

7. **(Minor) Add a closing Traceability table to `session-keeper-design.md`** mirroring the other
   three docs, so SK-R1..R11 → section coverage is auditable in one place.

None of these reopen a locked decision or a pinned contract; all are internal-consistency and
coverage fixes. Once #1–#4 land the docs are mutually consistent and the spec-drafter has one
answer per contract.

---

## Re-review (2026-07-13) — all seven items verified → **Approved**

Read `04-design/00b-review-resolutions.md` (authoritative addendum) and spot-checked the in-place
edits. Findings:

- **#1 / X1 (payload structs) — RESOLVED & VERIFIED.** 00b R1+R2 pin canonical structs. Verified
  in place in BOTH docs: `SessionKeeperHandoffWrittenPayload` is now the union `Nonce` +
  `Recovered` + `HandoffMtime` (events-design §2.2 l.155–162; session-keeper §4 l.453–460);
  `ModelDonePayload` = `Source` required / `Degraded,omitempty` in both; `NewSessionUpPayload.
  PrevSessionID` required (no omitempty) in both; `ClearSentPayload` identical. The recovery fields
  are `omitempty` so the normal nonce path stays clean. The union preserves session-keeper's
  `handoff_written{recovered:true}` emission — the exact break the finding named.
- **#2 / X2 (keeperCodec) — RESOLVED & VERIFIED.** 00b R3 adopts measurement's model authoritatively.
  session-keeper §3e rewritten in place (l.392–419): the codec **deserializes already-synthesized
  input-event lines**; output→input synthesis lives in the measurement `StimulusSynthesizer` at
  corpus-build time; the raw recorded OUTPUT log is consumed only by the `internal/replay`
  invariant-checker and does not drive the reactor. Old-corpus `ModelDone` synthesis correctly
  moved from the codec into the synthesizer. Both docs now cross-reference. The two-corpora /
  two-consumers split is now unambiguous.
- **#3 / SB-R12 — RESOLVED.** 00b R4 states the two-layer decode discipline as a normative reusable
  pattern (strict outer / tolerant inner preserve-and-count / unknown→typed-raw), concrete framing
  staying vertical-side (codexwire), tied to the ReplayCodec skip-vs-fatal split and EV
  `DecodePayloadStrict`, with a traceability row. Gap closed.
- **#4 / HC-035, SH, ON pointers — RESOLVED.** 00b R5 records all three as integration-pass
  cross-references with the substance already covered; correctly scoped as pass-6 edits.
- **#5 / D13 permanent-net residual — RESOLVED.** 00b R6 requires the `StimulusSynthesizer` decision
  table be frozen and reviewed against a green old-vs-new differential BEFORE the scaffold is
  deleted, with the fault matrix + out-of-band jq/stat oracle as the non-synthesizer-derived
  permanent nets. This directly closes the residual I flagged in sanity-check (d).
- **#6 / #7 (traceability) — RESOLVED.** 00b R7 supplies the authoritative SK-R1..R11 → section
  index; session-keeper-design gets the closing table.

**Load-bearing code claims** (spot-checked against the repo during the original review) remain
valid: §8.20 free, FileEmitter has no `Sync()`, `emitOperatorAttached` is a no-op, 0 external
importers of the codex packages, new event names absent.

**One trivial nit for the spec-drafter (non-blocking):** 00b R1 abbreviates the `model_done`
`Source` enum as `"transcript"` (l.33) while both component docs and measurement use
`"transcript_turn"`. Pick one string at spec-draft; does not affect approval.

**Final verdict: Approved.** The four blocking contradictions/gaps are closed, the pins are
mutually consistent (00b governs on conflict), requirement coverage is complete, and the four
high-risk design calls hold. Ready to advance to spec-draft.
