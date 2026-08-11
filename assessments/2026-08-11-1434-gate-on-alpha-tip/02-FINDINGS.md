# Findings

Five findings. One is the gate blocker and it changes what everyone thought the problem was.

---

## F1 — The merge decision is red because a test suite locks the operator's real home config, and one run is enough to do it to itself

**Severity: release-blocking.** Beads `hk-g8d5x` (escalated P1 → P0 today, it owns the mechanism),
`hk-n4vsc`, `hk-core-gate-nondeterministic-m9vlf`.

Ten of fifteen `make full` failures are one defect. The tests wedge because the remote branch of
`EnsureWorktreeTrustVia` takes an **unbounded** `fcntl.flock(LOCK_EX)` on `~/.claude.json.lock` —
the operator's real Claude Code config, in their real home directory — and a leaked `daemon.test`
binary from the same run holds it.

Proved in both directions on a quiet box: remove the lock and five wedged tests pass in about a
second each against a 50-second deadline; hold the lock and the failures return at the identical
`50.19s` with the identical event set. Evidence §4 and §5.

**Three things this adds to what was already filed.**

1. **Scale.** `hk-g8d5x` names three baseline tests. Ten wedged, including the entire
   `TestDotReviewer_*` family and `TestLegacySingleInput_*`. Whatever selects the remote path
   reaches further than the bead says, so a repair scoped to three tests will not close it.

2. **It is self-inflicted, and that is why nobody could find the cause.** `hk-n4vsc` frames this as
   an orphan from an earlier run poisoning later ones — "until someone kills it by hand". The box was
   checked and clean before this run. The holder was created *by* this run and outlived it by eight
   minutes at 98.6% of a core. **One `make full` is sufficient to fail itself on a pristine box.**
   Every search for a stale corpse was looking for something that did not need to exist.

3. **It explains the nondeterminism.** Under a deliberately held lock, one previously-failing test
   passed while two failed. Which tests wedge depends on which need the config write during the
   window. That is a concrete mechanism for `hk-core-gate-nondeterministic-m9vlf` Cause A — a
   different subset every run, from one cause — and it means a changed failure set is not evidence
   of a changed problem.

**The repair is already written and has been sitting unmerged for two days.** `d119d5149` on
`work/alpha-trust-isolation` bounds the wait and honours `HARMONIK_CLAUDE_CONFIG_PATH` in both
python worker programs. It is held by its own author behind an unaddressed `REQUEST_CHANGES`
review. Landing it is the highest-value single act available on the merge decision.

**Why it matters.** A gate that is red for reasons unrelated to the work being gated does not just
waste time — it teaches every reader to say "probably flake", and that is the habit through which a
real failure eventually ships. This project has the habit already.

---

## F2 — I published a load story four days ago and on a quiet box it does not hold

**Severity: correction to this lane's own record.** No bead; it is recorded in the corpus.

The 2026-08-10 assessment concluded the scenario tier's redness was starvation, on the strength of
isolation margins. Tonight the same tier was red with load between 2.5 and 5.6 and 16 GiB free.
Starvation does not explain it.

The margins reasoning was not wrong — it is still the right way to recognise starvation, and the
load class is real and separately filed as `hk-vp02y`. What was wrong was treating "not a defect"
and "load" as the only two answers. **There was a third and it was the majority of the failures.**

The tell that separates them was in the data the whole time and I did not look for it: starvation
produces a *spread* of elapsed times, one per test crossing its own deadline. A lock produces a
*uniform* one. Eight failures at exactly `50.18s` is not a busy box; nothing schedules that evenly.
And the log said so outright, five times, in a line nobody grepped for:
`write-lock acquire timed out (contended ~/.claude.json)`.

The pattern this lane keeps repeating is inferring from a plausible cause instead of measuring for
a specific one. This is the fourth instance. It is now LP-019, with the both-directions control as
a required step, because a positive control alone would have let tonight's story stand too.

---

## F3 — Alpha's two fixes hold, verified by running

**Severity: none — this is a pass.**

- **`hk-5ji8t`**, the stale codex scenario test this lane filed last night: `PASS ... (5.59s)`,
  package green. Was failing 4 of 4. Alpha's diagnosis matched this lane's — the product was right
  and the test was stale — and the repair went into the test. Fixed at `0b01f9f6b`.
- **`hk-set-concurrency-unbounded-ad79i`** (corpus LP-005): fixed at `1a33cd904`, verified against a
  live scratch daemon. `set-concurrency 999999` now exits 2; sane raises up to 16 still apply; 32 is
  refused on a 10-core host. The ceiling is derived from the host rather than hardcoded. **The
  release-critical part — a fleet outage by operator typo — is closed.**

Both were confirmed by running them, not by reading the commit messages. That distinction is worth
stating because reading a commit message and calling a thing fixed is a mistake this lane made
twice on 2026-08-10.

---

## F4 — The spawn-cap refusal stops one step short of useful

**Severity: minor.** Bead `hk-qm3zv` (P3, filed today).

The refusal is `error: spawn_cap_exceeded (code -32099)`. It names neither the ceiling nor the
current value, so the operator who typed an extra digit cannot tell whether the answer is 16 or 4.
The success path already prints exactly the missing clause — `safe max_concurrent = 16; restart to
raise` — so the number is computed and formatted, just not put where the reader needs it.

Recorded as a partial pass against LP-005's stated bar, which asked for a refusal *naming the
ceiling*. Nothing is unsafe. It is not release-critical and should not be treated as one.

---

## F5 — The task runner reported success on a gate that exited 2, for the twelfth time

**Severity: process, and it is not decreasing.**

`make full` logged `rc=2`. The harness reported "completed (exit code 0)". Every session that has
hit this has recorded it and worked around it by reading the exit code from inside a file; no
session has fixed it, and the count keeps climbing.

The workaround is reliable and it is in the corpus README, LP-018 and now LP-019. But a defence that
has to be remembered twelve times is a defence that will eventually be forgotten once — and the
failure mode is recording a red gate as green, which is the single worst wrong answer this system
can produce. Worth someone's time to fix at the source rather than documenting again.
