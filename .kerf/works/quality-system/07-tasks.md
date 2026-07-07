# quality-system / `core-loop-proof` — Tasks (proposed bead breakdown)

Tranched breakdown for the `core-loop-proof` chunk. **One epic + tranched child tasks**, sized so multiple
crew can build concurrently on the `epic/core-loop-proof` integration branch. **Every bead gets label
`codename:quality-system`.** These are PROPOSED — the captain reviews and creates them (review gate); this
doc does NOT create beads.

Priority key: P0 critical / P1 high / P2 medium.

## Epic

- **E0 — `core-loop-proof`: live-verify the real task-processing loop across {claude,codex,pi}×{local,remote}**
  - type: epic · priority: P1 · labels: `codename:quality-system`
  - scope: umbrella for the matrix acceptance harness that proves bead→queue→correct-model harness→real
    change→provider-through-sandbox→DOT verdict→terminal transition, on a scratch daemon, closing the top-5
    coverage gaps.
  - deps: none · proves: all 5 gaps (via children)

## Tranche 1 — Foundation (serial; unblocks everything). One crew.

- **T1 — Matrix runner skeleton on `scratch-daemon.sh batch`**
  - type: task · P0 · deps: none (blocks T2–T8)
  - scope: single-command runner that `init`/`cycle`s a clean scratch daemon, iterates the
    `{harness}×{substrate}` cells, submits the seed bead per cell via `batch`, captures per-cell
    `harmonik subscribe --json` by run_id, prints a green/red grid + exit code. Cell gating: pi/codex
    default, claude behind `--enable-claude`, remote SKIP-loud when no tcp:// worker.
  - proves: harness scaffold for gaps 1–5 (no assertions yet)

- **T2 — Assertion-library module + seed-bead fixtures + expected-cell spec**
  - type: task · P0 · deps: T1
  - scope: standalone module (jq or small Go test binary) taking a captured event stream + expected-cell
    spec → typed per-gap pass/fail. Defines the "correct" contract chunk 2 must satisfy. Ships the known
    seed bead(s) (fixed title/body/label, pinned model+harness).
  - proves: the contract surface for gaps 1–5

- **T3 — Red-cell → deduped bead wiring (`scratch-daemon.sh feedback`)**
  - type: task · P1 · deps: T1
  - scope: on a red cell, file a deduped MAIN-repo bead via `feedback` (dedupe on cell+gap identity);
    green cells file nothing.
  - proves: closes the loop so reds become tracked work

## Tranche 2 — Gap assertions (parallel after T2; disjoint surfaces). Two crew.

*Crew A (model + fields + provider surface):*

- **T4 — Gap-1 assertion: model reaches harness per family**
  - type: task · P1 · deps: T2 · proves: **gap 1 (C4)**
  - scope: assert `model_selected` == configured model for the bead's harness family across
    {claude,codex,pi}; add the node-`model=`-pin-no-leak case (claude pin must not hit pi). Seed one row
    from the pi-model-leak regressions (hk-pkugu/lfrub/ytzj2).

- **T5 — Gap-4 assertion: queue-submit → dispatch field fidelity**
  - type: task · P1 · deps: T2 · proves: **gap 4 (C7)**
  - scope: submit a fully-specified item (`workflow_ref, workflow_mode, model, harness`); assert the
    dispatched run carried every field; no hardcoded review-loop override. Seed from hk-u6zp/hk-y3o51.

- **T6 — Gap-3 assertion: provider comms through the sandbox**
  - type: task · P1 · deps: T2 · proves: **gap 3 (C3-provider/C6)**
  - scope: assert ≥1 `tool_call` + real HEAD content change; assert a `content:null`/no-`tool_call`
    provider reply surfaces an explicit failure event, not a silent no-commit. Seed from hk-4ir08/hk-u69my.

*Crew B (substrate parity + startup):*

- **T7 — Gap-2 assertion: remote(tcp://) path == local path**
  - type: task · P1 · deps: T2 (uses remote-test-pyramid runner seam) · proves: **gap 2 (C2)**
  - scope: run the same seed bead through the remote runner; assert the event sequence + terminal outcome
    equal the local cell (same seam; no sandbox-wrap misapplied to tcp). Seed from hk-ybuts. Requires a
    reachable tcp:// worker; SKIP-loud otherwise.

- **T8 — Gap-5 assertion: real Claude-worktree startup → agent_ready (flag-gated)**
  - type: task · P1 · deps: T1 · proves: **gap 5 (C8/PR-19)**
  - scope: real git-worktree claude launch (behind `--enable-claude`) reaching `AgentReady` past
    folder-trust/permissions/onboarding modals; assert no `AgentReadyTimeout`/`StallDetected`. Minimal
    token spend. Seed from HANDOFF-gb-pr-19.

## Tranche 3 — Close-out (serial after tranche 2). Either crew.

- **T9 — Full-matrix green run + clean-reset reproducibility proof**
  - type: task · P1 · deps: T4,T5,T6,T7,T8 · proves: **chunk done-when**
  - scope: one command runs the whole matrix green for pi+codex×{local,remote} with claude cells passing
    when enabled; prove reproducibility across a clean scratch-daemon reset; wire the run into the orphaned
    `harmonik smoke` / a documented entrypoint. Records residual SKIPs.

## Dependency graph

```
E0 (epic)
T1 ──┬── T3
     ├── T2 ──┬── T4 ─┐
     │        ├── T5 ─┤
     │        ├── T6 ─┤
     │        └── T7 ─┤
     └────────────── T8 ─┴── T9
```

Concurrency: T1→T2 serial foundation; then Crew A (T4,T5,T6) ‖ Crew B (T7,T8) on disjoint surfaces; T9
merges. T3 slots in anytime after T1.
