# Post-submission finding — the generation nonce cannot be minted where pass 4 proposes

**Dated 2026-07-22, found by kilo after the pass-4 design was submitted for review.**
Written as a separate file, NOT folded into the three reviewed design documents, so the
design reviewer is not handed a moving target. Pass 5 must absorb it.

**Scope: this is verdict-independent.** It touches OD-4 and PL-006f(4) — the generation
nonce — and has nothing to do with the grandchild non-achievability claim the reviewer was
routed to stress-test. Whichever way that verdict lands, this finding stands.

---

## What pass 4 proposed

`process-lifecycle-design.md` §PL-006f item 4:

> `HARMONIK_SESSION_GEN` is a new variable, needing a mint point and a definition of
> "generation." Smallest defensible: **minted per `harmonik start <role>` invocation,
> inherited thereafter.** Pass 5 must define it precisely or OD-4 regresses to argv.

OD-4 rests on it: the reaper gates on marker **and** nonce, which is what makes "reap
**prior**-generation watchers" decidable without argv.

## Why that mint point does not work

**A keeper restart never re-invokes `harmonik start <role>`.** It is a `/clear` plus a brief
re-injection into the same tmux pane, driven by the same live agent process
(`internal/keeper/cycle.go`; no `harmonik start` call exists anywhere in the keeper package).
So under a per-`start` mint, every generation produced by a keeper restart carries an
**identical** nonce — and keeper restart is the event that produces the duplicates in the
first place. The nonce would be constant across exactly the population it exists to separate.

## The measurement that found it

Live box, 2026-07-22 11:41Z, `ps -eo pid,ppid,lstart,args`: **12 `comms recv --follow`
watchers serving 6 agents** — mike 3, juliet 3, lima 2, assessor 2, admiral 1, kilo 1 (mine,
after I killed three of my own leaked ones at boot). **Three are reparented to PID 1** — their
sessions are gone and they are still connected. Oldest 13 hours. Daemon caps subscribers at 32
(`internal/daemon/subscribe.go:156`), so ~37% of that ceiling is currently held, some of it by
the dead.

Cause: `ReapPriorAgentFollowWatchers` is wired to the **launch** path only
(`cmd/harmonik/captain.go:486`, `cmd/harmonik/crew.go:317`). The duplicate is created by
**restart**. The trigger point is on the wrong event. (This also re-points bead hk-5z4ww,
whose "zero production callers" premise was already corrected by research §7.1 / Finding 4.)

**Environment comparison of juliet's three duplicate watchers — the decisive evidence:**

| Variable | pid 27409 | pid 64675 | pid 41611 |
|---|---|---|---|
| `HARMONIK_AGENT` | juliet | juliet | juliet |
| `HARMONIK_PROJECT` | /Users/gb/github/harmonik | same | same |
| `TMUX_PANE` | %8565 | %8565 | %8565 |
| `CLAUDE_PID` | 57868 | 57868 | 57868 |
| `CLAUDE_CODE_SESSION_ID` | d110ec23… | 4bced084… | 509066bf… |

Every harmonik-owned identity field is identical across all three generations. The project
marker cannot separate them; the agent name cannot; the pane cannot; the parent pid cannot —
`claude` pid 57868 has been alive since 19:36 on Jul 21 and spans all three.

## The second finding, which is the one that constrains pass 5

**Only the harness's own variable changes per generation, and harmonik cannot mint into a
running process's children by the mechanism pass 4 assumes.**

`CLAUDE_CODE_SESSION_ID` differs across all three watchers while `CLAUDE_PID` is constant, and
none of the three equals the `--session-id` the process was launched with (`0d263fb8…`, visible
in pid 57868's argv). So the harness re-stamps a fresh per-generation session id into the
environment of each child it spawns, even though the agent process itself never restarts.

The keeper's existing hook cannot do the same thing. `SetTmuxEnvFn` (`cycle.go:152`) sets a key
in the **tmux session** environment so it is inherited by a **newly created** pane or shell. The
already-running agent process's environment is fixed at exec; its children inherit from *it*,
not from tmux. So a `tmux setenv HARMONIK_SESSION_GEN=<new>` after `/clear` would not reach the
watcher the agent arms a moment later.

**Do not adopt `CLAUDE_CODE_SESSION_ID` as the nonce.** It is harness-owned, it does not exist
for codex-harness crews, and binding a normative spec to another vendor's environment variable
is the coupling this project already refuses elsewhere.

## What pass 5 should specify instead

Mint the generation at the point harmonik already owns and already updates per restart: the
keeper writes the new session id into `.managed` at the end of every cycle
(`SetManagedSessionFn`, `cycle.go:145`). Make **the harmonik CLI stamp the nonce at arm time**
— when `harmonik comms recv --follow` (or any long-lived watcher) starts, it reads the current
generation from that harmonik-owned file and records it on itself. Two properties follow:

1. **No harness cooperation is required**, and no env-inheritance path through a running agent
   process is required — the child stamps itself from a file at the moment it starts.
2. **"Prior generation" becomes decidable by comparing a recorded value to the file's current
   value**, which is what OD-4 needs, without argv and without liveness.

The reap trigger should move with it: **reap on restart, not only on launch**, since restart is
the event that creates the duplicate. Pass 4's PL-006f(4) sentence should be replaced, not
amended — the per-`start` mint is not a smaller version of the right answer, it is the wrong
event.

## What is still open

- Whether the nonce is carried in the watcher's environment, its argv, or a per-watcher file.
  Argv is available and cheap but A6 forbids argv as the *match*; as a **post-match narrowing
  filter** OD-4 already permits it, so argv is defensible here. Pass 5 must pick one and say why.
- Reaping at restart must exclude the watcher the current generation is about to arm. With a
  file-read nonce this is straightforward (same generation ⇒ spared) — it is the reason to
  prefer the file-read mint over any scheme keyed on the pane or the parent pid, both of which
  are provably constant across generations per the table above.
