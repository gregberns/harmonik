# Cluster design — testing (R-C)

Codename: `2026-07-18-keeper-restart-delivery` · pass 4 (change-design)

Cluster-level navigation + summary doc for research cluster **R-C**. Design decisions are
summarized faithfully here; normative text and full rationale live in the spec-named files
pointed to at the bottom.

## Scope & components covered

R-C covers **K6 — test coverage** for the whole work: which test vehicle carries which
failure, and the thin change that lands in scenario-harness.md.

## Key design decisions

**The load-bearing correction: two twins, and the keeper's failures live in the OTHER one.**
`cmd/harmonik-twin-claude` (the parity-audit / scenario-harness twin) has no tmux pane — it
speaks NDJSON wire events only. Four of the five target failures (operator-typing collision,
late-handoff after 300s, operator-present misread, FORCE-ACT-still-cuts) are
pane/timing/handoff/operator-typing failures, invisible to it. The right vehicle is
`cmd/harmonik-twin-session`, which the keeper's own integration tests build and run in a real
tmux pane with the real `keeper.InjectText` and a real `HANDOFF-<agent>.md`. So "the twin is
the scenario-test vehicle" is only correct for wire-observable behavior; the rest ride the
keeper's session-twin integration tier.

**Extend the mature keeper integration tier; do NOT invent a harness.** The keeper already has
(a) a real-tmux integration tier, (b) an offline causal reactive harness plus a
`substrate.FakeClock`. K6 extends these rather than building anything new.

**SC-7 failure → test mapping (each fails before, passes after):**

- (a) *operator-typing collision* — integration (real tmux) via the pty-attach harness: put
  partial input on the pane (no Enter), trigger warn, assert the operator line is not
  submitted; unit adjunct swaps `tmuxRunFn` to assert **zero** pane write when operator-present
  and comms is taken.
- (b) *late-handoff after 300s* — harness unit drives to AwaitingHandoff with `writeNonce=false`
  via `FakeClock`, `Advance(300s+)`, asserts `cycle_aborted{reason=handoff_timeout}`; then an
  integration smoke for the T+301 self-restart completing a clean clear (SC-4).
- (c) *comms-unreachable fallback* — integration (comms/daemon) with in-process daemon+UDS and
  the presence registry: seed target absent → assert delivery resolves to terminal-fallback,
  never a silent no-op (SC-2); positive control seeds present → comms.
- (d) *operator-present misread* — unit primary over the pure `operatorActiveSince` table: feed
  a stale-but-present client_activity, assert the current 5-min window says absent (fail-before)
  and the augmented signal says present (pass-after).
- (e) *FORCE-ACT still cuts a never-idle session* — extend the existing forced-clear and
  hard-ceiling backstop tests; assert the K2 deferral does NOT weaken the backstop.

**What changes in scenario-harness.md is thin.** (i) Add at most ONE wire-observable scenario,
and ONLY IF the comms-unreachable fallback (c) emits an assertable bus event the wire twin can
see. (ii) Record normatively that pane/timing/handoff/operator-typing coverage (a,b,d,e) is
delivered by the keeper's session-twin integration tier, OUTSIDE the SH YAML contract —
mirroring the spec's own real-tmux carve-out. (iii) Any addition to the §10.1 three-scenario
conformance floor is a foundation amendment and is avoided for v1. Net: scenario-harness.md
gets a thin K6 pointer + optionally one wire scenario; it is not the primary carrier of this
work's tests.

## Requirement IDs this cluster produces

- scenario-harness.md: **SH-035** (pane/timing/handoff/operator-typing coverage rides the
  session-twin integration tier, outside the SH contract) and **SH-036** (at most one
  wire-observable comms-fallback scenario; §10.1 floor untouched), in a new §4.14.

## Normative text and full rationale

- Design detail: `04-design/scenario-harness-design.md`.
- Normative draft: `05-spec-drafts/scenario-harness-amendment.md` (SH-035, SH-036).
- Cluster spec-draft index: `05-spec-drafts/testing.md`.
- Grounding research: `03-research/testing/findings.md`.
