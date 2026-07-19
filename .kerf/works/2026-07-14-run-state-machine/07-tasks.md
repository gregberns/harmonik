# 07 — Implementation Tasks (`run-state-machine`, M3)

> Breaks the RX draft into implementer-consumable tasks with deliverables, spec
> citations, acceptance, and a dependency graph. Execution model = P1's:
> off-daemon Claude Code sub-agents, worktree-isolated where file-disjoint,
> SINGLE-WRITER on shared packages, human-style review, merge to a branch.
> `internal/daemon` is single-owner throughout (census: no parallel agents in
> the daemon core). Every acceptance is out-of-band (`go build`/`go test`/jq) —
> never the daemon pipeline.
>
> **External gates (not tasks here):** Phase-1 (RT1–RT3) additionally requires
> **M1-1 landed** (honest coverage baseline); Phase-2 (RT4+) requires the
> **M1-5 coverage audit** artifact. P1-proven is satisfied (keeper reactor
> landed through T7 on this branch). The M3-4→M2-1 edge is satisfied BY
> CONTRACT (RX-009..011): the transitional tmux shell implements the same
> contract, so no RT task blocks on M2's implementation.

## Dependency graph (waves)

```
W0  RT0 spec landing (RX + registry [+ reciprocal touches])   [SINGLE-WRITER specs/]
W1  RT1 ClockPort (run path) ──────────────┐                  [SINGLE-WRITER daemon]
    RT2 internal/mergeq pkg [DISJOINT] ─────┤
W2  RT3 merge prepare/commit split + domain wiring (needs RT1,RT2)   [SINGLE-WRITER daemon]
W3  RT4 ports: RunEnv/RunPorts/SharedHandles (needs RT1; RT3 for MergePort)
W4  RT5 internal/runexec: vocab + Dispatch + L0 [DISJOINT] ──┐
    RT6 internal/runexec: Run machine + spine + L0 [DISJOINT]─┤ (share pkg → one agent)
W5  RT7 shell + single-mode migration (needs RT4,RT5,RT6)     [SINGLE-WRITER daemon]
W6  RT8 review-loop + DOT dispatch segments on Dispatch; caulk deleted (needs RT7)
    RT9 terminal-spine unification ×4 + runSucceeded/emitDone removal (needs RT7; RT8 for modes)
W7  RT10 replay run-checkers + run corpus + synthesizer [DISJOINT replay/testdata] (needs RT5/6)
    RT11 fault matrix + N=10 relaunch oracle + coverage floor (needs RT8,RT10)
W8  RT12 acceptance oracle + parity confirmation (needs all)
```
Parallelizable: RT2 ∥ RT1; RT5/RT6 (one agent, disjoint from daemon) ∥ RT3/RT4;
RT10 ∥ RT8/RT9.

## W0

### RT0 — Land the RX spec
**Spec:** whole draft. **Deliverables:** `specs/run-state-machine.md` from
`05-spec-drafts/`; `_registry.yaml` `RX` reservation (same commit); optional
reciprocal one-liners in RS §9 / SK §9 (integration §2.2). **Acceptance:**
registry lint passes; spec-template conformance checklist passes; RS/SK
unmodified except the optional pointers. **[SINGLE-WRITER specs/]**

## W1

### RT1 — ClockPort through the run path (C1, mirrors P1 T5)
**Spec:** RX-017 (RS-015 consumer). **Deliverables:** `workLoopDeps.clock
substrate.ClockPort` wired `SystemClock` in `newWorkLoopDeps`; the 23
enumerated run-path sites migrated (c3-workloopdeps-ports-design §4: workloop.go ×8,
reviewloop.go ×8, dot_cascade.go ×7 — the five `time.After` selects become
Clock-based deadlines); a FakeClock unit test drives one agent-ready timeout
deterministically. **Acceptance:** `go test ./internal/daemon/...` green; zero
OUTER-loop sites touched (diff-scoped check); the FakeClock test exercises the
timeout edge without wall-clock sleeps.

### RT2 — `internal/mergeq` (C2 mechanism) **[DISJOINT new pkg]**
**Spec:** RX-002, RX-012 (FIFO), RX-014 (inventory test harness).
**Deliverables:** `Queue`/`Start`/`Submit` per c2-merge-queue-design §1; depguard
`mergeq` rule (deny daemon — integration §2.3, NEW rule, TC-6 pattern);
`coverage.baseline` seed; tests: FIFO order under N concurrent Submits (-race),
ctx-cancel-before-execution, recording-runner harness scaffolding for RX-INV-005.
**Acceptance:** `go test ./internal/mergeq/... -race` green; depguard passes;
package imports `$gostd` only.

## W2

### RT3 — The merge split + exclusion-domain wiring (C2 completion, = census M3-phase-1, HARD M4 prereq)
**Spec:** RX-012..015, RX-INV-005. **Deliverables:** `mergeRunBranchToMain` →
`prepareMerge` (outside) + `commitMerge` (inside Submit) per merge-queue-design
§2, ErrStale re-validation, retry budget + reason strings unchanged; the 5
call-sites submit through the queue; escape check + remote base-sync/create
sections submitted into the SAME domain (§4); `mergeMu` deleted;
`WithMergeMutex` test hook replaced by a queue-injection hook. **Acceptance:**
the RX-INV-005 recording test proves no build-class command inside the domain
and the closed commit-inventory; `escapedetect_hkooexj_test.go` green
UNMODIFIED; full regression net green; shutdown-drain path proven with a bgCtx
submission test. **Depends:** RT1, RT2. **[SINGLE-WRITER daemon]**

## W3

### RT4 — RunEnv / RunPorts / SharedHandles (C3, mirrors P1 T6)
**Spec:** RX-016. **Deliverables:** `internal/daemon/runports.go` per
c3-workloopdeps-ports-design §1 (LedgerPort, EmitterPort alias, WorktreePort, MergePort=RT3
handle, LaunchPort, GatePort, Clock; RunEnv; SharedHandles incl. BudgetPort);
DELETE dead fields `h` + `beadAuditLogger`; lift the 4 loop-owned value fields
out of the by-value bundle; `beadRunOne` + reviewloop/dot helpers re-signed
onto the bundles (still sequential logic — this task is boundary-only);
nil-default adapters per c3-workloopdeps-ports-design §3. **Acceptance:** regression net green;
no behavior change (event goldens spot-check); each port lands as its own
green commit. **Depends:** RT1 (Clock), RT3 (MergePort). **[SINGLE-WRITER daemon]**

## W4 — `internal/runexec` **[DISJOINT pkg; RT5+RT6 = one agent]**

### RT5 — Vocabulary + the `Dispatch` machine + L0
**Spec:** RX-001, RX-005, RX-007..011, RX-INV-001/002. **Deliverables:**
`vocab.go`, `dispatch.go` per c4-runexec-reactor-design §1–§3 (full table incl. explicit
no-ops, SkipReadyHandshake config, ctx-cancel mapping); depguard `runexec` rule
(trimmed allow: `$gostd`+core+substrate+self, 00b R8); L0 table tests + property
tests (terminal exclusivity; every TimerFired edge → action-or-terminal; no
brief delivery before ready). **Acceptance:** `go test ./internal/runexec/...`
green; depguard proves the deny edges; property tests run zero-token.

### RT6 — The `Run` machine + terminal spine + L0
**Spec:** RX-006, RX-008, RX-INV-004. **Deliverables:** `run.go` per
c4-runexec-reactor-design §4 (incl. Guarding, the ONE close ladder with exact
summary-string data, merge-retry rows, DOT carve-out row, shutdown-drain edge,
subsumed/noChange no-merge entries); L0 tests covering all four terminal-entry
events → one spine. **Acceptance:** as RT5; a table test proves the 6
pre-change close-ladder variants map onto one tail with byte-identical strings.

## W5

### RT7 — The shell + single-mode migration (mirrors P1 T7)
**Spec:** RX-003, RX-006/007, RX-019. **Deliverables:**
`internal/daemon/runshell.go` (effector table per c4-runexec-reactor-design §2 failure
policies; drive loop w/ nearest-deadline wake per keeper shell.go); single-mode
`beadRunOne` path re-driven: prepareRun guards feed the Run machine,
P11–P18 replaced by machine-driven dispatch + Guarding + spine; frozen watchdog
wired as event source; `runSucceeded` still bridged (removed in RT9).
**Acceptance:** regression net green; single-mode event-stream goldens
byte-equal except allowlist; FakeClock test drives ready-timeout → reopen
deterministically. **Depends:** RT4, RT5, RT6. **[SINGLE-WRITER daemon]**

### RT8 — Review-loop + DOT dispatch segments on `Dispatch`; the caulk dies (C5 structural)
**Spec:** RX-005, RX-010, RX-INV-002/003. **Deliverables:** reviewloop.go +
dot_cascade.go launch/ready/brief/wait segments call `shell.RunDispatch`;
DELETE `resumeReadyFallbackGrace` (reviewloop.go:110) + `waitAgentReady`
open-coded waits as callers migrate; transitional readiness probe emits
run_id-stamped `EvAgentReady`; DOT resumes gain the bound. **Acceptance:**
regression net green; a FakeClock resume test: no ready signal ⇒
agent_ready_timeout + reopen within the bound (the census fault-injection
seed); grep proves the caulk constant is gone; DOT resume path exercises the
same edge. **Depends:** RT7. **[SINGLE-WRITER daemon]**

### RT9 — Terminal-spine unification ×4 + out-param removal (C6 completion)
**Spec:** RX-006, RX-008, RX-INV-004. **Deliverables:** review-loop/DOT/
agent-completed/exit-0 terminal blocks route through the Run tail (RT6);
`emitDone` closure + `runSucceeded *bool` deleted; wrapper (workloop.go:3023)
reads the Run terminal for EM-015f/hk-f722; sessiondata/bgCtx drain policies in
the effector. **Acceptance:** regression net green; grep proves zero
`runSucceeded` refs and one close-ladder site; beadRunOne line count recorded
(thin-driver DoD — target ≤ ~200 lines of guards+drive). **Depends:** RT7
(and RT8 for the mode entries). **[SINGLE-WRITER daemon]**

## W7

### RT10 — Replay run-checkers + run corpus + synthesizer **[DISJOINT replay/testdata/scripts]**
**Spec:** §6 L1, RX-INV-003/004 checkers. **Deliverables:**
`internal/replay/runcheckers.go` (run-keyed state track + RX9 finalizer
mirroring SR9Checker + RX4 ordering checker — additive, cycle-keyed surface
untouched, 00b R6 fallback noted); `scripts/extract-run-corpus.*` →
`testdata/daemon-runs/baseline-<date>/` per-run streams + summary goldens +
strata manifest; the StimulusSynthesizer (summary → reactor schedule).
**Acceptance:** RX9 flags a seeded hung-run fixture and passes a clean one;
extractor reproduces pinned manifest counts; `go test ./internal/replay/...`
green incl. existing cycle checkers unmodified. **Depends:** RT5/RT6 (vocab).

### RT11 — Fault matrix + N=10 relaunch oracle + coverage floor
**Spec:** §6 L2/Oracle, RX-INV-003. **Deliverables:** substrate.Twin fault
matrix over synthesized dispatch schedules (FaultStall-after-resume headline
cell + DropAfter/Truncate/Dup across strata), all virtual-time; the N=10 clean
relaunch runner (FakeClock review-loop harness); coverage measured on
runexec/mergeq/runshell vs the M1-5 floor and recorded. **Acceptance:** matrix
100% terminal-never-silence; N=10 green; floor met & recorded. **Depends:**
RT8, RT10.

## W8

### RT12 — Acceptance oracle + parity confirmation
**Deliverables (evidence bundle):** (1) N=10 consecutive full-suite green runs;
(2) fault matrix 100%; (3) out-of-band jq verification of RX9 over replayed
logs; (4) coverage floor record; (5) regression net + escapedetect suite green,
`git diff` shows the hk- test files unmodified; (6) the allowlist audit — every
observed stream divergence is in RX-019(a–d); (7) thin-driver metrics
(beadRunOne line count, param count, zero out-params). **Acceptance:** all
seven recorded; then `kerf finalize` copies the RX spec. **Depends:** all.

## Notes for the executor
- `internal/daemon` tasks (RT1, RT3, RT4, RT7–RT9) are strictly sequential,
  single-writer. RT2, RT5/RT6, RT10 are worktree-parallel safe.
- Review gate per orchestrator-rules on every task diff; the ratchet
  (`--new-from-rev`) WILL flag funlen/gocognit on new code and on any giant
  whose signature line the diff touches — expected (Track C note (a)); new
  extracted functions must meet the full ceiling, do not `//nolint`.
- Spec-vs-code: RX is normative; a discovered spec defect goes through kerf,
  never silent divergence.
- Deferred (explicitly NOT tasks): watchdog dissolution; full DOT/review-loop
  control reactorization; Region A re-home (M4); workLoopDeps outer remainder
  (M5); durable merge-queue event.
