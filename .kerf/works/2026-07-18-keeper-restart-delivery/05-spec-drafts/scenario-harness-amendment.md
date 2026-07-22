# Amendment to specs/scenario-harness.md (v0.2.3 → v0.2.4)

## Frontmatter

- `version: 0.2.3` → `version: 0.2.4`
- `last-updated: 2026-06-23` → `last-updated: 2026-07-18`

## New requirements

The highest occupied requirement ID is SH-034 (§4.13) and the highest invariant is
SH-INV-005 (§5). The two new requirements introduced by this kerf
(`2026-07-18-keeper-restart-delivery`, K6 testing) are appended as SH-035 and SH-036 in a
new §4.14, after the highest occupied ID. This amendment is deliberately THIN: it records the
two-twins correction and carves the keeper's pane/timing/handoff/operator-typing coverage
OUT of the SH YAML contract, adding at most one wire-observable comms scenario. No prior IDs
are renumbered or retired; the §10.1 three-scenario conformance floor is UNTOUCHED.

**The two-twins correction.** `cmd/harmonik-twin-claude` — the scenario-harness twin SH
governs — has no tmux pane and speaks NDJSON wire events only. Four of the five
keeper-restart-delivery failures (operator-typing collision, late-handoff after 300s,
operator-present misread, FORCE-ACT-still-cuts) are pane/timing/handoff/operator-typing
failures invisible to it. Their test vehicle is `cmd/harmonik-twin-session`, which runs in a
real tmux pane with the real keeper injector — the keeper's own session-twin integration
tier, OUTSIDE this spec's YAML contract.

---

### Add new §4.14 — Keeper session-twin integration tier is outside the SH YAML contract (K6). Add after §4.13:

#### SH-035 — Keeper pane/timing/handoff/operator-typing coverage rides the session-twin integration tier, outside the SH contract

The scenario harness governed by this spec (the `harmonik-twin-claude` wire twin) MUST NOT
be treated as the carrier for the keeper-restart-delivery pane/timing/handoff/operator-typing
tests. Those failures — operator-typing collision, late-handoff after the 300s watch,
operator-present misread, and the FORCE-ACT-still-cuts backstop — are delivered by the
keeper's own **session-twin integration tier** (`cmd/harmonik-twin-session` + a real tmux
pane + the real keeper injector + a real `HANDOFF-<agent>.md`), which is OUTSIDE the SH YAML
contract. This mirrors the spec's existing real-tmux carve-out (SH-INV-003 / the twin-parity
boundary): real-tmux behavior is not the SH harness's job. The SH harness MUST NOT be
extended with a tmux-pane or operator-typing surface to cover these; they remain
integration-tier tests owned by the keeper subsystem ([session-keeper.md §10.2]).

Tags: mechanism

#### SH-036 — At most one wire-observable comms-fallback scenario MAY be added; the §10.1 floor is untouched

The harness MAY add AT MOST ONE wire-observable scenario for the keeper's comms-unreachable
fallback (SC-7 failure (c)), and ONLY IF that fallback emits an assertable bus event the wire
twin can observe (candidate `scenarios/regression/*.yaml`): seed the target absent → assert
delivery resolves to terminal-fallback and NEVER a silent no-op ([session-keeper.md §5
SK-INV-006]); a positive control seeds the target present → comms. If K1 emits no
wire-observable event, no SH scenario is added. This scenario MUST NOT be promoted into the
§10.1 three-scenario conformance floor for v0.1; the floor is unchanged and any addition to
it remains a foundation amendment per [architecture.md §4.6].

Tags: mechanism

## Amendment to §9.3 (co-references — additive, read-only)

Add to the "Co-references (read-only consumption)" list:

- **[session-keeper.md §4.11–§4.15, §10.2]** — the keeper-restart-delivery behaviors (K1–K5)
  whose pane/timing/handoff/operator-typing coverage rides the keeper session-twin
  integration tier per SH-035, with at most one wire-observable comms-fallback scenario per
  SH-036. Read-only co-reference; no reverse dependency.

## Revision-history entry

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-07-18 | 0.2.4 | foundation-author | Keeper-restart-delivery test-vehicle correction (codename: 2026-07-18-keeper-restart-delivery, K6). New §4.14: SH-035 records normatively that the keeper's pane/timing/handoff/operator-typing coverage (operator-typing collision, late-handoff after 300s, operator-present misread, FORCE-ACT-still-cuts) is delivered by the keeper's session-twin integration tier (`harmonik-twin-session` + real tmux + real injector), OUTSIDE the SH YAML contract — mirroring the spec's real-tmux carve-out; the SH harness (the `harmonik-twin-claude` wire twin) MUST NOT be extended to cover them. SH-036 permits AT MOST ONE wire-observable comms-unreachable-fallback scenario, only if K1 emits an assertable bus event, and explicitly keeps it OUT of the §10.1 three-scenario conformance floor (floor untouched). §9.3 gains a read-only co-reference to session-keeper §4.11–§4.15/§10.2. No SH IDs renumbered or retired; SH-001…SH-034 and SH-INV-001…SH-INV-005 unchanged; the §10.1 floor is unchanged. Status remains `reviewed`. |
