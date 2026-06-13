# Harmonik

> **SAFETY — read before pointing the daemon at any repo.**
> By default the daemon **merges and pushes to `origin/main` on every successful bead**.
> To target an integration branch and protect `main`, configure `.harmonik/branching.yaml`
> `protect_branches` (or pass `--target-branch`/`--protect-branch`/`--forbid-default-main`);
> the daemon then **fail-closes** and refuses to push `main`.
> Until you configure that, **do NOT run against a work repo or any branch that must not be
> auto-committed**. Personal/throwaway repos where auto-pushing `main` is acceptable are fine.

---

## What is Harmonik?

Harmonik is an **agentic orchestration daemon** for software projects. You describe work as
*beads* (discrete, self-contained tasks stored in a local SQLite ledger via the `br` CLI), and
harmonik dispatches each bead to a Claude Code session running in an isolated git worktree,
merges the result back to your target branch **one-at-a-time** (no merge races), and pushes —
all without human intervention.

The core thesis: a **deterministic Go skeleton** (daemon, queue, merge pipeline, review loop)
wrapping **probabilistic organs** (LLM implementer + reviewer sessions). The daemon is the part
you can reason about; the agents are the part that scales.

### Why

- **Parallel implementation at scale.** Queue 20 beads; the daemon fans them out to N concurrent
  Claude sessions, each in its own worktree, then serialises the merges. You keep dispatching
  while work lands.
- **Review by default.** Every bead goes through an implementer session then a reviewer session.
  The reviewer can approve, request changes (triggering iteration), or block. The review loop is
  enforced by the daemon, not by convention.
- **Safe merge pipeline.** Integration-branch targeting and `protect_branches` mean the daemon
  can fail-closed on your protected refs. You configure it once; every subsequent bead respects it.
- **Inspectable.** Sessions run in tmux; you can attach and watch. Events land in
  `.harmonik/events/events.jsonl` as NDJSON. Nothing is hidden inside an opaque cloud.

---

## Documentation

New here? Read these in order, then keep the references on hand.

- **[OVERVIEW.md](OVERVIEW.md)** — what harmonik is, who it's for, and its honest limits.
- **[CONCEPTS.md](CONCEPTS.md)** — the core ideas (daemon, beads, queues, worktrees, review-loop, crews) in plain English.
- **[INSTALL.md](INSTALL.md)** — prerequisites and setup.
- **[QUICKSTART.md](QUICKSTART.md)** — run your first bead end to end.
- **[CLI-REFERENCE.md](CLI-REFERENCE.md)** — every command, flag, and exit code.
- **[OPERATING-GUIDE.md](OPERATING-GUIDE.md)** — day-2 operations and troubleshooting.
- **[CONFIGURATION.md](CONFIGURATION.md)** — every config key, daemon flag, and environment variable.

---

## Prerequisites

| Tool | Purpose | Install |
|------|---------|---------|
| Go 1.22+ | Build harmonik | https://go.dev/dl/ |
| tmux | Session substrate | `brew install tmux` / `apt install tmux` |
| Claude Code CLI (`claude`) | Agent runtime | https://claude.ai/code |
| `br` (beads_rust) | Task ledger (required) | See below |
| `kerf` | Planning / prioritization (optional) | See below |

### Install `br` (beads_rust)

`br` is the task ledger CLI. harmonik reads and writes bead state through it.

```bash
cargo install --git https://github.com/Dicklesworthstone/beads_rust
```

> **Note:** this install path has not been verified on a clean machine. If it fails, check the
> [beads_rust README](https://github.com/Dicklesworthstone/beads_rust) for the current install
> instructions. The installed binary is `br`.

### Install harmonik

```bash
git clone https://github.com/your-org/harmonik   # or wherever you have it
cd harmonik
go install ./cmd/harmonik
```

Verify:

```bash
harmonik version
br --version
```

### Install `kerf` (optional)

`kerf` is the planning and prioritization layer. The core daemon loop runs without it — you can
submit beads directly via `harmonik queue submit`. Install it only if you want ranked feeds
(`kerf next`) and structured planning passes.

```bash
# kerf is an internal sibling tool; see docs/components/internal/kerf.md for details
go install ./cmd/kerf   # from the kerf source tree
```

---

## Quickstart (minimal happy path)

### 1. Bootstrap a project

```bash
cd /path/to/your/repo
harmonik init
```

`harmonik init` creates `.harmonik/` (gitignored runtime state), an initial `.harmonik/branching.yaml`,
and scaffolds the bead ledger. Follow the prompts.

### 2. Configure branch protection (recommended)

Edit `.harmonik/branching.yaml` to protect `main` and target an integration branch:

```yaml
version: 1
defaults:
  lands_on: harmonik-integration
  protect_branches:
    - main
```

Or pass flags at daemon start:

```bash
--target-branch harmonik-integration \
--protect-branch main \
--forbid-default-main
```

With `protect_branches` set, the daemon fail-closes and will not push `main` under any condition.

### 3. Start the daemon

Run the daemon in a detached tmux session so it survives your terminal:

```bash
tmux new-session -d -s harmonik-daemon \
  'harmonik --project /path/to/your/repo --no-auto-pull --max-concurrent 4'
```

- `--no-auto-pull` — queue-only mode. The daemon dispatches only work you explicitly submit;
  it will not auto-drain the ledger.
- `--max-concurrent 4` — parallel dispatch ceiling. 4–5 is the recommended ceiling on a
  10-core machine; wider oversubscribes cores and can exhaust disk.

Confirm it is running:

```bash
harmonik queue status
```

### 4. Create a bead

```bash
br create --title="Fix the login redirect" --type=task --priority=2
# prints something like: hk-abc12
```

### 5. Submit a batch

Submit beads straight to the running daemon by id — no file needed:

```bash
harmonik queue dry-run --beads hk-abc12              # validate without persisting
harmonik queue submit  --beads hk-abc12              # accept; prints queue_id
```

Submit several at once, optionally to a named queue:

```bash
harmonik queue submit --beads hk-abc,hk-def,hk-ghi   # multiple beads to the default queue
harmonik queue submit --queue myqueue --beads hk-abc,hk-def
```

Absent `--queue`, beads go to the `main` queue. For advanced submits (multiple groups, or
mixing `wave` and `stream` kinds in one request) you can still hand a JSON `QueueSubmitRequest`
file to `submit` / `dry-run` (`harmonik queue submit /tmp/batch.json`), but the `--beads` form
is the normal path.

### 6. Monitor progress

```bash
harmonik subscribe \
  --types run_completed,run_failed,run_stale,heartbeat \
  --heartbeat 60s \
  --json
```

Each completed bead prints a `run_completed` event. Failures print `run_failed` with a
`failure_class` field (e.g. `no_commit`, `context_cancelled`).

---

## Key concepts

| Term | Meaning |
|------|---------|
| **Bead** | A discrete unit of work stored in the `br` ledger (`br show <id>`) |
| **Run** | One daemon dispatch of a bead: worktree spawn → implementer → reviewer → merge → push |
| **Wave** | A fixed, concurrent batch (immutable after submit) |
| **Stream** | A queue group that accepts mid-flight appends while active |
| **Worktree** | Isolated `git worktree add` clone where the implementer session runs |
| **Review loop** | Implementer commits → reviewer evaluates → APPROVE/REQUEST_CHANGES/BLOCK |

---

## For agent operators

These docs are for setting up and running **automated agent workflows** (implementers, reviewers, orchestrators) — not the human-facing getting-started path above.

- **[AGENT_OPERATING_MANUAL.md](AGENT_OPERATING_MANUAL.md)** — operating manual for agents
  (implementers, reviewers, orchestrators): session discipline, bead lifecycle, monitoring patterns,
  daily loop. Start here if you are setting up an automated agent workflow.
- **[AGENT_INDEX.md](AGENT_INDEX.md)** — master map of the knowledge base; every doc is
  reachable from here within two hops.
- **[AGENTS.md](AGENTS.md)** — agent instructions (same file as `CLAUDE.md`): the full daily
  loop, submission protocol, monitoring patterns, and multi-agent coordination.
- **[docs/components/internal/kerf.md](docs/components/internal/kerf.md)** — kerf planning tool
  reference (optional layer).
- **[specs/](specs/)** — normative specs; the spec is always right, code matches it.

---

## Milestones

- **2026-05-14 — Phase 1 OPERATIONAL GREEN**: harmonik runs Claude end-to-end on a bead with
  zero human input (smoke v13).
- **2026-06-03 — Integration-branch protection landed**: `protect_branches` + `--target-branch` /
  `--protect-branch` / `--forbid-default-main` flags enforced fail-closed at boot, dispatch,
  and in-merge.
