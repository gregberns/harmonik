# CLI structure assessment — findings

Assessment for [`tasks/cli-structure-assessment.md`](tasks/cli-structure-assessment.md)
(`hk-cli-structure-assessment-a0dzb`). Read-only: no code moved, nothing changed. Feeds
`tasks/cli-extract-logic.md`, which decides the actual moves.

Measured 2026-08-24 against `cmd/harmonik/` (package main, top-level files only — excludes the
`cmd/harmonik/supervise` and `cmd/harmonik/digest` subpackages, which are already separate
packages and out of scope). 71 production files, 24,815 lines; 102 test files, 21,119 lines
(the task's 24,491/0.85 figures are one day stale, per `PLAN.md`; the shape of the finding does
not change).

## 1. Is it well structured?

**Partly.** The majority of the 71 files are exactly what a CLI package should be: a
`run<Verb>Subcommand` function that parses flags, calls one function in an existing `internal/`
package, and prints the result. `sentinel_cmd.go`, `usage_cmd.go`, `project_hash_cmd.go`,
`remote_control_prefix_cmd.go`, `write_review_verdict_cmd.go`, `supervise_cmd.go`, `digest.go`,
`agent.go`, `release_cmd.go`, `reconcile.go`, `branch_reap_cmd.go` and about a dozen more are
thin in exactly this sense — no move is indicated for them.

But four things are not:

- **A cluster of pure, zero-CLI-dependency logic sits in `cmd/harmonik` for no structural
  reason.** The four `resolve_*_config.go` files (`resolve_keeper_config.go` 516,
  `resolve_pi_config.go` 276, `resolve_watch_config.go` 136, `resolve_stall_sentinel_config.go`
  170 — 1,098 lines) are pure validation/merge functions over `internal/projectconfig` types.
  They take no terminal input and produce no terminal output; they happen to be called from a
  flag handler. Same shape for `asset_manifest.go` (162), `asset_reconcile.go` (273),
  `asset_skew.go` (267) — a diff/reconcile algorithm with no `fmt.Print` anywhere in the diffing
  code — and for `beadsmerge.go` (434) / `beadsdedup.go` (108), a three-way JSONL merge
  algorithm.
- **One file (`comms.go`) hand-rolls the same low-level protocol six times** instead of calling
  a shared helper — see §2.
- **One file (`harness.go`) is 80% orchestration logic that belongs next to its own siblings**,
  which already exist in `internal/scenario` — see §2 and §4.
- **Two files (`session_bootstrap.go`, `substrate_select.go`) are business logic that never had
  a package to begin with** — nobody put them in `cmd/harmonik` by mistake; there was simply
  nowhere else for them to go, and the assessment below proposes one.

None of this is a scandal. It reads as organic growth — each new verb was added the fastest way
that worked, and nobody has stepped back to sort the accumulated pile since. That is the state
described in the Problem section, and it holds up: the package works, is not architecturally
broken, but is carrying weight it does not need to carry.

## 2. The 24,815-line split

Estimated from a file-by-file read (not an automated classifier — see the per-file table in §3
for the basis of each number).

| Bucket | Est. lines | Share |
|---|---|---|
| A — CLI-only (flag parsing, usage text, output formatting, command wiring) | ~9,800 | ~40% |
| B — Business logic that belongs in a reusable package | ~13,400 | ~54% |
| C — Neither (embedded data/templates, `//go:embed` wiring, dead-ish glue) | ~1,600 | ~6% |

The B-bucket is not evenly distributed. Two shapes dominate it:

- **Thin-looking files with one non-thin function.** `keeper_cmd.go` is mostly config-assembly
  glue with one genuinely mixed 265-line function (`runKeeperSubcommand`); `harness.go` is one
  ~490-line orchestration function surrounded by CLI scaffolding. Moving "the business logic
  out of the file" in these cases mostly means moving one function, not gutting the file.
- **Files that are business logic wearing a `_cmd.go` filename.** `init_cmd.go` (889),
  `sync_assets_cmd.go` (876), `eval_cmd.go` (511) plus its siblings, `beadsmerge.go`,
  `beadsdedup.go`, the four `resolve_*.go` files — these are not CLI files that grew some logic;
  they are logic files that never got their own package.

The C-bucket is small and not interesting: `keeper_config_example.go` and
`codex_config_example.go` are embedded YAML template constants, `init_skill_assets.go` is a
`//go:embed` directive, `usage.go` is pure help text. None of it is dead code — a scan for
unreferenced exported functions across the package turned up nothing worth flagging.

## 3. Candidates to move, and whether the destination exists

Full per-file table (66 of 71 files; the other 5 are covered in depth in §4):

| File | LOC | Verdict | Destination |
|---|---|---|---|
| `init_cmd.go` | 889 | move (partial) | new `internal/projectinit` — shared with `sync_assets_cmd.go`'s apply logic |
| `sync_assets_cmd.go` | 876 | move (partial) | new `internal/assetsync` — with `asset_manifest.go`, `asset_reconcile.go`, `asset_skew.go` |
| `handler.go` | 742 | stays | already delegates to `internal/handler` |
| `run.go` | 710 | move (partial) | `classifyRunExit` / archive logic → `internal/runlaunch` (exists) |
| `decisions.go` | 677 | move (partial) | new `internal/decisionsclient` — with `decisions_k4.go` |
| `crew.go` | 640 | stays (mostly) | thin wiring into `internal/crew`/`internal/lifecycle` |
| `smoke.go` | 624 | move (partial) | new `internal/smoketest` |
| `resolve_keeper_config.go` | 516 | **move** | `internal/keeper` (exists, already imported) |
| `eval_cmd.go` | 511 | move (partial) | new `internal/evalreport` — with `eval_metrics_cmd.go`, `eval_report_cmd.go`, `eval_guardrails_lygpp.go` |
| `run_via_daemon.go` | 477 | move (partial) | `internal/runlaunch` client helpers |
| `promote_cmd.go` | 474 | stays (mostly) | built on `internal/branching` + `internal/runmerge` already |
| `schedule.go` | 466 | move (partial) | spec parsing → `internal/schedule` (exists) |
| `sleepwake.go` | 465 | move (partial) | new `internal/sleepwake`, or fold into `internal/crew` |
| `version_verify.go` | 456 | move (partial) | new `internal/versionverify`, or fold into `internal/release` |
| `release_cmd.go` | 454 | stays | already thin over `internal/release` |
| `queue_readiness.go` | 449 | stays | thin over `internal/queue/readiness` |
| `decisions_k4.go` | 444 | move (partial) | pairs with `decisions.go` → `internal/decisionsclient` |
| `subscribe.go` | 440 | move (partial) | new `internal/subscribeclient` |
| `beadsmerge.go` | 434 | **move** | new `internal/beadsmerge` |
| `captain.go` | 406 | move (partial) | new `internal/captainlaunch`, or fold into `internal/crewrun` |
| `eval_metrics_cmd.go` | 385 | move (partial) | `internal/evalreport` cluster |
| `ops_monitor_cmd.go` | 358 | move (partial) | new `internal/opsmonitor` (macOS launchd service management) |
| `dashboard_cmd.go` | 353 | move (partial) | filter/health helpers → `internal/dashboard` (exists) |
| `commitmsg_cmd.go` | 314 | move (partial) | resolution helpers → `internal/commitmsg` (exists) |
| `sentinel_cmd.go` | 310 | stays | already thin |
| `agent.go` | 297 | stays | already thin |
| `digest.go` | 294 | stays | already delegates |
| `resolve_pi_config.go` | 276 | **move** | `internal/projectconfig` (exists, already imported) |
| `asset_reconcile.go` | 273 | **move** | `internal/assetsync` cluster |
| `asset_skew.go` | 267 | move (partial) | `internal/assetsync` cluster |
| `migrate_rc_prefix_cmd.go` | 256 | move (partial) | `internal/projectconfig` |
| `eval_report_cmd.go` | 246 | move (partial) | `internal/evalreport` cluster |
| `substrate_select.go` | 242 | **move** | new `internal/substrateselect` (glue between `handler`/`substrate`/`workers`/`codexdriver`) |
| `start.go` | 228 | stays | legitimate dispatcher |
| `branch_reap_cmd.go` | 224 | stays | thin over `internal/lifecycle` |
| `state_cmd.go` | 209 | move (partial) | `internal/daemon` (StateSnapshot already lives there) |
| `confirm_verdict.go` | 204 | move (partial) | pairs with `veto_verdict.go` |
| `reconcile.go` | 202 | stays | thin over `internal/lifecycle`/`internal/queue` |
| `usage_cmd.go` | 189 | stays | thin over `internal/usage` |
| `session_bootstrap.go` | 189 | **move** | new `internal/sessionbootstrap`, or fold into `internal/dispatch` |
| `captain_respawn.go` | 188 | move (partial) | fold into `internal/crewrun` |
| `goalkeeper_cmd.go` | 176 | stays | already thin over `internal/goalstate` |
| `resolve_stall_sentinel_config.go` | 170 | **move** | `internal/projectconfig` |
| `graph.go` | 167 | move (partial) | diagnostic formatting → `internal/workflow/dot` |
| `asset_manifest.go` | 162 | **move** | `internal/assetsync` cluster |
| `veto_verdict.go` | 152 | move (partial) | pairs with `confirm_verdict.go` |
| `resolve_watch_config.go` | 136 | **move** | `internal/projectconfig` |
| `subscribetypes.go` | 133 | move (partial) | shared util with `subscribe.go` / `comms.go` |
| `usage.go` | 119 | stays | pure help text |
| `write_review_verdict_cmd.go` | 118 | stays | thin over `internal/workspace` |
| `keeper_config_example.go` | 117 | stays | embedded template, bucket C |
| `beadsdedup.go` | 108 | **move** | pairs with `beadsmerge.go` |
| `greenlight_cmd.go` | 106 | stays | small CLI signal |
| `project_hash_cmd.go` | 105 | stays | thin over `internal/lifecycle` |
| `tmuxhosting.go` | 96 | move (partial) | fold into `internal/lifecycle/tmux` |
| `supervise_cmd.go` | 92 | stays | pure forward |
| `remote_control_prefix_cmd.go` | 72 | stays | thin over `internal/projectconfig` |
| `eval_guardrails_lygpp.go` | 70 | move (partial) | `internal/evalreport` cluster |
| `version.go` | 44 | stays | version wiring |
| `codex_config_example.go` | 23 | stays | embedded template, bucket C |
| `workers_boot.go` | 22 | **move** | `internal/workers` (exists, one-line move) |
| `watcherreap.go` | 22 | move (partial) | fold into `internal/lifecycle` |
| `subscriberefusal.go` | 22 | move (partial) | shared util with `subscribe.go`/`comms.go` |
| `closewrite.go` | 13 | stays | trivial, no gain from moving |
| `scanbuf.go` | 9 | stays | trivial utility |
| `init_skill_assets.go` | 6 | stays | `//go:embed` wiring, bucket C |

**Four clusters carry most of the weight**, and each has a second caller or an existing sibling
package that earns it — none of these is a package created only to receive one file:

1. **`internal/projectconfig` absorbs the four `resolve_*_config.go` files as-is.** The package
   already exists, is already imported by all four files, and the functions being moved are
   pure — no terminal dependency to strip out first.
2. **New `internal/assetsync`** for `asset_manifest.go`, `asset_reconcile.go`, `asset_skew.go`,
   plus the reconcile/apply logic inside `init_cmd.go` and `sync_assets_cmd.go`. Two existing
   callers (`init` and `sync-assets` are separate commands that both drive the same
   manifest/lock/reconcile machinery), so this clears the one-caller bar on its own.
3. **New `internal/beadsmerge`** for `beadsmerge.go` + `beadsdedup.go` — a hand-rolled
   three-way-merge algorithm over `beads.jsonl` rows, already factored as one cohesive unit with
   two entry points (merge, dedup) sharing the same row model.
4. **New `internal/evalreport`** for `eval_cmd.go`, `eval_metrics_cmd.go`, `eval_report_cmd.go`,
   `eval_guardrails_lygpp.go` — four files already collaborating on one pipeline (collect →
   compute metrics → guardrail-filter → report), currently split across `cmd/harmonik` for no
   reason other than that's where `harmonik eval` lives.

**Two single-file moves stand on their own** because the destination package already exists and
already owns everything adjacent:

- `substrate_select.go` → new `internal/substrateselect`. It already has two callers
  (`main.go` and `run.go`, both calling `selectSubstrate`), and it is glue that already imports
  four different `internal/` packages (`handler`, `substrate`, `workers`, `codexdriver`) to
  decide tmux-vs-codex — the kind of decision that belongs in a package other code can call
  without going through the CLI.
- `session_bootstrap.go` → fold into `internal/dispatch`. It is already fully testable (every
  I/O call is injected), has one caller, but its logic (dial an acknowledgement endpoint, exec
  into a resolved handler binary) is dispatch-domain, not CLI-domain.

**Everything else marked "move (partial)"** is the shape found in §4 for `keeper_cmd.go` and
`harness.go`: a file that is legitimately mostly CLI, with one or two functions inside it that
are not. Those are named per-file above and in §4; the file itself does not relocate.

## 4. The five named largest files

### `comms.go` (1,897 lines)

The `harmonik comms` client: send/log/join/leave/who/recv against the daemon's messaging bus,
including a client-side reconnect-with-backoff follow loop. Roughly 55% CLI (flag parsing,
usage strings — five `commsXUsage()` functions alone run ~230 lines), 40% real logic.

The finding that matters: **comms.go hand-rolls "dial the daemon's unix socket, marshal a
`{op, payload}` envelope, write, decode the response" independently at least six times** —
in `runCommsSendSubcommand`, `runCommsPresenceSubcommand`, `runCommsRecvSubcommand`,
`runCommsRecvFollowIO` (~260 lines, the file's largest function — a genuine reconnect/backoff
state machine), `runCommsRecvWait`, `sendPresenceRefreshBeat`, and `sendPresenceLeaveBeat`.
There is no `internal/comms` package; `internal/transport` exists and is the first place to
check before creating a new one — this file may simply be missing a
`transport.CallSocketOp(sockPath, op, payload)` helper that belongs there. Whichever package
ends up owning it, this is the strongest concrete duplication finding in the whole package —
not a placement judgment call, six copies of the same twenty lines.

Verdict: **move** the socket-protocol plumbing (not the flag parsing / output formatting) into
`internal/transport` or a new `internal/comms` client package, once `cli-extract-logic` confirms
which.

### `keeper_enable_doctor_cmd.go` (1,207 lines)

`harmonik keeper enable` (wires keeper hook stanzas into the user's *global*
`~/.claude/settings.json`) and `harmonik keeper doctor` (a 12-check read-only drift validator).
~55% real logic: hand-rolled JSON-tree manipulation of an untyped `map[string]interface{}`
settings file (`mergeStatusLineStanza`, `mergeHookStanza`, `findHookForScript`,
`updateHookCommand`, ~200 lines) and the doctor's 12 sequential checks (~275 lines).

It calls `internal/keeper` correctly for keeper-domain primitives and does not duplicate
anything there. The settings.json logic is real, self-contained business logic — but it operates
on a different domain object than anything `internal/keeper` models (Claude Code's global
config, not harmonik's own `.harmonik/keeper/*` state), and it has exactly one caller.

Verdict: **stays**, per the task's own limit — "do not recommend a move that has no home… a
package with one caller needs a reason." No second caller exists and none is in sight.

### `main.go` (995 lines)

`run() int` (770 lines, lines 150–923) is excluded per the task's limits — its problem is
complexity, not placement, and it is being handled elsewhere. The remaining ~225 lines are
`main()` itself (2 lines), two usage-string constants (~85 lines), a `flag.Value`
implementation, and four small single-caller shutdown/watchdog helpers
(`inFlightDrainGoroutine`, `startSupervisorWatchdogIfEnabled`, `buildSupervisorWatchdogSpec`,
`spawnCapFromEnv`) that delegate cleanly to `internal/supervise`.

Verdict: **no move.** The reviewable slice is small, clean composition-root code; every finding
in this file is inside the excluded `run()`.

### `keeper_cmd.go` (904 lines)

`harmonik keeper` (the watcher entry point) plus marker-mutation subcommands (`hold`,
`release`, `restart-now`, `ping`, `await-ack`). ~50% real logic, concentrated in two functions:
`buildKeeperConfigs` (~90 lines, translates CLI-resolved flags into `keeper.CyclerConfig`/
`keeper.WatcherConfig`) and `runKeeperSubcommand` (~265 lines, mixes flag parsing with real
sequencing: boot-time doctor check, lock acquisition, tmux resolution, crash recovery).

Everything else delegates cleanly to `internal/keeper`, including the one place this file has
to reach across a depguard boundary (`keeperOperatorWarnFn` shells out to
`harmonik comms send` as a subprocess, because `internal/keeper` cannot import `internal/daemon`
or the comms client directly).

Verdict: **no move.** `buildKeeperConfigs` translates CLI flag types into keeper's config
structs — moving it into `internal/keeper` would make that package depend on CLI flag shapes,
which is backwards. This is legitimate composition-root code, not misplaced logic.

### `harness.go` (898 lines)

The `harmonik harness` scenario-test driver: discover/load/filter scenario YAML, then for each
scenario resolve the twin binary, bootstrap a fixture, apply fixture files and workflow DOT,
drive orchestration, evaluate assertions, tear down, emit a `SuiteResult`. ~80% real logic — by
far the highest logic share of the five.

`internal/scenario` already exists and already owns every *adjacent* step of this exact
pipeline: `BootstrapFixture`, `TeardownFixture`, `DriveOrchestration`, `EvaluateAssertions`,
`WriteSuiteResult`, `CheckTwinBinaryPath`, each in its own file
(`fixturebootstrap.go`, `fixtureteardown.go`, `orchdrive.go`, `twinpathcheck.go`). What's missing
from `internal/scenario` is exactly what's sitting in `harness.go`: the suite loop that
sequences those primitives (`runHarnessWithSigs`, ~490 lines — a genuine execution engine with
its own signal-handling goroutine for graceful-vs-forced shutdown), plus `harnessDiscoverScenarios`
(directory walk, duplicate-name detection, cadence filtering), `harnessResolveTwinBinary`,
`harnessApplyFixtureFiles`, and `harnessApplyWorkflowDOT`.

Verdict: **move.** This is not a "does a home exist" judgment call — the home is already built
and 90% occupied by this pipeline's other stages; the CLI file holds the one stage that never
made the trip. `internal/scenario` gains `DiscoverScenarios`, `ResolveTwinBinary`,
`ApplyFixtureFiles`, `ApplyWorkflowDOT`, and a `RunSuite` entry point; `harness.go` keeps flag
parsing, `--list`/`--dry-run` printing, and calling `scenario.RunSuite`.

## 5. Why the test ratio is 0.85

Not mostly untested logic. Measured coverage is far higher than the line-count ratio implies:

```
cmd/harmonik            43.7%–58.3% of statements   (fresh run vs. ratified CI baseline — see note below)
cmd/harmonik/digest     82.5%
cmd/harmonik/supervise  35.9%–47.9%
```

(The fresh local run and the CI-ratified baseline at `scripts/cmd-coverage.baseline`, dated
2026-07-24, disagree by ~14 points — worth a look by whoever owns that gate, but not a finding
about the CLI's structure, and immaterial to the conclusion here: even the lower number puts
`cmd/harmonik` in the same range as `internal/daemon` at 58.9%, despite daemon's test-line ratio
being 2.7x higher.)

Three things explain the gap between the line-count ratio and the coverage percentage:

1. **The dominant factor: tests are named after the bead or feature that added them, not after
   the production file they cover.** `wave4_cli_fixes_test.go` alone exercises pieces of six
   different production files. `decisions.go` is covered by four differently-named test files
   with no `decisions_test.go` among them. A same-name-pairing heuristic — which a raw
   ratio implicitly assumes — badly undercounts real coverage here. `harness.go` and
   `keeper_cmd.go` both lack a same-named test file but are partially exercised by tests filed
   elsewhere (`keeper_cycle_composition_test.go`, `wave4_cli_fixes_test.go`).
2. **Thin CLI wrappers over well-tested `internal/` packages don't need their own tests, and
   mostly don't have them.** `internal/keeper` (72.6% baseline) and `internal/scenario` (44.0%,
   30 test files) carry the real logic and its coverage; `keeper_cmd.go`'s per-verb dispatchers
   (`runKeeperHold`, `runKeeperPing`, etc.) are locally 0% but are ~10-line pass-throughs to
   functions that are directly tested one layer down.
3. **A real, non-trivial exception: `harness.go`'s `runHarness`/`runHarnessWithSigs` (the
   ~500-line suite-loop orchestrator — signal handling, fixture-root lifecycle, twin-binary
   resolution, interrupt-result emission) is genuinely 0% covered**, and it is not a thin
   wrapper — it is the actual execution engine described in §4. `crew.go` has a comparable tail
   of untested dispatch functions (14 of its functions show 0%). This is the one place where
   the low ratio is correctly, if crudely, flagging real risk rather than a naming artifact.

So: mostly (2) and (1), with (3) as a real and specific exception concentrated in exactly the
file §4 already recommends moving — moving `harness.go`'s orchestration into `internal/scenario`
would let it inherit that package's existing test harness and close this gap as a side effect,
which is the kind of evidence `cli-extract-logic`'s "test ratio for whatever remains improves"
done-when criterion is asking for.

## 6. What the 106 lint exclusions are hiding

176 entries in `tools/lintreport/allow.txt` mention `cmd/harmonik`; 147 of those are in
top-level `cmd/harmonik/*.go` (the task's "106" is stale — see the coverage-baseline caveat
above, same drift pattern), split 109 production / 38 test. The test-file and production-file
profiles are distinctly different shapes:

| Linter | Production | Test | What it checks |
|---|---|---|---|
| gocognit | 63 | 0 | Cognitive complexity of a function |
| cyclop | 18 | 0 | Cyclomatic complexity (branch count) |
| gocritic | 13 | 3 | Diagnostic/style/performance bundle |
| gosec | 12 | 14 | Security-sensitive patterns (file perms, subprocess args, path taint) |
| errcheck | 0 | 16 | A returned error is silently discarded |
| unparam | 3 | 1 | A parameter or return value never varies |
| noctx | 0 | 3 | A call is made without a cancellable context |
| prealloc | 0 | 1 | A slice grown by `append` without a capacity hint |

**Production (109 entries, 74% gocognit+cyclop): mechanically-branchy CLI plumbing, not hidden
risk.** The pattern repeats across almost every entry: a flag parser with one branch per `--flag`
(`parsePromoteFlags`, `parseReleaseFlags`), a verb dispatcher with one branch per subcommand
(`main.go`'s `run`, `supervise_cmd.go`'s dispatcher), or JSON-shape-walking with one guard per
nesting level (`keeper_enable_doctor_cmd.go`'s `updateHookCommand`). These are wide-but-shallow —
each branch is a one-line error path — which is exactly the shape complexity linters
over-flag and exactly the shape a CLI dispatch layer is supposed to have. The gosec entries here
(file-permission bits on directories, `exec.Command` with operator-supplied paths) are the
expected profile for a local, single-operator CLI/daemon-management tool; several are already
annotated `//nolint:gosec` with a stated reason. One exception worth a closer look on its own
merits: `beadsmerge.go`'s `mergeBeadRows` trips gocritic on genuine conflict-resolution branching,
not style noise — moving it to `internal/beadsmerge` (§3) is a chance to also simplify it.

**Test (38 entries, 74% errcheck+gosec): near-total false positives on throwaway fixture code.**
`t.Cleanup(func() { _ = os.RemoveAll(base) })`, fake-daemon goroutines writing to a pipe with no
`*testing.T` handle to report through, `os.MkdirAll` inside `t.TempDir()` — these are the
correct pattern for test teardown and fixture setup; errcheck/gosec/noctx have no way to
distinguish this from production risk swallowing a real error.

**Synthesis: this is "big mechanically-branchy CLI plumbing that got grandfathered in when the
gate was introduced," not "real risk being waved through."** Nothing found here changes the
picture in §1–§4. It is, however, corroborating evidence for the same conclusion: the CLI layer
carries functions (dispatchers, JSON-walkers, flag parsers) that are complex because of their
CLI shape, not because business logic is tangled into them — which is consistent with "the
package is not architecturally broken, it just needs sorting."

## First move

**Start with `harness.go` → `internal/scenario`.**

Reasons, in order:

1. It is the cleanest case in the whole survey — not a "does a home exist" judgment call, since
   `internal/scenario` already holds every sibling stage of this exact pipeline. There is nothing
   to design; there is a gap to close.
2. It is the one place in the whole file where the low test ratio (§5) is flagging real,
   specific, non-trivial untested risk (`runHarnessWithSigs`, ~500 lines, 0% covered) rather than
   a naming or wrapper artifact. Moving it lets it inherit `internal/scenario`'s existing 30-file
   test harness, which directly serves `cli-extract-logic`'s stated done-when: "the test ratio
   for whatever remains in `cmd/harmonik` improves."
3. It is self-contained: one file, one destination, no shared cluster to coordinate (unlike the
   `resolve_*_config.go` group, `assetsync`, `beadsmerge`, or `evalreport`, each of which needs
   its receiving package created or its member files agreed on before the first commit).
4. `comms.go`'s six-fold protocol duplication (§4) is the more dramatic finding, but its
   destination is not yet settled — it depends on whether `internal/transport` already has room
   for a `CallSocketOp` helper or a new client package is needed. That's a five-minute check
   `cli-extract-logic` should do first; it should not gate starting on `harness.go`.
