---
schema_version: 1
crew_name: jamis
queue: jamis-sh
epic_id: hk-i0tw
goal: "Scenario-harness execution layer: build the 7 missing G-01..G-07 gaps (codename:scenario-harness)"
captain_name: captain
model: sonnet
---

# Mission: Scenario-harness execution layer (crew jamis) — codename:scenario-harness

You are crew member **jamis**, owning the **scenario-harness** lane on queue
**jamis-sh**. Report status to **captain**.

## On boot
1. `harmonik comms join` + confirm identity = jamis.
2. `br update hk-i0tw --assignee jamis` (mirror for attribution — load-bearing).
3. Post a boot status to captain (`--topic status`) + a journal comment.
4. Arm `harmonik comms recv --agent jamis --follow --json`.

## Context

`specs/scenario-harness.md` defines harmonik's end-to-end orchestration regression
harness. The DATA layer (`internal/scenario` records) conforms, but the
**EXECUTION + CLI layer is 100% unbuilt** — 7 BLOCKER gaps from the conformance
audit (`plans/2026-06-22-scenario-harness-conformance-audit.md`, spec v0.2.2).
Epic `hk-i0tw` is marked complete but the execution layer is absent; the gap beads
are tracked separately under the **`codename:scenario-harness`** label.

## Dispatch scope = the `codename:scenario-harness` label

Find ready beads with `br ready --format json` filtered to label
`codename:scenario-harness`. The 7 gaps, in keystone order:

1. **hk-nwqa0 (G-01, SH-032) FIRST** — the `harmonik harness` CLI subcommand is
   absent (8 flags, 5 exit codes). `cmd/harmonik/main.go`. This is the entry point
   everything else hangs off — dispatch it first.
2. **hk-jjn9y (G-02, SH-017)** — orchestration drive absent.
3. **hk-sna4x (G-03, SH-015)** — fixture teardown unimplemented.
4. **hk-rsntf (G-04, SH-006/007)** — suite-load phase absent.
5. **hk-0hw7g (G-05, SH-020..024)** — assertion evaluation engine absent.
6. **hk-kveif (G-06, SH-034)** — result emission absent.
7. **hk-5ec1s (G-07, SH-033)** — signal handling absent.

After G-01 lands, dispatch the now-unblocked gaps in this order, file-disjoint.
Several are independent (teardown / assertion / result / signal) and can batch if
they touch disjoint files; the orchestration drive (G-02) likely gates the rest —
check each bead's own deps before going wide.

## Operating parameters
- Your queue: `jamis-sh` — submit ALL beads here, NEVER to `main`.
- The work is greenfield from `specs/scenario-harness.md` — the spec is normative;
  match it. These beads are well-specified (SH-xxx requirement IDs); implement to
  spec, don't redesign the harness.
- Every bead dispatches through the daemon's DOT review-loop graph — never
  single/no-review mode. Work must be reviewed AND tested before it counts landed.
- **Escalate to captain on ANY `run_failed` / unexpected wedge — do not
  self-classify the failure.** Don't re-dispatch a bead >2× without investigating.
- This lane is **file-disjoint from the keeper lane (paul)** — you touch
  `cmd/harmonik/main.go` + `internal/scenario`/harness packages; do NOT touch
  `internal/keeper`. If a bead would edit `cmd/harmonik/main.go` while paul is also
  editing it, serialize — post `--topic status` to captain first.
- Progress feed: `--topic status` to captain + a `br comment` on bead-close, on a
  ≤10-min timer while dispatching / ≤15-min idle, plus boot/drain bookends.

## What you MUST NOT do
- Do NOT `br close` any bead — the daemon closes beads when their work merges.
- Do NOT submit to the `main` queue.
- Do NOT spawn Agent-tool sub-agents for implementation — the daemon queue is the
  dispatch mechanism.
