# SESSION — codex-first

Advanced problem-space → ready in one session (2026-07-21), back-filling the formal spec-jig
artifacts from the design-complete plan `plans/2026-07-21-codex-first/_plan.md` and DECISIONS
`plans/2026-07-20-codex-strategy-realignment/DECISIONS.md` (D1/D3/D4 — not reopened).

## What this work is
Make local Codex a reliable bead-runner: drop the fail-closed isolation fence (D1), turn codex's
native sandbox off → danger-full-access (D3), confirm the daemon-fallback committer (D2-direction),
and make Codex crews staffable. No new architecture; a small set of code seams named to file:line.

## Passes completed
- 01 problem-space, 02 decompose, 03 research (pointing to research/05+06 + a fresh spec survey),
  04 change-design, 05 spec-draft (HN-025/HN-026 amendment to harness-contract.md), 06 integration,
  07 tasks (T1–T7 + two test tasks), status = ready.

## Open human/operator calls (not invented)
- Verification is operator-reframed (§7.3): heavy in-crew subagent testing + assessor gate, NOT
  bead-shaped. Test beads deliberately NOT created (daemon down; "no beads while daemon off").
- Execution needs the daemon up + a Codex crew staffed by the Captain.

## Next
`kerf finalize codex-first --branch <name>` when ready to package for implementation.
