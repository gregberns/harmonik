---
id: harnesspick-extract
title: Extract harness selection — the one extraction that collides with nothing
type: task
priority: 1
labels: [daemon, extraction, clear-the-ground]
depends_on: [lint-rekey-exclusion-list, harness-composition-root-policy]
blocks: []
workstream: W2
batch: 3
---

## Problem

Harness selection — which agent harness runs a bead, which model, which profile, which workflow mode
— is five files in `internal/daemon` totalling **795 production lines**. (The 2026-08-22 review says
1,218; that figure predates the comment cut. Use 795.)

**It shares nothing with the other three extractions.** Zero shared symbols, zero shared production
files. It is the only one of the four that can run fully in parallel with the run-registry keystone.

Like the run registry, the moved code references **nothing** that stays in `internal/daemon` — every
signature in the cluster is typed only against external packages.

## Scope

Move to a new package `internal/harnesspick`:

| File | Lines |
|---|---|
| `internal/daemon/harnessregistry.go` | 270 |
| `internal/daemon/modelpreference.go` | 174 |
| `internal/daemon/moderesolve.go` | 144 |
| `internal/daemon/harnessresolve.go` | 112 |
| `internal/daemon/pi_profile_resolve.go` | 95 |

**Scope may shrink.** `harness-composition-root-policy` decides whether `pi_profile_resolve.go` and
`newHarnessRegistry` are in or out. Do not start until that ruling is recorded.

Thirteen symbols cross the boundary inbound, twelve of them unexported. Five production files call
them: `bootworkloop.go`, `runports.go`, `dot_cascade_core.go`, `dot_gate.go`, `workloop_runplan.go`
(which alone uses eight of the thirteen).

**`export_launchrouting_test.go` must be SPLIT.** These wrap moved symbols and go with them:
`ExportedNewHarnessRegistry`, `ExportedNewHarnessRegistryWithPi`, `ExportedEffectiveModel`,
`ExportedRoutedLaunchSpecBuilder`, `ExportedObservedRoutedLaunchSpecBuilder`,
`ExportedPinnedHarnessLaunchSpecBuilder`. These build specs from a script path, touch nothing moved,
and stay: `ExportedPiProcessExitLaunchSpecBuilder`, `ExportedCodexProcessExitLaunchSpecBuilder`.
`export_resolvers_test.go` likewise holds one non-cluster shim, `ExportedLoadStandardGraph`.

## Done when

1. `internal/harnesspick` exists with a `depguard` rule denying `internal/daemon`.
2. Five production call-site files compile against it.
3. Five white-box test files that call moved *unexported* symbols are converted or re-pointed:
   `agenttask_completion_instruction_test.go`, `conformance_m4c7_test.go`,
   `harnessregistry_pi_remote_runner_m4c4_test.go`, `routed_preexec_identity_hksll_test.go` (all four
   call `buildCodexRoutedLaunchSpec`), and `pi_profile_resolve_test.go` (calls `resolvePiProfile`).
4. The 17 test files consuming the harness `Exported*` shims are rewritten when those shims are
   deleted. **The shims do not survive the landing that moves their targets.**
5. `tools/lintreport/allow.txt` gains nothing. 14 existing entries fall inside this cluster; they
   travel.
6. `scripts/harnesspi-freeze-gate.sh` and `Makefile:406` are updated per the policy ruling.

## Limits

- **This has the largest test blast radius of the four** — 17 shim consumers plus 13 daemon e2e tests
  that exercise the selection path. Budget for that; it is not a small move despite the clean symbol
  boundary.
- Do not start before the policy ruling. Extracting against a written policy is how stale guidance is
  made.
- Do not combine a move with a change.
