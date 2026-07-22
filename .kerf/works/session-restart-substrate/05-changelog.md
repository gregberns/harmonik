# 05 — Spec-Draft Changelog

> **Pass 5 (Spec Draft).** The normative spec text produced by this work, ready for Integration
> (pass 6). Two NEW specs + one AMENDMENT to an existing spec. All drafts follow the house template
> (`docs/foundation/spec-template.md` v1.1), RFC2119-correct, requirement-IDs assigned. Autonomous
> mode (signoffs waived, 2026-07-13).

## Drafts produced (all under `05-spec-drafts/`)

### 1. NEW — `replay-substrate.md` (prefix **RS**) — the generic record→replay seam
- 23 requirements (RS-001..RS-023, with a deliberate legal gap at RS-013) + 4 invariants
  (RS-INV-001..004). Requirements-first shape.
- Owns: the typed generic seam (`EventSource[E]`/`Effector[A]`/free-function `Run[E,A]`), the
  fused stateful `ReplayCodec[E]` + `Twin[E]` + `FaultConfig` (four vertical-neutral fault modes),
  the two test doubles, `ClockPort`+`Ticker`, the two-layer decode discipline (SB-R12 / 00b R4),
  the 1 MB scanner default, and the L0–L3 taxonomy + minimum-artifact-list + Makefile/env-gate
  policy. Codex is the reference instantiation (MUST stay green); session-keeper is the second.
- Spec-file name is `replay-substrate.md` (NOT `substrate.md`) per D5 — dodges the 3-way
  "substrate" collision; the Go package stays `internal/substrate`.
- **Landing action:** reserve prefix `RS` + spec-id `replay-substrate` in `specs/_registry.yaml`
  (both verified free).

### 2. NEW — `session-keeper.md` (prefix **SK**) — the session-restart vertical
- 20 requirements (SK-001..SK-020) + 5 invariants (SK-INV-001..005, the load-bearing
  SR3/SR4/SR6/SR7/SR9 ordering/liveness properties). Requirements-first shape.
- Owns: the 5 consumer-owned ports (Pane/Gauge/Handoff/Emitter/Clock) + RespawnPort, the pure
  `Step(state,event)→(state,[]action)` reactor with timers-as-events, the 11-gate ladder preserved
  (unconditional prelude side effects), the 4 durable interior events' emission + ordering, the
  model-done detection protocol (D12), behavior-parity (reproduce-the-freeze via `InCycle`
  suppression), and the baseline anchor (427/507 = 84% restart-completion; 347 clear_unconfirmed).
- References `event-model.md §8.20` for the event *registration* shape (SK owns only the *when*/
  ordering); references RS for the seam+ClockPort+taxonomy; PL-021d/PL-021b and ON-059 for
  consistency.
- **Landing actions:** reserve prefix `SK` + spec-id `session-keeper` in `specs/_registry.yaml`
  (both verified free); `depends-on: replay-substrate` will fail the "depends-on spec exists" lint
  until RS lands — expected (SK finalizes last of the three per the dependency map).

### 3. AMENDMENT — `event-model.md` (prefix EV) — `event-model-amendment.md`
- Version bump 0.6.4 → 0.7.0. Five new requirements **EV-046..EV-050** (next-free was EV-046;
  highest existing was EV-045):
  - **EV-046** — cycle-scoped keeper events join on a REQUIRED payload `cycle_id`, never a zero
    envelope `run_id`; composite `(agent_name, cycle_id)` join key (EV-U2 / D7).
  - **EV-047** — `ScanAfter` declared the normative offline-replay read surface + the cross-writer
    EventID-sort caveat (EV-U4 / D9).
  - **EV-048** — the typed-decode registry adopted as the decode/assert layer for the sanctioned
    `internal/replay` observational consumer (EV-U3 / D6).
  - **EV-049** — the additive `DecodePayloadStrict` variant surfacing additive writer drift
    (EV-U3 / D6).
  - **EV-050** — the `session_keeper_*` cohorts (§8.16 + §8.20) carved out of `allEventTypeCohort` /
    EV-027 count guards; the gjyks doc-comment contract amended (EV-U1 / D8).
- **§8 numbering reconciliation (EV-U5):** adds real spec sections §8.16 (the existing 18
  `session_keeper_*` types, renumbered out of the §8.13 collision), §8.17/§8.18/§8.19 (the
  code-invented alarm/remote/flywheel-stall cohorts), closing the code↔spec drift; lists the exact
  code-comment citation fixes (keeperevents.go, eventtype.go, eventreg_hqwn59.go, pertypecompat).
- **§8.20 — new interior-event cohort:** the four `session_keeper_*` interior events with canonical
  payload structs (00b R1+R2, verbatim), O-class durability + emit-failure hardening (D9), and the
  argued §8.9(b) cycle-interior exception (the `internal/replay` harness is the §8.9(a)
  cross-subsystem consumer; NOT EV-026-internal because they cross the process boundary).
- No new EV-NNN for the §8 reconciliation or the bare registration — consistent with taxonomy-first
  precedent (cohort additions add §8 rows without new requirement IDs).
- **Landing action:** no registry reservation needed (EV already reserved). Noted a pre-existing
  registry/spec status mismatch (registry `EV: status: draft` vs spec front-matter `reviewed`) —
  flagged, not touched by this work.

## Consolidated landing checklist (for Integration + Implementation)
1. Reserve `RS` (replay-substrate) and `SK` (session-keeper) in `specs/_registry.yaml`; land each
   reservation in the same commit as its spec.
2. Apply the event-model amendment to `specs/event-model.md` (the EV-U5 §8 reconciliation lands
   BEFORE the §8.20 additive cohort).
3. Copy the two new drafts to `specs/replay-substrate.md` and `specs/session-keeper.md` on
   `kerf finalize`.
4. Order: EV amendment + RS may land in parallel; SK finalizes last (it references both).

## Cross-reference graph (as drafted)
- RS → depends-on: event-model (ScanAfter/DecodePayloadStrict). Co-references: scenario-harness
  (SH-018/SH-INV-001), handler-contract (HC-035), session-keeper (2nd instantiation).
- SK → depends-on: replay-substrate, event-model, process-lifecycle, operator-nfr.
- event-model (EV) → referenced by both RS and SK; the §8.20 cohort is co-owned (EV owns shape, SK
  owns emission/ordering).
