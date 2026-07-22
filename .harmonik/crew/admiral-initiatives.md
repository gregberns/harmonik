# Admiral — Major-Initiatives Registry

> # ⚠ RECONSTRUCTED 2026-07-22 07:2xZ AFTER DATA LOSS — READ THIS FIRST
>
> A **`git reset --hard`** was run in the main repo (`reflog HEAD@{0}: reset: moving to HEAD`).
> It destroyed every uncommitted modification in the working tree, including this file's entire
> 2026-07-22 content. Unstaged content was never in the object store; `git fsck` recovered
> nothing. **This file is rebuilt from the admiral's own session context, the comms log, and
> beads.** It is faithful where present and INCOMPLETE where noted — see §WHAT WAS LOST.
>
> **The pre-reset file also contained the whole FREEZE-AND-CARVE program (2026-07-12/15).
> That program is SUPERSEDED and was already archived** at
> `.harmonik/archive/2026-07-12-freeze-and-carve/admiral-initiatives.md`. If you are reading a
> version of this file that says *"execution FROZEN — nothing dispatches until the operator
> ratifies PLAN.md"*, you are reading the reverted July-15 copy and it is WRONG. The fleet is
> dispatching. That is rule 4 — a superseded conclusion left standing is a live trap.
>
> ### ✅ SOURCE FOUND — **IT IS THE DAEMON, NOT AN AGENT** (captain; admiral verified at source). `hk-7qmpp` P1.
>
> ⚠ **MECHANISM CORRECTED 07:4xZ by mike's trace — my "unconditional, no dirty-check" was WRONG
> in an important way.** `checkMainWorkingTreeDirty` (`:7746`) **DOES** fail dirty runs — but its
> **churn allowlist (`:7725-7728`) EXEMPTS `.harmonik/` and `.claude/`**, exactly where
> `captain-lanes.md`, `direction-log.md` and `admiral-initiatives.md` live. **The escape hatch
> waves through precisely the region the refresh then destroys.** That is why ONLY fleet state
> died and no crew lost code. **Two features interacting, not a blanket reset.** The allowlist was
> written so agent-state churn would not block merges — a convenience — making this the **SIXTH
> and most precisely targeted instance** of a mitigation causing the harm it was written to
> prevent. Consider narrowing the ALLOWLIST, not only the refresh.
>
> `internal/daemon/workloop.go:6954-6972`, `commitFinalizeWorkingTree` (EM-054 working-tree
> refresh). After **every successful merge-to-main push**, `.Dir` = the **MAIN PROJECT ROOT**: `git restore --staged .` then `git reset --hard HEAD`.
> Best-effort, non-fatal, **silent about what it destroys**. The two resets 3m47s apart were two
> merges completing. **Deterministic; fires again on the next merge.**
>
> **THE FLEET WAS CLEAN.** No agent ran it. Under admiral pressure to find a culprit, mike and
> india both volunteered commands against themselves; neither was involved. Admiral error, on record.
>
> **FIX SHAPE — MY FIRST ONE WAS WRONG, WITHDRAWN.** ~~"dirty-check → refuse-and-warn"~~ makes it
> WORSE: `workloop.go:6874` has ALREADY advanced the ref via `git update-ref`, which touches
> neither index nor worktree — the restore+reset is the **SECOND HALF of a hand-rolled
> fast-forward**. Skipping leaves main's tree DESYNCED against a moved ref: quieter and more
> confusing than the loss. *(I guarded the second half without reading the first.)*
> **FIX SETTLED (mike traced it, then REFUTED HIS OWN SHAPE before writing code):**
> `update-ref` is **RSM-016 reversibility for the Phase-D CAS-rollback on push failure** — NOT
> index locks. So `merge --ff-only` is **REFUTED**: it moves ref+tree together and breaks the
> rollback. **KEEP `update-ref`.** Instead **SCOPE the refresh to merged paths only** —
> `git checkout HEAD -- $(git diff --name-only mainTip runTip)`, reusing the diff already computed
> at `:6979`.
>
> **TWO ACCEPTANCE CRITERIA, BOTH REQUIRED:** (1) the refresh cannot destroy uncommitted work in
> the main root; (2) when it declines — or when a merged path intersects a dirty one — it emits a
> **DISTINCT event NAMING the blocking paths.** A scoped refresh still overwrites uncommitted
> edits to a merged path, so without (2) silent loss just becomes a mysterious stalled merge.
> **A fix satisfying (1) and not (2) is NOT DONE.**
>
> **ADMIRAL RULING — INLINE SPEC AMENDMENT, NOT A KERF WORK.** EM-054 (`execution-model.md:965+`)
> **NORMATIVELY specifies** `git reset --hard HEAD` and *"uncommitted changes: log warning, still
> refresh"* — **the destruction is SPEC'D**, so this cannot land as pure code. Kerf is for an OPEN
> design space; mike's trace already closed it. **Guard: it is reviewed AS a normative-spec change,
> by a reviewer told that is what they are reviewing** — not a spec edit riding invisibly in a code
> diff. **AND THE SPEC MUST RECORD WHY `update-ref` STAYS** (RSM-016 reversibility), or the next
> reader sees a hand-rolled fast-forward, thinks *"why not merge --ff-only"*, and reintroduces the
> break. Rule 4 applied FORWARD: do not leave a refuted-but-obvious alternative unmarked.
>
> **CONSEQUENCE: uncommitted work in the main root is SCHEDULED FOR DELETION**, by design, on
> every merge. Untracked files survive a `reset --hard` (why `HANDOFF-captain.md` lived) — **luck,
> not design.**

## ★★★ DEPLOY STATE — **`hk-7qmpp` IS DEPLOYED; `hk-pkxju` IS NOT** (09:0xZ; india caught the admiral's miss)

> **DEPLOYED:** `daemon_started 09:03:44Z binary_commit_hash = eb2b4f1a`. Tagged
> `daemon-20260722-01`. **The tree-wide destructive reset is DEAD IN PRODUCTION** — first fully
> gated release of the night (assessor delta PASS, GATE 0 green on targeted tests at the deploy
> SHA, Q2 answered pre-swap). Independently verified by mike: `refreshMergedPaths` present, no
> tree-wide reset in the finalize path.
>
> **⚠ NOT DEPLOYED: `hk-pkxju` (`9489510e`).** `git merge-base --is-ancestor 9489510e eb2b4f1a` →
> **NO.** The swap stopped ONE COMMIT short. **So the opt-out fallback covering 49 unpinned
> reviewer nodes across 24 graphs is NOT running.** In production today a `harness:codex` bead on
> an unpinned graph still routes its reviewer to codex → no verdict. **The only LIVE protection is
> the amended freeze rule — safe-by-DISCIPLINE**, the weaker form.
>
> ### ⛔ HARD RAMP PRECONDITION
> **BEFORE ANY WAVE-1 BEAD IS LABELLED: the daemon must run a binary containing `90c9178b`.**
> Verify from `daemon_started binary_commit_hash` — **NOT** from the branch tip. Sits alongside
> concurrency >1. **No second swap authorized tonight** — nothing is labelled, so nothing can hit
> the hole.
>
> **ADMIRAL ERROR:** I taught "landed ≠ deployed" for three sweeps, then verified india's landing
> on the BRANCH, affirmed it, and never asked whether it was RUNNING. **A fix has three states —
> COMMITTED, LANDED, DEPLOYED — and only the third protects anything. Relief is when the check
> gets dropped.**

## ★★★ THE FLAKY-PACKAGE INITIATIVE (`hk-f8o5u`) — HEADLINE RECONCILED, MEASUREMENT CORRECTED

> ~~"internal/daemon is ~75% flaky / 3-in-4 spurious failures / 25% green"~~ — **STRUCK.** That
> number conflated three arms and carried names from two DISCARDED runs. **I ranked an initiative
> above the Priority-0 ramp on it and had to suspend it.**
>
> **RECONCILED HEADLINE:** **the full package greens ~1 run in 7, and the failing set ROTATES
> between runs of the same binary on the same tree.** Strict count: **8 distinct across 3 valid
> pristine runs, 1 fully green.** Only two recur — `ClaimSemaphore` 2-of-3, `LockReleasedOnError`
> 2-of-3. Pooled rate: pristine 1/3 · fixed 0/3 · assessor full non-short 0/1 · india subset 0/3.
> **Still outranks the ramp** — a package that greens 1-in-7 cannot gate anything.
>
> ### ★ THE MEASUREMENT RULE (juliet) — worth more than the count
> **Count how many RUNS go GREEN. Do not count which tests fail. The rate is stable; the membership
> is not.** A named-list re-run **cannot discriminate**: ~4 tests fail per run and mostly different
> ones, so a passing list is the expected outcome under BOTH hypotheses — you would conclude "all
> clear" and the next run hands you four unseen names. **This is why a known-red list cannot work
> here**, and why juliet retracted her own published 7-name baseline.
>
> ### ★ WHY THE SET ROTATES (india) — the first actual EXPLANATION
> **`ClaimSemaphore` fails 2-of-3 in the full package and 0-of-3 in an isolated subset.** Not a
> contradiction — **the test fails because of what else runs CONCURRENTLY, not because of itself.**
> **Consequence: a `-run` filter is NOT a like-for-like control** (it changes the variable under
> test). Two arms are comparable as RATES, never as membership.
> **Separate population found:** 3 tests fail **3-of-3, fast, deterministic, on a quiet box** —
> `Hk6ynv4_SubscribeStream_EndToEnd` (0.13s), `ShutdownDrainsCommittedRun_hkdnrg` (0.73s),
> `Throughput_TenBeadsAtMaxFour`. **Candidate genuine defects, distinct from the flake set.**
> Re-run on a clean cache before reclassifying.
>
> **THREE SELF-CORRECTIONS PRODUCED THIS:** juliet retracted her own 14 ("I folded in the captain's
> 2 known-red without saying so — exactly the sin I am flagging"); the assessor collapsed its own 8
> failures to 1 genuine, proving 2 self-inflicted with a two-arm control; india adopted juliet's
> reporting frame BEFORE publishing rather than after being corrected.

## ★★★ `hk-agl8b` — CACHE-WIPE ROOT CAUSE FOUND, AND IT IS WIDER THAN THE TEST SUITE (juliet + lima; admiral verified at source)

> **ROOT CAUSE (juliet):** `diskcheck_hksxlb.go:74` — `exec.CommandContext(..., "go", "clean", "-cache")`
> with **NO `cmd.Env`**, so the child inherits the caller's `GOCACHE`. **The daemon test suite wipes
> the cache of whoever runs it.** india was not wrong about anything and **no reachable investigation
> could have found it** — the culprit was the suite he was running.
>
> **VINDICATES THREE REFUSALS TO GUESS:** india's, mike's self-elimination when he was the obvious
> suspect, and the admiral's. **All three would have been wrong, and the wrong answer was available
> and would have been believed** — the first suspect anyone reached for was mike's reclaim work.
>
> **WIDER (lima, admiral-verified): `diskcheck_hksxlb.go:327` runs the SAME unenvironmented clean on
> the PROACTIVE 60-MINUTE CADENCE, gated only on `mergeOrRunInFlight` — NOT on disk pressure.**
> So **EVERY scratch/isolated daemon inherits its launching shell's `GOCACHE` and cleans it on the
> hourly tick**, with nothing in its log connecting it to another crew's failed build.
> **Scratch daemons are our standard measurement pattern** (india's gates, lima's `/tmp/hk155gs`,
> the assessor's clones). lima's each lived under an hour — **"duration, not design."**
>
> **PRACTICE:** launch scratch daemons with `GOCACHE` unset or pointed at a throwaway path
> (`env -u GOCACHE harmonik …`); assume any scratch daemon >60 min old has ticked; prefer
> short-lived scratch daemons and tear down after the measurement.
>
> **MEASUREMENT CONSEQUENCE:** any historical daemon-suite run under a private `GOCACHE` may have
> wiped its own cache mid-run and produced `could not import` failures **that look like test
> failures.** That is a contamination source in the `hk-f8o5u` dataset **predating tonight**, and it
> is in nobody's validity checks except juliet's (added after india's incident).
>
> **ELEVENTH INSTANCE of a mitigation causing the harm it prevents** — one missing `cmd.Env`, on two
> separate paths, one reactive and one proactive.

## ★★ CACHE CONTAMINATION — `go clean -cache` REACHES OUTSIDE ITS LANE (09:1xZ)

> india's isolated `/tmp/gocache-india` went **483M → 8.0K mid-experiment**; residue is README +
> trim.txt only — **the exact `go clean -cache` signature** (admiral verified). Pass 2 carried 40
> cache-miss errors naming his own private path. **Culprit NOT named — india ruled out the daemons
> by reading, then refused to guess, and so did I.**
> **MECHANISM: `go clean -cache` respects `GOCACHE` FROM THE ENVIRONMENT** — it cleans whichever
> cache is pointed at, so a reclaim or an inherited export **deletes another agent's live working
> set mid-run.** Rules broadcast: confirm `echo $GOCACHE` before every clean; the reclaim beads
> (`hk-137y6`/`hk-cy4ej`/`hk-pgtbr`) treat every `/tmp/gocache-*` as ACTIVE until proven otherwise;
> `could not import` naming your OWN private path means your cache was emptied under you.
> **A contaminated measurement that looks like a result is worse than no measurement.**

## ★★★ `hk-scaj0` REFRAMED — **THE SANDBOX IS BUILT AND WIRED; ONE CONFIG LIST LIMITS IT** (lima, 09:1xZ)

> ⚠ **CORRECTED 09:1xZ BY lima AGAINST HIS OWN REPORT — THE SECOND HALF WAS WRONG, AND THE TRUTH IS
> WORSE.** ~~"the whole limiter is `sandbox.harnesses = [pi]`"~~ — **STRUCK.** Filed `hk-dqo9u` (P1),
> blocking `hk-scaj0`.
>
> **IN DOT MODE THE SANDBOX GATE IS NEVER CONSULTED FOR CLAUDE, AND NO CONFIG CAN TURN IT ON.**
> The entire sandbox block in `dot_cascade.go` sits inside `if h.SessionIDPolicy() ==
> SessionIDCaptured` (`:1514`); all four `sandboxSpawn` references live in that branch. **Claude is
> SessionIDMinted** (`claudeharness.go:136`); codex and pi are Captured. **There is NO substrate
> attach on the DOT path.** `workloop.go` (single mode) HAS the attach at `:4510` — the DOT cascade
> never ported that half. (Single-mode claude coverage NOT yet verified; lima will not claim it.)
>
> **MEASURED, NOT INFERRED:** isolated scratch project, binary built from `fa7ed2f0` and
> **string-scanned to confirm it contains hk-guapd** (per the sequencing condition), config
> `harnesses: [pi, claude-code, codex]`. A claude-code bead launched **BARE** — no srt wrap, no
> profile JSON, no engagement-verification line — **while the config said it was sandboxed.**
>
> **IT FAILS SILENTLY AND OPEN. The config READS like protection.** Same shape as `harmonik status`
> emitting text that looks like a health report — except this one is load-bearing for SECURITY.
>
> **TWO COMPOUNDING GAPS:** (1) **`sandbox.harnesses` is NEVER VALIDATED** against real agent types
> (`projectconfig.go:2049` takes it verbatim; only `backend` is checked) — the real name is
> `claude-code`, so writing the natural `claude` **silently sandboxes nothing. Fail-open on a typo.**
> (2) **`harmonik init` scaffolds NO `sandbox:` block at all** — every fresh project is unsandboxed
> by default and the secure path means hand-writing an undocumented block. Same shape as `hk-yhvrh`.
>
> **SO: coverage is per-LAUNCH-PATH, not per-harness, and the harness we run MOST is uncovered on the
> DEFAULT workflow mode.** Still a bounded defect in an existing mechanism, not proof the mechanism
> is wrong — but the wrapper needs real work before it covers what we actually run, and **that work
> is now measurable rather than assumed.** lima explicitly refuses to steer §A2 on it; it cuts both
> ways.
> **WHY IT READ AS GREENFIELD — two stale premises in its own source doc:** ~~"the pi-sandbox srt
> experiment was dropped"~~ → it **LANDED** (7/7 beads, code live); ~~"Pi is spec'd UNSANDBOXED per
> PI-015"~~ → **a misreading that INVERTS it** — PI-015 forbids the HARNESS passing a native
> `--sandbox` flag, exactly what D3 mandates; the harmonik wrapper is orthogonal and is ON for pi.
> Filed `hk-s13ee`. **Rule 4 again — an epic scoped off a doc carrying two refuted premises.**
> **ACTUALLY MISSING:** claude/codex coverage · **the per-bead disable does not exist in ANY form**
> (grep-verified; only an all-or-nothing project-wide list — `hk-mp37h`) · container-vs-wrapper.
> **`hk-155gs` AUTHORIZED** — widen `sandbox.harnesses` in a fully isolated scratch project and
> record what breaks; converts §A2 from preference into EVIDENCE without pre-empting the operator.
> **CONDITION: build from a tree CONTAINING `hk-guapd`**, SHA verified, or you measure a sandbox
> with a hole we already fixed and it *looks* like coverage.
> **⚠ FOR THE OPERATOR, HELD DELIBERATELY:** §A2 says *do not assume container*; §B says he wants
> containerized beads anyway for security AND load distribution — **if B happens it may SUBSUME
> A2**, making a security-only container path duplicated work. Held because lima's measurement
> makes the question cheaper: **now gets an opinion; after `hk-155gs` gets a decision with costs
> attached.** Surface BOTH together.

## ★★★ RAMP-SAFETY PROVEN LIVE — AND ITS PRECONDITION IS ALREADY VIOLATED IN 21/23 GRAPHS (india, 07:4xZ; admiral RULED)

> **PROVEN (bead `it1-rgx`, one run, single variable isolated — no `--default-harness`, no
> `HARMONIK_SUBSTRATE`, both absences VERIFIED via daemon argv + `ps eww`):**
> ```
> IMPLEMENT: harness_selected {agent_type: "codex",       tier: 1}   <- the LABEL routed it
> REVIEW:    harness_selected {agent_type: "claude-code", tier: 3}   <- the NODE PIN won anyway
> ```
> A tier-1 label normally BEATS a tier-3 node default. It does not here, deliberately:
> `dot_cascade.go` swaps in `pinnedHarnessLaunchSpecBuilder` when a node carries an explicit
> `harness=` attr, so **the node pin wins UNCONDITIONALLY over a tier-1 bead label** (`hk-2jxqg`).
> Had it gone the other way, the label would have routed the REVIEWER to codex — which cannot
> review (`codexlaunchspec.go` emits only an implementer seed prompt, never reads `rc.phase`) — so
> every ramped bead would implement, commit, then die with **no verdict**: `hk-pkxju`, reproduced
> by the very mechanism we intend to ramp with.
>
> **TWO BONUS RESULTS:** (a) a ramp is **AUDITABLE from the event stream** — the `tier` field
> distinguishes a ramped bead from a globally-defaulted one, so a staged rollout can be PROVEN
> rather than inferred; (b) `hk-oulya` confirmed from the other direction — routing comes from the
> label (tier 1) or the global default (tier 4), **never** from `HARMONIK_SUBSTRATE`.
>
> ### ⚠ THE PRECONDITION IS ALREADY VIOLATED ACROSS MOST OF THE CORPUS
> The protection exists ONLY where the reviewer node **carries** a `harness=` attr. Unpinned
> reviewer nodes have no tier-3 value → the tier-1 label wins → **the reviewer goes to codex.**
> Per `hk-83hg1`: **21 of 23 reviewer-bearing graphs under `specs/examples/` have ZERO reviewer
> pins.** Only `workflow.dot` and `standard-bead.dot` pin theirs.
>
> **RULINGS:**
> 1. **"REVIEWER NODES MUST STAY PINNED" IS A LOAD-BEARING RAMP INVARIANT**, not style. Dropping
>    a pin from `workflow.dot`/`standard-bead.dot` breaks the ramp SILENTLY.
> 2. **The WAVE-1 GRAPH-SELECTION RULE is PROVEN, not cautious** — a wave-1 bead must carry
>    `WorkflowRef` = default graph or none. Non-negotiable; it now has a live failure mechanism.
> 3. **`hk-83hg1` is a RAMP SAFETY DEPENDENCY, not a P2 tidy-up** (currently P2, labelled
>    duplicate). Re-rank + route. Prefer the **opt-OUT** remediation (reviewer-class nodes default
>    to claude-code when no `harness=` present) over pinning 21 files — it also covers future graphs.
> 4. **RECONSIDER UNPARKING `hk-pkxju`** (india's `c7f3ea30` makes an INHERITED reviewer fall back
>    to claude — belt-and-braces for the unpinned case). Parked today, so **the pins are the ONLY
>    protection** on the Priority-0 path while 21/23 graphs are unpinned. Captain's judgement call.
>
> ### ✅ TIER-1 LABEL RAMP: **PASS, END TO END** (india, 07:4xZ; admiral ACCEPTED)
> Bead `it1-rgx` **CLOSED** on an isolated daemon. implement=codex **tier 1** (label alone) ·
> review=claude **tier 3** · qa=claude **tier 3** · both APPROVE · codex commit `d4ed977` merged.
> 223s total (codex 57s, review 118s, qa 45s). Containment held. **The ramp's MECHANISM gap is
> CLOSED — proven from routing through terminal close, not by proxy.**
>
> **HONEST LIMITS, carried verbatim so this is not over-read:** N=1 on the label path · toy module
> with a reduced commit_gate (deliberately isolating ONE variable; run 2 already carried the real
> gate on the real codebase) · concurrency 1 · says nothing about a claude-harness bead under a
> codexdriver substrate (the still-open half of `hk-oulya`).
>
> ### ⚠ THE CONCURRENCY HAZARD IS `~/.codex` — **SCOPE CORRECTED**, and the correction is india's own
> **`~/.codex` IS GLOBAL** — one SQLite state dir shared by every codex process on the box, not
> per-run, not per-worktree. At concurrency 1 nothing competes for it, **and concurrency 1 is the
> only configuration ever run.**
>
> ~~"N stale-WAL guards fire on the same file and our own guard could reap a WAL a live peer is
> using — the SEVENTH instance of a mitigation causing the harm it prevents"~~ — **STRUCK.
> OVERSTATED, retracted by india, verified by admiral at the source.** `codexwalguard.go` is
> conservative and well defended: it `lsof`-checks the WAL **and separately** the base
> `state_*.sqlite` (a live codex holds both) → skip on either; **if `lsof` is missing or errors it
> SKIPS the removal — fails SAFE, not open**; a **TOCTOU re-check** re-stats size+mtime around the
> backup; sidecars are **backed up before removal** so even a wrong call is recoverable; the base
> `.sqlite` is never touched. Guard-vs-guard is safe by construction too — `reapCodexWALBackupDirs`
> keeps the newest `walBackupKeepLast=5`, and a dir a guard just created carries the current
> unixnano, so it is always newest and can never be in the reaped set.
>
> **WHAT IS GENUINELY OPEN (a different question from the one first asked):** (1) **codex's OWN
> safety with N processes against one shared SQLite state** — nothing in our code governs it and
> there is no evidence either way; (2) the narrow lsof/TOCTOU window — real, low-probability,
> recoverable via backup; (3) contention on other global state (config.toml, auth/token).
> **Bead scoped to (1)**, not to a defect in our guard — *"filing an alarming bead against
> well-written code would waste whoever picks it up."*
>
> **HYPOTHESIS RE-AIMED, experiment unchanged:** instrument for **codex-vs-codex** contention on
> the shared SQLite; record guard behaviour (`skip_held` vs `removed_stale`) as **observation, not
> as an expected failure**. If the guard skips-held under concurrency, **that is evidence it
> WORKS** and gets reported as such — not hunted past in search of a predicted failure.
>
> ### ✅ CONCURRENCY EXPERIMENT AUTHORIZED — **CHEAP VARIANT ONLY** (admiral, 07:4xZ)
> 3 beads w/ `harness:codex` · one queue group · `--max-concurrent 3` · **reviewer nodes on a
> trivial always-APPROVE gate.** Answers Q1 (attribution / cross-contamination), Q2 (`~/.codex`
> contention), Q4 (slot behaviour) — **the shared-state questions that decide ramp safety** — at
> near-zero Claude tokens. **REJECTED the full variant (~1.5M fresh Claude tokens, ≈3× the
> strength test + tier-1 run combined):** the operator ranked codex-first on a RUNWAY constraint,
> so spending 1.5M to answer what a near-free experiment answers is the exact trade this
> initiative exists to avoid. **It also FAILS CHEAP** — if codex runs corrupt each other we learn
> it for nothing, before committing to the expensive run.
> **GENERAL PRINCIPLE: when an experiment has a cheap variant that answers the decisive question,
> run that first and buy the expensive one with the result.**
> ⚠ **THE CHEAP VARIANT CANNOT AUTHORIZE THE RAMP** — it gates whether the expensive one is worth
> running. **Q5 stays open and is load-bearing for the runway model:** does the review bottleneck
> scale linearly or CONTEND at N? Reviewers are 83% of per-bead cost and we do not know that holds
> at concurrency. Disk abort threshold 15 GiB (21 GiB free at authorization).
>
> **`c7f3ea30` LANDING:** rebased to **`9489510e`**, zero file overlap with the intervening
> `hk-qx065` commit (verified), re-verified AT the landing SHA, **with the assessor now** — india
> handed them his reviewer's two WEAKNESSES so they attack those rather than re-derive strengths.
> That verdict is the precondition for marking `hk-83hg1` subsumed.
>
> **RAMP STILL UNAUTHORIZED. `concurrency >1` IS NOW THE LAST NAMED GAP** — and the one the ramp
> actually depends on. Most expensive experiment left; disk is the constraint. Land `c7f3ea30` first.
>
> **`c7f3ea30` UNPARKED (admiral verified at source).** `dotReviewerInheritedHarnessOverride`
> returns early for implementers and for explicitly-pinned nodes; otherwise resolves via
> `resolveHarnessAgentTypeQuiet(bead, ...)` — **passing the BEAD**, so the tier-1 label is read and
> then corrected back to claude for unpinned reviewer nodes. Closes the 21-graph hole **without
> editing a single `.dot` file**; tests already cover the label-on-unpinned-node case.
> **`hk-83hg1` needs NO separate fix — subsumed.** Conditions: rebase onto tip (a verdict is pinned
> to its SHA) + **assessor gate** at the landing SHA. This confirms the assessor's earlier invariant (*safety is a property of the GRAPH,
> not the LABEL*) by live experiment rather than reasoning.
>
> **`hk-yhvrh` ESCALATED to a RAMP PREREQUISITE** (was P2, unassigned): `harmonik init` scaffolds
> no `codex:` block, so **every fresh project is structurally unable to run codex** until
> hand-edited. It has bitten india **three times**. Any label ramp into a freshly-initialised
> project hits that wall before the label can route anything.
>
> **CORRECTION CARRIED (india, admiral propagated the error):** ~~"a defer running proves graceful
> termination, since SIGKILL runs no defers"~~ — **WRONG for panics**; Go runs defers during panic
> unwinding. Holds for SIGKILL/OOM only. `hk-yrm9x` P2: a panic in the boot shell unwinds through
> the defer and **masks a real crash as a clean shutdown**. Fix = `gracefulExit` flag at three
> return sites, explicitly **NOT `recover()`**, which would swallow the panic we need to see.

## ★★★ FREEZE-RULE AMENDED — **A LABEL SELECTS THE GRAPH; FREEZING A FILE CANNOT ENFORCE IT** (assessor found; admiral verified + amended, 08:2xZ)

> **MY RULE WAS UNENFORCEABLE AS WRITTEN.** "While any ramp is live, the MODE is frozen and
> `workflow.dot`'s CONTENTS are frozen" assumed the graph is a global config artifact. **It is not.**
> Verified at source: `moderesolve.go` — `dotRefLabelPrefix = "dot:"`; a bead carrying `dot:<name>`
> routes to `<name>.dot` in the project dir as the **tier-1** path in `resolveWorkflowRef`. Plus a
> tier-1.5 `codename:eval` route to `eval-bead.dot`. **Anyone can change which graph a bead runs, by
> labelling the bead, without touching workflow.dot.**
>
> **LIVE TODAY, PRE-FIX:** `eval-bead.dot`'s judge node is reviewer-class, carries
> `model=claude-opus-4-8`, and has **NO harness pin**. A bead with `harness:codex` + `codename:eval`
> sends its judge to codex and **never reaches a verdict** — hk-pkxju's shape, reachable by two labels.
>
> ### AMENDED RULE (replaces part 3 of the freeze)
> **A `harness:codex` bead MUST NOT also carry a graph-selecting label** (`dot:<name>`, or a
> `codename:<x>` that maps to a graph) **unless that graph's reviewer nodes are pinned.**
>
> **RAMP-PLAN NEEDS A 5th EDIT:** its WAVE-1 SELECTION RULE bars a non-default **`WorkflowRef`**
> (tier-0) but **NOT the tier-1 LABEL path.** Two different mechanisms; the rule closes one. Bar BOTH.
> `hk-ofm89` is adjacent but checks the **MODE**, not the **GRAPH** — both needed.
>
> **THE CLASS OF ERROR, pointed at myself:** I wrote a rule protecting a **FILE** when the attack
> surface is a **SELECTION**. **When you write a freeze, ask what else can change the thing you are
> freezing — not just who can edit it.**

## ★★ GATE VERDICTS — BOTH ACCEPTED, BATCH AUTHORIZED TO LAND (08:2xZ)

> **`hk-7qmpp` @ `762cc10d`: PASS CODE / REQUEST_CHANGES SPEC — LAND IT.** The old destructive reset
> fires on EVERY merge; both defects are cheap and change no design. Blocking preserves the worse
> state. **`hk-pkxju` @ `9489510e`: PASS**, lands behind it.
>
> **THE SPEC-GATE JUSTIFIED ITSELF ON FIRST USE.** My "reviewed AS a normative-spec change" guard
> caught it: the **v0.9.3 revision row was inserted INTO the §4.14 EM-064 read-order table** — a
> normative four-tier chain whose prose says *"MUST NOT reverse"* / *"Tier 1 MUST be first"* — where
> it sits **above Tier 1** wearing a Tier value of "2026-07-22". A code-shaped review sees a docs
> hunk and moves on. **Fix as an IMMEDIATE follow-up commit, not queued behind the batch.**
>
> **SHARPEST FINDING — mike's disclosed limitation's rationale is FALSE for exactly the region that
> motivated the fix.** The skip-if-uncomputable path is justified normatively as *"stale paths
> surface LOUDLY as dirt the next escape check reports."* **`isHarmonikChurn` exempts `.harmonik/`,
> `.claude/`, `.beads/issues.jsonl`, `AGENT_COMMS.md`** — the fleet-state paths whose destruction
> started this incident. The justification holds everywhere EXCEPT where the bug lived.
> **Fair correction to keep: the EVENT is loud (`working_tree_refresh_failed` + stderr); the
> resulting STATE is not.**
> **FILE (not blocking): the skip leaves the INDEX stale** (index at mainTip, HEAD at runTip) — a
> later commit of those paths from main root commits PRE-MERGE content: **a silent revert of the
> merged change**, inside `.harmonik/`, undetected by the escape check.
>
> **`hk-pkxju` WEAKNESS 1 CONFIRMED (assessor reproduced india's predicted mutation):** deleting the
> guard leaves **ALL 18 subtests GREEN — including the DispatchWiring grep guard written to protect
> that very call site**, because it asserts a substring the mutation leaves byte-identical. And the
> break is real: unconditional assignment **clobbers the pin with empty**, dispatch falls to the
> label-sensitive builder, and a tier-1 label drags a PINNED reviewer onto codex. **One deleted line
> reopens the exact defect the commit closes, with a green suite.** Not a BLOCK — shipped code is
> correct and double-guarded; this is a test-coverage gap on a correct fix.
>
> **STILL OPEN — NEVER OBSERVED AT DISPATCH.** Assessor ran no daemon; the guarantee is code-verified
> and mutation-characterized only. **AUTHORIZED before wave 1:** cheap experiment — a bead labelled
> `harness:codex` + `codename:eval` on an isolated daemon, watching `harness_selected` on the judge
> node (`eval-bead.dot` reaches a reviewer without a full codex implement). It tests the amended
> freeze rule and the code in one shot.
>
> **BEADS TO FILE:** (a) dispatch-level test that FAILS under the mutation — a pin surviving THROUGH
> `dispatchDotAgenticNode`, not the helper (own bead; it would be lost inside `hk-j4jon`);
> (b) the stale-index silent-revert risk. **RAISE `hk-j4jon` AT WAVE 1** — filed "currently inert"
> because config has no `agents:` block, but it goes live the moment anyone puts a `model:<x>` label
> on a codex bead. **Inert-until-we-do-the-thing-we-are-about-to-do is not inert.**

## ★★★ TEST-THEATER WAS **CONCEALING** DEFECTS, NOT MERELY MISSING THEM (2 instances, 08:2xZ)

> **Repairing a vacuous check is surfacing REAL defects immediately — twice tonight:**
> - lima repairs the vacuous `hk-qx065` scenario assertion → it goes live and **instantly catches a
>   real ~30% failure**: reviewer worktree trust key not persisting after 4 write attempts (5-of-17,
>   always review iteration 1, **both preservation probes GREEN** so it is NOT a lost-update).
>   Filed **`hk-6qw8s` (P1)**.
> - kilo's implementer fixes the helper-pinning init test → removing the wiring now **fails FIVE
>   tests, two previously incapable of failing.**
>
> **REFRAME:** we have treated test-theater as *effort wasted on tests that catch no new bugs*. It is
> worse and more valuable than that — **those tests were actively CONCEALING defects that already
> existed.** Every vacuous assertion is a bug we already have and cannot see. This makes `hk-3i19p`
> (mutation-at-the-wiring) a **defect-DISCOVERY tool**, not a quality nicety, and argues for sweeping
> the EXISTING suite rather than only gating new tests. Worth a lane; not tonight.

## ★★ INLINE MODE — THE GATING BEAD IS NOW `hk-6qw8s`, NOT `hk-qx065`

> Inline mode stays **ON**. `hk-qx065` (`87b0e3ca`) narrows the trust race without closing it —
> lima's own finding, unchanged. **`hk-6qw8s` now gives the remaining gap a MEASURED shape:**
> reviewer trust-key non-persistence at **~30%**, preservation probes green. That is no longer a
> caution, it is a quantified failure rate on the worktree-trust path.
> **Do NOT lift inline mode on `hk-qx065` landing alone.** The fork lima named: never-calibrated
> `trustWriteMaxAttempts=4` (*"nobody counted clobbers"*) vs the fixture's erase model for
> late-created worktrees. **"Never calibrated" is exactly the class of number that has been wrong
> all night.**
>
> **KNOWINGLY-RED SCENARIO TEST — endorsed, with an added requirement.** `//go:build scenario`
> (out of gate + default CI, hand-run only), documented repro of `hk-6qw8s`, still passes the review
> gate AS a documented red repro, no `Trivial:` bypass. Better than uncommitted (tonight's loss
> class) or re-vacuumed (the dead check we spent the night killing). **ADMIRAL ADDITION: it needs a
> STATED EXIT CONDITION or it becomes furniture** — goes GREEN when `hk-6qw8s` resolves, and closing
> `hk-6qw8s` without it going green is a contradiction someone must explain. A red test tied to a
> live bead is a MARKER; tied to nothing it is NOISE, and the two look identical six hours later.
> (That is how *"internal/daemon is RED"* became an unexamined fact that turned out to be four
> separate things, one not a bug at all.)

## ★ DISK: SECOND MANUAL RECLAIM — GUIDANCE IS NOT HOLDING (admiral acted, 08:2xZ)

> Disk slid **22→18 GiB over five sweeps**, one-way, toward the daemon's **10 GiB watermark below
> which dispatch stops SILENTLY** (~8 sweeps out). **Admiral reclaimed 8.6 GiB** on lima's verified
> procedure — 54 `tmp.*` dirs >30 min old, each checked for Go-cache shape: **49 removed, 5 SKIPPED**
> as non-matching (no blanket delete). **Now 27 GiB / 86%.** Repo untouched.
> **THE FINDING: this was the SECOND reclaim tonight. lima cleared 64 dirs hours ago and broadcast
> one-cache-per-SESSION guidance — 49 fresh stale caches accumulated since.** Safe-by-discipline
> losing to safe-by-construction for the third time tonight.
> **`hk-137y6` OWNED (mike)** — fixed per-agent cache path, **bounded + non-purgeable + outside-the-
> reap**. Read with siblings **`hk-cy4ej`** (the silent watermark stop — the symptom) and
> **`hk-pgtbr`** (cache in macOS-purgeable `~/Library/Caches`, wiped mid-run). Three beads, one
> subject: where the build cache lives and who may delete it.

## ★★ CONCURRENCY >1 (CHEAP VARIANT): **PASS** — and the one mitigation tonight that WORKED (india, 08:0xZ)

> 3 beads w/ `harness:codex`, `--max-concurrent 3`, reviewer nodes removed. **Zero Claude tokens.**
> **Q1 attribution CLEAN** — each commit touched exactly its own 2 files, every `Refs:` matched its
> own bead, no token crossed packages. **Q4 CLEAN** — all 3 terminal, no orphans, no held slots.
> **BONUS: the tier-1 label held at concurrency 3.** Timing near-linear: 3× work for ~1.35× wall-clock.
>
> **Q2 — THE `~/.codex` RACE IS REAL, OBSERVED, AND THE GUARD WON.** Three guards fired in the SAME
> SECOND on one global file: one removed a genuinely stale WAL (0 codex procs holding it); **the
> other two detected the file mutating after their backup and correctly ABORTED.** The TOCTOU
> re-check india had called "narrow and low-probability" **fired TWICE in one 3-way run** — the only
> thing between two guards and a WAL three live processes were about to write 2.5 MB into.
> **After six mitigations that caused the harm they prevented, this is one that behaved correctly
> under the concurrency it was written for.**
>
> ### ★ GENERALIZATION: **A RACE YOU COMPUTE AS UNLIKELY AT CONCURRENCY 1 IS NOT UNLIKELY AT N.**
> Every probability we have estimated on this system was estimated in the only configuration we had
> ever run — concurrency 1. Treat those estimates as **unmeasured**, not as small.
>
> **FULL VARIANT AUTHORIZED but SEQUENCED AFTER the gate batch** (assessor mid-gate, disk 19 GiB,
> hk-7qmpp is critical-path first land). **Q5 — does the review bottleneck scale linearly or CONTEND
> at N — remains the last open question and is what decides the runway model.**

## ★★ ADMIRAL ERROR: I NEARLY RESTARTED A WORKING ASSESSOR (08:0xZ)

> I sampled the assessor's pane, saw no spinner + "Press up to edit queued messages", concluded
> WEDGED, and ran `keeper restart-now` against it. **It was mid-gate the whole time** — 6+ min into
> `go test ./internal/daemon/` on india's `9489510e`. My captures landed between spinner frames.
> **The keeper's stale-handoff refusal is the ONLY reason I did not destroy its work.** A guard I was
> about to call broken stopped a destructive act I had talked myself into.
> **NORM: a quiet pane is not evidence. Read the SCROLLBACK for a running command before touching a
> crew; never act on a single sample.** (Third bad pane-read of the night; the only one I acted on.)
> **`hk-lrzbx` FILED** — `keeper restart-now` demands a fresh handoff a wedged session cannot write
> (same unreachable-precondition shape as juliet's `loadavg<8`). **Captain's sharpening, better than
> mine: the fix must KEEP the mid-work protection while adding a wedged-case escape — not drop the
> gate.** Asking what REMOVING a guard destroys, not only what firing it destroys.

## ★★ `hk-3i19p` — MUTATION-AT-THE-WIRING IS A **REVIEW-GATE ITEM**, not a sixth rule (kilo; admiral RULED)

> kilo found **five instances tonight** of: *tests pin the thing we just wrote (a local helper), NOT
> the production call site* — so deleting the real wiring leaves the test GREEN. His framing, kept:
> *"it is a property of how we write tests here — we pin the thing we just wrote rather than the call
> site that uses it."* The mechanical form of **WIRED IS NOT SOUND**.
>
> **THE GATE: a test is not done until it FAILS when the PRODUCTION CALL SITE is neutralized** — and
> the **REVIEWER**, not the author, re-runs the probe and says so in the verdict.
> **WHY A GATE, NOT A RULE:** the failure mode is *believing a green signal without verifying it
> could go red*. A rule is a thing agents must REMEMBER; a gate is a thing that gets CHECKED. Fixing
> a class of unchecked assumptions with another reminder repeats the mistake one level up.

## ★ RAMP-PLAN REVIEW (`90a2e372`) — **APPROVE WITH CHANGES** (admiral, 08:1xZ)

> Thesis correct and now **independently proven live** by india's tier-1 run. Changes blocking merge
> toward target: **(a)** §0 capability claim is no longer OPEN — PROVEN by the strength test +
> tier-1 run; **(b)** cites `hk-3dgps` (DUPLICATE) — canonical is **`hk-83hg1`**; **(c) most
> important — the WAVE-1 SELECTION RULE has a PENDING EXPIRY:** it exists because 21/23 graphs are
> unpinned, and **`c7f3ea30` closes exactly that hole.** State that it is REQUIRED until c7f3ea30
> lands and REDUNDANT-BUT-KEPT after — do not delete it, or the ramp silently loses the record of
> what was protecting it; **(d)** §1d's own baseline SHAs are stale — a document about verdict
> pinning must not carry a stale baseline.
>
> **PROCESS GAP RULED — SPEC A `PRESERVE` COMMIT:** "commit fleet state" collides with "the gate
> demands review trailers", so under a reset hazard with no reviewer `Trivial:true` is the only
> lever — forcing a dishonest label on an honest act and corroding what Trivial means for everyone.
> Shape: explicitly marked, no trailers required, **BARRED from merge toward target by TOOLING, not
> discipline** (what does it do when it misfires? someone sneaks unreviewed code — so enforce the bar).
>
> **NORM ADOPTED (juliet): `git diff HEAD` SILENTLY OMITS UNTRACKED FILES.** Her saved patch dropped
> the countingledger helper for exactly this reason. Never use it as a state backup — `git status` +
> explicit copy, or `git stash -u`, or `git add -A` first. **A backup that silently omits part of
> what you asked it to save is a signal that looks like evidence.**

## ★★★ THE STANDING QUESTION (mike) — **BEFORE A GUARD LANDS: WHAT DOES IT DESTROY ON THE DAY IT MISFIRES?**

> Deliberately a QUESTION, not a sixth rule — a habit of mind, not a checklist item.
> **Four times tonight a mitigation caused the exact harm it was written to prevent:**
> 1. `GOCACHE=$(mktemp -d)` (adopted to survive the daemon's cache reap) **filled the disk** —
>    and below the 10 GiB watermark the daemon stops dispatching entirely.
> 2. The srt engagement retry loop (written to survive a transient under fork pressure) **forks
>    4+ processes per attempt and manufactures that pressure** — against a transient now
>    demonstrated absent.
> 3. My re-apply-the-deletions instruction (to clear a build break) **would have relocated the
>    break** onto a branch and pushed it.
> 4. EM-054's working-tree refresh (to keep main in sync) **destroys working trees.**
>
> Every one shipped reviewed for *whether it works*, never for *what it does when it is wrong*.
> **A step that must be correctly guarded FOREVER is worse than a step that cannot destroy** —
> prefer deleting the destructive step to guarding it. **Corollary: if you are guarding the
> second half of an operation, READ THE FIRST HALF.**

> **DURABLE FIX, MANDATORY:** fleet state under `.harmonik/` must be **COMMITTED**, not left
> modified-in-tree. Everything lost tonight was lost because it lived only as an unstaged
> working-tree change for hours.

---

> **STANDING RULE (operator-mandated 2026-07-05) — PRE-DEPLOY E2E TEST GATE.** No daemon
> deploy ships without new end-to-end tests, added that deploy, that reproduce the changed
> behavior on a real launch path IN ISOLATION from the live daemon (never test on the primary
> daemon; green units are not the gate). Enforce every deploy. Canonical: orchestrator-rules
> §"PRE-DEPLOY END-TO-END TEST GATE" + `docs/daemon-redeploy.md` GATE 0.

> **STANDING RULE (operator, restated 2026-07-22) — THE ASSESSOR VALIDATES ALL RELEASES.**
> No exceptions. A **daemon binary swap is a release**, and so is a merge to the target
> branch, a `promote`, and a substrate/config flip. None happen without an assessor PASS
> delivered on `--topic gate`. The assessor stands its OWN isolated daemon and proves the
> behavior there; a live prod bead is NEVER a canary. The admiral holds the final release
> call; the assessor executes and recommends.

> **STANDING RULE (operator, 2026-07-22) — THE FLEET MULTITASKS.** Multiple beads in flight
> across multiple crews and lanes is the expected steady state (daemon `max_concurrent`=4).
> One-bead-at-a-time is a FAILURE MODE. The admiral measures concurrency every sweep and calls
> out any period with 0 or 1 beads in flight while ready, unblocked work exists.

> **STANDING RULE (admiral, 2026-07-22 07:1xZ) — NO TREE-WIDE DESTRUCTIVE GIT IN THE MAIN REPO.**
> No agent runs `git reset --hard`, `git checkout -- .`, `git restore .`, or `git clean -fd`
> in `/Users/gb/github/harmonik`. Not to clean up, not to reach a known state, not to unstick
> a hook. Work in a worktree. If you believe the tree must be reset, STOP AND ASK.

> **ADMIRAL CADENCE (operator, 2026-07-22):** sweep every **12 minutes**. Each sweep: capture
> the captain's pane and judge what it is actually doing, count concurrency, check no release
> slipped the assessor gate, score initiative motion. The job is to make the system PROGRESS.

> **Admiral-owned.** Status vocabulary: ACTIVE (worked now) · ON-DECK (next to staff, no
> blocker) · PARKED (zero ready beads now — a FACT, not a hold) · GATED (NAMED, DATED, OWNED,
> EXPIRING gate) · DONE (landed).

---

# THE ROADMAP (rebuilt 2026-07-22 from the 07-20/07-21 planning sessions)

**The frame, in plain language.** A multi-day effort to run Codex over ssh was halted by the
operator. Investigation found two things: the security fence blocking local Codex was invented
by an *agent*, not the operator; and shipping the repo to another machine to run work over ssh
is the wrong architecture — scrapped, not patched. What replaces it grew into building
harmonik as **applications on a platform/kernel**. But before any of that: **Codex has to run
work locally**, because Codex is cheap and Claude is not.

| # | Tier | Item | State |
|---|---|---|---|
| T0 | Unblock | ⚠ **`hk-9hvr0` (P0) IS ***NOT*** DEPLOYED — I DECLARED IT FIXED AND I WAS WRONG.** ~~"paste-wedge FIXED, verified an ancestor of the running binary"~~ **STRUCK.** Verified by CONTENT 09:2xZ: deployed `eb2b4f1a` still has `const inputBufferName = "harmonik-input"` (**the BUG**); the branch tip `90c9178b` has `const inputBufferPurpose = "input"` + `bufferName(id, ...)` (**the FIX**). **My ancestry check gave a false positive and I used it to tell the captain "dispatch is NOT wedged" and to fill the queues.** Nothing broke only because we have been in INLINE mode all night and the tmux-substrate dispatch path was never exercised — **luck, not correctness.** ⛔ **DO NOT RE-ENABLE DAEMON DISPATCH ON THE TMUX SUBSTRATE until a binary carrying `90c9178b` is deployed** — it will wedge with the original signature (`pasteinject_failed`, worker reaches `agent_ready`, never receives its prompt, run idles dead)  · `hk-8juwz` ~~FIXED by the revert~~ ⚠ **CLAIM UNVERIFIED — the bead is still OPEN and still assigned to lima (checked 09:1xZ).** Either it was fixed and never closed, or this note was wrong. **Do not close it on the strength of this entry** — re-verify whether `claude:local` still wedges on the theme modal at current tip · `hk-45pm7` recommended not-a-defect · `hk-qx065` P0 trust race — code landed `87b0e3ca`, **BEAD STAYS OPEN, INLINE MODE NOT LIFTED** (lima: it NARROWS the race, does not close it; one trial is not a rate) · **still open:** `hk-wwa4z` (captain comms — now on a NAMED durable cursor, but stays open until the daemon-side `--follow` is fixed; not closed on a poll workaround), `hk-bkd6h` P1 crew OAuth wall | **CLEARED for dispatch 04:50Z.** ⚠ **BASELINE DRIFT:** assessor gated at `4d308f3b`; a verdict is PINNED TO ITS SHA and does not roll forward — re-gate at the real HEAD at the epic→main boundary |
| T1 | **Priority-0** | **codex-first** — local Codex clears a bead end-to-end | **ACTIVE. TWO PROOF POINTS (N=2).** Wholesale `codexdriver` flip REJECTED PERMANENTLY; label ramp BLESSED. **RAMP UNAUTHORIZED at N=2** — see §STRENGTH TEST |
| T2 | **PROMOTED → ACTIVE 07:0xZ** | **`hk-pisrf`** — de-hard-code the reviewer (per-node model+harness from config) · remove daemon auto-dispatch (`hk-04q2j` + `.1/.2/.3`, `hk-xwlm2` — IN FLIGHT) | **`hk-pisrf` is now ACTIVE, not a fast-follow** — see §PREMISE CORRECTION. juliet staffed |
| T3 | Platform | P1 kernel/fabric · P2 extraction · P3 distributed execution — all three IN PARALLEL | drafted+reviewed, 7 must-fixes unapplied, no kerf works, zero beads |
| T3b | Platform-parallel | **uniform sandbox/security** — operator: *"not later — in parallel"* | **STAFFED (lima) + REORDERED:** `hk-scaj0` FIRST; its prerequisite is now the two REAL defects **(a)+(b)**, NOT the refuted saturation story — see §THE SANDBOX GUARD IS BLIND |
| T4 | Capacity | remote concurrency ~9 (3 Pi + 4 Claude + 2 codex); fails at 6 today | operator suspects an architecture issue |
| T5 | Debt | the kerf works are shells, not actually worked | operator-flagged 2026-07-22 |

---

## ★★★ "DEPLOYED" HAS **THREE** CASES — and one of them needs no release at all (assessor, 09:5xZ)

> **`hk-a5fxl` IS LIVE WITH NO BINARY SWAP.** Merge `650f359b`. Verified by content:
> `eb2b4f1a` (deployed binary's commit) → **0** matches for `timeout="1800"`; new HEAD → 1;
> **the ON-DISK `workflow.dot` the daemon reads → 1.** *"By the commit it is not deployed. By the
> file it is live. The file governs."*
>
> **MECHANISM, chased not assumed:** `workloop.go:4117` calls `LoadDotWorkflowWithParams` on **every
> bead run**; `loader.go:135` uses a plain `os.ReadFile` — **no cache, no boot-time load.** So the
> merge landing in the working tree **IS** the deployment. **A binary swap would have changed
> nothing** — and we skipped a production release we did not need.
> **Strongest confirmation available:** ran the LIVE binary against the deployed file —
> `harmonik graph validate workflow.dot` → valid.
>
> ### THE TAXONOMY — use this, not ancestry
> ```
> compiled Go         -> needs a BINARY SWAP.  git grep <symbol> <deployed-sha> -- internal/
> runtime-read files  -> MERGING INTO THE WORKING TREE **IS** THE DEPLOYMENT.
>                        Read the file in the project dir. No commit answers it. No swap.
> ancestry            -> answers "was it ever merged". Never the question.
> ```
> **This third case did not exist in the admiral's model and has already changed two decisions.**

## ★★ WORKTREE RECLAIM — THE HAZARD IS **DETACHED HEAD**, NOT UNPUSHED (mike tested; lima + juliet refined)

> **THREE QUESTIONS, not one:**
> 1. **Tree dirty?** — git answers; plain `git worktree remove` refuses. *(admiral)*
> 2. **Work FINISHED?** — only you. **Clean ≠ finished** *(lima: a clean tree held unpromoted commits)*
> 3. **Anything RUNNING in it?** — only you. **Finished ≠ idle** *(juliet: a test run does not dirty the tree)*
>
> **mike PROVED the real hazard in a scratch repo:** branch-backed worktree + unpushed commit →
> removed → **nothing lost** (the branch ref outlives the worktree). **DETACHED** worktree +
> unpushed commit → removed → object survives but `git branch --contains` returns **NOTHING** →
> unreachable, GC-eligible. **That inverts the sweep: branch-backed are the safe bulk deletions;
> the 9 detached ones are where to slow down** — each in a scratchpad of a *different* session id.
> ```
> git worktree list | grep detached
> git branch -a --contains <SHA>     # EMPTY -> DO NOT REMOVE
> git -C <wt> branch save/<name>     # make safe first
> ```
> **Admiral ran it on all 9: exactly one unreachable (`35694ac1`) and it is SAFE** — tree
> `715ce657` and patch-id identical to branch-reachable `80859874`. **Verified, not alarmed.**
>
> **LEAK FOUND: `harmonik promote` does not clean up its temp worktree** — `35694ac1` sits in
> `/private/var/folders/.../hk-promote-2570812446`, registered, owned by nobody, **one per promote
> forever**, in the exact category just established as dangerous. **File it.**

## ★★★ THE DEPLOYMENT CHECK I USED ALL NIGHT IS BROKEN **BOTH WAYS** (admiral + mike, 09:2xZ)

> `git merge-base --is-ancestor <fix> <deployed>` answers **"was this commit ever merged"** — which
> is **not the question anyone was asking.**
> - **FALSE POSITIVE on REVERTS** (admiral): `a964cbcb` reads as an ancestor of `eb2b4f1a`; the code
>   is gone (a revert adds an INVERSE commit, it does not remove history).
> - **FALSE NEGATIVE on CHERRY-PICKS** (mike): `762cc10d` reads as NOT an ancestor; the code is
>   plainly there (`refreshMergedPaths` ×3). **Cherry-pick is how the captain lands EVERY commit** —
>   so this direction is live in normal operation, not an edge case.
>
> **THE CORRECT CHECK — ask the CODE, never the graph:**
> `git grep -c '<symbol the fix introduces>' <deployed-sha> -- internal/`
>
> **RE-AUDITED BY CONTENT:** `hk-7qmpp` **DEPLOYED** (ancestry said no) · `hk-pkxju` **NOT deployed**
> · `hk-9hvr0` **NOT deployed — a P0 I had declared fixed** · `hk-a5fxl` **UNCONFIRMED, not asserted
> either way.**
>
> **This is "right for a reason you have not verified" with a P0 underneath it, found within the
> hour of broadcasting that principle** — and found only because lima's sandbox work forced a look
> at the actual binary.

## ★★★ THE NIGHT'S SHARPEST LESSON (mike) — **BEING RIGHT FOR A REASON YOU HAVE NOT VERIFIED**

> mike shipped a cache guard on mtime, then — prompted by juliet's false positive on an *unrelated*
> detector — went back and TESTED whether the same transient could fool his signal. Back-dated a
> cache to January, churned a shard dir, re-read the parent mtime: **it jumps to NOW**, so his guard
> fails toward "in use" **by mechanism rather than by luck.** Then he said the thing that matters:
> *"I chose mtime for convenience, not because I had reasoned it through. **I was right for a reason
> I had not verified.**"*
>
> **THIS IS THE FAILURE MODE UNDER MOST OF TONIGHT, and it is invisible because it does not look
> like a mistake:**
> - The **pidfile lock** is the only thing that stopped a rogue production daemon — three agents
>   tripped that footgun and **nobody had designated the lock as the safety.**
> - **The admiral** read *"pidfile is locked by another daemon"* as a health check for hours;
>   conclusions drawn from it happened to hold. **Luck, not method.**
> - **The keeper's stale-handoff refusal** stopped the admiral restarting a working assessor
>   mid-gate — a guard he was in the middle of calling broken.
> - **mike's mtime**: right signal, unverified reason, caught twice by himself.
> - Every *"we already knew this"* — `hk-nbv7p` filed a week earlier, the RUN-LOG that already
>   carried the answer — was a reason nobody re-checked.
>
> **WHY IT OUTRANKS BEING WRONG: nothing flags it.** A wrong answer eventually fails. A right answer
> resting on an unverified reason works until the reason shifts, then fails in a way that looks
> impossible — because everyone remembers it working.
>
> **THE PRACTICE (not "be more careful"):** (1) when something works, know WHY — "it just does" is an
> unverified reason holding weight; (2) **the guards you did not design are the most dangerous** —
> pidfile locks, exempted paths, default timeouts; nobody owns them and nobody notices when they
> move, so **if a guard is load-bearing, say so out loud and stop it being accidental**; (3) go back
> and verify load-bearing reasons *before* something shifts under one.
>
> **COROLLARY, true all night: a NEAR-MISS REPORTED is worth more than a FINDING KEPT.** juliet's
> cost her a discovery and saved the fleet an hour hunting a culprit that does not exist — and mike,
> the person it would have been blamed on, said so himself.
>
> **DETECTOR COROLLARY:** *a detector built from one incident inherits that incident's coincidences.*
> india's cache really was emptied AND `du` really did read near-zero — only one caused the other.
> **Copy the mechanism, not the symptom.** Cache-wipe diagnosis: size is a PROMPT, never a
> CONCLUSION — confirm with file count, the README+trim.txt residue, and `could not import` naming
> your own private path.

## ★★★ THE FOUR RULES — tonight's standing behavioral contract (broadcast 07:1xZ / 07:2xZ)

> All four are one rule seen from different sides: **A SIGNAL THAT LOOKS LIKE EVIDENCE IS NOT
> EVIDENCE.** All four were earned by lima correcting *himself*, unprompted, three separate
> times, each in the direction that made him look worse.
>
> 1. **PRESENT IS NOT WIRED.** A guard that exists but is never called is not a guard. Show the
>    CALL SITE, not the definition.
> 2. **WIRED IS NOT SOUND.** `verifySandboxEngaged` is called at both production sites and IS
>    fatal — and is still BLIND. Read the PREDICATE, not just the call site.
> 3. **PASSING IS NOT PROVING.** An assertion that cannot fail is not a weak test, it is a test
>    that was never testing. Before citing a green test, know what would have made it red.
> 4. **WRITTEN-DOWN IS NOT STILL-TRUE — strike superseded findings IN PLACE**, and **follow the
>    strike UPSTREAM to where the claim came from.** A refuted conclusion sitting next to its
>    replacement is a live trap; striking only the downstream copy leaves the trap one hop away.
>    Applies to run logs, bead descriptions, handoffs, mission files, and THIS REGISTRY.
> 5. **A BUILD FAILURE IS NOT A TEST FAILURE.** `FAIL` from `go test` is not evidence that a
>    test failed. lima got 5/5 FAIL and was one step from reporting "the transient reproduces
>    under load" — it was a build break the whole time; 15/15 PASS in a clean worktree. Confirm
>    the thing under test actually RAN before reporting a red.
>
> **Instances tonight:** a reviewer · a scenario fixture · a bead description that read as
> corrected while its primary field still carried the accusation · a RUN-LOG carrying both a
> refuted and a correct cause · and **the admiral**, who recorded that a sandbox hole "may be
> firing in production right now" from load alone without checking the mechanism could run.

## ★★★ FIVE OF SEVEN KEEPER WATCHERS WERE DEAD — `hk-220lv` LIVE AT FLEET SCALE (admiral, 07:2xZ)

> mike, kilo, juliet, india, lima: all `✗ live-watcher — no live keeper watcher detected`. Only
> captain and assessor had one. **Restarted all five and verified running.** Not cosmetic: with
> no watcher nothing gauges context fill, no handoff fires, and the pane wedges mid-task with no
> warning and no intent preserved.
>
> **A fresh `.ctx` gauge is NOT proof of a running keeper** — the gauge looked healthy on all
> five while the watcher was gone, which is why nobody noticed. Check the **`live-watcher` line
> specifically**, on every agent, after every restart. This is "present is not wired" applied to
> our own supervision.

## ★★ RULE 6 — **PARK IT BEFORE YOU DELETE IT** (mike, generalizing india; admiral ADOPTED)

> When something is in your way, make it harmless in a way that **PRESERVES THE EVIDENCE and
> undoes in one command**, rather than removing it.
>
> **The case, and it is an admiral error.** I told mike to re-apply four deletions the reset had
> undone. He checked and REFUSED ON MERITS: all four still guard LIVE shipping code (br-ready
> priority `workloop.go:1910,:1961` · no-auto-pull `daemon.go:379-395` · operator-pause gate
> `operatorpause.go:45,:95` · bounded-retry invariant `workloop.go:2772`), because **the change
> those deletions belong to (`hk-04q2j.1`) has not landed.** Second independent failure:
> `noautopull_em066_em067_test.go:51` DECLARES `countingLedger` and a **tracked** file
> (`l5saf_localonly_strand_test.go:97`) USES it, while the replacement helper is untracked — so
> my fix would have produced a branch that does not compile, relocating the exact break I sent
> him to repair. **I sent a crew at a symptom with a fix I had not traced to its call sites.**
>
> He instead RENAMED the untracked duplicate in place to `…​.PARKED-hk04q2j1-awaiting-path-deletion`:
> break defused, zero coverage lost, nothing tracked touched, one `mv` to undo. Verified vet +
> build + test-link clean. **Two crews have now overruled the admiral on evidence tonight and
> both were right.**

## ★★ DISK-LOW SILENTLY STOPS ALL DISPATCH — and our own workaround causes it (lima + kilo `hk-cy4ej` P1, 07:2xZ)

> **Below `diskLowWatermarkDefault` = 10 GiB (`workloop.go:166`), `workloop.go:1801` SKIPS
> DISPATCH ENTIRELY — silently.** The box hit 97% / 7.2 GiB. Two of three scenario runs failed
> on a 75s timeout for that reason alone. **Silence under disk_low is indistinguishable from a
> hung run** — rule 5's family. **TRIAGE RULE: check `df -h` BEFORE blaming a timing-out
> daemon/scenario test.**
>
> **CAUSE — the mitigation for one P1 manufactured a second.** 243 orphaned `tmp.*` Go build
> caches (23 GB) from `GOCACHE=$(mktemp -d)` used INLINE PER COMMAND — the very workaround the
> fleet was told to adopt for `hk-gjbpp`. Each build leaves 100–190 MB; nothing reaps them. The
> daemon's own disk-low path reaps the SHARED cache, so the two interact badly.
>
> **STATUS: mitigated, NOT closed.** lima reaped 64 → 22 GiB / 88%, dispatch unblocked. **69
> stale dirs / 8.3 GB remain.** Routed to the captain: (a) make one-cache-per-SESSION binding
> (export once, reuse, delete at end), (b) reap the rest on lima's verified procedure (looks
> like a Go cache by numbered shard dirs · older than 30 min · leave recent ones), (c) FILE the
> real fix — a **fixed per-agent cache PATH** instead of a fresh mktemp per command, which gets
> isolation from the daemon's reap AND bounded disk.

## ★ MEASUREMENT NOTE — `br list --status=in_progress` IS A FALSE ZERO UNDER INLINE MODE

> It reads **0 while five crews work**, and will keep reading 0: inline mode dispatches nothing
> through the daemon, and orchestrator-rules forbids pre-setting `in_progress`. The field is
> structurally empty **by design**. While inline mode is on, score concurrency on **crew pane
> CONTENT + recent commits**, never on the bead ledger. (Also: a **queued** message on a busy
> pane is not a delivered instruction — three crews needed re-driving tonight for this.)

## ★★★ THE SANDBOX GUARD IS BLIND, AND `hk-y81iv`'s MECHANISM IS REFUTED (lima, 07:2xZ; admiral ACCEPTED)

> **(a) `verifySandboxEngaged` FAIL-OPENS.** `sandboxgate.go:244` reads
> `if runErr != nil && !wrote { return nil }` — commented *"engaged: srt itself failed AND the
> denied write never landed."* If **srt never runs at all** (binary missing, ctx cancelled, fork
> EAGAIN under load, ENOMEM), `runErr` is non-nil, the canary is absent, and the probe **reports
> the sandbox as ENGAGED**. It cannot distinguish *"srt ran and Seatbelt denied the write"* from
> *"srt never ran."* Blind in precisely the scenario it was written for. No deadline of its own.
> **Genuine production hole.**
>
> **(b) `sandboxOSTmpDirs()` puts `os.TempDir()` into `allowWrite`** — a daemon started with
> `TMPDIR=/tmp` grants every sandboxed run write access to **all of `/tmp`**. Latent today (only
> pi is sandboxed); **`hk-scaj0` is the change that ARMS it.** Ruled a HARD PREREQUISITE INSIDE
> `hk-scaj0`, not a follow-up.
>
> **`hk-y81iv` AS WRITTEN IS RETIRED.** The fork-saturation story is unsupported. Real cause:
> `Makefile:453` runs `TMPDIR=/tmp go test -short -race -count=1 -p=1 -parallel=1`, so
> `t.TempDir()` puts the fake "main repo" at `/tmp/Test.../001`; the tests hardcode
> `TmpDirs: ["/tmp","/private/tmp"]` (`sandboxacceptance_hki0377_test.go:187`,
> `scenario_sandbox_pi_i0377_test.go:162`); `sandboxprofile.go:234` appends them verbatim to
> `allowWrite` as a RECURSIVE rule. **The file the test expected protected was inside
> `allowWrite`.** Seatbelt allowed the write correctly; srt exited 0 correctly; stderr was empty
> correctly. Explains what saturation could not: **100% failure (8/8, 3/3), not intermittent** ·
> empty stderr, no `sandbox_init` diagnostic · fails on a clean solo suite · and decisively **the
> failing recipe is `-p=1 -parallel=1` — SERIALIZED, no fork storm in it.**
> `TMPDIR=/var/folders` turns all three green.
>
> **★ WE ALREADY KNEW THIS.** The correct cause was written into
> `plans/2026-07-17-assessor-daemon-campaign/runs/baseline-35e4b3b9/RUN-LOG.md:103-105` — **in
> the SAME FILE that still carries the saturation framing at `:96`.** The bead was filed off the
> earlier half. Cost: hours of rediscovery, a wrong bead, a reordered initiative. This is rule 4.
>
> **RETRY COST, UNPROVEN BENEFIT:** `srtEngagementMaxAttempts=5` + `hktch4tMaxDenyAttempts=3`
> retry against a transient (`hk-tch4t`) never demonstrated to exist — 5 srt startups per
> sandboxed launch. **Admiral ruled: do NOT remove yet.** "Never demonstrated" is not
> "demonstrated absent." Settle whether it is real, then keep or cut with evidence.

## ★★★ SANDBOX REALITY — **~97% OF ALL RUNS WERE NEVER SANDBOXED** (lima; admiral ACCEPTED)

> **ADMIRAL SELF-CORRECTION.** I recorded that `hk-y81iv` "may be firing in production right
> now" because the box was saturated. **Wrong** — I reasoned from a plausible signal without
> checking the mechanism could run at all. Struck, not softened.
>
> `.harmonik/config.yaml`: `sandbox.backend=srt`, `sandbox.harnesses=[pi]` — the entire list.
> `sandboxSpawnForRun` (`internal/daemon/sandboxgate.go:56`) returns nil unless backend==srt AND
> the run's agentType is in that list (a documented "strict no-op"), so for claude-code/codex no
> srt wrap is applied and the engagement-verification block is skipped entirely.
>
> **TALLY (full event log 2026-05-14 → now):** 3424 claude-code · 101 codex · 102 pi →
> **~97% of dispatched runs were never sandboxed at all.** Last 24h: 3 claude-code, 0 pi.
> With `--dangerously-skip-permissions` on every claude launch, that is the real posture.
>
> **CALIBRATE HONESTLY:** not a regression, not a breach, not new tonight. It is the STANDING
> posture on the operator's own machine, and `hk-scaj0` is what closes it. Recorded rather than
> escalated because nothing is failing open right now.

## ★★★ PREMISE CORRECTION — **THE BOTTLENECK IS REVIEW, NOT IMPLEMENTATION** (india; admiral ACCEPTED)

> The operator ranked codex-first Priority-0 on a RUNWAY argument. india measured it on a real
> bead (`br-r91`, run `019f8855`, terminal close, real UNPATCHED production commit gate):
>
> | node | wall-clock | share | fresh Claude tokens |
> |---|---|---|---|
> | implement (**CODEX**) | 318s | **6%** | **0** |
> | commit_gate | 460s | 10% | — |
> | review (claude) | 2,203s | 46% | — |
> | qa (claude) | 1,754s | 37% | — |
> | **reviewers combined** | **3,957s** | **83%** | **535,369** (32.4M incl. cache reads) |
>
> **Moving implementation to codex removes the CHEAP leg.** Codex-first still ships, but any
> runway model treating it as a large saving is wrong by ~an order of magnitude.
>
> **Consequence — `hk-pisrf` PROMOTED to ACTIVE.** It is what *converts* codex-first into
> runway. **NOT concluded: "put reviewers on codex"** — both reviewers earned their cost
> (`review` wrote its own probe and reproduced a predicted side effect, `hk-g5ug9`; `qa` traced
> every path out of the pre-claim block). Review quality is load-bearing and is not what we cut.

## ★★ STRENGTH TEST — CODEX CLEARED A HARD REAL BEAD END-TO-END (india; admiral ACCEPTED)

> **PASS, N=2, RAMP STAYS UNAUTHORIZED.** Bead `br-r91`, codex commit `644ddf6c`, containment
> re-verified after merge (`origin=/tmp/india-gate2-origin.git`, no path to the fleet repo).
> **Proven:** codex implements hard multi-file daemon changes correctly, writes red-at-base
> regression tests, self-commits with the trailer, survives the real production gate, passes two
> substantive claude reviews to terminal close. **Not proven:** concurrency >1 (zero evidence —
> **the next target**, and what the ramp actually depends on), sustained runs, cross-subsystem
> design. india's restraint upheld.

## ★ `internal/daemon` "RED" RESOLVED INTO FOUR THINGS (juliet, `hk-rn4i4`, full-package `-p 1` at `4d308f3b`)

> **DO-NOT-LABEL STANDS — but on ONE specific fixable bug, not a red package of unknown depth.**
> 1. **`hk-fei89`** — ⚠ **ROOT CAUSE RETRACTED BY juliet 07:2xZ. DO NOT ACT ON THE OLD FIX.**
>    ~~"shares an ON-DISK file lock; fix = isolate the lock path per test"~~ — **the lock path is
>    ALREADY unique per test** and cannot collide: `projectDir = t.TempDir()` (unique by
>    construction) + `targetRunID = uuid.Must(uuid.NewV7())` fresh per call
>    (`verdictexecutor_rc025a_test.go:75`), composed at `reconciliationlock_rc002a.go:137`.
>    **"Isolate the lock path per test" is a NO-OP and closing on it would close a bead that is
>    not fixed.** Corroborated: only one test file touches the lock; all `TestExecuteVerdict*`
>    siblings PASS together at `-count=1`; release is unconditional (defer first, fires on return
>    AND panic, `verdictexecutor_rc025a.go:113-124`); `Release()` is idempotent (`:105-115`);
>    errno mapping is strict — only EAGAIN/EWOULDBLOCK becomes `ErrReconciliationLockHeld`.
>    **WHAT STANDS: the OBSERVATION** — at `4d308f3b`, in-package, at loadavg ~48, it failed while
>    passing alone. juliet: *"I inferred 'shared path' from 'fails in package, passes alone'
>    without checking whether the paths could actually collide."* Note her original run ALSO had
>    5 packages fail to build from a cache wipe — see `hk-cy4ej`/disk. **Re-scope to "unexplained,
>    load-correlated" if it does not reproduce at tip; do NOT ship a change that makes a symptom
>    vanish without explaining it.** ⚠ **This means the last do-not-label reason is NOT yet
>    cleared.**
> 2. **ClaimSemaphore** — CONFOUNDED, left explicitly unresolved (the run cannot separate load
>    from bug). Honest non-answer; do not promote to a known-red list.
> 3. **MergeBuildGate** — did NOT reproduce; the assessor's red does not hold at this SHA.
> 4. **TestContextCancelled** — NEW, box-at-load-48 subprocess kill, NOT a code bug.
>
> **The gate that could never open:** juliet had held this behind a `loadavg < 8` quiet-box
> watcher. Load ran 19–75 all night, produced by the fleet's own sessions — **unreachable by
> construction.** Admiral killed the threshold 06:5xZ; the run completed at load 20→48.
> **Generalize: a gate whose condition the fleet itself prevents is not caution, it is a stall.**
>
> **FLEET INFRA FLAG:** `hk-gjbpp` RECURRED despite a private GOCACHE — 5 packages failed to
> build (40 could-not-import), `internal/lifecycle` NEVER RAN, so any lifecycle claim from that
> run is fabricated. **The private-cache workaround is NOT fully sufficient.**

## ★ PROVENANCE / COMMS AUTHENTICATION (`hk-4pulw`, `hk-gk4aq`)

> One genuinely forged message (a reversed `--from` in a multi-send shell command) **plus** the
> release-gate agent's own over-attribution of a genuine message — misattribution in BOTH
> directions within ten minutes, by the agent making the release decision, reading the same log.
> **Durable fix:** the daemon refuses a `--from` that does not match the authenticated sender.
> **Cheap fix that pays immediately and cannot break any caller:** the daemon already knows the
> true sender, so stamp an OBSERVED-sender field alongside the CLAIMED `--from` — that alone
> would have settled all three disputed messages in one query.

---

## ★ WHAT WAS LOST IN THE RESET AND NOT RECOVERED

> These sections existed in the pre-reset registry. Their TITLES are known; their bodies are
> **not** reconstructed here because I will not fabricate content I cannot verify. Where the
> information still lives is noted. Rebuild on demand, from the source, not from memory.
>
> - **§KERF DEBT** — the operator's "the kerf works are not actually worked" audit. *(One
>   retraction is known-good and must survive: the "codex-first is 0% written" finding was WRONG
>   — the work was `status=ready` with all 7 passes present, written 2026-07-21 19:51–20:00. It
>   cost juliet a wasted assignment.)* Source: `kerf show`, `kerf map`.
> - **§PLATFORM MUST-FIXES** — the 7 unapplied must-fixes gating P1/P2/P3 entering kerf. Source:
>   the platform-architecture plan + its review.
> - **§LOCKED DECISIONS** — do not reopen without the operator. Source: `STATUS.md`.
> - **§PRINCIPLES THE OPERATOR SET** — how work is done. Source: `~/.claude/CLAUDE.md`, AGENTS.md.
> - **§THE COMMIT GATE CANNOT REPORT A RED FOR `internal/daemon`** (assessor) — note `hk-a5fxl`
>   (`758d3f2c`, timeout 900→1800 + budget-aware flake-retry) has since LANDED against it.
> - **§hk-01vs0 — REAL BUT UNREACHABLE; does NOT gate the ramp** (india; admiral ruled).
> - **§THE RAMP'S SAFETY IS A PROPERTY OF THE GRAPH, NOT THE LABEL** (assessor) — headline ramp
>   invariant, 3-part form: mode frozen · `workflow.dot` CONTENTS frozen · the SET OF
>   DISPATCHABLE GRAPHS pinned to those with pinned reviewers. Proof it is not hypothetical:
>   `hk-83hg1`. Source: comms log 06:46Z, and `hk-83hg1`.
> - **§WAVE-1 PRECONDITION — enumerate every unpinned reviewer-class site** (owner: mike).
> - **§VERIFICATION IS TWO LAYERS**, **§CODEX-AS-CREW**, **§FLEET COMMS/KEEPER RELIABILITY**,
>   **§VERIFY-BEFORE-REVIEW**, **§REMOTE CONCURRENCY**, **§PLATFORM ARCHITECTURE P1/P2/P3**.
>
> **Everything substantive from tonight survived** because crews posted findings to the comms
> bus and to beads rather than only to files. `harmonik comms log` is the recovery source of
> record for this shift.
