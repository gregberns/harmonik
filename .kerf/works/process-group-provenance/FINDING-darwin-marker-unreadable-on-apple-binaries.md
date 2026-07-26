# FINDING — the darwin marker read is blind to Apple-signed system binaries

**Found:** 2026-07-22, after the pass-5 APPROVE, during post-review verification.
**Status:** measured on this box, not inferred. Affects a load-bearing claim in the approved text.
**Decision owner:** captain — this is a change to text that has already passed review.

## The claim this challenges

The design resolves the darwin read to `B4-strict`: recover a candidate process's environment with
`ps -E` and match the provenance marker. Pass-4 review credited that read, together with the marker
write obligation, with reaping the orphaned-to-init `setsid` descendant — the leak measured earlier
this shift (pid 8610, a `comms recv --follow`). Pass-5 review re-confirmed it.

That credit is correct **for the process that was measured**, and it does not generalise. The read
works on some binaries and silently returns nothing on others, and which one you get is a property
of the binary, not of the process's ownership.

## What was measured

macOS 26.3.2 (build 25D2140). Same user, same session, no elevation. A variable was exported, a
child was started, and `ps -Ewww -p <pid>` was read back.

| Process | Binary provenance | Marker readable via `ps -E`? |
|---|---|---|
| `harmonik comms recv --agent kilo --follow` (live, pid 53426) | our own Go binary | **YES** — `HARMONIK_AGENT` and `HARMONIK_PROJECT` both plainly visible |
| `tmux` (homebrew) | third-party, not Apple-signed | **YES** |
| Xcode-framework `python3` | Apple-shipped but not `/bin` | **YES** |
| `/opt/homebrew/bin/bash -c '…'` | homebrew | **YES** |
| `/bin/sh -c '…'` | Apple-signed system binary | **NO — empty** |
| `/bin/zsh -c '…'` | Apple-signed system binary | **NO — empty** |
| `/bin/sleep` | Apple-signed system binary | **NO — empty** |

The failure is silent in the worst way: `ps -E` prints the process row with its command line and
simply omits the environment. There is no error, no exit code, and no way to distinguish "this
process carries no marker" from "this process's environment cannot be read." The two are the same
observation.

## Why it is not academic on this box, right now

The sweep's target population is `PPID == 1`. Enumerated live during this check:

- **pid 83775 — `/bin/zsh`, PPID == 1.** Orphaned to init. Environment unreadable.
- **pid 83773, pid 604 — `/bin/sh`, PPID == 1.** Same.
- **41 Apple-signed shells alive**, against **29 processes carrying a readable marker.**

Whether pid 83775 is harmonik's is not merely unknown — it is unknowable *by the mechanism the
spec adopts*. The sweep can neither claim it nor disclaim it.

And the population is generated continuously by normal operation: every agent tool call in this
fleet runs through `/bin/zsh`. The table above shows 16 live `/bin/zsh` processes parented to a
`claude` process. Each one becomes a `PPID == 1` unreadable orphan the moment its agent dies —
which is the exact event the orphan sweep exists to clean up after.

## What it does and does not break

**Polarity is correct, and that is the good news.** PL-006f(3) fails closed on an unreadable
matcher input, so an unreadable candidate is **spared**, never killed. This is a **coverage** hole,
not a safety hole. Nothing in the design kills someone else's process because of it.

**What it does break is a completeness claim.** PL-021b §7's coverage table carries exactly one
dated non-coverage row — the still-parented `setsid` descendant. This finding is a **second,
orthogonal** non-coverage boundary that the table does not name:

> On darwin, a candidate whose binary is Apple-signed and system-resident carries a marker that
> cannot be read, regardless of its parentage or origin. It is spared by the fail-closed rule and
> is never reaped.

It is orthogonal because it cuts across the existing row rather than sitting inside it: it applies
to orphaned and still-parented candidates alike, and it applies whether or not `setsid` was ever
called.

**It does not resurrect B1/B2/B3.** Nothing here argues for PGID, start-time, or a registry — those
were rejected on their own evidence and a per-binary read gap does not revive any of them. It also
does not weaken the marker-write obligation, which remains the load-bearing half.

## The honest reading of the review that just passed

The pass-4 reviewer's irony was that the proudest part of the design — the honest non-coverage row —
was its least accurate part, because it was drawn too wide. This finding is the same lesson from the
other side: the row was then drawn too *narrow*. Crediting `ps -E` with the darwin read on the
strength of one measured process, when the mechanism's success depends on a property of the binary,
is the same shape of error as reading a proxy and reporting it as the thing.

## Recommended disposition — captain's call, not mine

1. **Add a second non-coverage row** to PL-021b §7 in the pass-5 draft, worded as above, and route
   the delta for a narrow re-review rather than re-running the pass. Preferred: the text is not yet
   finalized, and this is precisely the class of claim this work exists to state honestly.
2. **Or**, if the captain would rather not reopen approved text, file it against finalization as a
   named follow-up so it lands before the spec becomes normative.

What should **not** happen is the option that looks cheapest: leaving the coverage table as-is on
the grounds that the measured leak was reaped. That table is what an implementer reads to decide
what is done, and it would tell them darwin coverage is complete when a routinely-generated
population is permanently invisible to it.

## Reproduce

    export PROBE=visible
    (/bin/zsh -c 'sleep 12; true' &) ; (/opt/homebrew/bin/bash -c 'sleep 12; true' &)
    ps -Ewww -p <each pid> | grep -o 'PROBE=[a-z]*'

The homebrew shell prints the variable. The system shell prints nothing.
