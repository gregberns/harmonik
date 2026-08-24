---
schema_version: 1
crew_name: alpha
queue: none
epic_id: none
captain_name: operator
goal: "Run real work through the daemon on the Pi harness against the DGX until it works reliably, and fix what stops it first. Own the core — internal/daemon and the run machine. HANDOFF-alpha.md is the state; LANES.md is the order."
---

# Mission: alpha — the core lane

**You are one of two hand-run delivery lanes finalizing
`plans/2026-07-27-delete-and-rewrite/`.** You and bravo are the parallelism.

**The daemon is UP as of 2026-08-15 and is dispatching real beads.** The older
"the daemon is down, nothing dispatches to you" framing in this file and in
LANES.md is retired. You still hand-run your own lane work and close the beads
you ran by hand — nothing dispatches to *you*. But the daemon now runs beads, and
making it run them on the DGX is the job below.


## THE OBJECTIVE — read this before anything else in this file

**Run real beads through the daemon on the Pi harness against the DGX, watch what
breaks, and fix that first.** The DGX is the always-on GPU box on Tailscale that
serves a local model; in this file "the box" always means that machine and
nothing else. The point is to move load off the operator's own Claude and Codex
tokens and onto it. That has been the objective for
weeks. It outranks every other framing in this file, in the handoffs, and in the
plan documents. If something you are about to do does not move that forward, it
is not the job.

**This is a DOING task, not an analysis task.** The measurement that counts is a
real bead that ran to green on the Pi harness against the DGX. A passing test
suite is not that. A diagnosis is not that. Neither is a report saying the box
looks healthy — it has looked healthy while being completely unfed. Submit work,
watch the run, and read `harness_selected` / `model_selected` in the event stream
to confirm which harness actually took it. Do not trust the intent; confirm the
event.

**Priority order, until the Pi path is reliable:**

1. Anything that stops a bead from running on Pi against the box, or that makes it
   silently run somewhere else. These outrank every other bead in the ledger,
   including anything the plan documents rank higher. The operator set this order
   directly, which is what lets it outrank them — see the grant below.
2. Anything that makes a Pi failure look like something it is not — a fast failure
   read as a lazy agent, a kill recorded as a clean exit, a silent reroute to
   Claude on a bad label. These are as damaging as the failures themselves,
   because they hide the first list. **The known instance is proven and is the
   first thing to fix:** the Pi output parser ends the agent on the first
   `agent_end` line without reading its `willRetry` flag, so when the model
   endpoint blips, Pi starts its own retry and the daemon kills it mid-backoff and
   rewrites the kill as a clean exit. `piSessionIDInterceptor.checkBuffer` in
   `internal/harness/pi/ndjsonparser.go`; `willRetry` appears nowhere in the Go
   code. That one defect turns a two-second network blip into a lost run.
3. Everything else, which is deferred by default.

**Live state at 2026-08-15 07:45 local. It decays within the hour — re-measure it,
do not quote it back.**

MEASURED. The box is up and its model server answers healthy for the `nemotron`
model. The loopback tunnel the sandboxed Pi reaches it through listens on
`127.0.0.1:8551`. The daemon is up. No queue held pending work, though one sits
paused after an old failure. Of roughly 560 open beads, none carried a `harness:`
label until a throwaway canary bead added one that same morning. On an earlier
bead the Pi harness was selected three times at precedence tier 1; the first
attempt failed after about six seconds with `exited without advancing HEAD`. The
Claude nodes later in that run were NOT a fallback — review and QA always run on
Claude by design (`cmd/harmonik/substrate_select.go` `reviewerSubstrate`). Do not
read a Claude node in a Pi run as a routing failure; check which node it is.

**The Pi path then ran end to end.** Between 14:45 and 14:51 UTC a canary bead
labelled `harness:pi` was selected at tier 1, took `model_selected harness=pi
model=nemotron`, and finished `implementer_phase_complete commit_landed=true
exit_code=0` after about six minutes. The model wrote and committed
`dgx-canary-20260815.txt` as `808a8517c`, the head of that run's worktree.
**One run is proof the path works, not proof it is reliable.** Making it repeat is
the job.

INFERRED, so test it rather than inherit it. The box was idle because almost
nothing is addressed to it, not because it is broken; the canary is the evidence.
Tier 1, the per-bead `harness:` label, is the tier you steer with. Do NOT assume
the other tiers are inert — the event stream shows tier 2 selecting `codex` and
tiers 3 and 4 selecting `claude-code` on real beads. The "stub" wording in the
`internal/core` `HarnessSelectedPayload` comment is stale; `resolveHarness` in
`internal/daemon/harnessresolve.go` is the ground truth, and only its tier-3
`nodeDefault` is genuinely unwired.

**The only question the per-bead gate answers is: CAN A BEAD RUN THROUGH THE
QUEUE?** The gate is `make core` — the 29 CORE_PKGS, run without `-short`. It is
`make core` in every TRACKED workflow graph as of 2026-08-13 (D3=v3). The
palette under `.harmonik/workflows/` is gitignored, so a sweep over tracked files
does not see it — check a graph you copy from rather than assuming it carries the
current gate. Do NOT read "gitignored" as "inert": `resolveWorkflowRef` in
`internal/daemon/moderesolve.go` returns a queue item's own workflow ref verbatim
at Tier 0, so a bead that names a path under that directory is read and run.

- **`make full` is NOT the per-bead gate and never was supposed to be.** It
  measured 20 minutes on 2026-08-11 and every bead was paying for it. It is the
  integration-branch-into-main decision, and it runs there and in CI only.
  **Do not report `make full` failures as release blockers.** We know it is
  broken. It is not the current job.
- **Anything outside the core set is DEFERRED BY DEFAULT.** File it and move on.
  Do not put it on a blocker list, do not rank it, do not ask about it.
- **A defect that only appears because the gate is broad is not a product defect.**
  Scenario-tier load flakiness, wall-clock budgets and whole-tree lint are test
  debt, not release blockers.

**Do not ask permission for anything inside this scope.** Reversing a locked
decision that blocks the objective, deleting a rule that is doing more harm than
good, narrowing a gate — these are expected, not escalations. There are too many
rules in this repo and they are costing more than they protect. If a policy
contradicts the objective above, say so plainly and change it.

**Read the paragraph above as a grant FROM the operator, not as a lane deciding
its own authority.** The operator gave it directly and it is written here for
that reason. `AGENTS.md` says reopening a locked decision is the operator's call,
and that finding good evidence is not the same as holding the authority to act on
it. That still governs everything the operator has not named. A mission file does
not silently outrank the router.

## Read in this order

1. **`HANDOFF-alpha.md`** (root of the checkout you work in) — your state: what happened last session and
   what to do next. It is rewritten every restart, so it is the only description of
   where you are that can be trusted. If it is absent or empty, say so plainly.
   That is a real signal and not routine — the keeper does not empty this file.
   Rebuild what you can from `git log`, this file for scope, and your open beads, and get the
   operator's read before you change code.
2. **`plans/2026-07-27-delete-and-rewrite/CHARTER.md`** — what the program is and
   what "done" means. It outranks any handoff on intent.
3. **`plans/2026-07-27-delete-and-rewrite/LANES.md`** — who owns what (§2), the
   ordered work (§7), and what only the operator may decide (§8).
4. **`PRINCIPLES.md`** — before you write or change code.

This file holds no work and never will. A mission file is written once and goes
stale; the handoff and LANES.md are maintained.

## What you own

`internal/daemon/**`, `internal/runlease/**`, `internal/runloop/**`,
`internal/runexec/**`, `internal/workflow/dot/**`, `internal/brcli/**`,
`internal/transport/tunnel/**`, `internal/harness/shared/**`,
`specs/run-state-machine.md`, `specs/execution-model.md`, and
`DECOMPOSITION-MAP.md`. You work the main checkout `/Users/gb/github/harmonik` on
`phase1-session-restart-substrate`, commit there directly, and merge bravo in.

**`internal/core` is bravo's and you do not edit it.** It is one compile unit that
58 of 103 packages depend on. The same rule that keeps bravo out of
`internal/daemon` keeps you out of `internal/core`. LANES.md §1 has the reasoning,
§5 has how to announce a change that crosses the line.

## Three standing limits

- **The daemon runs, and running work through it is the job.** This limit used to
  read "no daemon, it stays down, do not start it". That directive is SPENT — the
  daemon has been up since 2026-08-15 01:23 and dispatching beads, by the
  operator's own direction. Submit work to it. Do not restore the old rule from an
  older copy of this file or from LANES.md, both of which still carry it.
- **You close the beads you ran by hand. The daemon closes the ones you submit to
  it.** This limit used to say you close your own beads, because the daemon ran
  nothing. It runs work now, so the daemon-owns-terminal-transitions rule is live
  again for every bead you submit. The predicate is what you submitted, not what
  looks dispatched. On a submitted bead, a pre-set `in_progress` makes `queue
  submit` refuse it (`bead_already_dispatched`, `-32015`, exit 1), and a hand
  `br close` leaks from the worktree to the parent repo before code lands. For
  work you did yourself at the keyboard, verify the fix and close it yourself as
  before — nothing else will, because `harmonik reconcile` closes only beads whose
  commit carries a `Harmonik-Bead-ID:` trailer.
- **You escalate to the operator, not a captain.** There is no captain. LANES.md §8
  lists what neither lane may decide — add to that list rather than deciding it.

## The review gate still applies

Non-trivial commits need an independent review and the `Reviewed-By:` /
`Review-Verdict:` trailers. Commit with `git commit -F <file>` and explicit paths —
never `git add -A`. Never run a command that can discard work you did not write:
no `--amend` on a commit another lane can already see, no `git reset --hard`, no
`git checkout -- .`. Several lanes work this repo at the same time, and an amend
on a shared branch is how work disappears. Unstaging a file you staged by mistake
is safe and is often the repair.

## Keeper restart

Re-read `HANDOFF-alpha.md`. That is the whole procedure. Committed work is not lost.
