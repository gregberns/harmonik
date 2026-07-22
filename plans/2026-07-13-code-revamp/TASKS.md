# Code-Revamp — Consolidated Task Registry

> Authored 2026-07-13. **Task DEFINITIONS only — no beads filed, per explicit operator
> directive (DECISIONS.md operator verdict: "do NOT file beads — create the task definitions
> only").** This registry is the record of task units; any bead filing (including the Track C
> grandfather umbrella) is deferred until the operator lifts that directive. Daemon is OFF —
> do not enable it. Sources consolidated (not re-derived): `track-c-enforcement.md`,
> `track-b-m1.md`, `DECISIONS.md`, `.kerf/works/run-state-machine/02-components.md` (M3),
> `.kerf/works/agent-input-substrate/02-components.md` (M2). Track C is being **implemented
> this session** by another agent — its tasks are marked `in-progress` and stand as the record
> of what is being done.

## Ready to start NOW

| What | Tasks |
|---|---|
| **Applied this session (uncommitted)** | Track C config: TC-1/TC-2/TC-3 **done**, TC-4 **applied-standalone** (wire-in gated on substrate ≥90%) |
| **Ready (unclaimed)** | B1 (queue two-writer fix), B2 (Cat-3c false-close fix), M1-1 (specaudit relocate), M1-4 (event-registry delete-list strike/guard) |

Everything else is gated — see "Blocked / gated" at the bottom.

Statuses: `in-progress` / `ready` / `pending-design` / `needs-operator-signoff`.

---

## Track C — Enforcement levers (config apply; executing this session)

Source: `track-c-enforcement.md`. Key design fact: the ratchet already exists — the merge gate
is `make check-short` → `.tools/golangci-lint run --new-from-rev=origin/main`
(`.github/workflows/ci.yml:42`, `Makefile:239`), so enabling complexity linters
auto-grandfathers every existing violator with zero `//nolint` and zero baseline file. No new
kerf codename: complexity+depguard fold into `validation-net`, coverage into `quality-system`
(§5, advisory).

| Task ID | Title | Files/scope | Acceptance criterion | Depends on | Status |
|---|---|---|---|---|---|
| TC-1 | Enable complexity ceiling + depguard cleanup in `.golangci.yml` | `.golangci.yml`: add `funlen`/`cyclop`/`gocognit` to `linters.enable` (after `depguard`, ~line 40); settings `funlen: lines 100 / statements 60 / ignore-comments`, `cyclop: max-complexity 15, package-average 0`, `gocognit: min-complexity 20` (~lines 55–63); exclusion rule for `(_test\.go$\|^internal/scenario/\|^internal/specaudit/)`; same commit: add `github.com/google/uuid` to the `queue` allow-list (real dep at `internal/queue/rpc.go:32`, §2.3); convert 6 inert depguard rules (`policy` :280–282, `agentrunner` :350–357, `hook` :360–365, `memory` :367–373, `adapter-ntm` :322–324, `adapter-br` :319–321 speculative half) to commented-out "reserved for M5" blocks per the existing `# orchestrator:` convention (§2.1) | `gocyclo` NOT enabled (cyclop is the superset); zero `//nolint` directives added; inert-rule conversion is a CI no-op (rules matched zero files) | — | done |
| TC-2 | Verify grandfathering (gate stays green) | Scratch no-op branch; `.tools/golangci-lint run --new-from-rev=origin/main` (or `make check-short`) | 0 new issues reported despite 104 existing >100-line functions — proves existing violators do not gate | TC-1 | done |
| TC-3 | Confirm the ratchet bites (probe) | Throwaway ~120-line function added on the branch, then deleted | Re-run of the TC-2 gate reports a `funlen` finding on the probe function; probe removed afterward | TC-2 | done |
| TC-4 | Coverage floor: prune stale entries + scoped merge-time gate | `scripts/coverage-gate.sh`: remove nonexistent `internal/orchestrator` / `internal/reconciler` from `HIGH_THRESHOLD_PACKAGES` (lines 187, 191) as a single-concern protected-rule commit; `Makefile`: add `check-carve-coverage` target (`CARVE_PKGS = ./internal/substrate/... ./internal/runexec/...` → `go test -covermode=atomic -coverprofile` → `scripts/coverage-gate.sh`), **left STANDALONE — NOT wired into `check-short`** (wiring gated on substrate ≥90%, a fix that touches `internal/**`, outside Track C's config-only scope) | `check-carve-coverage` target added but **LEFT STANDALONE — NOT wired into `check-short`**; `substrate` (the only carve package today) is at **83.1%, BELOW the inherited 90% `internal/**` floor**, so wire-in is GATED on substrate reaching ≥90%; gate is vacuously green for absent packages (script skips missing/0-statement, lines 140–151); no new coverage tool built | TC-1 | applied-standalone (wire-in gated ≥90%) |
| TC-5 | Grandfather-debt tracking record (`codename:complexity-grandfather` umbrella) | The 11-function grandfather table in `track-c-enforcement.md` §4 (`beadRunOne` 2367 → M3, `runWorkLoop` 1544 → M3, `mergeRunBranchToMain` 525 → M3, `startWithHooks` 1675 → M5, `cmd main.run` 1269, `dot/parser.parse` 842, `runKeeperDoctor` 796, `runBeadSubcommandIO` 692, `queue Validate` 444, `keeper watcher.Run` 436 → P1, `handleSocketConn` 421 → M5) | ONE umbrella bead (not 11) filed listing the table, with M3/M5/P1 slices linked — **filing blocked by the operator's no-beads directive**; until lifted, §4 of `track-c-enforcement.md` + this row ARE the tracking record. Do NOT hand-edit any grandfathered function in Track C | TC-1; operator lifting no-beads directive | needs-operator-signoff |
| TC-6 | `runexec` depguard rule + coverage baseline entry (pre-authorized) | `.golangci.yml`: add the `runexec` rule from §2.2 (`deny: internal/daemon` "functional core; shell drives it", `deny: internal/workloop`; minimal allow set — deny edges are load-bearing); `coverage.baseline`: seed entry at measured value | Applied in the same M3 change that creates `internal/runexec`; package is direction-locked (cannot import daemon back) and under the 0.3pp regression gate | M3-4 (`internal/runexec` lands) | pending-design |
| TC-7 | Commit the Track C config (ownership + trigger) | `.golangci.yml`, `scripts/coverage-gate.sh`, `Makefile`, `track-c-APPLIED.md` — the uncommitted Track C session work | The applied Track C config is committed once **P1 lands / at branch handoff** — the trigger so a `/clear` or reset does not silently discard the whole session's work; currently uncommitted with no owner | TC-1…TC-4 applied | gated (trigger: P1 lands / branch handoff) |
| TC-8 | P1 author: resolve the 2 gocognit findings the new ceiling trips | `internal/replay/replay.go:157` (`Replay`, gocognit 48) + `internal/substrate/replay.go:123` (`Twin.replay`, gocognit 31) | P1 author refactors or operator-accepts both — they will trip P1's `make check-short` once the `gocognit` ceiling is enabled. Relay could not go over comms (bus offline); carried in handoff | TC-1 (gocognit enabled) | gated (P1 author) |

## Track B — Data-integrity fixes (beads-sized, direct; no kerf)

Source: `track-b-m1.md` Track B. Both are out-of-pipeline, seam-independent, daemon-OFF.

| Task ID | Title | Files/scope | Acceptance criterion | Depends on | Status |
|---|---|---|---|---|---|
| B1 | `queue.json` two-writer lost-update: route append RPC through `LockForMutation` | `internal/queue/rpc.go` — `HandlerAdapter.HandleQueueAppend` (:993), disk `Load` (:356/:374), `Persist` (:1009), `SetQueue` (:1016); `internal/daemon/queuestore_hkj808w.go` — `LockedQueueStore` (:69), `LockForMutation` (:274), no-wake set (:233–251). May need a signature tweak so pure `HandleQueueAppend` (:342) accepts a pre-loaded queue instead of doing its own disk `Load` | Append adapter does its whole read-modify-write under `lq := a.qs.LockForMutation()` — reads the live locked snapshot (not disk), `Persist`s while holding the lock, writes back via the locked view + `Wake()`. A `-race` concurrency test in `internal/daemon` (`queuestore_append_lostupdate_hkXXXXX_test.go`): goroutine A does LockForMutation→mutate-status→Persist→set while B appends via the adapter, N iterations; after both settle, `queue.json` AND the in-memory store contain BOTH A's status mutation AND B's appended items. Test must FAIL on current code (reproduces the clobber) and pass after | — | ready |
| B2 | Cat-3c subsumption false-close: genuine trailer + non-docs-diff evidence | `internal/lifecycle/orphansweepbeads.go` — `GitMergeCommitScanner.HasMergeCommitForBead` (:230–247, the `git log --grep` body-regex match), conservative-failure comment (:219–223); call sites :583/:589–595 and `internal/daemon/reconciliationcadence_rc020a.go:193` (single scanner — one fix covers both). Trailer-parse precedent: `internal/lifecycle/activerun_em031a.go:192` | Scanner (1) extracts the actual git trailer (`git log --format='%H %(trailers:key=Harmonik-Bead-ID,valueonly=true)'`) and requires exact value equality — body mentions rejected; (2) requires the matched commit's diff to touch non-docs files (`git diff-tree --name-only`, not exclusively `*.md`/docs/captain-lanes); (3) scan errors still return `(false, nil)`. Table test in `internal/lifecycle` (`orphansweepbeads_falseclose_hkXXXXX_test.go`) against a seeded temp git repo: docs-only body-mention → false; genuine trailer but `*.md`-only diff → false; genuine trailer + source-file diff → true. Verified by diff-content assertions against the seeded commits, never by the reconcile path's own "closed" status (the close path cannot be its own oracle) | — | ready |

## M1 — Test-theater keep/delete/relocate

Source: `track-b-m1.md` M1. Deletion slice only — NOT a new kerf work. Per operator-approved
**DECISIONS B5, `testing-strategy-uplift` is SUPERSEDED** (do NOT re-open it as a separate work);
**harvest its 5-layer test taxonomy INTO M1**, with M1's coverage-gate result as the baseline input.
Do NOT conflate with the `scenario` depguard violations (`hk-uyxg0`) or the event-registry
ADOPT (owned by P1).

| Task ID | Title | Files/scope | Acceptance criterion | Depends on | Status |
|---|---|---|---|---|---|
| M1-1 | Relocate `internal/specaudit` (37,646 test LOC, 132 files) to one CI lint script outside `go test` | `internal/specaudit/` (non-test surface is 9 LOC `doc.go` only; tests are markdown/spec-prose regex assertions over `specs/`), honoring the **3-file product-import carve-out** flagged in `DECISIONS.md` (files that import product code are carved out of the relocation, not bulk-moved) | Explicit allowlist of the **129** spec-prose files moved to the CI-lint script (M1 DoD 1); those 129 execute zero product code and are removed from `go test` with zero coverage lost; **the 3 product-importing files STAY under `go test`** (product-import carve-out, per DECISIONS/B3) — NOT bulk-moved; the spec-drift check survives as CI lint | — | ready |
| M1-2 | `operatornfr` keep/prune classification + delete (census Q4 / O1+O2) | `internal/operatornfr/` (1,081 non-test LOC / 13,598 test LOC, 37 test files). KEEP set (verbatim, no action): `commandcodes.go`, `exitcode.go`, `securitypolicy_on006_on026.go`, `sandboxinvariant_on024.go` + their real tests. Ambiguous set: the tautological fixture-mirror self-assert tests, plus per-file calls on `commitintegrity_on005.go` / `reviewloopstatus_on035a.go` / `upgradingmarker_on020a.go` (+ tests). Recommended defaults: O1 DELETE the self-assert tests / KEEP the four named files; O2 KEEP pending a per-file exec check — delete a file+test pair only if its test asserts constants and never drives product code (err toward keep) | Per-file keep/delete classification produced → operator ratifies the ambiguous set → deletion in reviewable batches. Every RETAINED `_test.go` package shows non-zero product coverage under `go test -coverpkg=./internal/...` (M1 DoD 2); every deleted symbol has zero remaining references, grep-clean (M1 DoD 3) | Operator sign-off on O1/O2 | needs-operator-signoff |
| M1-3 | `scenario` test-file prune (census Q4 / O3) | `internal/scenario/`: KEEP the harness — all 34 non-test files, 4,792 LOC (`asserteval`, `crashrecovery`, `orchdrive`, network-sandbox, fixture bootstrap/teardown, leak sensors — the one test-adjacent package driving real product code end-to-end). Prune among the 42 `_test.go` files (~17.3k LOC): census says keep ~11 behavioral files that exec real code; which 11 is the judgment call | One-pass behavioral-vs-theater classification of the 42 test files → operator ratifies → delete the theater in a reviewable batch. Same DoD gates as M1-2: retained packages show non-zero product coverage under `-coverpkg`; deletions grep-clean | Operator sign-off on O3 | needs-operator-signoff |
| M1-4 | Strike the event-registry path from the delete set (census DELETE verdict REVERSED — guard) | `internal/core`: `DecodePayload`, `DispatchObservational`, `DispatchSynchronous`, `ValidateEnvelopeSchemaVersion`, `pertypecompat` — live production readers in `internal/replay/replay.go` (`DecodePayloadStrict` :196, `DispatchObservational` :205, `ValidateEnvelopeSchemaVersion` :184, `LookupPayloadCompatEntry` :187), landed by P1 T4 (`5262aa48`); bench D6 `[OPERATOR-LEAN CONFIRMED]` "ADOPT … Do NOT delete"; EV-048 normative | These symbols appear in NO delete batch (M1 DoD 4); `internal/replay` references remain intact (grep verifies); the census DELETE line is explicitly retired in the M1 classification record (O5, settled) | — | ready |
| M1-5 | Audit `beadRunOne`/workloop test-net coverage BEFORE extraction (M1→M3 producer) | `internal/daemon/workloop.go` — measure current line/branch coverage of the `beadRunOne` + run-workloop test net (`go test -coverprofile`, coverpkg the run-lifecycle path) | Recorded line/branch coverage baseline for the run-lifecycle net, captured before any M3 extraction so post-extraction parity can be checked; this is the artifact the M3 phase-2 "M1→M3 coverage audit" gate consumes. Today that gate is cited with nothing producing it | — | ready |

## M3 — Run-state-machine (`beadRunOne` extraction) — PENDING DESIGN PASS (P1-gated)

Source: `.kerf/works/run-state-machine/02-components.md` (decompose pass; design DEFERRED
until P1 proves the reactor seam generalizes — the work stops at the decompose boundary).
All six tasks below are seeds for the held design pass, **not ready to start**. Phase-1 tasks
(M3-1, M3-2) can begin once P1 is proven + **M1-1 lands (specaudit relocate = honest coverage
baseline)** and do NOT need M2; among Phase-2 tasks, only **M3-4 needs M2-1 (seam contract)** —
M3-3/M3-5/M3-6 do NOT need M2 (confirm at M3 design, held) — plus the M1→M3 coverage audit (M1-5).

| Task ID | Title | Files/scope | Acceptance criterion | Depends on | Status |
|---|---|---|---|---|---|
| M3-1 | (C1) `ClockPort` in the daemon | Thread `substrate.ClockPort` (`internal/substrate/clock.go:10`) through the run-lifecycle core: the 26 direct `time.Now()` sites in `internal/daemon/workloop.go`, `agentReadyTimeout` / `postAgentReadyHangTimeout`, `noChangeTimeoutCh` (`workloop.go:4932`), resume grace (`reviewloop.go:110`). `SystemClock` in prod, `FakeClock` (`substrate/fakeclock.go`) in tests | Mechanical, behavior-preserving substitution; zero new abstraction (reuses the landed port); timeout tests become deterministic (prerequisite for M3-5). Mirrors P1 D4 | P1 seam proven; M1-1 landed (coverage baseline; independent of operator-gated M1-2/M1-3) | pending-design |
| M3-2 | (C2) Explicit merge queue — the `mergeMu` split | Replace global `mergeMu` (`workloop.go:384`) with a merge queue serialising only the ref-update critical section; the 5 `lockedMergeRunBranchToMain` call sites (`:3904`, `:4123`, `:5252`, `:5302`, `:5374`) submit to it; absorb the 2 non-merge `mergeMu` uses (remote fetch+worktree-add `:3623` region; escape-worktree check `:5162`). `worktreeCreateMu` (`:398`, hk-5qp7z) already split — C2 finishes the merge path | No daemon-wide lock held across `git rebase`/`go build`/`vet`/`git push`/`br sync` inside `mergeRunBranchToMain` (`:6544`–); a mechanically checkable lint/test fails if a lock is held across an IO call (census M3 DoD 2). Hard prerequisite for M4 (remote merges thread the queue). Design pass resolves: queue shape, true critical section (update-ref `:6821` only vs full FF-check window), fairness, hk-zguy6 escape-check preservation, `br sync --import-only` (`:7056`) / `git reset --hard` (`:7022`) in-or-out | M3-1; P1 proven + M1-1 landed | pending-design |
| M3-3 | (C3) `workLoopDeps` decomposition into ports | Break the 85-field `workLoopDeps` (`workloop.go:182`–`:942`) into minimal consumer-owned ports (keeper-D10 idiom): LedgerPort, EmitterPort, WorktreePort, MergePort (M3-2's queue), LaunchPort, ClockPort (M3-1), plus shared-by-reference concurrency handles (`runRegistry`, `localInFlight`, `agentSpawnSem`). Scope: only fields the run lifecycle touches — the rest stays for M5 | Each port extraction lands as a small, green, mechanical promotion; the M3/M5 cut line is explicit. Design pass resolves the exact field cut, the 4 loop-goroutine-owned maintenance fields (`:826`+, stay out), shared-mutable handling, nil-means-disabled fallbacks | M1→M3 coverage audit (M1-5) (Phase 2); does NOT need M2 (confirm at M3 design, held) | pending-design |
| M3-4 | (C4) The `runexec` reactor — pure `Step` state machine | New `internal/daemon/runexec` sub-package mirroring `internal/codexreactor` (`reactor.go:193`) over `substrate.Run`: explicit states `Claiming → ResolvingRun → WorktreeReady → Launching → Monitoring → Gating → Merging → {Closed \| Reopened}`; workflow-mode fork (ReviewLoop `:3825`, Dot `:4014`, single `:4221`) as early branch; timers become `ArmTimer`/`TimerFired` events (D11), dissolving `waitAgentReady` `:4798`, ProcessExit fallbacks `:5066`, `noChangeTimeoutCh` `:5331`; `beadRunOne` shrinks to a thin shell + `Effector` | Built state-by-state, each increment green (small state-transition-at-a-time PRs); behavior parity — observable event streams unchanged except the SR9 fix (parity mechanism per design pass, à la D13 differential). TC-6 depguard rule + baseline entry land in the same change | M3-3, M3-1, M2-1 (seam input/ack contract) | pending-design |
| M3-5 | (C5) Resume-hang bounded-liveness invariant + property test | Normative invariant on the reactor — daemon peer of SK-INV-005/SK-015 (SR9): every run reaching resume/relaunch (`reviewloop.go:250`, iteration ≥2 `:252`) reaches exactly one terminal outcome or emits a `run_stale`/failure signal within a bounded window — silence forbidden; every `TimerFired` edge lands in a state with an outgoing action. Replaces the fixed 2s `resumeReadyFallbackGrace` caulk (`reviewloop.go:110`). Likely rides the `internal/replay` harness (P1 T4) | Property/fault-injection test (stalled agent on relaunch, driven over `FakeClock`) asserts terminal-or-stale; plus N=10 clean relaunch cycles (census M3 DoD). This is the correctness deliverable of M3 (census STEP-0a) | M3-4, M3-1 | pending-design |
| M3-6 | (C6) Terminal-spine factoring — the 4× merge/close block | Collapse the four near-identical launch→gate→merge→close blocks (`workloop.go:3904`/`:4123`, `:5252`, `:5302`, `:5374`) into one factored spine: scenario-gate → `preMergeSync` → merge-queue submit (M3-2) → close-or-reopen; unify 50 `ReopenBead` / 27 `CloseBead` / 45 `emitDone` open-coded call sites behind reactor terminal transitions; eliminate the `runSucceeded *bool` out-param (`workloop.go:3120`) | Four copies → one spine; success is a terminal state read by the shell. Design pass resolves: exit-0 auto-close (`:5277`) genuinely redundant?, shutdown-drain `bgCtx` semantics, `emitDone` captured state (reactor state vs shell effect) | M3-4, M3-2 | pending-design |

## M2 — Agent-input-substrate — PENDING DESIGN PASS (P1-gated)

Source: `.kerf/works/agent-input-substrate/02-components.md` (decompose pass; design held
until P1 proves the reactor+replay seam — open questions are the design-pass agenda). All
seven tasks below are seeds, **not ready to start**.

| Task ID | Title | Files/scope | Acceptance criterion | Depends on | Status |
|---|---|---|---|---|---|
| M2-1 | (C1) Seam input method + ACK contract | Widen `handler.Substrate`/`SubstrateSession` with a typed input op returning a real ack ("agent accepted this input", not "tmux queued a buffer"): retire the `substrateSessionAdapter.SendInput`/`CloseStdin` no-ops (`internal/handler/substrate.go:140`, `:173`) and the type-asserted side-interfaces (`enterSender`, `paneCapturer`, `quitSender`, `paneOutputSizer`, `paneLivenessChecker`, `commandRunnerProvider` — `pasteinject.go:187/206/236/254/280/493`). Spec surface: seam contract (PL-021b/HC-054 family) + new input-protocol spec | Contract type-checks before any driver exists; depguard inversion preserved (handler declares the port, daemon supplies the driver — `substrate.go:5`). Design pass resolves: port placement, Ack contents, sync return vs awaited reactor event, the M2 bounded-liveness analog of SR9 (ack-or-stale, never silence). **Design RESOLVED (04-design/m2-1-seam-confirm.md):** port = handler-declared narrow `InputPort` (AIS-001/HC-069); Ack = binary Delivered/Rejected + Seq + optional Token (AIS-003); dual sync-return AND emitted `agent_input_acked`/`agent_input_stale` (AIS-004); bounded-liveness = AIS-INV-001/HC-INV-008. **First domino — nothing upstream; the event-model `class`-field "drift" was a false alarm (already fixed in c019).** | P1 seam proven | ready |
| M2-2 | (C2) Structured-protocol driver = the **Codex app-server driver** | Wire the **proven, subscription-compatible Codex app-server driver** as the structured input path — NOT a claude driver (Claude runs on tmux by design; a structured Claude driver needs `-p`/API key, breaks subscription-first). `codexwire`-shaped codec + `Event`/`Action` vocab + `Step` over generic `Run[E,A]` (`internal/substrate/seam.go:27`), `Effector[A]`, `ClockPort`; injected at the composition root as the Codex path, selected per agent type alongside the tmux paste path (`workloop.go:489/4346`). The whole AIS architecture was modeled off this driver; treat it as largely done | Codex driver swappable at the composition root; twin-blind (`harmonik-twin-codex`). Design pass: concrete `E`/`A` types, `ReplayCodec[E]` (P1 D2) fit, stdin ownership. **Design RESOLVED (04-design/m2-2-codex-driver.md):** E/A = `codexreactor.Event`/`Action` (no new types; read-half proven/landed), codec = existing `codexCodec`, stdin = app-server owns child stdin via JSON-RPC (real request ids → clean non-positional ack). Residuals = LiveSource + production Effector + client/stdin writer (**= M2-1's InputPort instantiated for codex**) + composition-root select (new `codex-app-server` agent type). Supersedes the claudewire `driver-design.md`. | M2-1 | design-done · gated M2-1 |
| M2-3 | (C3) Keep tmux paste as Claude's first-class input; source its ack from the **hook bridge** | tmux paste STAYS as Claude's input transport — retain `load-buffer`/`paste-buffer`/`send-keys`; retain `capture-pane` as a human observation window only. Replace the flaky heuristics (blind `Enter×3`, screen-scrape verify) with a **hook-sourced ack**: `SubmitInput` returns `Delivered`; the submission is confirmed by the hook-bridge event (`SessionStart`→`agent_ready` for start/resume, `Stop`→`outcome_emitted` for a turn) or reaches `agent_input_stale` on the bounded timeout (AIS-INV-001 — the resume-hang fix). Signal source = `specs/claude-hook-bridge.md` / `internal/hookrelay`; **pane-scraping as a signal is dropped**, transcript-tail is secondary corroboration only | Every tmux submit reaches acked-by-hook XOR stale, never silent; no pane-scrape in the ack path. Design SETTLED: `agent_ready` gates start/resume submits, `outcome_emitted` gates turn-completion submits; window bound = AIS-INV-001 (keeper's ~60s fail-open analog); `capture-pane` is human-observation-only (C4 tee is the recorded stream) | Alongside M2-2; keeper/CLI carve-outs already retain the paste verbs | ready |
| M2-3b | (C3b) Restart/handoff **done-gate** — wire "model done" to the Stop hook | Close PLAN.md problem #6's unimplemented SR4 ("wait for model done before `/clear`", today only an `.idle` pre-gate): make the keeper/restart cycle's done-gate consume **`outcome_emitted` (Stop) AND an artifact-present check** (handoff file written / fingerprint, or a `PostToolUse`-on-`Write`). Reuse the proven pattern — `hookrelay.buildStopMessage` already reads `.harmonik/review.json` on Stop for the reviewer phase. Bounded by a stale timeout | Handoff completion is detected deterministically from the hook + artifact, not the flaky `.idle`/transcript path. **Discrimination RESOLVED (OQ-AIS-006, operator-ratified):** gate = `outcome_emitted` (Stop) + `HANDOFF-<agent>.md` present-carrying-this-cycle's-nonce with `mtime ≥ NonceConfirmedAt` (reuse `pollAwaitModelDone`'s `.idle` freshness anchor, key it on Stop+artifact); no real-time pending-question discriminator (handoff turn is a `/session-handoff` injection + operator disables interactive-question behavior) — any Stop without the fresh nonce'd artifact is NOT-done and falls through to the keeper's ~60s fail-open `ModelDoneTimeout`. `PostToolUse`-on-`Write` hook + `waiting_input` veto deferred as tune-later hardening | M2-1, M2-3 | ready |
| M2-4 | (C4) Live capture tee — apptap production wiring | Splice `internal/apptap.Tap` (`InCapture`/`OutCapture`, `tap.go:48/58/63`) onto the driver's input/output stream — apptap's first production consumer (closes ROADMAP orphan `ROADMAP.md:69`). Spec surface: capture/corpus persistence + redaction | Production input stream recorded verbatim for replay; feeds M2-5's corpus (production-captured, not spike-captured). Design pass resolves: corpus persistence/rotation/budget, redaction/secret-scan (event-model precedent), shared recorder infra with the P1 "record keeper live" follow-up. **Design RESOLVED (04-design/m2-4-capture-tee.md):** splice at the Codex driver owned-stdio seam (AIS-009) via a new pipes-only `apptap.Splice` (raw wire, pre-decode); tmux `capture-pane` stays human-observation-only, never the corpus; persistence/rotation/redaction in a shared `internal/capture.Recorder` (fail-to-uncaptured, AIS-INV-002). Design self-contained; can't START until M2-2 lands the driver stdio. | M2-2, M2-3 | design-done · gated M2-2 |
| M2-5 | (C5) Replay harness + L0–L3 taxonomy + fault injection | Instantiate `Twin[E]` + `FaultConfig` (`internal/substrate/replay.go`) over the captured corpus; L0–L3 taxonomy (zero-token L0/L1/L2, env-gated live L3, drift canary), mirroring `internal/codextest` | Fault-injecting integration test: stalled-agent injection asserting **hook-acked-or-stale** (Stop/`agent_ready`, else `agent_input_stale`), never silence (`PLAN.md:197`); N-consecutive-full-runs gate with zero sleeps / zero pane-scrape in the ack path (`PLAN.md:200`). These are DoD items, not spec prose. Acceptance oracle for M2-6. Design pass resolves: which faults map to real input failures, the concrete output-or-stale oracle, corpus provenance/size, typed-decode registry reuse (P1 D6 / EV-033). **Design RESOLVED (04-design/m2-5-replay-fault-harness.md):** two-layer — Layer-B `internal/replay` Checker is the headline hook-acked-XOR-stale oracle (**event-sourced, needs only M2-3, NOT M2-4**), Layer-A `substrate.Twin[E]` covers the Codex wire path; fault matrix Stall/Drop/Truncate/Dup → `agent_input_stale` on `FakeClock`; new `internal/aistest`+`aistwin`. Prereq: register `agent_input_*` payloads (AIS-004) before Layer-B strict decode. | M2-2, M2-3, M2-4 | design-done · gated M2-2/3/4 |
| M2-6 | (C6) Retire the **flaky paste heuristics** (NOT the paste transport) | After a defined bake window (`PLAN.md:202`): remove only the unreliable scaffolding the hook-sourced ack replaces — blind `Enter×3`, screen-scrape verify, and any dead pane-scrape ack code. The tmux **paste transport** (`load-buffer`/`paste-buffer`/`send-keys`, `pasteinject.go`, the input portions of `tmuxsubstrate.go`) **STAYS** — it is Claude's first-class input path. No wholesale ~5.4k-LOC deletion | Flaky heuristics gone; the paste transport + hook-sourced ack remain and pass M2-5's oracle. Design SETTLED: dead-once-hook-ack-lands = blind `Enter×3` + screen-scrape verify + any pane-scrape ack code; the paste transport (`load-buffer`/`paste-buffer`/`send-keys`) + keeper/CLI/remote carve-outs are preserved. Final dead-set confirmed at implementation time against the landed ack | M2-5 + bake window | ready |
| M2-7 | (C7) Fold in the codex daemon-harness WAL-guard | Absorb the 380-line WAL-guard symptom-treatment (census `REPORT.md:35`; homed to M2 by `ROADMAP.md:73`) into the rebuilt input path | The guard's concern is handled by the structured protocol, not a bolt-on. Design pass resolves: what it compensates for; whether the driver's ack makes it redundant (delete) or still needed (adapt). **Design RESOLVED (04-design/m2-7-walguard-foldin.md): ADAPT, not delete** (AIS-017 confirmed) — the WAL corruption is a process-termination failure (a SIGKILL'd codex leaves a stale `-wal` that fast-fails the NEXT launch), orthogonal to input-ack so the ack can't retire it; prevention = graceful `turn/interrupt` term + typed launch-fail (M2-2), residual stale-WAL sweep demotes from per-launch to boot-time crash-recovery. **Open verify (rides M2-2): does codex checkpoint its WAL on graceful term?** | M2-2, M2-5 | design-done · verify rides M2-2 |

## M4 — Remote rebuild: approved reconciliation calls

Source: `DECISIONS.md` Group C (operator verdict 2026-07-13: C2 and C3 APPROVED on the
recommended defaults). These encode the approved decisions into the `remote-substrate-phase2`
work when its replan runs — M4 itself follows M2/M3 and hard-depends on **M3-2 (merge-queue, HARD)
+ M2-1 (seam input/ack contract)** — not all of M2, and M2-1 is not omitted — so they are gated,
not ready-now.

| Task ID | Title | Files/scope | Acceptance criterion | Depends on | Status |
|---|---|---|---|---|---|
| M4-1 | Record DEC-A supersession: M4 adopts the proven substrate seam | `remote-substrate-phase2` work (its DEC-A rejected `handler.Substrate` for a `CommandRunner`-only seam) — reversed per DECISIONS C2, operator-approved | The phase2 work's decision record marks DEC-A superseded; the M4 replan designs the remote rebuild on the proven `handler.Substrate` seam (same seam P1/M2/M3 instantiate) | M4 replan pass (follows M2/M3; hard prereqs = **M3-2 (merge-queue, HARD) + M2-1 (seam input/ack contract)**) | pending-design |
| M4-2 | Split container-isolation/egress OUT of M4 | `remote-substrate-phase2` scope: phase2 bundled container-isolation/egress; per DECISIONS C3 (operator-approved), M4 = remote rebuild only | Container-isolation/egress is removed from M4 scope and defined as its own later work; M4's replan scope statement reflects the split | M4 replan pass | pending-design |

## M5 — Daemon god-package decompose (HELD placeholder)

Net-new work (**NOT `subsystem-proofs`**); no existing owner to reconcile. No design tasks yet — held.

| Task ID | Title | Files/scope | Acceptance criterion | Depends on | Status |
|---|---|---|---|---|---|
| M5-1 | M5 daemon god-package decompose — HELD placeholder | `internal/daemon` (`startWithHooks` 1675, `handleSocketConn` 421, the ~32/45 daemon-unconstrained packages) | Un-hold trigger = **M3 first slice (merge-queue) merged** → open the M5 problem-space. Net-new (NOT `subsystem-proofs`) | M3 merge-queue slice merged | held |

---

## Blocked / gated

| Task(s) | Gate |
|---|---|
| TC-5 | Operator's **no-beads directive** — the umbrella `codename:complexity-grandfather` bead cannot be filed until lifted; this registry + `track-c-enforcement.md` §4 are the interim record |
| TC-6 | `internal/runexec` does not exist yet — lands with M3-4, pre-authorized by Track C |
| M1-2, M1-3 | **Operator sign-off on census Q4** (decisions O1/O2 for `operatornfr`, O3 for `scenario`) — a load-bearing regression test may hide in the theater; deletion is hard to reverse |
| M3-1, M3-2 (Phase 1) | **P1 must prove the reactor seam** generalizes end-to-end + **M1-1 landed** (coverage baseline; NOT the operator-gated M1-2/M1-3). Design pass held at `decompose`. Does NOT need M2 |
| M3-3 … M3-6 (Phase 2) | Above, plus the M1→M3 coverage audit (M1-5); only **M3-4** needs **M2-1 (seam contract)** — M3-3/M3-5/M3-6 do NOT need M2 (confirm at M3 design, held); internal order C3→C4→{C5, C6}, C6 also needs C2 |
| M2-1 … M2-7 | **P1 must prove the reactor+replay seam**; design pass held at `decompose`. Internal order C1→C2→{C3, C4}→C5→C6, C7 rides C2/C5 |
| M4-1, M4-2 | The M4 replan pass — M4 follows M2/M3; hard prereqs = **M3-2 (merge-queue, HARD) + M2-1 (seam input/ack contract)** (not all of M2). The decisions themselves are already operator-approved |

> **M2/M3 un-hold proof condition (checkable; re-checked by admiral on P1 merge):** "P1 proves the
> reactor seam" = **P1 lands with `internal/substrate` + `internal/replay` green AND the keeper
> vertical running on the seam (SK-INV-005 holding).** This is the gate the P1-proven M2/M3 rows above
> depend on.

---

## Deferred orphans / in-passing findings

In-passing findings homed only in ROADMAP prose; carried here with their host so they aren't lost.

| # | Finding | Host |
|---|---|---|
| 1 | JSONL rotation/retention | Track B/C hygiene |
| 2 | discarded `_ = Emit` errors + `Type`→enum + `SourceSubsystem` stamping | ride the M-phases |
| 3 | queue `HandlerAdapter` eviction | Track B |
| 4 | lifecycle-reconcile intent-log BI-031 | own small work |
| 5 | STEP-0c honest-probe guard | M4 |
| 6 | workspace event-dark instrumentation before rebuild | M4 |
| 7 | real-Claude restart integration test | P1 follow-up / M2 |
| 8 | record-keeper-live apptap recorder | M2-4 |
| 9 | pass-status-lag kerf housekeeping (advance stale passes) | housekeeping |

---

## Validation-net — KEPT IN SCOPE (default), staged before M3

`validation-net` is the concurrency safety net (`RunConcurrentMerge` fixture, N≥3 concurrent-dispatch
guard, CI run lane, 21 quarantined-E2E restore). It is named in the ROADMAP as a day-one protective net
but is **hollow**: 12 of 13 spec'd beads (incl. flagship VN4 `hk-ukhzu`) are ABSENT from the `br` DB —
only 1/13 exists. **DEFAULT (admiral, operator-informed 2026-07-13): KEEP IN SCOPE; stand up just before
M3's merge-queue work** (the concurrency-critical rewrite = when the net must be live), given the
project's concurrency-bug history. Reversible before M3 if the operator cuts it.

| Task ID | Title | Files/scope | Acceptance criterion | Depends on | Status |
|---|---|---|---|---|---|
| VN-1 | Re-file the 12 missing validation-net beads + build the net | `br` DB — re-create the 12 absent VN beads incl. flagship VN4 `hk-ukhzu`; wire the concurrent-merge fixture + N≥3 dispatch guard + CI lane; restore the 21 quarantined E2E | The 12 beads exist; the concurrency net runs in CI and is green before M3's merge-queue work lands | staged before M3 (kept-in-scope default) | gated (stand up pre-M3) |
