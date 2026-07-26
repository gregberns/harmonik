# Mega-Review Condensed — Lens: Test-Theater / Fake-Anchored Coverage / Coverage Gaps / Dead Code / Spec Drift

Deduped and ranked from all 31 raw RU files. Source RU noted per item. Severity is the reviewer's, adjusted where the lens changes the stakes.

---

## Tier 1 — HIGH: whole subsystems dead or spec obligations that never run in production

### 1. `internal/workflowvalidator/*` — entire package is a dead second DOT parser + validator (~1085 impl + ~2000 test LOC)
- **dead-code / high** · `internal/workflowvalidator/validator.go:1` · RU-15
- A complete parallel implementation of the workflow-graph pipeline (own DOT parser, Tarjan SCC, BFS reachability, EM-038 rules). Zero non-test references anywhere; the live path is `internal/workflow/dot`. The migration bead (hk-0a60l/hk-kxygy) landed but the old package was never deleted.
- **Why it matters:** ~2000 lines of green tests give a false impression of coverage for code the daemon never executes; two full engines maintained in parallel invite silent drift.
- **Companion drift (spec-drift / medium, `validator.go:268`, RU-15):** the two validators enforce *opposite* rules for `handler_ref` on non-agentic nodes (live: required; dead: forbidden) and disagree on `start_node` vs `start_node_id`. Inert today, but a landmine for anyone who resurrects/copies the dead package.

### 2. `internal/operatornfr` — entire production API unwired; 22 of 37 test files are pure spec-mirror theater
- **dead-code / high** · `internal/operatornfr/exitcode.go:36` · RU-20
- No product (non-test) code imports the package. Every exported symbol (ExitCodes/LookupExitCode, command-exit-code sets, CommitHashIntegrityCheck, IsPathWithinWorkspace, upgrade-marker read/write, BinarySigningPolicy) has 0 external callers. The real daemon enforces (or fails to enforce) these obligations elsewhere; this package is a parallel spec-mirror that proves conformance of code no one runs.
- **test-theater / high** · `restartrto_sx9r81_test.go:112` · RU-20: 22 of 37 test files (~13.6k test LOC) declare inline fixture constants and assert those literals against themselves — 0 production symbol references, 0 product imports. E.g. `const restartRTOFixtureNominalSeconds = 30` … `if restartRTOFixtureNominalSeconds != 30`. Can only fail if someone edits both halves of the same file. Reviewer verdict: DELETE these 22 files.
- **test-theater / medium** · `statemachinetransition_test.go:203` · RU-20: reimplements the pause/resume/stop/upgrade state machine locally (`smFixtureApply`) and tests the copy; the real daemon state machine is never imported. Keep the ExitCodes cross-checks, drop the fossilized transition asserts.

### 3. `internal/scenario` harness — three fully-built, green-tested subsystems the runner never calls
- **test-theater / high** · `matrixexpand.go:53` · RU-22: `ExpandMatrix`/matrix cartesian expansion (325 impl + 685 test LOC) is never invoked by `cmd/harmonik/harness.go` (it ranges raw `discovered`). A scenario declaring `matrix:` with `{{.param}}` runs exactly ONCE with template markers unsubstituted. SH-030/SH-005/SH-007 are entirely unreached in production.
- **dead-code / high** · `postsuiteleaksensor.go:111` · RU-22: `CheckPostSuiteLeaks` (SH-INV-002, ~360 lines across 3 platform variants) is never called by the runner despite its own contract's MUST. A teardown that leaks a live HARMONIK_RUN_ID process / held lease / open fd is silently never detected.
- **dead-code / medium** · `crashrecovery.go:204` · RU-22: crash-injection fixture types + `canonicalInvariantsFor` (296 lines) have no runner path — `DriveOrchestration` has no crash hook. The harness advertises crash-recovery conformance it cannot execute; types validate only their own JSON round-trips.
- **spec-drift / medium** · `cmd/harmonik/harness.go:481` · RU-22: the timeout branch never reads the partial JSONL log nor evaluates assertions (SH-026), so a timed-out scenario emits an empty AssertionResults array — operators lose the promised best-effort partial diagnostics.

### 4. EV-036 secret-field startup guard never invoked at daemon startup
- **spec-drift / high** · `internal/core/eventregistry.go:331` · RU-10
- `ScanRegisteredPayloadsForSecretFields` MUST run after registration and before the bus is sealed, refusing start on a secret-named field (event-model.md §4.10). Grep across cmd/ and internal/daemon/ finds ZERO callers — only test files invoke it.
- **Failure scenario:** a future payload with a field like `SecretToken` ships and emits to the durable JSONL log with no startup failure; the fail-closed guarantee lives only in a unit test scanning the same registry.

---

## Tier 2 — MEDIUM: real coverage gaps, dead product code, and spec/impl drift

### 5. hookrelay retry never covers the socket-absent startup race
- **spec-drift / medium** · `internal/hookrelay/hookrelay.go:544` · RU-14
- The retry loop is only entered on `daemon_not_ready` ACK; a `DialContext` failure (socket not yet listening — the normal cold-boot case) returns `bridge_dial_failed` immediately with no retry. The 25s wallMax/backoff machinery and the `bridge_daemon_startup_window_exceeded` path are effectively unreachable for the most common startup race, defeating CHB-016.

### 6. smoke Signal 3 greps for a `Refs:` trailer the smoke task never writes
- **test-theater / medium** · `cmd/harmonik/smoke.go:534` · RU-16a
- `smokeCheckCommitOnBranch` asserts `git log --grep "Refs: <beadID>"`, but the smoke task body instructs a commit message with no `Refs:` trailer and no merge-path injects one. **Failure scenario:** agent follows the task verbatim, commit lands correctly, Signal 3 reports FAIL (false negative, exit 1/2) on a healthy daemon.

### 7. Nine specs declare a requirement-prefix absent from the prefix registry
- **spec-drift / medium** · `specs/_registry.yaml:24` · RU-25
- CI, DC, FW (NORMATIVE), HD, PR, RP, SW, SS, WG all declare a front-matter prefix with no registry entry, violating the registry's own MUST. WG carries 44 req-IDs cited across internal/+cmd/; FW is normative. The duplicate-prefix lint can no longer catch a colliding re-reservation of these 9 codes, and the conformance-checklist lint is either dark or ignored.

### 8. `SubstituteTemplateParams` — dead product code kept alive by its own tests
- **dead-code / medium** · `internal/workflow/params.go:56` · RU-15
- Exported raw-string primitive with 0 non-test callers (live path is `substituteGraphParams`). Passing unit tests inflate apparent coverage of the params subsystem while exercising a function no runtime path calls.

### 9. `LoadDotWorkflowWithPolicy` (CP-057 skills_ref resolution) never called at runtime
- **dead-code / medium (wiring gap)** · `internal/workflow/loader.go:262` · RU-15
- Skills-ref resolution against policy is implemented + unit-tested but only ever called from tests; the daemon uses loaders that skip it. A workflow declaring `skills_ref` is never validated against a policy at dispatch — a spec obligation silently does not run in production.

### 10. Run-machine drive loop is production-dead, only test-exercised
- **dead-code / medium** · `internal/daemon/runshell.go:242` · RU-01b
- `drive`/`driveOnce`/`fireOnCancel` reachable only via `driveRun`, which is invoked exclusively from tests. Production drives the Run machine via `runBridge.feed`. The tests assert a code path the daemon never takes (and per a sibling finding cannot even catch the ctx-cancel busy-spin because they never cancel).

### 11. EV-034 registry sealing never implemented
- **spec-drift / low→medium** · `internal/core/eventregistry.go:57` · RU-10b
- The registry is specced to be sealed (writes forbidden) after first emit, but sealing was punted. `RegisterEventTypeAtVersion` can succeed at any point in the lifetime, including after dispatch begins — a late/stray registration silently mutates the dispatch table at runtime with no invariant enforcement.

### 12. Per-type schema-version / N-1 compat machinery is entirely speculative
- **dead-code / low→medium** · `internal/core/pertypecompat_hqwn38.go:52` · RU-10
- Every one of ~169 rows is `{v1, prev0, CompatWindowHolds:true, AdditiveOnly:true}`; no type ever advanced. The single live reader only checks `CompatWindowHolds`, which is unconditionally true, so the branch is dead in practice. ~300-line table + per-event test obligation delivering no behavior today.

### 13. `state_cmd.go` still honors the retired `HK_PROJECT` env var
- **spec-drift / low→medium** · `cmd/harmonik/state_cmd.go:107` · RU-16a
- `harmonik state` reads `HK_PROJECT` as first-priority project source, while every other subcommand uses `--project`/cwd. A stale `HK_PROJECT` makes `state` silently snapshot the wrong project.

---

## Tier 3 — LOW: smaller drift, dead knobs, and unfinished fields

- **spec-drift / low** · `internal/core/eventtype.go:6` · RU-10b — header says "~79 constants" but 179 EventType constants defined (>2x off); misrepresents §8 taxonomy coverage on a spec-anchored file.
- **spec-drift / low** · `internal/keeper/watcher.go:1279` · RU-13 — `WarnOnly` gates `RunForIdle` but NOT `MaybeRun`/`RunForPrecompact`; a caller setting `WarnOnly=true` + non-nil Cycler + TmuxTarget would fire the destructive handoff+clear cycle in "warn-only" mode. Latent contract violation.
- **spec-drift / low** · `internal/daemon/projectconfig.go:1664` · RU-06 — `keeper.hard_ceiling.cooldown` is parsed + strict-validated but never consumed (resolver reads `cadence.hard_ceiling_cooldown`). A config trap: plausible key that silently does nothing.
- **spec-drift / low** · `internal/queue/rpc.go:1295` · RU-09 — `HandleQueueSetConcurrency` doc claims "validates N >= 1" but never checks `req.N`; negative N silently skips the spawn-cap guard, correctness left to an optional collaborator.
- **spec-drift / low** · `internal/lifecycle/orphansweep.go:956` · RU-12 — `EnumerateStaleIntents` doesn't filter dirs / `.tmp-` / non-`.json` like sibling `GCRetiredIntents`, so the surfaced stale-intent count is inflated vs what the GC pass processes.
- **spec-drift / low** · `internal/brcli/terminaltransition_bi010.go:336` · RU-12x — `ReopenBead` doc says `br reopen --reason` but code issues `br update --status open --notes`; doc/impl drift on a load-bearing terminal-transition method.
- **spec-drift / low** · `internal/core/daemonevents_hqwn59.go:272` · RU-10b — payload field self-flagged as "stale, needs cross-spec amendment" with a dangling TODO; consumers can't tell which shape is authoritative.
- **spec-drift / low** · `internal/agentmanifest/brief.go:74` · RU-23 — context entries with `as:instruction` pass validation but `BuildBootDoc` only handles `skill`/`doc`, so they're silently dropped from the boot doc.
- **dead-code / low** · `internal/hook/sessionstore.go:84` · RU-14 — `session.closed` is never set true (close is done via map delete); the `sess.closed` guard in `updateOutcome` is always-false dead code and the doc describes non-existent soft-closed state.
- **dead-code / low** · `internal/daemon/pasteinject.go:353` · RU-05a — `hasChildProcess`/`hasAnyDirectChild`/`commandMatchesLiveAgent` reachable only from tests; live liveness uses the `*OrSSHFail`/`*Via` variants. Two near-identical copies invite drift.
- **dead-code / low** · `internal/lifecycle/tmux/orphanwindow.go:146` · RU-05b — `windowSweepAnyRemain` takes a `prefix` param it immediately discards (`_ = prefix`); misleading dead signature surface.
- **dead-code / low** · `internal/core/eventregistry.go:205` · RU-10 — `CurrentPayloadSchemaVersion` is a pure alias with no non-test caller; duplicate exported API to keep in sync.
- **dead-code / low** · `cmd/harmonik/decisions_k4.go:548` · RU-16a — `decisionsClientProjection` is production-file code used only by a parity test; a second projection impl that can drift from the daemon's K3 projection it mirrors.
- **dead-code / low** · `internal/usage/usage.go:276` · RU-23 — `knownSessionIDs` is created + threaded through but never populated; intended dedup-by-session-id is dead, so a daemon run can be double-counted as productive + orchestrator.
- **coverage-gap / low** · `internal/keepertest/l1_contract_test.go:135` · RU-19 — L1/L2 golden-outcome replay is anchored to SYNTHESIZED input derived from the summary's own `Classify()`, not recorded input traffic; largely proves synthesizer↔reactor agree on stratum shape, not conformance to real captured input. The "507-cycle corpus golden replay" framing overstates input-side independence.
- **test-theater / low** · `internal/specaudit/sh_inv005_declarative_loadable_test.go:250` · RU-21 — `TestSHINV005ImplementationAudit` asserts the source contains the substring `"KnownFields(true)"` rather than exercising strict parsing; a rename/wrap/stale-comment keeps it green while strict mode regresses. Behavior is only really covered by the sibling corpus-lint frame.

---

## Nits

- **spec-drift / nit** · `internal/core/eventregistry.go:186` · RU-10 — stale "71" event-type count across doc comments vs ~169 registered (comments also say "79"/"169"; mutually inconsistent).
- **dead-code / nit** · `internal/lifecycle/tmux/windowname.go:14` · RU-05b — unused constant `hashSuffixLen`; will drift from the actual hash width.
- **dead-code / nit** · `internal/codexwire/codexwire.go:275` · RU-07 — `parseResult` is a no-op stub returning nil with a doc comment describing behavior it doesn't implement; leftover scaffolding.
- **dead-code / nit** · `internal/queue/append.go:194` · RU-09 — dead computed-and-discarded `_ = tailStart + i` expression in the deferred-event loop.
- **dead-code / nit (redundant branch)** · `internal/sentinel/governor.go:517` · RU-23 — if/else staircase where both branches do `ConsecutiveLowWindows++`; misleads a reader into thinking the moderate band is treated differently.
- **test-theater / nit** · `internal/specaudit/sh_inv005_declarative_loadable_test.go:162` · RU-21 — spec-body-audit greps spec prose for enforcement-phrase substrings; a prose lint masquerading as a conformance sensor, inflates the SH-INV-005 test count.
- **fake-anchor / nit** · `internal/keepertest/canary_test.go:28` · RU-19 — redundant frozen anchor: `count` duplicates `started` (both 507).

---

## Coverage gaps — units that did NOT fully review their scope

- **RU-08 — status: partial.** Reviewed the substrate/Handler-contract interface seam and the largest production sources (~5k LOC). The ~50 one-clause-per-file HC-xxx contract files and ~14k LOC of test files were SAMPLED, not exhaustively read. Findings focus on interface seams and driver logic only — the HC-xxx contract files and handler test suite are an un-audited surface for this lens (potential undetected test-theater/dead-code there).
- No RU reported status `failed`. All others are `reviewed`.
