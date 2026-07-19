# remote-substrate — Phase 1 Design: Remote Mac over SSH

> The first concrete build (renamed from "Phase 0" to drop the awkward zero).
> Connect-in · bead-work-only · per-box. Crews/daemon/captain stay on box A (D3).
> Auth = subscription via one-time interactive login (D2). Grounded in the
> `handler.Substrate` seam (RESEARCH-NOTES.md §R6).

## Topology

```
┌─ Box A — "the harmonik box" (your 16 GB Mac) ─────────────────────┐
│  • harmonik daemon  (control plane: queue, dispatch, MERGE-to-main)│
│  • captain + ALL crew Claude sessions (local)                      │
│  • comms bus  (local unix socket — UNCHANGED, no network transport)│
│  • authoritative git repo (main)                                   │
└───────────────────────────┬───────────────────────────────────────┘
                            │  Tailscale tailnet (WireGuard) + SSH
              ┌─────────────┴──────────────┐
       ┌──────▼───────┐            ┌────────▼───────┐
       │ worker-mac-1 │            │ worker-mac-2   │  (spare MacBook Pros)
       │ • git clone  │            │ • git clone    │
       │ • tmux       │            │ • tmux         │
       │ • claude (interactively logged in → subscription billing)   │
       │ • per-bead worktrees under .harmonik/worktrees/<run_id>/    │
       └──────────────┘            └────────────────┘
```

Control plane stays entirely on box A. The ONLY thing that relocates is the per-bead
implementer Claude session + its worktree + its build/test gate. This is the
control-plane/data-plane split that all prior art converges on (R1).

## One-time worker setup (per spare Mac)

1. Install toolchain: `git`, `tmux`, Claude Code CLI, Go (so the bead's build/test gate
   runs on the worker), `gh`.
2. `tailscale up` → stable name `worker-mac-1`, reachable from box A over WireGuard.
3. **`claude` interactive login ONCE** → credential persists in Keychain/`~/.claude` →
   every session on this box bills the **subscription** (satisfies D2). Never put
   `ANTHROPIC_API_KEY` in the environment (C2 — API key would win and silently bill credits).
4. Clone the repo to a known path, e.g. `~/harmonik-worker/repo` (or the daemon clones on
   first use).
5. (Optional) author/maintain the box with `adze` (drift-capture beats a one-shot script
   for a long-lived pet worker — R7).

## The code change — three seams (R6)

v1 needs only seams **A, B, C**. Seam D (network bus transport) and keeper-remoting are
NOT needed because crews stay local (D3).

- **A. A second `handler.Substrate` impl — `sshRemoteSubstrate`.** `SpawnWindow(ctx,
  SubstrateSpawn{Cwd,Env,Argv})` runs the *same* tmux commands as today, prefixed with
  `ssh worker-mac-1 --`:
  `ssh worker-mac-1 -- tmux new-window -P -F '#{pane_id}' -d -t <sess>: -n <win> -c <remote-cwd> -e K=V -- <argv>`.
  The daemon already injects the substrate via `LaunchSpec.Substrate` — this is the single
  seam that makes remote spawn work.
- **B. Paste-inject + liveness over the same SSH.** `pasteInjecter`/`enterSender`/
  `quitSender` → `ssh … tmux load-buffer/paste-buffer/send-keys`. Liveness
  (`paneLivenessChecker`, the raw `pgrep -P`/`ps -o comm=`, and `tmuxSubstrateSession.Wait`'s
  PID poll) → `ssh … pgrep/ps`. Reuse the existing pasteinject auto-recovery logic verbatim,
  just over the wire.
- **C. Remote worktree provider.** `workspace.CreateWorktree`/`RemoveWorktree`/`WorktreePath`
  become an interface the substrate provides; the remote impl runs `ssh … git -C <repo>
  worktree add/remove …` on the worker.

**SSH efficiency:** OpenSSH `ControlMaster` + `ControlPersist` so the per-bead chatter
(spawn + paste + dozens of liveness polls) rides ONE persistent multiplexed connection per
worker — no per-call handshake. (R8.)

**Worker registry:** a config file lists workers, their reachable name, and slot capacity.
The daemon's scheduler picks a worker with a free slot per bead (falls back to local-tmux if
none free / all workers offline). NFR7: with zero workers configured, behavior == today.

## Per-bead lifecycle (end to end)

1. **Pick:** daemon pulls a bead from the queue, picks a worker with a free slot.
2. **Sync base:** the worker's clone must have box A's current `main` SHA as the worktree
   base. (See "Code sync" decision below.)
3. **Worktree:** `ssh worker -- git -C <repo> worktree add -b run/<run_id>
   .harmonik/worktrees/<run_id> <base-sha>` (seam C).
4. **Spawn:** `sshRemoteSubstrate.SpawnWindow` → `ssh worker -- tmux new-window … -- claude …`
   in the remote worktree cwd. Auth = the worker's own logged-in credential (NOT injected →
   subscription, D2). No API key in env.
5. **Kickoff:** paste-inject the bead prompt over SSH (seam B).
6. **Run + gate:** the implementer Claude works AND runs the build/test gate **on the
   worker** — this is the whole point of offloading (box A stops thrashing).
7. **Monitor:** liveness polls over SSH; a heartbeat/lease TTL miss (worker dead / partition)
   → existing `run_stale` path → recover.
8. **Commit detect:** `ssh worker -- git -C <worktree> log` finds the `Refs: <bead>` commit.
9. **Result out:** worker pushes branch `run/<run_id>` to the shared git remote (or box A
   fetches it over the tailnet — see decision).
10. **Merge (UNCHANGED — stays on box A):** box A's daemon fetches the branch and does the
    one-at-a-time merge to main exactly as today (ff/merge, push, auto-skip on conflict).
    Centralizing the merge on box A is what keeps "no merge races" true. We do NOT distribute
    merging.
11. **Review loop:** runs on box A against the fetched branch (simplest for v1; reviewer is
    cheap). Could move to the worker later.
12. **Teardown:** `ssh worker -- git worktree remove --force <worktree>`; free the slot.

## Auth & billing (D2 satisfied)

The worker Mac's one-time interactive login → subscription billing for every remote session,
no per-bead credential handling. This is the only billing-safe path for a *persistent owned
Mac* and it is officially supported. The daemon must guarantee `ANTHROPIC_API_KEY` is absent
from the spawned env (a fail-closed check, echoing the credfence work).

## Networking / security

- Tailscale tailnet box A ↔ workers (LAN now; internet / work-box later, same model).
  Tailscale SSH removes key management (identity-based, deny-by-default).
- **Egress:** on a *bare* remote Mac the session has open network (which is fine — some beads
  need network for integration tests, operator's point). True two-phase / locked-down egress
  is a **Phase 2 (container)** capability, not available on a bare Mac. Phase 1 egress ≈ today.
- Secrets: nothing per-bead is injected in Phase 1 (the worker is pre-logged-in), so the
  R1/R8 "broker secrets outside the sandbox" concern is naturally satisfied — there's no
  token to leak into the worktree.

## Failure model

- Worker offline at dispatch → scheduler skips it (другой worker, or local fallback).
- Worker dies mid-bead → liveness TTL miss → `run_stale` → re-queue fresh / clean-fail;
  orphaned remote worktree GC'd on the next orphan sweep (run over SSH).
- Network partition → looks like worker death from box A; on reconnect, reconcile (did a
  commit land? did it push?) before re-dispatching (avoid the dup-work trap).

## Worker registry, health-check & live-disable

`.harmonik/workers.yaml` (V1 = exactly one worker; multi-host parked):
```yaml
version: 1
workers:
  - name: worker-mac-1        # tailnet name = ssh target
    transport: ssh
    host: worker-mac-1
    os: darwin                # darwin | linux — crews use this to know how to remediate
    repo_path: ~/harmonik-worker/repo
    max_slots: 4
    enabled: true             # live-disable without deleting the entry
```
- **Boot health-check (FR11):** for each `enabled` worker, verify SSH reachable + `tmux` +
  `claude` + repo present (the Part-4 checklist in `WORKER-SETUP-macos.md`). A
  configured-but-failing worker → mark **unhealthy + skip**, keep the config, raise a clear
  error. Re-check to recover.
- **Live-disable (FR12):** flip `enabled` at runtime; no config edit/delete.
- **Worker identity in run metadata (FR13):** every run records the worker name + OS so a crew
  can `ssh <worker>` to fix a missing dependency. Dependency drift on the worker is an expected
  operational event (Part 5 of the setup guide).

## OS decision (D4) & setup

V1 worker = **macOS** — near-zero setup, identical to box A's model, preserves the
interactive-login subscription, no new environment for the agents. The provisioning runbook is
`WORKER-SETUP-macos.md` (hand it to a Claude on the worker). Linux/containers are Phase 2,
where auth switches to `CLAUDE_CODE_OAUTH_TOKEN` (minted once via `claude setup-token`, injected
as env — you never log in *inside* a container).

## Open Phase-1 design decisions (for the analyze/spec passes)

- **DD1 — Code sync — LOCKED (operator):** reuse the existing **GitHub remote** (worker
  clones/pushes through it; box A merges as today). box-A-as-git-remote-over-tailnet parked.
- **DD2 — Worker control — LOCKED (operator):** **SSH-shelling** tmux/git for v1. The
  `harmonik-worker` gRPC helper binary is the deliberate Phase-2 evolution.
- **DD3 — Reviewer location — LOCKED (panel 3/3):** box A. Cheap/short; branch already
  fetched for merge; zero review-loop change. Remoting it is a Phase-1.5 follow-up.
- **DD4 — Worker registry/capacity — LOCKED:** `workers.yaml` schema (see `03-components.md`
  C3/C5); v1 = one worker, `max_slots` cap, `enabled` gate, local fallback. No cross-worker
  scheduling in v1.
- **DD5 — base-sha sync — LOCKED (panel 3/3):** box A ensures `baseSHA` is on origin, worker
  `git fetch origin <baseSHA>` then `worktree add <sha>`. Box A re-fetches the run branch before
  merge → any mismatch surfaces as a normal merge conflict (auto-skip), never silent. (See
  `03-components.md` DEC-B.)

> Full converged design + 12-bead build plan: `03-components.md` + `07-tasks.md`.

## Phase 2 preview (containers — the "spin up a sandbox" wish)

Swap the substrate impl from "ssh to a Mac" → "podman run a pinned `linux/arm64`
devcontainer", inject `CLAUDE_CODE_OAUTH_TOKEN` (subscription, headless OK — D2 still met),
get **true per-bead isolation + two-phase egress**. Most of the lifecycle above carries over
unchanged (worktree, commit-detect, push, merge-on-box-A); only the substrate, auth-delivery,
and egress-control differ. Runs on a dual-boot Asahi-Linux Mac (ARM64) or an x86 VPS/Azure at
work. This is also where leveraging OpenHands' pluggable-runtime backends (E2B/Modal/Daytona)
becomes concrete.
