---
schema_version: 1
crew_name: india
queue: india-q
epic_id: hk-tckw3
goal: "Codex-first: make LOCAL codex a reliable bead-runner through the existing DOT flow (native sandbox OFF, no ssh)"
captain_name: captain
---

# Mission: Codex-first (P0) — local Codex as a reliable bead-runner

You are crew member **india**, owning epic **hk-tckw3** on queue **india-q**. Report status to **captain**.

**This is Priority-0** (operator, `plans/2026-07-21-platform-architecture/DECISIONS.md` §PRIORITY-0). Getting local Codex operational as a bead-runner conserves Claude tokens, which the whole platform effort needs. You are the first thing to get working.

## The spec you execute
`plans/2026-07-21-codex-first/_plan.md` is your implementation spec — READ IT FIRST, in full. It is design-complete and names every seam to `file:line`. Grounded in `plans/2026-07-20-codex-strategy-realignment/DECISIONS.md` (D1, D3, D4). Take it through kerf (work codename `codex-first`, `kerf resume codex-first`) and implement.

## What "done" means (the shape, not a checklist)
Local codex runs a bead end-to-end through the UNMODIFIED DOT flow — implement → commit → review → close — with:
- native sandbox OFF → `danger-full-access`, uniform with how Claude already runs (D3);
- NO ssh, NO remote worker requirement (D4 scrapped ssh-per-node); drop the D1 fail-closed fence;
- the **daemon-fallback commit** (`ensureCodexRefsTrailer`) as the reliable committer — codex self-commit is a bonus, not a requirement;
- **reviewer stays claude** for now (codex implements, claude reviews the diff). Do NOT de-hard-code the reviewer — that is a separate fast-follow, explicitly OUT of scope here.

## The beads (already created under hk-tckw3)
- **hk-tckw3.1** Step 1 — flip the fence + posture (3a drop isolation fence + 3b exec-path `danger-full-access`), minimal diff + `TestBuildCodexLaunchSpec` argv assertions.
- **hk-tckw3.2** Step 2 — **GO/NO-GO gate**: live-prove ONE real bead end-to-end on local codex. If it does NOT clear, STOP and fan out on the failure before any hardening.
- **hk-tckw3.3** Step 4 — thread `HARMONIK_SUBSTRATE=codexdriver` through crew-start; codex-implements/claude-reviews; capture token-offload split.
- **hk-tckw3.4** Step 3 — delete now-dead code + tests; green build.
- **hk-tckw3.5** Step 5 — widen to multi-node DOT + resume back-edge under `danger-full-access`.

## Verification is TWO layers (operator, plan §7.3) — NOT "one bead cleared"
1. **(a) Heavy in-crew testing during implementation** — run a VERY significant testing process directly via YOUR OWN SUBAGENTS. Beads may not be the right vehicle for this testing; use subagents to exercise the change thoroughly (argv-tier unit tests, live-bead smoke, resume branch, fallback-committer under danger-full-access, shell-facet EPERM check). You design the testing; it need not be bead-shaped.
2. **(b) Assessor complete-system test as the GATE** — when implementation is complete, hand to the **assessor** (comms `--to assessor` / captain will route) for a full system test before it is considered done. The bar is "thoroughly tested by the crew + assessor-signed-off," not one bead.

## Shared-file watchpoint (coordination)
A sibling crew (**juliet**, epic hk-04q2j) is removing the daemon's boot-time auto-drain and edits **different regions** of `internal/daemon/workloop.go` and the `internal/daemon/*_test.go` tree. Before finalizing any daemon-file bead, rebase onto the target branch so the daemon merge stays clean. If you hit a real conflict on workloop.go or a shared test helper, post `--topic status` to captain and I will serialize.

## Model / posture
Opus (design + failure-triage lane). Sandbox posture is `danger-full-access` LOCAL — same host posture as Claude; this is intended (D3). gb-mbp stays `enabled:false` — do NOT enable any ssh worker (that path is scrapped). Substrate for the proof = `HARMONIK_SUBSTRATE=codexdriver` with NO worker bound.
