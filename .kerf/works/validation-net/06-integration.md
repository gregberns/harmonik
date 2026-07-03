# validation-net — Integration

## How the components fit
- **C2 (infra: VN1 fixture + VN2 twin + VN3 make target)** is the foundation: it makes the bug-class reproducible
  and scenario tests cheap. Must land first.
- **C1 (VN4 flagship + VN5 launch-liveness + VN6 merge)** is built ON the C2 fixture/twin. The flagship VN4 is the
  keystone regression guard.
- **C3 (VN7 gate-escape + VN8 gate-efficacy)** and **C4 (VN9 CI lane + VN10 quarantine restore + VN11 flake)** are
  independent of C1/C2 and run in parallel.
- **C5 (VN12 comms + VN13 verdict-absent)** is stretch, any time.

## Sequencing (see 07-tasks.md for bead IDs)
Wave 1 = VN1, VN2 (+ independents VN3/VN7/VN9/VN11) → **wait for VN1+VN2 merge** → Wave 2 = VN4/VN5/VN6 →
Wave 3 = VN10 (after VN9). No hard `br` deps (open-dep insta-fails daemon dispatch); the captain enforces order.

## Boundaries with other works
- **hk-37giq / hk-goczd / hk-pj4b6 / hk-tcenh** are fix beads (other lanes) — validation-net writes the tests that
  guard them, does not own the fixes. VN7 pairs with the hk-pj4b6 fix; VN4 guards hk-37giq.
- **captain** (`hk-tfxjp`/`hk-zi4ej`/`hk-rbpss`) and **codex** (`hk-vfmn9`) scenario beads are referenced exemplars,
  not duplicated.
- **commit_gate dependency:** infra/CI beads that touch `internal/daemon`/Makefile (VN3/VN9/VN10) can only LAND once
  the commit_gate no-escape loop (`hk-pj4b6`, named-queues) is fixed — daemon-touching beads currently fail at the gate.

## Supersedes — with an EXPLICIT carry/drop ledger (reviewer-required)
`testing-strategy-uplift` (stalled at integration, 0 beads filed; 8-track SPEC at
`~/.kerf/projects/gregberns-harmonik/testing-strategy-uplift/SPEC.md`). validation-net absorbs only its
**scenario / coverage-execution slice**. To avoid silently recreating the original "strategy-but-no-execution" gap,
the carry/drop is explicit:
- **CARRIED** → the scenario + integration-coverage tracks (T3/T4-ish) = validation-net C1–C5.
- **RE-HOMED** as a thin follow-up bead **VN14 `hk-yag0s`** → T5 (depguard component-matrix activation in
  `.golangci.yml`) + T6 (coverage-baseline calibration — the coverage gate is **vacuous** today). Cheap, high-value.
- **EXPLICITLY DROPPED (not carried)** → T1 (per-package unit-gap audit), T2 (property/`rapid` tests), T7 (cadence),
  T8 (friction-mining doc). If the operator wants these, re-open `testing-strategy-uplift` or file fresh beads —
  validation-net is scoped to the high-blast-radius scenario gap, not a full test-suite overhaul.
