# Open defects found during the delete-and-rewrite program

**Why this file exists.** The bead ledger is machine-local and gitignored — beads do not travel between
clones and never reach CI. A defect that lives only as a bead is invisible to everyone but the machine
that filed it. This file is the tracked record; the bead IDs are a convenience for looking up the full
detail on the machine that has the ledger, not the handle for the thing.

**Standing directive (operator, 2026-07-28):** defects found along the way are **recorded, not chased**.
Nothing here is an instruction to stop and fix it. The point of the table below is the *third* column —
several of these are eliminated as a side effect of work already planned, and knowing which ones saves
someone fixing a thing that is about to be deleted.

Found 2026-07-29 unless noted.

**Staleness sweep, 2026-07-30.** Every checkable claim in this file was re-run against the tree at
`15bfdc1544370003a8bd619b61a380f2b034c3e3`. Corrections are inline and dated, and each says what
changed rather than deleting the old reading. Two things are worth knowing before you read on.

- **The launch-path collapse LANDED on 2026-07-29.** Seven entries that were filed as live defects
  "eliminated by planned work" are now fixed in the tree. Their beads are still open. Read that section
  as history, not as a to-do list.
- **A bead's open state is not evidence.** Five beads in this file are open against code that is
  already fixed (`hk-z4cow`, `hk-q15hi`, `hk-5sebh`, `hk-dqmw2`, `hk-j52we`), and one that this file
  said still needed closing has since been closed (`hk-zobns`). Check the code, not the ledger.

Re-confirmed as STILL TRUE on 2026-07-30, with no change needed: the stale-blocker sweep that closes on
a bare commit-message match, `GenerateSandboxProfile`'s detached doc comment, the `workloopPollInterval`
comment that describes only one of three wait shapes, the bare `"blocked"` substring in the
dependency-blocked detector, the two `"sentinel"` constants in different packages, the `handler_capabilities`
decoder mismatch, the discarded `ErrRemoteTransport` in `runAutoStatusInspection`, the Pi profile that
reaches only single mode, `useIndepSession` assigned only in the single-mode tail, the two freeze gates
with an optional declaration keyword, `make check-fast` skipping its own test step on a clean tree, the
format check reporting success when its tools are absent, and the two spec clauses that still mandate a
bare `Refs:` grep as a completion test.

---

## RESOLVED by the launch-path collapse — verified 2026-07-30 against HEAD

> **Status change, 2026-07-30.** This section used to be headed "Eliminated by planned work — do not fix
> these directly", and every entry pointed forward at a collapse that had not happened. Every item in it
> is now FIXED. The launch-path collapse landed on 2026-07-29 across seven commits, from `6077a24dc`
> (collapse plus the cognition gate) through `30b5cf02c` (config as the only sandbox switch) — the full
> list is `6077a24dc`, `ea96471b3`, `18d59aded`, `159b68e78`, `0743e4dec`, `8346edc2f` and `30b5cf02c`.
> `internal/daemon/agentlaunch.go` `runAgentLaunch` is now the single agent-launch path. Its three
> callers — `internal/daemon/workloop.go` `beadRunOne`, `internal/daemon/dot_cascade_core.go` and
> `internal/daemon/dot_gate.go` — build the spec and then hand it to that one function.
> `scripts/readywait-freeze-gate.sh` now allows exactly one `runloop.DispatchSegment` in the tree, in
> that file. Every entry below was re-checked in the code and all seven are closed. The kept text is the
> original filing, because the reasoning is the reusable part.
>
> **Live launch-site count is TWO, not three.** The line above counts three *callers*, which is right.
> Only two of them can execute. The gate does not belong in a live count: `daemon.Config.CPRegistry`
> has zero assignments anywhere in the tree, tests included, and no shipped or project graph declares a
> `type="gate"` node — so `executeCognitionGate` cannot run. Prior counts on this line read five, then
> three. Live, it is two.
>
> **Earlier count correction, 2026-07-29.** These two were measured when there were **five**
> agent-launch sites. Deleting `reviewloop.go` removed two of them, which made the count **three**.
> The collapse then made it one launch function.
>
> Both beads are still OPEN in the ledger as of 2026-07-30 (`hk-z4cow`, `hk-q15hi`). The code is fixed
> and the ledger has not caught up. Do not read the open state as live work.

| What was wrong | Resolution | Bead |
|---|---|---|
| **The credential guard covered 1 of 3 dispatch sites, and not the default one.** `d2RemoteAPIKeyRefusal` ran only on the single-mode path. DOT is the default and carries essentially all traffic, so the 2026-05-30 credential-leak gate protected the path that has run twice ever and not the path everything uses. The conformance test repaired 2026-07-28 guarded the guard's *shape*, not its *coverage* — which is why nothing caught this. | **FIXED by the collapse, and closed by construction.** `runAgentLaunch` calls `d2RemoteAPIKeyRefusal` once, on the final spawn environment, ahead of the only launch, so every launch asks it. The conformance test in `internal/daemon/conformance_m4c7_test.go` was re-anchored on the collapsed shape and passes. | `hk-z4cow` |
| **Launch-failure classification was computed by every site and consumed by none.** `classifyLaunchFailure` maps a launch error onto structural event classes. `dispatchSegmentRun.emit` still switches on two other event types and drops both structural classes into `default:`. Sites then hand-rolled the same check themselves, and at least one emitted nothing. | **FIXED by the collapse.** There is now one production `OnLaunchFailed` hook, in `runAgentLaunch`, and it emits both structural diagnostics. `classifyLaunchFailure` is now private to `runloop.DispatchSegment` with one caller and still fills the `EvLaunchFailed` reason, and `runexec`'s `EvLaunchFailed` handler emits `launchFailedEventType(ev.Reason)`, so the class reaches the log. The classifier and the hook duplicate one test between them — that is a tidy-up, not a coverage hole. ⚠ **New, smaller defect in its place:** `launchFailedEventType` returns `spawn_cap_blocked` for every reason except `tmux_new_window_timeout`, so a plain launch error is reported as a spawn-cap block. Unbeaded — record it before fixing. ⚠ **Open disagreement:** the 2026-07-30 per-finding recount in `ONE-OF-N-DRIFT.md` §(c) scores the matching finding **N1 as still OPEN**, on the grounds that `classifyLaunchFailure` has no consumer. Both readings were written the same day. Re-derive this one row before acting on it. | `hk-q15hi` |

**The five that rode along — found 2026-07-29 while mapping the three sites. All five are FIXED by the
same collapse, confirmed 2026-07-30.** Each one is a guard that existed on one path and was simply
absent on another, with the compiler silent throughout. They are closed because the step that carried
them now exists once. `agentlaunch.go` carries a `NORMALIZED (was: …)` comment beside each fix, so the
old behaviour is still readable at the seam.

- **A cognition gate's agent-ready signal carried no run id**, so the stale-run watcher skipped it, the
  "never spawned" flag never flipped, and the reaper stayed armed for the whole run. FIXED: the shared
  agent-ready callback emits `agent_ready` through `EmitWithRunID` with a full
  `core.AgentReadyPayload`, for every site.
- **A gate launch that failed reported no reason.** The gate set both classifier errors and then emitted
  neither, so the operator saw a failed launch with no cause. FIXED: the shared `OnLaunchFailed` emits
  `EmitSpawnCapBlocked` or `EmitTmuxNewWindowTimeout` on every path.
- **Single-mode runs never disarmed the never-spawned reaper.** Only the graph-node path armed the
  proof, so a codex or pi run in single mode was killed around the thirty-minute mark while healthy.
  FIXED: `newCapturedSpawnProof` is armed unconditionally in the shared path.
- **The agent-ready-timeout event reported the wrong number on remote runs.** All three sites passed the
  *local* configured timeout to a parameter that means the *effective* one. FIXED: `runAgentLaunch`
  resolves `runlaunch.EffectiveAgentReadyTimeout` once and reports the same value it arms.
- **Two of the three paths emitted that timeout on a cancellable context**, which is the context the
  reaper has already cancelled by the time the emission runs. FIXED: the one emission uses
  `context.Background()` with a `//nolint:contextcheck` naming the reason.

**Not fixed by the collapse, and still live:** the Pi provider profile still reaches only the
single-mode launch context. See the `hk-yo9g6` row below.

## RESOLVED by the review-loop retirement — verified 2026-07-29 on `e46658ed5`

Both were cases where `runReviewLoop` was the **sole** non-test home of a behaviour, so deleting the file
would have removed it from the product entirely — and neither would have failed to compile. Third
instance of the pattern that already cost this program the crew idle-reap tests and the D2 conformance
test. Both are fixed in the code; kept here because the *reasoning* is the reusable part.

**Ledger correction, 2026-07-30.** An earlier version of this line said "Both are now closed out". That
is false. `hk-5sebh` and `hk-dqmw2` are both still OPEN in the bead ledger. The code changes landed; the
beads did not follow. Read the open state as ledger lag, not as live work.

| What was wrong | Resolution | Bead |
|---|---|---|
| **Crash-recovery resume worked only in review-loop mode.** `persistClaudeSessionID` was review-loop-only; single-mode and DOT both captured a Claude session id and dropped it. | **Consciously retired, with evidence** — the durable write had no reader anywhere (nothing reads `context.json` back, nothing consumes the persisted event, EM-031 recovery reads branch-tip trailers instead), and the resume that *did* work read an in-memory field, not the persisted copy. So the machinery was deleted rather than ported. No production symbol survives; only comments reference it. Re-checked 2026-07-30: `internal/daemon/sessioncontext_chb023.go` is gone and `persistClaudeSessionID` has no reference in any `.go` file. Note that `specs/execution-model.md` still describes the symbol as live under EM-012, so the spec text lags the tree. | `hk-5sebh` |
| **The default mode treated every merge failure as terminal.** `Retryable: runmerge.IsRetryableReason` was passed to the terminal spine only by the review-loop, and a DOT failure never charged the retry budget, so the close-with-needs-attention ladder could not fire on the default mode. | **Ported to DOT.** Both now ride the DOT arm of the terminal spine in `beadRunOne` — `Retryable: runmerge.IsRetryableReason` and the `Budget.ChargeReviewLoopFailure` ladder. Single-mode still carries neither. **Correction 2026-07-30:** the reason given here was "single mode is scheduled for deletion; if that sequencing changes, this reopens", and no plan document schedules it. The honest reason is that single mode has run twice in 2,153 runs, so the gap costs nothing today. It closes when single-shot becomes a graph (DECOMPOSITION-MAP §Step 7). | `hk-dqmw2` |

## RESOLVED by the sandbox-gate consolidation — 2026-07-29

**Ledger note, 2026-07-30.** `hk-j52we` is still OPEN in the bead ledger. The code landed on 2026-07-29
at `30b5cf02c`. The open state is ledger lag, not live work. `sandboxSpawnForRun` is confirmed as the
one gate, in `internal/daemon/sandboxgate.go`, asked once per launch from `runAgentLaunch`. UNVERIFIED
in this pass: the live config values quoted in the row below (backend `srt`, harnesses `[pi]`). They
were a dated measurement and this pass could not read the project config file.

| What was wrong | Resolution | Bead |
|---|---|---|
| **The sandbox gate never wrapped claude nodes on the default graph path.** Single mode computed the sandbox spawn unconditionally, covering both the substrate and the exec path. The graph-node path computed it only on the captured-session-id branch, so a claude node on the substrate path was never wrapped even with the backend configured and claude listed. Gates were never sandboxed at all. | **Consolidated, with the verification deferred by operator direction.** Measuring first is what made this cheap: against the live config (backend `srt`, harnesses `[pi]`) the divergence changed nothing in production, because pi captures its session id and was already sandboxed on the graph path, and claude mints its own and is not listed. So the per-site scope is gone. Every launch asks `sandboxSpawnForRun` and `sandbox.harnesses` is the only switch. Three mutation-checked tests guard it. The one live behaviour change is the build-cache redirect now reaching pi graph nodes, which is the deferred check — `NEXT_STEPS.md` item F, which also records what adding a harness to that list now costs | `hk-j52we` |

## Needs deliberate attention — nothing planned will fix these

| What is wrong | Notes | Bead |
|---|---|---|
| **A guard accuses innocent runs of escaping their worktree, and kills them.** Originally filed as a lost-commit race; **root-caused 2026-07-29 and it is neither a race nor a lost commit.** Full detail below — **still the most serious item in this file.** | Fix direction identified; `RSM-018` must be corrected in the same change | `hk-co8g8` |
| **A wave-group queue stalls for 5 minutes** whenever its lowest-indexed pending item is claimed by a sibling queue, while its other items sit ready. Self-heals, so it presents as "the queue was slow." Reaches production. Detail below. | Missing fallback, not a tuning problem | `hk-nown4` |
| **The registered bus decoder for `handler_capabilities` cannot decode what production emits** — wrong field key and `[]string` vs `[]int`. The wire path works (a different decoder agrees); it is the registered core payload type that is wrong, which breaks strict-decode replay verification. | Independent | `hk-b882r` |
| **A flapping SSH makes the remote C2 gate pass.** `runAutoStatusInspection` discards `ErrRemoteTransport`, the sentinel that exists specifically to distinguish "SSH failed, inconclusive" from "confirmed absent". The sibling reader in the same package explicitly retries on it. On the **kept** graph path — survives all Phase 3 deletions. Was the only genuine bug among 152 delta-lint findings. | Independent fix | `hk-sbd4l` |
| **`TestScenario_ConcurrentMultiQueue_N2_HappyPath` is red, and the failure signature has changed.** It was filed as `error_category=structural / sub_reason=protocol_mismatch` on a deterministic dispatch ordinal — not load — failing 4/4. **Re-measured 2026-07-30: still red, but the mismatch is gone.** The test now times out at `MustCompleteWithin` after 60 s with two `run_started` events and no `protocol_mismatch` anywhere in the event log. So the recorded root cause no longer matches the observed failure and must be re-derived. It is listed as a known flake in `plans/2026-07-13-code-revamp/RT12-acceptance-evidence.md` "Bucket A", wrongly — that list is a document, not a mechanical gate. | Second confirmed case of a known-flake list absorbing a real defect. The failure shape needs a fresh diagnosis | `hk-t2d7n` |
| **DOT runs cannot be adopted after a daemon restart.** `useIndepSession` is declared before the mode switch in `internal/daemon/workloop.go` `beadRunOne` but assigned only in the single-mode tail, so the shared worktree-cleanup defer's guard can only be false on that one path. DOT runs lose their worktree on shutdown. Re-checked 2026-07-30: still true. (Text corrected 2026-07-30: the entry used to read "Non-single-mode … Review-loop and DOT". Review-loop is deleted, so the whole population of affected runs is now DOT — that is, everything.) **Promoted in importance:** this is the one capability that makes "express single-shot as a graph and delete the tail" more than a mechanical move | Needs the terminal spine collapsed — a *second* step after the launch-path collapse, which landed 2026-07-29 and did not touch this | `hk-mh3qy` |
| **The Pi provider profile never reaches graph nodes or cognition gates.** The single-mode launch context carries the provider, key-env, key-file, base-URL and API fields resolved from the Pi profile; the graph-node and gate launch contexts set none of them. A Pi-harness node under the default mode is launched without its profile. **Re-verified 2026-07-30 and still true:** `resolvedProfile` is resolved once in `beadRunOne` and read only by the single-mode `shared.LaunchCtx`. `driveDotWorkflow` is not passed it. Put concretely, `workloop.go` sets `Provider`, `APIKeyEnv`, `APIKeyFile` and `BaseURL` from the resolved profile, and `dot_cascade_core.go` and `dot_gate.go` set none of them. The MODEL does reach the graph, because `resolvedModel` is overwritten from the profile before the mode switch, which is why this looks half-wired. **This is the largest live defect on the default path in this file.** | Found while mapping the launch sites. **Not** fixed by the launch-path collapse — spec construction stays at the call site by design, so this needs its own change | `hk-yo9g6` |
| **The graph path runs the wrong harness's commit fallback for Pi.** `dispatchDotAgenticNode` calls `codex.EnsureRefsTrailer` for every `CompletionProcessExit` harness, and Pi is one. The single-mode tail branches on `core.AgentTypePi` and calls `pi.EnsureRefsTrailer`. Behaviour is the same — both wrappers call the same `internal/harness/shared/refstrailer.go` primitives — so the only wrong thing is the commit message: a Pi node's daemon-fallback commit on the DEFAULT path says `feat(codex): codex turn output`. Found 2026-07-30. This is finding N6 in `ONE-OF-N-DRIFT.md`, which the 2026-07-30 recount scores as still open. | Cosmetic today, and a trap tomorrow: the two wrappers are free to diverge and the compiler will not say so. Fix with the post-exit collapse (§Step 7 piece 1) | unbeaded |
| **An entire tier of test failures is invisible.** `go test -tags scenario ./internal/daemon/` yielded eight failures where the untagged run yielded one. UNVERIFIED as of 2026-07-30 by the staleness sweep: that sweep did not re-measure the eight-versus-one count, because a whole-package daemon run leaks daemon processes and clears the shared build cache. A separate 2026-07-30 pass did run the tier and recorded its result — read `NEXT_STEPS.md` §5.2, which holds the per-test disposition and is the more recent reading. Root cause established: no assessment ever ran the tagged tier at all — the recipes only `go vet`ed it and the CI workflow carried `continue-on-error: true`. | **The reporting half is FIXED 2026-07-29** — the flag is removed, so the tier is red and visible. §5.2's hardening remains, and half the tier still skips in CI (`hk-ynohn`) | `hk-97gcz` |
| **Two freeze gates match call sites, not declarations.** `scripts/runloop-freeze-gate.sh` and `scripts/queuewiring-freeze-gate.sh` make the declaration keyword optional in their regex, so a bare call to a watched symbol reads as a declaration. Re-checked 2026-07-30: still true in both. `queuewiring` makes all four of `func`/`var`/`const`/`type` optional in its one scan. `runloop` makes `type`/`var`/`const` optional in its type scan; its separate function scan already requires `func`, so only the type scan is exposed. Measured: a first draft of the new scheduler gate copied that pattern and raised **six false failures against a correct tree**. The two siblings have not fired only because nothing yet calls their symbols bare at line start. | A gate that cries wolf gets disabled, so this is worse than it looks. Fix: require the keyword at column 0, plus a second scan for the grouped `const (` / `type (` form. `workloop-scheduler-freeze-gate.sh` does both and is the model | `hk-freeze-gate-callsite-regex-uemrd` |
| **A reporting flag hid 20 straight real failures on every REST surface.** `continue-on-error` was documented in two files as masking only the *run* conclusion, leaving the step conclusion honest — and the nightly ops-monitor probe was built on that. False: run, step, and check-runs conclusions all read `success` after an exit 2. Only the annotations API told the truth, so the alert could never fire. | **FIXED 2026-07-29.** Flag removed, probe works unchanged, and the false comments are corrected in `scenario.yml`, `nightly-race.yml` and `ops-monitor-check.sh` | `hk-21v7c` |
| **The release-ready prompt ignores its own 30-minute cooldown.** `test/exploratory/ops_monitor_check_test.sh` test 27e seeds the signal as alerted 6 minutes ago and expects suppression. It is sent anyway. Root cause **not** established — either it is matched against the 5-minute critical cooldown instead of the 30-minute one, or the cooldown key stops matching because the seeded key embeds the commit count. Two red assertions that were stepped over and untracked until 2026-07-29, which is the same shape as the masked tier above. | Pre-existing. Re-run 2026-07-30: **314 passed, 2 failed**, and the two failures are still exactly 27e's. The earlier reading of 313 passed was correct when written and has since gained one assertion | `hk-release-due-cooldown-chujb` |
| **`main`'s only required status check has failed on every run since 2026-07-17.** Branch protection requires exactly one check, `check (Tier 2)` from `ci.yml`. Five consecutive failures, latest 2026-07-22. This was invisible because the ops-monitor health probe read *the newest run of any workflow* on main — which is the Scenario tier reporting a masked success every day. | Surfaced 2026-07-29 by filtering that probe to the required check. `release_due` is gated on a green CI status, so it now correctly refuses to fire — do **not** widen the probe again to make it green. Re-measured 2026-07-30: branch protection still requires exactly `check (Tier 2)`, the last green run on main was 2026-07-17, and five failures have followed it, the latest still 2026-07-22 | `hk-main-required-check-red-i14hq` |
| **About half the scenario tier skips in CI, and a skip reads as a pass.** Nothing installs `br` and nothing declares a twin build. Evidence: `internal/daemon` takes 69s in CI against 368s locally, and `TestThroughput_TenBeadsAtMaxFour` fails locally every time yet has never failed in 20 CI runs. | Means a **green** run on that workflow proves much less than it appears to — do not read one as the tier passing | `hk-ynohn` |

## The format check reports success when its tools are absent — found 2026-07-29

`scripts/go-format.sh` resolves its two tools with `gofumpt=${GOFUMPT:-"$repo_root/.tools/gofumpt"}` and
never checks that the path exists. Under `set -euo pipefail` a missing tool exits **127 with zero bytes
on stdout**, and bash writes its diagnostic to stderr only. Empty stdout is byte-identical to a clean
pass, so `bash scripts/go-format.sh check | tail -5` reports success in any caller that does not set
`pipefail`.

Reproduced directly: `GOFUMPT=/nonexistent/gofumpt bash scripts/go-format.sh check` exits 127 and prints
nothing to stdout. The same command piped through `tail` exits 0. Re-reproduced 2026-07-30 — the exit
code and the empty stdout are unchanged.

**Why it matters more than it looks.** `.tools/` is gitignored, so a fresh clone and every agent worktree
starts without it. Agents in this program run the format check and report "format check passed" from the
piped output. Three separate agents did so today in worktrees that had no `.tools/` at all. `make
fmt-check` and CI are unaffected — neither pipes, and CI runs `make tools` first — so this hides only
from the agents who report it most often.

Fix is a path-existence check that fails with a named error. **Until then, treat any bare "format check
passed" as unverified unless the exit code is shown.** This is the fourth green-signal-protecting-nothing
in this file, after the masked scenario tier, the `continue-on-error` flag that masked it, and
`make check-fast` skipping its own test step on a clean tree.

**A second way to mis-read the same script, found 2026-07-30, corrected 2026-07-30.** The script is
bash, not POSIX shell. An earlier version of this entry said it "carries no `#!/usr/bin/env bash`
protection". That is false — line 1 of `scripts/go-format.sh` IS `#!/usr/bin/env bash`. A shebang does
not help here, because it is read only when the file is executed directly. Naming it as a missing
shebang points the reader at the wrong repair. The real behaviour is unchanged: `sh scripts/go-format.sh
check` ignores the shebang, dies on a bash-only construct, and exits **2** with a syntax error on
stderr. That failure is loud rather than silent, so it is the milder sibling of the defect above, but
the non-zero exit has nothing to do with formatting. An agent that reads only the exit code concludes
the tree is badly formatted when the real fault is the interpreter. Invoke it with `bash`, or execute it
directly.

**This section was re-confirmed on 2026-07-30 rather than extended.** An agent independently
"discovered" the stdout-versus-stderr behaviour already written above and reported it as a correction.
It was not one. Recorded because the re-discovery is itself the evidence that the trap is easy to hit
twice.

---

## Found 2026-07-29 while harvesting the three `workloop.go` comment blocks

None of these was chased. The first one is a live correctness defect and the most serious item in this
section. The rest are the same shape as the decay the harvest itself found: a pointer that was right
when it was written.

- **A stale-blocker sweep closes a bead on a bare commit-message match, with no evidence the work is
  present.** `autoCloseStaleBlockersOnClaimFailure` in `internal/daemon/scheduler.go` reaches
  `shared.MainHistoryHasRefsTrailer` for each candidate blocker and, on a match, closes that blocker
  through `SweepCloseBead`. Nothing else is checked. **This is the same false-close shape as the
  incident that removed the pre-dispatch check** (`hk-f38n`: bead `hk-cmry` closed wrongly, remaining
  work refiled as `hk-zmpd`), and it is live in the daemon today.
  Measured across all four production call sites of that primitive. Three pair the match with evidence
  that the work is genuinely absent, and only close when both agree: the no-change timeout in the run
  driver waits on `noChangeTimeoutCh`, `noCommitGuardShouldReopen` requires `curHeadSHA == parentSHA`,
  and the graph cascade requires `postHeadSHA == preHeadSHA`. This fourth site pairs it with nothing.
  It is worse than merely unpaired: the three good sites gather evidence about the same bead they then
  close, while this one reads the *dependent* bead's status and then closes a *different* bead — the
  blocker — about which it has no signal at all.
  It also directly contradicts the godoc now written on the primitive, which tells every caller to pair
  the match with work-absence evidence and never to use it as a standalone completion test. **Not
  fixed here:** `scheduler.go` belongs to another piece of work, and chasing defects is against the
  standing directive. This wants its own change.
- **`GenerateSandboxProfile` has no doc comment.** Its 19-line comment block in
  `internal/daemon/sandboxprofile.go` is separated from the function by the `worldSharedTempRoot`
  helper, so Go attaches it to nothing and the exported function godocs as bare. The comment holds the
  full `allowWrite` inventory and the world-shared-root rejection rule, so this is the most valuable
  detached comment in the file. Moving the helper above the comment fixes it.
  **Still true on 2026-07-30**, proven by `go doc ./internal/daemon GenerateSandboxProfile`, which
  prints the bare signature and no prose.
- **FIXED 2026-07-30. Both `hk-l5saf` comments in `internal/daemon/scheduler.go` cited stale line
  numbers.** The hoisted guard comment cited "~line 1818" for the Step-2 split gate and "~line 3072" for
  the `localInFlight` increment. The post-stamp "no guard here" comment cited "~line 3072" as well. All
  three were `workloop.go` positions and none survived the Seam A split. Two unrelated "~line 1954"
  citations in the same file had the same problem. **All of them are gone.** The commit that made the
  dispatch admission order data rather than source position (`0b7857c1b`, 2026-07-30) removed every
  `~line` citation from the file, and a grep for `~line` in `internal/daemon/scheduler.go` now returns
  nothing. Kept as history: this was the exact failure the "cite symbols, not line numbers" convention
  exists to stop, and it appeared within one day of the split.
- **`specs/execution-model.md` EM-063 Phase 2 and EM-064 tier 2 mandate a completion test that is
  known to produce false positives.** Both require `git log --grep "Refs: <bead_id>"` and read a match
  as "already landed" — EM-063 in the daemon's eager-refill pre-screen, EM-064 in the orchestrator's
  guard before it submits. `hk-f38n` measured that a bead worked in several parts leaves an older
  partial commit carrying the same ID, so the match fires while work is outstanding. The dispatch-time
  use of that grep was removed for exactly this reason. These two were never revisited. Recorded as a
  spec-drift item, not fixed: narrowing a normative test is an execution-model amendment and needs
  adjudication. Flagged in the new informative note under BI-022 in `specs/beads-integration.md` §4.7.
- **`make check-fast` runs no tests at all on a clean tree, and reads green.** Its final step derives
  the package list from `git diff --name-only HEAD`, so after a commit the list is empty and the step
  prints "no changed Go packages, skipping go test" and exits 0. The repo's own instruction is to run
  that gate *after* committing, which is precisely when the test step does nothing. So a green
  `check-fast` on a committed tree proves the build, the linters and the freeze gates, and proves
  nothing whatever about tests. Run `go test -short` directly against the packages you touched.
  This is the same pattern the program keeps finding — a green signal protecting nothing — and it is
  the third instance recorded in this file after the masked scenario tier and the `continue-on-error`
  reporting flag.

## RESOLVED — the P0 that was probably wrong, settled 2026-07-30

**`hk-zobns` is invalid as written and the coverage it stood for is restored.** The product was
correct all along. The diagnosis below held up under a third measurement. What changed on 2026-07-30
is that the reasoning became executable, so nobody has to re-derive it a fourth time.

**The recorded fix — "put the bead's own target in the protected set" — does not work, and the reason
is the useful part.** That fix assumed the early landing gate and the deep merge guard could be made
to disagree. They cannot. The work loop resolves ONE value, the per-bead landing branch, and hands the
same value and the same protected list to both gates. Both compare by exact string. So protecting the
bead's target makes the EARLY gate refuse, no worktree is ever cut, and the deep guard is still never
reached — a green test asserting the wrong guard, which is the same loss of coverage in a shape that
looks fixed. **No work-loop fixture exists in which the early gate passes and the deep guard refuses.**
For a cross-repo run the loop skips the early gate and also empties the protected list, so neither
fires.

**So the backstop is now asserted where it lives**, by a direct call to the merge entry point with a
protected target over a real worktree — which is the shape the original issue asked for and which no
test in the repo had ever done. It pins the refusal reason, that the result is a refusal rather than a
no-change short-circuit, and that both branches, both remote refs, both reflogs and the run-branch tip
are byte-for-byte unchanged. A companion test pins the landing-branch measurement the whole argument
rests on, so if the per-bead landing rule ever reverts, that test fails and says to rewrite the
backstop test as a full work-loop run.

Verified by mutation: neutering the protected-branch check leaves `go build` and `go vet` green and
turns the test red on five assertions — and the merge genuinely runs, moving `refs/heads/main` and
`origin/main`. The test drives the real merge path, not a stub.

**One caveat, and it is not small.** `internal/daemon/branchguard_test.go` is behind the `scenario`
build tag, so this restored assertion does NOT run in the default short gate. It runs only in the
scenario tier, which this program treats as red and does not gate on. **The backstop is asserted but
nobody is watching the tier that asserts it.** That is a weaker outcome than "covered" and should be
read that way. The build tag was re-checked on 2026-07-30 and is still there.

**Correction, 2026-07-30 — the two sibling failures are gone.** An earlier version of this paragraph
said `TestBranchGuard_TargetBranchMergeIsolation` and `TestBranchGuard_FailClosed_TargetInProtectSet`
"fail there today, both timing out at about 30 seconds". They now PASS. `go test -tags scenario
./internal/daemon/ -run '^TestBranchGuard_TargetBranchMergeIsolation$|^TestBranchGuard_FailClosed_TargetInProtectSet$'`
is green in 4.6 seconds, and `TestBranchGuard_FailClosed_MergeGuardBackstop` is green in 1.1 seconds.
The 30-second timeouts were the low-disk gate described in the disk section below, which held every
tick before the loop reached the thing under test. The disk was reclaimed on 2026-07-30 and the
timeouts went with it.

**RESOLVED, 2026-07-30. The bead was closed as invalid.** An earlier version of this line said the
close was the owner's call and was "named here rather than done". It has since been done: `hk-zobns` is
CLOSED, dated 2026-07-30, with the invalid-reason text carrying this same argument.

---

## The original diagnosis, kept because the resolution above is a response to it

`hk-zobns` — *"Branch-protection deep guard fails open: bead merges to protected target and closes
approved."* Re-measured 2026-07-29: **the guard did not fail open.** The ref that moved was the
*unprotected* `integration` branch that the test's own bead body asks to land on; every assertion about
`main`, `origin/main` and main's reflog passed. The test's premise went stale on 2026-07-06 (`hk-lgykq`)
when merge-target resolution moved to the per-bead `lands_on` and became **stricter**.

The real cost is coverage, not safety: this was the only work-loop exercise of that backstop, so the
backstop has been unasserted for roughly three weeks. Annotated on the bead rather than re-scoped —
changing a P0's priority is the owner's call.

**Re-confirmed 2026-07-29 (late), and a warning about how to read the failure.** The repro was run again
and it does fail deterministically in about 4.4 seconds. Reading only the failure output leads to the
wrong conclusion — it says the `integration` ref moved, the bead closed, and the outcome was `approved`,
which reads exactly like a fail-open. The fixture settles it, and the test states it in its own setup
comments: the bead body sets `target_branch: integration`, the comment beside it says integration is
**NOT protected**, and the protected set passed to the run is `["main"]` alone. So the daemon merged to
an unprotected branch that the bead asked for. That is correct behavior.

The stale half is the next comment: *"the merge call uses deps.targetBranch (main), so the deep guard
fires."* Per-bead `lands_on` resolution replaced the daemon-wide target, so the merge targets
`integration` and the guard correctly does not fire. **Do not "fix" the product against this test.** Fix
the fixture — put the bead's own target in the protected set — or the backstop stays unasserted.

---

---

## The two that were root-caused, in detail

Both were diagnosed 2026-07-29. **In both cases the symptom in the original filing was misleading**, and
the real defect was worse. That is worth noting as a pattern in itself: the first-order reading of a
failing test in this codebase has now been wrong three times running.

### `hk-co8g8` — the escape guard fires on a sibling's half-finished merge

**Not a merge race. No commit is lost.** The losing run is killed *before* it ever attempts its merge;
its commit sits on `refs/heads/run/<id>` and simply never becomes an ancestor of the target. That
exonerates the whole merge apparatus — `resolveMergeTips`, the fast-forward re-validation, the CAS
rollback, the retry budget, the worktree lifecycle. None of them run for the losing bead.

`beadRunOne`'s single-mode tail runs `runmerge.CheckMainWorkingTreeDirty` — a bare
`git status --porcelain` on the project root — inside an escape-check slot in the merge exclusion domain.
Both the call site and the function's own doc claim that domain makes the check race-free.
**That invariant is dead.** `runmerge.RunBranchToTarget` splits the commit phase: Phase A
`commitAdvanceRef` (inside the domain) → **domain released** → Phase B `gitPushOrigin` (outside) → Phase C
`commitFinalizeWorkingTree` (inside). Between A and C the ref has advanced but the tree has not been
refreshed, so every merged path reads dirty — and the domain is free. A sibling's escape-check lands in
that gap and reports *the other bead's file* as this run's escape.

Proven by intervention, not inference: widening the window (300 ms before push) takes losses from 1 to 3
and all three losers name the same mid-push sibling's file; closing it (moving push and finalize inside
Phase A) passes 4/4 at N=5. Minimal repro is N=4 on shipped code with no instrumentation.

**Production exposure is larger than the test's**, because Phase B there is a network push to GitHub —
hundreds of milliseconds to seconds, against single-digit milliseconds locally. A hit reopens valid
reviewed work, re-runs the agent from scratch, and emits a durable event **accusing an innocent run**.
Past `implementer_escaped_worktree` events under concurrent dispatch should be re-read as suspect.

**This was predicted and refused, then done anyway.** The M3 design pass (`M3-D5`) said splitting these
"regresses the escape invariant"; the merge-queue design flagged it as an open reviewer challenge; the
relocation's own design doc said to keep the ref-advance *and* the tree reset inside. The implementation
split them, and `M4-C5` answered only the rollback half. The commit that deleted the old path-exclusion
heuristic *also pre-registered the fallback for exactly this contingency*.

**`RSM-018` in `specs/run-state-machine.md` is now unsatisfiable as written** — it mandates exclusion
against an interval that is no longer a critical section. It must be corrected in whatever change fixes
this. Fix direction: reinstate path exclusion with a **blob compare** against the pre-merge tip, which is
the pre-registered fallback and does not relitigate the push relocation.

No existing test can catch it: the concurrent-merge test binds a merge mutex that suppresses the window,
and the nearest regression test models the pre-split atomic sequence.

### `hk-nown4` — head-of-line blocking stalls a queue for five minutes

A sibling queue wins a duplicate-bead race → this queue's pre-claim guard sees `in_progress` and arms a
**five-minute** cooldown, deferring the item → the deferral is immediately reversed on the next tick
because the item has no blocking sibling → it lands back at index 0 → `SelectNextQueue` only ever offers
`Eligible[0]` → the cooldown guard `continue`s **with no fallback to the next eligible item**.

So the queue blocks on the one item it will refuse for five minutes and never looks at the items behind
it. It self-heals, which is why it reads as "the queue was slow" rather than as a stall. The same
`continue`-without-fallback shape also guards the greenlight gate.

The cooldown that made this a five-minute stall (rather than the previous 2.5-second spin) landed
**three weeks after** the test that exposes it was written — the test's 60-second budget is 5× too short.
**Do not fix this by shortening the cooldown**; that reverts a deliberate fix instead of supplying the
missing fallback.

### Six line-number citations inside `runWorkLoop` pointed at nothing — FIXED 2026-07-30

**Status, 2026-07-30: all six citations are gone.** The commit that made the dispatch admission order
data rather than source position (`0b7857c1b`) removed them. A grep for `~line` in
`internal/daemon/scheduler.go` now returns nothing. The record below is kept because the lesson is the
reusable part, and because two `~line` citations still live elsewhere in the package — one in
`internal/daemon/workloop.go` beside the `SelectWorker` pre-reservation, and one in
`internal/daemon/remote_completion_misfire_repro_test.go`. Neither was re-checked for accuracy in this
pass.

Found while pinning the admission-gate order (§3 Step 3 prerequisite). The comments in
`internal/daemon/scheduler.go` `runWorkLoop` cited six approximate line numbers, and **all six were
wrong**. The Seam A split moved the loop into a new file and every number stayed behind:

| The comment says | Where the thing is now |
|---|---|
| `beadRecord` construction "below (line ~1658)" | the `beadRecord = core.BeadRecord{` assignment |
| the post-claim `ShowBead` "at ~line 1954" (twice) | the queue-path label-hydration `ShowBead` |
| "The Step-2 split gate (~line 1818)" | the Step-2 split capacity gate |
| `localInFlight` "increment at ~line 3072" (twice) | `deps.localInFlight.Add(1)` |

Every one landed past the end of the function or in unrelated code. This is the exact rot the repo's
cite-symbols rule exists to stop, and the guidance it produced was actively misleading: the hoisted
local-cap guard's safety argument rested on "localInFlight is not incremented until ~line 3072", so a
reader who checked that line found no increment and could not verify the claim. Cite the symbol.

### The admission-order constraints that no test pins, and why

`internal/daemon/admissionorder_test.go` now pins constraints 1, 2, 5 and 7 from DECOMPOSITION-MAP §3, the
second clause of 3, and half of 4 and 8. Constraint 9 has no real constraint to pin. What is left is
recorded here so the next reader does not spend the same time discovering it. **The numbers are the
plan's numbers.** Keep them aligned.

**This section carries no headline count, on purpose.** Three successive rounds of review found the count
wrong — once too low, twice stale after the list beneath it was corrected. A count is the one line most
likely to be quoted and the least likely to be re-derived, and it can disagree with the list two
paragraphs below it. A list cannot disagree with itself. So the state of each constraint is stated once,
where that constraint is discussed, and nowhere else. The map in
`internal/daemon/admissionorder_test.go` follows the same rule and is the other authority.

**Incompletely pinned: 4, 6 and 8.** Each is below. Constraint 3 is complete, and the story of how it was
nearly missed is worth keeping, because it is the reason this section stopped carrying a count.

An earlier version of this section said constraint 3 was fully pinned by
`l5saf_localonly_strand_test.go`, "re-checked for gaps and none found." That is wrong, because constraint
3 has TWO clauses and that test covers one:

- The guard's POSITION relative to the Phase-3 stamp. Pinned by that test.
- **`localInFlight` must not be incremented before the guard.** This is the hoist's own safety argument —
  the source comment says the pre-stamp read stays below `gateMax` through dispatch *because* the
  increment is post-claim. `l5saf` structurally cannot see it: it preloads the counter to `gateMax` with
  `gateMax = 1`, so the guard reads `1 >= 1`. Hoist `deps.localInFlight.Add(1)` above the guard and it
  reads `2 >= 1` — the SAME branch. The item stays pending and the test stays green. Its fixture sits on
  the saturated side of the boundary, so it cannot see the boundary move. Only two test files touch that
  seam and only `l5saf` drives the loop, so nothing in the tree pinned it.

  **Now pinned** by `TestAdmissionOrder_LocalCapGuardReadsThePreIncrementCount`, which sits one slot
  BELOW the cap, where the increment's position changes the answer.

The lesson is the same one §5 keeps teaching: a multi-clause constraint summarized as one line reads as
covered. Count the clauses, not the constraints.

**Constraint 4 — the write-lock hold is not observable through `runWorkLoop`.** The source comment says
the dedup check must run while the write lock is held so the winning queue's stamp is visible. Selection
and stamping run on ONE goroutine for the daemon's whole life, so two queues can never reach the stamp at
the same time whatever the lock does. The lock defends the stamp against the per-run goroutines that write
item status through `evaluateGroupAdvanceWithOutcome`, and no seam lets a test interleave one of those
with the stamp. The *outcome* is pinned; the lock boundary is not.

**Constraint 6 — CLOSED. `governor.tick` before the sentinel-queue gate is now pinned.** The gate is
`loopMaintenance.sentinelBlocksDispatch`, which calls `movementGovernor.dispatchBlocked` and returns false
on a nil governor. `newMovementGovernorIfEnabled` builds one only when the `movement_governor` subsystem is
enabled AND `workLoopDeps.governorState` is non-nil. `governorState` had no field on `WorkLoopDepsParams`,
so no external test could construct a loop in which this gate could fire.

*What closed it:* one field, `WorkLoopDepsParams.GovernorState`, wired straight through to
`workLoopDeps.governorState`. Nil keeps the subsystem absent, so every existing fixture is unchanged.
`SentinelMode` was **not** added and was not needed — the mode stays at the production default (observe)
and the trip is injected through `DecisionBlocker.AddQueueBlock("sentinel", …)`, which is the same
in-memory state a real ACT-mode trip writes. The finding above was right: the governor has to exist, not
to trip.

The tests are in `internal/daemon/sentinelgate_test.go`, built on the `admissionorder_test.go` fixture:

- the gate holds a bead on the **queue** dispatch path, and the same fixture claims with no trip pending;
- the gate holds a bead on the **br-ready** path (the gate is written out twice, so deleting either copy
  compiles clean and un-gates one path);
- with the subsystem **absent** the same block does not gate dispatch, which is the "off does not get to
  hold the dispatcher shut" property `movementgovernor.go` documents;
- **the ordering clause itself**: a trip armed INSIDE `governor.tick` gates the SAME tick. The trip is
  armed through `brAdapter.Ready`, which `governorGatherInput` is the only caller of once `NoAutoPull` is
  set, so the arming happens at a point strictly inside `governor.tick`. Mutating
  `sentinelBlocksDispatch` to serve a snapshot taken at the TOP of `tickBeforeSelect` produces exactly one
  claim instead of zero — the one-extra-bead hazard described below, now executable.

*And one finding that shrinks the stake. This is a closed-world enumeration, not a survey of the
neighborhood.* The writer set for the sentinel block is provably complete:

- `DecisionBlocker`'s mutators are exactly `AddBeadBlock`, `AddQueueBlock` and `Acknowledge`.
- **No interface in the repo declares any of them.** The field is always the concrete
  `decisionBlocker *DecisionBlocker`, so there is no structural back door — nothing can substitute another
  implementation.
- The only non-test callers are `movementGovernor.onTrip` and `onClear`, both on the dispatch goroutine,
  plus boot-time `loadOneAckFile`. `LoadDecisionAckState` has exactly one caller, in `bootsocket.go`, and
  there is **no reload path**.
- `internal/sentinel` cannot reach `DecisionBlocker` at all: `internal/daemon` imports `internal/sentinel`,
  so the reverse import would be a cycle. That is why `sentinelAckRecord` duplicates the on-disk shape and
  the subject constant is spelled again in the daemon package — its own comment says so.

So in steady state there is one writer, on this goroutine, and nothing between the maintenance pass and
the gate writes the sentinel subject (the intervening work is the queue snapshot, the bootstrap, the
deferred re-evaluation, selection, the cooldown, handler-pause and decision-required).

**But note WHERE that writer sits, because it changes the conclusion's shape.** `governor.tick` is the LAST
statement of `tickBeforeSelect`. So a snapshot is equivalent to the live read only if it is taken AFTER
that call. Taken at the top of the pass, the daemon dispatches one extra bead on the tick a trip first
fires — the same one-tick-late hazard `loopmaintenance.go` already documents for `halt`, and documents as
deliberate there. The conclusion holds for the natural implementation, and it holds because of where the
snapshot sits, NOT because ordering is irrelevant.

`dispatchBlocked`'s own comment already says converting it to a snapshot is "arguable on its merits, not
obviously wrong". That still holds, and the test does not contradict it: what the test pins is the
one-tick edge, not a preference between a live read and a snapshot. A snapshot taken AFTER `governor.tick`
keeps the test green. A snapshot taken before it does not.

**Constraint 8 — half pinned, and the earlier reading of it was WRONG. Corrected here.**
An earlier version of this section claimed there were two no-sleep sites, that both drive the item
terminal first, that a merged variant would be "slower, never wrong", and that only a wall-clock flake
could test it. Every one of those four claims is false. Re-derived by classifying every outer-loop
`continue` statement in `runWorkLoop`:

> **Count re-measured 2026-07-30. The numbers below moved.** The classification was written against 31
> outer-loop `continue` statements, five of which did not wait. `runWorkLoop` now holds **28** outer-loop
> `continue` statements: **4** that do not wait and **24** that do. Two commits on 2026-07-30 caused the
> change. The one that made the dispatch admission order data rather than source position (`0b7857c1b`)
> reshaped the ordering, and the one that made the dispatch stamp a single durable write (`b029f9ce1`)
> **merged the cross-queue-duplicate site and the `hk-6pspu` max-attempts stamp site into one**
> `reservationItemFailed` branch. It also added a new waiting site, `reservationWriteFailed`. The
> reasoning below survives the change; only the tally moved. The per-site list is corrected inline.

- **There are FOUR no-sleep sites, not two, and there were five before 2026-07-30:** the queue
  bootstrap, the `hk-pina9` pre-claim `ShowBead` bound, the reservation-item-failed branch, and the
  `hk-n91y0` claim-blocked path. Twenty-four sites wait. The reservation-item-failed branch is the merge
  of what used to be two separate sites, the cross-queue duplicate and the `hk-6pspu` max-attempts
  **stamp** bound. The plan's own §3 item 8 says "twelve sites sleep" and names three no-sleep sites, so
  it undercounts on both sides.

  **`hk-6pspu` tags TWO sites** — the queue-path stamp bound, which does not sleep, and the br-ready skip
  bound, which does. Keep the word "stamp" or the bead tag alone points at both.

- **"24 sites sleep one poll interval" is loose, and the shape it hides is the one a merge would flatten.**
  One statement — the `continue` taken when `selectNextQueue` selects nothing — carries **three wait
  shapes**:

  1. a 2-second `workloopSleep` when deferred items remain;
  2. a 2-second wait that ALSO selects on the schedule wake channel, when an enabled scheduled job is
     loaded;
  3. `workloopIdleWait` with no timer at all, when neither holds.

  Shape 2 is bounded by a flat `time.After(workloopPollInterval)` — the same 2 seconds as shape 1, with no
  next-fire-time arithmetic. **Never write that it "waits until the next scheduled job time."** It does
  not compute one. Shape 3 is untimed but wake-interruptible: the daemon parks, it does not stall, and
  putting a timer there would restore the busy-poll PL-013 forbids.

  By TIMEOUT semantics there are only two shapes. Three appears only when you count select shape, and
  shape 2 is the one that disappears silently if a merge keeps only the timeout: a scheduled job would
  then wait for a queue-submit wake instead of its own channel.

  That same branch has a fourth outcome that is not a wait at all. When ZERO queues are loaded it falls
  through with neither a wait nor a `continue`, which is how the br-ready fallback is reached — and what
  the empty queue store in `TestAdmissionOrder_ReadyPathBoundsAttemptsBeforeHandlerPause` relies on.

  So across the whole loop there are **four distinct delay outcomes** once immediate-continue is counted,
  not two.
- **The queue-bootstrap site does not terminalize anything** — its items stay pending. It is still sound,
  but for a different reason: that tick writes group→active, which flips its own `hasActiveGroup`
  predicate, so the next pass sees the active group and dispatches. The "they all drive the item
  terminal" reasoning does not cover it.
- **"Slower, never wrong" holds only for merging toward the SLEEPING variant.** Merging the other way
  busy-spins the `hk-403fw` cooldown — the exact `bead_claim_skipped` storm the cooldown was added to
  stop. The direction has to be stated or the conclusion is not usable.
- **It is not even slower in that direction, and a deterministic test exists.** Every no-sleep site
  except the bootstrap reaches its `continue` through `evaluateGroupAdvanceWithOutcome`, which calls
  `queueStore.Wake()` unconditionally on the not-all-succeeded branch. `workloopSleep` selects on that
  same channel, so a sleep there returns at once: merging those costs ZERO latency. This read "four of
  the five" before the 2026-07-30 stamp merge cut the no-sleep set to four; it is now three of the four.
  The bootstrap site is the one
  exception — no `Wake()` fires there, so merging it would cost one poll interval on every queue submit.
  `WakeCh()` is exported, which makes the token a non-blocking-receive observable with no wall clock in
  it. `TestAdmissionOrder_TerminalDedupLeavesAWakeTokenPending` now asserts it.

What remains unpinned is only the busy-spin direction, and no test can reach a code shape that does not
exist in the tree.

### Three more found while classifying the loop's wait shapes

All pre-existing, none fixed, all recorded because each is cheap to trip over and expensive to diagnose.

**`workloopPollInterval`'s own comment is now false.** It says the constant "is NOT used for queue-loaded
idle states, which block indefinitely via `workloopIdleWait` per PL-013". Wait shapes 1 and 2 above are
queue-loaded idle states and both use exactly this constant. The comment describes shape 3 and presents it
as the only case. A reader who trusts it will conclude the daemon never re-polls with a queue loaded, which
is wrong for two of the three shapes, and PL-013 is cited in support of the wrong scope.

**The dependency-blocked detector is a bare substring match.** `runWorkLoop` decides whether a claim
failure means "the bead has open dependencies" with
`strings.Contains(claimErrStr, "cannot claim blocked issue") || strings.Contains(claimErrStr, "blocked")`.
The second clause subsumes the first, so the specific phrase is dead code, and ANY error text containing
the word "blocked" anywhere silently changes behavior: the queue item is driven terminal through
`evaluateGroupAdvanceWithOutcome` instead of being reverted to pending and retried. A `br` wording change,
a wrapped network error, or a path with "blocked" in it is enough. This is the movement-governor shape from
§5 — a coarse signal read as intent — applied to an error string. `internal/daemon/admissionorder_test.go`
dodges it deliberately and its fake error carries a DO-NOT-SIMPLIFY note, because every test there that
counts claims across ticks depends on the retry path.

**Enabling the FIRST scheduled job does not wake a parked daemon.** In wait shape 3 the loop blocks on
`workloopIdleWait`, which selects only on the queue-submit wake and shutdown — not on the schedule wake
channel. Shape 3 is reached precisely when no enabled job exists, so the job that would move the loop to
shape 2 is the one that cannot announce itself.

**The mechanism matters, and a first draft of this entry got it wrong.** `harmonik schedule enable` does
NOT fail to signal: `runScheduleEnableDisable` calls `Store.SetEnabled`, which routes through
`Store.mutate`, which calls `signalWake` — as eight store methods do. The signal is real. It just cannot
arrive, because **the CLI is a separate process, so its wake lands on its own in-memory channel and never
on the daemon's.** `WakeCh`'s own doc already hedges with "when the daemon shares the in-memory store",
which a separate CLI process does not. Do not record this as a missing call.

So the CLI's own header claim — "a running daemon
reloads the file on its next tick and picks up the change within one poll interval" — is false in this
state: there is no next tick until a queue submit or a restart. Once one enabled job exists the loop sits
in shape 2 and later edits are picked up within 2 seconds, so this bites exactly once per daemon life, at
the moment an operator first arms a schedule. `WakeCh`'s own doc already hedges with "when the daemon
shares the in-memory store", which the CLI does not.

### The string `"sentinel"` names two different things, and one of them is a real queue

Latent, not live. Recorded because it is cheap to trip over and expensive to diagnose.

`internal/sentinel/adversary.go` sets `AdversaryQueueName = "sentinel"` — the named queue the adversary
crew member binds to, so a queue with that literal name really exists once ACT mode spawns one.
`internal/daemon/decision_block_ev043a.go` sets `sentinelSubjectIDACT = "sentinel"` — the decision-block
SUBJECT the dispatch gate asks about. Two constants, two packages, one string, two unrelated meanings.

They live in different maps today, so nothing is broken: the dashboard forcing gate's `blockedQueues` is
keyed on queue NAME and consumed by `selectNextQueue`, while `IsQueueBlocked` is keyed on subject ID.
**No code writes across them.** The hazard is that both key spaces are `map[string]…` over the same
literal, so any future code that reads one with a key from the other silently type-checks. The concrete
shape to watch: write the decision-block subject into `blockedQueues` and the adversary's own queue is
withheld from dispatch — the sentinel would gag the crew it just spawned.

Related and worth knowing: this duplication exists because `internal/sentinel` cannot import
`internal/daemon` without a cycle, which is the same reason `sentinelAckRecord` re-declares the on-disk
ack shape. So the fix is not "share the constant" — there is nowhere shared to put it that does not mean
moving one of the two. Leave it until something needs it.

---

## The nightly race signal cannot pass, and it hides two real failures — found 2026-07-30

**The nightly race job has failed every run in retained history — 22 nights, back to 2026-07-09.** A
previous handoff reported it as failing "since at least 2026-07-26" and noted no earlier handoff
mentioned it. Both parts understate it. There is no successful run to compare against.
Re-measured 2026-07-30: still 22 failures and no successes in retained history. GitHub ages the oldest
run off as each new night lands, so the tally stays flat while the window slides.

**It cannot pass, and the reason is structural rather than a bug in the product.** The job runs
`make check-race-full`, which is `go test -race -count=1 ./...` across the whole module. The module
contains `evaltasks/`, a set of thirteen fixture packages that hold DELIBERATE bugs — they are the
inputs to bug-fixing evaluations. One of them states its own defect in a source comment
(`evaltasks/eval-bugfix-rate-limiter/limiter.go`, the token-bucket initial-token count, tagged
`BUG(off-by-one)`).

**This is why the failure is invisible to the gating tier.** Those fixture tests skip under
`testing.Short()`. CI Tier 2 passes `-short`, so it never sees them, and `go test -short ./evaltasks/...`
is green here on all thirteen. The nightly job deliberately drops `-short` in order to surface races
that the gating tier's parallelism cap suppresses. Dropping `-short` is exactly what un-skips the
planted bugs. So the one job designed to catch real races is the only job that runs the code designed
to fail.

**Two failures were recorded as sitting behind that permanent red**, in product packages, not fixtures:

- `TestDaemonWatchdog_PhantomReviveGuard` in `internal/supervise`.
- `TestWM040a_OrderingSettingsBeforeWorkspaceLeased` in `internal/workspace`.

**Correction, 2026-07-30 — neither reproduces.** Both were re-run with `-race -count=1`, first alone by
exact name and then as part of their whole package. All four runs are green. `internal/supervise` is
fully green as a package in 40 seconds. `internal/workspace` fails as a package, but on a DIFFERENT
test, `TestWM001_GitWorktreeAddProducesCanonicalPathAndBranch`, which dies on a segmentation fault from
`git rev-parse HEAD` — that looks like a property of the agent worktree it was run in, not of the
product, and it was not chased. So the two named failures should be treated as unreproduced until
someone shows them again from the real nightly job. The rest of the entry stands.

The point of the entry is that nobody could have seen them: a signal that
is always red carries no information, so it stopped being read. Four data-race warnings also appear in
the fixture package and are presumably planted too, but that was not confirmed.

**Not fixed, per the standing directive.** When it is fixed, the shape is to scope the race target off
`./...` rather than to change the fixtures — the fixtures are correct as they are, and their bugs are
the product. Note that `make check` and `check-short` share the `./...` spelling, so whatever excludes
`evaltasks/` should be checked against every tier that walks the module, not the nightly one alone.

---

## The daemon package is not flaky. This machine is out of disk. Measured 2026-07-30

**This corrects a belief the program has been acting on for days, and the correction goes both ways:
tests that fail here are not environmental, and at least one test that PASSES here is asserting
nothing.**

**The standing claim.** The record has said the full `internal/daemon` package is "already red and
flaky here — about 18 baseline failures, needing real `br`, git remotes and tmux", and that the fix is
to gate on a narrow deterministic subset instead. Every part of that except the failure count is
wrong.

**The measurement.** Three runs of `go test -short ./internal/daemon/`, same box, same commit, one
variable each:

| Variant | Top-level failures | Wall clock |
|---|---|---|
| Real disk reading (low), real cache reap | 23 | — |
| Real disk reading (low), cache reap stubbed out | **23** | 297 s |
| Disk reading reports healthy, nothing else changed | **0** | 84 s |

The cache reap is not the cause — the count is identical with it disabled. The low disk reading is
the whole cause. **The package is GREEN when the disk reading is healthy**, and it is also three and a
half times faster, because the low-disk gate makes every tick sleep a poll interval.

**The mechanism.** The dispatch loop holds a tick when the maintenance pass reports disk below the
watermark. That gate sits BEFORE queue selection. A fixture that does not stub the disk reading gets
its tick held before the loop ever reaches the thing the test is about. At the time of the measurement
this box had about 6.4 GiB free against a 10 GiB watermark, so the gate was armed for every such
fixture. It now reports 58 GiB free, well above the watermark, so the gate is not armed today.

**The dangerous half. `TestL5saf_LocalOnlyItemNotStrandedByCapGuard` was VACUOUS on this box, and it
is one of the two tests this program had been using as its deterministic gate.** Proven by deleting
the guard it exists to protect: the test stayed GREEN with the guard entirely removed. Adding the
one-line disk stub, with the guard still removed, turns it RED. So the guard position that
`DECOMPOSITION-MAP.md` constraint 3 records as PINNED was not pinned here at all. The newer
admission-order tests were unaffected — they already stub the reading, which is why they kept their
kill power and why only the older test rotted.

**Exposure.** 32 of the daemon test files that drive the work loop do not stub the disk reading.
Re-measured 2026-07-30: **37** test files under `internal/daemon` reference `WorkLoopDepsParams` and
**5** of them set `DiskFreeBytesFunc`, so the unstubbed count is still 32. The denominator was 36 when
this was written and is now 37 — one file was added and it stubs.
One file is proven vacuous. The rest are not individually verified and should not be assumed either way —
some will fail loudly, some may assert nothing. The failure mode is silent in the direction that
matters.

**DONE, and confirmed on the real tree.** Disk was reclaimed on 2026-07-30 and the package was then
run unmutated, with the real disk reading: **`go test -short ./internal/daemon/` passes in 103
seconds.** Not a subset, not a stub — the whole package, green. Treat `internal/daemon` as a usable
gate again.

*Not re-run in the 2026-07-30 staleness sweep*, because a whole-package daemon run leaks daemon
processes and clears the shared build cache. The precondition still holds: the box reports 58 GiB free
against a 10 GiB watermark, well clear of the gate. Two narrow readings taken instead both agree with
the DONE claim — the scenario-tagged branch-guard tests that used to time out at 30 seconds now finish
in under 5, and the admission-order and sentinel-gate tests are green.

The reclaim itself is worth recording, because `docs/disk-reclaim.md` §0 points at the shared Go
caches and on this box that was **not** where the space was. `~/Library/Caches/go-build` held 7 MiB,
because the daemon's own low-disk reap had already emptied it — the runbook's number-one source is
self-clearing exactly when the problem is worst. The space was 14 GiB of agent session scratchpads
under `/private/tmp/claude-502/`, mostly per-session Go build caches and mutation-test copies of the
tree. Two stale session directories from 2026-07-28 gave back 9.5 GiB and took the box from 5.2 GiB to
14 GiB. **The runbook should promote agent scratchpads above the shared Go caches**, or at least say
that a tiny `go-build` reading is evidence the reap already ran rather than evidence of a clean box.

**Still open:** stubbing the disk reading in the remaining 32 fixtures. That is the only part that
survives a future low-disk machine, and it is 32 files of churn. Tracked as `hk-q2r9q`.

**`WorkLoopDepsParams` exposes no watermark field**, so a fixture cannot lower the floor. The disk
reading function is the only lever, which is why omitting it is silently fatal.

**This is the fifth green-signal-protecting-nothing in this file** — after the masked scenario tier,
the `continue-on-error` flag that masked it, `make check-fast` skipping its own test step, and the
format check passing when its tools are absent. It is the worst of the five, because the other four
hide a missing check and this one also manufactured a false story about the environment that shaped
how the whole program tests.

---

## The test tiers report on themselves, and four of those reports are wrong — found 2026-07-30

Found while working out what a live-daemon phase would cover. **All four are the same shape as the
`continue-on-error` defect above: a gate that reports a result it did not produce.** None is chased.

| What is wrong | Notes |
|---|---|
| **The per-commit scenario gate allows the merge when it cannot compile the tests.** `classifyScenarioGateError` in `internal/runloop/scenariogate.go` returns `warn("compile-fail")`, and warn means ALLOW. Observed live: `scenario-gate: WARNING: could not produce a verdict (compile-fail) … — ALLOWING merge (fail-open, hk-ur428)`. Fail-open on *flake* is a deliberate and defensible choice. Fail-open on *does not build* is not the same decision, and it was not made separately. | The gate is loudest exactly when it is least informative. Splitting compile-fail out of the flake path is a small change |
| **The crash-recovery tier and the integration tier are empty stubs.** `test/crash/crash_stub.go` and `test/integration/integration_stub.go` are 7-line build-tag package docs holding **zero test functions**, so `check-full`'s `go test -tags=crash ./test/crash/...` runs nothing and exits 0. Crash, SIGKILL-mid-merge and restart recovery have no home. | A named tier that runs nothing is worse than an absent one, because `check-full` lists it as covered |
| **Seven scenario-tagged tests are never run by the tier that names them.** `make test-scenario` covers `./test/scenario/...` and `./internal/daemon/...`, but `//go:build scenario` files also exist in `cmd/harmonik` (4 tests), `internal/sentinel` (2), `internal/keeper` (1) and `internal/runloop` (1). **Two fail when invoked directly** — a missing keeper hook script, and a warn-cooldown that fires twice inside its own window. | Either the target's package list or the tag is wrong. Both are one line |
| **`make test-scenario` states a build precondition it does not satisfy.** Its comment says `build-all` compiles `harmonik-twin-claude` "so scenario tests can locate them without a rebuild". `build-twin-claude` is an alias to `build-twin-generic`, which writes `twins/generic-twin` — a different name in a different directory — and carries a live `TODO(hk-w5vra.2)` saying the real twin build has not shipped. Seven tests skip on the missing binaries, **locally as well as in CI**, and `twin-fail` / `twin-hang` are built by nothing anywhere in the tree. | This is why "the twins are built" reads as settled. Detail and the two-resolver problem are in `NEXT_STEPS.md` §5.1a |

**One environmental row, because it is a one-line fix that has been open a while:**
`TestScenario_RemoteSubstrate_Localhost_DOT_E2E` fails under `make test-scenario` because macOS
`TMPDIR` puts `daemon.sock` at 130 bytes against the 104-byte `sun_path` limit. `check-short` pins
`TMPDIR=/tmp` and `test-scenario` does not. `NEXT_STEPS.md` §5.2 lists this as fixed; the fix landed in
the wrong target.

---

## The pattern worth carrying forward

Most of the launch-path items above are instances of one shape: **several code paths perform the same
conceptual step, and only one of them actually applies it.** The full catalogue — seventeen instances,
with a step-by-path matrix — is in `ONE-OF-N-DRIFT.md`.

(This sentence used to read "six of the eleven items above". The count went stale the moment entries were
added above it, which is the same failure the admission-order section now avoids by carrying no count.
`ONE-OF-N-DRIFT.md` owns the number.) The reason it matters for this program is that
the compiler is silent on every one of them: the callers still compile, the tests still pass, and the
guard simply stops being applied on four paths out of five.

That was the argument for the launch-path collapse being the highest-value move, and the collapse
LANDED on 2026-07-29. It did not fix the instances one at a time. It made the class unable to recur on
the launch path, because one path cannot drift from itself.

**Updated 2026-07-30. The argument now points somewhere else.** The prediction held: the seven
launch-site items above are closed by construction, and a CI gate holds the invariant.
`ONE-OF-N-DRIFT.md` records which of its own seventeen findings that removed and which it did not. The
mode-driver tails still hold ten and need the terminal spine collapsed as a separate step. The
sub-workflow walker needs a third. The same argument applies unchanged to the step that was
deliberately left out of scope — "decide whether the agent did the work". That step is still written
twice, once in the single-mode tail of `beadRunOne` and once in `dispatchDotAgenticNode`, and four of
its sub-steps already differ, with the default path holding the weaker half. The highest-value
remaining move is therefore the same move on that step. DECOMPOSITION-MAP §Step 7 states it.
