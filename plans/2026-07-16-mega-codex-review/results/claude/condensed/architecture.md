# Condensed Review — Architecture Rot / Over-Abstraction / Maintainability Debt

Lens: architecture rot, over-abstraction, god-functions/structs, coupling, unmaintainable/sloppy/garbage code, maintainability debt. Deduped and ranked most-severe first. Source reviews cited as (RU-xx).

---

## Ranked real issues

### A. Dead parallel implementations — whole subsystems that duplicate live code

1. **HIGH — Entire `internal/workflowvalidator` package is a second, fully-dead DOT parser + validator** — `internal/workflowvalidator/validator.go` (+`dotparser.go`, ~2000 lines of tests) (RU-15). A complete second implementation of the workflow-graph pipeline (own DOT parser, own Tarjan SCC, own BFS reachability, own EM-038 ruleset) with **zero** production/CLI symbol references — everything went through `internal/workflow/dot`. The migration bead landed but the old package was never deleted. *Failure:* ~2000 lines of green tests give false coverage impression; any maintainer editing graph rules can waste effort on / drift the dead copy. **The prime over-abstraction/rot finding.**

2. **HIGH — Entire `internal/operatornfr` production API is unwired** — `internal/operatornfr/exitcode.go` and all package exports (RU-20). No product code imports the package; the only external references are one code comment and two test string-literals. Exit-code taxonomy, upgrade-integrity gate, sandbox path check, and upgrade-marker I/O are all enforced elsewhere (or nowhere) — this is a parallel spec-mirror. *Failure:* the real daemon can diverge from this taxonomy and every test here still passes green — it proves conformance of code no one runs.

3. **HIGH — `internal/scenario` matrix-expansion + leak-sensor + crash-recovery are never invoked by the real runner** (RU-22, three findings):
   - `matrixexpand.go` `ExpandMatrix` (325 impl + 685 test lines) — the production runner (`cmd/harmonik/harness.go:356`) ranges over raw scenarios and never expands. A `matrix:` YAML with `{{.param}}` placeholders runs once **unsubstituted**. SH-030/005/007 unreached in production.
   - `postsuiteleaksensor.go` `CheckPostSuiteLeaks` (~360 lines, 3 platform variants) — spec says MUST be called before `WriteSuiteResult`; runner never calls it. Leaked run processes / held leases / open fds silently undetected.
   - `crashrecovery.go` fixture types + `canonicalInvariantsFor` (296 lines) — no runner path arms a `CrashPoint` or checks an invariant; harness advertises crash-recovery conformance it cannot execute.

### B. God-functions / god-structs — unreviewable units

4. **HIGH — `runWorkLoop` is a ~1560-line god-function** — `internal/daemon/workloop.go:1531-3097` (RU-01a). One lexical scope interleaves ≥12 concerns (merge-queue ownership, claim-semaphore, schedule ticks, coordinator reap, disk watermark, split gate, dashboard gate, two sentinel governor blocks, cross-queue round-robin lock dance, pause/decision/greenlight gates duplicated across queue+br-ready paths, spawn). Nesting 6-7 deep; dozens of `continue`s each after a sleep. *Failure:* trivially easy to leak a held `LockForMutation`/`lq.Done()` or skip a state reset on a new branch; invariants cannot be held in one head.

5. **HIGH — `beadRunOne` is a self-admitted god-function** — `internal/daemon/workloop.go:3121-past 4100` (RU-01a). Carries `//nolint:gocognit,cyclop,funlen` promising an "M5 reactorization" that has not landed, so the nolint is load-bearing debt. Every guard path duplicates a reopen-bead-and-return idiom with `reopenTID, _ :=` (discarded error).

6. **HIGH — `driveDotWorkflow` is a ~1000-line god-function with an overlapping merge-decision exemption lattice** — `internal/daemon/dot_cascade.go:~499-747` (RU-02). One for-loop with ~15 entangled state vars; the no-progress block stacks ≥5 completion/suppression exemptions (each keyed on the same `committedResult`/`priorVerdict`/`lastGatePassed`/`isReviewer` predicates), guarded only by prose comments citing bead IDs. *Failure:* each branch independently decides whether valid committed work is MERGED or STRANDED; any edit can silently merge un-reviewed work or strand approved work. **Highest maintainability risk in its scope.**

7. **MEDIUM — `dispatchDotAgenticNode` mixes 5+ concerns in a ~450-line, 20-parameter function** — `internal/daemon/dot_cascade.go` (RU-02). Harness resolution + sandbox engagement + stdout interception + paste-inject + watchdog wiring inline. The 20-arg signature is duplicated verbatim into `sub_workflow_runner.go` and partly re-implemented in `dot_gate.go`. *Failure:* one cross-cutting change requires editing three near-identical launch bodies — the prime driver of copy-paste drift across reviewloop/dot_cascade/dot_gate.

8. **MEDIUM — `Watcher.Run` is a ~450-line god-function; `WatcherConfig`/`CyclerConfig` are ~60-field god-structs** — `internal/keeper/watcher.go:965-1416` (RU-13). ~17 loop-local mutable state vars, deeply nested hard-ceiling gating; config structs mix policy scalars, injectable fn seams, and named ports. State is closure-local so it cannot be unit-tested in isolation; load-bearing ordering ("MaybeRun before RunForPrecompact") is a prose comment, not enforced. The pure reactor (`step.go`) is clean; the shell side never got the same treatment.

9. **MEDIUM — `reconcileThreeWay` is a ~315-line god-function with 4 interleaved mismatch classes** — `internal/lifecycle/startup_pl005_qm002.go` (RU-12). 5+ nesting levels; the identical `ReconciliationMismatchObservedPayload` build+marshal+append appears **five times**. Class-C-vs-A′ dispatch is an inverted `!isPendingLike`/`!isQueueTerminal` branch. *Failure:* any new mismatch class or payload-field change must be edited in five near-identical places.

10. **MEDIUM — `run()` in main.go is a ~700-line god-function with ~40 sequential `os.Args[1]==` branches** — `cmd/harmonik/main.go:148-923` (RU-16a). No dispatch table; verbs can't be enumerated; an earlier branch can shadow a later one; every subcommand re-implements the same `--project` flag loop. *Failure:* already produced the shipped-but-unwired confirm/veto-verdict commands.

11. **MEDIUM — `RunState`/`run.go` is a fragile god-module whose correctness is "byte-exact strings preserved as data"** — `internal/runexec/run.go` (RU-03). ~15 tightly-coupled latch fields; core behavior is preserving exact human-readable summary strings by scattered concatenation, decipherable only via inline `RF :NNNN` line anchors and spec IDs that rot on every edit. *Failure:* a label typo silently changes an observable outcome string with no compile-time protection.

12. **NIT→MEDIUM — `codexSession` is a large god-struct mixing five concerns under four overlapping mutexes** — `internal/codexdriver/session.go` (RU-07). phaseMu/mu/submitMu/outcomeMu + multiple `sync.Once` + five lifecycle channels; the concurrency contract lives in prose, not encoded. Maintainability hazard, easy to regress.

13. **MEDIUM — `Config` god-struct interleaves production surface with test-injection seams** — `internal/daemon/daemon.go:581` (RU-06). `daemonTestHooks` was introduced explicitly to hold test-only seams, yet `Config` still carries `Runner`, `WorkerRegistryObserver`, `QueueStore`, and a long tail of `Skip*` flags documented as "solely for unit tests." A ~50-field composition-root where a production caller can't tell which fields it must set — defeats the stated rationale for `daemonTestHooks`.

### C. Copy-paste duplication / over-duplication

14. **MEDIUM — review-loop and DOT drivers duplicate the entire no-progress / HEAD-advancement / feedback-write machinery** — `internal/daemon/reviewloop.go` vs `dot_cascade.go` (RU-02). Two separate ~2000-line copies re-implement HEAD-advancement no-progress signal, diff-hash payload, session-id interceptor, no-commit guard, reviewer-feedback write, verdict threading. Every fix lands twice (bead comments show hk-togxq/m1wqp/wixms/fxy9 applied in both) and they can drift (enum vs free-form summary; different REMOTE feedback handling).

15. **MEDIUM — Five near-identical emit pipelines copy-pasted in the bus** — `internal/eventbus/busimpl.go:412/540/661/751/858` (RU-11). `Emit`/`EmitWithRunID`/`EmitAgentMessage`/`EmitAgentPresence`/`EmitTyped` each re-implement the same 6-step redact→marshal→append→fan-out pipeline; the ConsumerClass fan-out switch is duplicated verbatim five times. *Failure:* any change to redaction/envelope-stamping/dead-letter/async-dispatch must hit five sites; `EmitWithRunID` already uniquely tracks `runWG` — a future edit silently diverges. Architecture rot in the most load-bearing file of the bus.

16. **LOW — Handler-pause / decision-blocker / sentinel gates copy-pasted between queue and br-ready paths** — `internal/daemon/workloop.go:2308-2348` vs `2743-2780` (RU-01a). Near-verbatim duplicates already diverged in log wording. A new pause dimension silently misses one path.

17. **LOW — Six-level telescoping `RunSocketListener*` constructor ladder survives after `SocketHandlers`/`Serve` replaced it** — `internal/daemon/socket.go:279-338` (RU-06). Six exported functions each forwarding to the next, differing only in trailing nil handlers — exactly the positional telescoping the SR-3 refactor was meant to retire. A new handler means touching every rung.

18. **LOW — Two overlapping exit-classification paths** — `internal/handler/classify_hc023.go` vs `internal/handlercontract/outcomedelivery.go:179-225` (RU-08). Both encode the HC-008/023/024 "exit-without-outcome is structural" rule in different packages over different structs; `ClassifyExitState` has a cancel/transient priority ladder `ClassifyExit` lacks. Which one the daemon consults determines whether cancellation is honored.

19. **LOW — `TerminalOpReset` and `TerminalOpReopen` re-issue byte-identical br argv** — `internal/brcli/reissueintent_bi031.go` (RU-18). Two semantically-distinct terminal ops collapse to `update <id> --status open`; a reset that should clear assignee is replayed as a plain reopen and under-applies. Latent drift trap.

20. **NIT — Two hand-rolled git-porcelain parsers in one package** — `internal/workspace/mergedispatch_wm018a.go:167` vs `wipcapture_rc019.go:118` (RU-04b). Subtly different splitting rules, both share the `-z` gap. Invites divergence.

### D. Brittle coupling — stringly-typed control flow, hardcoded literals, positional fragility

21. **MEDIUM — Cap-hit salvage and resume-nudge branch on the hardcoded node-ID string `"commit_gate"`** — `internal/daemon/dot_cascade.go:1122,851` (RU-02). The graph is arbitrary author-defined DOT; the file's own header says terminal disposition MUST resolve by node identity via the spec surface, not ad-hoc topology. A graph naming its gate anything else silently loses cap-hit salvage (committed tip stranded) and gate-failure feedback.

22. **MEDIUM — Event taxonomy is three hand-maintained parallel lists keyed on duplicated string literals with no compile-time link** — `internal/core/pertypecompat_hqwn38.go` + `eventtype.go` + `eventreg_*.go` (RU-10). ~169 event names spelled independently in three places; registration uses raw literals not the `EventType` constants; registry keys are `map[string]` (standing TODO to hoist). The ~300-line compat table is near-identical rows. A typo/added-type in one list is invisible to the compiler.

23. **MEDIUM — Unknown-keeper-key detection couples to yaml.v3's internal error-message text via regex** — `internal/daemon/projectconfig.go:1481` (RU-06). `keeperUnknownFieldRe` matches yaml.v3's literal TypeError wording; a yaml.v3 upgrade silently degrades precise `*ErrUnknownConfigKey` (naming the bad key) to a generic malformed error. Also depends on `keeperTypeToPrefix` staying in exact sync with Go type names.

24. **LOW — `policy_ref` rejection routed by `strings.Contains(err, "CP-056")`** — `internal/workflow/loader.go` (RU-15). Stringly-typed control flow over a formatted message: breaks if wording changes; masks other parse errors when a graph has both a policy_ref and real errors; misclassifies any future text containing "CP-056". Typed `ParseError` values already exist.

25. **LOW — Router `Classify` hinges on `len(rawJSON)>2` of the `type` value** — `internal/daemon/router/router.go:44-49` (RU-06). `{"type":null}` misroutes as a hook-relay envelope. Pinning routing to raw-JSON byte-length rather than a decoded discriminator is brittle and surprising (documented tech-debt).

26. **LOW — `parseStallSentinelBlock` allocates 7 throwaway `*time.Duration` via `new()` then dereferences by position** — `internal/daemon/projectconfig.go:1882-1905` (RU-06). Reordering the fields slice silently misassigns values with no compile error; the sibling parsers already use the safe `&cfg.Field` pattern.

27. **LOW — Multiple hand-maintained field-by-field `*BlockAbsent` invariants** — `internal/daemon/projectconfig.go:402/766/1794/1134/1280-1292` (RU-06). Each enumerates every field of its raw struct guarded only by an "extend in lockstep" comment; two (watch, harnesses) have **already** drifted out of sync. No compile-time enforcement; scales poorly across 10+ config blocks.

28. **LOW — Orchestrator-session discovery hardcodes the harmonik repo path and `$USER`** — `internal/usage/usage.go:424` (+`sessiondata.go:623`) (RU-23). Builds `-Users-<USER>-github-harmonik`; any other checkout path silently yields zero orchestrator sessions, so `harmonik usage` under-reports burn with no warning. Duplicated brittle assumption.

29. **LOW — `DefaultHarmonikPath` hardcodes an operator-specific home path** — `internal/workers/workers.go` = `/Users/gb/go/bin/harmonik` (RU-04). Any worker not user `gb`/non-macOS silently falls back to a nonexistent binary; the run then times out at agent_ready with no clear signal.

### E. Sloppy / stale / misleading code carried in production

30. **MEDIUM — `normalize()` silently no-ops load-bearing effectors (`closeBead`/`submitMerge`)** — `internal/daemon/runshell.go` (RU-01b). Defaulting terminal-spine effectors to nil-returning no-ops means a forgotten `wireSpine` arm produces a run that hangs or mis-terminates with zero error surfaced — exactly the "fabricated done-status" failure class the subsystem exists to prevent.

31. **LOW — Stale `mergeMu` references throughout workspace/daemon comments after RSM-015 replaced the mutex with a FIFO merge queue** — `internal/workspace/worktreepath.go:51,85`; `workloop.go:403,406,408,6785,7676` (RU-25). The mutex is gone but comments claim a live `mergeMu` invariant. A maintainer reasons about serialisation with the wrong (mutex vs FIFO, different fairness) mental model or hunts a field that doesn't exist.

32. **LOW — Inconsistent local/remote runner classification across the `*Via` surface** — `internal/workspace/diffhash.go:47` + `remotematerialize.go` branch on `runner==nil`, while `reviewverdict.go:162`/`autostatusmarker.go:96` branch on `runner==nil || runnerIsLocalFS(runner)` (RU-04). A `tmux.LocalRunner` gets local behavior in one set and shell-routed "remote" in the other, contradicting the NFR7 byte-identical-local claim. Latent drift trap.

33. **LOW — `session.closed` field is never set true; the guard and its doc are dead/misleading** — `internal/hook/sessionstore.go:84-88,263` (RU-14). Close is achieved by `delete`; nothing writes `closed=true`, so the `sess.closed` term is always false. Doc describes a soft-closed-but-present state that does not exist.

34. **LOW — Redundant if/else where both branches are identical** — `internal/sentinel/governor.go:517-522` (RU-23). `isLowWindow` and its "moderate" else both run `state.ConsecutiveLowWindows++`; with default config the moderate band is empty. Pure noise that implies differentiated handling.

35. **LOW — `windowSweepAnyRemain` takes a `prefix` param it immediately discards** — `internal/lifecycle/tmux/orphanwindow.go:166` (RU-05b). Threaded from the call site purely to be ignored (`_ = prefix`); misleads a reader into thinking prefix filtering still happens.

36. **LOW — `RunID` placeholder pointer (`&runUUIDStr` with empty string) is a brittle two-phase patch** — `internal/daemon/workloop.go:2608-2611,2794-2818` (RU-01a). queue.json persists with `RunID=""` between stamp and patch; a crash or group/index drift durably leaves `RunID=""`, indistinguishable from an intentionally cleared RunID, defeating QM recovery keyed on RunID presence.

### F. Speculative / over-engineered machinery delivering no behavior

37. **LOW — Per-type schema-version / N-1 compat apparatus is entirely speculative** — `internal/core/pertypecompat_hqwn38.go` (RU-10). Every row is `{CurrentVersion:1, PreviousVersion:0, CompatWindowHolds:true, AdditiveOnly:true}`; no type ever advanced; the single live reader only checks `CompatWindowHolds` which is unconditionally true (dead branch). A ~300-line table + registration API requiring a new row per event, enforced by tests, delivering nothing today.

38. **LOW — Pervasive deferred-typing debt: specced typed aliases carried as raw unvalidated strings** — `internal/core/*_hqwn59.go` (RU-10b). Dozens of identifier fields (SkillVersion, BatchID, ControlPointName, CommitHash, OperatorID, GuardRef, HookInvocationID, ToolName, ...) are plain `string` with "TODO: hoist to typed alias" notes. Malformed commit hashes / empty control-point names round-trip silently; the promised type-safety layer is a package-wide backlog.

39. **LOW — `decisionsClientProjection` is production-file code used only by tests** — `cmd/harmonik/decisions_k4.go:548-594` (RU-16a). A full open-set fold shipped in the binary purely to back a parity test — a second implementation of the projection that can silently drift from the daemon's K3 projection it claims to mirror.

### G. Dead production code kept alive only by its own tests (test-theater by coverage)

40. **MEDIUM — Run-machine drive loop (`drive`/`driveOnce`/`fireOnCancel`) is production-dead** — `internal/daemon/runshell.go:242,433` (RU-01b). Only callers are test files; production single-mode drives via `runBridge.feed` and dispatch via `RunDispatch`. Tests assert a path the daemon never takes (and can't even catch a ctx-cancel busy-spin since they never cancel).

41. **MEDIUM — `SubstituteTemplateParams` is unused in production, kept alive by its own unit tests** — `internal/workflow/params.go` (RU-15). Header says it MUST NOT be used on any load path; live path is `substituteGraphParams`. Dead exported product code whose passing tests inflate apparent params coverage.

42. **MEDIUM — `LoadDotWorkflowWithPolicy` (CP-057 skills_ref resolution) is never called at runtime** — `internal/workflow/loader.go` (RU-15). Only callers are tests; the daemon uses loader variants that do no skills resolution. CP-057 skills_ref semantics are implemented + unit-tested but never exercised — dead code or a silent wiring gap where a spec obligation doesn't run.

43. **LOW — `hasChildProcess`/`hasAnyDirectChild`/`commandMatchesLiveAgent` are production-dead** — `internal/daemon/pasteinject.go:353,371,389` (RU-05a). Reachable only from tests; the live liveness path uses `PaneHasActiveProcess`→`*OrSSHFail`/`*Via`. Two near-identical copies (bare vs `*Via`) invite drift.

44. **LOW — `CurrentPayloadSchemaVersion` is a pure alias with no non-test caller** — `internal/core/eventregistry.go` (RU-10). Live consumers call `LookupTypeSchemaVersion` directly; this exported duplicate carries its own doc block to keep in sync.

45. **LOW — `knownSessionIDs` is created and threaded through but never populated** — `internal/usage/usage.go:275,279,440` (RU-23). Intended dedup-by-session-id is dead; a daemon run whose transcript isn't all-run/-branches can be double-counted as both a run and an orchestrator session.

### H. Test-theater — tests that validate copies/literals, not the system

46. **HIGH — 22 of 37 `operatornfr` test files are pure spec-mirror theater** — e.g. `internal/operatornfr/restartrto_sx9r81_test.go:29,115` (RU-20). ~13.6k test LOC across 22 files declare fixture constants inline and assert them against themselves, referencing zero production symbols and importing no product package (`const X = 30 … if X != 30 { t.Errorf }`). Zero regression protection; can only fail if someone edits both const and assertion together. CLASSIFY: DELETE.

47. **HIGH — `operatornfr` state-machine test reimplements the transition logic locally and tests the copy** — `internal/operatornfr/statemachinetransition_test.go:21-26,203,291` (RU-20). Own state enum + own `smFixtureApply`; the real daemon state machine is never imported. Only the exit-code cross-checks touch real code. The product state machine can change and the test stays green.

48. **MEDIUM — smoke Signal 3 greps for a `Refs: <bead>` trailer the smoke task never instructs the agent to write** — `cmd/harmonik/smoke.go:239-249,534` (RU-16a). Nothing in the daemon merge path injects a `Refs:` trailer. A correct commit reports Signal 3 FAIL → smoke exits 1/2 (false negative) on a healthy daemon; passes only if agents add the trailer by unrelated convention.

49. **MEDIUM — FIFO test proves only the sleep-tuned choreography it constructs** — `internal/mergeq/mergeq_test.go` (RU-03). `TestFIFOOrderUnderConcurrentSubmits` sleeps 2ms between launches to manufacture the exact serial arrival order it then asserts — never exercises genuinely-concurrent submits (the real hazard). False confidence + timing-fragile/flaky under CI load.

50. **LOW — `TestSHINV005ImplementationAudit` asserts on a source-code substring, not behavior** — `internal/specaudit/sh_inv005_declarative_loadable_test.go` (RU-21). Passes if `"KnownFields(true)"` appears anywhere in source (incl. a comment) — doesn't verify the decoder actually invokes it. A refactor leaving a stale comment keeps it green while strict parsing regresses.

---

## Nits

- **Exported function name typo `DeriveCIaudeTranscriptPath`** (capital-I for lowercase-l in "Claude") — baked into the package API — `internal/handler/claudehandler_chb006_024.go` (RU-08).
- **`handlercontract` fragmented into ~50 one-spec-clause-per-file units (HC-xxx)**; related state scattered, filenames encode tracking IDs not content — `internal/handlercontract/` (RU-08). Also contradicts the project's "say the thing, not the pointer" guidance.
- **56 of 237 `internal/core` source files carry opaque bead-id filename suffixes** (agentevents_hqwn59.go, etc.); navigation/grep-by-topic tax (hashes stay out of exported symbols) — (RU-10b).
- **Production constructor `newPerRunSubstrate` calls a test-only seam `notifySubstrateRunner` on every construction** — hot path per bead run — `internal/daemon/tmuxsubstrate.go` (RU-05a).
- **Direct `fmt.Printf` to stdout from a long-lived daemon path** (emitWarn/maybeRespawn/maybeLivePaneRecover) — can't be routed/leveled/silenced — `internal/keeper/watcher.go:1560,1615,1722` (RU-13).
- **`parseResult` is a no-op stub with a misleading doc comment** — `internal/codexwire/codexwire.go` (RU-07).
- **Unused constant `hashSuffixLen`** — `internal/lifecycle/tmux/windowname.go` (RU-05b).
- **Dead computed-and-discarded expression** `_ = tailStart + i` — `internal/queue/append.go` (RU-09).
- **`appendUnderLock` disk-fallback safety comment relies on an invariant `submit` violates** — false assurance — `internal/queue/rpc.go` (RU-09).
- **Stale absolute line-number cross-references in comments** — `internal/daemon/dot_cascade.go:916,891` (RU-02).
- **Stale TODO claims `TimeoutSecs` is unwired but `harness.go:463` already populates it** — `internal/scenario/orchdrive.go:90-94` (RU-22).
- **`sendVerdictOverrideRequest` doc + stderr prefix hardcoded to `confirm-verdict` on the shared veto path** — confusing during triage — `cmd/harmonik/confirm_verdict.go` (RU-16a).
- **Unrecognised-verb error omits valid verbs (reap)** — hand-maintained duplicate of the switch, already drifted — `cmd/harmonik/supervise_cmd.go` (RU-17).
- **goal-keeper schedule seeds `Argv[0]="harmonik"` from PATH rather than `os.Executable()`** — inconsistent with the same file — `cmd/harmonik/init_cmd.go:873` (RU-16b).
- **statusline omits the `HARMONIK_KEEPER_AGENT` back-compat alias the three other hooks honor** — divergent agent identity — `cmd/harmonik/assets/scripts/keeper-statusline.sh` (RU-24).
- **Docstrings say "four required patterns" but `RequiredGitignoreEntries` has six** — `internal/workspace/gitignorehygiene.go` (RU-04b).
- **`doc.go` frames `operatornfr` as exit-code-only but it now spans 8 obligation areas** — misleading entry point — `internal/operatornfr/doc.go` (RU-20).
- **Hand-rolled `itoa` to avoid importing `strconv`** — needless, untested neg branch — `internal/operatornfr/commandcodes.go:252` (RU-20).
- **Condition bridge keys evaluator lookup by raw condition string rather than edge identity** — avoidable dot/core coupling — `internal/workflow/dispatcher.go` (RU-15).
- **Redundant frozen anchor: `count` duplicates `started` (both 507)** — `internal/keepertest/canary_test.go` (RU-19).
- **`TestSHINV005SpecBodyAudit` greps spec prose for enforcement-phrase substrings** — a prose lint masquerading as a conformance sensor — `internal/specaudit/sh_inv005_declarative_loadable_test.go` (RU-21).

---

## Coverage gaps (failed / partial review units)

- **RU-08 — status = `partial`** (`internal/substrate/`, `internal/handler/`, `internal/handlercontract/`). Reviewed the substrate/Handler-contract interface seam + the largest production source (~5k LOC); the ~50 one-clause-per-file HC-xxx contract files and ~14k LOC of test files were NOT line-audited. The two-overlapping-exit-classification finding surfaced here — the fragmented HC-xxx surface is under-reviewed for further duplication.

No units reported `status = failed`.

Self-noted non-exhaustive passes (status `reviewed` but explicitly not line-by-line — treat as partial coverage for this lens):
- **RU-01b** — the ~4000-line `workloop.go` body was read only selectively around the shell→machine mapping, not line-by-line.
- **RU-10b** — skim/classify pass over ~26k LOC / 237 core source files; systemic-pattern focus, not an exhaustive line audit.
- **RU-02** — read `dot_cascade.go` to ~line 1693 of 2669 and `reviewloop.go` to ~997 of 2000+; the tails of both god-functions were not fully audited.
- **RU-08** (as above).
