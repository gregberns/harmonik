# 06 — Integration (Pass 6): corpus-wide cross-reference, contradiction & terminology check

> **Pass 6 (Integration), session-restart-substrate.** Verifies the three Pass-5 drafts
> (`replay-substrate.md` / RS, `session-keeper.md` / SK, the `event-model.md` amendment / EV-046..050 +
> §8.16–§8.20) plug into the full `specs/*.md` corpus without contradiction. Every check cites a real
> `specs/<file>.md:line` / anchor against HEAD `5160326b`. Verdict + tallies at the end.

---

## 1. Cross-reference checks performed

Every inter-spec link the three drafts assert, verified against the live target. ✅ = target exists and
the cited content is accurate; ⚠️ = exists but the anchor/wording is imprecise (non-blocking tidy); ❌ =
target missing or claim false.

| # | Link (source → target) | Claim under test | Verify against | Verdict |
|---|---|---|---|---|
| X1 | RS §9.1 depends-on → event-model §4.5 **EV-021** | "observational replay MUST NOT reconstruct state" read surface | `event-model.md:611` EV-021 present, text matches | ✅ |
| X2 | RS §9.1 / RS-014 → event-model **ScanAfter** | ScanAfter is the declared offline-scan read surface | `event-model.md:755` (EV-038 references `ScanAfter(watermark)`); promoted to first-class by amendment **EV-047** | ✅ (EV-047 makes it first-class; RS anchors it to "§4.5", correct) |
| X3 | RS §9.1 / RS-014 → event-model **DecodePayloadStrict** | strict-outer decode enforced by `DecodePayloadStrict` | Added by this amendment as **EV-049 (§4.7)**; RS §9.1 attributes it to "§4.5 EV-021" and RS-014 to "§8" | ⚠️ mis-anchor — should cite **§4.7 EV-049** |
| X4 | RS §9.3 → scenario-harness **SH-018 / SH-INV-001** | no-test-branch discipline; substitution by wired value not runtime branch | `scenario-harness.md:289` SH-018 (forbidden-token set), `:465` SH-INV-001 | ✅ RS-017's "MUST NOT introduce a runtime test-branch" is consistent, not contradictory |
| X5 | RS §9.3 → handler-contract **HC-035** | HC-035 disclaims governing the in-process-fake surface | `handler-contract.md:607` carve-out: "in-process fakes … are NOT twins … does NOT forbid in-process `Handler` implementations" | ✅ exactly as RS claims |
| X6 | RS §9.3 → **session-keeper (SK)** | SK is the 2nd instantiation (RS-022) | SK draft depends-on replay-substrate; SK-009/SK-022 host on the seam | ✅ mutual |
| X7 | RS-023 / §2.2 → process-spawn seam **PL-021b, HC-056, PI-012a, CI-004** | a *different* "substrate" sense, not conflated | `process-lifecycle.md:728` PL-021b "Direct-tmux substrate"; `pi-harness.md:50` PI-012a "forced-exec substrate"; `credential-isolation.md:63` CI-004 "Substrate handoff"; HC-056 = `agent_ready` timeout | ⚠️ PL-021b/PI-012a/CI-004 exact; **HC-056** is `agent_ready` timeout (substrate-*adjacent*, not the seam definition) — loose but defensible |
| X8 | RS-023 → cognition **CL-015 / CL-024** "substrate teardown" | a different "substrate" sense | `cognition-loop.md:149` CL-015 "fresh-start recycle … (substrate teardown …)", `:156` CL-024 | ✅ |
| X9 | RS-023 / §2.2 → transport **credential-isolation §2.2 / pi-harness PI-069** | a different "substrate" sense | `credential-isolation.md:44` "The LLM transport substrate" (in §2.2 Out-of-scope, heading `:40`); `pi-harness.md:185` PI-069 "production substrate = paid" | ✅ |
| X10 | RS-015 **ClockPort** | does any spec already define a clock/time seam this duplicates? | corpus grep for clockport/clock-seam/`cfg.Now`/nowfunc: **no** existing spec-level clock port (only `pi-provider-switch.md:496` "claim-time seam", unrelated) | ✅ novel — no duplication/conflict |
| X11 | SK-002 → process-lifecycle **PL-021d** | `PanePort.Inject` follows `tmux load-buffer`+`paste-buffer`; bare `send-keys` FORBIDDEN | `process-lifecycle.md:770-784` PL-021d states exactly this (bare `send-keys` without `-l` FORBIDDEN) | ✅ (see §3 for the daemon-vs-keeper scoping note) |
| X12 | SK-002 → process-lifecycle **PL-021b §5** | daemon forbidden the equivalent pane read; `Capture` stays keeper-only | `process-lifecycle.md:774` confirms "PL-021b §5 forbids the daemon from *reading* pane output via `tmux pipe-pane` or any equivalent channel" | ⚠️ accurate for *bridge* reads; the daemon's own `logs` uses `capture-pane` (`process-lifecycle.md:958`), so "any equivalent pane read" slightly overstates — see §3 |
| X13 | SK-006 → replay-substrate §6 **ClockPort** | `ClockPort` defined in RS, required by reference | RS §6.4 defines `ClockPort`(`Now`/`Since`/`NewTicker`/`Sleep`)+`Ticker`; SK does not redefine | ✅ single definition, referenced |
| X14 | SK-009/SK-020 → replay-substrate §4 **seam/Run/Twin/faults/L0–L2** | 2nd instantiation on the seam | RS-001/002 (seam+`Run`), RS-008 (`Twin`), RS-012 (4 fault modes), RS-017 (L0–L3) | ✅ |
| X15 | SK-012 / §6.4 → event-model **§8.20** registration + payloads | payload shape owned by EV §8.20 | Amendment §3.1/§3.2 adds §8.20 with the four structs | ✅ **byte-identical** — SK §6.4 and EV §3.2 structs match field-for-field (`agent_name`,`cycle_id` required no-omitempty, etc.) |
| X16 | SK-016 → operator-nfr **§4.13 ON-059** | ON-059 owns the restart-now gate ladder AND the operator-pinned warn/act bands | `operator-nfr.md:1061` ON-059 (RunOnDemand gate order `:1111-1132`); bands `:1073` warn=200k, act=215k, force-act=240k; `min(absTokens, pctCeil×windowSize)` ceiling `:1074` | ✅ bands + ceiling fn (`minAbsOrPctCeil`) confirmed present in ON-059 — see §3 for the two-distinct-ladders note |
| X17 | SK-INV-001..003 ordering → event-model §8.20 emission-ordering note | SR3/SR4 orderings consistent with EV's stated order | EV §3.1: `handoff_written → model_done → clear_sent → new_session_up`; `clear_sent` after both handoff_written+model_done | ✅ identical to SK-INV-001/002 |
| X18 | EV §5 → **session-keeper SK-R4** | SK owns *ordering*; EV owns *registration/shape* | SK §4.4/§5 owns SR3/SR4/SR6/SR7/SR9; SK §6.5 defers shape to §8.20 | ✅ mutual, clean split |
| X19 | EV §5 → **replay-substrate** (consumes EV-047/048/049) | RS harness consumes ScanAfter + typed-decode + strict | RS-014/RS-020 build on them | ⚠️ EV §5 cites stale design IDs "SB-R4/R6/R12"; final RS uses RS-014/RS-020 — informative parenthetical, harmless |
| X20 | EV amendment §8.16–§8.20 vs spec **§8.13/§8.14/§8.15** | spec §8.13/8.14/8.15 are correct and NOT renumbered | `event-model.md:354` §8.13 Epic-completion, `:368` §8.14 HITL, `:392` §8.15 Bead-ledger — exactly as the amendment states | ✅ no spec renumber; only ADD §8.16–8.20 |
| X21 | EV amendment new §8.16–§8.20 numbers | does any OTHER spec cite event-model §8.16–8.20 by number? | corpus grep: **zero** external cites of event-model §8.16/8.17/8.18/8.19/8.20 | ✅ no cross-spec collision from the additions |
| X22 | EV §3.3 §8.9(b) exception vs event-model **§8.9 criteria** | criteria (a)–(h) exist; recorded-exception pattern is precedented | `event-model.md:283` §8.9(b) boundary criterion; §8.12 (`:348`) & §8.14 (`:386`) record §8.9 evidence inline | ✅ same pattern; not "against" the criteria — satisfies (a),(c)–(h), reframes (b) |
| X23 | EV amendment ID/version bookkeeping | highest existing EV = EV-045; next-free EV-046; version 0.6.4→0.7.0 | `event-model.md` highest EV-045 (added 0.6.2 for §8.14, `:1594`); `version: 0.6.4` `:11` | ✅ EV-046..050 continue cleanly; no prior ID renumbered |
| X24 | EV-046 zero-`run_id` vs event-model **§6.1 envelope** | "`run_id` present when scoped to a run" applied to cycle-scoped events | §6.1 rule + reconciliation precedent (`reconciliation_run_id` payload identity) | ✅ consistent application (cycle-scoped ≠ run-scoped) |

**Tally: 24 checks — ✅ 20, ⚠️ 4, ❌ 0.** No check failed; the four ⚠️ are anchor/wording imprecisions, not
design defects.

---

## 2. The reference-touch edits (00b R5) — REQUIRED integration edits

Decompose R5 promised three light "reference-touch" edits so the new specs are mutually discoverable
from the *unchanged* side. The RS/SK drafts state these only from their own side (RS §9.3, SK §9.1); the
reciprocal pointer on the target spec is **not** in any draft, so each is a REQUIRED integration edit.
The EV↔SK and EV↔RS pairs are already mutual in-draft (EV §5 + SK §9.1 + RS §9.1) and need no touch.

| Edit | Target anchor | Pointer text to ADD | Status |
|---|---|---|---|
| **R5-a** | `handler-contract.md` §9 Cross-references (near the HC-035 co-ref block; HC-035 body at `:603-607`) | Add: *"[replay-substrate.md] RS-001/RS-006/RS-007 — the in-process `EventSource`/`Effector` seam and its two test doubles now govern the in-process-fake surface HC-035 carves out of the twin-parity rule. HC-035 disclaims it; RS owns it. Read-only co-reference; no reverse dependency."* | **REQUIRED** (RS §9.3 has the RS→HC side; HC side absent) |
| **R5-b** | `scenario-harness.md` §9 Cross-references (SH-018 body `:289`, SH-INV-001 `:465`; existing cross-ref block `:847-854`) | Add: *"[replay-substrate.md] RS-017 — the substrate's zero-token L0–L2 tiers select doubles by which `EventSource`/`Effector` value is wired, never a runtime branch; this is consistent with SH-018 / SH-INV-001. Read-only co-reference; no reverse dependency."* | **REQUIRED** (RS §9.3 has the RS→SH side; SH side absent) |
| **R5-c** | `operator-nfr.md` §4.13 ON-059 (`:1059-1132`), or its §9 cross-refs | Add: *"[session-keeper.md] SK-016 re-expresses the keeper decision logic these bands drive behind ports + a `Step` reactor and preserves the warn/act/force-act bands (200k/215k/240k) and the ceiling function unchanged; band changes remain HARD-NO without operator direction (ON-059)."* | **REQUIRED** (SK §9.1 cites ON-059; ON-059 side absent) |

These three edits are pure additive cross-reference pointers (no normative change) and SHOULD land in the
same wave that finalizes RS/SK so the corpus has no one-directional dangling reference.

---

## 3. Contradictions checked

No contradiction rises to "update-draft." Each candidate below is resolved **documented-acceptable**,
with one recommended wording softening.

**C1 — "substrate" naming collision (the headline risk).** RS §2/§3 uses "substrate"; the corpus already
carries three unrelated normative "substrate" senses: process-spawn (`process-lifecycle.md:728` PL-021b,
`credential-isolation.md:63` CI-004, `pi-harness.md:50` PI-012a), cognition teardown
(`cognition-loop.md:149/156` CL-015/CL-024), transport (`credential-isolation.md:44` §2.2,
`pi-harness.md:185` PI-069). **Resolution: documented-acceptable.** RS names the *spec* `replay-substrate`
and the *prefix* `RS` (not `substrate`/`SB`), and RS-023 disambiguates all three senses by name and leaves
those specs unedited. The Go *package* stays `internal/substrate`, but that is a package-vocabulary
decision explicitly separated from the spec name (RS §2.2 INFORMATIVE note). No spec text is contradicted.

**C2 — ClockPort duplicating an existing clock/time seam.** **Resolution: no conflict.** Corpus grep finds
no pre-existing spec-level clock port, time seam, `cfg.Now`, or fake-clock contract (the only near hit is
`pi-provider-switch.md:496` "claim-time seam", an unrelated credential-claim concept). RS-015's `ClockPort`
is the first normative clock seam in `specs/`. SK-006 requires it by reference and does not redefine it —
single owner, no duplication.

**C3 — EV §8.16 renumbering vs specs citing §8.13/8.14/8.15 by number.** **Resolution: no conflict.** The
amendment does **not** move spec §8.13/8.14/8.15 (confirmed Epic/HITL/Bead at `event-model.md:354/368/392`);
it only ADDS §8.16–§8.20. The "§8.13→§8.16" renumbering is entirely a *code-comment* correction
(eventtype.go etc.), not a spec-section move. A corpus grep for external cites of event-model
§8.16/8.17/8.18/8.19/8.20 returns **zero** — nothing downstream breaks.

**C4 — tmux/PanePort boundary vs the daemon's process-spawn seam.** **Resolution: documented-acceptable,
with one recommended softening.** SK-002 is careful: `PanePort` injects into an *existing* agent pane and
does not spawn it, so it does not rebuild the PL-021b process-spawn seam; SK-002 explicitly states
"`PanePort` MUST remain consistent with the PL-021b process-spawn seam without rebuilding the daemon side"
and "does not extend that [pane] read to the daemon." The keeper is a standalone per-agent process (SK
Constraint 1: no daemon), so PL-021b §5 (which binds the *daemon*) does not even reach the keeper's own
`Capture`. **One wording imprecision (X12):** SK-002 says "[PL-021b §5] forbids the daemon *any equivalent
pane read*," but the daemon's `logs` command legitimately uses `capture-pane` (`process-lifecycle.md:958`);
PL-021b §5's prohibition is specifically the `pipe-pane` *bridge-data side-channel*, not all `capture-pane`
use. Recommended (non-blocking): change SK-002 to "PL-021b §5 forbids the daemon a `pipe-pane` bridge-read
of pane output; this spec does not add a keeper-style `Capture` to the daemon."

**C5 — two distinct gate ladders (SK-011 vs ON-059).** SK-011 defines an autonomous **11-gate** ladder
(threshold-triggered); ON-059 defines a captain-initiated **RunOnDemand** ladder that "bypasses the act
threshold gate only" (`operator-nfr.md:1066`). **Resolution: no conflict.** These are related-but-distinct
ladders, and SK §1 is explicit that restart-cycle correctness "has no normative home in `specs/` today" —
SK-011 is the *first* normative home for the autonomous ladder, while ON-059 keeps the on-demand ladder and
the band values. SK-016 correctly cites ON-059 as the band owner, not as the autonomous-ladder owner. The
existing `session_keeper_restart_now_blocked` event ON-059 emits (`:1126`) is one of the 18 §8.16 types the
amendment re-homes — consistent.

**C6 — RespawnPort.ForceRestart vs ON-INV "no new abort control surface" (`operator-nfr.md:1188`).**
**Resolution: no conflict.** ON's rule forbids new surfaces that abort an in-flight *daemon work-run*
without routing through `stop --immediate`. `RespawnPort.ForceRestart` kills+respawns an *interactive agent
session* (captain/crew) after `MaxHandoffTimeouts` — orthogonal to daemon runs — and is a re-expression of
behavior the keeper already performs (SK §1: "re-expresses the *existing* logic"). No new operator control
surface is introduced.

---

## 4. Terminology consistency

The drafts are internally and corpus-consistent on every load-bearing term:

- **substrate / replay-substrate** — spec name `replay-substrate` + prefix `RS` reserved for the seam; the
  three prior senses kept distinct (RS-023, §C1). ✅ The one residual overlap (Go package `internal/substrate`
  vs the `internal/handler.Substrate` *type*) is a package-name matter, not a spec-term clash, and is called
  out explicitly. Documented-acceptable.
- **keeper / session-keeper** — spec-id `session-keeper`; event family `session_keeper_*` (matches the 18
  existing types); EV §3.1 explicitly rejects the `keeper_*` prose shorthand to avoid a name split. ✅
- **cycle / cycle_id** — uniform across RS (out-of-scope note), SK (§3, §6.4), EV (§8.20 / EV-046); JSON
  `cycle_id`, required, no `omitempty`; composite join key `(agent_name, cycle_id)`. ✅
- **model-done / `model_done` / `session_keeper_model_done`** — prose "model-done", signal `ModelDone`, event
  type `session_keeper_model_done`; used consistently in SK §4.5, §6.4, §7 and EV §8.20. ✅
- **the four event names** — `session_keeper_handoff_written` / `_model_done` / `_clear_sent` /
  `_new_session_up` — identical in SK §4.4, SK §6.4, EV §8.20, and the changelog. ✅
- **reactor / Step** — RS glossary "reactor `Step`" (vertical-owned, not part of the seam); SK "Step reactor"
  mirroring the codex reactor. Same concept, no clash. ✅
- **port** — "consumer-owned 1–3-method narrow interface" (RS-004) used identically for SK's five ports +
  RespawnPort. ✅ `ClockPort`, `EventSource`, `Effector`, `Twin`, `ReplayCodec`, `FaultConfig`/`FaultMode`
  are single-defined in RS and referenced (never redefined) by SK. ✅

No term the drafts introduce clashes with an established corpus term.

---

## 5. Changelog (`05-changelog.md`) accuracy

Verified against the drafts:

- **RS:** "23 requirements (RS-001..RS-023, deliberate legal gap at RS-013) + 4 invariants
  (RS-INV-001..004)." ✅ The draft skips RS-013 (RS-012 → RS-014) and stops at RS-023; RS-INV-001..004
  present.
- **SK:** "20 requirements (SK-001..SK-020) + 5 invariants (SK-INV-001..005)." ✅ **Confirmed fixed** — the
  changelog reads **20**, not the erroneous 25 the reviewer caught; the draft has exactly SK-001..SK-020 and
  SK-INV-001..005.
- **EV:** "Five new requirements EV-046..EV-050; version 0.6.4 → 0.7.0." ✅ Matches the amendment header and
  the live `version: 0.6.4`; highest prior ID EV-045 confirmed; §8.16–§8.19 drift + §8.20 cohort as
  described.
- **Landing actions:** reserve RS + SK in `_registry.yaml` (both verified free — grep shows neither present);
  EV needs no reservation (already reserved, `_registry.yaml:18`). ✅
- **Cross-reference graph** (changelog §"Cross-reference graph"): RS depends-on event-model; SK depends-on
  replay-substrate/event-model/process-lifecycle/operator-nfr; EV §8.20 co-owned (EV shape, SK when). ✅
  matches all three front-matters.

**One minor changelog/cross-ref tidy (non-blocking):** the EV amendment §5 and RS §9.1 still carry a couple
of stale/imprecise anchors — EV §5 names design IDs "SB-R4/R6/R12" (final: RS-014/RS-020), and RS §9.1/RS-014
attribute `DecodePayloadStrict` to "§4.5 EV-021"/"§8" when it is **EV-049 §4.7**. Recommend fixing these
anchors at finalize; neither changes normative content (X3, X19).

---

## 6. Final coherence assessment

**The three drafts plug into the corpus cleanly.** The seam/ClockPort/taxonomy of RS, the five-port `Step`
reactor of SK, and the §8.20 four-event cohort + §8.16–19 drift-reconciliation of the EV amendment form a
coherent, non-contradictory whole:

- **No real contradiction.** Every candidate collision (substrate naming, ClockPort, §8.16 renumber,
  PanePort-vs-daemon, gate ladders, RespawnPort-vs-ON-INV) resolves documented-acceptable. The substrate
  disambiguation (RS-023) is airtight; ClockPort is novel; the §8 additions collide with nothing downstream.
- **No orphaned/dangling references** *within* the drafts — RS↔SK, RS↔event-model, SK↔event-model, EV↔SK,
  EV↔RS are all mutual as drafted. The only one-directional pointers are the three intentional R5
  reference-touches (§2), which are REQUIRED integration edits, not dangling-reference defects.
- **The known lint failures are landing-order artifacts, not design defects:**
  - *SK `depends-on: replay-substrate` fails "depends-on spec exists" until RS lands* — expected; SK
    finalizes last of the three (changelog landing order item 4). Landing-order.
  - *RS + SK registry reservations pending* (`_registry.yaml` grep: neither `RS`/`SK` nor
    `replay-substrate`/`session-keeper` present) — each reservation lands in the same commit as its spec
    (RS front-matter note, SK `:22` INFORMATIVE). Landing-order.
  - *Landing-order coupling to note:* RS-014/RS-020 consume `ScanAfter` (EV-047) and `DecodePayloadStrict`
    (EV-049), which exist only after the EV amendment lands. The changelog says "EV amendment + RS may land
    in parallel" — fine, but the EV amendment MUST land **no later than** RS, since RS references surfaces
    the amendment introduces. Sequence: EV amendment → RS → SK (SK references both).

**Recommended (all non-blocking, apply at finalize):** (1) the three R5 reference-touch edits in §2; (2) fix
the four ⚠️ anchor imprecisions (X3 RS DecodePayloadStrict→§4.7 EV-049; X7 soften HC-056 as substrate-*adjacent*;
X12/C4 soften SK-002 "any equivalent pane read"→"`pipe-pane` bridge-read"; X19 EV §5 SB-*→RS-* IDs).

**Coherence verdict: PASS** — the three drafts are corpus-coherent and ready to advance, subject to the three
REQUIRED reference-touch edits landing in the finalize wave and the documented EV→RS→SK landing order.

## Post-integration draft fixes APPLIED (the four ⚠️)

All four anchor imprecisions have been corrected in the drafts (not deferred):
- **X3** — `replay-substrate.md`: `DecodePayloadStrict` now anchored to `[event-model.md §4.7 EV-049]`;
  the §9 cross-ref cites EV-021 (observational classification) + EV-047 (ScanAfter surface) + EV-049
  (strict variant) with the correct sections.
- **X7** — `replay-substrate.md` §2/RS-023: `HC-056` removed from the process-spawn-substrate anchor
  list (it is the `agent_ready` timeout, not the seam); list is now `PL-021b / PI-012a / CI-004`.
- **X12** — `session-keeper.md` SK-002 (§4, §6.1, §9): "forbids the daemon any equivalent pane read"
  softened to the accurate "PL-021b §5 forbids the `pipe-pane` bridge side-channel specifically (the
  daemon's own `logs` uses `capture-pane`)"; `PanePort.Capture` stated keeper-scoped / not extended
  into the daemon path.
- **X19** — `event-model-amendment.md` §5: stale design IDs `SB-R4/R6/R12` replaced with final
  `RS-014/RS-020` and the ReplayCodec contract reference.

The three REQUIRED reciprocal reference-touch edits (R5-a HC-035→RS, R5-b SH→RS, R5-c ON-059→SK) remain
landing-wave actions on the UNCHANGED target specs — carried into the Tasks pass, not draft edits.
