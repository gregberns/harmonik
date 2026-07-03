# reap — Change-Spec Review

Review of `05-specs/C1..C6-*.md` against `03-components.md` (requirements), `04-research/{component}/findings.md`, and `02-analysis.md` (real file paths). Single fresh-context review round (the spawning agent runs without the Agent tool; this is a fresh-context re-read against the jig's Review Criteria, per the project review-gate rule). Verdict: **PASS with two integration-pass carry-forwards (no spec defects requiring a re-round).**

## Review checklist (jig Review Criteria)

### 1. Every requirement from 03-components.md has a corresponding spec section — PASS
Mechanical cross-check (grep of `C[0-9]-R[0-9]+` per file): all 37 requirements appear in their component spec — C1-R1..R6, C2-R1..R6, C3-R1..R6, C4-R1..R7, C5-R1..R6, C6-R1..R7. Each is named in the Requirements section AND addressed in Approach + an Acceptance Criterion (AC) with a matching `(Cn-Rk)` tag.

### 2. No spec content exists that is not backed by a requirement — PASS
Every normative clause traces to a requirement: PL-006e→C1-R1..R6; PL-006f→C2-R1..R6; QM-002c→C3-R1..R6; PL-014b→C4-R4..R6, PL-019(i)→C4-R1..R3; PL-002c→C5-R1..R6; PL-005c→C6-R1..R7. The two new events (`spawn_cap_exceeded`, `daemon_boot_throttled`) trace to C4-R5 / C6-R5 (observability). The two new exit codes (24, 26) trace to C5-R2 / C6-R2.

### 3. Acceptance criteria are concrete and testable — PASS
Each AC names an observable terminal condition: a file-system state (`os.Stat → ENOENT`), a Beads status (`open`), a queue item status, an event in `.harmonik/events/events.jsonl` with a named field/reason, an exit code, or a `VerifyCommandExitCodeSets()` pass. No vague language ("works correctly") survives. The reproduce-first ACs (C2-AC1, C6-AC1) assert BOTH the with-fix and without-fix branch in one table-driven test, locking the fix per the jig's reproduce-first motivation (hk-aievp / hk-ry3be).

### 4. Files & changes reference real paths — PASS (validated against 02-analysis.md + live grep)
Spot-checked anchors that the specs cite: `internal/daemon/orphansweep.go` (RunOrphanSweep ~198, ToPayload ~24-92) ✓; `internal/workspace/orphansweep.go` (SweepStaleLeaseLocks 60-98) ✓; `internal/workspace/createworktree.go` (reviewer-cleanup `git worktree remove --force --force` idiom) ✓; `internal/lifecycle/orphansweepbeads.go` (SweepStaleInProgressBeads, IntentProvenanceSet) ✓; `internal/lifecycle/startup_pl005_qm002.go` (reconcileDispatchedItems ~169) ✓; `internal/supervise/supervisor.go` (Run 185, buildCmd/Setpgid, terminateChild, backoffWithJitter) ✓; `internal/daemon/workloop.go` (~595 capacity gate) ✓; `internal/daemon/runregistry.go` (Register/Unregister/Len) ✓; `internal/lifecycle/pidfilelock.go` (ProbePidfileLock) ✓; `cmd/harmonik/supervise/start.go` (flock supervisor.lock, ExitCodeSupervisorRunning=25, ExitCodeDaemonDown=17) ✓; `internal/operatornfr/{exitcode.go,commandcodes.go}` (§8 registry 0–23, VerifyCommandExitCodeSets) ✓. New files (`worktreereap.go`/`runledger.go`/`flywheellock.go`/`bootlog.go`/`bootbackoff.go`) are correctly marked NEW and modeled on a named precedent.

### 5. Verification steps are runnable — PASS
Each spec gives `go test ./internal/<pkg>/... -run '<pattern>' -count=1` plus a manual smoke. Test-file patterns match the existing PL-clause-suffixed convention (`*_pl006_test.go`, `orphansweepbeads_test.go`, `supervisor_test.go`, `provenance_pl006a_test.go`) named in 02-analysis.md §A/§E.

### 6. Error handling and edge cases addressed — PASS
Every spec has an explicit Error handling & edge cases section: locked worktree / prune-already-ran / symlink-escape (C1); ledger-append failure / corrupt row / generation mismatch / unbounded growth (C2); ShowBead failure / worktree-removal race vs C1 / double-work (C3); re-entrant reap / PGID-escape / cap-counter leak (C4); recycled PID / ENOLCK / torn marker / both-locks-contended (C5); missing-disposition / clock-regression / corrupt row / retry-storm (C6).

### 7. Approaches consistent with research findings — PASS
Each spec records explicit Decisions resolving the open change-spec questions the research flagged: C1 chose A-then-B worktree removal + preserve-sidecar-evidence (research R3); C2 chose the run-ledger (Option A) + prune-on-terminal + generation tag (research R1/R3); C3 narrowed scope to dead-run + terminal-failure branches per QM-002a's already-done claim-write-lost case (research RQ1); C4 chose project-prefix reap (Option A) relying on the C5 invariant + cap counts reviewer/resume spawns + refuse-not-sleep (research RQ3/R1/R2); C5 chose the dedicated `.harmonik/flywheel.lock` (Option A) scoped to flywheel topology (research RQ2/R1) + code 24; C6 chose append-with-prune `boots.jsonl` (Option B) + delay-then-refuse + code 26 (research RQ3/RQ5).

## Findings / carry-forwards (NOT blocking — for the Integration pass)

- **CF-1 (exit-code adjacency, from C5 research RQ4):** code 25 (`supervisor-already-running`) is a LOCAL const in `start.go:23` referenced by PL-019(c) as "PL-INTERIM pending ON absorption" but is NOT in the central operator-nfr §8 registry; and `start.go:19`'s local `ExitCodeDaemonDown=17` semantically collides with the registry's code-17 `multi-daemon-target-missing`. The C5 spec FLAGS both and explicitly says "do NOT silently fix." The Integration pass (06-integration.md) should reconcile: absorb 25 into §8 alongside the new 24, and disambiguate the code-17 dual meaning. Recorded, not actioned here (out of strict reap scope; touching it silently would violate file-discipline).

- **CF-2 (PL-005 step renumbering):** C6 inserts a "step 1.5" boot-backoff gate into the PL-005 deterministic startup-order list. The Integration pass must confirm the insertion does not collide with any other in-flight spec amendment to PL-005's numbered list (pilot's quiet-daemon work also touches PL-005 auto-pull). The reap text uses "1.5" to avoid renumbering existing labels; integration should verify the final merged ordering across reap + pilot.

- **CF-3 (C3↔C1 filesystem-read ordering):** C3's `dead_run_failed` classification reads "worktree present, no Refs: commit" as terminal-failure evidence, but C1 (step 3 sweep) removes bare-stale worktrees BEFORE C3 (step 8a) reads. C1's PL-006e clause (iii) preserves worktrees carrying an unprocessed session sidecar until Cat-3 reconciliation, which is the guard that keeps the `dead_run_failed` evidence alive for C3. The two specs are CONSISTENT (C3 §Error-handling cross-references C1 PL-006e), but the Integration pass should make the ordering dependency explicit in 06-integration.md (build C2 ledger first; C1 + C3 share the EM-031a survive-check inputs; C1's sidecar-preservation gates C3's terminal-failure read).

## Test-bead gate (jig Validation/Acceptance Tests) — SATISFIED
Two beads filed and recorded in C6 §Test beads: scenario **hk-a31od** (`scenario-test`) covering C1+C2+C3+C6 boot-path terminal JSONL conditions; exploratory **hk-izs8s** (`exploratory-test`) covering C4 `supervise stop` reap + C5 exit-24 refusal + C6 exit-26 throttle. Both labelled `codename:reap`, priority 1, and wired as blockers of all four implementation beads (hk-9eury, hk-xb5yi, hk-li14r, hk-7t9g1) so the tests gate the work. `br sync --flush-only` ran clean.

## Verdict
PASS. All 37 requirements covered, all ACs testable, all paths validated, all research decisions recorded, edge cases addressed. Three carry-forwards (CF-1/2/3) are integration-pass coordination items, not spec defects. Advance to Integration.
