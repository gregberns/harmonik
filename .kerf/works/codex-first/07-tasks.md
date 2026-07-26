# 07 — Implementation Tasks

> Pass 7. Decomposed from `04-design/code-seams-design.md` + `_plan.md` §6 sequencing. Each task is
> independently landable. Step 2 (T3) is the go/no-go gate: if the local bead does not clear, fan
> out on the failure BEFORE hardening (T4/T5).
>
> **Bead note:** the daemon is DOWN and this project's standing guidance is "no beads while daemon
> off." Tasks are listed here (not created as beads). When the daemon is back, the Captain turns
> T1–T5 into work items and the two test tasks (T-SCEN / T-EXPL) into the crew's subagent test plan
> + assessor gate per the reframed verification (`_plan.md` §7.3). Do NOT close this work until the
> test tasks pass.

## Task List

### T1 — Drop the isolation fence (component A / D1)
- **What:** flip `requireIsolationBoundary`/`requireBoundary` to false so codexdriver falls through
  to `LocalRunner`; remove the `refusedIsolationBoundaryArgv0` diagnostic; remove the
  `workloop.go:3626` guard block + the now-always-false `CodexRequireIsolationBoundary` plumbing.
  Leave `codexWorkerRoutingRunner`/SSHRunner inert + marked deprecated.
- **Spec sections:** harness-contract.md §4.10 **HN-025**.
- **Deliverables:** `cmd/harmonik/substrate_select.go`, `internal/daemon/workloop.go`,
  `internal/daemon/daemon.go`, `cmd/harmonik/main.go`, `cmd/harmonik/run.go`.
- **Acceptance:** `HARMONIK_SUBSTRATE=codexdriver` + no worker launches a codex implement node on
  `LocalRunner` — no refusal, no refused-argv0, no isolation-boundary stderr (accept. crit. 1).
  `go build`/`go vet` green.
- **Depends on:** none.

### T2 — Codex-exec posture → `danger-full-access` (component B / D3)
- **What:** change `sandbox_mode="workspace-write"` → `"danger-full-access"` on BOTH argv branches
  (`codexlaunchspec.go:230,236`); delete the writable-roots derivation
  (`codexExecWritableRoots`/`codexWritableRootsArg`) and the app-server twin
  (`codexWorktreeWritableRoots`/`codexGitCommonDir`, `substrate_select.go:286-320`) + its
  `WritableRoots` hook. KEEP the `-c` mechanism and the `codexHeadlessSandbox` constant. Add a
  `TestBuildCodexLaunchSpec` asserting `danger-full-access`, no `--sandbox`, no `writable_roots` on
  both branches (mirror the hk-daegv test).
- **Spec sections:** harness-contract.md §4.10 **HN-026** (S1, S2).
- **Deliverables:** `internal/daemon/codexlaunchspec.go`, `cmd/harmonik/substrate_select.go`, the
  new/updated launch-spec unit test.
- **Acceptance:** effective posture is `danger-full-access` on both branches, no residual
  `workspace-write`/`writable_roots` in the argv (accept. crit. 4); unit test green; `ubs` clean.
- **Depends on:** none (parallel with T1).

### T3 — Live-prove one bead end-to-end (GO/NO-GO gate)
- **What:** bring the daemon up locally, `HARMONIK_SUBSTRATE=codexdriver`, no worker; run one
  trivial real bead through the unmodified DOT flow.
- **Spec sections:** HN-025 + HN-026 (behavioral proof).
- **Deliverables:** a run record + captured logs (proof, not code).
- **Acceptance:** accept. crit. 2 (implement → commit with `Refs:` trailer → review verdict → bead
  closed), crit. 3 (codex's own `exec_command`/gate shell step runs with NO EPERM/"Operation not
  permitted" — retires the exec-facet + research/05 #12 live-verification debt). Either committer
  (self or fallback) counts.
- **Depends on:** T1, T2.

### T4 — Delete now-dead code (harden)
- **What:** remove the refused-argv0 doc block, the `CodexRequireIsolationBoundary` field/plumbing,
  and the deleted writable-roots helpers' tests/call sites left after T1/T2. Keep
  `codexWorkerRoutingRunner` inert (P3 removes it).
- **Acceptance:** `go build`/`go vet`/`ubs` green; no dead-symbol references (accept. crit. 7).
- **Depends on:** T3 (do not harden before the gate passes).

### T5 — Codex crew staffing + offload (component D / 3d)
- **What:** verify/thread `HARMONIK_SUBSTRATE=codexdriver` through the Captain's crew-start path
  (add if absent); run a crew that implements on codex and reviews on claude and clears ≥1 bead;
  capture the token-offload split.
- **Spec sections:** HN-025 (local launch enables codex crews).
- **Deliverables:** Captain crew-start env path (if a change is needed) + a captured run.
- **Acceptance:** accept. crit. 5 (crew implements-on-codex/reviews-on-claude, clears ≥1 bead) +
  crit. 6 (offload demonstrated qualitatively).
- **Depends on:** T3.

### T6 — Widen (confirm resume branch + fallback under danger-full-access)
- **What:** run a couple more beads of varying shape (a multi-node DOT, a review back-edge/resume
  turn) to confirm the resume argv branch and the fallback committer both behave.
- **Acceptance:** both branches + both committers behave; no regressions.
- **Depends on:** T3.

### T7 — Land the HN-025/HN-026 spec amendment
- **What:** apply `05-spec-drafts/harness-contract-amendment.md` into `specs/harness-contract.md`
  §4.10; confirm HN-025/HN-026 free in `specs/_registry.yaml` at apply time.
- **Deliverables:** `specs/harness-contract.md`.
- **Acceptance:** additive (HN-023 honored); IDs unique; renders in the spec tree.
- **Depends on:** none (can land with T1/T2; keeps spec in sync with code).

## Validation / Acceptance Test tasks (REQUIRED — must pass before this work closes)

### T-SCEN — scenario: codex-first — a labelled bead clears end-to-end through DOT cascade
> **AMENDED 2026-07-22 (juliet).** The original text below the line described
> `HARMONIK_SUBSTRATE=codexdriver`. That approach is **DEAD**: the assessor proved it
> argv-byte-identical to the per-bead label but with strictly more blast radius (it hands every
> claude-resolved bead a codex-JSON-RPC-locked substrate, re-arming the hk-3eso9 silent hang).
> See hk-oulya. The surviving path is the tier-1 `harness:codex` **label**.

- **What:** end-to-end validation of the LABEL path: label one real bead `harness:codex`, dispatch
  through **DOT cascade**, and confirm the implement node runs on codex while **both** reviewer
  nodes (`review`, `qa`) stay claude-code and emit a verdict; commit gate runs; a `Refs:<bead>`
  commit lands; clean (no-EPERM) codex shell step; bead reaches closed.
- **Must also assert ROUTING, not just outcome** — a green run with the wrong routing is false
  evidence. Check `harness_selected` reports tier 1 for **implement** and the node pin (not tier 1)
  for `review`/`qa`.
- **Preconditions:** resolved workflow mode is `dot`, and the bead carries no `workflow:<mode>`
  label. See `plans/2026-07-21-codex-first/RAMP-PLAN.md` §1, §1a.
- **Bead:** hk-ity2u. **Depends on:** T1, T2, T3 + the assessor capability verdict. Do NOT run this
  to find out whether codex is capable — that is testing in production.

### T-EXPL — explore: codex-first — operator-facing surface of per-bead labelling
> **AMENDED 2026-07-22 (juliet).** Reframed off the dead `HARMONIK_SUBSTRATE=codexdriver` approach
> (see T-SCEN note and hk-oulya) onto the surviving label path.

- **What:** exercise the operator-facing surface of **labelling** — how an operator labels a bead,
  sees which beads are labelled, observes implements-on-codex / reviews-on-claude, and un-labels to
  revert (no daemon restart, no binary swap, no config change).
- **Highest-value probe:** the **silent-ignore** case. Multiple `harness:` labels, or an invalid
  agent-type value, are both treated as **absent** (`harnessresolve.go:79-85`), so the bead quietly
  runs on claude and the operator learns nothing. That is the difference between "codex failed" and
  "codex never ran".
- **Bead:** hk-32xma. **Depends on:** T5.

## Dependency Graph
```
T1 ─┐
T2 ─┴─> T3 ─┬─> T4
            ├─> T5 ──> T-EXPL
            ├─> T6
            └─> T-SCEN   (T-SCEN also needs T1,T2)
T7 (independent; land with T1/T2)
```
Valid DAG. T3 is the gate; T4/T5/T6 and the test tasks all wait on it.

## Parallelization Plan
- **Concurrent:** T1 ∥ T2 ∥ T7 (independent seams / spec text).
- **Serialize:** T3 after T1+T2 (needs both). T4, T5, T6 after T3 (do not harden before the gate).
  T-SCEN after T1/T2/T3; T-EXPL after T5.
- **Gate discipline:** if T3 fails, STOP and fan out on the failure (major-issue protocol) before
  any hardening — do not proceed to T4+.
