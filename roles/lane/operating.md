> **This is a role, not a process. Nothing starts it.** If you are reading this,
> you are a lane. The daemon is down by standing operator directive, so nothing
> in this file needs one. Where a lane and the fleet disagree, this file wins for
> you and `LANES.md` wins for the boundary between lanes.

Identity is `$HARMONIK_AGENT`. My state is `HANDOFF-$HARMONIK_AGENT.md` at the
root of my own checkout. My standing scope is
`.harmonik/crew/missions/$HARMONIK_AGENT.md`.

This is a working engineering session that resumes from a file. It is not a
dispatch loop. Nothing sends me work and nothing finishes it for me.

## On wake (fresh start or keeper restart — same ritual)

1. Read `HANDOFF-$HARMONIK_AGENT.md` at the root of the checkout you work in.
   It is gitignored, so it does not travel between checkouts and it never shows
   in `git status`. An absent or empty file is a real signal and not routine —
   the keeper does not empty it. Say so plainly, then rebuild what you can from
   `git log`, the mission file and your open beads, and get the operator's read
   before you change code.
2. Read the mission file for scope. Then `CHARTER.md` for intent, `LANES.md` for
   order, and `PRINCIPLES.md` before writing code.
3. **Find out where you are before you build.** Run `git rev-parse --show-toplevel`
   and `git branch --show-current`. The mission file and `LANES.md` §2 each name
   a branch and both have gone stale. The checkout you are in is the answer.
4. **The handoff is a claim, not ground truth.** Before acting on any "landed",
   "blocked", "open" or "next", check it live: `git merge-base --is-ancestor`
   for a claimed commit, `br show` for a bead, the real command for a claimed
   blocker. A bead's status and its newest comment are both stale in both
   directions.
5. Say back, in under ten lines: where things stand, the next step, and any real
   blocker. Translate every bead ID and codename into plain words. Then get to
   work — do not ask whether to continue.

## Where a lane works

**Alpha works in the main checkout at `/Users/gb/github/harmonik`, on the shared
integration branch. Every other lane works in its own worktree** under
`/Users/gb/github/harmonik-wt/<name>`, on its own branch. Alpha is the exception
because it is the lane that merges, and a merge needs the shared branch checked
out somewhere. `LANES.md` §5 owns this rule and gives the reasons.

Run your gates where you work. A gate run from the wrong checkout tests another
lane's branch and reports a result that means nothing for your change. Never
`cd` from one checkout to the other. Reach the other with `git -C` and an
absolute path.

**Generate this brief from the main checkout, whichever lane you are.** Run
`harmonik agent brief --project /Users/gb/github/harmonik`.

The same command aimed at a worktree does not fail. It succeeds at exit 0 and
serves whatever copy of this role sits on that lane's branch, which is normally
an older one. A silent stale contract is worse than a hard error: it boots you
on the defects the newer copy repairs and gives you nothing to notice. Until
this role lands everywhere at once, treat the main checkout as the only address
that gives you the current text. Refs `hk-jlb13`.

## Working

- **Delegate.** Work that splits — several files, independent parts, a search,
  an investigation — goes to sub-agents, and independent work goes out in one
  batch so it runs at the same time. Keep the main context for judgment. An
  independent review is a sub-agent too, and the review gate wants one.
- Commit with `git commit -F <file>` and explicit paths. Never `git add -A`.
- **Never run a command that can discard work you did not write.** No
  `--amend` on a commit another lane can already see, no `git reset --hard`, no
  `git checkout -- .`. Unstaging a file you staged by mistake is safe and is
  often the repair.
- Non-trivial commits carry `Reviewed-By:` and `Review-Verdict:` trailers. If you
  cannot reach a reviewer, record that absence in the trailer and commit anyway.
  Never write your own approval — the validator refuses an `APPROVE` whose
  reviewer says "self", and it accepts only `agent-reviewer` or
  `agent-config-reviewer` as the name. The absent-reviewer form is exact:

      Reviewed-By: none — no reviewer was reached for this commit
      Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": ["no-reviewer-reached"], "notes": "<what was verified instead, and by whom>"}

  A `BLOCK` verdict is never committed, and `--no-verify` is forbidden. Confirm
  `git diff` is empty for the reviewed files before you write the trailer — a
  review reads the working tree and a commit ships the index, and nothing makes
  them agree. A commit marked "not reviewed" is a state the next person can act
  on. Work stranded in a worktree is one `checkout` from gone.
- **Gates.** `make fast` while you work. `make full` is the merge decision and is
  what CI runs — it never scopes by what changed and it never approves on a
  timeout, an out-of-memory kill, a compile failure or a passing retry. Run
  `/check` after you commit; it runs those two targets and nothing else.
- **Nothing checks your commit message for you.** `/check` does not read it, and
  the git hooks are retired. `scripts/validate-commit-msg.sh` holds the rules.
  A subject over 72 characters fails, a bad trailer fails, and the type must be
  one of nine — `feat fix refactor test docs chore spec build perf`. `style`,
  `ci` and `revert` are not among them and are the easy mistake. Call the script
  yourself, on the message file, before you commit.
- Close your own beads once the fix is verified. The rule that the daemon owns
  terminal transitions applies when a daemon runs the work, and none does here.
- `br --db /Users/gb/github/harmonik/.beads/beads.db` — there is one ledger and
  it lives in the main checkout. A worktree carries an empty one, and a bare `br`
  inside a worktree silently forks a new database.

## Habits that have cost this project real time

- **Read the logged exit code, not the one the harness reports.** Redirect to a
  file and read `$?` there. In zsh `$PIPESTATUS` is not a thing — the array is
  `$pipestatus`, and the bash spelling silently evaluates to empty, which reads
  as success.
- **A test can pass against the defect it was written for.** Change the fix in
  place, watch the test go red, then put it back. If it stays green, the test is
  measuring nothing. Do this before believing your own work.
- **The branch moves under you while a suite runs.** Check `HEAD` before you
  commit.
- **A red gate is often another lane's.** Read the failure before you debug it.

## No daemon — standing operator directive

The daemon is down and stays down. Do not start it, and do not start a
supervisor. `harmonik queue submit`, `harmonik subscribe`, and
`harmonik comms join / leave / send / recv` all fail with "daemon not running".
You cannot send anything.

You can still read. `harmonik comms log --project /Users/gb/github/harmonik`
reads the event file directly, needs no daemon, and holds the record of what the
fleet did before it went down. Pass `--project` every time — it defaults to the
current directory, and from a worktree it finds an empty event file and reports
"no agent_message events found" at exit 0. That is the same silent false
negative as the empty bead ledger above. `harmonik comms who` also needs no
daemon, but it reports live presence only and knows no history.

Take care with the CLI itself. **Put the verb first.** A bare `harmonik` with no
arguments starts a daemon in the current directory, and so does any invocation
whose first token is a flag — `harmonik --project DIR queue list` drops the verb
and starts a daemon in `DIR`. A mistyped verb is now refused, so the hazard that
survives is the flag-first spelling. `--help` and `--version` are safe. Refs
`hk-cli-flag-first-starts-daemon-gjhiy`, which is the operator's bead.

There is no captain. Escalate to the operator, in this session, in plain words.

## Before handing off

Run `/session-handoff HANDOFF-$HARMONIK_AGENT.md`. Read the file before you write
it, so you carry forward what still matters.

**The keeper does not empty this file.** It removes only its own
`<!-- KEEPER:... -->` marker and leaves every other byte and the file mode alone.
With no marker present it does not write the file at all. See
`internal/keeper/cycle.go` `defaultScrubHandoffNonces`. The identifiers
`TruncateHandoffFn` and `ActTruncateHandoff` still carry the old name from when
it did truncate — read the body, not the name. The belief that the keeper zeroes
your handoff is false and has already cost real design work.
