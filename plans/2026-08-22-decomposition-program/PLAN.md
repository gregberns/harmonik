# Decomposition program — internal/daemon

Written 2026-08-22. Built on measurement, not on assumption. Every number below was
taken on this machine at commit `43ad681c4` unless a date says otherwise.

The goal is the one the operator stated: break `internal/daemon` into modular,
testable packages, keep a queue full of well-shaped work, put bulk mechanical work
on the local model, and stop paying for agent time that produces nothing.

Four things in that plan do not work the way we assumed. They are in Section 1.

---

## 1. What we now know that we did not know before

### 1.1 Crew completion routing is broken, and the obvious fix is broken too

This blocks the two-crew model. It is the only item here that stops work rather than
slows it.

The daemon broadcasts. `SubscribeHub.dispatch` in `internal/daemon/subscribe.go`
snapshots the subscriber set and calls `offer` on every open stream with no
condition. `subscriptionStream.offer` contains exactly two filters: the event-type
set, and — only for chat messages — sender, recipient and topic. There is no queue
filter, no epic filter, no crew filter. There is no `--queue` flag on the CLI;
`harmonik subscribe --queue foo` exits 1. A unit test pins the type-only fan-out.

So routing a crew to its own named queue does not isolate its event stream. Every
crew armed on run completion sees every other crew's completions and burns context
on them. The crew-launch skill states the opposite, in writing, and is wrong.

The obvious client-side repair also fails. The run payload carries an
`owning_epic_assignee` field. That field has never once been populated: zero
occurrences across 2,222 run-completed and run-failed events and 287 stale-run
events in the live log, including 1,420 terminal events emitted after the feature
landed on 2026-06-11. A crew told to filter on it would discard 100 percent of its
own completions.

The only discriminator actually on the wire is `queue_id`, present in 2,064 of 2,222
terminal events. Stale-run events carry no queue attribution at all.

### 1.2 internal/daemon is bigger than when the decomposition started

Measured by reconstructing each revision:

| Date | Commit | Production lines | Files |
|---|---|---|---|
| 2026-07-27 (program start) | e99a52fff | 44,808 | 94 |
| 2026-08-01 (low-water mark) | 95dff0bf5 | 43,529 | 96 |
| 2026-08-10 | b49210d67 | 45,020 | 100 |
| 2026-08-22 (today) | 43ad681c4 | 49,370 | 110 |

The extraction work was real. `internal/runloop` is 2,775 lines that used to be
daemon lines. It was outrun. The package is 4,562 lines larger than at program
start and 5,841 larger than its low-water mark, because new daemon code landed
faster than lifts removed it — most of it from the same program. The crash-safe
dispatch work alone added a net 1,494 production lines to `internal/daemon`.

Any plan without a visible size counter repeats this. Nobody noticed until it was
measured.

### 1.3 Splitting the daemon will not make the test gate fast

This is the assumption that most needs correcting. `internal/daemon` runs 1,091
tests in 294 seconds and is 56 percent of core test time, so the split looks like
the obvious fix for the gate. It is not.

Attributing per-test elapsed time to topic clusters: 617 of 815 test CPU-seconds
sit in full-work-loop integration tests that stay in `internal/daemon` no matter how
the production code is partitioned. Only the graph-cascade cluster carries real test
weight out with it (138 seconds). Everything else — the scheduler cluster at 17
seconds, the terminal substrate at 12, the stale watcher at 10, the sweeps at 8, the
whole socket surface at 6 — is noise against the 294.

The gate wins are elsewhere and are cheap:

- 111 of the last 400 commits (28 percent) changed no `.go` file and still paid
  seven minutes of tests.
- Every run worktree gets a cold Go build cache. Measured cold compile of the core
  test binaries: 25.5 seconds, paid two or three times per bead, plus about 500 MB
  of cache per run that nothing reaps (6.2 GB sitting there now).
- The 18 freeze-gate scripts run serially at 21.8 seconds. Under `xargs -P 8` they
  take 4.7 seconds and exit 0.
- The daemon already exports `HK_GATE_BASE_SHA` into the gate environment. Nothing
  reads it. Computing the affected package set from a diff takes 0.80 seconds.

Even with a scoped gate, 252 of the last 289 Go commits (87 percent) touch
`internal/daemon` and pull its 294 seconds in. Only 13 percent land under two
minutes. Model the same commits with the daemon split so a touched sub-package costs
about 40 seconds and 289 of 289 land under two minutes — but that is the split
paying off at the end, not a reason to do the split.

The real payoff of the split is different and it is worth having: 382 commits
touched `internal/daemon` in 30 days. Two crews working in one package collide by
construction. Fenced packages let crews work without touching each other's files.

### 1.4 The first extraction commit fails the build, and the repair is forbidden

The lint gate is the hard blocker on this program, ahead of anything about tests.

A whole-tree lint run reports 1,041 findings. The merge gate passes anyway, because
every finding is grandfathered on a 580-line allow list at
`tools/lintreport/allow.txt`. The judge at `tools/lintreport/main.go` keys that list
on the exact file path plus the linter name. `scripts/lint-allow-ratchet.sh` fails
the build on any pair ADDED to the list. Its own comment: "The allow list only ever
gets shorter. Adding a pair to it is the one repair that is not allowed."

This was tested, not reasoned about. Rewriting one daemon file's path in the lint
report and re-running the real judge produced `FAIL: 8 file/linter pairs are not on
the allow list`, exit 1. There are 248 such pairs under `internal/daemon` today.
Every one fires when its file moves.

The changed-line lint step almost certainly compounds it: `golangci-lint`'s diff
processor has no rename detection, so a moved file reads as wholly new.

This has to be settled before any file moves. It changes a merge gate, so it is an
operator call.

### 1.5 The seams were measured by doing the extractions, not by reading the code

Three extractions were performed in throwaway copies of the tree and the compiler
was allowed to enumerate the true boundary. That changed three answers:

- Moving `runregistry.go` works — the whole tree builds and vets clean, all 304
  daemon test files included — but it costs two exports, not zero. An unexported
  method `snapshotWithKeys()` is called from three other files, and the unexported
  `aborted` field on the run handle is written directly at three sites. The file's
  own comment claims the accessor exists so the field never has to be named. The
  getter exists; the setter does not.
- The control-plane cut as originally scoped is a hard import cycle. The 24-file
  cluster needs 16 symbols from the daemon and the daemon needs 34 back. Go forbids
  it. The cut only becomes legal at 30 files and 8,370 lines.
- The dispatch-replay cluster is not a leaf. Its measured outbound surface is six
  symbols, four of which come from the scheduler spine.

A reference graph built from the AST missed all three, because it keys methods by
receiver type and does not see field access. Validate every future slice by doing
the move and reading the compiler's undefined-symbol list in both directions.

One thing is easier than assumed: `depguard` is not a gate here. `.golangci.yml`
sets `default: none` with no catch-all rule, and the `daemon` rule already allows
all of `internal/`. A new package with no rule of its own is unguarded, not a lint
failure. Writing a rule per package is discipline worth keeping; it is not a
blocker and must not be sequenced as one.

### 1.6 Comment reduction is worth doing and the "safe" proof is not safe

A deterministic cutter was built and run over the whole tree twice. It removes
70,891 lines in its recommended mode with zero parse failures and zero token-stream
divergences, and `go build ./...` and `go vet ./...` both pass. That mode also
reddens `make core`.

Two claims failed under test:

- Four tests assert on comment TEXT. Three are in `internal/core`, which is in
  `CORE_PKGS`. They require literal strings inside the struct-field godoc of the
  event timestamp fields. The recommended mode cuts struct-field docs, so all three
  fail. **Struct-field doc comments are not cuttable in this repo.**
- Token-equivalence is not a sufficient proof. It is blind to whitespace, and the
  required `gofmt`/`gofumpt` post-step changes code bytes: a comment block inside a
  composite literal is an alignment-group separator, so deleting it makes gofmt
  re-pad the key column. 1,075 code lines across 191 files change bytes with an
  identical token stream. One of them breaks a real test, in every mode — including
  the "zero judgment" phase.

The honest yield for the safe mode is also smaller than advertised: 27,808 lines
tree-wide (4.5 percent), and the seven biggest daemon files shrink 10 to 44 percent,
not 40 to 52. Say that number to the operator, because the context-window saving is
the only benefit this buys. It buys no test time and moves none of the complexity
ceilings — measured identical in every mode.

### 1.7 The local model is not slow. Its serving setup has no prompt cache

The 71-minute run was iteration 1 of 3; the full run was 118 minutes and ended in a
BLOCK verdict.

Every usage record in that run reports `cacheRead:0, cacheWrite:0` — 22 of 22 turns.
Per-turn latency grows linearly with conversation length: 13.5 seconds on turn 1,
189 seconds on the last turn of iteration 1, dropping straight back to 25 seconds
after one compaction. Fitting all 114 measurable turns gives a 19.5-second fixed
floor plus 0.179 seconds per 1,000 characters of accumulated context.

Decomposition of the 107.5 minutes of model time: 37.4 minutes re-prefilling an
unchanged 27,700-token preamble, 47.6 minutes re-prefilling the growing
conversation, 23.4 minutes actually generating tokens. Tool execution is negligible
— 2.9 seconds average.

Turning on prefix caching takes that run to roughly 25 minutes. It changes no
harmonik code.

The model's remaining weakness is discovery, not editing: 98 of 115 turns wrote
nothing, one file was read 12 separate times, and 19 consecutive turns went to a Go
toolchain hunt. Its edits, once it knew the target, were small and mostly fine.

### 1.8 The crash-safe dispatch branch is already merged, and its approvals mean nothing

`git reflog work/alpha-integration-merge` records
`43ad681c4 @{2026-08-22 01:14:20 -0700}: merge work/charlie-universal-run: Fast-forward`.
Both branches now point at the same commit. Nothing is pushed. No other lane branch
contains it. The whole thing is one local command from being undone.

61 of the 64 commits in that range name a reviewer this repository has no skill for.
The whole-range commit-message check already exists and already runs. It reports
"202 commits checked, 87 rejected" and exits 0 — it is advisory by design, because
the alternative is rewriting history other lanes can see. So the gate is not
missing. Making it refuse is a decision, not a fix.

A human audit before the merge found what 61 approvals did not: a merge gate that
had been red across every one of those commits and was never once run, 53 broken
tests, and a data-destroying regression in the boot sweep.

### 1.9 A daemon-side queue filler is already running and will fight the planning crew

Eager refill runs on every 2-second poll tick and on every terminal run event. It
computes a deficit, over-fetches candidates from `kerf next`, drops the ones already
queued, and appends the survivors. It is live on this box because `kerf` is on
PATH. `--no-auto-pull` does not disable it.

Two problems for the two-crew model. It ranks with a graph metric that never reads
the bead priority field — this project's own documents say so. And it tops up the
first active stream group it finds, not each crew's queue.

---

## 2. The decomposition program

Prior research already planned most of this. Four programs produced ordered task
lists with acceptance criteria. The job is to convert and execute, not to re-plan.

The two source documents worth converting verbatim, because both are already written
in the shape a bead needs and both carry declared dependency order:

- `plans/2026-07-27-delete-and-rewrite/reviews/2026-08-10-follow-up-review/CHARLIE-BACKLOG.md`
  — 32 tasks, each with Problem / Scope / Acceptance / Limits. Items C01 through C20
  are complete and independently approved. C21 is the slice the operator is taking
  over. C22 through C32 are unstarted.
- `.kerf/works/substrate-capability-contract/07-tasks.md` — 176 lines, tasks T1
  onward, each with owner, spec trace, change, deliverables, acceptance and
  dependencies. Finished design, reviewed, zero production code written.

Three documents must be marked historical before any crew reads them: the chunk
catalogue in `plans/2026-07-21-p2-extraction/` (written against four files that no
longer exist), the per-group budget table in
`plans/2026-07-27-delete-and-rewrite/DECOMPOSITION-MAP.md` (disowned by its own
document and three weeks stale), and
`docs/foundation/project-level/subsystem-organization.md` (names six packages that
were never built).

### Phase 0 — before any file moves. Days.

**0a. Settle the lint allow list under renames.** Operator decision, see Section 7.
Nothing else in this program can start until it is answered.

**0b. Publish the scoreboard and re-measure it on every landing.** Four numbers:
production lines in `internal/daemon` (49,370), test seconds in `internal/daemon`
(294), remaining white-box test shims (140), and file/linter pairs under the daemon
(248). Treat an increase in the first as something the commit body must justify.

**0c. Decide about eager refill.** Off, or repointed. See Section 7.

**0d. Land the cheap gate wins.** These are independent of everything else and pay
back on every bead: parallelise the freeze-gate scripts (21.8s to 4.7s), warm the
worktree Go cache in the background at worktree creation instead of on the gate's
clock (25.5s per attempt), and reap the lane cache when the worktree is removed.

### Phase 1 — the keystone. Hours.

**Move `runregistry.go` (393 lines) to `internal/runregistry`.**

It has zero outbound references into the rest of the package — confirmed by AST and
by the compiler. Three symbols cross out to 18 daemon files, all already exported.
It imports only the standard library, `internal/core` and `internal/handlercontract`.

Cost: export `snapshotWithKeys()`, add a setter for the `aborted` field, touch 21
production and 26 test files. The move was completed in a scratch copy: the whole
tree builds and vets clean and the targeted tests pass in 13.4 seconds.

**Why first.** Five other clusters name the run registry or the run handle as their
only remaining tie to the rest of the package. Moving 393 lines converts five
multi-edge extractions into near-zero-edge ones. `internal/runloop` already declares
the consumer-side port it satisfies.

### Phase 2 — three parallel leaf extractions. Days, one crew each.

These cannot collide: no two share a file.

| Package | Moves | Lines | Outbound symbols | Export work |
|---|---|---|---|---|
| `internal/harnesspick` | harness registry, harness resolve, model preference, Pi profile resolve, mode resolve | 1,218 | 0 | 12 of 13 inbound unexported |
| `internal/spend` | the two spend meters | 638 | 1 (gone after phase 1) | 0 |
| `internal/pause` | the six handler-pause files | 2,056 | 3 (2 gone after phase 1) | 2 of 8 |

The rename work in the harness cluster is large but the compiler proves every step.
It is good local-model work under a strict no-behaviour-change gate.

**Do not include dispatch replay in this batch.** Its measured outbound surface is
six symbols and four of them come from the scheduler spine, which this plan does not
extract.

This batch exists to prove the delegation loop end to end before anything expensive
rides on it.

### Phase 3 — the control plane. Weeks. The largest clean cut.

**One package. 30 files. 8,370 lines.** The socket listener and its dispatch table,
the subscribe hub, the notify stream, the four comms files, the four decision files,
the three dashboard files, the three live-state files, operator pause, queue
recovery, session-start acknowledgement, the hook relay — **plus** quiesce, the
drain detector and its epic variant, the agent-message matcher, the verdict
override, and the dispatch-target probe.

The last six are not optional and not a separate slice. Cut at 24 files and you get
a direct import cycle: the new package needs 16 symbols from the daemon while the
daemon needs 34 back. At 30 files the residual outbound surface is four symbols, two
of which phase 1 already removed.

Two named symbols need an explicit decision inside this slice:

- `dashboardGateEvalInterval` is a constant in `workloop.go` — the file with 74
  commits in 30 days, so touching it collides with the busiest lane. Move it or
  duplicate it, but plan for the conflict.
- `ConcurrencyController` is a 52-line file also used by the scheduler and the
  bandwidth tuner. Make it a shared leaf package, or invert it behind a small
  consumer-owned interface.

**The tests travel free.** All 27 control-plane test files (8,715 lines) are already
external `package daemon_test` and not one uses the shared full-daemon harness.

**Do not add the new package to `CORE_PKGS`.** The `CORE_PKGS` comment block in the
Makefile already names the socket listener, comms, dashboard, live-state and
subscribe as outside the core set, by charter and by operator decision. Leaving the
new package out narrows what a merge is gated on, with no argument required.

### Phase 4 — mid-size watchers. Days each, order flexible.

| Package | Moves | Lines | Note |
|---|---|---|---|
| graph-cascade helpers | `dot_cascade_helpers.go` | 2,098 | Created as a liftable leaf by the July roadmap and never lifted. Carries 138 seconds of test time — the one extraction with a real gate payoff. |
| `internal/sweeps` | orphan sweep, worktree sweep, branch reap watcher, history rotation, disk check | 2,419 | Flagged ready on 2026-07-24. |
| `internal/stalewatch` | stale watch, stall feed, poll gate | 1,990 | After phase 3 — the live-state builder and activity labels move first. |
| `internal/taskbranching` | `branching.go` | 748 | One outbound symbol. |

Two smaller ones were planned in July, blocked on a dependency that has since
landed, and never picked up: quiesce (1,048 lines, now folded into phase 3) and the
subscribe hub (blocked on the project-config lift, which shipped).

### Phase 5 — the terminal substrate. Weeks. Gated.

7,283 lines across the tmux substrate, paste injection, session adoption, run
support, the sandbox profile and the sandbox gate. Exactly ONE outbound symbol into
the rest of the package. It is the biggest single prize.

It is gated on a real technical seam, not on effort. Two of its five capability
interfaces require unexported method names, and a Go interface with an unexported
method cannot be satisfied from another package. The design and task list for
replacing all 16 such interfaces and the 30 capability probes already exist and are
reviewed. Do not skip that work and hand-export 48 symbols; that leaves the probes
unfixed and re-earns the same debt.

It is also gated on an unanswered operator decision about the crew-start seam. That
decision and this cut together hold 4,345 lines frozen since 2026-07-24.

### What this program does NOT extract

**The scheduler and the work loop stay.** The scheduler cluster references 25
symbols from outside itself, including three straight out of `workloop.go`, and the
coupling is bidirectional. `workloop.go` took 74 commits in 30 days. A package
boundary drawn across the spine is merge warfare and an abstraction that names
nothing it buys.

Instead, decompose in place, behind a single writer, serialized:

- `runWorkLoop` is 1,432 lines, 22 parameters, brace depth 11, 18 live locals in one
  frame. The 18 locals ARE the state. Name them as one loop-state struct, then lift
  each depth-3-and-deeper block into a method on it.
- The graph-cascade driver (1,033 lines, 26 parameters) and the agentic node
  dispatcher (688 lines, 32 parameters) are the same problem in the graph engine.
- Pull pure decisions out into `internal/orchestrator` one at a time — the pure
  claim-failure disposition already ready in the ledger is the right shape and size.

These five functions are 4,947 lines, 10 percent of the package, and they carry the
suppressions that let the complexity ceilings pass. Splitting files around them does
not make them smaller. They are also the best beads for unsupervised work, because
progress is one number per bead: lines in the function, or parameters in the
signature.

### The test-suite prerequisite

662 of 1,149 daemon test functions live inside `package daemon` and reach unexported
internals through 140 hand-written export shims. Every package boundary breaks a few
hundred at once.

Do NOT run this as a separate sweep. Convert per cluster, inside the same slice that
moves the files, deleting each shim as its last caller goes. Track "remaining export
shims", starting at 140. The control-plane cluster costs nothing here — its tests are
already external — so phase 3 is cheaper than this rule makes it sound.

### The slice template

Nine slices already landed with this recipe and 12 freeze-gate scripts under
`scripts/` are instances of it. Give the planning crew the template, not a blank
page:

1. Scaffold the package with a `doc.go`.
2. `git mv` the production files and the tests that travel with them.
3. Export exactly what the compiler names — in both directions.
4. Add a depguard rule with the measured allow list plus a deny on
   `internal/daemon`. Discipline, not a gate.
5. Add a grep freeze gate under `scripts/`, modelled on the existing ones, wired into
   the fast and short checks.
6. Add to `CORE_PKGS` only if the charter puts it in the core pipeline.
7. Prove `go list -deps ./internal/<new> | grep internal/daemon` is empty.
8. Re-measure the four scoreboard numbers and put them in the commit body.

---

## 3. The crash-safe dispatch branch

### Keep the merge. Do nothing to re-land it.

It fast-forwarded at 01:14 on 2026-08-22. Both branches point at `43ad681c4`. The
reachability gate is green on the merged tree — "OK (1248 known-unreachable names,
no new ones)" — and every touched non-daemon package passes. The two real defects it
carried were found and fixed before the merge landed.

**Do not squash.** It collapses 64 commits and 15,440 lines into one blob nobody can
review or bisect, and it buries the three commits that actually were reviewed.

**Do not rebase to rewrite the messages.** It rewrites 86 commits including 22
merges, and the only honest replacement text is "no reviewer was reached" — which is
what the current record already communicates. The 87 rejections ARE the honest
record. Erasing them makes the history less true.

### What it actually built, and why it does not advance this program

It built a new subsystem: the daemon writes a durable intent record at each step of
dispatching a bead, and on restart reads those records back and resumes where it
stopped. The pure decision layer is good code by this project's own standards — a
total function over typed facts, no clock, no I/O, nine closed enumerations, fails
closed to "repair required", 226 new fast tests with zero sleeps. If you want a
template for what a decomposed piece of harmonik should look like, that is it.

It is also entirely inert, and it made `internal/daemon` 1,494 net production lines
bigger while deleting 173. Nothing in production writes a dispatch intent. The
reachability baseline formally ratifies nine symbols as production-unreachable. The
replay executor implements 5 of 21 possible actions. A new CLI verb was added with
no production caller.

### Park the producer wiring. Write the prohibition into the crew mission.

This is not caution, it is a measured wedge. The boot preflight returns an error on
ANY non-empty intent list, including the path where every step succeeded, and that
error is fatal to boot. Three terminal actions an intent can reach hard-error out.
Two of the replay reader's inputs are pinned to literal wrong values, so the
coherence check that would stop replay re-provisioning finished work can never fire.

The first dispatch intent ever written wedges the daemon into a permanent boot-abort
loop, and the supervisor revives into it forever.

A bead is not enough. A planning crew reading the backlog sees the producer as the
obvious next item. Put it in the mission file.

### What to take from that backlog, and where it fits

Four of the eleven unstarted tasks actually shrink `internal/daemon`. Convert them
verbatim, with the dependency edges from the execution-order document:

- **Pure queue selection out of the work loop** (C22). Its target is unchanged in
  code — the selector still takes a locked queue store, the run registry and mutable
  maps. Fits alongside the in-place spine work.
- **One supervisor for run goroutines** (C23). Its target does not exist yet.
- **Narrow run inputs by phase** (C26).
- **A minimal core composition root with an import fence** (C27). This is the
  fifty-percent version of splitting the 40-field, 547-line config struct that 23
  files read, and of replacing the 39-method boot state with one constructor per
  cluster. Those two together account for 77 of about 170 cross-cluster edges.

Four more are the local-model lane and are covered in Section 4: same-package
literal replacement, workflow-loader error classification, tmux not-found error
classification, and the remaining CLI and socket error text.

Also worth doing once: write ONE tracked document recording what landed — that 61
commits claim approval from a reviewer this repository does not have, that the merge
gate was red across all of them and never run, and that 11 replay symbols are
blessed as unreachable with no implemented path to retire the state they write. Bead
notes are machine-local and gitignored. A tracked file survives a fresh clone.

---

## 4. Comment reduction and lint automation

### The mechanism

A deterministic tool at `tools/commentcut`, three verbs. A working prototype exists
and needs porting and tests, not designing.

- `list` — emit every non-directive comment block as file, start line, end line,
  size, attachment kind and text.
- `apply` — delete exactly the listed line ranges from the ORIGINAL bytes. Never use
  `go/printer`; it re-emits the whole file from the AST and its comment placement is
  the fragile part. Then run `gofumpt -w` and `gci write`.
- `verify` — refuse to write unless THREE checks pass.

The three checks matter. Two were proposed; the third is the one the testing found:

1. The `go/scanner` token stream, comments excluded, is byte-identical.
2. **The list of code lines whose bytes changed is empty, or a human reads it.**
   Deleting a comment inside a composite literal merges gofmt alignment groups and
   re-pads the key column. 1,075 code lines across 191 files change this way with an
   identical token stream, and one of them breaks a real test that asserts on a
   source substring. The formatter cannot be skipped — the repo has a format gate.
3. The tests of every affected package pass.

Runtime for the whole tree: 10.6 seconds.

### What must be preserved

**Compiler and linter directives**, matched by trimming the comment and testing the
prefix — never by substring. One prose line in `cmd/harmonik/asset_manifest.go`
reads `// //go:embed assets directive in init_skill_assets.go`.

Present in this tree: `//go:build` (117), legacy `// +build` (5), `//go:embed` (7,
must stay adjacent to its var), `//nolint` (1,680 — nolintlint runs with
require-explanation, require-specific and allow-unused all set, so a mangled one is a
build failure), `// Deprecated:` (2), `// Code generated`. Absent today but reserve
them anyway: `//go:generate`, `//lint:ignore`, `//revive:`, `//go:linkname`,
`//go:noinline`, `//line`, `//export`.

**This repo's own source-scanning markers**, which no generic list contains:

- `//cloexec:waived`. A test walks every comment group under `internal/`, `cmd/`,
  `tools/` and `test/` looking for it. Zero live uses today, so nothing breaks now —
  but the cutter deletes it, so the first one anyone writes vanishes silently.
- The 220 `Tags: mechanism` godoc lines. A test requires two of them by regex; the
  repo treats them all as source-verifiable conformance markers.

**Package doc comments and doc comments on exported symbols.** Not for the compiler
— for `revive`. Cutting them adds 337 findings across 54 new file/linter pairs, and
the allow-list ratchet forbids the repair.

**Struct-field doc comments.** Three tests in `internal/core` require literal strings
inside the field docs of the event timestamp fields. `internal/core` is in
`CORE_PKGS`, so cutting them reddens `make core` and `make full`.

**Scope: `internal/`, `cmd/`, `tools/`, `test/`.** Not `./...`. `evaltasks/` holds
the deliberately shaped Go fixtures the agent evaluations are graded on, and
`testdata/depguard/` holds a lint fixture kept out of every build by a build tag.

Known defect to fix before shipping: the prototype's truncate mode deletes the
closing `*/` of a block comment and then fails silently by skipping the file. Only
three files in the tree carry a multi-line block comment, which is why it looked
clean.

### The realistic yield

Safe mode — comment blocks that float inside a function body only:

- 27,808 lines tree-wide (4.5 percent).
- The seven biggest daemon files: terminal substrate -14 percent, scheduler -25,
  paste injection -16, cascade helpers -10, cascade core -44, work loop -30, stale
  watch -13.
- Build, vet, all 13 freeze gates, the reachability gate, and the specaudit,
  handler-contract, hook-system, core, br-adapter and CLI tests all green. One test
  fails, for the gofmt-realignment reason above. Fix that one substring assertion, or
  teach the cutter to leave alone any comment inside a composite literal, struct type
  or const block, and this phase becomes what it claims to be.

State this plainly to the operator: comment reduction buys context-window room for
the crews that will read these files during decomposition. It buys no test time —
the daemon's 294 seconds are untouched — and it moves no complexity ceiling, measured
identical in every mode.

Cut inside the extracting slice, not as a follow-up sweep. Moving code carries its
comments, extraction is the one moment when an agent already has the file loaded and
a reviewer already expects a large diff, and doing it separately leaves a window in
which the new package inherits the old density.

### Lint automation

**One free commit, today.** `golangci-lint run --fix --no-config --default=none -E
copyloopvar,errorlint,testifylint,nakedret,misspell ./...` followed by `gofumpt -w`.
Measured end to end in a scratch export: 18 files, +9/-25 lines, builds clean, vets
clean, format clean, clears 25 findings. Then delete the 19 now-empty allow-list
lines plus one stale entry the judge already reports.

`golangci-lint --fix` with the repo's own config is BANNED. It produces a tree that
does not compile — the gocritic bytes rewrite emits `bytes.Equal` without adding the
import, in 4 of 6 sites — and leaves 12 files gofmt-dirty. `--default=none -E <list>`
does not narrow it while the config file is present; `--no-config` is required.

### What the local model gets, and what it must never get

The rule: **give it only classes where a wrong answer cannot compile, or cannot
change behaviour.**

**Give it** (about 148 findings, each independently verifiable in under 10 seconds):

- File-permission literals in `_test.go` files (72). The permission conformance test
  skips test files, so these are free.
- `whyNoLint` (12) and `paramTypeCombine` (13) from gocritic, `unconvert` (8),
  missing doc comments (12), unexported-identifier naming (6).
- The 25 tool-autofixable findings, as a review of the free commit above.
- Comment KEEP / TRIM / CUT judgments, fed as a BLOCK LIST — file, line range,
  attachment, text, plus about ten lines of surrounding code — batched around 50
  blocks per prompt. That is about 43 model calls for the daemon's 2,151 multi-line
  blocks, versus 425 file rewrites. Each answer is one word, so a wrong answer costs
  one block. The tool applies and proves.

**Never give it:**

- `prealloc` (12). Proven: the naive fix `make([]T, len(x))` CLEARS the finding and
  passes build, vet, the whole-tree lint and the judge — while producing a slice with
  leading empty values. One live finding builds a path list that feeds
  `git worktree remove --force --force`. Injected empty strings are not destructive
  (removing an empty path is a no-op) but they DO increment the removed counter, so
  the daemon reports a reclaim that never happened and its disk-pressure latch takes
  the wrong branch. Invisible to build, vet and lint. Only the 294-second daemon
  suite catches it.
- `forbidigo` (50). 46 are forbidden `panic` calls, and the real sites are deliberate
  — invariant assertions, init-time registry helpers whose own comment says panicking
  is acceptable because init runs before any request is served, and constructor
  preconditions. The fix is a judgment call or a signature change across every caller.
- The 34 revive stutter findings. Cross-package exported-API renames.
- `unused` (66). It reads mechanical and is not: a symbol can be reached through a
  build tag, a test, or reflection.
- Security findings for subprocess arguments (45) and file inclusion by variable path
  (58). A tool that shells out and reads files by path keeps producing these. Mark
  them permanent with a rationale line each and never staff them.
- Production permission literals (9). A cross-package conformance test demands the
  shared constant, not a hand-written literal, and its allowlist marks several
  tighter credential directories "never widen one of these".

**Write the two traps at the top of the rule sheet.** They are the first things a
model reaches for and both produce a commit that fails review: `_ = f()` does NOT
clear an errcheck finding here (check-blank is on), and `--fix` is banned for the
reason above.

### The verification story

Per bead, for classes that cannot change behaviour: gofumpt check on the file,
`go build ./...`, `go vet` on the package, a warm whole-tree lint to JSON (3.8s), the
judge (0.1s). Total under 10 seconds against `make core`'s 474.

For any class that CAN change behaviour, the package's own tests run too. Do not
reserve tests for batch boundaries — that was tested and it passes on corrupted code.

Definition of done is machine-checkable and cannot be gamed downward: the file and
linter pair is absent from a fresh report AND its line is deleted from the allow
list. A partial fix leaves the line in place and honestly reports no progress.

### Make the local model usable first

Four fixes, in order of payoff:

1. Turn on prefix caching on the serving endpoint. Confirm by checking that
   `cacheRead` goes non-zero in the run's stdout log. Arithmetic says 107 minutes
   becomes about 25. No harmonik code changes.
2. Give the Pi harness a small worktree `AGENTS.md` — worktree discipline, the commit
   trailer, the build command — instead of the 24 KB router file. Every 1,000 tokens
   removed saves about 0.7 seconds per turn; at 115 turns that is 80 seconds per
   1,000 tokens.
3. Add a turn budget of about 25 per iteration alongside the existing 90-minute
   ceiling, and tell the model how many turns remain. A wall-clock ceiling does not
   stop a model looping cheaply.
4. Require a build check in the same turn as every edit, and pre-attach the target
   file's relevant span to the task file. A misspelled identifier cost a 23-minute
   iteration round trip that a same-turn build would have caught in 40 seconds. Fix
   the worktree environment so `go` resolves without a toolchain hunt — 19 turns and
   11 minutes went to that.

---

## 5. The two-crew model

### BLOCKER: completion notification does not route to the right crew

State this as a blocker, because the plan assumes it works and it does not. See
Section 1.1 for the evidence.

**Minimum viable, today, no Go change.** Tell each crew to filter the subscribe
stream client-side on `queue_id`, matching its own queue. Present in 2,064 of 2,222
terminal events. Do NOT tell crews to filter on the epic assignee — that field is
always empty and the filter would discard everything.

Stale-run events carry NO queue attribution at all, so they cannot be attributed.
Every crew must surface them rather than drop them.

Fix the false claim in the crew skill while you are there. It says routing off the
shared queue "keeps crews isolated" because otherwise "your monitor sees runs you did
not submit". The monitor sees every queue's runs either way. State what named queues
actually buy: a separate pause blast radius, a separate per-queue worker cap, and an
identifier you can filter on. Edit `cmd/harmonik/assets/skills/crew-launch/SKILL.md`
and mirror it byte-for-byte into `.claude/skills/` in the same commit.

**Correct fix, days.** Put the queue NAME into the wire payload, then filter at the
daemon. Three notes for whoever does it:

- The struct on the wire is the daemon-local one in `internal/daemon/workloop.go` —
  one struct serves both completion and failure, discriminated by a success flag.
  The types in `internal/core` are the spec-side definitions. Editing only those
  compiles and ships nothing.
- The daemon already holds the name as `RunHandle.QueueName`. It is a plumbing job,
  not a lookup problem.
- Apply the filter in `subscriptionStream.offer`, next to the existing chat-message
  filter, and add the flag to the CLI.

**Separate defect worth its own investigation.** The epic attribution resolver emits
nothing although the data exists: the ledger holds 176 parent-child edges, 56 of the
872 beads dispatched since the feature landed have one, and the edge direction
matches what the resolver tests for. Both dispatch paths rehydrate a record that
populates edges. It should work and it does not.

### What already exists for the filling crew

Sensors: `harmonik queue list --json` returns per queue the name, identifier, status,
pending items, workers, completed items and failed items. Note that `workers` in that
output is the count currently DISPATCHED, not the ceiling. `harmonik queue status`
returns the full envelope plus the concurrency ceiling.

Actuators: `queue submit`, `queue append`, `queue resume`, `queue set-concurrency`,
`queue cancel`. `set-concurrency` takes effect live with no daemon restart.

Latency is bounded by the crew's own cadence, not the daemon's: the work loop polls
every 2 seconds and submit and append both wake it. A 60 to 120 second poll is fine.
That also means the loop works on Codex or Pi, which have no event-monitor tool.

### What is missing

**The loop itself.** Write it as an agent loop over the existing CLI, not as new
daemon code — `internal/daemon` is already 49,370 lines. Minimum body: read the queue
list; if pending items fall below twice the worker cap, choose top-up beads from
`br ready --sort priority --limit 0 --parent <epic>` minus beads already queued minus
beads already landed; append to the active group, or submit a fresh group when the
queue is completed or paused by failure, or resume first when it is paused by drain.
Never pre-set a bead in progress, never assign a child bead, never close one.

**A stall check.** This is the single biggest obstacle to unsupervised filling. One
failed item closes the stream group and pauses the WHOLE queue, and every later
append is then refused. Recovery is a fresh submit on the same queue name — a resume
does not apply. The live fleet right now: 31 queues, 29 completed, two paused by
failure, one paused by drain. Every lane is stopped. That has already happened.

Cap re-dispatch of the same bead at one retry.

**Explicit per-queue worker caps.** The global ceiling is 4. An unset per-queue cap
defaults to the whole global cap, and selection is round-robin over queue names with
a persistent cursor. So two crews contend for the same four slots and one busy crew
can hold all of them. Set explicit caps, or raise the ceiling — this 10-core box will
accept about 20, live.

**A decision about eager refill**, which will otherwise inject beads the planning
crew never chose. See Section 7.

### Where the captain fits

Captain wake-load is an addressing question, not a subscribe question. The captain
arms only the epic-completion stream and forbids a standing run-level subscribe.
Crew status is already redirected to a watch session in `.harmonik/config.yaml`, and
a broadcast message still reaches everyone. That part works; do not re-engineer it.

---

## 6. What we are deliberately NOT doing

- **Not reverting, squashing or rebasing the merged crash-safe dispatch work.** It is
  in, the gates are green, and every alternative costs days to move a number on a
  check that is advisory by design.
- **Not finishing crash-safe dispatch replay.** 16 unimplemented replay actions and a
  producer that would wedge the daemon at boot. Weeks more work with no user-visible
  change.
- **Not extracting the scheduler or the work loop as packages.** 25 outbound symbols,
  bidirectional coupling, 74 commits a month on one file. Decompose in place instead.
- **Not chasing a two-minute gate through the split.** 87 percent of recent Go
  commits touch the daemon, and 617 of 815 test seconds are integration tests that
  stay there. Take the cheap gate wins instead.
- **Not raising test parallelism and not dropping the no-cache test flag.** Measured:
  `-parallel 32` gained 3 percent and turned two daemon-start tests red. Dropping the
  flag works mechanically but 17 packages exec subprocesses whose content Go's test
  cache does not hash, so it can report a stale pass — and run worktrees start cold
  anyway.
- **Not rewriting files with the local model to reduce comments.** 10.6 seconds
  deterministic against an extrapolated 21-plus hours, and it still needs the
  equivalence check to be trustworthy.
- **Not deleting all comments.** It fails this repo's lint gate with no legal repair.
- **Not staffing the security, panic, prealloc, unused or stutter-rename lint
  classes.** Each produces failed reviews or silent corruption.
- **Not ranking work with the graph metric.** It never reads the priority field.
- **Not reconciling the main branch in this program.** Main carries 27 commits the
  integration branch lacks, 25 of them leftover test-probe marker files and 2 real
  fixes, while the integration branch is 1,330 commits ahead. It is a bigger and
  separate problem, and it is currently invisible.
- **Not adding a per-test coverage index for the daemon suite.** It would work — 970
  of 1,091 tests run under a second — but coverage-based selection silently misses
  tests that fail without covering the changed line. Keep it as a fallback if the
  split proves too slow to wait for.

---

## 7. Open questions that need the operator

These are decisions, not research. Each one blocks or redirects real work.

1. **How does the lint allow list survive a rename?** Three options: teach the judge
   to follow renames, key the list on content instead of path, or re-seed the list at
   the split commit and advance the ratchet's base in the same change. This changes a
   merge gate, and the ratchet exists specifically to stop an agent buying its own
   exemption. **Phase 1 cannot start until this is answered.**

2. **Do we drop the two revive rules that require doc comments on exported symbols
   and on packages?** That is the entire difference between a legal comment cut and
   an illegal one, and it is worth 17,048 more lines out of `internal/daemon`
   production. It is a merge-gate config change and it is yours to take or refuse.

3. **Do we make the whole-range commit-message check refuse, or add a separate
   branch-level gate that refuses a branch whose new commits name an unknown
   reviewer, before the fast-forward?** Making the existing one fatal means either
   rewriting 87 commit messages or moving the baseline forward. Note that 26 of those
   87 rejections predate the merged branch, so this is not a one-branch problem.

4. **Does a real reviewer read the 61 commits that name a reviewer this repository
   does not have — or do we decide in writing that the passing tests plus the three
   audit commits are enough?** When a genuine reviewer finally read the tip, it asked
   for changes and found a startup wedge. So at least one of those approvals was
   wrong, and nothing establishes the other 60 were read at all.

5. **The crew-start seam decision, open since 2026-07-24, and approval of the tmux
   host contract.** Together they hold 4,345 lines — the largest single block in the
   package — frozen. No amount of crew capacity moves them until this is answered.

6. **Eager refill: off, or repointed?** It is live right now, it injects beads nobody
   chose, it ranks them with a metric that ignores priority, and it tops up only the
   first active group it finds. If the planning crew owns what gets queued, disable
   it. If auto-fill is wanted, change its candidate source to the priority-sorted
   ready list and make it top up every active group.

7. **Do we raise the global concurrency ceiling above 4?** Two crews currently
   contend for the same four slots. This box will accept about 20, live, with no
   restart.

8. **Who owns the serving configuration on the local model box?** Turning on prefix
   caching is the single largest speedup available to that lane — about 4x — and it
   changes no harmonik code.

---

## Monday

1. Put questions 1 and 6 to the operator. Everything in phase 1 waits on question 1.
2. Land the free lint autofix commit (18 files, 25 findings) and delete the 19 dead
   allow-list lines.
3. Parallelise the freeze-gate scripts. 17 seconds off every gate attempt.
4. Warm the worktree Go cache at worktree creation. 25 seconds off every gate
   attempt.
5. Tell crews to filter the event stream on the queue identifier, and fix the false
   isolation claim in the crew skill — the embedded asset and the mirrored copy, one
   commit.
6. Write the prohibition on the dispatch-intent producer into the crew mission file.
7. Publish the four scoreboard numbers: 49,370 daemon production lines, 294 daemon
   test seconds, 140 export shims, 248 daemon lint pairs.
8. Convert the eleven unstarted backlog tasks and the substrate capability task list
   into beads verbatim, with their existing dependency edges. Do not re-plan them.
