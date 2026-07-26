# Spec-Draft Pass (Pass 5) — Independent Review

**Work:** session-restart-substrate
**Reviewer:** independent spec-draft reviewer
**Date:** 2026-07-13
**Artifacts reviewed:** `05-spec-drafts/replay-substrate.md` (RS), `05-spec-drafts/session-keeper.md` (SK), `05-spec-drafts/event-model-amendment.md` (EV amendment), `05-changelog.md`
**Checked against:** `docs/foundation/spec-template.md` v1.1; `04-design/00-decisions.md` + `00b-review-resolutions.md` (governing) + `02-components.md`; live `specs/event-model.md` (HEAD 5160326b) + `specs/_registry.yaml`.

---

## Verdict: **Approved** (with minor, non-blocking nits to sweep at integration)

The three drafts are faithful to the authoritative design, cover every decomposed requirement, and are internally consistent on every load-bearing contract. All pinned signatures and the four canonical payload structs appear **verbatim**. The EV amendment is factually accurate against the live spec. The only findings are cosmetic template-lint nits and one changelog miscount — none is a design mismatch, requirement gap, or cross-doc inconsistency, so none rises to "Changes requested." Recommend fixing nits 1–2 before `kerf finalize`.

---

## Per-criterion assessment

### 1. Template conformance — 4/5

- **§0–§12 present in both drafts.** RS and SK each carry §1 Purpose, §2 Scope (2.1/2.2), §3 Glossary, §4 Normative requirements, §5 Invariants, §6 Schemas, §7 Protocols, §8 Error taxonomy, §9 Cross-references (9.1/9.2/9.3), §10 Conformance (10.1/10.2/10.3), §11 Open questions, §12 Revision history. Ordering matches `requirements-first`.
- **Front matter** — SK is fully template-conformant. **NIT (RS):** `replay-substrate.md:11` declares `spec-category: runtime-subsystem`, a field that is **not** in the template §0 front-matter schema; SK correctly omits it. Extra/undocumented field, inconsistent between the two new drafts.
- **RFC 2119 discipline** — every MUST/SHOULD/MAY sits inside a numbered requirement/invariant block. Purpose/Scope/Glossary/§7/§8 use descriptive verbs (`satisfies`, `is reached at`, `terminates`); lowercase "may" in RS §8 ("no fault mode may produce…") points at RS-INV-003 and is non-normative. No loose normative keywords found; no MUST inside an INFORMATIVE/RATIONALE callout.
- **IDs + Tags** — every requirement has a unique `<prefix>-NNN` anchor and a `Tags: mechanism` line. RS-001..023 with a **legal gap at RS-013** (changelog-declared; template permits gaps). SK-001..020 + SK-INV-001..005. Each ID appears exactly once as an anchor.
- **Axes** — applied where required and omitted (declaration-only) elsewhere, correctly. SK's IO/mutation/emit requirements (SK-002/003/004/007/011/012/013/014/015/017/020, SK-INV-005) carry Axes; RS-016 carries Axes. Axis grammar (fixed order, `; ` separator, lowercase, no spaces around `=`, valid tokens incl. `replay-safety=n/a`) is correct. *(Trivial: EV-047 carries an Axes line equal to baseline — redundant per template guidance, harmless.)*
- **Informative markers** — RS uses `> INFORMATIVE:` / `> RATIONALE:` correctly; none contains a normative keyword. No rationale leaks into normative bodies (requirement text says "the system MUST X," not "we chose X because"; the one `> RATIONALE:` under RS-003 is correctly a callout).
- **NIT (RS cross-ref form):** §9.3/§10.2 use `[scenario-harness.md]` and `[handler-contract.md]` with **no section number**, deviating from the strict `[spec-id.md §N.N]` form. Minor.
- **NIT (SK §11):** says "None blocking." rather than the literal "None." The section is honest (the two deferred items are design-deferrals, not open decisions), but the template's exact-token expectation is "None."

### 2. Design fidelity — 5/5

Every pinned contract appears unchanged from the governing design (`00-decisions.md` + `00b`):

- **Generic seam (D1):** RS §6.1 `EventSource[E]`, `Effector[A]`, free-function `Run[E,A any]` — verbatim. Test doubles `FakeEffector[A]` / `SyntheticSource[E]` (§6.2) — verbatim.
- **ReplayCodec/Twin (D2):** RS §6.3 `ReplayCodec[E]` (`DecodeLine`/`ErrorEvent`/`DisconnectEvent`), `FaultConfig{Mode,EventN}`, and the `FaultMode` const order `FaultNone, FaultDropAfter, FaultStall, FaultTruncate, FaultDup` — verbatim. `NewTwin` signature + 1 MB buffer default (RS-010) — matches D3.
- **ClockPort (D4):** RS §6.4 and SK §6.1 both give `Now/Since/NewTicker/Sleep(ctx,d) bool` + `Ticker{C()/Stop()}`; SK requires it by reference (SK-006). Consistent.
- **5 ports + RespawnPort (D10):** SK-001..007 name PanePort/GaugePort/HandoffPort/EmitterPort/ClockPort + one-method RespawnPort; PanePort methods `Inject/SendEscape/SetEnv/Capture/OperatorAttached`; EmitterPort = `keeper.Emitter` subset; GateSnapshot with the 7 predicate reads. All match.
- **Step machine (D11):** states `Idle → AwaitingHandoff → AwaitModelDone → Clearing → Briefing → {Complete|Aborted}`, timers-as-events (`ArmTimer`/`CancelTimer`/`TimerFired`), 4 timer kinds `handoff_timeout/model_done_timeout/clear_settle/clear_backstop`. Match.
- **Four canonical payload structs (00b R1+R2) — verbatim in BOTH SK §6.4 and EV §3.2**, byte-identical to `00b` (field names, json tags incl. `cycle_id` no-omitempty, `omitempty` on recovery/degraded fields, and comments):
  - `model_done` `Source` REQUIRED enum `"idle_marker" | "transcript_turn" | "timeout"` — present (SK §6.4.2 / SK-014 / EV §8.20.2). ✓
  - `HandoffWritten` carries `Nonce` + `Recovered` + `HandoffMtime` (the union) — present. ✓
  - `NewSessionUp.PrevSessionID` REQUIRED (no omitempty), `Valid()` asserts `NewSessionID != PrevSessionID` — present. ✓
- **§8.20 / cycle_id / EV-046..050** — all present and consistent (see criteria 4–5).

No signature, field, or enum drift found.

### 3. Requirement coverage — 5/5

Every SB-R#/SK-R#/EV-U# maps to at least one drafted requirement; spot-checks below confirm the mapping against actual spec text, not just the drafters' tables.

### 4. Cross-doc consistency (RS↔SK↔EV) — 5/5

- **Event names:** `session_keeper_{handoff_written,model_done,clear_sent,new_session_up}` identical across SK (§2.1/§4.4/§6.5/invariants), EV (§3.1 §8.20 rows), and the emission-ordering notes. All use the `session_keeper_*` prefix (EV-U1a), never the `keeper_*` shorthand.
- **§8.20:** SK cites `[event-model.md §8.20]` for registration/shape and owns only the *when*/ordering; EV §8.20 owns registration/shape and points ordering back to SK. Clean co-ownership, no double-normative-claim.
- **Payload structs:** SK §6.4 ≡ EV §3.2 ≡ `00b` R1+R2 (verified identical).
- **ClockPort/seam signatures:** RS §6.1/§6.4 ≡ SK §6.1 restatement; SK defers the type definition to `[replay-substrate.md §6]`.
- **cycle_id semantics:** SK glossary and EV-046 both pin the join key as the composite `(agent_name, cycle_id)`, both note `cyc-<ts>-<seq>` is per-process-non-unique. Identical.
- **Cross-references:** RS↔SK↔EV are mutually discoverable (RS §9 names SK as 2nd instantiation + depends-on event-model; SK §9.1 depends-on replay-substrate §4/§6 + event-model §8.20 + PL + ON; EV §5 cross-refs both). No dangling or contradictory pointer.

**Mismatches found: none.**

### 5. EV amendment accuracy vs live `event-model.md` — 5/5

Verified against HEAD 5160326b:

- **§8.13/8.14/8.15** are exactly Epic-completion / HITL-decisions / Bead-ledger merge (`event-model.md:354/368/392`). Amendment's collision claim is accurate. ✓
- **§8.16–8.20 free** — §8 headings end at §8.15; no §8.16+ exists. ✓
- **EV-046 is the next free ID** — highest live ID is EV-045 (`event-model.md:380`, §8.14 `decision_id` keying). ✓ EV-046..050 assigned; no prior ID renumbered.
- **Version** — live is `0.6.4`, status `reviewed`; amendment bumps to `0.7.0`, keeps `reviewed`. ✓
- **§8.9(b) exception argued, not asserted** — §3.3 walks (a)–(h), reframes (b) ("these ARE the lifecycle boundaries of the restart state machine's sub-lifecycle"), and contrasts the deferred `tool_command_completed`. It correctly claims §8.12/§8.14 carry inline §8.9 evidence — verified true (`event-model.md:348,386`). ✓
- **Placement anchors** — EV-047 after EV-021 (§4.5), EV-046 after EV-025 (§4.6), EV-050 after EV-027 (§4.6), EV-049 after EV-028 (§4.7): all four anchor requirements exist in exactly the cited sections. ✓
- **Registry** — RS, SK, replay-substrate, session-keeper all absent from `specs/_registry.yaml` (both prefixes free, as the drafts' landing actions state). ✓

---

## Requirement-coverage spot-check

| Source req | Drafted req(s) | Verified against real spec text? |
|---|---|---|
| SB-R1 (seam + Run loop) | RS-001, RS-002 | Yes — RS-002 pins `Run` as free function, ranges `src.Events`, returns first effector error |
| SB-R2 (typed, not stringly) | RS-003 | Yes — "MUST NOT be an `any`-typed or string-keyed boundary" |
| SB-R4 (replay Twin) | RS-008..011 | Yes — Twin presents corpus **as** EventSource; codec-only vertical surface |
| SB-R5 (fault model) | RS-012, RS-INV-003 | Yes — 4 vertical-neutral modes + terminal-never-silence |
| SB-R9 (ClockPort) | RS-015 | Yes — Now/Since/NewTicker/Sleep, real+fake, first-tick semantics |
| SB-R11 (capture-tee) | RS-016 | Yes — "MUST own no file format and MUST NOT open or name any file" |
| SB-R12 (two-layer decode) | RS-014 | Yes — strict outer / tolerant inner, framing stays vertical |
| SK-R1 (5 ports) | SK-001..007 | Yes — ports enumerated with method surfaces |
| SK-R5 (SR4 model-done before clear) | SK-014, SK-INV-002 | Yes — "`session_keeper_model_done(c)` MUST be emitted before `session_keeper_clear_sent(c)`" |
| SK-R6 (SR9 bounded liveness) | SK-015, SK-INV-005 | Yes — bounded window ≈520s or `restart_failed`; silence FORBIDDEN |
| SK-R11 (PanePort cites PL-021d) | SK-002 | Yes — Inject follows PL-021d; Capture keeper-only per PL-021b §5 |
| EV-U2 (cycle_id join, no zero run_id) | EV-046 | Yes — REQUIRED `cycle_id`; envelope `run_id` MUST be absent; composite key |
| EV-U3 (adopt typed-decode) | EV-048, EV-049 | Yes — registry adopted; additive `DecodePayloadStrict` |
| EV-U4 (ScanAfter read surface) | EV-047 | Yes — promoted to first-class; cross-writer EventID re-sort caveat |
| EV-U5 (§8 reconciliation) | §2 (§8.16–8.19 + comment fixes) | Yes — anchored on verified live §8.13–8.15 |

---

## Cross-draft consistency findings

**None.** Event names, §8.20 co-ownership split, the four payload structs, ClockPort/seam signatures, and cycle_id composite-key semantics are identical everywhere they appear across RS, SK, and the EV amendment.

---

## Minor nits (recommended, non-blocking)

1. **`05-spec-drafts/replay-substrate.md:11`** — remove the non-template front-matter field `spec-category: runtime-subsystem` (not in template §0; SK omits it). Keeps the two new specs consistent and lint-clean.
2. **`05-spec-drafts/replay-substrate.md` §9.3 / §10.2** — the cross-refs `[scenario-harness.md]` and `[handler-contract.md]` omit a section number; use the `[spec-id.md §N.N]` form (e.g. `[scenario-harness.md §<n>]`, `[handler-contract.md §<n>]`) or reference the requirement ID bare (`SH-018`, `HC-035`) without the sectionless bracket.
3. **`05-changelog.md`** (SK entry) — "25 requirements (SK-001..SK-020 + the port sub-IDs)" miscounts: the draft has 20 requirement blocks (SK-001..SK-020); there are no separate port sub-ID blocks. Correct to "20 requirements." Changelog-only; the draft itself is fine.
4. **`05-spec-drafts/session-keeper.md` §11** — use the literal token "None." (the template's expected form) alongside the deferred-items INFORMATIVE note.
5. *(Trivial)* EV-047 carries an Axes line equal to baseline; may drop per template guidance, or keep since it reads a file. Either is acceptable.

None of nits 1–5 blocks integration; 1 and 2 are the only genuine template-lint misses and should be fixed before `kerf finalize`.
