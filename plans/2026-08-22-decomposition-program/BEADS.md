# Decomposition program — work breakdown

**Written 2026-08-22. Baseline commit `43ad681c4` on `work/alpha-integration-merge`.**

This is the task list, not the beads. A later step turns each numbered task below into one
bead. Nothing here has been filed.

## What this program is for

`internal/daemon` is 110 production files and 49,370 lines of Go (51,261 with its three
subpackages), plus 304 test files and 95,594 lines of test. It is 26% of all production code
under `internal/`, 1.6x the next biggest package, and it takes 294 to 334 seconds of the
~474-second `make core` run — well over half. It is one connected blob: every one of its 110
files sits in a single connected component of the internal reference graph, so nothing comes
out by moving a file on its own.

Three months of prior planning already exist and most of it is unexecuted. This document does
not re-plan that work. It orders it, adds the pieces the prior plans were missing, and states
which harness runs each piece.

**The one number that says whether this is working: test-seconds resident in
`internal/daemon`.** It is 294.5s today over 1,091 tests. Publish it after every extraction.
Package line count is the second number: 49,370 today, up 4,562 from the day the
delete-and-rewrite program started. Extraction has been real and new code has outrun it.

## Ground truth that changes what you would otherwise do

Read these before writing a single bead body. Each one has been measured, and each one
invalidates an obvious plan.

1. **Charlie is already merged.** `work/alpha-integration-merge` and `work/charlie-universal-run`
   both point at `43ad681c4`; the reflog records the fast-forward at 01:14 on 2026-08-22. There
   is no merge to plan. Keep it.
2. **The crash-safe dispatch replay subsystem Charlie built is inert and must stay inert.**
   Nothing in production writes a dispatch intent. `preflightDispatchReplay` in
   `internal/daemon/bootreconcile.go` returns a fatal error on *any* non-empty intent list, and
   that error reaches `daemon.go` and kills the boot. Three terminal replay actions
   (`RemoveDispatchIntent`, `ReplayCleanupOnly`, `ReplayRepairRequired`) hit the default arm and
   hard-error. The first intent ever written wedges the daemon into a boot-abort loop that the
   supervisor revives forever. **No task in this program turns on the producer.**
3. **The lint allow list is keyed on exact file path, and moving a file strips its
   grandfathering.** Proven: rewriting one file's path in the lint report produced
   `FAIL: 8 file/linter pairs are not on the allow list`, exit 1. `scripts/lint-allow-ratchet.sh`
   then forbids the repair — "the allow list only ever gets shorter". 249 daemon pairs are
   queued behind this. **The first extraction commit hits this. Fix it first (T01).**
4. **Every extraction edits the composition root.** Measured call-site files per cluster:
   harness resolution touches 10 other files, handler-pause 11, stale-watch 10, sweeps 6. The
   boot files (`daemon.go`, `bootstate.go`, `bootsocket.go`, `bootworkloop.go`,
   `bootreconcile.go`) appear in nearly every list. That is why the waves below are narrower
   than the seam analysis alone suggests, and why splitting boot wiring per cluster (T22) comes
   before the parallel extractions.
5. **`owning_epic_assignee` has never been populated.** Across 2,222 `run_completed` /
   `run_failed` events and 287 `run_stale` events in the live log it appears zero times,
   including 1,420 emitted after the feature landed. A crew filtering on it drops every line
   including its own. **Filter on `queue_id`.** `run_stale` carries no `queue_id` at all.
6. **The subscribe stream is a broadcast.** `subscriptionStream.offer` filters on the event-type
   set and, for `agent_message` only, sender/recipient/topic. There is no queue filter and no
   `--queue` flag. Every crew armed on `run_completed` sees every other crew's completions.
   The crew-launch skill's claim that a named queue isolates your monitor is false.
7. **Deleting all comments is illegal here** — it adds 337 revive findings across 54 new
   file+linter pairs and there is no waiver route. **And struct-field doc comments are not safe
   to cut**: three tests in `internal/core` (in `CORE_PKGS`) assert on the literal text of the
   godoc on `Event.TimestampWall` and `Event.TimestampMonoNsec`.
8. **Token equivalence is not a sufficient safety proof for a comment cut.** The mandatory
   gofmt/gofumpt post-step changes code bytes: a comment inside a composite literal is a gofmt
   alignment-group separator, so deleting it re-pads the key column. 1,075 code lines across
   191 files change bytes with an identical token stream, and one of them breaks
   `TestM4C7_SeamSurvival_StructuralFloors`. The cutter needs a third check.
9. **The genuinely mechanical lint tier is ~148 findings, not 464.** `prealloc` is not
   mechanical — the naive fix `make([]T, len(x))` clears the finding, passes build and vet, and
   silently produces leading zero values; a live one feeds `git worktree remove --force --force`
   paths. `forbidigo` is 46 deliberate panics. 34 of the 57 revive `exported` findings are
   cross-package API renames.
10. **The local model is not slow; the serving setup is.** 78% of a measured 107-minute run was
    re-prefilling context that had not changed — zero prompt-cache reuse on every turn. Turning
    on prefix caching is arithmetically a 4.4x speedup and costs no repo change.

## Rules that apply to every task

- **File scope is binding.** Each task lists the files it may edit. Two concurrent beads must
  never share a file. The dispatching crew keeps the ownership table current and refuses to
  dispatch a bead whose scope overlaps an in-flight one.
- **Hot files.** `internal/daemon/scheduler.go`, `workloop.go`, `workloop_runplan.go`,
  `agentlaunch.go`, `dot_cascade_core.go`, `dot_cascade_helpers.go`, `dot_gate.go`,
  `crewstart.go`, `daemon.go`, `bootstate.go`, `bootsocket.go`, `bootworkloop.go`,
  `bootreconcile.go`, `Makefile`, `.golangci.yml`. A wave may contain at most one task that
  edits any given hot file. `workloop.go` took 74 commits in 30 days; `scheduler.go` 40.
- **Comment cleanup travels with the file.** A file being extracted is comment-cleaned by the
  extraction bead, not by the bulk lane. The bulk comment lane only touches files with no open
  extraction.
- **Never `cd` into a worktree.** Operate from the repo root with `git -C <abs path>`.
- **The daemon owns terminal transitions.** Never `br update --status=in_progress`, never
  `br close`. A bead pre-set to `in_progress` silently stops being dispatchable.
- **A bead that swaps one library call for another must state what the new call does on error
  and name every call site it was checked against.** This is the rule that would have caught the
  P0 in the Charlie audit: `runpkg.List` (skips an unreadable record) swapped for
  `runpkg.ScanRegistry` (all-or-nothing) inside a fail-open caller, which made the boot sweep
  force-remove live agents' worktrees and report success. That substitution hit five call sites.
  The planned decomposition is nothing but that class of change at scale.
- **Harness routing.**
  - **DGX (local nemotron)** — only where a wrong answer cannot compile or cannot change
    behaviour: comment text, doc comments, file headers, permission literals in `_test.go`,
    `gocritic whyNoLint` / `paramTypeCombine`, `unconvert`, `revive` missing-doc and
    unexported var-naming. Give it the exact file path and the exact change; it must never
    have to search. 85% of its turns in the measured run wrote nothing, and it read one file
    12 times.
  - **Codex** — compiler-proven mechanical work: `git mv` plus requalification, depguard rules,
    freeze-gate scripts, per-package test conversion.
  - **Claude** — gate changes, the composition-root split, the control-plane cut, god-function
    decomposition, anything on an error path, and every review.
- **Definition of done, by class.** Extraction: `go build ./...`, `go vet ./...`, the moved
  package's tests, `./internal/daemon` tests, a depguard rule denying `internal/daemon`, a
  freeze-gate grep, and a published test-second delta. Lint: the file+linter pair is absent from
  a fresh report *and* its line is deleted from `tools/lintreport/allow.txt` *and* the package's
  tests still pass. Comment: `tools/commentcut verify` clean, `gofumpt -l` empty, the changed-
  code-bytes report empty or human-read, and `make core` green.

---

# Wave 0A — Unblock the gates and the tools

Nothing in Wave 1 onward can land until T01 exists. These run concurrently; scopes are disjoint.

### T01 — Make the lint allow list survive a file rename
- **What.** `tools/lintreport` keys the allow list on `filepath.ToSlash(iss.Pos.Filename)` plus
  linter, so a moved file loses its grandfathering and the ratchet blocks re-granting it. Add
  rename following (git rename detection, or content-keyed entries), or re-seed the list at a
  designated split commit and advance the ratchet's base in the same change. This changes a
  merge gate — put the choice to the operator with the number attached (249 daemon pairs).
- **Done when.** Moving `internal/daemon/pasteinject.go` to a new package in a scratch tree
  produces zero not-allowed pairs and `go run ./tools/lintreport -allow tools/lintreport/allow.txt`
  exits 0; `scripts/lint-allow-ratchet.sh` still fails on a genuinely added pair.
- **P0 / M / Claude.** Depends on nothing. Blocks every extraction task.
- **Scope.** `tools/lintreport/**`, `scripts/lint-allow-ratchet.sh`.

### T02 — Build tools/commentcut with a third, byte-level check
- **What.** Port the working prototype: `list` (emit every non-directive comment block as file,
  start, end, size, attachment kind, text), `apply` (delete listed line ranges from the original
  bytes — never `go/printer` — then `gofumpt -w` and `gci write`), `verify` (refuse to write if
  the `go/scanner` token stream changed). Add the third check the prototype lacks: report every
  **code** line whose bytes changed after formatting, and treat a non-empty list as something a
  human reads before the commit lands. Preserve, by trimmed-prefix match and never by substring
  grep: `//go:build`, `// +build`, `//go:embed`, `//go:generate`, `//nolint`, `//lint:ignore`,
  `//revive:`, `//go:linkname`, `//go:noinline`, `//line`, `//export`, `// Deprecated:`,
  `// Code generated`, and this repo's own `//cloexec:`. Also preserve package docs, doc comments
  on exported declarations, **and struct-field doc comments**. Fix the prototype's block-comment
  truncation defect (truncating `/* ... */` deletes the closing delimiter and silently drops the
  file from the output set).
- **Done when.** Run over `internal/`, `cmd/`, `tools/`, `test/` (never `evaltasks/`, never
  `testdata/`): zero parse failures, zero token divergences, `gofumpt -l` empty, `go build ./...`
  and `go vet ./...` exit 0, and `go test -short ./...` is no worse than baseline.
- **P0 / M / Claude.** Depends on nothing. Blocks every comment task.
- **Scope.** `tools/commentcut/**` (new).

### T03 — Fix the composite-literal realignment test break
- **What.** `internal/daemon/conformance_m4c7_test.go` asserts `strings.Contains(hrSrc, "Runner: rc.Runner")`.
  Deleting a comment inside the composite literal in `harnessregistry.go` makes gofmt re-pad the
  column to `Runner:       rc.Runner,` and the test fails. Either relax the assertion to be
  whitespace-insensitive, or teach `commentcut` to leave alone any comment inside a composite
  literal, struct type or const block. Do both if cheap.
- **Done when.** `commentcut` in in-body-only mode over the whole tree leaves
  `TestM4C7_SeamSurvival_StructuralFloors` green.
- **P0 / S / Claude.** Depends on T02.
- **Scope.** `internal/daemon/conformance_m4c7_test.go`, `tools/commentcut/**`.

### T04 — Parallelize freeze gates and add a diff-scoped gate target
- **What.** Two Makefile changes in one bead because they share the file. (a) Run the 18 freeze
  and ratchet scripts under `xargs -P 8` — measured 21.8s serial to 4.7s, green. (b) Add a
  diff-scoped gate target that reads `HK_GATE_BASE_SHA` (already exported into the gate
  environment by `dot_cascade_core.go`, currently read by nothing), computes the affected package
  set from one `go list`, intersects with `CORE_PKGS`, and **falls back to the whole `CORE_PKGS`
  list on any doubt**: no base SHA, a failing `go list`, a changed path mapping to no package, a
  changed non-Go path outside an explicit allowlist, or an empty set from a non-empty Go diff.
  Include the path-to-package table for couplings the import graph cannot see: `specs/**` →
  `internal/specaudit`; `cmd/harmonik/**` → also `internal/daemon`; `*.dot` → `internal/workflow`,
  `internal/workflow/dot`, `internal/daemon`; `Makefile` / `scripts/` / `go.mod` / `.golangci.yml`
  → everything. Leave `make core` itself unchanged — it is the assessor's sign-off gate and must
  stay one name for one thing.
- **Done when.** `make freeze-gates` exits 0 in under 6s; the new target on a diff touching only
  `internal/queue` runs `internal/queue` and its reverse-dependency closure and nothing else;
  every fallback branch is unit-tested and widens rather than narrows.
- **P1 / M / Claude.** Depends on nothing.
- **Scope.** `Makefile`, `tools/gatescope/**` (new), `scripts/` (new script only).

### T05 — Skip the test step when no compiled code changed
- **What.** Two cases, decided in under a second: no `.go` file changed at all (111 of the last
  400 commits), or every changed `.go` file is identical to its base after non-directive comments
  are stripped. Keep fmt-check, build, vet and the freeze gates; skip only the tests. Treat
  `//go:`, `//nolint`, `// +build`, `//line` and `//cloexec:` as code. Widen to the normal path
  on any parse error.
- **Done when.** A `docs(...)`-only commit runs the static half and no tests; a commit labelled
  `docs(...)` that actually changes test code runs the full set. Both cases covered by a test.
- **P1 / M / Claude.** Depends on T04 (same Makefile), reuses T02's comment-stripping.
- **Scope.** `tools/gatescope/**`, `Makefile`.

### T06 — Warm the run worktree Go build cache off the gate clock
- **What.** `scripts/with-lane-gocache.sh` keys `GOCACHE` on the worktree root, so every bead
  pays 25.5s of cold compile on every gate attempt — 50-75s across a typical fix loop — and
  leaves ~500 MB behind that nothing reaps (8 caches, 6.2 GB on disk right now). After
  `git worktree add`, start `go test -run='^$' -count=1 <core pkgs>` in the background and let
  it fill while the implementer thinks. Reap the lane cache when the worktree is removed.
- **Done when.** A freshly created run worktree's first `make core` shows no cold-compile
  penalty; `~/Library/Caches/harmonik-lane-gocache` has no entry for a removed worktree.
- **P1 / M / Claude.** Depends on nothing.
- **Scope.** `internal/workspace/createworktree.go`, `internal/workspace/` removal path,
  `scripts/with-lane-gocache.sh`.

### T07 — Turn on prefix caching on the DGX endpoint
- **What.** Every `message_end` usage record in the measured Pi run reported `cacheRead:0,
  cacheWrite:0` across all 22 sampled turns. Per-turn latency grows linearly with conversation
  length and the fixed ~27,700-token preamble is re-prefilled every turn (37 of 108 minutes).
  Enable automatic prefix caching on the vLLM server. This is an ops change, not a repo change.
- **Done when.** `cacheRead` is non-zero in a run's stdout log, and a re-run of one identical
  bead is materially faster than the 107-minute baseline.
- **P0 / XS / operator.** Depends on nothing. Multiplies the value of every DGX task below.

### T08 — Give the Pi harness a small context, a turn budget, and a working Go
- **What.** Three fixes in one bead because they share `internal/harness/pi/launchspec.go`.
  (a) Point the Pi worktree at a short worktree `AGENTS.md` — worktree discipline, the commit
  trailer, the build command — instead of the 24 KB router file; at ~1,470 tokens/sec prefill
  and 115 turns, every 1,000 preamble tokens costs ~80 seconds per run. (b) Add a per-iteration
  turn budget (~25) alongside the existing 90-minute ceiling in `pasteinject.go`, and state the
  remaining count in the resume prompt; the measured run spent 19 consecutive turns and 11
  minutes on a `which go` / `GOTOOLCHAIN` / `brew install go@1.25` detour that a wall-clock
  ceiling cannot stop. (c) Fix the launch environment so `go` resolves without a `$HOME` hunt.
- **Done when.** A dispatched Pi bead shows a preamble under 8,000 tokens, terminates at the
  turn budget with a reviewable commit rather than running to the wall clock, and never probes
  for the Go toolchain.
- **P1 / M / Claude.** Depends on nothing.
- **Scope.** `internal/harness/pi/**`, the Pi worktree `AGENTS.md` template.

### T09 — Require a build check in the same turn as every edit on Pi
- **What.** The measured run misspelled an identifier as `Review-Vertict` at three of four sites,
  broke `go build`, and paid a 23-minute iteration round trip for it. Make the seed prompt
  require `go build` in the same turn as an edit, and pre-attach the target file's relevant span
  to `agent-task.md` rather than making the model read it — 32.5 minutes of that run was
  re-sending tool output the daemon could have placed in the prompt once.
- **Done when.** A deliberately introduced typo is caught and fixed inside one iteration.
- **P1 / S / Claude.** Depends on T08.
- **Scope.** `internal/harness/pi/launchspec.go`, the Pi seed-prompt template.

### T10 — Publish the daemon scoreboard after every merge
- **What.** A script that prints, for the current commit: `internal/daemon` production line
  count, its test line count, per-package test seconds for `CORE_PKGS`, and the count of
  remaining `Exported*` test shims (140 today). Append one row per merge to a tracked file.
- **Done when.** `scripts/daemon-scoreboard.sh` runs in under 10 seconds for the line/shim
  counts, has a `--with-tests` mode for the timing pass, and the tracked file has its first row
  at `43ad681c4`: 49,370 production lines, 95,594 test lines, 294.5s, 140 shims.
- **P1 / S / DGX.** Depends on nothing.
- **Scope.** `scripts/daemon-scoreboard.sh` (new), `plans/2026-08-22-decomposition-program/SCOREBOARD.md` (new).

### T11 — Land the measured safe autofix, tree-wide
- **What.** `golangci-lint run --fix --no-config --default=none -E copyloopvar,errorlint,testifylint,nakedret,misspell ./...`
  then `gofumpt -w` on the changed files, then delete the 18 now-empty lines from
  `tools/lintreport/allow.txt` plus the stale `internal/daemon/sandboxgate.go gocritic` entry.
  Measured end to end in a scratch export: 18 files, +9/-25 lines, build clean, vet clean, format
  clean, 25 findings cleared. **Do not** run bare `--fix` with the repo config — it emits
  `bytes.Equal` without the import and produces a tree that does not compile.
- **Done when.** `make core` green, `go run ./tools/lintreport` exits 0 with 19 fewer allow lines.
- **P1 / S / DGX.** Depends on nothing. **EXCLUSIVE — tree-wide, blocks all concurrent beads.**
- **Scope.** 18 files tree-wide plus `tools/lintreport/allow.txt`.

### T12 — Write the lint rule sheet for the local model
- **What.** One page, and the two measured traps go at the top: `_ = f()` does **not** clear an
  errcheck finding here (`check-blank: true` and `check-type-assertions: true` in `.golangci.yml`);
  `golangci-lint --fix` with the repo config is banned. Then the allowed classes, the forbidden
  classes (`prealloc`, `unused`, `forbidigo`, gocritic append/shadow, revive stutter), the
  `//nolint:<linter> // <explanation>` form (nolintlint requires an explanation, requires a
  specific linter, and forbids unused directives), and the production permission-literal rule:
  `internal/lifecycle/harmonikdirmode_test.go` forbids hand-written mode literals in non-excluded
  packages and demands `core.HarmonikDirMode`, with `//dirmode:allow <reason>` as the only
  escape and several `0o700` credential dirs marked "never widen one of these".
- **Done when.** The sheet exists and is referenced by every generated lint bead body.
- **P0 / S / Claude.** Depends on nothing. Blocks T60-T63.
- **Scope.** `plans/2026-08-22-decomposition-program/LINT-RULES.md` (new).

### T13 — Fix the crew subscribe filter and delete the false isolation claim
- **What.** The crew-launch skill says routing off `main` "keeps crews isolated" because
  otherwise "your monitor sees runs you did not submit". That is false — the daemon broadcasts
  every run event to every subscriber. Replace it with what a named queue actually buys (a
  separate pause blast radius, a separate per-queue worker cap, a `queue_id` to filter on) and
  add the client-side filter: ignore any `run_completed` / `run_failed` line whose `queue_id` is
  not your queue's. **Do not use `owning_epic_assignee`** — it is empty in all 2,509 terminal
  events in the live log. `run_stale` carries no `queue_id`, so surface unattributable stale
  lines rather than dropping them. Edit `cmd/harmonik/assets/skills/crew-launch/SKILL.md` and
  mirror byte-for-byte into `.claude/skills/crew-launch/SKILL.md` in the same commit.
- **Done when.** Both copies are byte-identical and a crew following the skill reacts only to
  its own completions plus every stale line.
- **P0 / S / Claude.** Depends on nothing. Blocks the crews starting.
- **Scope.** `cmd/harmonik/assets/skills/crew-launch/SKILL.md`, `.claude/skills/crew-launch/SKILL.md`.

### T14 — Decide eager-refill before the crews start
- **What.** `eagerRefillEval` is live on this box (kerf is on PATH at `/Users/gb/go/bin/kerf`;
  `--no-auto-pull` does not disable it). It over-fetches candidates from `kerf next` — the one
  ranking source this project's own docs say never reads bead priority — and tops up only the
  first active stream group it finds, not each crew's queue. If the planning crew owns what gets
  queued, set `HARMONIK_DISABLE_EAGER_REFILL=1` on the daemon. If auto-fill is wanted, change the
  candidate source to `br ready --sort priority --limit 0` and make it top up every active stream
  group.
- **Done when.** Either the daemon runs with the disable set and a test confirms no injection, or
  the candidate source is changed and covered by a test.
- **P0 / S / Claude.** Depends on nothing. Blocks the crews starting.
- **Scope.** `internal/daemon/eagerfill_em063.go`, `internal/orchestrator/`, daemon launch config.

### T15 — Mark the stale planning documents historical
- **What.** Three documents will actively mislead a crew and must carry a header saying so
  before anyone reads them. `plans/2026-07-21-p2-extraction/E5-CHUNK-CATALOGUE.md` is written in
  file-plus-line coordinates against `reviewloop.go`, `dot_cascade.go`, `export_test.go` and
  `workLoopDeps` — all four gone. The per-group budget table in
  `plans/2026-07-27-delete-and-rewrite/DECOMPOSITION-MAP.md` is disowned by its own document and
  is three weeks and 5,841 lines stale. `docs/foundation/project-level/subsystem-organization.md`
  names six packages that do not exist (`internal/agentrunner`, `internal/memory`,
  `internal/improvement`, `internal/adapter/br`, `internal/adapter/ntm`,
  `internal/handler/contract`) and asserts a composition-root property that Charlie task C27
  exists to create. Also re-measure the function-size table in `DECOMPOSITION-MAP.md` and record
  the tip commit beside each number — four of five have drifted since 2026-07-31
  (`runAgentLaunch` 558 → 835, `dispatchDotAgenticNode` 544 → 688, `beadRunOne` 1,705 → 959).
- **Done when.** Each of the three carries a dated "historical, do not plan from this" header and
  the function table names its measurement commit.
- **P1 / S / DGX.** Depends on nothing.
- **Scope.** the three named files.

### T16 — Record what the Charlie merge actually landed
- **What.** One short tracked document, committed with a real review. It must say: 62 of the 64
  commits in the merged range name reviewers this repo has no skill for ("Kierkegaard" x51,
  "Avicenna" x10) and cannot be traced to a review that happened; the merge gate was red across
  the whole run and was never once executed until three cleanup commits on 2026-08-21/22; 11
  replay symbols are now blessed as production-unreachable in `scripts/reachability.baseline`
  with no reachable path to retire the durable state they would write; and the two defects a
  human audit found — the boot sweep force-removing live agents' worktrees, and 53 broken tests.
  Bead notes are machine-local and gitignored, so a tracked file is the only record that travels.
- **Done when.** The file exists on the integration branch with an `agent-reviewer` trailer.
- **P1 / S / Claude.** Depends on nothing.
- **Scope.** `plans/2026-08-22-decomposition-program/CHARLIE-MERGE-RECORD.md` (new).

### T17 — Forbid staffing the dispatch-replay producer
- **What.** Write the prohibition into the crew mission text, not only into a bead. A planning
  crew reading `CHARLIE-BACKLOG.md` will otherwise see C21 producer wiring as the obvious next
  item, and turning it on wedges the daemon into a permanent boot-abort loop the supervisor
  revives forever. State the two other blockers so nobody re-derives them: the replay reader's
  `readObservations` returns literal `ClaimNone` and `GitAbsent`, so replay cannot distinguish
  "the claim never happened" from "the claim succeeded then we crashed", and the coherence check
  that would stop it re-provisioning a run whose work already landed can never fire.
- **Done when.** The prohibition appears in the mission file for both crews and in this plan's
  README.
- **P0 / S / Claude.** Depends on nothing.
- **Scope.** `.harmonik/crew/missions/*.md`, `plans/2026-08-22-decomposition-program/README.md`.

### T18 — Run the full merge gate once, unattended, and log the exit code
- **What.** `make full` covers 110 packages; the verified run covered 29. Log the exit code to a
  file rather than reading a shell variable. Record the result in the scoreboard.
- **Done when.** An exit code and a package-level report exist for `43ad681c4`.
- **P1 / M / Codex.** Depends on nothing. Runs unattended, 30+ minutes.
- **Scope.** none (read-only run) plus one scoreboard row.

# Wave 0B — Queue and notification plumbing

Depends on Wave 0A landing. Disjoint from each other.

### T19 — Put the queue name on run terminal events and add a --queue filter
- **What.** The daemon already holds the queue name (`RunHandle.QueueName`, `capturedQueueName`
  in the scheduler) and declines to publish it. Add it to the wire payload — which is
  `workloopRunCompletedPayload` in `internal/daemon/workloop.go`, one struct serving both
  `run_completed` and `run_failed` discriminated by a `success` bool, **not** the `core.*` spec
  types; editing only the `core` types compiles and ships nothing. Add `queue_id` and the queue
  name to `RunStalePayload` too, which carries neither today. Then add a `--queue` field to
  `SubscribeRequest` and the CLI, applied in `subscriptionStream.offer` next to the existing
  `MatchAgentMessage` branch, and in the replay path which has the identical filter pair.
- **Done when.** `harmonik subscribe --queue foo` delivers only that queue's terminal events, a
  test pins it the way `TestSubscribeHub_DispatchFanOut` pins the type split, and the crew skill
  from T13 is updated to use the server-side filter.
- **P1 / M / Claude.** Depends on T13.
- **Scope.** `internal/daemon/workloop.go`, `internal/daemon/subscribe.go`,
  `internal/core/eventreg_wkzlc.go`, `cmd/harmonik/subscribe.go`, the two crew-launch skill copies.
  **Touches hot file `workloop.go` — exclusive against Wave 4-6.**

### T20 — Find out why owning_epic_assignee is always empty
- **What.** The field has never been populated in 2,509 terminal events, 1,420 of them after the
  feature landed on 2026-06-11. The ledger holds 176 parent-child edges and 56 of the 872
  post-feature dispatched beads have one; the edge direction the resolver expects
  (`resolveOwningEpicFromRecord` in `workloop.go`) matches how the ledger stores them; both
  dispatch paths rehydrate from `ShowBead`, which populates `Edges`. Data and code path both
  exist and the output is still empty. Find the break. One caveat to rule out: some edges may
  have been added after their bead was dispatched.
- **Done when.** Either a fix lands with a test, or a written finding says why the field cannot
  be populated and the field is removed from the payload rather than left as a trap.
- **P2 / M / Claude.** Depends on T19 (same file).
- **Scope.** `internal/daemon/workloop.go` (after T19), `internal/brcli/`.

# Wave 1 — The keystone

**One daemon task, exclusive.** Everything in Waves 3-8 gets cheaper after it. The non-daemon
lanes (T60, T70) run concurrently.

### T21 — Lift runregistry.go into its own package
- **What.** Move `internal/daemon/runregistry.go` (393 lines, zero outbound references to any
  other daemon file, imports only stdlib + `internal/core` + `internal/handlercontract`) to
  `internal/runregistry`. This was verified end to end in a scratch copy: the whole tree builds
  and vets clean and the targeted tests pass. **The export cost is 2, not 0** — the compiler
  finds both. `snapshotWithKeys()` is an unexported method called from `stalewatch.go`,
  `handlerpause_policy_37zy8.go` and `stategather.go`, and the `aborted` field on `RunHandle` is
  written directly as `handle.aborted.Store(true)` at two sites in `stalewatch.go` plus one test
  shim (the `Aborted()` getter exists; the setter does not). Real churn: 21 production files and
  26 test files. Add the depguard rule (allow `$gostd`, `core`, `handlercontract`, self; deny
  `internal/daemon`) and a freeze-gate grep modelled on `scripts/runmerge-freeze-gate.sh` in the
  same commit. `internal/runloop` already declares the consumer-side port it satisfies.
- **Done when.** `go build ./...` and `go vet ./...` clean tree-wide including all 304 daemon
  test files; `go list -deps ./internal/runregistry | grep internal/daemon` is empty; the freeze
  gate is wired into `check-fast` and `check-short`; scoreboard row published.
- **P0 / M / Codex.** Depends on T01.
- **Scope.** `internal/daemon/runregistry.go` → `internal/runregistry/`, 21 daemon production
  files, 26 daemon test files, `.golangci.yml`, `scripts/runregistry-freeze-gate.sh`, `Makefile`.
  **EXCLUSIVE within `internal/daemon`.**
- **Why first.** Five other clusters — handler-pause, stale-watch, sweeps, spend meters and the
  control plane — name `RunRegistry` or `RunHandle` as their only remaining tie to the rest of
  the package. Moving 393 lines turns five multi-edge extractions into near-zero-edge ones.

# Wave 2 — The composition root

**Two daemon tasks, serialized against each other and exclusive within the package.** This is
the enabling move: without it every parallel extraction in Wave 3 collides on the same five boot
files. Charlie backlog task C27 covers the same ground.

### T22 — Split daemon boot wiring into one file per cluster
- **What.** `bootState` has 39 methods and holds references to nearly every file in the package;
  the boot files account for 77 of roughly 170 cross-cluster file edges. Give each cluster one
  `bootwire_<cluster>.go` holding its construction, and reduce `bootstate.go`, `bootsocket.go`,
  `bootworkloop.go`, `bootreconcile.go` and `daemon.go` to calling those. No behaviour change:
  same construction order, same singletons, same error paths. Clusters to give a wiring file:
  schedules, preflights, spend, pause, sweeps, stale-watch, harness-pick, branching, control
  plane, substrate, dot-cascade, scheduler.
- **Done when.** `make core` green; each subsequent extraction can requalify its call sites by
  editing exactly one `bootwire_*.go`; a test asserts boot order is unchanged.
- **P0 / L / Claude.** Depends on T21.
- **Scope.** `internal/daemon/bootstate.go`, `bootsocket.go`, `bootworkloop.go`,
  `bootreconcile.go`, `daemon.go`, new `bootwire_*.go`. **EXCLUSIVE within `internal/daemon`.**

### T23 — Split daemon.Config into one config struct per cluster
- **What.** 40 exported fields across 547 lines, read by 23 production files. No cluster can be
  constructed independently until it is split. One struct per cluster, assembled at the four
  existing assembly sites. `.kerf/works/daemon-config-construction/` holds the problem statement
  (Charlie backlog C27, decomposition-map Step 27a); it has a problem space and a spec and no
  design, so this needs a kerf design pass first.
- **Done when.** Each `bootwire_*.go` from T22 takes only its own cluster's config struct;
  `make core` green.
- **P1 / L / Claude.** Depends on T22.
- **Scope.** `internal/daemon/daemon.go`, `bootwire_*.go`, the 23 config-reading files.
  **EXCLUSIVE within `internal/daemon`.**

# Wave 3 — The free leaves (4 concurrent)

Verified non-overlapping after T22: each touches its own cluster files plus exactly one
`bootwire_*.go`, plus at most one other file.

### T24 — Extract branching.go to internal/taskbranching
- **What.** 748 lines. Measured inbound call sites: `workloop_runplan.go` only.
- **Done when.** Extraction definition of done (see Rules), depguard rule, freeze gate.
- **P1 / S / Codex.** Depends on T22.
- **Scope.** `internal/daemon/branching.go`, `workloop_runplan.go`, `bootwire_branching.go`,
  `.golangci.yml`, new package, new freeze gate.

### T25 — Extract the three schedule registrations to internal/schedreg
- **What.** `watch_liveness_schedule.go` (113), `opsmonitor_schedule.go` (77),
  `ctx_watchdog_schedule.go` (74) = 264 lines. Measured inbound: `bootworkloop.go` only.
- **P1 / S / Codex.** Depends on T22.
- **Scope.** those three files, `bootwire_schedules.go`, `.golangci.yml`, new package, freeze gate.

### T26 — Extract the startup preflights to internal/preflight
- **What.** `walcheckpoint.go` (150), `beadsmergedriver.go` (86), `brhistoryrotate.go` (345) =
  581 lines. Measured inbound: `daemon.go` only.
- **P1 / S / Codex.** Depends on T22.
- **Scope.** those three files, `bootwire_preflight.go`, `.golangci.yml`, new package, freeze gate.

### T27 — Extract the two spend meters to internal/spend
- **What.** `spendmeter_hkk3f8g.go` (291), `perqueuespendmeter_tigaf11.go` (347) = 638 lines.
  Measured inbound: `bootstate.go` and `daemon.go`, both resolved by T22. Zero unexported
  symbols cross the boundary. Note `perqueuespendmeter_tigaf11.go` also references handler-pause,
  so this must land **before** T28 or the two collide.
- **P1 / S / Codex.** Depends on T22. Blocks T28.
- **Scope.** those two files, `bootwire_spend.go`, `.golangci.yml`, new package, freeze gate.

### T28 — Extract harness resolution to internal/harnesspick
- **Listed here for its cluster, but it RUNS IN WAVE 6.** See below.
- **What.** `harnessregistry.go` (381), `harnessresolve.go` (188), `modelpreference.go` (253),
  `pi_profile_resolve.go` (150), `moderesolve.go` (246) = 1,218 lines, **zero outbound references**
  — verified by moving it and reading the compiler. But 12 of its 13 inbound symbols are
  unexported, and its measured call sites are `agentlaunch.go`, `crewstart.go`,
  `dot_cascade_core.go`, `dot_gate.go`, `runports.go`, `sandboxgate.go`, `scheduler.go`,
  `workloop.go`, `workloop_runplan.go`. That is the widest call-site footprint of any leaf.
- **P1 / M / Codex.** Depends on T22, T31 (stale-watch), T30 (sweeps) — see Wave 6.
- **Scope.** those five files plus the nine call-site files plus `bootwire_harnesspick.go`.
  **EXCLUSIVE against every other daemon task.**

# Wave 4 — Two mid-size clusters (2 concurrent)

Verified disjoint after T22: handler-pause touches `{dispatchports, hookrelay_chb025,
perqueuespendmeter, quiesce, scheduler, wiringlog}`; sweeps touches `{loopmaintenance, workloop}`.

### T29 — Extract handler-pause to internal/pause
- **What.** `handlerpause_9hwbw.go` (929), `handlerpause_persist_m0k0a.go` (440),
  `handlerpause_policy_37zy8.go` (284), `handlerpause_autoresume_0otqs.go` (199),
  `handlerpause_sigusr1_bdvae.go` (109), `workloop_handlerpause_kac8g.go` (95) = 2,056 lines.
  Measured outbound after T21: 1 symbol (`dispatchGatesPort`). 8 inbound, 2 unexported.
- **P1 / M / Codex.** Depends on T22, T27.
- **Scope.** those six files, `dispatchports.go`, `hookrelay_chb025.go`,
  `perqueuespendmeter_tigaf11.go` (or its new home), `quiesce.go`, `scheduler.go`, `wiringlog.go`,
  `bootwire_pause.go`. **Holds hot file `scheduler.go`.**

### T30 — Extract the sweeps to internal/sweeps
- **What.** `orphansweep.go` (1,340), `claudeworktreesweep.go` (321), `branchreapwatcher.go`
  (125), `diskcheck_hksxlb.go` (288) = 2,074 lines. Measured outbound: `RunRegistry` (gone after
  T21), two disk constants in `workloop.go`, `loopMaintenanceState`. 7 of 14 inbound symbols are
  unexported. **Read the P0 note in the Rules section before touching the orphan sweep** — this
  is the code that force-removed live agents' worktrees.
- **P1 / M / Codex.** Depends on T22.
- **Scope.** those four files, `loopmaintenance.go`, `workloop.go`, `bootwire_sweeps.go`.
  **Holds hot file `workloop.go`.**

# Wave 5 — Stale watch

### T31 — Extract the stale watcher to internal/stalewatch
- **What.** `stalewatch.go` (1,601), `stallfeed.go` (323), `pollgate_hkw6q7.go` (66) = 1,990
  lines. Measured outbound: `RunRegistry`, `RunHandle` (both gone after T21), `ActivityLabel`,
  `ActivityInactive`, `LiveStateBuilder`. 1 of 5 inbound symbols is unexported. `LiveStateBuilder`
  lives in the control-plane cluster, so either land T32 first or invert that one dependency
  behind a consumer-owned interface. It also holds the package-level test seam
  `execGitRevParse`, which stops being reachable from `package daemon` tests once the file moves
  — convert those tests with it.
- **P1 / M / Codex.** Depends on T21, T22. Serialized against T29/T30 (shares `scheduler.go`,
  `workloop.go`, `agentlaunch.go`, `quiesce.go`).
- **Scope.** those three files, `agentlaunch.go`, `bandwidthtuner.go`, `quiesce.go`,
  `scheduler.go`, `workloop.go`, `bootwire_stalewatch.go`. **EXCLUSIVE within `internal/daemon`.**

# Wave 6 — Harness resolution

### T28 runs here (see Wave 3 entry for detail)
Serialized after T29, T30 and T31 because it shares `agentlaunch.go`, `scheduler.go`,
`workloop.go` and `workloop_runplan.go` with them.

# Wave 7 — The control plane

The largest clean cut in the package and the one with a second payoff: the charter already puts
the socket listener, comms, dashboard, live-state and subscribe outside the core set, so a
package left out of `CORE_PKGS` narrows what a merge is gated on.

### T32 — Free ConcurrencyController and dashboardGateEvalInterval
- **What.** Two named symbols block the control-plane cut and neither is in any prior plan.
  `dashboardGateEvalInterval` is a constant in `workloop.go` — the file with 74 commits in 30
  days, so touching it collides with the busiest lane. `ConcurrencyController` is a 52-line file
  also consumed by `scheduler.go` and `bandwidthtuner.go`; it must become a shared leaf package
  or be inverted behind a small consumer-owned interface in `stategather.go`.
- **Done when.** Neither symbol is referenced across the intended package boundary.
- **P1 / S / Claude.** Depends on T22. Serialized against T30 (`workloop.go`) and T29
  (`scheduler.go`).
- **Scope.** `internal/daemon/concurrencycontroller.go`, `workloop.go`, `scheduler.go`,
  `bandwidthtuner.go`, `stategather.go`.

### T33 — Extract the control plane to internal/daemoncontrol
- **What.** **30 files, 8,370 lines — not the 24-file, 6,432-line cut a narrower reading
  suggests.** The narrow cut was attempted in a scratch tree and produces a hard Go import cycle:
  the new package needs 16 symbols from daemon and daemon needs 34 back. Extracting a separate
  `fleetstate` package first does not break it, because `quiesce.go` itself needs
  `AgentMessagePayload` from the comms files. The cycle-free width is one package containing:
  `socket.go`, `socketdispatch.go`, `socket_dashboard.go`, `socket_state.go`, `subscribe.go`,
  `notifystream.go`, `commscursor.go`, `commshandler_nbrmf.go`, `commsrecvhandler_nnwaa.go`,
  `commspresencehandler_7t27s.go`, `decision_block_ev043a.go`, `decisionshandler_k4_kba.go`,
  `decisionshandler_xz9.go`, `decisionsprojection.go`, `dashboardgather.go`, `dashboardgate.go`,
  `dashboardtypes.go`, `stategather.go`, `statedisk.go`, `statetypes.go`, `operatorpause.go`,
  `queuerecover.go`, `session_start_ack.go`, `hookrelay_chb025.go`, **plus** `quiesce.go`,
  `draindetect.go`, `draindetect_epic.go`, `agent_message.go`, `verdictoverride.go`,
  `dispatch_target_probe.go`. At that width the residual outbound is exactly four symbols, two of
  which T21 already removes and two of which T32 removes. Export cost: 11 unexported of 40
  inbound. **All 27 control-plane test files (8,715 lines) are external `package daemon_test` and
  none use the shared full-daemon harness — they travel at zero cost.**
- **Done when.** Extraction definition of done, plus: the new package is **not** added to
  `CORE_PKGS`, and a scoreboard row shows the test-second delta.
- **P1 / L / Claude.** Depends on T21, T22, T29, T31, T32.
- **Scope.** the 30 files above plus their 27 test files, `bootwire_control.go`, `.golangci.yml`,
  `Makefile`, new freeze gate. **EXCLUSIVE within `internal/daemon`.**

# Wave 8 — The terminal substrate

The biggest single line-count prize: 7,283 lines with exactly one outbound edge (the
`windowCleaner` interface declared in `scheduler.go`, which is consumer-owned so the consumer
keeps its copy). It is gated on a real Go constraint, not on effort.

### T34 — Land the substrate capability contract, tasks T1-T5
- **What.** Two of the five capability interfaces use unexported method names
  (`substrateWithAdapter` requires `tmuxAdapter()`, `substrateWithSessionName` requires
  `daemonSessionName()`), and a Go interface with an unexported method cannot be satisfied from
  another package. A finished, reviewed kerf design with an executable task list already exists
  at `.kerf/works/substrate-capability-contract/07-tasks.md` (176 lines, T1 onward, each with
  owner, spec trace, change, deliverables, acceptance and depends-on). Re-run the step's own
  probe command first: it counted 30 capability probes in 11 files on 2026-08-02 and the surface
  has since grown to 33 in 12. Convert those five kerf tasks into five beads verbatim.
- **Done when.** All 33 comma-ok capability probes are replaced by the contract and no capability
  interface requires an unexported method.
- **P1 / L / Claude.** Depends on T22.
- **Scope.** the 12 probe files plus `internal/handlercontract/`.

### T35 — Extract the terminal substrate to internal/agentsubstrate
- **What.** `tmuxsubstrate.go` (3,626), `pasteinject.go` (2,744), `sandboxprofile.go` (414),
  `sandboxgate.go` (346), `runsupport.go` (153) = 7,283 lines. 48 of its 52 inbound symbols are
  unexported — a large but compiler-proven rename job. Inbound edges are concentrated: boot 6,
  DOT cascade 6, launch 3, scheduler 3, socket 2, reconcile 1. It carries the package-level test
  seams `substrateRunnerObserver` and the three atomic delay knobs in `pasteinject.go`, which are
  set by `package daemon` tests and stop being reachable across a boundary — convert those tests
  with the move. **Comment-clean these files inside this bead** (`tmuxsubstrate.go` is 58%
  comment, `pasteinject.go` 54%) rather than leaving them to the bulk lane. Pulling this out also
  removes tmux from the daemon package's test closure — 70 daemon test files reference tmux today.
- **Done when.** Extraction definition of done plus a scoreboard row; the daemon test closure no
  longer requires tmux for the moved tests.
- **P1 / XL / Codex** (with a Claude reviewer on the rename diff). Depends on T34, T33.
- **Scope.** those five files plus their tests plus the concentrated call sites plus
  `bootwire_substrate.go`. **EXCLUSIVE within `internal/daemon`.**

# Wave 9 — In-place decomposition of the god functions

These are 4,947 lines, 10% of the package, and they carry the `nolint` suppressions that let the
complexity ceilings pass (`funlen` 100 lines / 60 statements, `cyclop` 15, `gocognit` 20, all
ratcheted with `--new-from-rev` and 104 functions grandfathered). Splitting files around them
does not make them smaller. **Do not extract the scheduler or the work loop as packages** — the
scheduler group references 25 symbols from outside itself including `beadRunOne`, `beadLedger`
and `strandedInProgressResetter` straight out of `workloop.go`, and `workloop.go` took 74 commits
in 30 days. These are the spine. Serialize all of Wave 9 behind a single writer.

Each of these is a high-value bead shape for unsupervised work because progress is one number:
lines in the function, or parameters in the signature.

### T36 — Name runWorkLoop's 18 locals as one loop-state struct
- **What.** `runWorkLoop` in `scheduler.go` is 1,432 lines, takes 22 positional parameters,
  reaches brace depth 11, and holds 18 top-level locals in one frame. The 18 locals *are* the
  state. Name them as a struct. No behaviour change. Its `//nolint:gocognit,cyclop,funlen`
  comment already records that a prior seam moved this code unchanged.
- **Done when.** The 18 locals are fields; `make core` green; `gocognit` for the function is
  reported before and after.
- **P1 / M / Claude.** Depends on T21-T33 landing (the loop's dependencies shrink first).
- **Scope.** `internal/daemon/scheduler.go`. **EXCLUSIVE.**

### T37 — Lift runWorkLoop's depth-3-and-deeper blocks into methods
- **What.** With the state struct in place, each nested block becomes a method on it, named for
  what it decides. Target: no block deeper than 4.
- **P1 / L / Claude.** Depends on T36.
- **Scope.** `internal/daemon/scheduler.go`. **EXCLUSIVE.**

### T38 — Pull pure queue selection out of runWorkLoop
- **What.** Charlie backlog task C22, unchanged in code: `selectNextQueue` still takes a
  `LockedQueueStore`, a `RunRegistry` and mutable maps. Move the decision into
  `internal/orchestrator` as a total pure function over typed facts, leave the effects in the
  shell. Bead `hk-dstwt` is the right shape and already open.
- **P1 / M / Claude.** Depends on T37.
- **Scope.** `internal/daemon/scheduler.go`, `internal/orchestrator/`.

### T39 — Reduce runWorkLoop's 22-parameter signature
- **What.** After T23 (per-cluster config) and T38, most parameters are one cluster's handle.
  Group them. Target: under 8.
- **P2 / M / Claude.** Depends on T23, T38.
- **Scope.** `internal/daemon/scheduler.go`, `bootwire_scheduler.go`.

### T40 — Decompose driveDotWorkflow
- **What.** 1,033 lines, 26 parameters, brace depth 9, in `dot_cascade_core.go`. Same treatment
  as T36/T37: name the state, lift the deep blocks. **Read the file header first** — it is the
  only record that two earlier execution engines were deleted and that a third copy of the review
  loop was written as late as 2026-07-24 and thrown away.
- **P1 / L / Claude.** Depends on T28 (harness pick leaves the file first).
- **Scope.** `internal/daemon/dot_cascade_core.go`. **EXCLUSIVE.**

### T41 — Decompose dispatchDotAgenticNode
- **What.** 688 lines and **32 parameters** — the widest signature in the package — in
  `dot_cascade_core.go`.
- **P1 / M / Claude.** Depends on T40.
- **Scope.** `internal/daemon/dot_cascade_core.go`. **EXCLUSIVE.**

### T42 — Lift dot_cascade_helpers.go out as an ordinary leaf
- **What.** 2,098 lines, deliberately created by the 2026-07-24 roadmap as "liftable as an
  ordinary leaf" and still sitting in `internal/daemon` a month later. The DOT cascade cluster
  has only 4 inbound edges from outside itself, and it carries 138 seconds of test time — the one
  extraction with a real test-time story.
- **P1 / L / Codex.** Depends on T40, T41.
- **Scope.** `internal/daemon/dot_cascade_helpers.go`, `dot_cascade_core.go`, `dot_gate.go`,
  `sub_workflow_runner.go`, new package. **EXCLUSIVE.**

### T43 — Decompose beadRunOne and runAgentLaunch
- **What.** `beadRunOne` is 959 lines in `workloop.go`; `runAgentLaunch` is 835 in
  `agentlaunch.go` and **grew from 558 since 2026-07-31**. Same treatment.
- **P2 / L / Claude.** Depends on T30, T31, T28.
- **Scope.** `internal/daemon/workloop.go`, `agentlaunch.go`. **EXCLUSIVE.**

# Wave 10 — Testability

These are the tasks that make every future extraction cheap rather than expensive. They can start
early on packages already extracted and must not run concurrently with an extraction on the same
files.

### T44 — Convert daemon white-box tests to external test packages
- **What.** 662 of the 1,149 daemon test functions are in `package daemon` across 157 files and
  reach unexported internals through 140 hand-written `Exported*` shims;
  `export_testruntime_test.go` alone is 542 lines and its own header says 157 files reference
  them. Every package boundary you draw breaks a few hundred tests at once. Delete each shim as
  its last caller goes. **One bead per cluster, never all at once.** Track "remaining `Exported*`
  shims" — it starts at 140.
- **Done when.** Per bead: that cluster's tests are external, its shims are deleted, `make core`
  green, the shim count published.
- **P0 / XL, split into ~12 beads / Codex.** Each cluster bead depends on that cluster's
  extraction bead, or precedes it — decide per cluster and state it in the bead.
- **Scope.** per cluster: that cluster's `*_test.go` files plus the `export_*_test.go` entries
  they use. **Never concurrent with an extraction on the same cluster.**

### T45 — Introduce a clock port and route the daemon's 86 time.Now() calls
- **What.** 86 direct `time.Now()` calls in daemon production code (`scheduler.go` 15,
  `workloop.go` 6, `tmuxsubstrate.go` 6, `quiesce.go` 5), plus 22 `time.After` /`NewTimer` /
  `NewTicker`. `internal/runloop` already meets a no-wall-clock rule with an injected clock port,
  so the pattern is established and a reviewer has something concrete to check against.
  **Per cluster, as that cluster is extracted** — not one sweep.
- **P1 / L, split per cluster / Claude.**
- **Scope.** per cluster.

### T46 — Introduce a process-runner port and route the 59 exec.Command calls
- **What.** 59 `exec.Command` calls in daemon production (`pasteinject.go` 8,
  `dot_cascade_helpers.go` 6, `scheduletick.go` 5, `branching.go` 5) and 107 direct `os` file-IO
  calls. This is why 70 daemon test files need tmux and 43 shell out to git, and it is the
  likeliest cause of most of the 294 seconds — the tests spawn real processes and create real git
  worktrees, they do not sleep (all declared test sleeps total roughly 10 seconds).
- **P1 / L, split per cluster / Claude.**
- **Scope.** per cluster.

### T47 — Retire the 43 package-level timeout knobs
- **What.** 43 package-level `var` timeout knobs, 16 in `pasteinject.go` alone
  (`commitPollTimeout`, `commitHardCeiling`, `reviewFileTimeout`, `launchHeartbeatTimeout`, …).
  No test assigns them directly — they are mutated through the `Exported*` shims — but they are
  still process-global, which is why so much of the suite cannot run in parallel despite 747
  `t.Parallel()` calls. Move each into its cluster's config struct from T23.
- **P2 / M / Codex.** Depends on T23, T44.
- **Scope.** per cluster.

### T48 — Do NOT raise -parallel and do NOT drop -count=1
- **What.** Record both as tried and refused so nobody re-tries them. `-parallel 32` gained 3%
  (284.9s vs 294.5s) **and turned two daemon-start tests red** that pass at the default
  (`TestDaemonStart_EmitsDaemonStarted`, `TestDaemonStart_DaemonStartedInJSONLLog`). Dropping
  `-count=1` works mechanically but 17 packages exec subprocesses whose content Go's cache does
  not hash, so it can report a stale PASS, and run worktrees start cold anyway.
- **P2 / XS / DGX.**
- **Scope.** `plans/2026-08-22-decomposition-program/TRIED-AND-REFUSED.md` (new).

# Wave 11 — Fences and hygiene, one per extracted package

### T49 — A depguard rule and freeze gate per extracted package
- **What.** Not a gate today — `.golangci.yml` uses `default: none` with no catch-all, and the
  `daemon:` rule already allows `github.com/gregberns/harmonik/internal/` wholesale, so a new
  package with no rule of its own is simply unguarded rather than a lint failure. But unguarded
  means it can import the monolith straight back and quietly undo the extraction. One rule per
  new package: allow list from the measured import set, deny `internal/daemon`. Plus a grep
  freeze gate modelled on the 14 that already exist, wired into `check-fast` and `check-short`.
  The depguard block is already 1,068 of `.golangci.yml`'s 1,265 lines across 55 component rules,
  so this is a fill-in-the-blanks edit. **`.golangci.yml` is a hot file — one such bead at a time.**
- **P1 / XS each, ~12 beads / Codex.** Each depends on its extraction bead.
- **Scope.** `.golangci.yml`, `scripts/<pkg>-freeze-gate.sh`, `Makefile`.

### T50 — A package comment and doc comments on every newly exported identifier
- **What.** revive's `exported` and `package-comments` rules are enabled, and they fire on a
  brand-new file — which is a new unlisted allow-list pair and an immediately red build. Part of
  the definition of done for every extraction, not a follow-up.
- **P1 / XS each / Codex** (folded into each extraction bead).

# Continuous lane A — Comment reduction

`internal/daemon` production is 41-42% comment by line: 20,860 of 49,370 lines. Half the
production comment mass sits in blocks of ten or more consecutive lines — essays, not doc
one-liners — and exported declarations average 6.2 doc lines each where the lint floor is one.
Under 2% of the daemon's comment volume is lint-required. Production comment text is ~5.5 MB,
roughly 1.4 million tokens; every agent reading `tmuxsubstrate.go` pays for 2,100 comment lines
to see 1,300 lines of code.

**Say plainly what this buys before spending on it:** no test-time win (comments contribute none
of the 294 seconds), no movement on the `funlen`/`cyclop`/`gocognit` ceilings (measured unchanged
in every mode). One real win: the seven largest daemon files shrink 40-52% under the full cut, or
10-44% under the safe in-body-only cut. That is context-window money for the decomposition crews.

### T51 — Add a comment norm to PRINCIPLES.md and the reviewer criteria
- **What.** Nothing in `PRINCIPLES.md`, the `agent-reviewer` skill or `build-practices.md` asks
  for comments at all — the 44.7% density is pure imitation of the resident corpus, and it is
  file-local: `internal/daemon` added comments at 44.9% of new lines against its own 43.9%
  resident baseline in the same period that greenfield packages landed at 1.8%. Add one line:
  comments explain a non-obvious why, a trap, or a refuted alternative; they do not restate the
  code, narrate a refactor, or cite a bead. **And explicitly sanction comment-reduction diffs**
  so the reviewer does not read a large deletion as removing documentation and return
  REQUEST_CHANGES — that is exactly the useless-churn loop to avoid.
- **P0 / S / Claude.** Blocks T52-T57.
- **Scope.** `PRINCIPLES.md`, `.claude/skills/agent-reviewer/SKILL.md` and its
  `cmd/harmonik/assets/skills/` source (byte-identical mirror required).

### T52 — Calibration pass: separators, spacers, exact duplicates
- **What.** 450 pure `// ----` / `// ────` separator lines, 10,594 bare `//` spacer lines, and
  4,881 exact-duplicate comment lines. Roughly 5,000-10,000 lines removable by regex with no
  judgment. Use it as the calibration task that proves the DGX pipeline end to end on work where
  a wrong answer is obvious and cheap.
- **Done when.** `commentcut verify` clean, changed-code-bytes report empty, `make core` green.
- **P1 / S / DGX.** Depends on T02, T03, T51.
- **Scope.** whole tree under `internal/`, `cmd/`, `tools/`, `test/`. **EXCLUSIVE.**

### T53 — Phase A: in-body comment blocks only, one commit per package
- **What.** Delete only comment blocks that float inside a function body. Every doc comment,
  every struct-field doc, every file-header block and every directive survives untouched.
  **Measured honestly: this removes 27,808 lines tree-wide (4.5%), not 70,891 (11.5%)** — the
  bigger number is the recommended mode, which breaks three tests in `internal/core`. Daemon
  file shrink under this mode: `dot_cascade_core.go` -44%, `workloop.go` -30%, `scheduler.go`
  -25%, `pasteinject.go` -16%, `tmuxsubstrate.go` -14%, `stalewatch.go` -13%,
  `dot_cascade_helpers.go` -10%.
- **Done when.** Per package: `commentcut verify` clean, `gofumpt -l` empty, changed-code-bytes
  report empty or human-read, `make core` green.
- **P1 / M, ~20 beads (one per package) / DGX.** Depends on T52. **Daemon packages only when no
  extraction owns the file — check the ownership table.**
- **Scope.** one package per bead.

### T54 — Phase B: model-judged block list for internal/daemon
- **What.** Feed the local model the **block list**, not files: one comment block plus the ~10
  lines of code it sits above, batched ~50 blocks per prompt, answer KEEP / TRIM / CUT. Then
  `commentcut apply` performs the accepted cuts and proves nothing else moved. That is ~43 model
  calls for the daemon's 2,151 multi-line blocks, versus 425 file rewrites. Per-file rewriting
  is four orders of magnitude slower — the deterministic pass did the whole tree in 10.6 seconds
  against an extrapolated 21+ hours — and still needs the token check to be trustworthy.
  Comment value is not a syntactic property, which is why the model judges and the tool executes:
  the recommended deterministic mode deletes the operational reason for the bounded submit-retry
  in `pasteinject.go` ("under concurrent cold-boots the splash takes >750ms to clear … the
  post-paste submit Enter lands on the splash and is SWALLOWED") in the same block as a throwaway
  line about a var being a var so tests can override it. A tool cannot tell those apart.
- **P2 / L / DGX judging, tool applying.** Depends on T53.
- **Scope.** per cluster, matching the ownership table.

### T55 — Delete bead-ID comment footers, keep spec references
- **What.** 6,056 comment lines across 439 production files cite a bead ID. `.beads/beads.db` is
  gitignored and machine-local, so none of them resolve from a fresh clone or in CI — a private
  tracking ID has been made the handle for the explanation, which is the exact thing the
  no-jargon rule forbids. Delete the 1,306 `// Bead ref:` / `// Bead:` footer lines outright and
  strip bare bead IDs from comments whose prose stands on its own. **Keep `// Spec ref:` (1,710
  lines)** — `specs/` is tracked, so those resolve.
- **P1 / M / DGX.** Depends on T51, T53.
- **Scope.** per package, matching the ownership table.

### T56 — Harvest the scar comments before splitting anything
- **What.** The highest-value prose in the repo is the comments that name an experiment that was
  tried and refuted, and they are the most likely thing to be lost or duplicated when a
  3,626-line file becomes six packages. Two examples to anchor the criterion:
  `internal/harness/claude/launchspec.go` records "LOCAL: DELIBERATELY NOT ISOLATED — do not
  re-add … LIVE-REFUTED by an A/B on one daemon with one line toggled: isolation ON →
  agent_ready_timeout at 150s with the pane parked on the Bypass Permissions modal; isolation OFF
  → agent_ready in 2.0s"; `internal/daemon/workloop.go` records "Do NOT add a pre-dispatch
  'already landed on main?' check here … the daemon closed a bead whose remaining work had not
  run". Collect them into a tracked document and point at it from the code rather than carrying
  20-line narratives inline into every new file.
- **P1 / M / Claude.** Depends on nothing. **Do before Wave 8.**
- **Scope.** `docs/` (new file), read-only over `internal/`.

### T57 — Fix the provable comment rot before any decomposition
- **What.** 50 `file.go:LINE` citations exist in comments (AGENTS.md forbids them) and 9 point
  past the end of the file they name: `socket.go:742` (file is 640 lines),
  `workloop.go:2079/3077/3099/3103/4077/4079/4127` (file is 1,630), `cycle.go:1349` (file is
  936). And `internal/daemon/doc.go` — the package doc for the largest package, 33 comment lines
  to 1 line of code — says startup steps "are added by follow-on beads hk-8mup.62 and
  hk-8i31.83". Both closed 2026-05-12, and the pidfile lock it lists as pending is implemented in
  `daemon.go` via `acquirePidfile`. These are wrong today and will be wronger after files split.
- **P1 / S / Claude.** Depends on nothing.
- **Scope.** `internal/daemon/doc.go` plus the 50 citation sites.

### T58 — Treat internal/core's event catalog as a design question, not bulk work
- **What.** `internal/core` is 59% comment — 17,221 lines — but 12,027 of them are attached to
  1,698 exported event types as structured banners (`Tags: mechanism`, `Axes:`,
  `Durability class:`, `# Payload fields`). At least two tests parse that text:
  `internal/handlercontract/cp051_skill_mechanism_tagged_test.go` requires a `Tags:.*mechanism`
  line in the godoc of named exported types (220 such lines exist), and three tests in
  `internal/core` require literal spec-clause strings in **struct-field** godoc on
  `Event.TimestampWall` and `Event.TimestampMonoNsec`. The real question for the operator: is
  this an event catalog that belongs in a generated file or a spec, rather than 17,221 lines of
  godoc? **Do not hand this package to the bulk lane.**
- **P2 / M / Claude, then operator decision.**
- **Scope.** decision document only.

### T59 — Add a comment-share ratchet
- **What.** New production lines are still 35.9% comment, down from 40.0% two months ago — real
  but far too slow to matter. Without a ratchet every line removed is re-added by the next crew.
  Report per-package production comment share and fail a new or heavily-changed file above a
  threshold, wired the way the complexity ceilings already are (`--new-from-rev`, existing code
  grandfathered).
- **P2 / M / Claude.** Depends on T53.
- **Scope.** `scripts/`, `Makefile`, `tools/commentcut`.

# Continuous lane B — Mechanical lint

The tier where a wrong answer cannot compile or cannot change behaviour is **~148 findings**:
`gosec` G301/G302/G306 in `_test.go` (72), `gocritic whyNoLint` (12), `gocritic paramTypeCombine`
(13), `unconvert` (8), `revive` missing-doc-comment (12), `revive` var-naming on unexported
identifiers (6), plus the 25 tool-autofixable from T11. Each is pure comment text, a permission
number inside a `t.TempDir` fixture, or a change the compiler rejects if wrong.

### T60 — Generate one bead per clearable (file, linter) pair
- **What.** Emit them mechanically from a fresh `golangci-lint run` JSON, restricted to the six
  safe classes above. Each bead carries the file, the linter, the exact finding lines, the rule
  sheet from T12, and one acceptance test: the pair is absent from a fresh report **and** its
  line is deleted from `tools/lintreport/allow.txt` **and** the package's tests still pass.
  Per-bead verification is `gofumpt` on the file (3.4s), `go build ./...`, `go vet ./<pkg>/`
  (0.5s), a warm whole-tree lint to JSON (3.8s) and the judge (0.1s) — not `make core` at 474s.
  Reserve the test suite for batch boundaries **except** where the class can change runtime
  behaviour, which none of the six can.
- **P1 / ~90 beads, XS each / DGX.** Depends on T01, T11, T12.
- **Scope.** one file per bead. Aim the first wave at `internal/daemon` — it holds 249 of the 580
  allow-list lines, and every pair cleared now is one fewer that fires when the file moves.

### T61 — Exclude the unsafe classes explicitly and say why
- **What.** `prealloc` (12) — the naive fix clears the finding, passes build and vet, and
  silently yields leading zero values; a live one in `diskcheck_hksxlb.go` feeds
  `git worktree remove --force --force` and would make the daemon's disk-low latch believe a
  reclaim succeeded when it reclaimed nothing. `forbidigo` (46 of 50 are deliberate panics:
  invariant assertions in `edgecascade_em042.go`, init-time `mustRegister` helpers in
  `internal/core`, constructor preconditions in `NewDetectorBarrier`). `unused` (66) — deleting a
  function is only safe once you know nothing reaches it through a build tag, a test or
  reflection. `revive` stutter (34 cross-package API renames). `gosec` G204 (45) and G304 (58) —
  a tool that shells out and reads variable paths will keep producing these; mark them permanent
  allow-list residents with a rationale line each and do not staff a crew against them.
- **P0 / S / Claude.** Depends on T12.
- **Scope.** `plans/2026-08-22-decomposition-program/LINT-RULES.md`, `tools/lintreport/allow.txt`
  (rationale comments only).

### T62 — Fix the 9 production permission literals correctly
- **What.** `internal/lifecycle/harmonikdirmode_test.go` forbids hand-written mode literals in
  non-excluded packages that build a `.harmonik` path and demands `core.HarmonikDirMode` (0o750),
  with `//dirmode:allow <reason>` the only escape and several 0o700 credential directories marked
  "tighter on purpose: never widen one of these". The naive "change 0755 to 0750" is wrong here.
  The conformance scan skips `_test.go`, which is why the other 72 are safe for T60.
- **P2 / S / Claude.** Depends on T12.
- **Scope.** the 9 production sites.

### T63 — Take the complexity findings off the mechanical backlog
- **What.** `gocognit` 187 (median 32, p90 80, max 260 — `cmd/harmonik/main.go run` at 260,
  `pasteinject.go pasteInjectQuitOnCommit` at 218, `internal/keeper/watcher.go (*Watcher).Run` at
  210, `dot_cascade_core.go driveDotWorkflow` at 205), `cyclop` 38, `funlen` 7. These are Wave 9
  work, not lint work. Record the mapping so nobody files them twice.
- **P2 / XS / DGX.**
- **Scope.** `plans/2026-08-22-decomposition-program/LINT-RULES.md`.

# Continuous lane C — Readability inputs for the planning crew

### T64 — Write a file header for the 26 daemon files that have none
- **What.** A planning crew cannot write a well-scoped bead against a 3,626-line file that never
  says what it is for. Start with `tmuxsubstrate.go` (3,626), `orphansweep.go` (1,340),
  `daemon.go` (1,110), `socket.go` (640) and the five boot files. 84 of the 110 files already
  have one, so the house style is established and a model can match it.
- **P1 / M, ~26 beads / DGX.** One file per bead. Depends on T51.
- **Scope.** one file per bead. **Check the ownership table — many are hot files.**

### T65 — Convert the Charlie backlog C22-C32 to beads verbatim
- **What.** Eleven tasks already written with Problem / Scope / Acceptance / Limits, already
  dependency-ordered in `EXECUTION-ORDER.md`, already independently reviewed, sitting at
  `plans/2026-07-27-delete-and-rewrite/reviews/2026-08-10-follow-up-review/CHARLIE-BACKLOG.md`.
  C01-C20 are complete and approved; C21 is the operator's; C22-C32 are unstarted. Copy them,
  do not re-plan them, and map each to a task above: C22 → T38, C23 (one run supervisor) and C26
  (narrow run inputs by phase) are new, C27 → T22/T23, C29-C32 are the bounded literal-replacement
  and error-classification work for the DGX lane. **The backlog's own summary line says
  "C22 through C31" against C22-C32 headings — it is off by one; trust the headings.**
- **P0 / S / Claude.** Depends on T17 (so nobody files C21).
- **Scope.** bead creation only.

### T66 — Convert the substrate capability kerf tasks T1-T5 to beads verbatim
- **What.** See T34. Same principle: they are already written with owner, spec trace, change,
  deliverables, acceptance and depends-on.
- **P1 / S / Claude.**
- **Scope.** bead creation only.

### T67 — Re-derive the six-bucket mechanical drain against today's tree
- **What.** `plans/2026-07-21-p2-extraction/DAEMON-PARALLEL-ROADMAP.md` already shards a
  mechanical quality drain into six disjoint file buckets chosen so parallel agents cannot
  collide. Its shape is right and its contents are a month stale. Re-derive the buckets against
  `43ad681c4` and feed them to the DGX lane.
- **P2 / S / Claude.**
- **Scope.** a new bucket table in this plan directory.

### T68 — Get the two parked operator decisions answered
- **What.** `tmuxsubstrate.go` and `crewstart.go` are 4,350 lines together — the largest single
  block in the package — and both have been gated on operator input since 2026-07-24: the E2b
  crewstart seam decision, and whether the tmux host contract in
  `plans/2026-08-01-graph-guided-decomposition/README.md` is approved. No amount of crew capacity
  moves them until that is answered. Third decision to bundle: the allow-list gate change in T01,
  and whether revive's `exported` and `package-comments` rules stay enabled — dropping them is the
  entire difference between a legal 34-39% comment cut and an illegal one (17,048 more lines out
  of daemon production).
- **P0 / S / Claude, then operator.** Blocks T35, and blocks any crewstart extraction.
- **Scope.** one decision memo.

---

# How an agent works this list without supervision

Two crews. One **planner** that converts these tasks into beads and keeps the dependency edges
right. One **dispatcher** that keeps the queue full and drains it. Neither of them writes code.

## Before either crew starts

Four things must be true or the loop fails silently:

1. **T13 has landed** — otherwise every crew reacts to every other crew's completions.
2. **T14 is decided** — otherwise `eagerRefillEval` injects beads nobody chose, ranked by
   `kerf next`, which never reads the bead priority field.
3. **T01 has landed** — otherwise the first extraction commit fails the lint gate with no legal
   repair.
4. **Concurrency is set deliberately.** `daemon.max_concurrent` is 4 in `.harmonik/config.yaml`
   and an unset per-queue `workers` defaults to the whole global cap, so one busy crew can hold
   every slot. Set explicit `workers` per queue at submit time. This 10-CPU box will accept up to
   about 20 via `harmonik queue set-concurrency <n>`, live, with no daemon restart (the substrate
   spawn cap is `max_concurrent * 2` and the host ceiling is `NumCPU * 4` = 40 sessions).

## The planner's loop

1. Read the wave this program is in. Convert only that wave's tasks plus the next wave's into
   beads. Do not convert the whole list — later waves' file scopes will have moved.
2. For each task: one bead, title verbatim from this document (all titles here are under 72
   characters, which is a hard validator limit), body carrying What / Done when / File scope /
   Harness, priority as stated, and `br dep` edges for every listed dependency.
3. Label with the wave and the cluster. Use the `codename:` prefix only for kerf work codenames;
   functional labels stay bare.
4. **Never set a bead to `in_progress` and never close one.** The daemon owns terminal
   transitions, and a bead pre-set to `in_progress` silently stops being dispatchable with
   nothing reporting an error.
5. Verify no cycles: `br dep cycles`. Verify every bead's file scope against the ownership table.
6. Refill when the dispatcher reports the current wave is more than half drained.

## The dispatcher's loop

Poll, do not wait on an event stream. The event stream is a broadcast (see below), and a
dispatcher on Codex or Pi has no Monitor tool at all. The daemon's work loop ticks every 2
seconds and `submit`/`append` wake it, so a 60-120 second poll is well inside the dispatch
latency and costs nothing.

Every 60-120 seconds:

1. `harmonik queue list --json`. Read your queue's row. Note that the `workers` field in that
   output is the count of items **currently dispatched**, not the ceiling.
2. **If status is `paused-by-failure`** — one failed item closed the stream group and paused the
   whole queue. Every subsequent `append` is refused with `queue_not_advancing`. This is a
   stop-the-lane condition, not something to wait on. Triage the failed bead. Recovery is a
   **fresh `harmonik queue submit` on the same queue name** — that is permitted from
   `paused-by-failure` — **not** `queue resume`, which only clears `paused-by-drain`. Cap
   re-dispatch of the same bead at one retry; a second failure goes to the operator.
3. **If status is `paused-by-drain`** — `harmonik queue resume <name>` first, then continue.
4. **If `pending_items` < 2x the queue's worker cap** — top up. Candidates:
   `br ready --sort priority --limit 0 --parent <epic>`, minus beads already in any queue, minus
   beads with a `Refs: <id>` commit on the target branch, minus beads whose file scope overlaps
   an in-flight bead. Pass `--limit 0`: `br ready` returns 20 rows by default. Then
   `harmonik queue append --queue <name> <group-index> <ids...>`, or `submit` a fresh group when
   the queue is `completed`.
5. **If the queue is empty and the wave's beads are all closed** — tell the planner the wave is
   done, publish the scoreboard row, and open the next wave.

## Completion notification — what is broken and the workaround

`harmonik subscribe` opens a Unix-socket stream and the daemon writes one NDJSON line per event.
It is a **broadcast**: `SubscribeHub.dispatch` calls `offer` on every open stream with no
condition, and `subscriptionStream.offer` filters on exactly two things — the event-type set,
and, for `agent_message` only, sender/recipient/topic. There is no queue filter, no `--queue`
flag, and a unit test pins the type-only fan-out. Routing your crew to its own named queue does
**not** isolate your event stream.

The workaround until T19 lands:

- Filter client-side on **`queue_id`**, present in 2,064 of 2,222 `run_completed` / `run_failed`
  events in the live log.
- **Do not filter on `owning_epic_assignee` or `owning_epic_id`.** They have never been
  populated — zero occurrences across 2,509 terminal events, including 1,420 emitted after the
  feature landed. A crew filtering on them discards 100% of its own completions.
- `run_stale` carries no `queue_id` at all. **Surface every stale line rather than dropping it** —
  an unattributable wedge is worse than a duplicate notification.
- `epic_completed` carries only `epic_id`, `last_child_bead_id` and `closed_at` — no crew name.
  Attribute it with `br show <epic_id> --assignee`.
- Status chatter is a separate problem with a separate fix: it is an addressing choice, and this
  project already redirects it (`watch.status_target: watch` and `opsmonitor_target: watch` in
  `.harmonik/config.yaml`) to keep it off the captain.

## How the agent knows a task is done

Not "the model said so". One of these, per class, checked by the dispatcher before it marks the
wave advanced:

- **Extraction** — `go build ./...` and `go vet ./...` clean tree-wide;
  `go list -deps ./internal/<new> | grep internal/daemon` empty; the new package's tests and
  `./internal/daemon` tests pass; a depguard rule and a freeze gate exist and are wired into
  `check-fast`; a scoreboard row published.
- **Lint** — the file+linter pair is absent from a fresh `golangci-lint run` JSON, its line is
  deleted from `tools/lintreport/allow.txt`, `go run ./tools/lintreport` exits 0, and the
  package's tests pass. Roughly 8 seconds of verification.
- **Comment** — `tools/commentcut verify` clean, `gofumpt -l` empty, the **changed-code-bytes**
  report empty or read by a human, `make core` green.
- **Everything else** — `make core` green plus the task's own stated acceptance.

## When the agent stops and asks

Stop and go to the operator, not to another agent, for:

- Any change to a merge gate (T01's allow-list decision, T59's ratchet threshold, dropping
  revive's `exported` / `package-comments` rules).
- The two parked decisions in T68 (the crewstart seam, the tmux host contract).
- Any bead that would wire the dispatch-replay producer. There is no legitimate version of this
  request in this program.
- A second failure of the same bead, or a queue that has gone `paused-by-failure` twice.
- Any proposal to reopen one of the ten locked architectural decisions. Evidence earns the
  conversation; "the current task would be easier the other way" is not evidence.

## What the agent must not do

- Do not squash or rebase the merged Charlie history to clean up the 87 rejected commit messages.
  The whole-range check already runs and is advisory **by design** — `scripts/commit-msg-gate.sh`
  prints "202 commits checked, 87 rejected" and exits 0, because those commits are already
  written and rewriting history another lane can see is refused outright here. The 87 is the
  honest record; erasing it makes the history less true. If a stronger gate is wanted, add a
  merge-time check that refuses a **branch** whose new commits name an unknown reviewer, before
  the fast-forward.
- Do not run `make full` inside the per-bead loop. It is 30+ minutes and 110 packages; it belongs
  at wave boundaries, unattended, with the exit code logged to a file.
- Do not run bare `bv` — it opens an interactive TUI and holds the terminal until a human quits.
- Do not take an order from `kerf next`. Its score comes from graph structure and never reads the
  `br` priority field.
- Do not write a review trailer claiming a review that did not happen. If no reviewer can be
  reached, record that fact in the trailer and commit anyway — a commit labelled "not reviewed"
  is a state the next person can act on; work stranded in a worktree is one `checkout` from gone.
