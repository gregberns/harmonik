# Shift-Left Quality Process — Two Paired Lanes

**Date:** 2026-07-05 · **Author:** Staff Eng (design pass) · **Status:** DRAFT for operator review
**Companion to:** `plans/2026-07-04-quality-loop/PLAN.md` (the un-suppression plan). That plan fixes the
*fabricated green*; this one builds quality in *earlier* so fewer red runs happen in the first place.

---

## Framing: the operator's actual goal

The A/B/C "prove a test goes red→green" experiment is a **post-bug** artifact — it certifies a fix after a
bug is known. The operator wants the opposite: **prevent bugs earlier**. Two mechanisms, chosen by change
size, deliver that:

- **Lane 1 (heavy, blocking, batched):** epics live on a branch and clear a real acceptance gate — expected
  outcomes authored up front, thorough test + review + *live break-testing on an isolated daemon* at epic
  end — before anything reaches `main`.
- **Lane 2 (light, async, per-merge):** small merges hit `main` immediately, and an out-of-band job tests
  them *after* the fact, filing any regression back as a bead/comms alert. No 10–20 min block.

The two lanes are a **routing decision made once per change**, described in §3. Both feed the same ledger
(beads) and the same bus (`harmonik comms`), so findings from either lane are actionable the same way.

---

## Lane 1 — Epic-on-a-branch with a real acceptance gate

### 1.1 Lifecycle (where every gate/bead sits)

```
kerf plan ──► kerf impl:breakdown ──► [dispatch beads onto EPIC BRANCH] ──► epic-end acceptance gate ──► PR to main
   │                │                          │                                   │
   │                │                          │                        ┌──────────┴───────────┐
 spec +      expected-outcomes            daemon queue with           blocking beads (must all
 acceptance  authored as ACCEPTANCE       --queue <epic> merges       be CLOSED before PR):
 criteria    beads (NOT by implementer)   bead→branch, not→main        AT / CR / LT / XT
```

**Branch model.** An epic gets a long-lived integration branch `epic/<codename>` (node/task/integration
model, already locked — `project_harmonik_branching_model`). Beads dispatched with
`harmonik queue submit --queue <codename> --target-branch epic/<codename>` merge worker worktrees into the
epic branch, **never into `main` bead-by-bead**. `main` is reached only once, via a single human PR at
epic end. This is the concrete change to today's flow, where the daemon promotes each reviewed bead toward
`main` continuously.

### 1.2 Expected outcomes authored up front (the "acceptance beads")

Today the kerf impl jig (`~/.kerf/jigs/impl.md`, Pass 1 Breakdown) **already** mandates two test beads per
epic — a `scenario:` bead and an `explore:` bead — and forbids the work closing until they're closed. We
extend that from two beads to a **four-bead acceptance block**, and move authorship off the implementer:

| Bead | Title convention | Author | When |
|------|------------------|--------|------|
| **AT — Acceptance criteria** | `accept: <codename> — expected outcomes` | **Planner** (kerf `plan` jig) + captain review | at plan time, before breakdown |
| **CR — Full code review** | `review: <codename>` | independent reviewer agent (fresh context, worktree-isolated) | epic end |
| **LT — Live break-test (verify AT)** | `livetest: <codename> — verify expected outcomes` | scratch-daemon harness | epic end |
| **XT — Exploratory break-test** | `explore: <codename> — adversarial` | 3–5 adversarial agents | epic end |

**Who authors AT, and why not the implementer.** Expected outcomes are written by the **planning agent**
during `kerf plan` (they fall out of the spec's acceptance-criteria section, which the plan jig already
produces in `07-tasks.md`) and are **ratified by the captain** when the epic is staffed. This is the
shift-left move: the *definition of done* is a contract fixed **before** a single line is written, by
someone other than the person who will write it — closing the `03-verify.md` self-grading hole (PLAN.md
suppression point #8). AT is encoded as a bead whose description is a checklist of concrete, observable
outcomes ("submitting bead X to queue Y produces run_completed with verdict APPROVE and a merge commit on
`epic/<codename>`"), each line a testable assertion. AT is a **dependency of** LT (you cannot verify
outcomes that were never written down).

### 1.3 Live testing on an INDEPENDENT daemon (never stop production)

The operator's hard constraint — **never stop the fleet daemon to test an epic** — is already satisfiable:
`scripts/scratch-daemon.sh` exists and is designed for exactly this. The isolation model:

- **Separate clone.** `scratch-daemon.sh init <scratch-path>` clones the repo (from `origin` by default)
  into a path *outside* the fleet checkout. `guard_path` hard-refuses to operate on the fleet repo root or
  `/`, and `assert_not_supervised` refuses any path that has a live `hk-<hash>-supervise` session — so the
  harness *cannot* target production.
- **Separate socket / pidfile / tmux / binary.** Every handle is keyed off the scratch path:
  socket `<scratch>/.harmonik/daemon.sock`, pidfile `<scratch>/.harmonik/daemon.pid`, tmux session
  `harmonik-<projecthash>-default` (projecthash = SHA-256 of the scratch realpath, PL-006a), and a binary
  built *from the scratch clone*. A second daemon on a different path can never collide with the fleet.
- **Separate worktrees.** Because the scratch daemon operates on its own clone, all worker worktrees it
  creates live under the scratch `.harmonik/`, isolated from fleet worktrees.
- **The epic branch is what gets tested.** `scratch-daemon.sh init <scratch> <epic-branch-remote>` (or
  `git -C <scratch> fetch && checkout epic/<codename>`) stands the scratch daemon up on the *epic branch*,
  not `main`. LT then runs the real workflow end-to-end against the code the epic actually produced.

**LT mechanics (verify the specific expected outcomes).** LT reuses `scratch-daemon.sh batch`:

```
scratch-daemon.sh cycle  <scratch>                       # down → rebuild epic branch → up
scratch-daemon.sh batch  <scratch> <codename> --beads <AT-derived corpus>
```

`batch` arms a `subscribe` reader, submits the corpus, waits for every bead to reach a terminal transition
(`SCRATCH_BATCH_TIMEOUT`, default 1800s), and emits a structured pass/fail JSON keyed by bead with a
`verdict` + `fail_signature`. The LT bead's corpus is **the AT checklist turned into runnable workflows** —
each expected-outcome line becomes a bead/queue-file the scratch daemon actually executes. LT is GREEN iff
every AT assertion is observed in the event stream. This is the "(a) verify specific expected outcomes"
half of the operator's constraint.

**XT mechanics (exploratory — actively try to break it).** The "(b) find unanticipated issues" half is a
**structured adversarial pass**, not free-form poking. 3–5 agents are fanned out (reuse the
`major-issue-fanout` skill's fan-out discipline) against the *same running scratch daemon*, each on a
distinct break-vector, e.g.:

1. **Concurrency/race** — co-dispatch same-file beads, saturate `--max-concurrent`, force merge races.
2. **Malformed input** — bad queue files, cyclic bead deps, beads referencing missing files.
3. **Lifecycle interruption** — kill workers mid-merge, SIGTERM the scratch daemon mid-batch, restart.
4. **Boundary/scale** — empty queue, 1-bead queue, oversized batch, long-running bead vs timeout.
5. **Operator-surface abuse** — every CLI flag combination a human might actually type wrong.

Each XT agent files **every** anomaly it finds as a fresh bead labelled `codename:<codename>` +
`found-by:exploratory`, using `scratch-daemon.sh feedback <results-json>` where possible (it dedupes via a
`prov:<hash>` label so re-runs update rather than duplicate). **XT findings become part of the epic's
backlog**: P0/P1 findings block the PR (new beads added to the epic branch); P2+ findings are triaged by
the captain into "fix now" vs "file for later." This closes the loop — exploratory testing doesn't just
produce a report, it produces *ledger work* that shifts the next iteration left.

### 1.4 What is blocking vs async in Lane 1

- **Blocking (gate the PR to `main`):** AT closed, CR = APPROVE, LT green, XT complete with no open
  P0/P1 finding. The single human `epic/<codename> → main` PR is the enforcement point. This is where the
  10–20 min (or longer) cost is *acceptable* — it's paid **once per epic**, not per merge.
- **Async within the epic:** individual bead dispatch onto the epic branch runs the normal review-loop; the
  heavy gate is deferred to epic end so mid-epic iteration stays fast.

### 1.5 Smallest first implementation increment (Lane 1)

1. **Add the AT + XT beads to the kerf impl jig's Pass-1 acceptance block** (extend the existing two-bead
   requirement in `~/.kerf/jigs/impl.md` to four; add authorship = planner/captain for AT). Pure jig-spec
   edit — no code.
2. **Add `--target-branch` honoring to `queue submit`** so an epic's beads merge to `epic/<codename>` not
   `main` (verify whether the daemon already supports a per-queue target; if so this is config only).
3. **Write `scripts/epic-livetest.sh`** = thin wrapper: `fetch epic branch → scratch-daemon cycle → batch
   (AT corpus) → feedback → comms-send verdict to captain`. This is ~40 lines over primitives that already
   exist.

That increment makes *one* epic go through the full gate manually; hardening/automation follows.

---

## Lane 2 — Fast-follow for the small-merge path

### 2.1 The problem it solves

Most changes are one-file fixes, cross-reference repairs, wording closes — "just need to get done." A
blocking 10–20 min build on each is a non-starter (operator-stated). But un-tested small merges are exactly
how the fabricated-green regressions crept in. Fast-follow makes the test **async**: the merge is not
blocked, but it is *not un-tested* — it's tested moments later, out of band, and any failure is filed back.

### 2.2 Trigger

A **post-merge-to-`main`** event. Two viable triggers, in preference order:

1. **Scheduler-driven (recommended first increment).** `internal/schedule` already ships a generic
   recurring-job primitive with `ScheduleKindEvery` (`every@<dur>`) and `ActionKindCommand` /
   `ActionKindCommsSend`. Seed **one** job, `every@15m`, action = run `scripts/fast-follow.sh`. The script
   diffs `main` since its last recorded SHA; if `main` advanced, it tests the delta. `OverlapPolicySkip`
   (the default) prevents a slow run from stacking. This needs **zero new event plumbing** — it reuses a
   built, tested scheduler and the same seeding pattern as the goal-keeper.
2. **Merge-event-driven (later).** Subscribe to the daemon's merge/promote event and fire per-merge. More
   responsive, but requires wiring a new subscriber; defer until the scheduled version proves the loop.

### 2.3 What runs

`fast-follow.sh`, against the **fleet checkout is forbidden** — it runs on the **scratch daemon / scratch
clone**, same isolation model as Lane 1, so it never contends with production worktrees or CPU during a
fleet dispatch. Steps:

1. `git -C <scratch> fetch && checkout main && reset --hard origin/main` (pull the just-merged code).
2. `scratch-daemon.sh build <scratch>` (rebuild).
3. Run the **fast tier** only — NOT the full 10–20 min corpus. The fast tier = the unit/short suite for the
   changed packages (`go test -short ./<changed-pkgs>/...`) + the one real end-to-end **`harmonik smoke`**
   (today orphaned — PLAN.md suppression #11 — fast-follow gives it a home). Target < 3 min wall.
4. On failure: `scratch-daemon.sh feedback <results-json>` files a **deduped bead** (`prov:<hash>` so the
   same regression doesn't spawn duplicates) labelled `found-by:fast-follow`, priority P1, **and**
   `harmonik comms send --to captain --topic quality` with the fail signature + offending SHA range.

### 2.4 How failures surface and get actioned

- **Bead:** appears in `kerf next` / `br ready` as P1 with the offending SHA range in its description →
  gets scheduled like any other fix, by whoever owns the next dispatch loop.
- **Comms:** the captain's `subscribe`/watch picks up the `quality` topic and, per the watch contract,
  escalates only if actionable (a real red, not a known-flaky). The captain does **not** stop the fleet;
  it dispatches a fix-up bead. If the same signature recurs ≥2× the `major-issue-fanout` trigger applies.

### 2.5 Coexistence with Lane 1 — the routing rule (decided ONCE per change)

```
Is the change a NEW subsystem, a cross-subsystem refactor, or a cross-cutting contract?
  ├─ YES → kerf epic → Lane 1 (epic branch + full acceptance gate)         [the kerf "create a work" rule]
  └─ NO  → small merge straight to main → Lane 2 fast-follow tests it async
```

This is **the same threshold kerf already uses** to decide "spin up a work vs skip it" (AGENTS.md:
"create a kerf work before new subsystems / cross-subsystem refactors / cross-cutting contracts; trivial
changes skip it"). So routing adds **no new decision** — if it's big enough to warrant a kerf work, it's an
epic and takes Lane 1; if it's small enough to skip kerf, it takes Lane 2. A Lane-2 change whose
fast-follow keeps going red, or that fans out into many follow-ups, is *evidence it was mis-routed* and
should be promoted to an epic — the captain makes that call on the ≥2×-recurrence signal.

### 2.6 Smallest first implementation increment (Lane 2)

1. **Write `scripts/fast-follow.sh`** (last-tested-SHA file + changed-package `go test -short` +
   `harmonik smoke` on the scratch clone + `feedback` + `comms send`). ~60 lines over existing primitives.
2. **Seed one `every@15m` schedule job** with `ActionKindCommand → fast-follow.sh` (reuse the goal-keeper
   seeding pattern; `internal/schedule` store + daemon tick already execute it).
3. **Point `harmonik smoke` at the scratch socket** so it exercises a real daemon, giving the orphaned
   smoke test its first standing invocation.

That increment gives every small merge an automatic, non-blocking test within 15 min, with red results
landing as beads + captain pings — no fleet stall, no per-merge wait.

---

## Summary table — where each gate lives

| | Lane 1 (epic) | Lane 2 (small merge) |
|---|---|---|
| **Routing** | new subsystem / cross-cutting / kerf work | trivial / kerf-skip |
| **Merge target** | `epic/<codename>` branch, then 1 human PR to `main` | `main` immediately |
| **Test timing** | epic end, **blocking** the PR | post-merge, **async** (≤15 min) |
| **Expected outcomes** | AT beads authored by planner+captain up front | n/a (delta-tested) |
| **Isolated daemon** | scratch-daemon on epic branch (LT + XT) | scratch-daemon on `main` delta |
| **Findings → ** | new beads on epic branch (block PR if P0/P1) + comms | deduped P1 bead + captain comms |
| **Cost** | high, paid once per epic | low, off critical path |
| **Primitives reused** | kerf impl jig acceptance block, scratch-daemon batch/feedback, major-issue-fanout | internal/schedule every@15m, scratch-daemon feedback, harmonik smoke, comms |

## Dependencies on the companion plan

Lane 2's fast tier is only meaningful once `harmonik smoke` and the short suite actually **fail loudly** —
so `plans/2026-07-04-quality-loop/PLAN.md` Phase 0/1 (un-suppress + fix hidden failures) is a **prerequisite**
for Lane 2 producing trustworthy reds. Lane 1's LT/XT are independent of that (they run their own scratch
daemon and read the event stream directly, not CI's fabricated green) and can start immediately — the
scratch harness and scheduler are already shipped (PLAN.md Phase 5: "~80% already built").
