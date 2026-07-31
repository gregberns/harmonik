# Prior art — what already exists on abandoned branches, and what of it is worth taking

**Date:** 2026-07-28
**Scope:** three named sources of possible prior art for the Phase 3 run-machine decomposition, plus the
`reviewloop.go` safety floor that constrains any of it. Read-only analysis; one document, no code changed.
**Measured against:** `bcc53af1a` (`phase1-session-restart-substrate` tip). The tip moved twice during
this pass; every count below was re-derived against the final one.

**This is a findings document, not a plan. Nothing in it is scheduled work.** Its job is to stop the
decomposition re-deriving work that already exists, and to stop it adopting work that looks finished and
is not.

Siblings: [`CHARTER.md`](CHARTER.md) — what the program is. [`DECOMPOSITION-MAP.md`](DECOMPOSITION-MAP.md) —
where the seams are. [`NEXT_STEPS.md`](NEXT_STEPS.md) §Deferred item C — the claim this document verifies.

---

## 0. The headline

**Item C is already done — and chasing it turned up something better than prior art: the reviewed spec no
longer describes the shipped review loop.**

The two "reviewed-but-unwired pure decision kernels" `NEXT_STEPS.md` item C says to cherry-pick were
cherry-picked onto this line on 2026-07-28, hours before this hunt started. Both build and pass, and both
have zero production callers.

The `reviewcycle` kernel does two things that `specs/execution-model.md` — `reviewed`, v0.9.5 — says it
must not: it never emits `no_progress`, and it treats a flagless `REQUEST_CHANGES` as an approval. **Both
are shipped daemon behavior.** `emitNoProgressDetected` in `internal/daemon/reviewloop.go` has **zero call
sites anywhere in the tree**, and `hk-thbbv` already routes a flagless `REQUEST_CHANGES` to APPROVE in the
live driver. So the kernel is a faithful extraction, and the spec is the thing that drifted — two
behaviors shipped under bead IDs (`hk-togxq`, `hk-thbbv`) without the amendment `CHARTER.md` §3 requires.

This inverts the disposal of the abandoned branch's spec commit. `88f36c15d` was assessed as an unlanded
draft to be wary of; it is in fact **the only place the shipped behavior is written down**. Its
`execution-model.md` text is worth harvesting *because* it describes what already runs — while the same
commit's `reviewed → draft` demotion is still a regression to refuse. Harvest the clauses, not the file.

| Source | Verdict | The measurement |
|---|---|---|
| `harmonik-wt/lift-l8-reviewloop` | **Abandon** | 0 commits ahead of our tip, 129 behind. Its L1–L7 already landed. The remaining L8 move cannot compile — depguard forbids the import it needs. |
| Item C's two kernels | **Already harvested.** `reviewcycle` is good prior art; `continuity` is not | Both pass, both unwired. `reviewcycle` 81.2% coverage / ~27 behaviors pinned, and faithful to shipped behavior. |
| `/private/tmp/harmonik-main-integration-20260725` | **Abandon the branch, harvest two spec clauses** | 44 ahead / 105 behind; 25 of the 44 are probe garbage; merging demotes `execution-model.md` from `reviewed` to `draft`. |
| Three further commits on that branch | **Take none** | All three apply cleanly. All three have zero callers or pin signatures rather than behavior. |

---

## 1. The stale-tip trap fired again, on the first move

Before any source was examined, this agent's own worktree measured **414 commits behind** the base branch
— a pre-`PRINCIPLES.md` `CLAUDE.md`, and a `plans/` directory with no `CHARTER.md`, `NEXT_STEPS.md`, or
`DECOMPOSITION-MAP.md`. Every source below would have been compared against a tree from before the
deletion program began, and every delta would have been wrong in the same direction: inflated, with the
inflation looking like payload.

Same shape as the branch sweep one day earlier. The lesson is not "check the tip" — it is that freshness
is not a property of a worktree but a fact that decays, so the check is the **two-way** count at the
moment of use: `<branch>..<tip>` for how stale it is, `<tip>..<branch>` for what it actually carries.
Running only the second is what makes garbage look like payload.

---

## 2. Source 1 — `harmonik-wt/lift-l8-reviewloop`: abandon

`NEXT_STEPS.md` §D keeps this worktree for "2 staged renames moving `reviewloop.go` into `internal/runloop`
— relevant to the run-machine decomposition (step 6)." The renames are there. They are not relevant.

### What is actually there

Branch `refactor/lift-l8-reviewloop` at `44266076e`:

```
git rev-list --count bcc53af1a..44266076e  →  0        # nothing ahead
git rev-list --count 44266076e..bcc53af1a  →  129      # 129 behind
git merge-base 44266076e bcc53af1a         →  44266076e
```

**The branch tip is an ancestor of our tip.** Its seven commits — `LIFT L1` through `LIFT L7`, moving the
socket-grace wait, run shell, dispatch segment, scenario gate, run bridge and reviewer harness into
`internal/runloop` — all landed. `internal/runloop/` on our tip already contains every one of them.

The branch's entire unique contribution is **uncommitted working-tree state**:

- staged: `internal/daemon/reviewloop.go` → `internal/runloop/reviewloop.go` (pure rename, 0 content lines)
- staged: the same for `reviewloop_test.go`
- unstaged: 16 insertions / 17 deletions across the two — `package daemon` → `package runloop`, drop the
  `internal/runloop` import, strip the now-redundant `runloop.` qualifier from ~14 call sites.

That is the mechanical half of a package move, done correctly, and stopped.

### Why it stopped, and why resuming it does not work

Most of the package-private identifiers `reviewloop.go` uses are declared in `reviewloop.go` — every `rl*`
helper, every review-loop event emitter, and the `substrateRunnerObserver` test seam (`hk-fxy9`, read only
by `notifySubstrateRunner`, also local). Those move with the file. **The real external-coupling surface is
18 symbols**, and it is not diffuse:

```
pasteinject.go            pasteInjectOnLaunch  pasteInjectQuitOnCommit  pasteInjectQuitOnReviewFile
                          ReadReviewerBudgetSentinelVia  quitSender
sessioncontext_chb023.go  emitClaudeSessionIDPersisted  newSessionIDInterceptor
                          persistClaudeSessionID  sendVersionSelectedACK
tmuxsubstrate.go          newPerRunSubstrate  substrateSpawnStats  pasteInjecter
                          ErrSpawnCapTimeout  ErrTmuxNewWindowTimeout
                          defaultSpawnAcquireTimeout  defaultNewWindowTimeout
harnessregistry.go        routedLaunchSpecBuilder
claudeheartbeat.go        newDaemonHeartbeatEmitter
```

Three clusters and two singletons — paste-injection, session-context/CHB-023, and the tmux substrate.
18 reconciles with the `go/types` baseline of 26 in `f03ff7c1b` (§5); the numbers below did not.

**This document reported 32, then 13, before arriving at 18, and the way it kept being wrong is the point.**
The first grep compared `reviewloop.go`'s call set against declarations in `internal/daemon/*.go` — a glob
that includes `reviewloop.go`, so everything the file declares itself came back as "coupling". The second
over-matched method names. The third matched only `identifier(`, so it saw function calls and missed the
error values, constants and interface types above — six real couplings, invisible because they are never
*called*. **A grep cannot answer "declared here or elsewhere", and a call-shaped grep cannot see
non-call references; a resolver does both.** That is precisely why `f03ff7c1b`'s census is worthless as a
ratchet and its *baseline* is the only trustworthy measurement of this surface anyone has produced.

For the moved file to compile in `internal/runloop`, that package must import `internal/daemon`.
**`.golangci.yml` forbids it** — the `runloop` depguard component's allow-list is the measured import set
and does not include `daemon`; `internal/runloop/ports.go` states the rule in its own header (*"must NOT
import internal/daemon (depguard fences that edge …)"*). The lift's own scaffolding says the same:
`reviewerharness_hkiv748.go` carries *"Temporary export: reviewloop.go still calls this from
internal/daemon. LIFT L8 …"*.

So L8 is blocked behind the later lifts moving those 18 symbols first. The work is not half-done; it is
**correctly stopped at the point where the cheap half ran out** — and the job is materially smaller than
this document first claimed.

### The verdict, and it is not about difficulty

Even if the import problem were solved, this is the wrong shape of work.
`DECOMPOSITION-MAP.md` §6 retires the extraction-in-place framing outright: *"The 2026-07-27 decision is
that decomposition-in-place cannot save this code."* `reviewloop.go` is 2,194 lines whose single function
`runReviewLoop` is 1,584 of them, and the map's §5 puts it in the rotten centre — *"the same disease as
`workloop.go`, not a neighbour of it"*, to be rewritten with `workloop.go` and `dot_cascade_core.go` **as
one unit**.

Moving a file that is going to be rewritten changes its directory and nothing else. **Abandon the
worktree and the branch.** Nothing is lost: L1–L7 are on our tip, and the L8 diff is 33 lines of
mechanical qualifier-stripping that any future move regenerates in minutes.

One thing is worth carrying forward — **the 18-symbol list above is the size of the job for anyone
separating the review loop from the daemon by any method, rewrite included**, and it is three clusters
rather than a diffuse surface. Two of the three (paste-injection, the tmux substrate) are already named in
`DECOMPOSITION-MAP.md` §5 as rot to be rewritten; the third (`sessioncontext_chb023.go`) is the
session-continuity work the harvested `continuity` kernel models. So the coupling that blocks the move is
almost entirely coupling the rewrite was already going to touch.

---

## 3. Source 2 — item C's two kernels: already harvested, and one of them is loaded

### They are on this branch already

`NEXT_STEPS.md` item C says *"Cherry-pick those two packages; still do not merge the branch."* Done, on
2026-07-28, before this hunt began:

| Origin (abandoned branch) | Landed here as | When |
|---|---|---|
| `3f02f2f9a` feat(reviewloop): add pure review-cycle kernel | `7f73548d2` + `128d3c88b` (lint conformance) | 07:52 / 08:04 |
| `b62f75b9f` + `c9b957a5c` continuity checkpoint contract | `2b3fad9b7` | 08:05 |
| — | merged at `1b56dafb5` | 12:05 |

All three verified ancestors of `bcc53af1a`. Both packages build and pass:

```
ok  internal/runloop/continuity   0.394s
ok  internal/runloop/reviewcycle  0.854s
```

The harvest was faithful. The diff against the origin is lint conformance (comments on `const` blocks, one
`validate` split for cyclomatic complexity) plus one deliberate rewording: every reference to *"an EM-023a
**context-checkpoint** transition"* became *"a **durable transition** per EM-023a"*. That is not cosmetic —
it is the seam where the harvest correctly **declined** to take the origin commit's other half.

### What was correctly left behind

`b62f75b9f` also changed `internal/core` — `transition.go`, `transitionkind.go`, `evidence.go`,
`durability.go` — to add `context-checkpoint` as a new transition kind. None of that came across. It
belongs to the abandoned branch's unlanded `execution-model.md` v0.10.0 draft, which widens the durable-
transition kind set. Taking the kernel while leaving the core vocabulary alone, and rewording the kernel's
comments to match, was the right call and is worth naming as such: **the harvest separated a design from
the spec change it assumed.**

### `reviewcycle` — genuinely good, and the best model available for the mode boundary

`Decide(State, Observation) -> (Decision, error)`. A free function, no receiver, no `context.Context`, no
clock, no ports. It returns an ordered `[]Intent` (`IntentPersistIterationFacts`,
`IntentDispatchFreshReviewer`, `IntentPublishRawVerdict`, …) that a shell interprets — the same shape as
`internal/runexec`'s `[]Action`, which `DECOMPOSITION-MAP.md` §4a calls *"the best code in the run path"*.
It deep-clones every pointer field on ingress and egress, with a property test pinning that.

Measured against `runexec` as the baseline:

| | prod LOC | test LOC | coverage | behaviors pinned |
|---|---|---|---|---|
| `reviewcycle` | 652 | 715 | 81.2% | ~27 |
| `continuity` | 475 | 640 | 82.8% | ~21 |
| `runexec` (baseline) | 925 (+233 vocab) | 1,216 | 95.6% | 45 test funcs |

The tests are real: a 12-case decision table over `Decide`, four property tests (iteration monotone and
bounded over caps 1..12, terminal absorption, defensive slice handling), a 4-case intent-ordering table,
a 5-case invalid-input table. Not signature theater.

**In one respect it is better than `runexec`** — `Decide` has no receiver state and no `context`
parameter, so its purity is structural rather than conventional. In two respects it is behind: coverage
(81.2% vs 95.6%) and enforcement (see below).

### The kernel is faithful to the code and divergent from the spec — and the spec is what is wrong

`reviewcycle` does two things `specs/execution-model.md` (`reviewed`, v0.9.5) says it must not. The
first reading of this was that the kernel had imported unlanded spec changes and was unsafe to wire. **That
was wrong, and the correction is the most useful thing in this document.** Both behaviors already ship.

**(a) It never emits `no_progress` — and neither does the daemon.** A property test,
`TestPropertyBuiltInNeverProducesNoProgress`, pins the kernel's refusal;
`ReviewLoopCompletionReasonNoProgress` survives only as an accepted-but-unproduced legacy value. Our
reviewed EM-015d still routes `{… no-progress: close-needs-attention}` and still says *"the daemon MUST
emit `no_progress_detected`"*, and `event-model.md` §8.1a's ordering rule requires it whenever no prior
`REQUEST_CHANGES` exists — plus a normative note requiring it from DOT mode.

Measured: **`emitNoProgressDetected` has zero call sites in the entire tree.** It is defined in
`reviewloop.go` and called from nowhere — not the review loop, not the DOT cascade. The live driver routes
on HEAD advancement via `state.lastIterHeadSHA` and emits `emitReviewFixupStalled` instead; the code
comment records the reason (`hk-togxq`: *"The progress signal is now HEAD advancement … NOT diff-hash
equality"*). `no_progress_detected` remains registered in `internal/core` with a typed payload and a
compat-window entry, so it looks entirely alive to every mechanical check.

**(b) It treats a flagless `REQUEST_CHANGES` as approval — and so does the daemon.** `hk-thbbv` in
`reviewloop.go` does exactly this, with the rationale in-line: a flagless `REQUEST_CHANGES` *"carries no
actionable item … the implementer then finds nothing to change, `/quit`s without a new commit, and the
next iteration's no-progress guard … wedges the run even though nothing is actually wrong."* Nothing in
`specs/execution-model.md` mentions flag-conditioned actionability; `grep` for `flagless` returns nothing.

**So the kernel is a faithful extraction of shipped behavior, and the reviewed spec is stale on both
counts.** Two behaviors shipped under bead IDs without the spec amendment `CHARTER.md` §3 requires — the
same silent-violation failure the charter names, caught here from the opposite direction. This is a
concrete instance of `NEXT_STEPS.md` §1's category C (*"stale — described a real design since replaced"*),
found in `execution-model.md`, one of the specs its table rates **healthy at 5% orphan rate**. The
orphan-rate signal does not catch polarity drift; §1 says so, and this is the proof.

Two consequences:

1. **Wiring `reviewcycle` is safer than it looked** — it would not change behavior. The real hazard is the
   opposite one: wiring it, discovering the spec disagrees, and "fixing" the kernel to match a requirement
   that describes a design `hk-togxq` replaced. `specs/` is the rewrite's oracle, and on this path it
   would have pointed the wrong way.
2. **`emitNoProgressDetected` is dead code** with a live-looking event registration behind it. Recorded,
   not chased — but it belongs on the dead-code list, and the registration is exactly the "dead and
   unfinished look identical" hazard `CHARTER.md` §5 names.
3. **The oracle tier contains a test asserting the dead event.**
   `TestScenario_EM015e_NoProgress_ReviewerNotLaunched`
   (`internal/daemon/scenario_reviewloop_em015de_hkintln_test.go`) asserts both
   `completion_reason == no_progress` and that `no_progress_detected` is emitted before
   `review_loop_cycle_complete` — for a fixture whose iteration 1 ends in `REQUEST_CHANGES`, which is the
   `fixup_stalled` path in the live driver. Since nothing emits the event **on the review-loop path**,
   that test either fails or does not run.

   > CORRECTION (main session, 2026-07-28, verified before landing this doc). The unqualified claim
   > "nothing emits the event" is too strong and is corrected here rather than left to mislead. The
   > *function* `emitNoProgressDetected` in `reviewloop.go` is genuinely dead — zero callers, confirmed.
   > But a SECOND emitter of the same event is live: `emitDotNoProgressDetected`
   > (`internal/daemon/dot_cascade_helpers.go`), called from `dot_cascade_core.go`. So the event type and
   > its registration are NOT dead, and a mechanical "is this event ever emitted" check will correctly
   > answer yes. What is dead is the review loop's own emitter, which is exactly the path
   > `TestScenario_EM015e_NoProgress_ReviewerNotLaunched` exercises (it enters through
   > `ExportedRunReviewLoop`), so the conclusion about that test stands on the narrowed premise.
   > Note this is a *fourth* instance of the 1-of-N drift pattern the decomposition map predicts: two
   > drivers implementing the same step, one of which quietly stopped calling its own copy.
   > **Open and NOT resolved:** whether that scenario test currently fails or silently does not run. The
   > two possibilities have different dispositions and nothing in the tree distinguishes them. The
   > nightly's failure-masking was removed this session, so the next nightly run answers it. It is `//go:build scenario` and `-short`-skipped, and per `NEXT_STEPS.md` §5 the scenario
   workflow carries `continue-on-error: true` and no required status check. **This is the strongest
   available argument for `NEXT_STEPS.md` §5.1** — `DECOMPOSITION-MAP.md` names this tier the rewrite's
   only oracle, and it currently contains a test pinning behavior the product does not have, with nothing
   to surface that. Recorded, not chased.

**And the abandoned branch's spec commit inverts with the kernel.** `88f36c15d` rewrites EM-015d to
`{APPROVE: close, actionable REQUEST_CHANGES: implementer, BLOCK: …, iteration-cap: …, fixup-stalled: …}`
with actionability defined as non-empty `flags` — i.e. it is **the only correct description of the shipped
review loop that exists anywhere**, stranded on a dead branch. Its clauses are worth hand-copying for that
reason, not despite it. What must not come with them is the same commit's `status: reviewed` v0.9.3 →
`status: draft` v0.10.0 demotion (§4). Harvest the clauses; refuse the front matter.

A third clause in it is worth having on its own merits and is **not** in our specs: the
reviewer time budget as a formula rather than as scattered constants —
`allowance = clamp(base + per_kline × changed_lines/1000, base, hard_ceiling)`, with liveness grace
extending in `base` increments and a one-shot 75-second reseed that resets nothing. The **numbers**
already live in `internal/daemon/pasteinject.go` (`reviewFileTimeout` 10 m, `reviewFilePerKLineBudget`
10 m, `reviewFileHardCeiling` 60 m, `implementerReseedGrace` 75 s, `reviewerHeartbeatActiveGrace` 10 m);
what is missing is the statement that they compose into a clamped allowance and that liveness evidence is
liveness only — never progress, never a reset. `DECOMPOSITION-MAP.md` §4a already flags these as
CARRY-FORWARD fact 18 ("each set by a production false-kill, several twice") and says keep the values,
change the mechanism. This clause is the mechanism, written down.

### `continuity` — good code, wrong lesson

It is well-built and well-tested, and it is **not** a functional-core exemplar. `Service.Execute` calls
`CheckpointPort.EnsureCommitted` (git + persistence) and `VersionSelectedPort.SendVersionSelected` (a
protocol write). It is a shell. Its own test is named `TestProductionPackageRemainsEffectFree`, which is
false as written and will mislead anyone who copies the pattern believing they are copying a pure core.

It teaches `PRINCIPLES.md` §4 (consumer-owned ports — it declares both ports itself and imports nothing
outward) and not §1. Neither port has an implementation anywhere in the tree: the only `EnsureCommitted`
in the repo is a test recorder. **The boundary has never met real git or a real protocol write**, so its
shape is a hypothesis.

What is genuinely valuable in it is about 30 lines of contract, not the package: the checkpoint must be
durably committed **before** the `version_selected` handshake is sent, and on a post-checkpoint handshake
failure the committed checkpoint is returned **inside** the typed error so the irreversible fact is not
lost. That ordering is pinned by a test that asserts call *sequence*. Keep it as a stated obligation for
whatever the decomposition builds; do not copy the shape.

### `purity_test.go` is a bespoke lever on top of one that already exists

Both packages carry a near-identical ~68-line AST test: parse every non-test `.go` file, assert every import
is in a hardcoded allowlist, no package-level `var`, no `go` statement, no `defer`.

It is real — add `"time"` and it goes red — but it is narrower than its name and it duplicates existing
enforcement. **Both packages are already inside the `.golangci.yml` `runloop` depguard component**
(`files: ["**/internal/runloop/**"]`), which is where this project keeps its import-boundary gate —
`.golangci.yml` and `docs/foundation/project-level/quality-checks.md` own that setting. The AST
test adds: no package vars, no goroutines, no defer. It misses `func init()` entirely, and it is not
transitive — `internal/core` is allow-listed, so a clock read added there stays green.

The `defer` ban is cargo-cult (a `defer` is not an effect) and it has already cost something: to avoid
importing `github.com/google/uuid`, `continuity.go` hand-rolls
`func uuidVersion[T ~[16]byte](id T) byte { return id[6] >> 4 }`, duplicating logic that
`core.TransitionID.IsUUIDv7()` already has — and the same file calls `m.TransitionID.IsUUIDv7()` one line
above the hand-rolled generic. A rule producing a worse implementation to satisfy its letter is the
"agents treat a rule as a law" failure `AGENTS.md` warns about, appearing inside code we just adopted.

`runexec`'s answer is better and already deployed: layering by depguard, correctness by
`TestRun_TotalityNoPanic` / `TestDispatch_TotalityNoPanic` asserting the function is total over all
(phase, event) pairs. "Is this function total?" is a stronger question than "does this file import `os`?"

### Standing

Both packages have **zero importers** — confirmed by grep, already recorded as `UNWIRED-INVENTORY.md`
row 39, "knowingly parked". They are dead code that compiles. That is the correct state for them right
now; the point of harvesting them was to have the design available when the rewrite reaches the mode
boundary, not to wire them first.

---

## 4. Source 3 — the integration worktree and branch: abandon

### What it is

`/private/tmp/harmonik-main-integration-20260725`, branch `integration/phase-reviewloop-20260725` at
`30d4e0876`. **Its working tree is clean** — nothing staged, nothing modified. `NEXT_STEPS.md` §D lists it
as *"staged for the item-C hand-harvest"*; that is stale. The harvest happened by cherry-pick on the main
line instead, and the worktree was never used for it.

```
git rev-list --count bcc53af1a..30d4e0876  →  44
git rev-list --count 30d4e0876..bcc53af1a  →  105
```

It **is** the branch item C names, and it **contains** `origin/reviewloop-decoupling` — merged at
`33542a3b6`, same commit SHAs on both. So the two are one abandoned effort, not two: the decoupling branch
is the work, the integration branch is that work plus a merge of `phase1-session-restart-substrate` plus
a large amount of noise. Either spelling reaches the same seven payload commits.

### The 44 commits, sorted

**25 of 44 are probe garbage** — matching `NEXT_STEPS.md` §C's original count, which this pass initially
and wrongly revised down. Eleven markers (`PROBE.md`, `PROBE4.md`, `CONC1.md`, `CONC3.md`,
`docs/conc-a.md` through `conc-f.md`, `docs/regate-seq.md`) plus **seven repetitions** of the identical
pair `harmonik: persist claude_session_id to Run.context (CHB-023)` + `chore: strip run-context from
merge (hk-4je)` — a smoke-test replay loop that committed its own output each time.

**7 are the real payload** — the `reviewloop-decoupling` set, dispositioned in §3 and §5.

**2 are already on our line** (`9db85569f`, `f8d3a42ef` — the remote-cwd fixes, merged as PRs #32/#33).
Of the remaining 10, seven are merges and `docs(plan)`/`docs(queue)` process files; three are code
(`fix(p2): re-pin readywait gate`, `fix(queue): preserve default harness through dispatch`,
`test(runloop): format reviewer harness fixture`). `git cherry` marks **one** of the three —
`74c663fb2` — as patch-equivalent to work already here. Of the other two: `30d4e0876` (readywait gate) is
155/27 across **two shell scripts and zero Go**; `8478de9fa` (default harness) is the only one carrying a
real production payload — eight production files, `workloop.go` 88 lines among them — alongside a new
378-line bead-named test file. Neither changes the verdict: the same harness fix landed
independently on this line as `3d9e99fab` (same subject, verified ancestor of the tip), and the readywait
scripts are gate tooling for a freeze this program has superseded.

### The regression, confirmed

`NEXT_STEPS.md` item C predicted merging would *"demote `specs/execution-model.md` from `reviewed` to
`draft`"*. Verified in `88f36c15d`'s diff:

```
-status: reviewed          +status: draft
-version: 0.9.3            +version: 0.10.0
```

Our tip is `reviewed` v0.9.5 — i.e. our line has moved *forward* since their fork point while their
version number moved *up* and their status moved *down*. A merge resolves in favor of the higher version
number and lands the demotion. That matters even though `CHARTER.md` §2 is explicit that *"`specs/` is not
a trustworthy oracle yet"*: the `reviewed`/`draft` marker is how a reader tells the trustworthy specs from
the rest, so demoting one silently removes the signal §2 depends on. `NEXT_STEPS.md` Trap 2 flags the same
hazard from the other direction (`kerf finalize` on
`.kerf/works/reviewloop-decoupling/` overwriting eleven specs with pre-correction text). Same branch,
two different mechanisms, same outcome.

**Abandon.** Delete the remote branch and the `/private/tmp` worktree. Everything worth having is either
already on this line or dispositioned below.

### Two corrections to item C

**The HEAD-SHA no-progress detector is ours, not theirs.** Item C lists among the harvest-worthy clauses
*"a no-progress detector comparing HEAD SHAs instead of diff hashes."* **That has been on this line since
2026-06-10.** `hk-m1wqp` added `review_fixup_stalled` to `specs/event-model.md` §8.1a.7 — emitted when the
implementer advances HEAD by zero commits after a `REQUEST_CHANGES` verdict, in both review-loop and DOT
modes — six weeks before the abandoned branch existed. Item C is describing our own prior work as though
it were theirs.

What is genuinely absent is narrower: the context key `last_iteration_head_sha`, naming the HEAD baseline
as durable run context rather than leaving it implicit, with `last_diff_hash` explicitly demoted to
diagnostic evidence. That is a real gap and a small one.

**`workloop.go` is not byte-identical.** Item C's other supporting claim — *"`workloop.go` is
byte-identical to ours"* — is refuted: 60 insertions / 435 deletions between the two lines, i.e. our
version has diverged structurally and substantially since the fork. The claim was presumably true when
first measured and was not re-derived. It does not change the verdict — the divergence is *our* work, and
a merge would revert it — but it is the third figure in item C that no longer holds, which is itself the
argument for treating that entry as superseded by this document rather than read alongside it.

---

## 5. The three unharvested commits — take none

All three were checked for applicability with `git format-patch | git apply --check`: **all three apply
cleanly, zero conflicts, zero fuzz.** Every file they touch is byte-identical at our tip to the commit's
parent. Applicability is therefore not the deciding factor for any of them; payload is.

### `89cc3aa02` feat(runloop): add owned event subscriptions — **skip**

Turns `PerRunEventTap.subs` from a slice into a map and adds `SubscribeOwned()` returning a handle with
`Unsubscribe(ctx)` that is synchronous and idempotent — `fanOut` holds the mutex across sends so no send
can occur after `Unsubscribe` returns. Real, correct concurrency work with a good 215-line test
(including a `-race` unsubscribe/emit loop).

**The owned half has zero callers.** All five `NewPerRunEventTap` sites — in `workloop.go`,
`reviewloop.go` ×2, `dot_cascade_core.go`, `dot_gate.go` — use the deprecated `Subscribe()` wrapper, which
the commit reimplements as `SubscribeOwned().Events()` and leaves behaviorally identical: the handle is
created and immediately discarded, so `Unsubscribe` — the entire point of the commit — is never reached.
Net production delta is ≈ zero, with one change for the worse: `fanOut` now holds the mutex during
fan-out, on the hot producer path, bought for a capability nobody calls. And all five call sites are
inside the rewrite's blast radius.

**Worth keeping as a design note, not as code:** *unsubscribe is a barrier, not a hint* — the contract the
Phase 3 event-subscription design should satisfy, whatever shape it takes.

### `19975ef7a` feat(hook): add quiescent session registrations — **skip; record two defects**

The most tempting of the three, because `internal/hook` is **not** on the rewrite list, so unlike the
other two its payload would survive Phase 3. It adds a `SessionRegistration` ownership handle with four
mechanisms: generation binding (reject envelopes whose `HandlerSessionID` does not match the open window),
exactly-once ready delivery, a quiescent seal that drains in-flight callbacks, and idempotent close.

It still fails, on the same measurement plus an added risk. `RegisterSession` has **zero callers** — every
daemon site uses the legacy two-key wrappers. Legacy windows bind `handlerSessionID == ""`, so
`handlerGenerationMatches` returns `true` unconditionally for every one of them: **the headline mechanism
is inert in production.** What *does* reach production through the compat wrappers is the risky half —
`CloseHookSession` now routes through the seal and **blocks** until callbacks drain, with
`context.Background()` and therefore no timeout, across the teardown paths in `reviewloop.go`,
`dot_gate.go`, `dot_cascade_core.go` and `workloop.go`.

794 lines to get one genuine fix bundled with a new unbounded blocking dependency on run teardown, in
files slated for replacement. Per the standing rule — **record, do not chase** — two defects fall out:

- **Duplicate `agent_ready` invocation.** `notifyAgentReady` has no once-guard, and
  `buildSessionStartMessage` synthesizes `agent_ready` for **both** startup and resume `SessionStart`
  events — so a review-loop run, which resumes the implementer up to three times, emits `agent_ready`
  repeatedly for one logical readiness. (Not a relay-retry effect: `hookrelay` retries only pre-write dial
  failures and `daemon_not_ready` ACKs, so a delivered envelope is never re-sent.)
- **Stale-generation envelope accepted.** `updateOutcome` / `notifyAgentReady` key only on
  `(runID, claudeSessionID)` and ignore the `HandlerSessionID` that `RelayEnvelope` already carries, so a
  late envelope from a prior handler launch can write into a re-registered window.

### `f03ff7c1b` test(reviewloop): freeze decoupling behavior — **skip, clearest of the three**

Two artifacts, both fail for different reasons.

`reviewloop_relocation_r0_test.go` (223 lines) is a `go/types` census that walks the daemon package,
resolves every identifier in `reviewloop.go`, and asserts the resulting map of daemon-private symbols
never grows past a hardcoded 26-symbol / 48-site baseline. **It pins which private identifiers a file
mentions** — a signature fact, and the canonical example of what the 225,000-line deletion was aimed at.
It is also an extraction-in-place ratchet, a framing `DECOMPOSITION-MAP.md` §6 retires; it would go red on
the first Phase 3 commit by design.

(Its *baseline* is the same coupling surface §2 measures, and it is the **more trustworthy** of the two: a
`go/types` resolver distinguishes declared-here from declared-elsewhere, which is precisely what defeated
three successive greps in §2. Keep the number; delete the ratchet built on it.)

`reviewloop_r0_characterization_test.go` + its JSON oracle (286 lines) is a 6-row decision table that
would compile against our tip. But `DECOMPOSITION-MAP.md` §6 names the method dead — *"The rewrite's
oracle is the `-tags=scenario` tier, not a characterization harness"* — and the rows are already covered
at tip. `scenario_reviewloop_em015de_hkintln_test.go` asserts cap-hit ordering and `blocked` without
`iteration_cap_hit`, in the tier the plan designates as the oracle; `fixup_stalled` is covered instead by
`reviewloop_test.go`, `reviewloop_cycle_complete_hk7om2q24_test.go`, and `reviewcycle_test.go` — a real
gap in the scenario tier, but not one this commit fills. The one novel fact is the
`local_archive_iterations` column, which is a spec line, not 509 lines of test.

---

## 6. `reviewloop.go` is a reachability guarantee, not one mode of three

> **Follow-up, 2026-07-28 (late):** this section's observability finding was confirmed from the other
> direction and has a measurement consequence — see `DECOMPOSITION-MAP.md` §0-CORRECTION. Short form:
> because `workflow_mode` cannot distinguish a demoted run, measure graph-engine usage with
> `node_dispatch_*` events, which only the cascade emits. Doing so shows the engine is the default and
> has really run (1,697 `implement` dispatches across three graph files). The floor described below is
> therefore the single line keeping `reviewloop.go` alive, and dropping it is a **named spec amendment**
> to the three requirements quoted below — not a silent deletion.


Any decomposition touching `reviewloop.go` inherits a constraint that is easy to lose because **the place
it is written down and the place it executes are different files**.

### Where it actually is

`internal/daemon/daemon.go` only *documents* it — a comment on the `Config.WorkflowModeDefault` field:
*"When the embedded DOT graph fails to load, workloop demotes to review-loop as a safety floor
(EM-012a-FLOOR) — NEVER to single."* There is no code there.

The demotion executes in `internal/daemon/workloop.go`, inside `beadRunOne`, in the pre-switch block
immediately before the mode-dispatch `switch` (search for `Safety floor (hk-30vlb §REVIEW FLOOR item b)`).
When the mode resolved to `dot`, no per-item workflow ref was given, and no `<projectDir>/workflow.dot`
exists, it loads the embedded `standard-bead.dot`; on failure it reassigns the **local** `workflowMode` to
`core.WorkflowModeReviewLoop` and falls through to the review-loop case.

So a rewrite reading `daemon.go` finds an invariant with no code, and one reading `workloop.go` finds a
local-variable reassignment whose reason lives thousands of lines away. That split is the hazard.

**And the demotion is already invisible to the event log.** `emitRunStarted` fires *before* the pre-switch
block and is passed `string(workflowMode)` — the pre-demotion value. A run that demotes therefore records
`workflow_mode: "dot"` on `run_started` while executing the review loop. That is not merely an
observability defect: EM-012a requires that *"the resolved value MUST be surfaced on the `run_started`
event payload's `workflow_mode` field … for downstream consumers"*, so it is a spec violation. Recorded,
not chased — and it is the sharpest available evidence for the argument below: **the floor is real in
exactly one place and nowhere else — not in the events, not in the tests, not in `daemon.go`.**

### It is fully normative — this is not folklore

`EM-012a-FLOOR` is a real requirement, defined in `specs/execution-model.md` §4.3 and restated in
`specs/process-lifecycle.md` PL-004a and `specs/beads-integration.md` BI-009a:

> *"if the embedded `standard-bead.dot` artifact fails to load … the daemon MUST fall back to
> `review-loop`, NEVER to `single`. A bead resolved via tier-3 or tier-4 MUST NEVER be dispatched without
> a review gate … The fallback ordering … is therefore `dot → review-loop` (and the daemon MUST emit a
> diagnostic recording the fallback) … This floor is the structural guarantee that the system never
> silently bypasses review for default-resolved work."*

Note the parenthetical MUST: the diagnostic exists, but only as an `fmt.Fprintf` to stderr — not an event.
That is the same finding as the `run_started` mismatch above, from the spec side.

**The complement is NOT enforced, contrary to what the code says about itself.** `moderesolve.go`'s tier-4
comment claims: *"single is only reachable via an explicit `workflow:single` per-bead label or
`--workflow-mode single` flag — NEVER via tier-3 or tier-4 resolution."* The final clause is the false
one, and it is false because of the flag the same sentence names. Measured, there are three routes and
only the first is audited:

1. **The per-bead label** — tier 1, emits `review_bypassed`. This is the audited path.
2. **The daemon default** — tier 3 is `if daemonDefault.Valid() { return daemonDefault }`, and
   `core.WorkflowModeSingle` *is* `Valid()`. `bootconfig.ValidateWorkflowMode` rejects only empty and
   unrecognised values. So `--workflow-mode single` (a real flag in `cmd/harmonik/main.go`, assigned
   straight to `Config.WorkflowModeDefault`) silently resolves `single` for **every unlabeled bead**, with
   no `review_bypassed`. Note the asymmetry: the *config-file* path is blocked — `internal/projectconfig`
   returns `ErrWorkflowModeFloorViolation` — but the CLI flag bypasses that check entirely. Per PL-004a
   that flag path is itself a spec violation.
3. **The per-item tier-0 override** — `beadRunOne` overwrites `workflowMode` from `env.ItemWorkflowMode`
   *after* `resolveWorkflowMode` returns, bypassing the resolver and its audit event altogether;
   `cmd/harmonik/run.go` sets it to `single` on two paths, including `--no-review-loop`.

This matters more than a stray fact. §6 exists to argue the floor is fragile; the sentence this replaces
falsely reassured the reader that half of it was already safe. **The floor guards the default-resolution
path and nothing else** — and it was written into this document from `moderesolve.go`'s own comment rather
than from a measurement, which is the `CHARTER.md` §5 pattern ("a claim that sounds like a blocker gets
repeated as one") reproducing itself inside the document that cites it. Record the gap; do not chase it.

**The property to preserve is not "`runReviewLoop` must exist."** It is: *no bead whose mode was resolved
by default — rather than by an explicit operator label — is ever dispatched without a review gate, and a
failure to load the default graph degrades toward more review, never less.* State it that way and a
rewrite is free to satisfy it with any structure. State it as "keep `reviewloop.go`" and the rewrite
either preserves a 2,194-line file it was chartered to delete, or drops the guarantee while believing it
only deleted a file.

### Where a decomposition would lose it

`DECOMPOSITION-MAP.md` step 5 pulls workflow-mode resolution into a **pure `RunPlan` resolver** (seam C1),
run before any resource is acquired. But the demotion happens *after* mode resolution, deep inside
`beadRunOne`, by mutating a local — and it depends on an **I/O result** (does the embedded graph parse?).

A resolver built to the letter of step 5 returns `dot` and never learns the graph failed to load. Nothing
downstream demotes, because today nothing downstream *can* — the demotion is the pre-switch block itself.
**The floor disappears silently and the compiler is happy.** This is `CHARTER.md` §5's named pattern
("dead and unfinished look identical") in a new costume: an invariant implemented as a local-variable
assignment is invisible to every mechanical check.

The honest resolution is that graph *loadability* is an input to the plan, not a consequence of it — the
resolver takes "did the default artifact load" as a parameter and returns `review-loop` when it did not,
so the floor becomes a **table row in a pure function** instead of a mutation buried in a 2,289-line
driver. That is a design note for step 5, not a task.

### The test covers half, and cites a stale line number

`internal/daemon/standardgraph_sync_test.go` `TestStandardBeadDotLoadFailureReturnsError` corrupts the
embedded bytes and asserts `loadStandardGraph` returns an error. Its own comment is candid that this is
the *precondition*, not the behavior: it pins that the error exists so that *"the pre-switch block in
workloop.go … can demote"*. **Nothing tests that the demotion happens.** Corrupt the graph and delete the
demotion and the suite stays green.

That comment also points at *"lines ~1985-1992"*. The block is nowhere near there — it sits in
`beadRunOne`, immediately before the mode-dispatch `switch`, roughly 1,700 lines further down. A stale
line-number citation inside the one test guarding a safety floor is precisely the rot `AGENTS.md` §Key
conventions warns about ("cite symbols, not line numbers"), sitting in the highest-consequence place it
could.

Recorded, not fixed — no bugs this pass. But if any single test is worth writing before the rewrite
begins, it is the one that dispatches a bead with a corrupt embedded graph and asserts the review-loop
path ran. Today the floor's only real proof is that someone read `workloop.go`.

---

## 7. What this leaves

**No code to cherry-pick.** Item C's harvest already landed; the three remaining commits are all
zero-caller or signature-pinning; the two branches and two worktrees are disposals. The one thing still
worth taking off the abandoned branch is **prose** — two `execution-model.md` clauses, by hand.

**Three things to carry into the decomposition**, none of them new code:

1. **`execution-model.md` is stale on the review loop, and the kernel is right.** `reviewcycle` never
   emits `no_progress` and treats a flagless `REQUEST_CHANGES` as approval; so does the shipped daemon
   (`hk-togxq`, `hk-thbbv`), while the `reviewed` spec says otherwise. Amend the spec — the abandoned
   branch's `88f36c15d` already contains the correct text — rather than "fixing" the kernel toward it.
2. **The review floor is an invariant, not a file** — and the pure-resolver seam is exactly where it would
   be lost. §6 states the property in the form a rewrite can satisfy.
3. **The reviewer-budget clamp formula**, the other clause worth hand-copying: the constants are in
   `pasteinject.go`, the rule that composes them is written down nowhere on this line.

**Six defects recorded, not chased.** From §3: `emitNoProgressDetected` is dead code behind a live event
registration, and `TestScenario_EM015e_NoProgress_ReviewerNotLaunched` — in the tier named as the
rewrite's only oracle — asserts that dead event. From §5: duplicate `agent_ready` (no once-guard, and
`SessionStart` synthesizes it on resume as well as startup), and stale-generation envelope acceptance in
`internal/hook`. From §6: `run_started` carries the pre-demotion `workflow_mode`, violating EM-012a; and
`single` is reachable via `--workflow-mode single` and via the per-item tier-0 override without emitting
`review_bypassed`, the flag path bypassing the `ErrWorkflowModeFloorViolation` check that guards the
config-file path.

**Four things disposable:** the `lift-l8-reviewloop` worktree and branch, the
`/private/tmp/harmonik-main-integration-20260725` worktree, and the remote branches
`integration/phase-reviewloop-20260725` and `reviewloop-decoupling`. `NEXT_STEPS.md` §D's kept-worktree
table has two fewer rows after this.

**And `NEXT_STEPS.md` item C is superseded by this document, not read alongside it.** Three of its
figures no longer hold: the "HEAD-SHA no-progress detector" it lists as harvest material has been on this
line since 2026-06-10 (`review_fixup_stalled`, `hk-m1wqp`); `workloop.go` is not byte-identical (§4); and
the cherry-pick it recommends already happened. Its verdict — abandon the branch — is correct and stands.
