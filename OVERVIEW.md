# Harmonik — Overview

## What it is

Harmonik is a background service that runs AI coding agents against your backlog for you. You write down work as small, self-contained tasks ("beads") in a local ledger, and Harmonik's **daemon** — a long-running per-project process — pulls each bead off a **queue**, runs a fresh AI coding agent (Claude Code) on it inside an isolated copy of your repo, has a second agent review the result, and merges the work back into your branch one bead at a time. You queue the work; the daemon does the dispatching, reviewing, and merging without you sitting over each one.

## Why it exists

Running one AI coding agent is easy. Running ten of them against a backlog is not — somebody has to babysit each session, decide what to work on next, keep the agents from stepping on each other's changes, review what they produced, and merge it all without conflicts. That babysitting is the bottleneck.

Harmonik's design splits the problem into two halves. The **deterministic skeleton** — the daemon, the queue, the merge pipeline, the review step — is plain, predictable Go code you can reason about. The **probabilistic organs** — the AI agents that actually write and review code — are the part that scales. The skeleton's whole job is to make the unpredictable agents safe to run unattended: isolated worktrees so they can't collide, a merge that happens one bead at a time so there are no merge races, automatic skipping of any change that won't merge cleanly, and a review pass before anything lands. You keep feeding the queue while finished work merges in the background.

## Who it's for

A developer or operator who wants to point a queue of work at AI agents **on their own machine** and walk away. If you maintain a backlog of well-scoped tasks, are comfortable on the command line, and would rather supervise a queue than supervise individual agent sessions, this is built for you. It is a single-machine tool for personal and small-team workflows, not a hosted multi-user platform.

## Mental model

Work flows in one direction, from a tracked task to a merged, closed bead:

```
   ┌──────────┐     ┌─────────┐     ┌──────────────┐     ┌─────────────────────┐
   │  bead in │     │  queue  │     │    daemon    │     │  isolated worktree   │
   │  ledger  │ ──▶ │         │ ──▶ │  dispatches  │ ──▶ │  AI agent does the   │
   │  (`br`)  │     │         │     │  one bead    │     │  work, commits       │
   └──────────┘     └─────────┘     └──────────────┘     └──────────┬──────────┘
                                                                    │
                                                                    ▼
   ┌──────────┐     ┌────────────────────────┐     ┌───────────────────────────┐
   │   bead   │     │  one-at-a-time merge   │     │       review-loop          │
   │  closed  │ ◀── │  to target branch      │ ◀── │  reviewer agent checks the │
   │          │     │  (conflicts skipped)   │     │  commit (on by default)    │
   └──────────┘     └────────────────────────┘     └───────────────────────────┘
```

You add beads and submit them to a queue. The daemon picks them up, spins up a separate git worktree per bead, and an AI agent works there in isolation. A reviewer agent checks each commit (this **review-loop** is on by default and can approve, request changes, or block). Approved work merges into your target branch one bead at a time — any change that would conflict is skipped rather than forced — and the bead is marked closed. Sessions run in tmux, so you can attach and watch, and every step is logged as plain events you can read.

## Honest maturity and limits

This is a young project. The human-facing docs (including this one) are new. Be aware of the following before you point it at a repo:

- **Single machine only.** There is no multi-host or distributed mode. Everything runs on one box.
- **The implementer is Claude Code, billed against a Claude subscription** — not an API key. Credential guards exist specifically to avoid accidentally burning API credits, but you do need a working Claude Code setup.
- **The daemon auto-pushes `main` by default.** On every successful bead it merges and pushes to your repo's `main`. Until you configure an **integration branch** (a separate target branch, with `main` added to a protect list), do **not** run it against a work repo or any branch that must not be auto-committed. Personal or throwaway repos where auto-pushing `main` is fine are safe to start with. See CONFIGURATION.md for how to protect `main`.
- **Required tools:** Go, tmux, Claude Code, and the `br` beads ledger CLI. The `kerf` planning layer is **optional** — the core loop runs without it. See INSTALL.md.
- **Some features are still in flight**, including a recurring-job scheduler and a human-in-the-loop decision surface. Today's flow is queue-and-go.

## Where to go next

- **New to the vocabulary?** Read CONCEPTS.md for the terms — daemon, bead, queue, worktree, review-loop, crew, captain, and the rest.
- **Ready to set it up?** Follow INSTALL.md to install the prerequisites and Harmonik itself.
- **Want to run something?** QUICKSTART.md walks you through dispatching your first bead end to end.
- For day-2 operation see OPERATING-GUIDE.md, the full command list in CLI-REFERENCE.md, and branch-protection setup in CONFIGURATION.md.
