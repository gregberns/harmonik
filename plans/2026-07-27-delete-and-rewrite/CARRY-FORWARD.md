# Carry-forward facts

Irreducible facts about the **outside world** — tmux, terminals, the agent CLIs, SSH —
discovered the expensive way. These are not requirements of our design; they are
constraints any implementation must satisfy. A rewrite does not prevent a single one.

Source: bug archaeology across ~53 defects in `internal/daemon/pasteinject.go` (56 commits).
Each fact cost at least one production incident.

**Status:** harvested from `pasteinject` only. Still to harvest: `tmuxsubstrate`,
`codexwire`/`codexdriver`, `harness/pi`, `brcli`.

**Graduate this file to `specs/` once stable.** Any rewrite of an agent-substrate subsystem
must satisfy every fact below.

---

## tmux / terminal

1. **Bracketed paste delivers `\n` as a literal LF byte, never as an Enter key event.** A
   trailing newline in the payload does not submit. Only a bare `send-keys Enter` (not
   `-l`) produces a key event.

2. **`tmux load-buffer` + `paste-buffer` exits 0 as soon as tmux hands the buffer to the
   pane — not when the TUI has rendered it.** Exit 0 is not delivery. The only proof of
   delivery is `capture-pane` showing a marker you know is in the seed's first line.

3. **tmux misparses a slash-bearing target** (`session:some/path/name`) and silently returns
   the *session's active pane*, not the named window's — so a paste can land in a previous
   run's pane. Capture the pane id atomically at creation:
   `tmux new-window -P -F '#{pane_id}'`.

4. **An invalid tmux buffer name drops the payload rather than erroring.** Names are
   validated against `^harmonik-[a-z0-9-]+-[a-z0-9-]+$`; an uppercase letter, `_`, `.` or
   `/` causes silent loss. This bit us three times: a `20060102T150405Z` id, a missing
   purpose segment, and an unsanitized `fmt.Sprintf`.

5. **tmux `exec`s `sh -c "claude …"` into the pane, so the pane PID *is* the agent.**
   `pgrep -P <panepid>` returns nothing for a *healthy* agent. Liveness must accept any
   descendant **or** the pane PID itself when its `comm` matches the agent binary.

6. **"Pane has an active child process" means alive, not working.** An idle agent sitting
   at a prompt is indistinguishable from a working one. Liveness alone must never extend a
   budget indefinitely.

## Claude Code / agent CLI

7. **The welcome splash is an ink/React TUI that processes only key events.** It must be
   dismissed with a real Enter before a paste, and under concurrent cold-boots it takes
   **>750 ms** to clear — a single submit Enter at a fixed delay gets swallowed. Send the
   submit Enter with bounded retries; redundant Enters at a submitted REPL are harmless
   empty lines.

8. **While the TUI is absorbing a bracketed paste it swallows keystrokes.** A retry loop
   entirely inside that absorption window (3 attempts / 800 ms) helps nothing. Wait *after*
   the write, plus a late one-shot re-seed Enter (~75 s) as a net.

9. **The TUI collapses a long single-line paste into a `[Pasted text #N]` chip**, so the
   literal text never renders and capture-based verification fails deterministically. Keep
   the seed short (currently bounded at 300 chars) and push detail into a file the agent
   reads.
   **⚠ The collapse threshold changed under us in a Claude Code update (~2026-07-11) and
   wedged the whole fleet.** This is an external dependency that moves — no rewrite
   protects against it. Detect, do not assume.

10. **On `claude --resume <id>` the REPL input handler is intermittently not yet ready**;
    the first Enter is dropped.

11. **Sending a second message before the agent returns to the input prompt drops it
    silently.** There is no ready signal. The only reliable pattern is one paste, one Enter.

12. **The initial context-load/planning phase runs 8–10 minutes emitting nothing** — no
    heartbeat, no output, no file change — and is indistinguishable from a dead pane on any
    single signal. Three independent signals are required: heartbeats, worktree fingerprint
    (HEAD + `git status --porcelain`), and pane output growth (`history_size` + `cursor_y`).
    A read-heavy agent trips only the third.

13. **Claude Code's Stop hook fires on session exit only, not per response.** The agent
    stays alive at the REPL after finishing; the daemon must inject `/quit` itself, and must
    still force-kill after a grace because `/quit` does not reliably end the pane.

14. **The agent's Write tool creates the file before flushing.** Acting on `stat()`
    existence kills it mid-write and permanently truncates the output. Parse for a complete,
    valid document before acting.

15. **An LLM hand-writing JSON produces invalid JSON** (a backtick in a string became a
    stray `` \` ``). Give it a CLI that marshals instead of asking for literal JSON.

16. **A different harness has a different pane surface entirely** — Pi/codex emit NDJSON, or
    have no pane at all. Pane-scrape verification and paste injection must be gated on
    harness capability, never assumed.

17. **Reviewer agents will run destructive git commands on the shared repo**, approve
    partial "all-X" changes, and miss named identifiers unless the prompt forbids or
    requires each explicitly.

## Budgets — empirical, not derivable

18. Every one of these was set by a production false-kill, several twice:
    - Reviewer: ~10 min base **+ 10 min per 1000 changed lines**, ceiling 60 min
    - Implementer: 30 min per-progress budget, **90 min absolute hard ceiling**
    - Launch window: 180 s · launch-suppression ceiling: 12 min · heartbeat staleness: 8 min

## SSH / remote

19. **A shared SSH `ControlMaster` under churn silently drops multiplexed
    `load-buffer`/`paste-buffer`.** Pin the tmux runner to
    `ControlMaster=no -o ControlPath=none`.

20. **…but then each `capture-pane` is a fresh connection**, so transient capture failures
    become common. Distinguish "capture failed (infra)" from "marker absent (paste lost)" —
    treating them alike killed runs whose paste had landed.

---

## The shape of the problem

Facts 7, 8, 10, 11, 14 share one root cause: **the target TUI exposes no readiness or
acknowledgement signal.** Ordering cannot be enforced by types on our side of the boundary.
These are only ever fixable by observe-and-retry, and a rewrite inherits all of them.

Design consequence: the injection module's core loop is necessarily
**write → verify by observation → retry with bound**, never **write → assume**. Any design
that treats delivery as synchronous is wrong before it is written.
