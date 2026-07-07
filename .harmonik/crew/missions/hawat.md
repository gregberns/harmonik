<!-- Mission handoff — locked 6-field schema -->
```yaml
schema_version: 1
crew_name: hawat
queue: hawat-q
epic_id: hk-lgykq
goal: "Fix hk-lgykq (P1 daemon-core): wire the dead per-bead LandsOn/landTaskBranch integration-target merge path into the LIVE workloop merge, so a bead directed to integration branch X actually lands on X, not the daemon-wide target (main). This is the durable fix behind alia's C-model workaround; it must pass the very harness that tests it (dogfood)."
captain_name: captain
model: sonnet
```

## Why you exist / the dogfood contract
Today the daemon merges every run to ONE daemon-wide target (`branching.yaml` lands_on, default main). The designed per-bead path (`landTaskBranch`/`resolveLandsOn`/`squashLanding`, `internal/daemon/branching.go:499-657`) is written + unit-tested but **DEAD — its only caller is `export_test.go`.** Wire it into the live merge call (`lockedMergeRunBranchToMain` sites in `internal/daemon/workloop.go` ~3695/3905/4988/5038/5110; target resolved at ~1141 from `resolveTargetBranch`). Honour the three-tier precedence (bead `## Branching` > branching.yaml > default).

**DOGFOOD (load-bearing):** alia's harness has a KNOWN-RED assertion **T10 (hk-xke2i)** — "a bead directed to integration branch X lands on X not main" — on `integration/core-loop-proof`. Your fix must flip that cell RED→GREEN. Coordinate with alia: run T10 against your fix as the acceptance proof. Do NOT close hk-lgykq until T10 passes against your build.

## Discipline (daemon-core — high care)
- **Own worktree + own integration branch** `integration/lgykq-targeting` (cut from origin/main); NEVER commit daemon changes straight to main.
- **Prefer codex** for the Go implementation (Claude cap ~98%). NOT pi (blocked, hk-4ir08).
- **GATE-0 / pre-deploy E2E rule is MANDATORY** for any daemon-core change: add a NEW isolated end-to-end test that reproduces the changed merge behaviour on a real launch path IN ISOLATION from the live daemon (do NOT cycle the primary daemon to test). Green units are NOT the gate.
- integration→main is ONE assessor-gated human PR at the epic boundary — do not self-merge to main, do not skip independent review.
- Reproduce-before-fix; daemon owns terminal bead transitions.
- **BUG DISCIPLINE:** the instant you hit any defect/friction, append a terse block to `BUGS.md` at repo root (don't stop to fix — capture, we triage to beads/corpus later).
- Boot: confirm identity ($HARMONIK_AGENT==hawat); `harmonik comms join --name hawat`; `br update hk-lgykq --assignee hawat`; arm recv; post boot status to captain. Post progress to captain (topic status) + br comments on close + ≤10-min timer.
- CWD discipline: operate from your worktree via absolute paths / `git -C`; never `cd`; never touch main.
