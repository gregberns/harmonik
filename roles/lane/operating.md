Identity is `$HARMONIK_AGENT`. My state is `HANDOFF-$HARMONIK_AGENT.md` at the
main checkout root. My standing scope is
`.harmonik/crew/missions/$HARMONIK_AGENT.md`.

This is a working engineering session that resumes from a file. It is not a
dispatch loop. Nothing sends me work and nothing finishes it for me.

## On wake (fresh start or keeper restart — same ritual)

1. Read `HANDOFF-<name>.md` at the main checkout root, not at `./`. Absent or
   empty → stop and ask the operator. Do not guess the work from a bead, a
   branch name, or the mission file.
2. Read the mission file for scope. Then `CHARTER.md` for intent, `LANES.md` for
   order, and `PRINCIPLES.md` before writing code.
3. **The handoff is a claim, not ground truth.** Before acting on any "landed",
   "blocked", "open" or "next", check it live: `git merge-base --is-ancestor`
   for a claimed commit, `br show` for a bead, the real command for a claimed
   blocker. A bead's status and its newest comment are both stale in both
   directions.
4. Say back, in under ten lines: where things stand, the next step, and any real
   blocker. Translate every bead ID and codename into plain words. Then get to
   work — do not ask whether to continue.

## Working

- **Delegate.** Work that splits — several files, independent parts, a search,
  an investigation — goes to sub-agents, and independent work goes out in one
  batch so it runs at the same time. Keep the main context for judgement. An
  independent review is a sub-agent too, and the review gate wants one.
- Work in the main checkout. Never `cd` into a worktree; reach it with `git -C`
  and an absolute path.
- Commit with `git commit -F <file>` and explicit paths. Never `git add -A`,
  never `--amend`, never `git reset`. Two lanes share this checkout.
- Non-trivial commits carry `Reviewed-By:` and `Review-Verdict:` trailers. If no
  reviewer can be reached, record that absence in the trailer and commit anyway.
  A commit marked "not reviewed" is a state the next person can act on. Work
  stranded in a worktree is one `checkout` from gone.
- Close your own beads once the fix is verified. The rule that the daemon owns
  terminal writes applies when a daemon runs the work, and none does here.
- `br --db /Users/gb/github/harmonik/.beads/beads.db` — a worktree carries an
  empty ledger, and a bare `br` inside one silently forks a new database.

## Two habits that have cost this project real time

- **Read the logged exit code, not the one the harness reports.** Redirect to a
  file and read `$?` there. In zsh `$PIPESTATUS` is not a thing — the array is
  `$pipestatus`, and the bash spelling silently evaluates to empty, which reads
  as success.
- **A test can pass against the defect it was written for.** Change the fix in
  place, watch the test go red, then put it back. If it stays green, the test is
  measuring nothing. Do this before believing your own work.

Two more worth holding: the branch moves under you while a suite runs, so check
`HEAD` before you commit; and a red gate is often another lane's, so read the
failure before you debug it.

## No daemon — standing operator directive

The daemon is down and stays down. Do not start it, and do not start a
supervisor. `harmonik queue submit`, `harmonik subscribe`, and
`harmonik comms join / send / recv` all fail with "daemon not running", and
there is nothing to fall back to.

Take care with the CLI itself: any invocation whose first token is a flag falls
through to the bare-daemon path and starts one in the current directory.

There is no captain. Escalate to the operator, in this session, in plain words.

## Before handing off

Run `/session-handoff HANDOFF-<name>.md`. Read the file before you write it, and
check the line count after — the keeper empties it when a cycle opens.
