# Implementation Tasks — process-group-provenance

**Work:** process-group-provenance · **Date:** 2026-07-22
**Implements:** `05-spec-drafts/{process-lifecycle,handler-contract,beads-integration}.md` (PL v0.6.0, HC v0.8.0, BI v0.8.0)
**Reads with:** `05-changelog.md` (what changed), `06-integration.md` (what the change breaks elsewhere)

## The ordering constraint that shapes this whole plan

The spec states it as a rule about two clauses. It is really this plan's central sequencing fact:

> The matcher without the marker is a sweep that silently matches nothing. The marker without the
> matcher widens every reaper's candidate set with no new guard.

So **every write task (T1–T4) precedes every matcher task (T6–T12), with a verification gate between
them (T5)**. Shipping the halves in either order produces a defect, in *different* directions — which
is why "land them close together" is not good enough:

- **Matcher first:** sweeps stop matching. Safe, silent, and it *looks like success* — the sweep
  runs, reports zero, and the orphan leak stays open with no failing signal.
- **Marker first:** every reaper's candidate set grows to include newly-marked processes while the
  old, forbidden matching rules are still in force. This is the dangerous order.

T5 exists so the transition point is measured rather than assumed.

## Task List

### Phase A — Write the marker (no reaper behaviour changes in this phase)

### T1 — Launch-layer spawn sites write `HARMONIK_PROJECT_HASH`

- **What:** every process spawned by the native launch verbs — `harmonik start captain`,
  `start crew <name>`, `start commodore`, `start admiral` — plus the keeper and any watcher those
  paths arm, sets the provenance marker.
- **Spec sections:** `process-lifecycle.md` §4.2a PL-006e(3) (write obligation), PL-006e(5) (value shape).
- **Deliverables:** launch-path env assembly sets `HARMONIK_PROJECT_HASH` to the PL-006a 12-hex
  `project_hash`, obtained from the existing canonical hasher rather than recomputed.
- **Acceptance:**
  1. For each launch verb, the spawned process's environment contains
     `HARMONIK_PROJECT_HASH=<12-hex>`, **read back out-of-process** (`ps -E` on darwin,
     `/proc/<pid>/environ` on Linux) rather than asserted from the spawn-side struct.
  2. The value equals `harmonik project-hash --project <dir>` exactly.
  3. The value contains no whitespace (PL-006e(5)).
  4. A descendant two levels down (launch → agent → watcher) carries the marker by inheritance,
     including across a `setsid` boundary.
- **Depends on:** none. **Beads:** hk-o7x4w, hk-5z4ww (enabling half).
- **Note:** the daemon *handler* path already writes the marker (`workloop.go:1103`, the hk-nvrvp
  fix). Do not re-implement it or assume it absent — the pass-4 review corrected exactly that
  overstatement. Verify handler-path coverage; extend only the launch layer.

### T2 — The `br` adapter writes the marker on every `br` subprocess

- **What:** the `br` adapter sets the marker **explicitly** on each spawned `br` child.
- **Spec sections:** `beads-integration.md` §4.10 BI-014b.
- **Deliverables:** marker set at the adapter's spawn site.
- **Acceptance:**
  1. Every `br` subprocess carries the marker, verified out-of-process.
  2. **Inheritance is proven insufficient** by a test asserting the daemon's own environment does
     *not* carry the marker. This is the fact that makes explicit setting necessary; pin it so a
     later refactor to "just inherit" fails loudly.
  3. No `br` invocation path bypasses the adapter.
- **Depends on:** none (parallel with T1). **Bead:** hk-c6dt2 (write half).

### T3 — Spawn-site register

- **What:** an enumerated register of every process-spawn site, each marked conformant (writes the
  marker) or exempt-with-stated-reason.
- **Spec sections:** `process-lifecycle.md` §4.2a PL-006g; makes PL-INV-005's sensor checkable.
- **Deliverables:** the register, plus the mechanical check that keeps it honest.
- **Acceptance:**
  1. A mechanical scan (`exec.Command` / `exec.CommandContext` / `os.StartProcess` / the tmux spawn
     verbs) finds no spawn site absent from the register.
  2. Every exemption carries a reason, not a blank.
  3. **CI fails when a new spawn site is added without a register entry.** Without this the register
     is accurate only on the day it is written and then decays silently — the same failure mode as
     PL-021c, which this work retires precisely because it was conditioned on something nothing
     produced.
- **Depends on:** T1, T2 (it records their outcome). **Bead:** hk-6v0ez.

### T4 — darwin marker read: `ps -E`, the strip rule, fail-closed

- **What:** one shared read primitive `readMarker(pid) → (hash string, readable bool)` used by every
  reaper. Darwin derives the environment as *the `-E` listing minus the non-`-E` listing taken as a
  literal prefix*; Linux reads `/proc/<pid>/environ`.
- **Spec sections:** `process-lifecycle.md` §4.2a PL-006e(4), PL-006f(1), PL-006f(3).
- **Deliverables:** the primitive; all reapers call it.
- **Acceptance:**
  1. Raw `-E`-line matching is **absent**, and a test proves the distinction: a process carrying
     `HARMONIK_PROJECT_HASH=<hash>` in its **argv** and not its environment reads as *no marker*.
     This is the forgery the strip rule prevents — without it, any local process can hand harmonik a
     machine-wide kill primitive aimed at a victim of its choosing.
  2. An unreadable environment returns `readable=false`, never an empty-string marker a caller could
     mistake for "no marker, but readable".
  3. Apple platform binaries (`/bin/sh`, `/bin/zsh`, `/bin/sleep`) return `readable=false` with no
     error, per `FINDING-darwin-marker-unreadable-on-apple-binaries.md` (macOS 26.3.2).
  4. Exactly one read primitive exists; no reaper parses `ps` output itself.
- **Depends on:** none (read-only; changes no kill behaviour). **Bead:** hk-o7x4w.

### Phase B — The gate

### T5 — Write-coverage verification gate

- **What:** measure, on a live box, what fraction of harmonik-spawned processes carry the marker —
  by spawn regime (daemon handler, launch layer, `br`, tmux-hosted) and by
  readable / unreadable / absent.
- **Spec sections:** the PL-006f / BI-014b paired-change constraint.
- **Deliverables:** the measurement, and a go/no-go.
- **Acceptance:**
  1. Every spawn regime in the T3 register reports a marked population.
  2. The unreadable population is enumerated and each member explained (expected: Apple platform
     binaries only).
  3. **No matcher task starts until this reports clean.** With partial write coverage, the matcher
     tasks silently under-reap on exactly the gap.
- **Depends on:** T1, T2, T3, T4. **Blocks:** T6–T12.
- **This is a checkpoint, not a deliverable** — the only task here whose value is entirely in
  refusing to proceed.

### Phase C — Matcher discipline (gated on T5)

### T6 — Generation nonce: mint and propagate `HARMONIK_SESSION_GEN`

- **What:** long-lived processes record the current generation *at start*, reading it from the
  keeper-updated session-state file (`.managed`); descendants inherit it.
- **Spec sections:** `process-lifecycle.md` §4.2a PL-006e(6); `[session-keeper.md §4.1 SK-003]`.
- **Acceptance:**
  1. The nonce **changes across a keeper restart cycle** — the event a per-invocation nonce would
     miss. The test drives an actual restart, not a fresh launch.
  2. The nonce is **not** minted per `harmonik start <role>` invocation.
  3. Two watchers of different generations are distinguishable by nonce while remaining identical on
     marker, agent name, pane, PPID, and argv — the measured OD-4 condition.
  4. No dependency on a vendor env var (`CLAUDE_CODE_SESSION_ID` coupling was refused by design).
- **Depends on:** T5. **Beads:** hk-5z4ww, hk-3eurz.

### T7 — Retrofit every reaper to PL-006f matcher discipline

- **What:** all four reapers match on marker first via T4's primitive; argv / `comm` / binary path /
  `PPID==1` demoted to post-match narrowing filters; unreadable ⇒ spare; fixed evaluation order.
- **Spec sections:** `process-lifecycle.md` PL-006f(1)–(5), PL-007 as strengthened;
  `beads-integration.md` BI-014a.
- **Scope — all four, none omitted:** the PL-006 orphan sweep (`orphansweep.go`), the PL-021b session
  sweep, `SweepOrphanBr`, `ReapPriorAgentFollowWatchers`.
- **Acceptance:**
  1. **Mutation-verified in both directions, per reaper:** reverting to a loose match must fail a
     test, *and* over-narrowing the matcher must fail a different test. A guard verified in one
     direction only is satisfied by a matcher that never matches.
  2. `SweepOrphanBr` does not kill another project's `br` (hk-c6dt2's original defect).
  3. No reaper kills a candidate whose marker is unreadable.
  4. SIGKILL paths re-enumerate before signalling, so PID reuse cannot redirect a kill.
- **Depends on:** T5, T6 (the watcher reaper needs the nonce to express "prior generation").
- **Beads:** hk-c6dt2, hk-5z4ww, hk-o7x4w.
- **Prior art:** the `watcher-reap-scope` fix (`c718ca98`, reviewed APPROVE) already did this for the
  launch-path watcher reaper, with 15 tests pinning three failure directions. **Reuse its shape; do
  not redo it.**

### T8 — Reaper fires on session RESTART, not only on launch

- **What:** the normative MUST that PL-006e(6) supplies a mechanism for but no spec mandates, placed
  in whichever spec owns the launch/keeper reaper — plus the implementation.
- **Spec sections:** follow-up to PL-006e(6); flagged as pass-5 reviewer minor (1).
- **Acceptance:** a keeper restart cycle reaps the prior generation's watchers; a launch-only trigger
  fails the test.
- **Depends on:** T6, T7. **Bead:** hk-3eurz.
- **Not bookkeeping:** the measured live leak is 12 watchers, 3 dead, 37% of cap. A launch-only
  trigger never fires for a session that restarts in place — which is the normal case.

### T9 — HC-044: direct-exec subprocesses lead their own group; kill group-wise

- **What:** direct-exec spawns set their own PGID; termination is group-directed.
- **Spec sections:** `handler-contract.md` §4.10 HC-044(b), HC-044(e).
- **Acceptance:**
  1. A grandchild of a direct-exec subprocess is terminated by the group kill.
  2. A `setsid` descendant **escapes** the group kill — asserted, not merely unasserted, so the
     documented limit stays true and is not silently "fixed" into a false claim of total reach.
  3. The process group is used **only** as a kill handle; no code reads a PGID to decide ownership.
- **Depends on:** T5. **Bead:** hk-n93gq.

### T10 — HC-018: group-scoped cleanup bound

- **What:** the 5-second cleanup bound covers the whole group, with no per-descendant clock restart.
- **Spec sections:** `handler-contract.md` §4.4 HC-018 as amended.
- **Acceptance:** a depth-*n* tree is cleaned within the 5-second bound total; a test at depth ≥3
  fails under a restarting clock.
- **Depends on:** T9. **Bead:** hk-n93gq.

### T11 — HC-044a: swap the fail-fast detection mechanism

- **What:** retire the never-implemented `.lock` pidfile, its liveness probe, and its argv-check
  recycling discriminator; detect via marker + generation nonce.
- **Spec sections:** `handler-contract.md` §4.10 HC-044a as amended.
- **Acceptance:**
  1. The fail-fast obligation is **unchanged** — only detection is replaced.
  2. The launch polarity is preserved: unreadable ⇒ **treat as held** (do not launch). This is the
     *opposite* polarity from the reaper rule, and both must be pinned by tests in the same package
     so a later "harmonisation" cannot invert one. This is the most invertible pair in the work.
- **Depends on:** T6. **Bead:** hk-n93gq (adjacent).

### T12 — PL-017a(b): relay-grandchild exclusion inherits fail-closed

- **What:** an unreadable exclusion input causes the candidate to be spared.
- **Spec sections:** `process-lifecycle.md` §4.7 PL-017a(b) as amended.
- **Acceptance:** pin the old behaviour as a regression test — it previously failed **open**,
  meaning unreadable ⇒ killed.
- **Depends on:** T4, T7.

### Phase D — Dead-code and retirement cleanup

### T13 — Remove the dead `setsid` code

- **What:** remove `lifecycle.SetsidDaemon` and `lifecycle.SpawnSysProcAttr`, whose documentation
  quotes the withdrawn MUST and which have zero production callers.
- **Spec sections:** the PL-006a retirement note.
- **Acceptance:** no production caller; the withdrawn sentence appears nowhere in code comments;
  build green.
- **Depends on:** T7. **Bead:** hk-g1qby.
- **Sequencing note:** hk-g1qby reads as "the daemon never calls `Setsid()`, violating PL-006a." That
  framing is now void — the requirement is withdrawn, not unmet. **Close it as
  resolved-by-retirement, not fixed-by-adding-a-call.** Adding the call would implement a requirement
  this work deliberately removed.

### T14 — Retire the PL-021c code path, keep its event fields

- **What:** remove the pane-orphan-recovery path conditioned on the never-produced window-name
  sentinel.
- **Spec sections:** the PL-021c retirement.
- **Acceptance:**
  1. The conditioned path is removed.
  2. `tmux_windows_killed` and `tmux_kill_window_survivors` **remain** in the
     `daemon_orphan_sweep_completed` payload — two other clauses cite them as an additive-extension
     precedent, and removing them breaks the payload compatibility those clauses rely on. A test
     asserts their continued presence.
- **Depends on:** T7.

### Phase E — Cross-spec repairs (from `06-integration.md`)

### T15 — CR-1: `workspace-model.md` §4.8 stops identifying a generation by argv

- **What:** replace the argv-generation-discriminator clause at `:708` with the PL-006e(6) nonce
  conjunct; repoint the `:350` HC-044a coordination to the marker mechanism.
- **Spec sections:** integration finding C1. **Bead: hk-3ncno (P1, filed).**
- **Acceptance:** no spec in the corpus instructs an implementer to identify a daemon generation by
  command line.
- **Depends on:** the three spec files landing (it cites PL-006e(6) and PL-006f(2), which do not
  exist until then).
- **Why P1 while its siblings are not:** WM's rule authorises **reclaiming a workspace**. Every other
  CR is a stale pointer that misleads a reader; this one tells an implementer to make a destructive
  decision using the exact signal this work proves cannot carry it.

### T16 — CR-2..CR-5: remaining cross-spec repairs

- **CR-2** — `workspace-model.md:325`: record that HC-044a no longer names a lock filename; close
  OQ-WM-005 as resolved-by-convergence on `.harmonik/lease.lock`.
- **CR-3** — `claude-launchspec.md:152`: repoint the marker obligation from PL-006a to PL-006e(3).
- **CR-4** — `event-model.md:255`: annotate the two payload fields as sourced from retired PL-021c.
  **Do not rewrite the `:1808` revision-history entry** — it was accurate when written.
- **CR-5** — `session-keeper.md` §4.1 SK-003: state normatively that the managed-session value
  differs across generations, since PL-006e(6) now depends on it.
- **Depends on:** the three spec files landing. **Parallel with:** T15.

### Phase F — Validation (required before `ready`)

### T17 — Scenario test · bead hk-8xdur

> `scenario: process-group-provenance — a marked descendant is reaped after its root dies, and an
> unmarked or unreadable one is never killed`

- **Spec sections:** PL-006 coverage boundary, PL-006e(3)/(4), PL-006f(1)/(3), PL-021b §7.
- **Acceptance — end-to-end; both polarities plus both documented non-coverage rows:**
  1. A marked descendant orphaned to init **is** reaped by the PL-006 sweep.
  2. An unmarked descendant is **not** killed.
  3. A descendant whose environment is unreadable (Apple platform binary) is **not** killed.
  4. A peer project's marked process is **not** killed.
  5. **The two non-coverage rows hold as stated** — a still-parented `setsid` descendant is not
     reaped (row 3), and a darwin Apple-signed-binary descendant is not reaped regardless of
     parentage (row 4). These assert the documented *limits*, so a later change that accidentally
     widens or narrows coverage is caught rather than absorbed.
- **Depends on:** T7, T9.

### T18 — Exploratory test · bead hk-p9sl7

> `explore: process-group-provenance — operator can see from the CLI which processes harmonik claims,
> and why a spared one was spared`

- **Spec sections:** the operator surface over PL-006g and PL-006f(3).
- **Acceptance:** an operator can, from the CLI, list marker-claimed processes for a project and
  distinguish *no marker* from *marker unreadable* — two cases with identical outcomes (spared) and
  completely different meanings. Conflating them is what kept hk-o7x4w's darwin no-op invisible as
  long as it was.
- **Depends on:** T4, T7.

## Dependency Graph

```
Phase A (parallel)
  T1 ─┐   launch-layer marker write
  T2 ─┤   br marker write
  T4 ─┤   darwin read primitive (read-only; no kill behaviour)
      │
  T3 ─┘   spawn-site register            [T3 depends on T1, T2]
      │
      ▼
Phase B
  T5      WRITE-COVERAGE GATE            [needs T1,T2,T3,T4 — blocks all of Phase C]
      │
      ▼
Phase C (three independent chains)
  T6 ──► T7 ──► T8                       nonce → matcher retrofit → reap-on-restart
         └────► T12                      relay exclusion fail-closed  [also needs T4]
  T9 ──► T10                             own-group kill → group-scoped bound
  T6 ──► T11                             HC-044a detection swap
      │
      ▼
Phase D
  T7 ──► T13                             dead setsid removal
  T7 ──► T14                             PL-021c retirement (fields retained)
      │
      ▼
Phase E   (after the three spec files land)
  T15 (P1, hk-3ncno)   ∥   T16
      │
      ▼
Phase F
  T17 [needs T7, T9]   ∥   T18 [needs T4, T7]
```

**Valid DAG** — no cycles; every prerequisite is a declared task.

## Parallelization Plan

| Wave | Concurrent | Why they do not collide |
|---|---|---|
| 1 | **T1, T2, T4** | Two disjoint write paths (launch layer, `br` adapter) plus one read-only primitive. No shared files; none changes kill behaviour. |
| 2 | **T3** | Serialized by construction — it records what T1/T2 produced. |
| 3 | **T5** | Gate. Nothing runs beside it; its purpose is to be a stop. |
| 4 | **T6, T9** | Different subsystems: nonce/keeper surface vs. handler process-group spawn. |
| 5 | **T7, T10, T11** | Different call paths. **T7 should not be split across agents** — its correctness argument is per-reaper mutation testing in both directions, which fragments badly across contexts. |
| 6 | **T8, T12, T13, T14** | Independent cleanups, all downstream of T7. |
| 7 | **T15, T16** | Spec-only edits in different files. |
| 8 | **T17, T18** | Independent test surfaces. |

**Undeclared-dependency risk, stated rather than assumed:** T7 and T11 implement the two *opposite*
fail-closed polarities (reaper: unreadable ⇒ spare; launch: unreadable ⇒ treat as held). Running them
in one wave is deliberate — they should be reviewed **together**, because the failure mode is a later
reader harmonising them into one rule. If they must be split, the reviewer of the second must read
the first.

## Traceability — every changelog entry has a task

| Changelog entry | Task(s) |
|---|---|
| PL-006e (marker, inheritance, write obligation, darwin readability, value shape) | T1, T2, T4 |
| PL-006e(6) (generation nonce + mint point) | T6, T8 |
| PL-006f (matcher discipline, strip rule, argv prohibition, fail-closed, order) | T4, T7, T12 |
| PL-006g (spawn-site register) | T3 |
| PL-006a retirement (`setsid` MUST + PGID half) | T13 |
| PL-021c retirement (fields retained) | T14 |
| PL-006 amendment (false `br` sentence; coverage boundary) | T2, T7, T17 |
| PL-007 strengthening | T7 |
| PL-017a(b) fail-closed | T12 |
| PL-021b §7 (three-row + darwin fourth-row coverage) | T17 |
| PL-INV-005 (parentage per regime; checkable sensor) | T3, T9 |
| OQ-PL-008 resolved / OQ-PL-011 superseded | T4 (008); T13 (011) |
| OQ-PL-008a new | Stays open by design — split out so resolving the provenance half cannot silently close it |
| HC-044 (parentage, own group, kill handle, limits) | T9 |
| HC-018 (group-scoped bound) | T10 |
| HC-044a (detection swap) | T11 |
| BI-014a (`br` matcher) | T7 |
| BI-014b (`br` marker write) | T2 |
| OQ-BI-010 resolved | T2 |
| Integration C1–C5 (`06-integration.md`) | T15, T16 |

## Bead map

| Bead | Status | Tasks | Closing note |
|---|---|---|---|
| hk-n93gq | open P1 | T9, T10, T11 | |
| hk-o7x4w | open P1 | T1, T4, T7, T17 | **Do not close as a known gap.** The orphaned-to-init leak this bead names *is* fixed here; only the still-parented case stays uncovered. |
| hk-g1qby | open P1 | T13 | **Close as resolved-by-retirement**, not fixed-by-adding-`Setsid()`. |
| hk-c6dt2 | open P1 | T2, T7 | |
| hk-5z4ww | open P2 | T6, T7 | |
| hk-3eurz | open P2 | T8 | |
| hk-3ncno | open P1 | T15 | Filed by the integration pass. |
| hk-6v0ez | open P2 | T3 | Register + CI check that keeps it from decaying. |
| hk-8xdur | open P1 | T17 | Scenario gate. |
| hk-p9sl7 | open P2 | T18 | Exploratory gate. |

**No bead closes — and this work does not close — until hk-8xdur and hk-p9sl7 are also closed.**

## Two things an implementing agent will get wrong without being told

**1. `br` children do not inherit the marker.** The daemon's own environment does not carry it, so a
`br` child receiving only the daemon's environment carries nothing. An implementation relying on
inheritance produces a population BI-014a can never match — and the failure is silent: the sweep
runs, finds nothing, reports success. T2 pins this with an explicit negative assertion for exactly
this reason.

**2. The two fail-closed rules point in opposite directions, on purpose.** A reaper that cannot read
a marker must **spare** the process. A launch path that cannot read a marker must **treat the
workspace as held** and refuse. Both are "fail closed" — closed means *toward not causing harm*, and
the harmful act differs by caller. T7 and T11 must land with both polarities tested side by side.
