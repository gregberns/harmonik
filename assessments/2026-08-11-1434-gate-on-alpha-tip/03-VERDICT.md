# Verdict

## BLOCK — and for the first time the blocker is one named defect with a written fix

Measured on `4ebd8a334`, `work/bravo-reachability` rebased onto alpha's `eac00a6c2`, clean tree,
2026-08-11.

    make full  ->  LOGGED EXIT rc=2

This is a BLOCK, but it is not the same BLOCK as last time and the difference is the whole report.
Previous gates were red at the lint stage, or red for a different reason on every run, or starved
into meaninglessness. **Tonight the gate ran to completion on a quiet box, passed lint, passed 218
packages, and failed one target — and two thirds of that target's failures are a single defect that
somebody has already written the fix for.**

## The one thing to act on

**Land `d119d5149` from `work/alpha-trust-isolation`.** It bounds an unbounded file lock over the
operator's real `~/.claude.json` in both python worker programs. It has been sitting unmerged since
2026-08-09 behind an unaddressed `REQUEST_CHANGES` review, while the gate it repairs stayed red.

Ten of the fifteen failures are that lock. Proved in both directions on a quiet box: remove it and
they pass in about a second against a 50-second deadline; hold it and they fail again at the
identical time with the identical event set.

The review's secondary findings need closing first — that is a real gate and I am not asking anyone
to skip it. But this is the highest-value single act available on the merge decision, and its
blocker is a review nobody has picked back up, not an unsolved problem.

## Was alpha's round enough to justify re-testing? Yes, and it earned it

Three of the four things blocking the gate before this round are gone, all verified by running:

| Was blocking | Now | Verified how |
|---|---|---|
| Box below the 10 GiB disk floor, fleet-wide | **Cleared** — 16–18 GiB | `df` before and throughout |
| Lint red on receipt-GC findings (`hk-pw1wv`) | **Fixed** | gate reached the test tier |
| Lint red on `scheduler.go` + `queuewiring/store.go` (`hk-iy9tx`) | **Fixed** | gate reached the test tier |
| A leading flag turned off the mistyped-command guard and started a daemon (LP-002) | **Fixed** at `5dd157cb9` | probed live, 4 spellings |
| A stale codex test failed 4 of 4 (`hk-5ji8t`) | **Fixed** at `0b01f9f6b` | `PASS ... 5.59s` in the gate |
| `set-concurrency` applied any value to a live daemon (LP-005) | **Fixed** at `1a33cd904` | probed live against a scratch daemon |

Alpha also converted 22 shutdown stopwatches into one hang detector and repaired a real keeper bug
that load had been hiding. Both were aimed at the load-sensitivity class, and both look right — but
see below, because that is not what was blocking the gate.

**Six release-critical items closed. One remains, and it is the one nobody had correctly
identified.**

## The correction, and it is mine

Four days of this lane's output said the tier's redness was starvation — a busy box failing
wall-clock deadlines. Tonight the box was quiet, load 2.5 to 5.6, and ten tests still died on a
deadline. **That story does not survive the measurement.**

The load class is real and separately filed. What was wrong was offering only two answers, "broken
code" or "busy box", when the majority of the failures were a third thing. The tell was in the data
the whole time: starvation gives a *spread* of elapsed times, a lock gives a *uniform* one, and
eight failures at exactly `50.18s` is not a busy box. The log also named the cause outright, five
times, in a line nobody had grepped for.

This is the fourth time this lane has reached for a plausible cause instead of measuring for a
specific one, and it is the reason LP-019 requires a negative control rather than only a positive
one. A positive control alone would have let tonight's story stand as well.

## What this gate does NOT establish

- **`hk-vp02y`, the load class, is not cleared.** Alpha's hang-detector change looks correct and
  nothing tonight contradicts it, but a quiet box cannot test a fix for load sensitivity. It needs a
  deliberately contended run, and that measurement does not exist.
- **Five of the fifteen failures are unexplained.** They are not the lock. They may be the load
  class or something else; they were not isolated. Presumption, not measurement.
- **One `make full`, not two.** The both-directions controls carry the causal claim about the lock,
  but the failure *set* is a single sample.
- **Still no bead driven end to end through a live dispatched run.** A scratch daemon was stood up
  and probed live this time, which is further than the last session got, but the standing gap is
  open. It is the honest one, and it should be the next thing done.

## Process notes the operator should see

1. **The task runner reported "exit code 0" for a gate that logged `rc=2`.** Twelfth occurrence.
   Every session works around it; none has fixed it. The wrong answer it produces — a red gate
   recorded as green — is the worst one this system can generate.
2. **No reviewer read this session's work.** This session is under a standing instruction not to
   spawn sub-agents. Every commit trailer records the absence and carries no approval. The changes
   are corpus files, assessment records and bead updates — no product code.
3. **Twenty minutes went into a false trail that was my own fault**, and it is written up in the
   corpus rather than hidden: a scratch daemon built at a long path never binds its socket, because
   a Unix socket path is capped at 104 bytes. Three findings looked available and all three were the
   choice of directory. The daemon had said so plainly in its own log. The corpus README's
   `/tmp/h/<name>` is load-bearing.
