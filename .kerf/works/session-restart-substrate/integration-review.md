# Integration-pass review (pass 6) — session-restart-substrate

> Independent review of `06-integration.md` against the three Pass-5 drafts
> (`replay-substrate.md`/RS, `session-keeper.md`/SK, `event-model-amendment.md`/EV) and
> `05-changelog.md`, with spot-checks against the live corpus at `specs/*.md` (HEAD `5160326b`).

## Verdict: **Approved**

The integration deliverable is complete, its structure matches the pass criteria, its
"no real contradiction" conclusion is justified against the live specs, and all four
claimed ⚠️ anchor fixes are actually present in the drafts.

---

## Per-criterion assessment

### 1. Required contents present — PASS
`06-integration.md` contains all four required parts:
- **Cross-reference checks** — §1, a 24-row table (X1–X24) with per-link verdicts.
- **Contradictions found + resolution** — §3, C1–C6, each with an explicit resolution.
- **Consistency/terminology issues + resolution** — §4 (terminology) + the changelog-accuracy §5.
- **Final coherence assessment** — §6, with an explicit "Coherence verdict: PASS".

### 2. Broad sweep beyond the 3 modified specs — PASS
The checks reach well outside RS/SK/EV. Verified anchors exist in the live corpus:
- `process-lifecycle.md` — PL-021b (`:728` "Direct-tmux substrate"), PL-021d (`:770`). ✅
- `credential-isolation.md` — CI-004 (`:63` "Substrate handoff"). ✅
- `pi-harness.md` — PI-012a (`:50` "forced-exec substrate"), PI-069 (transport). ✅
- `cognition-loop.md` — CL-015 (`:149`), CL-024 (`:156`) "substrate teardown". ✅
- `handler-contract.md` — HC-035 (`:603`). ✅
- `operator-nfr.md` — ON-059 (`:1061`). ✅
- Plus `scenario-harness.md` (SH-018/SH-INV-001) and `pi-provider-switch.md` (clock-seam grep).
Eight-plus non-modified specs are exercised — this is a genuine corpus-wide sweep, not a
diff-of-the-modified.

### 3. Contradictions resolved / conclusion justified — PASS
All six candidates resolve documented-acceptable or no-conflict; none is left open. Spot-checked two:
- **C1 substrate-naming disambiguation is real.** The three prior normative "substrate"
  senses cited by RS-023 all exist verbatim: PL-021b "Direct-tmux substrate", CI-004
  "Substrate handoff", PI-012a "forced-exec substrate", CL-015/CL-024 "substrate teardown",
  credential-isolation §2.2 / PI-069 transport. RS dodges the collision by naming the spec
  `replay-substrate` + prefix `RS` and editing none of those specs. Justified.
- **C3 §8.16 renumber collides with zero external cites.** `grep -rn "8\.16|8\.17|8\.18|8\.19|8\.20"`
  across `specs/` excluding `event-model.md` returns **NONE**. A `§8.13` grep hit only
  `reconciliation/spec.md`, which cites *its own* §8.13 ("Failure-commit deferral"), not
  event-model's — so no external dependency breaks. event-model §8.13/8.14/8.15 confirmed
  Epic-completion / HITL-decisions / Bead-ledger, unmoved. Justified.

### 4. Cross-references valid both directions — PASS (with a correctly-flagged pending action)
Within the drafts, the pairs are mutual: RS §9.1/§9.3 ↔ EV §5 ↔ SK §9.1 all point at each
other. The doc §2 honestly identifies that the three R5 *reciprocal* pointers on the
**unchanged** target specs (HC-035→RS, SH→RS, ON-059→SK) are one-directional today and are
**REQUIRED integration edits** carried into the Tasks/finalize wave — not draft edits. This is
the correct handling, not a defect; nothing dangles inside the drafts.

### 5. Terminology consistent — PASS
§4 covers every load-bearing term: substrate/replay-substrate, keeper/session-keeper,
cycle/cycle_id, model-done, the four event names, reactor/Step, port. The residual
`internal/substrate` package vs `internal/handler.Substrate` type overlap is called out as a
package-vocabulary matter, not a spec-term clash. Consistent.

### 6. Changelog matches drafts — PASS
- **SK = 20 confirmed fixed.** Changelog reads "20 requirements (SK-001..SK-020)"; the draft
  ends at SK-020 (SK-INV-001..005). The earlier erroneous 25 is gone.
- **EV-046..EV-050 match** across changelog (§3), amendment (§4 EV-046/047/048/049/050), and
  integration doc (X23). Highest prior ID `EV-045` confirmed by grep; `version: 0.6.4`
  confirmed at `event-model.md:11`.
- **§8.16–§8.20 match** across changelog, amendment §2.2 table, and integration X20/X21.
- Registry state matches: `RS`/`SK`/`replay-substrate`/`session-keeper` absent from
  `_registry.yaml` (reservation pending, as stated); `EV` present at `:18`.

---

## Four claimed ⚠️ anchor fixes — all VERIFIED APPLIED

- **X3** — `replay-substrate.md` anchors `DecodePayloadStrict` to `§4.7 EV-049`. Present at
  RS-014 body (`…[event-model.md §4.7 EV-049] DecodePayloadStrict variant enforces…`) and in
  §9.1, which now cites EV-021 §4.5 + EV-047 §4.6 + EV-049 §4.7. ✅
- **X7** — `replay-substrate.md` no longer lists `HC-056` in the process-spawn-substrate anchor
  list. RS-023 reads "(PL-021b Substrate seam, PI-012a, CI-004)" and §2.2 lists
  "PL-021b … / PI-012a / CI-004" — no HC-056 anywhere in the draft. ✅
- **X12** — `session-keeper.md` SK-002 no longer says "forbids the daemon any equivalent pane
  read". It now reads "PL-021b §5 forbids the daemon the `pipe-pane` bridge side-channel
  specifically (the daemon's own `logs` uses `capture-pane`, `process-lifecycle.md:958`)".
  The `pipe-pane` wording is correct against `process-lifecycle.md:774`. ✅
- **X19** — `event-model-amendment.md` §5 uses `RS-014`/`RS-020` (and "the ReplayCodec
  contract"), with no remaining `SB-R4/R6/R12`. ✅

---

## Notes (non-blocking; no action required for approval)
- The doc correctly defers the three R5 reciprocal reference-touch edits to the finalize/Tasks
  wave; these must land in the same wave that finalizes RS/SK so the corpus has no
  one-directional dangling pointer. Already tracked in §2 and §6.
- The pre-existing EV `_registry.yaml` `status: draft` vs spec-front-matter `reviewed` mismatch
  is a pre-existing issue flagged in the changelog and out of this work's scope. No action.
