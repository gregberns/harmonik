# keeper-redesign — Affected Spec Areas (Decompose)

> Maps each goal in `01-problem-space.md` to the spec area that satisfies it. The primary
> normative artifact is the NEW spec `specs/keeper-identity-and-liveness.md` (drafted as
> `05-spec-drafts/keeper-identity-and-liveness.md`).

## New Specs

### specs/keeper-identity-and-liveness.md (NEW — primary normative artifact)

- **Scope:** The four invariants governing keeper session identity, gauge liveness, and the
  operator gate.
- **Requirements:** States SINGLE-WRITER (one `.managed`/`.ctx` writer; `WriteManagedSessionFn`
  once at boot, no auto-clear/flap loop), AUTHORITATIVE-IDENTITY (bind `--session-id` UUIDv4,
  never scrape gauge/transcript), the EXPLICIT DELETION TARGET (watcher.go:664-888 + named
  knobs + rebind CLI deleted, net LOC down) with a concrete deletion checklist, DEFAULTS-PIN
  (Act 300k/0.85, Warn 270k/0.70, Force +40k/0.95 unchanged), and the validation tiers (unit +
  `//go:build integration` twin-pane + manual LIVE-SOAK).
- **Dependencies:** none (root spec).

## Affected Existing Specs

### keeper thresholds / gauge contract

- **Change summary:** RESTATE the defaults-PIN; no value change.
- **Requirements:** The 27% warn on a 1M window is correct-by-design; band-widening is a
  blocking failure; constants are consolidatable (hk-bpkv) but value-identical.
- **Dependencies:** identity spec §4.

### keeper CLI surface

- **Change summary:** Flag-parity (`--agent <name>` uniformly); REMOVE the `rebind` subcommand.
- **Requirements:** enable/doctor/set-dispatching/clear-dispatching accept `--agent <name>`
  uniformly and reject a leading-dash positional; `rebind` no longer exists.
- **Dependencies:** identity spec §2 (rebind removal).

### keeper hooks

- **Change summary:** Unify env var onto `HARMONIK_AGENT`.
- **Requirements:** hooks resolve agent from `HARMONIK_AGENT`; `HARMONIK_KEEPER_AGENT` retired
  so crew gauge + idle/precompact markers correlate.
- **Dependencies:** identity spec §2.6.

### launch path (`scripts/captain-tools/captain-launch.sh`, `crewstart.go`)

- **Change summary:** Thread the launch UUIDv4 SID into the keeper launch.
- **Requirements:** `SID="$(uuidgen)"` is the source-of-truth mint, threaded into
  `keeper --session-id $SID`; crews via `resolveSessionID`.
- **Dependencies:** identity spec §2.5.

## Dependency Map

`keeper-identity-and-liveness.md` is the root; all other areas restate or consume it. Bead
land-order (see `07-tasks.md`): hk-7rmv (authoritative identity + deletion) FIRST → hk-81wk /
hk-0t5s → hk-75mr → NEW-forcerestart; hk-nlio flips RED→GREEN last; prereqs hk-baf4 / hk-psds /
hk-p9kw land in parallel up front; hk-bpkv is opportunistic.

## Goal → Area Traceability

- **G1 authoritative-identity** → identity spec §2; launch path; CLI (rebind removal).
- **G2 single-writer** → identity spec §1.
- **G3 net-LOC-down** → identity spec §3.2 deletion checklist.
- **G4 live gauge** → identity spec §3.1 (hk-81wk).
- **G5 activity-recency operator gate** → identity spec §6 (hk-0t5s).
- **G6 gauge-independent recovery** → identity spec §3.1 I3.2 (hk-75mr, NEW-forcerestart).
- **G7 defaults pinned** → identity spec §4; threshold/gauge contract.

Every goal maps to at least one area; no area is listed without a backing goal. Research and
change-design passes fold into the single normative spec draft because the architecture is
already established SOUND by the 33-agent deep-dive — the design decision (replace inference
with authoritative identity + delete the heuristic block) is fixed, not open.
