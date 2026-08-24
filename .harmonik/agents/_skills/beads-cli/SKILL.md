---
name: beads-cli
description: >
  The `br` (Beads) task ledger: the read surface agents use, and the write
  discipline they follow — whoever runs the work owns the terminal transitions.
  Load-bearing: must not rot.
---

<!-- SOURCE OF TRUTH: cmd/harmonik/assets/skills/beads-cli/SKILL.md (Go //go:embed).
     The copy at .claude/skills/beads-cli/SKILL.md is GENERATED OUTPUT — `harmonik sync-assets`
     overwrites it from the embed and there is NO reverse sync, so an edit made
     only there silently drifts and is eventually reverted. Edit the cmd/harmonik/assets/
     copy, then mirror it byte-for-byte into .claude/skills/ in the SAME commit. -->

# Beads-CLI Skill

Beads is harmonik's task ledger, reached through the `br` CLI. This skill is the
surface available to you and the discipline you follow when you write.

## Write discipline (read this first)

**Who runs the work owns its terminal transitions.** `claim` (open to
in_progress), `close` (in_progress to closed) and `reopen` are the terminal
transitions. One question decides whether any of them is yours: **did I submit
this bead to a queue?** Ask that, not "is it dispatched" — you cannot observe
dispatch, but you always know what you submitted.

**A bead you submit to a queue belongs to the daemon.** It claims the bead when
it dispatches and closes it when the work merges. Do not pre-set `in_progress`:
`queue submit` refuses that bead with `bead_already_dispatched` (JSON-RPC
`-32015`), prints the refusal, and exits 1, so the work never reaches the queue.
Do not `br close` it by hand either — a close from inside a worktree leaks to the
parent repo even when no code landed, so the ledger claims done over work that
does not exist. Bypassing the daemon's adapter also breaks the idempotency and
intent-log contracts it depends on.

**A bead you work by hand you close yourself, because nothing else will.**
`harmonik reconcile` closes only beads whose commit carries a
`Harmonik-Bead-ID:` trailer, and a hand commit never carries one — so a crew
orchestrator that fixes something inline, a hand-run delivery lane, a solo
session or the operator gets no help from any sweep. An open bead over finished
work is a lie in the ledger that costs the next reader a reconcile pass.

**The one genuinely silent failure is claiming and then not submitting.** Both
hazards above are loud. Claim a bead by hand and then never submit it, and
nothing reports anything at all — no run exists, so no watcher, no timeout and
no sweep has anything to notice. If you claim, either submit it or work it
through.

**Do not settle which case you are in from what looks idle.** Whether anything
drains your queue right now is the race this rule prevents, and a quiet queue is
not evidence. Your mission file or your role contract names your lane; read it
there.

What you MAY write on any lane is metadata:

| Operation | Command |
|---|---|
| Add a comment | `br comments add <bead_id> --message "..."` |
| Add or remove a label | `br update <bead_id> --add-label <l>` / `--remove-label <l>` |
| Update notes | `br update <bead_id> --notes "..."` |

## Read surface

### Check available work

```bash
br ready --format json --limit 0                      # everything dispatchable now
br ready --format json --limit 0 --sort priority      # by the br priority field
br ready --format json --limit 0 --sort oldest        # surfaces work that is starving
br ready --format json --limit 0 --parent <epic_id>   # scope to one lane
br ready --format json --limit 0 -l scope:x -l kind:y # label filters, AND logic
```

**`br ready` means dispatchable-now, not is-there-work.** Always pass `--limit 0`:
a bare `br ready` caps at 20 rows and silently makes the backlog look shorter, or
empty, when it is not. The default sort is `hybrid`, so ask for the ordering you
actually want. A bead correctly hidden from `br ready` — blocked, in-progress,
draft — is still work: never read an empty result as "drained" without also
checking in-progress beads, beads blocked by an open epic, and paused or failed
queues.

`br ready --sort priority` is where an ordering of the *unclaimed backlog* comes
from. Above that line, priority comes from stated intent — the named initiatives
of the operator and the admiral, which no ledger query returns.

### Inspect, list, search

```bash
br show <bead_id> [<bead_id2> ...] --format json
br list --format json -s open -l scope:x        # by status and label
br list --format json --label codename:<epic>   # children of an epic
br search --format json "keyword"
br count --by status                            # scalar text; no --format json
br dep cycles | br dep list <id> | br dep tree <id>
```

## Output format

Pass `--format json` to every `br` invocation that offers it, and parse only
that. The text layout is presentation: it re-flows on a column change or a
version bump, so a parser built on it breaks silently and reports the wrong state
rather than an error.

**The rule is about parsing, not about the flag.** A few subcommands emit a
scalar and support no `--format` at all — `br count` is the one you will meet.
Read its scalar directly and do not pipe it to `jq`. If a command you need has no
JSON form, that is a gap worth a bead, not a licence to regex a table.

`br schema issue` / `issue-details` / `ready-issue` emit JSON Schema for the
response shapes if you need them.

## What agents should not do

- Do NOT issue a terminal transition on a bead you submitted to a queue — see
  § Write discipline.
- Do NOT parse `br` text output.
- Do NOT mint, parse, or rewrite bead IDs. They are opaque strings owned by
  Beads.
- Do NOT use `br` to track transitions *inside* a run. Those live in the git
  checkpoint trail and the JSONL event log.
- Do NOT report a failing `br` command as a version problem. harmonik checks only
  that `br` is present and runnable; any version is accepted. Report the failure
  itself.
