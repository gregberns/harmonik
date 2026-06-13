# Quickstart

Run your first bead end-to-end. This is the shortest path from a clean install to a finished, merged change — about ten minutes.

> **Before you start:** finish [INSTALL.md](INSTALL.md). You need `harmonik`, `br`, `claude`, `git`, and `tmux` on your `PATH`. Verify with `harmonik version` and `br --version`.
>
> **One-time per project:** if you haven't already, initialize the repo for harmonik with `harmonik init` (it creates `.harmonik/`, the bead database, and the config files). See [INSTALL.md](INSTALL.md). This walkthrough assumes that's done.

> **Read this once:** by default the daemon **auto-pushes `main`** on every successful bead. If that is not acceptable for your repo, set up an integration branch **before** you submit real work — see [OPERATING-GUIDE.md](OPERATING-GUIDE.md). For this walkthrough, use a personal or throwaway repo where auto-pushing `main` is fine.

---

## 1. Be in your project repo

The daemon operates on a git repository.

```bash
cd /path/to/your/repo
git status          # confirms you're in a git repo
```

## 2. Start one persistent daemon

Start the daemon **once**, detached in a tmux session, so it outlives your terminal. Run only one per project.

```bash
tmux new-session -d -s harmonik-daemon \
  'harmonik --project /path/to/your/repo --no-auto-pull --max-concurrent 4'
```

- `--no-auto-pull` — **queue-only** (this is already the default; passing it makes the intent explicit): the daemon runs only the beads you explicitly submit and never auto-drains your whole ledger.
- `--max-concurrent 4` — how many beads run in parallel. 4–5 is a good ceiling on a 10-core machine; higher oversubscribes cores and can fill the disk.

Confirm it came up:

```bash
harmonik queue status
```

Exit code **17** means the daemon isn't running (start it again). If you try to start a *second* daemon for the same project, that start fails with exit code **5** — only one daemon per project is allowed, so just use the one that's already up.

## 3. Create a bead

A bead is one unit of work. Pick something trivial for your first run.

```bash
br create --title="Add a hello comment to README" --type=task --priority=2
# prints an ID, e.g.: hk-abc12
```

Note the printed ID.

## 4. Submit it

Hand the bead's ID straight to the queue — no JSON file needed.

```bash
harmonik queue submit --beads hk-abc12
```

Submitting several at once, or to a named queue:

```bash
harmonik queue submit --beads hk-abc,hk-def,hk-ghi
harmonik queue submit --queue myqueue --beads hk-a,hk-b
```

## 5. Watch it run

Submit returns immediately and does **not** block. Stream events to watch progress:

```bash
harmonik subscribe --types run_completed,run_failed
```

Output is one JSON object per line (NDJSON) by default. You'll see a `run_completed` event when the bead finishes, or `run_failed` (with a `failure_class` field) if something went wrong. Press Ctrl-C to stop watching.

## 6. What success looks like

For each submitted bead, the daemon automatically:

1. Spins up an isolated git **worktree** and runs a Claude Code agent on the bead.
2. Runs a **reviewer** agent that approves, requests changes, or blocks the work.
3. **Merges** approved work back to your target branch — one bead at a time, so there are no merge races.
4. Pushes, then **closes the bead** in the ledger.

When you see `run_completed`, check the result:

```bash
br show hk-abc12       # status should be closed
git log -1            # the merged commit
```

That's a full bead lifecycle with no human in the loop.

---

## Next steps

- **Protect `main` / use an integration branch** (do this before real work): [OPERATING-GUIDE.md](OPERATING-GUIDE.md)
- **All commands and flags:** [CLI-REFERENCE.md](CLI-REFERENCE.md)
- **Tuning the daemon and config files:** [CONFIGURATION.md](CONFIGURATION.md)
- **How the pieces fit together:** [CONCEPTS.md](CONCEPTS.md) and [OVERVIEW.md](OVERVIEW.md)
