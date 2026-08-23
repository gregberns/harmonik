# What the decomposition research missed

Written 2026-08-22 against `43ad681c4`. This is a completeness critique, not a summary. Everything
below is either a measurement I took myself today or a claim in the research that I checked and
found unsupported. Where I measured, the command is named so you can re-run it.

The research is twelve surveys plus five adversarial verifications. It is good work and most of its
facts hold. But it answered "how do we split the daemon" without ever asking "can this fleet
currently finish a bead", and the answer to the second question invalidates the economics of the
first.

---

## 0. The finding that outranks every other gap: the fleet has not worked in five weeks

Nobody measured throughput. I did, from `.harmonik/events/events.jsonl`:

| Date | beads closed | runs completed | runs failed |
|---|---|---|---|
| 2026-07-12 | 38 | 38 | 60 |
| 2026-07-18 | 0 | 0 | 6 |
| 2026-07-19 | 0 | 0 | 1 |
| 2026-07-22 | 1 | 1 | 3 |
| 2026-08-10 | 0 | 0 | 2 |
| 2026-08-15 | 6 | 6 | 2 |
| 2026-08-22 | 0 | 0 | 1 |

**Seven beads closed in the last five weeks.** The best day this fleet ever had was 82
(2026-05-31). All-time: 1,151 beads closed, 1,162 runs completed, **1,060 runs failed — a 48%
failure rate.**

Joining `model_selected` to the terminal event, by harness, all-time:

| harness | completed | failed | success rate |
|---|---|---|---|
| claude-code | 210 | 240 | 47% |
| pi (nemotron / ornith) | **1** | **40** | **2.4%** |
| codex | **0** | **29** | **0%** |

The one successful `pi` run was 2026-07-11. Every run that completed since 2026-07-22 was
`claude-code` on `claude-opus-4-8`.

**This is a direct refutation of the plan's economic premise.** The plan is "the local DGX model
does bulk mechanical work so it does not burn paid tokens." The measured success rate of that
harness on this queue is 1 in 41. `codex` — which AGENTS.md names as the default for implementer
crews — has never completed a run in 29 attempts. The `pi-perf` survey reported "4% landed" and
treated it as a latency problem to be fixed with prompt caching; it is a *completion* problem, and
prompt caching does not fix a model that cannot finish.

**And bead supply is not the constraint.** `br stats` right now: 577 open, **569 ready to work**,
0 in progress, median age 14 days, 5 of them P0. The plan staffs a crew to *produce* beads for a
system that already has 569 it cannot drain. The constraint is demonstrably downstream of planning.

> **Investigate first:** dispatch three trivially-scoped beads — one to `pi`, one to `codex`, one to
> `claude-code` — and measure completion, not latency. If `pi` and `codex` cannot close a
> single-file comment-deletion bead, the two-crew plan has no cheap lane and the whole cost model
> has to be rebuilt around Claude.

---

## 1. Probe: does splitting the daemon reduce the 320s, or relocate it?

**It mostly relocates it, and the part that could genuinely compress runs straight into a known,
reproduced, load-triggered gate failure.**

### What I measured

`/usr/bin/time -l go test -count=1 ./internal/daemon`, cold, exit 0:

```
      305.59 real       101.80 user       106.76 sys
           284262400  maximum resident set size
             1040539  involuntary context switches
```

101.80 + 106.76 = **208.6 CPU-seconds against 305.6 wall-seconds on a 10-core box — 0.68 cores
average. The suite is 93% idle.** It is not computing. It is waiting.

Then I built the test binary once (`go test -c -o /tmp/daemon.test ./internal/daemon`) and timed
disjoint shards:

| shard | wall |
|---|---|
| `^TestT` (32 tests) | **188.47s** |
| `^TestRun` (63) | 11.07s |
| `^TestDot` (41) | 9.59s |
| `^TestGate` (20) | 1.12s |
| `^TestReplay` (20) | 1.08s |
| `^TestSpawn` (27) | 0.54s |
| `^TestHandler` (23) | 0.07s |
| `^TestComms` (29) | 0.06s |

Inside `^TestT`, the top five:

```
TestT4_ConcurrentLoops                60.32s
TestT6_10BeadSequentialDrain          42.80s
TestT6_ConcurrentBeadCreate           19.36s
TestThroughput_TenBeadsAtMaxFour      19.22s
TestT6_UnicodeHeavyBody               15.19s
```

### Why this matters

**62% of the daemon's test time is 32 whole-work-loop end-to-end tests** — concurrent dispatch,
bead drain, throughput at max-concurrent, signal handling mid-run, twin subprocesses. Not one of
them belongs to a subsystem. They exercise the loop, and they stay in whatever package still owns
the loop no matter how the substrate, the sweeps, the control plane or the harness resolvers are
carved out.

The subsystem-shaped shards a decomposition would actually move sum to roughly **24 seconds**. The
`daemon-seams` survey said this and was right ("617s of 815 test CPU-seconds live in full-work-loop
integration tests that stay behind"), but that finding did not propagate: `gate-scope` then
recommended adopting *"test-seconds resident in internal/daemon"* as the headline metric for the
whole program, and `charlie-merge-mechanics` recommended using the 317-second figure as "the
scoreboard."

**That metric is misleading in the one direction that matters.** Splitting will make the number
drop — the extracted packages' seconds move to a different row — while `make core` wall clock
barely moves, because `go test` already runs packages in parallel and the daemon row is the
critical path. A crew optimising a number that falls while the gate does not get faster is exactly
the "useless changes" failure the operator named.

### The mechanism nobody named that *could* help — and why it is dangerous here

Splitting can compress idle time, but not for the reason anyone stated. I measured:

- **503 of 1,160 top-level daemon `Test` functions never call `t.Parallel()`** at the top of their
  body (AST scan; source at `/tmp/par.go`). They are a hard serial chain.
- **81 `t.Setenv` / `os.Setenv` calls across 25 test files.** Go's `testing` package *panics* if
  `t.Setenv` is called after `t.Parallel`. Those tests can never be parallelised *within* a package.

Both constraints are per-process. Split the package and each new test binary gets its own
environment and its own serial chain, and `go test -p 10` runs those chains concurrently. That is a
real mechanism and it is the only honest argument that decomposition speeds the gate up.

**But raising concurrency in this suite is a measured, reproduced failure mode.** `gate-scope`
already found `-parallel 32` turned `TestDaemonStart_EmitsDaemonStarted` and
`TestDaemonStart_DaemonStartedInJSONLLog` red for a 3% gain. Worse — and nobody in twelve surveys
mentioned this — **`hk-c7eox` is an open P0 that says `make full` is deterministically red under
load on this branch**:

> Two full gates, two identical results. […] This is NOT a random flake. It is deterministic under
> full-gate conditions and green under isolation (5/5). The variable is load.

Root-caused in the bead's own comments to a race-detector fork/exec `SIGSEGV`
(`__tsan::TraceSwitchPartImpl -> syscall.forkAndExecInChild -> … -> daemon.runAgentLaunch`) plus an
unbounded test fixture. Two follow-on beads (`hk-trust-repair-budget-uncalibrated-6xryw`,
`hk-remote-trust-write-unverified-34gb0`) are still open.

So: `charlie-value`'s recommendation to *"make the crew's definition of done include a green
`make full`"* asks crews to satisfy a gate that is known-red under exactly the load the plan
generates.

> **Investigate first:** run the same disjoint-shard experiment concurrently rather than
> sequentially (two `/tmp/daemon.test -test.run` invocations at once) and check whether the wall
> time halves or the tests go red. That single experiment decides whether splitting compresses the
> gate or just fragments the metric. Nobody ran it, and it costs ten minutes.

---

## 2. Probe: keeping the queue full — what actually happens at the limit

**Nobody measured this machine. Not disk, not memory, not swap.** The `queue-supervision` survey
recommended raising `max_concurrent` from 4 toward ~20 on the strength of a *session-count* ceiling
alone. Here is the box.

### The hardware

```
hw.memsize: 17179869184        (16 GiB)
hw.ncpu: 10
/System/Volumes/Data   228Gi   156Gi used   33Gi avail   83% capacity
vm.swapusage: total = 7168.00M  used = 5707.06M  free = 1460.94M
```

**Swap is 80% consumed before any of this work starts.** 12 `claude` processes currently hold
2.5 GB RSS between them. Free pages at the time of sampling were ~205 MB.

### The disk arithmetic nobody did

Per concurrent run, measured:

- a fresh worktree is **106 MB** and takes **1.36s** to create (`git worktree add --detach`); the
  two live run worktrees measure 125 MB and 293 MB once built
- a lane Go cache is **493 MB – 1.2 GB** (`du -sh ~/Library/Caches/harmonik-lane-gocache/*`)

That is roughly **600 MB – 1.5 GB per bead**. And the caches **are never reaped, by explicit
design.** `scripts/with-lane-gocache.sh` says so in its own header:

> "DISK, and this one has a sharp edge. Each checkout keeps its own cache, so N checkouts cost N
> caches, and **a cache outlives the worktree that made it**. Agent worktrees are created and
> thrown away constantly here, and nothing reaps what they leave behind."

Right now there are **8 lane caches totalling 6.2 GB and only 2 of them have a live worktree.**

The daemon stops dispatching below **10 GiB free** (`diskLowWatermarkDefault`,
`internal/daemon/workloop.go`), and `internal/daemon/scheduler.go` does it by sleeping the poll
interval and `continue`-ing — **silently**. From `docs/disk-reclaim.md`:

> "below the daemon's `diskLowWatermarkDefault` (10 GiB) dispatch is **skipped silently**. A full
> disk therefore manufactures fake test failures and fake hangs fleet-wide […] **That is not
> hypothetical. On 2026-08-15 it cost two days.**"

**So the budget is 33 − 10 = 23 GiB, and it is consumed cumulatively, not concurrently, because the
caches persist after the run ends.** At ~700 MB of durable residue per bead, the fleet reaches the
watermark after roughly **33 beads** — less than half of its own best day. Then it stops, and says
nothing.

### The CPU and memory arithmetic nobody did

`internal/daemon/bootsocket.go` sets the ceiling at `runtime.NumCPU() * 4` = 40 sessions, so
`queue set-concurrency` accepts up to about 20. Its stated reasoning:

> "An agent session is a tmux window running an agent CLI that spends most of its life waiting on a
> model response, so it is not one busy core each and a small multiple is right."

**That reasoning omits the commit gate.** Every bead's workflow runs a `commit_gate` node whose
`tool_command` is `make core` — 29 packages, `GOMAXPROCS` 10, measured at 474s — inside the run's
own worktree. N concurrent beads at the gate means **N concurrent full test suites on a 10-core
box**, each with its own multi-hundred-megabyte cache. That is a genuinely CPU-and-memory-heavy
workload, not a waiting one, and the ceiling that governs it looks at neither RAM nor disk.

At `max_concurrent = 4` this is already plausible. At 20 it is not survivable on 16 GB with 1.3 GB
of free swap — and the failure mode is not a clean refusal, it is the load-triggered gate red of
`hk-c7eox` plus a silent disk-low dispatch stall.

> **Investigate first:** (a) run `make core` in three worktrees simultaneously and record wall time,
> peak RSS, swap delta and exit codes — that is the real shape of a full queue; (b) decide who reaps
> `harmonik-lane-gocache`, because nothing does today and it is the largest per-bead residue;
> (c) instrument the disk-low stall so it is loud, since it has already cost two days once.

---

## 3. Probe: does mass comment deletion destroy something this project depends on?

**Yes, and the two surveys contradict each other on exactly the wrong file class — neither noticed.**

`comment-reduction` recommends shipping *"Phase A now with zero model involvement: delete only the
comment blocks that float inside a function body […] It is the largest cut that needs no judgment
at all."*

`comment-quality`, separately, names *"the highest-value prose in the repo"* and *"the most likely
thing to be lost."* It names two comments. **Both of them are free-floating in-function blocks —
precisely what Phase A deletes.**

I verified position by AST (`/tmp/scar.go`), not by eye:

**`internal/daemon/workloop.go:843`**, inside `beadRunOne` (function opens at line 143), between two
statements:

> "Do NOT add a pre-dispatch 'already landed on main?' check here. One used to sit at this point and
> closed the bead when the main history had its Refs — a bare `Refs: <id>` git-log grep — matched.
> On a bead worked in several parts an older partial commit carries the same ID, so the grep matched
> and the daemon closed a bead whose remaining work had not run."

**`internal/harness/claude/launchspec.go:124-146`**, inside `BuildLaunchSpec` (opens at line 78):

> "LOCAL: DELIBERATELY NOT ISOLATED — do not re-add (hk-8juwz). The local isolation was tried
> (a964cbcb) and LIVE-REFUTED by an A/B on one daemon with one line toggled: isolation ON →
> agent_ready_timeout at 150s with the pane parked on the Bypass Permissions modal; isolation OFF →
> agent_ready in 2.0s."

Census: **17,382 in-body comment groups** across `internal/` and `cmd/`. Of those, **13 in
production carry a do-not-redo directive**; 50 more sit outside function bodies. Thirteen lines of
text out of tens of thousands, and they are the ones that stop an agent re-running a refuted
experiment.

A third example the surveys missed entirely, and it is the best one:
**`internal/daemon/diskcheck_hksxlb.go` opens with a 41-line header whose entire content is a
prohibition**:

> "This path used to run `go clean -cache` when disk went below the watermark. That is removed and
> **it must not come back in any form**. […] Deleting it mid-build gave concurrent test suites
> 'could not import os/context/testing/... no such file or directory', and — worse — green runs that
> never actually rebuilt anything. A build that reports success without building is a wrong answer
> that looks like a right one."

### The cheap check nobody ran

Some scars are backed by tracked documents and some are not, and **nobody checked which**. I did,
for three:

| scar | also recorded in a tracked file? |
|---|---|
| pre-dispatch already-landed grep | **yes** — `specs/beads-integration.md` BI-022 informative note names `hk-f38n`, `hk-cmry`, `hk-zmpd` |
| `go clean -cache` prohibition | **yes** — `docs/disk-reclaim.md`, `docs/known-workarounds.md`, `docs/emergent-tooling-inventory.md` |
| Claude config-dir isolation refutation | **partly** — `docs/incidents/2026-07-21-daemon-wedge-rollback.md` |

That is the actual prerequisite for a comment pass and it takes an afternoon: for each of the 63
trap-bearing comment groups, does a tracked doc or spec carry the same lesson? Cut the backed ones
freely; harvest the unbacked ones into a doc *before* cutting. Neither survey proposed this.

### The gate nobody tested

`comment-quality` inferred that *"an agent-reviewer reading a diff that deletes hundreds of comment
lines will read it as removing documentation and return REQUEST_CHANGES."* It is an inference and
nobody tested it. I read the reviewer's flag vocabulary in
`.claude/skills/agent-reviewer/SKILL.md`: **28 tags, and not one concerns comments or
documentation.** There is no flag to fire and no flag that says it is fine. Whether a bulk comment
diff passes is therefore an undefined Opus judgment call — and it gates the entire DGX lane.

One flag *does* bear on the extraction work and nobody quoted it:

> `orphaned-reader` — "a test deleted alongside the code it covered leaves that behaviour asserted
> nowhere. **BLOCK** when a reader is orphaned with no replacement."

Every extraction that moves tests will meet this.

> **Investigate first:** run one real comment-reduction bead end to end through the live workflow
> and read the verdict. If it comes back `REQUEST_CHANGES` or `BLOCK`, the DGX lane does not exist
> until the reviewer criteria are amended — and amending review criteria is a rule-file change,
> which `rule-file-bundled` says must be its own commit.

---

## 4. Probe: charlie versus the decomposition — the ordering conflict is real, but it is not the merge

Both charlie surveys concluded "the merge already happened, move on." That is true and it hides the
actual conflict.

**The live daemon is not running charlie's code, and has never run it.**

```
$ go version -m /Users/gb/go/bin/harmonik | grep vcs
        build   vcs.revision=c070905d5231211079878bdce83584d16ef99e76
        build   vcs.time=2026-08-15T15:36:40Z
$ git log -1 --format="%H %ci" HEAD
43ad681c461ef3e5b98b72a9566eb88fae25ef1b 2026-08-22 01:14:01 -0700
```

Daemon pid 82451 has been up since 2026-08-15, under supervisor pid 81414. **It is 93 commits (71
non-merge) behind HEAD.** Not one of charlie's 61 commits, and none of the three audit-and-repair
commits, has executed in production.

The undeployed live-path delta is exactly the code the pre-merge audit flagged as risky:

```
internal/daemon/bootreconcile.go        | 273 ++++++++++-----
internal/workspace/discoverworktrees.go | 314 ++++++++++++-----
internal/daemon/orphansweep.go          | 244 +++++++++-----
internal/daemon/scheduler.go            |  95 +++----
internal/workers/registry.go            |  89 ++++++-
```

**So there is an unscheduled, mandatory, risky step sitting between "plan the decomposition" and
"dispatch the first bead": redeploying the daemon.** That redeploy is what arms, all at once —

- the rewritten orphan sweep and the rewritten `discoverworktrees` it reads,
- `internal/workers/registry.go` made **fatal at boot**,
- `preflightDispatchReplay`, which the adversarial check found returns a fatal error on **any**
  non-empty intent list, not merely on an unimplemented action.

Twelve surveys and nobody sequenced it. `docs/daemon-redeploy.md` is the runbook and no
recommendation points at it.

**The genuine file-level conflict is between four streams that all want the same files:**

| stream | target |
|---|---|
| Charlie C22 | pure queue selection out of `selectNextQueue` — `internal/daemon/scheduler.go` |
| Charlie C23 | one run supervisor — new type in `internal/daemon` |
| substrate capability contract T1–T5 | rewrite inside `internal/daemon/tmuxsubstrate.go` |
| `daemon-seams` proposal 4 | **move** `tmuxsubstrate.go` to a new package |
| `daemon-seams` proposal 1 | `runregistry.go` (18 consumers, 21 production + 26 test files touched) |

Two of these rewrite inside `tmuxsubstrate.go` while a third moves it. `scheduler.go` took 40
commits in 30 days and `workloop.go` took 74. Nobody assigned single-writer ownership across these
streams.

**And three unmerged branches carry real work into the same tree:**

- `work/alpha-trust-isolation` — 5 files, +721/-83, touches `internal/workspace`
- `work/bravo-reachability` — 21 files, +1,950
- orphaned commit `b585a210b` in a run worktree — `internal/runmerge/reviewtrailers.go`, not an
  ancestor of HEAD, the BLOCKed Pi run's output

> **Investigate first:** decide the redeploy before anything else. Either redeploy and prove the
> boot path on a scratch project first, or state explicitly that the decomposition runs on the
> 2026-08-15 daemon — but do not let a crew discover the answer by triggering a supervisor revive
> mid-program.

---

## 5. Angles nobody ran at all

### 5.1 Nobody costed the reviewer, and it is pinned to Opus

`internal/daemon/standard-bead.dot`:

```
    review [
        type="agentic",
        agent_type="reviewer",
        model="claude-opus-4-8",
        harness="claude-code",
```

**Every bead gets an Opus review.** Measured from the event log: 2,697 `reviewer_verdict` events
across 1,188 runs = **2.27 Opus review passes per run**. The distribution has a spike at exactly 4
(344 runs — the iteration cap). Verdicts: 2,367 APPROVE, 308 REQUEST_CHANGES, 22 BLOCK.

The plan's whole cost argument is "put bulk work on the DGX model so it does not burn paid tokens."
A high-volume mechanical queue is a **high-volume Opus-review queue**, and the reviewer is pinned in
the graph specifically so it cannot fall back to a cheaper harness (`hk-z4nif`, the Pi-reviewer
seed-paste bug). At 2.27 Opus passes per bead, a thousand mechanical beads is a thousand-bead Opus
bill that no survey put a number on.

Note the interaction with §0: `pi` fails 40 runs out of 41, and **a failed run still consumes its
Opus reviews.** The cheap lane is not cheap when it fails; it is Claude-priced with a two-hour
latency.

### 5.2 Nobody asked what a 48% failure rate does to a full queue

1,060 failed against 1,162 completed. A full queue at that rate spends half its capacity on runs
that end in nothing — and each of those leaves a worktree and a never-reaped Go cache behind
(§2). The plan treats queue depth as the thing to maximise without a stated position on the
failure rate.

### 5.3 Only 5 of 12 surveys were adversarially verified, and 3 of the 5 came back corrected

Verified: `charlie-value` (HOLDS, with 5 corrections), `daemon-seams` (HOLDS, with 7 corrections
including a hard import cycle that made proposal 3 non-compiling), `comment-reduction`
(**CORRECTED** — its central safety claim was false), `queue-supervision` (HOLDS, but its top
recommendation rested on a field never populated in 2,222 events), `lint-inventory`
(**CORRECTED** — its mechanical tier was overstated by 42%).

Unverified: `charlie-merge-mechanics`, `daemon-anatomy`, `prior-art`, `comment-ratio`,
`comment-quality`, `pi-perf`, `gate-scope`.

**Three of five verified surveys had a load-bearing claim overturned.** That is the base rate. The
seven unverified surveys are the ones the plan will lean on hardest — `daemon-anatomy` supplies the
cluster map, `prior-art` supplies the backlog, `gate-scope` supplies the gate design — and none of
them has been checked by anyone.

### 5.4 Load-bearing claims explicitly flagged low-confidence and never chased

- `gate-scope` on rename detection in the changed-line lint step: *"I did not measure this — I read
  the script and it passes the flag through untouched."* This is the mechanism that decides whether
  every moved file's findings report as new. It gates every extraction commit and it is unmeasured.
- `daemon-anatomy` on where the 320s comes from: *"The claim that process spawning rather than
  sleeping dominates the 320s is inference."* I have now measured it — 0.68 cores, so it is
  *waiting*, and §1 says which tests.
- `queue-supervision` on Monitor availability: crews on Codex or Pi have no Monitor tool, so the
  event-driven wake does not exist for them. The plan puts the dispatching crew on a cheap harness
  and the loop it needs is Claude-Code-specific.

### 5.5 Nobody wrote an abort story

There are 18 freeze-gate and ratchet scripts in `make freeze-gates`, plus a lint allow-list ratchet
whose own message reads *"The allow list only ever gets shorter. Adding a pair to it is the one
repair that is not allowed."* A decomposition that is abandoned half-done leaves those gates in a
state nobody has described. No survey said what "stop and revert" looks like after slice three of
eleven.

### 5.6 Nobody measured the test-side cost of extraction

95,594 lines of daemon tests. 68 test files reference `tmux`, 26 shell out to `git`, 602 `t.TempDir`
calls, 140 `Exported*` shims. `daemon-seams` verified test travel for **one** cluster (the 27
control-plane files, all external `package daemon_test`). `daemon-anatomy` says 662 of 1,149 test
functions are white-box and reach unexported internals. Nobody reconciled those two views, and the
gap between them is the difference between "extraction is hours" and "extraction is weeks."

---

## Ranked: what to investigate before trusting this plan

1. **Can this fleet close a bead at all?** Dispatch three trivial beads — one each to `pi`, `codex`,
   `claude-code` — and measure completion, not latency. Seven beads in five weeks, `pi` at 1-for-41
   and `codex` at 0-for-29, is the fact that decides whether any of the rest is worth planning.
   *(hours)*

2. **What does concurrency actually do to this box?** Run `make core` in three worktrees at once;
   record wall time, peak RSS, swap delta, exit codes. 16 GB with 1.3 GB free swap, 33 GiB disk
   against a silent 10 GiB dispatch cutoff, and ~700 MB of never-reaped residue per bead. Do this
   before touching `max_concurrent`. *(hours)*

3. **Is `make full` red under load on this branch?** `hk-c7eox` is an open P0 that says yes,
   deterministically, twice. Every "definition of done" recommendation in the research assumes a
   green gate. Settle it before writing acceptance criteria. *(hours)*

4. **Does splitting compress the gate or just fragment the metric?** Run two disjoint daemon shards
   concurrently versus sequentially. 62% of the time is 32 whole-loop tests that no split moves;
   the only compression mechanism is cross-package parallelism, and this suite is known to redden
   under parallelism. *(hours)*

5. **Redeploy the daemon, or state that you are not going to.** The live daemon is 93 commits and
   seven days behind HEAD; none of charlie's code has ever run. The redeploy arms the boot-abort
   preflight, the fatal-at-boot worker registry and the rewritten orphan sweep, all at once.
   *(hours, then days if it goes wrong)*

6. **Test one comment-reduction bead through the live review gate.** The reviewer's 28-flag
   vocabulary has nothing about comments. The DGX lane's viability is an untested Opus judgment
   call. *(hours)*

7. **Audit the 63 trap-bearing comments for tracked backing before any comment pass.** Thirteen
   in-function production comments carry do-not-redo directives, including the two the research
   itself calls the most valuable prose in the repo — and "Phase A, zero judgment, low risk" deletes
   exactly that class. *(afternoon)*

8. **Assign single-writer ownership across the four streams that target `tmuxsubstrate.go`,
   `scheduler.go` and `workloop.go`.** Two streams rewrite inside the substrate file while a third
   moves it; `workloop.go` took 74 commits in 30 days. *(hours)*

9. **Put a number on the Opus review bill.** 2.27 Opus passes per run, pinned in the workflow graph,
   paid on failed runs too. The plan's cost argument does not survive without it. *(hours)*

10. **Adversarially verify `daemon-anatomy`, `prior-art` and `gate-scope`.** Three of five verified
    surveys had a load-bearing claim overturned; these three supply the cluster map, the backlog and
    the gate design, and none has been checked. *(days)*

11. **Measure the changed-line lint's behaviour on a renamed file.** Explicitly unmeasured, and it
    decides whether every extraction commit reports its whole file as new findings against a ratchet
    that forbids the repair. *(hours)*

12. **Write the abort story.** 18 freeze gates and a one-way lint ratchet, and no stated way to stop
    a half-finished decomposition. *(hours)*
