---
name: harmonik-lifecycle
description: >
  The four commands that operate on a harmonik deployment itself — `init`,
  `supervise`, `reconcile`, `promote`. Load-bearing: must not rot.
---

<!-- SOURCE OF TRUTH: cmd/harmonik/assets/skills/harmonik-lifecycle/SKILL.md (Go //go:embed).
     The copy at .claude/skills/harmonik-lifecycle/SKILL.md is GENERATED OUTPUT — `harmonik sync-assets`
     overwrites it from the embed and there is NO reverse sync, so an edit made
     only there silently drifts and is eventually reverted. Edit the cmd/harmonik/assets/
     copy, then mirror it byte-for-byte into .claude/skills/ in the SAME commit. -->

# harmonik lifecycle / operator surface

These four commands operate on the **deployment itself** — the project, the
daemon process, the ledger's drift against git, and the integration-to-main
boundary. The per-task dispatch loop is the **harmonik-dispatch** skill; the
per-session context watcher is the **keeper** skill.

| command | when you reach for it |
|---|---|
| `harmonik init` | standing up harmonik on a NEW repo |
| `harmonik supervise` | start, stop or inspect the supervisor; pause or resume dispatch |
| `harmonik reconcile` | a bead is stuck `in_progress` though its work merged |
| `harmonik promote` | land a reviewed SHA, or open the integration-to-main PR |

**Always pass `--project` explicitly.** Only `promote` falls back to
`$HARMONIK_PROJECT`. `init`, `reconcile` and every `supervise` verb resolve the
flag or the cwd and never read the env var, so an orchestrator whose CWD drifted
operates on the wrong project without noticing.

## § `harmonik supervise` — the supervisor

The supervisor is a long-lived process in a detached tmux session. Its
load-bearing job, under `--watch-restart`, is **auto-revival of the daemon**: a
watchdog probes the daemon's Unix socket and respawns it detached when it finds
it dead. It revives with `--no-auto-pull`, the queue-only safe default.

Without `--watch-restart` and with a supervisee configured, the shim
exec-replaces itself with the supervisee and there is **no watchdog**. With no
supervisee configured at all, the watchdog runs on its own regardless of the
flag — the expected state once the operator has dropped the cognition command.

> **After a rebuild and restart the socket takes about 30 seconds to a minute to
> appear, and `supervise status` reports `(no socket)` for that whole window.**
> That is normal. The watchdog's revive window is deliberately sized to cover the
> daemon's boot backoff. Do not declare the daemon dead from one snapshot.

Verbs: `start | stop | status | ps | attach | restart | logs | pause | resume | reap`.

- **`start`** probes the socket, takes a flock, refuses if a flywheel session
  already exists, and creates the tmux session. `--require-api-key` fails closed
  when no key resolves; without it an empty key is allowed so the holder can auth
  over OAuth.
- **`stop`** SIGTERMs then SIGKILLs the supervisor and kills the tmux session's
  whole child tree. Idempotent — a missing pidfile exits 0. It does **not** kill
  an already-revived daemon: the daemon is spawned detached precisely so a
  SIGTERM to the pane does not cascade.
- **`status`** is a file-surface read; it does not connect to the daemon socket.
  It falls back to a process and tmux signature probe when the pidfile is stale.
- **`restart`** is stop, validate `config.json`, start. **Config is re-read, not
  hot-reloaded** — parameter changes take effect only here. This is the standard
  step after `go install`.
- **`pause` / `resume`** talk to the **daemon**, not the supervisor. `pause`
  blocks new dispatch and lets in-flight runs drain.
- **`logs`** captures the flywheel pane; **`attach`** execve-replaces you into
  it; **`reap`** clears dead orphan sessions, which `start` also does at boot.

Exit codes worth branching on: **17** the daemon is not running; **24** a
flywheel tmux session already exists; **25** the supervisor is already running.
Recover from either of the last two with `harmonik supervise stop` first.

## § `harmonik promote` — crossing the integration-to-main boundary

The daemon never auto-merges into the protected branch. `promote` is the tool
that does it, in one of two modes, and the two are mutually exclusive.

**Push-mode — `harmonik promote <sha>...`** cherry-picks the reviewed SHAs onto
the target in a temp worktree rooted at the fetched remote tip, runs a build gate
(`go build` and `go vet`, when a `go.mod` is present), and pushes race-safely,
retrying a lost compare-and-swap but failing at once on a refusal that will never
clear, such as a declined hook. The cherry-pick records provenance with `-x` and
is amended with a `Harmonik-Bead-ID:` trailer — from `--bead`, else auto-detected
from a `(hk-xxx)` parenthetical in the subject. That trailer is what lets
`reconcile` close the bead later.

**PR-mode — `harmonik promote --pr`** opens a PR from `--from` (default
`integration`) onto the target with `gh pr create`, and never pushes. It needs
the `gh` CLI on PATH. This is the mode for the human integration-to-main review.

**The protection gate is fail-closed.** If the resolved target is in the
project's `protect_branches` (from `.harmonik/branching.yaml` or
`--protect-branch`), push-mode is refused with **exit 5** and a pointer to
`--pr`. A protected target can only be promoted through a PR. Other exits: 2 a
cherry-pick conflict, 3 the build gate failed, 4 the push exhausted its retries.

`--target` defaults to `branching.yaml` `lands_on`. `--dry-run` prints the plan
and mutates nothing.

### Salvaging a `context_cancelled` run

When a run fails with `failure_class: context_cancelled` but the implementer
already committed — a `Refs: <bead_id>` commit exists on `run/<run_id>` — the
work is done and the SHA is immutable. **Do not re-dispatch.**

```bash
git log --oneline run/<run_id> | head -5          # find the SHA
git checkout --detach <sha> && go build ./... && go test -short ./... && git checkout -
harmonik promote --project $HARMONIK_PROJECT <sha>
br close <bead_id> --reason "Salvaged: context_cancelled; SHA promoted"
```

This is not a gate bypass — the build gate runs as normal and the commit carries
its `Refs:` trailer. Full procedure: `docs/known-workarounds.md`
§ Salvage-promote.

## § `harmonik reconcile` — close beads whose work already merged

Lists every bead in coarse status `in_progress`, scans the target branch's git
log for a commit carrying `Harmonik-Bead-ID: <bead_id>`, and closes any bead
whose work it finds. It is a mutating command. It reports
`closed=N skipped=N failed=N` on **stderr**.

```bash
harmonik reconcile --project $HARMONIK_PROJECT --target-branch <branch>
```

`--run RUN_ID` scopes the scan to the one in-flight bead tied to that run. Zero
in-flight beads and zero matches are both success (exit 0); exit 2 means at least
one close failed. The daemon runs the same sweep itself, so a manual reconcile is
mostly for the operator-triggered case; the race between them is benign because
`br close` is idempotent.

## § `harmonik init` — one-time project bootstrap

Run once on a brand-new repo. `harmonik init --doctor` runs the precondition
checks (git repo, `br` and `harmonik` on PATH) and mutates nothing — do that
first, then `harmonik init --smoke`.

It scaffolds `.harmonik/` and its runtime gitignore, runs `br init` with a prefix
derived from the directory name, writes `config.yaml` (including a complete
`keeper:` block, since harmonik ships no keeper defaults) and `branching.yaml`,
provisions the embedded fleet skills into `.claude/skills/`, seeds the context
tiers and `HANDOFF.md`, renders `AGENTS.md` from the embedded template, symlinks
`CLAUDE.md` to it, and starts the supervisor unless `--no-supervise`.

**Every step is skipped when its artifact already exists**, so a re-run on a
partly-initialized project is safe. `--force` overwrites. Flag parsing is
fail-closed: an unknown argument exits 1, so a typo cannot silently bootstrap
against defaults.

There is **no `--target-branch == main` guard**. Earlier versions of this skill
said there was. `init` passes the value straight through; the real fail-closed
enforcement is the daemon's boot guard, and `harmonik init --target-branch
integration` is supported and in the command's own examples.

A re-run prints a stale-assets hint when managed files are behind the running
binary. That is how it tells you to run `harmonik sync-assets`.

## § Quick reference

```bash
# Stand up a NEW project
harmonik init --doctor --project /path/to/repo
harmonik init --smoke  --project /path/to/repo

# Deploy a rebuilt binary (expect "(no socket)" for ~30s-1m afterwards)
go install ./cmd/harmonik
harmonik supervise restart --project $HARMONIK_PROJECT --watch-restart

# Inspect / drain / resume
harmonik supervise status --project $HARMONIK_PROJECT --json
harmonik supervise logs   --project $HARMONIK_PROJECT --lines 500
harmonik supervise pause  --project $HARMONIK_PROJECT
harmonik supervise resume --project $HARMONIK_PROJECT

# Close a bead whose work merged but is stuck in_progress
harmonik reconcile --project $HARMONIK_PROJECT --target-branch main

# Promote reviewed work
harmonik promote --dry-run abc1234
harmonik promote --target integration abc1234
harmonik promote --pr --from integration --title "Sprint 23"
```
