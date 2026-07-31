# Step 6 — resource leases: the measured map, and what it changes about the step

Measured 2026-07-31 against `8741d0272`. Three independent mapping passes, one per pair of
resources. Every claim below that the step depends on was re-verified against the code by hand
after the passes returned. Claims that did not survive that check are marked and corrected.

`beadRunOne` in `internal/daemon/workloop.go` is 1,603 lines. Step 5 removed 173 lines from
`beadRunOne`. The decomposition map credits Step 5 with 161, which is the net of Step 5 and the
socket-path-check commit that landed after it and added 12 lines back. Do not read 161 as a
measurement of Step 5.

---

## 1. The step says six resources. There are nine, in three lifetimes

The plan's C2 list is "worker slot, tunnel, worktree, tmux session, hook session, spawn token".
Three of those six are really two things each, and the six do not share a lifetime. A single scope
closed in reverse order — the shape the step proposes — cannot hold them.

**Per-run, acquired and released inside `beadRunOne`:**

| Resource | Acquire | Release | Idempotent today |
|---|---|---|---|
| Worker slot | the scheduler's dispatch loop, handed in as a parameter | top-level defer | no — saturates at zero instead |
| Local in-flight counter | the scheduler's dispatch loop | top-level defer, plus a mid-function correction | no, and **no floor** |
| Tunnel port reservation | `tunnel.AllocatePort` | `tunnel.ReleasePort` | yes — a map delete |
| Tunnel process | `tunnel.ReverseTunnelRunner` | defer: kill then wait | no guard. Safe only because there is one caller |
| Worktree | `WorktreePort.Create` inside the merge exclusion domain | one defer carrying two skip predicates | no guard. Errors are logged, not returned |

**Per-launch, nested inside the run, and there are N of these in a graph run:**

| Resource | Acquire | Release | Idempotent today |
|---|---|---|---|
| Hook session | `RegisterHookSession` in `runAgentLaunch` | four sites, **none deferred** | yes, but only by a second map delete |
| Tmux session | `spawnWindowVia`, or `SpawnRunSession` | `Cleanup` then `ForceTeardownSession` | yes — two `sync.Once` |
| Substrate spawn slot | `acquireSpawnSlot` | inside the session's `Kill` | yes, via the kill's `sync.Once` |

**Per-run, but only for a remote single-implementer run:**

| Resource | Acquire | Release | Idempotent today |
|---|---|---|---|
| Cold-start token | a daemon-global channel of capacity 3 | `sync.Once` plus a deferred backstop | **yes** |

The cold-start token is the best-behaved of the nine and is the model the other eight should be
written to. It is also the only one with **no test at all**.

**What "the spawn token" hides.** The plan names one token. There are two, and they never overlap:
a daemon-global cold-start channel that gates remote runs, and the tmux substrate's own spawn cap
that gates local ones. Different owners, different release mechanics, no coordination. The first is
skipped for local runs. The second is skipped for remote ones.

**The lifetime mismatch is the real difficulty.** A graph run with six nodes holds one tunnel and
one worktree while taking and releasing six hook sessions and six tmux sessions. "One scope, closed
in reverse" describes the per-run set only. The per-launch set needs its own nested scope, and the
step should say so.

---

## 2. Release is not a boolean per resource. It is one run-level outcome

The same condition — an independent session plus a cancelled context, which together mean "the
daemon is shutting down and this run is meant to outlive it" — reaches four places:

- **skip the release** for the worktree and the tmux session,
- **skip the release** for the run-registry record as well, so the next boot can find it,
- **return without reopening the bead**, so the next boot can adopt it,
- **and does not gate the hook session or the tunnel at all**, which are torn down regardless.

The first three agree on outcome. They do not agree on spelling. One condition is written three
different ways — two of the spellings 24 lines apart and the third 600 lines later — and one of
them carries a further condition the others do not. The hook session and the tunnel are then left
out entirely, so a surviving agent keeps a session it can no longer report through.

A per-resource `skip bool` reproduces that spread rather than removing it. The disposition has to be
**decided once for the run, as a value**, and the scope has to act on that value. That is the
pure-decision-then-effect shape the principles ask for, and it makes the incoherent combinations
unrepresentable rather than merely unwritten.

A second, independent skip retains the worktree when a Pi run fails, so an operator can read the
captured agent logs. It is set in one place inside the single-implementer tail, which means **a
graph-mode Pi run never retains anything**.

> **Settled by the operator, 2026-07-31: that behavior was not intended. Do not preserve it.** The
> asymmetry is a defect, not a decision. `runlease.Exit.EvidenceWorthKeeping` is mode-agnostic, so
> the migration resolves it by construction — a failed Pi run keeps its worktree whichever mode it
> ran in. Do not add a mode test to keep the old shape.

---

## 3. Only three ordering edges are hard

Release order today is the reverse of registration order, and it is correct. Most of it is free
choice. Three edges are load-bearing and break loudly if inverted:

1. **The tmux session must die before the worktree is removed.** Otherwise `git worktree remove
   --force` races a live process inside the directory and the run is misrecorded as having produced
   no commit.
2. **The tmux session must die before the Pi log capture runs**, because reading the session outcome
   blocks until the session is waited on.
3. **The Pi log capture must run before the worktree is removed**, because it writes into it.

Everything else — tunnel process before tunnel port, worker slot outermost — is sensible but not
enforced by anything.

**One mapping pass claimed the cold-start token breaks reverse order. It does not.** The token is
acquired after the worktree and released before it, which is correct reverse order. Checked by hand.

---

## 4. Four defects found while mapping. Recorded, and two belong in this step

**A doomed tunnel is spawned after port allocation fails.** When `AllocatePort` returns an error the
code logs it and falls through. It then spends an SSH round trip, builds tunnel arguments around
port zero, and starts a real `ssh -N -R` process that cannot work. Only afterwards does the
readiness gate fail the run. Nothing leaks — the deferred kill reaps the process — but two
acquisitions are spent on a run that is already lost. This is the same defect class that was fixed
one line above when the socket-path check was hoisted. **Fold into this step as its own commit.**

**Independent run sessions and crew sessions are created with no time bound.** The shared-window
path wraps creation in a 60-second bound with a goroutine backstop, precisely because an adapter can
ignore its context and a wedged tmux server otherwise blocks forever. The two sibling constructors
call the adapter directly. The step already requires that creation be externally bounded.
**Fold into this step as its own commit.**

**The survive-shutdown feature does not survive.** An independent run session is meant to outlive a
killed daemon so the next boot adopts the live agent. At boot the orphan sweep kills every tmux
session carrying the project prefix that is not in its exclusion set, with no liveness test. The
exclusion set holds the daemon's own session, the flywheel, the captain and live crews. Run sessions
match the prefix and are not excluded. The sweep runs 45 lines before the adoption pass, so the
session is dead before anything looks for it. The bead still recovers, because adoption then
classifies the run as dead and resets it — but the feature buys nothing, and the worktree it was
protecting leaks until the seven-day age prune. **Record it. Do not chase it inside this step.** It needs
a way to tell a live surviving session from a genuine orphan, which is its own piece of work.
Step 6 must not encode a survive guarantee the system does not actually have.

**The local in-flight counter has no floor.** One increment, two decrement sites 220 lines apart,
and no clamp. No double decrement is reachable today: the second site clears the flag that arms the
deferred one, the flag is never re-armed, and the site is a plain branch rather than a loop. So this
is a structural hazard, not a live defect. It is worth naming because the failure mode is silent —
a negative value permanently over-admits the dispatch gate. The worker registry's own release
clamps at zero instead, which is safer, but it means a double release hides as under-utilisation
rather than failing.

---

## 5. What the tests already defend, and the two holes

**Defended, and these are the guard rails for the whole step:** every refusal before launch takes
nothing — one test drives the real `beadRunOne` across four refusal kinds and asserts the worktree
factory never ran, the slot count returned to zero, and exactly one bead reopen. Another asserts
that resolving a run plan acquires nothing at all. The tunnel readiness probe's exact argument
vector is pinned, including a comment naming the existence-check regression it replaced. A live-tmux
test spawns a process that ignores termination signals and asserts no descendant survives the kill.

**Hole one: the cold-start token has no test.** Not acquired, not released, not released once, not
released on the ready-timeout path.

**Hole two: the entire survive-shutdown gate family is unpinned.** No test references the
independent-session flag, either adoption pass, or the registry write. That is both a risk for this
refactor and the reason the boot-sweep defeat above went unnoticed.

Both holes should be closed by the tests this step brings, not left for later.

---

## 6. Carry-forward facts that must survive verbatim

Confirmed present and correct at `8741d0272`:

- **The tunnel readiness gate is already a connectability probe.** It runs a TCP connect on the
  worker as the worker user and polls until it succeeds. It does not stat. The plan lists this as a
  risk to get right. It is in fact a thing not to regress.
- **The socket path-length check is already hoisted above every acquisition made inside
  `beadRunOne`.** The worker slot is not below it — the dispatch loop reserves that before the run
  function is entered at all. A second copy of the check still sits inside the tunnel block, because
  the run plan can only cover a run whose worker was chosen before the plan ran. The two can never
  both fire. The duplicated refusal reporting — log line, event, bead reopen, written out twice — is
  a collapse target for this step.
- **The kill that reaps a local agent targets the process group and then polls the group.** That is
  load-bearing. Polling only the group leader returns early and skips the kill that actually reaps
  the orphan. **The remote equivalent signals a bare process id and does not re-resolve the pane**,
  and it is the only reaper for a worker-side agent. Across `internal/daemon` and
  `internal/lifecycle` there are nine kill sites and one of them satisfies the group-kill-and-probe
  rule.

---

## 7. Shape of the work

In order. Each is one commit, each independently reviewable.

1. ✅ **Landed 2026-07-31.** The lease and scope types, pure, with the disposition as a value
   rather than a set of predicates. No wiring. Tests first. → `internal/runlease`, fenced by
   depguard to the standard library and itself, and made normative as
   `specs/run-state-machine.md` §4a (RSM-036 … RSM-038). Read §8 below before wiring it.
2. ✅ **Landed 2026-07-31.** Refuse the run when tunnel port allocation fails, instead of spawning
   a doomed tunnel.
3. ✅ **Landed 2026-07-31.** Bound the independent-session and crew-session constructors the way
   the shared-window path is bounded.
4. Migrate the per-run resources onto the scope, innermost first, one commit each.
5. Give the per-launch set its own nested scope, and collapse the duplicated tunnel refusal
   reporting into the one reporter the run plan already has.
6. Close the two test holes.

---

## 8. What the types decided, for whoever wires them

The package answers three questions the migration would otherwise re-open at each release site.

**The disposition is one of three values, not a set of flags.** `Reclaim` gives everything back.
`Survive` keeps the agent session, the worktree, the run registry record, the hook session and the
tunnel, and gives back the four accounting slots, which are this process's bookkeeping and not the
agent's. `RetainEvidence` keeps the worktree and nothing else. A caller cannot ask for "keep the
worktree but tear down the session" — the combination is unrepresentable rather than merely
unwritten, which is what §2 asked for.

**Two of the resources gain a keeper they did not have.** The hook session and the tunnel are in the
survive set. Today they are torn down regardless, which leaves a surviving agent holding a session it
can no longer report through. That is a behavior change and it arrives with the migration commit
that moves those two sites, not before.

**`Decide` is where the polarity lives.** Survival needs BOTH facts — an agent in its own session AND
a daemon that is stopping — and it wins over evidence when both apply. Eight input combinations, one
test.

`Survive` does not promise survival. §4 above says why, and both the package doc and the spec's §4a
say it in the two places a wiring agent will actually read.

**The scope enforces reverse-of-acquisition and nothing more, and that is less than §3 needs.** Two
of the three load-bearing ordering edges involve the Pi log capture, which is a step and not a
resource, so it cannot be a lease and the scope cannot order it. Those two edges stay obligations on
where the migrating code puts the capture relative to the acquisitions around it. This is the part
of RSM-038 most likely to be dropped in silence, because the other edge — session before worktree —
falls out of reverse order for free and makes the whole rule look automatic. It is not.

The step's stated size of ~450 lines is not defensible from this map and should not be quoted until
the first two commits have been measured.
