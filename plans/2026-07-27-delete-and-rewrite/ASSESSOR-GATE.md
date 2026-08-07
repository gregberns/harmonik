# The two lists — what blocks an assessor sign-off, and what does not

**Written 2026-08-06 on the operator's instruction. Read `CHARTER.md` first for what the program is.**

## The objective this file serves, in the operator's own framing

Right now we want **one build that works, handed to an agent acting as the assessor.** The assessor
audits a candidate commit in a scratch daemon. It is not production. We are not running real beads
through this until the work is merged to main, and merging to main happens *after* the assessor signs
off — not before.

So the sorting question for every open defect is exactly one thing:

> **Would this stop the assessor from reaching an honest verdict on a candidate commit?**

"Honest" is doing the work in that sentence. A defect that makes the assessor's run *fail* is a
blocker. So is a defect that makes it *pass when it should not* — a false green is worse than a red,
because the sign-off is the whole point of the exercise. A defect that is real, serious, and simply
not exercised by a local scratch-daemon audit is not a blocker.

**List A** must be fixed before the candidate goes to the assessor.
**List B** can be fixed while the assessor works, or by an agent before major daemon work resumes.

---

## How much to trust this file

**This is a reasoned first cut, not a completed triage.** There are 3 open P0 and 80 open P1 issues.
Every item below was sorted from its own record and, where the claim was cheap to check, against the
code. Items were **not** individually reproduced. Two consequences follow, and neither is hedging:

- An item in List A might turn out not to fire in a local scratch run. Check before you spend a day
  on it.
- **List A is a floor, not a ceiling.** Roughly forty P1 issues were not examined closely enough to
  place. Any of them may belong in List A. Treat an empty result from this file as "not yet looked
  at", never as "cleared".

---

## List A — fix before the candidate goes to the assessor

### A1. The assessor cannot run at all

- **`hk-g0xgo`** — a stale sentinel trip file wedges all dispatch on an observe-mode daemon.

> **`hk-joacj` was listed here and is withdrawn.** It said a fresh project's first bead dies at the
> commit gate because the shipped graph hardcodes `scripts/scenario-gate.sh`, which `harmonik init`
> never writes. That script is deleted and the gate now runs `make full`. The defect survives in a
> new form — `init` writes no Makefile either, so the shipped default still names a command only
> this repo has — but **the assessor is not exposed to it**: `scripts/scratch-daemon.sh` builds its
> scratch project as a clone of this checkout, so the Makefile is there. Moved to List B, and the
> bead is corrected.
>
> Worth reading as a warning about this file: that item was placed from the bead's own text without
> re-deriving it, and the bead was two weeks stale. **Re-check before you spend a day on anything
> here.**

> **Sandboxing is OFF for the assessor — operator, 2026-08-06.** `hk-quoka` (a sandboxed run gets
> zero network egress) and `hk-bzydx` (a sandboxed run cannot reach its harness state directory) are
> therefore **moved to List B**. They are still real and still serious; they are simply not on this
> path. Do not promote them back without turning sandboxing on first.

### A2. The assessor reaches a verdict, but the verdict is not honest

This group is the reason the list exists. Each one makes something pass that should not.

> **How to treat these — operator rule, 2026-08-06. Disable, do not delete, and do not fix now.**
> A test that cannot fail is not helping, so it should not cost a run. It also must not vanish, or
> the next person writes it again. `t.Skip` it with a reason naming the issue, and `tools/testreport`
> names it in the **NOT RUN** section of every run, green ones included. Getting these back to a
> real green is later work, done deliberately. The full rule is in `PRINCIPLES.md` §7. First two
> applied 2026-08-06 in `internal/keeper`.

- **`hk-hs4b0`** — `make full` runs a crash tier containing zero tests and always exits 0. `make full`
  is the merge decision.
- **`hk-97gcz`** — a whole tier of daemon failures is invisible: 8 failures under the scenario build
  tag, not the 1 believed.
- **`hk-ynohn`** — half the scenario tier silently skips in CI, and every skip reads as a pass.
- **`hk-q2r9q`** — 46 of 50 daemon work-loop fixtures omit the disk stub; 45 go loudly red and **8 go
  silently vacuous**.
- **`hk-oxbv9`** — the scenario scorer cannot tell an incomplete event log from a complete one. This
  is the assessor's own instrument.
- **`hk-mzwex`** — a typo in the diff ref makes **both halves of the review gate pass**.
- **`hk-2h2fa`** — the review gate approved a 1,412-line deletion that orphaned two subsystems and
  called it a clean diff.
- **`hk-7bfqe`** — `specs/assessor-handoff-schema.md` contradicts itself: §9 says version 3 is
  current, §4 and §8 say a handoff must equal 2. **This is the assessor's own contract.** Cheap.

### A3. The run does not survive its own lifecycle

- **`hk-hsp9e`** (P0) — both halves of hang handling are dead: nothing kills a hung agent, and nothing
  shows one as hung.
- **`hk-8uznx`** (P0) — a frozen agent is never killed; the freeze-kill events have no producer.
- **`hk-97jco`** — a graceful daemon shutdown does not let a run survive; only a SIGKILL does. The
  assessor will stop and start its scratch daemon.
- **`hk-b4xf2`** — the lifecycle terminal transition runs only in single mode, so **the default path
  is inert.** Graph is the only path, which makes this the live one.
- **`hk-rr1dy`** — handler-state schema split: the daemon writes version 2, the CLI accepts only
  version 1. The operator cannot read the daemon's own state.

### A4. The test bed corrupts itself or the machine

These matter more than usual because lanes run beside the assessor on one box.

- **`hk-ttri2`** — a daemon test run on a low-disk box runs `go clean -cache` and can corrupt every
  other build on the machine.
- **`hk-ts5wp`** — daemon tests leak production daemons that outlive the run, keep firing the cache
  reaper, and wipe the shared build cache during later runs. Contaminates every flake measurement.
- **`hk-c6dt2`** — the orphan sweep kills **other projects'** `br` processes: it matches on the
  process name with no project scoping.
- **`hk-59flr`, `hk-hqttl`, `hk-fr7ht`** — the daemon suite goes red under load with a different test
  each time, and each passes alone.

  > **Do not open another investigation into this — operator, 2026-08-06.** It has been looked at
  > four or five times and re-raising it is not progress. **The ruling narrows the question instead:
  > the only thing that has to work is the core queue.** Tests outside that do not have to run, and
  > can be ignored or disabled. Scope the suite to the core queue and judge on that. A flake in a
  > package the queue does not depend on stops being a blocker by definition rather than by
  > investigation.

### A5. Cheap safety, do them while you are in there

- **`hk-ifj6p`** — an unknown first token falls through and **boots a daemon against the current
  directory** instead of being refused.
- **`hk-brl9q`** — a missing branching config silently empties the protected-branch list *and*
  disables the guard that would report it. Pairs with `hk-uwhrb`.
- **`hk-6c85b`** — six daemon functions survive deletion, including the gate that stops an agent
  launching unsandboxed.
- **`hk-7ue65`, `hk-zusgg`** — two freeze gates pin files that the extraction removed.

---

## List B — fix while the assessor works, or before major daemon work resumes

Real, mostly serious, and not on the path between a local scratch run and an honest verdict.

**Sandbox — moved here 2026-08-06 because sandboxing is off for the assessor:**
`hk-quoka` (a sandboxed run gets zero egress: the allowed-domain list is operator-supplied and
production sets none, so the harness cannot reach its own model endpoint) · `hk-bzydx` (a sandboxed
run cannot reach `~/.claude` or `$CODEX_HOME`, so it fails at init) · `hk-mp37h`, `hk-scaj0` (the
per-bead sandbox switch and the uniform-sandbox epic). **These become blocking the moment sandboxing
is turned on.**

**Not exercised by a local, single-concurrency, local-only audit:**
`hk-uaka2` and `hk-t8myp` (the wrapped worktree probe and the remote shutdown drain — remote path
only, though `hk-uaka2` remains the named first blocker for distributed execution and should not
drift) · `hk-sbd4l` (a gate passing on a flapping ssh link) · `hk-6n0z7`, `hk-lprn7` (remote graph
runs) · `hk-71joj`, `hk-j63y6`, `hk-zjwke` (concurrency; the assessor runs at concurrency one) ·
`hk-o7x4w` (the orphan-handler sweep is a no-op on macOS).

**Correctness that shows up after the audit window:**
`hk-1a7yb` (a bead closes because some commit named it, and the check asks a branch no configuration
can change) · `hk-uwhrb`, `hk-q6fwh` (branch defaults falling back to the literal `main`) ·
`hk-zeox3` (every event subscription is live-only, so a restarting captain misses what completed) ·
`hk-1dwk2` (four of six subscribe clients read a daemon refusal as success) · the queue durability
set `hk-7t615`, `hk-c8mvo`, `hk-xhmf1`, `hk-jvrsv`, `hk-mk4cl`, `hk-e91d8` — **bravo's lane; some may
promote to List A if the assessor's audit drives the queue hard.**

**Test honesty, no runtime effect:**
`hk-9kkpp`, `hk-61urw` (assertions on events the package cannot emit) · `hk-1kv4l` (the Guarding
state is unreached, already recorded in the spec) · `hk-qjxvq` (per-function zero-coverage
reporting).

**Housekeeping and process:**
`hk-6lt60` (P0 by label, but it is a kerf-process defect, not a runtime one) · `hk-f1wb0`,
`hk-99szy`, `hk-gxy67`, `hk-7yd1u` (disk and cache) · `hk-7zzk7` (a bare `br` in a worktree forks the
ledger — real, and the working rule is already written down) · `hk-4pulw` (comms sender is
unauthenticated) · `hk-rlvhi`, `hk-xbrc2`, `hk-j7yo0` (stale binary and wrong-verb messages) ·
`hk-qbsha` (the review-loop vocabulary; the code pass landed, specs remain).

**Close rather than fix:** `hk-main-required-check-red-i14hq`. The operator ruled on 2026-08-06 that
`main` needs nothing until the merge at the end. Nothing should judge this branch's work against it.

---

## What has to happen before this file can be trusted as a plan

1. ~~**Define the core-queue test scope.**~~ **DONE 2026-08-06: `make core`.** It runs `CORE_PKGS` in
   the `Makefile`, which is `CHARTER.md` §3's decided pipeline — config, event bus, queue, bead-ledger
   adapter, worktrees, harness registry and one substrate, work loop, merge — resolved to packages in
   the charter's own order. Nothing was added to the set; §3 says anything absent is deferred by
   default rather than by argument. Keeper, crew, captain, dashboard, sentinel, subscribe, the socket
   listener and the second and third substrates are all outside it, by that section.

   **`make core` is what "the build works" means for this sign-off.** `make full` stays the merge
   decision and stays whole-tree. A green `core` beside a red `full` is a real answer, not a
   contradiction: the tool does its job and something outside the core does not.
2. **Triage the remaining ~40 P1 issues against the criterion at the top.** List A is a floor.

Sandboxing is settled: it is off, and the two sandbox items moved to List B.
