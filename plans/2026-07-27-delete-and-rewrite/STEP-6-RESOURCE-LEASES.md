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

> **Corrected 2026-07-31 while pinning this family: there are FIVE sites, not four, and the fifth
> fires first.** `WaitWithSocketGrace` in `internal/runloop/waitsocketgrace.go` ends its step 1 with
> a bare `else if ctx.Err() != nil { _ = sess.Kill(ctx) }`. The watcher is nil on the substrate
> path, which is the path a tmux-hosted independent session takes, so on shutdown this kills the
> session. It names no gate and has no equivalent of the two skip predicates. **The survive
> disposition is therefore defeated inside the daemon's own process**, before the boot sweep of §4
> is reached — the session is already dead when the daemon exits, so there is nothing left for the
> next boot to find. The authors came close to seeing it: the comment at the post-wait kill in
> `agentlaunch.go` calls this "a no-op kill inside the completion wait". It is not a no-op on a live
> agent. Filed as `hk-jyh5t`.

> **Also corrected: the tunnel entry above is unreachable, not a live leak.** No run holds both a
> tunnel and the survive condition. Three independent grounds, all checked:
>
> 1. Inside the `ConfigurePerRunSubstrate` closure, the `rbc != nil` arm returns before the
>    `useIndepSession = true` line in the `rbc == nil` arm.
> 2. DOT mode returns before single-mode `runAgentLaunch`, and only the work loop passes
>    `ConfigurePerRunSubstrate`.
> 3. The tunnel is an `exec.CommandContext` on the run context, so it dies with the run regardless.
>
> **Do not state this as "`beadRunOne` returns on the remote branch". That is false** — the remote
> block's returns are failure exits, and the remote happy path falls through and does reach
> `useIndepSession := false`. The wrong mechanism was written down once already, in the comment
> justifying the absent test, and a reader who checks it will find it does not match the code and
> may then distrust the correct conclusion.
>
> "Does not gate the tunnel at all" is literally true and reads as a leak. It is a hazard that
> arrives the day a remote run may keep its own session, not one to chase now. `runlease` keeps the
> tunnel in its survive set, which is the right shape for that day and costs nothing today.

A per-resource `skip bool` reproduces that spread rather than removing it. The disposition has to be
**decided once for the run, as a value**, and the scope has to act on that value. That is the
pure-decision-then-effect shape the principles ask for, and it makes the incoherent combinations
unrepresentable rather than merely unwritten.

A second, independent skip retains the worktree when a Pi run fails, so an operator can read the
captured agent logs. It is set in one place inside the single-implementer tail, which means **a
graph-mode Pi run never retains anything**.

> **Settled by the operator, 2026-07-31: that behavior was not intended. Do not preserve it.** The
> asymmetry is a defect, not a decision. Do not add a mode test to keep the old shape.
>
> **Corrected 2026-07-31 while migrating the worktree: it does NOT resolve by construction.** This
> section said `runlease.Exit.EvidenceWorthKeeping` is mode-agnostic and that the migration would
> therefore fix the asymmetry on its own. The FIELD is mode-agnostic. Its source is not. The run
> sets its Pi fact from the resolved launch artifacts in the single-mode tail, which is below the
> graph branch's return, so a graph run reports no evidence however the disposition is written.
> Meanwhile `runAgentLaunch` writes the capture for EVERY Pi launch and the graph cascade calls it
> per node, so a failed graph-mode Pi run does produce output and does have it deleted. Two
> independent readers reached the same conclusion. Fixing it needs the fact carried out of the
> cascade, which is its own commit — see §7 step 4a.

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
released on the ready-timeout path. **CLOSED 2026-07-31.**

**Hole two: the entire survive-shutdown gate family is unpinned.** No test references the
independent-session flag, either adoption pass, or the registry write. That is both a risk for this
refactor and the reason the boot-sweep defeat above went unnoticed. **CLOSED 2026-07-31.**

**Hole three: the tunnel port lease has no end-to-end test.** Opened by step 4 and named in its own
commit: the give-back moved onto the run's scope while the reservation set in
`internal/transport/tunnel` had no exported reader, so nothing outside that package could ask
whether a run gave its port back. The leak is silent — the set is process-global, nothing in
production reads it, and a kept port is simply never handed out again. **CLOSED 2026-07-31.** The
package gained `PortReserved`. Three drives of the real `beadRunOne` now assert the give-back on the
ordinary ending and on both refusals that sit below the allocation, and each pairs the free-port
claim with proof the run took the port first.

> Two things that came out of closing it. A production reader was preferred to a test-only seam
> because the test-only seam does not reach: an `export_test.go` compiles into its own package's
> test binary only, and the test that matters drives a whole run and therefore lives in
> `internal/daemon`. And one mutant nothing kills — put the give-back back on a bare `defer` and
> every test stays green, because no reachable run holds a tunnel port under any disposition but
> `Reclaim`. That is the same unreachability §2 records for the tunnel, so it is a fact about the
> system rather than a hole in the tests. It is written into
> `internal/daemon/tunnel_port_release_test.go` where someone about to simplify the lease away will
> find it.

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
4. ✅ **Landed 2026-07-31.** The per-run set is on the scope, innermost outward: the cold-start
   token, the run record, the worktree, the tunnel process, the tunnel port, the worker slot and
   the local in-flight count. One commit each. Two mutable flags and every per-site skip predicate
   are gone; the run reads one disposition.
4a. ✅ **Landed 2026-07-31.** The evidence fact is recorded at the LAUNCH, which is the one step
   both workflow modes pass through, so a failed graph-mode run keeps its captured output. It was
   NOT a consequence of step 4 — see §2's correction.
5. Give the per-launch set its own nested scope, and collapse the duplicated tunnel refusal
   reporting into the one reporter the run plan already has.
6. ✅ **Landed 2026-07-31, and moved AHEAD of steps 4 and 5 on purpose.** Close the two test holes.
   These are the guard rails for the migration, so pinning them after it would defend nothing. Both
   were closed before any release site moved, and both found things — see §9.

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


---

## 9. What closing the two test holes found — read this before writing the migration's tests

Both holes are closed. The tests are the guard rails for steps 4 and 5, so they went first. Three
things came out of writing them, and the first is the one that will bite again.

**A test that cannot fail looks exactly like a test that passes.** This happened three separate
times in one day, in three unrelated places, and each time it was caught by mutating the code rather
than by reading the test.

- Two cold-start tests passed with the acquisition deleted. "A free slot exists" and "the count is
  back where it started" are both true of a run that never took a token.
- Both survive-shutdown teardown mutants passed the whole suite. Every ordering the fixture could
  drive ended the session through an UNGATED site first, and the session's kill is once-guarded, so
  by the time the gated site ran there was nothing left for it to prevent.
- The `check-fast` gate itself had been running zero tests and reporting that there was nothing to
  do, which is the same shape one level up.

The defence that worked in all three: pair every "nothing happened" claim with positive evidence in
the same test, and prove the machinery RAN and chose not to act. Park the run and prove it parks
before freeing a slot. Count the kills that reached tmux, do not merely assert the session lived.
**Do this for each of the remaining resources, and do not trust a mutation result without first
confirming the mutation actually applied** — a no-op edit and a real edit produce identical output
when the thing under test is an absence.

**Two defects in the cold-start token, which the map called the best-behaved of the nine.** The cap
is daemon-global but six places call it per-worker, one of them the justification text for the
210-second remote deadline (`hk-r48zr`, needs a decision, do not tidy the comments). And a run that
times out waiting for readiness holds its slot for the whole run body, because the launch function
returns before the prompt give-back and the caller falls through (`hk-bp5cu`). The map's claim still
stands overall — it is the only one of the nine with an idempotent release and a backstop — but the
release POINT is wrong on one path, and the migration should put the give-back on the
ready-resolution edge for every outcome of that edge, timeout included.

**One mutant nothing kills, which is not a licence to simplify.** Dropping the cancellation conjunct
from `SkipAbortKill` alone breaks no test. It is an equivalent mutant today. It is NOT redundant:
the stall edge reaches that kill with a live context and is unreachable only for two reasons the
reactorization will remove, and — the stronger argument — RSM-037 requires one disposition read at
every site, so "redundant at this one site" is a claim about a per-site predicate, which is the
shape the rule exists to forbid. The reasoning is written into
`internal/daemon/survive_shutdown_run_resources_test.go` where someone about to simplify it will
find it.
