# T4 — Scenario Test Coordination + Checklist: Research Findings

**Track:** T4 — Scenario Test Coordination  
**Date:** 2026-05-20  
**Status:** complete

---

## Research Questions

1. What are the actual bead IDs for the P0 scenario gaps (the "hk-sc1..hk-sc5" references)?
2. What scenario tests already exist?
3. What twin variants are needed (--scenario X flags)?
4. What does the scenario test harness expect?

---

## Findings

### Q1: Actual bead IDs for scenario gaps

The original gap audit (`docs/scenario-test-gap-audit-2026-05-18.md`) referenced "hk-sc1..hk-sc5" as placeholders. The actual filed beads from 2026-05-20 activity are:

**From the hk-p3diy epic** ("Scenario-test plan: twin-driven spawn-path coverage, hk-012af class"), the child beads are:
  - hk-x3s1p: SC-1 — twin single-happy-path + AdapterRegistry2 wired (N=1)
  - hk-92v9m: SC-2 — twin single-happy-path N=2 concurrent (parallel spawn both complete)
  - hk-xfhva: SC-3 — twin silent-hang + AdapterRegistry2 wired (HC-056 timeout fires, bead reopened)
  - hk-wzygs: SC-4 — SIGINT during active run (daemon drains queue to cancelled, no orphan twin)
  - hk-35mpj: SC-5 — twin emits handler_capabilities but NEVER agent_ready (watcher exits, bead reopened)
  - hk-nx5wu: SC-6 — daemon.Start composition-root wiring scan (all pre-Seal subscriptions present)
  - hk-04azt: SC-7 — tmux substrate path (twin via real tmux pane, revWatcher nil-guard regression)
  - hk-d8u1y: Delete workloop nil-guards on adapterRegistry (force tests through daemon.Start composition root)

**Note:** These are NOT the original hk-sc1..hk-sc5 from docs/scenario-test-gap-audit-2026-05-18.md — those appear to never have been filed with those placeholder IDs. The hk-p3diy children are the current canonical set, covering a superset of the original 5 gaps.

The original audit's 5 gaps map approximately to:
  1. HandlerPause policy goroutine wired → hk-nx5wu (SC-6) + broader coverage
  2. Reviewer-phase tmux substrate wired → hk-04azt (SC-7)
  3. Reviewer-phase nil watcher handled → hk-04azt (SC-7)
  4. Handler-fatal → pause trip → queue held → hk-x3s1p/hk-xfhva (SC-1/SC-3)
  5. Reviewer ready-timeout → kill + reap → bead re-queued → hk-xfhva (SC-3)/hk-35mpj (SC-5)

All SC beads blocked by hk-p3diy (the epic); hk-p3diy is P1/OPEN.

### Q2: Scenario tests that already exist

`test/scenario/scenarios_test.go` has 5 real scenario tests (from the twin-parity audit):
  - Fix1: hk-lj1p9.3 (queue lifecycle)
  - Fix2: hk-tjl40
  - Fix3: hk-smuku
  - Fix6: hk-o5eww
  - Fix10: hk-cmybm

`internal/daemon/t2_scenarios_test.go` also has scenario tests tagged with the scenario build tag (confirmed by earlier analysis).

The test harness is in `test/scenario/harness_test.go` and references twin binaries from `test/twins/` (hang/main.go, fail-immediately/main.go).

### Q3: Twin variants needed for SC beads

From hk-p3diy children and original audit:
  - `harmonik-twin-claude --scenario budget-exhausted` → emits budget_exhausted after agent_ready
  - `harmonik-twin-claude --scenario handler-fatal` → emits agent_failed with is_handler_fatal:true
  - `harmonik-twin-claude --scenario reviewer-hang` → implementer completes; reviewer hangs
  - `harmonik-twin-claude --scenario agent-ready-hang` → emits handler_capabilities, never agent_ready

The `harmonik-twin-claude` binary exists (confirmed: `/Users/gb/github/harmonik/harmonik-twin-claude` is a Mach-O arm64 executable). The `--scenario` flag support is the gap.

### Q4: Harness expectations

The scenario test harness in `test/scenario/harness_test.go` (based on `02-analysis.md`):
  - Spawns the `harmonik-twin-generic` binary (from `make build-twin-generic`)
  - The existing scenarios_test.go tests reference specific bead IDs and check outcomes
  - The `//go:build scenario` tag gates them — run via `go test -tags=scenario ./test/scenario/...`

T4's authoring checklist must document: new scenario tests must build the appropriate twin binary, reference the bead they catch, and pass the checklist.

---

## Options and Tradeoffs

**Where to write the scenario authoring checklist:**

Option A: New file `docs/scenario-test-authoring-checklist.md`
- Pros: standalone, easy to link
- Cons: adds another doc to maintain

Option B: Section in `docs/foundation/project-level/testing.md`
- Pros: single source of truth for all testing conventions
- Cons: testing.md already long

Option C: Doc comment in `test/scenario/harness_test.go`
- Pros: colocated with the harness
- Cons: not linkable from beads

Verdict: Option A — standalone file, linked from testing.md §"Scenario tier" and from TASKS.md.

---

## Risks and Unknowns

1. hk-p3diy is blocking all SC beads — T4 CANNOT proceed until hk-p3diy is unblocked. T4 is a dependent, not a parallel track.
2. The original hk-sc1..hk-sc5 placeholder IDs were never formally resolved — the 03-components.md reference to them is stale. The T4 bead must update this reference to hk-p3diy children.
3. `harmonik-twin-claude` binary is in the repo root, not in cmd/ — its source location needs investigation for the twin extension pattern documentation.
