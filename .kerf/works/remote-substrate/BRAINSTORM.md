# remote-substrate — Brainstorm (working doc)

> Living scratchpad. We firm up `01-problem-space.md` + `REQUIREMENTS.md` FIRST, then
> go deep here. Seeded 2026-06-14 from the research fan-out (`RESEARCH-NOTES.md`).

## Decisions locked (operator, 2026-06-14)

connect-in first · only **bead-work** relocates (crews/captain/daemon/bus stay on box A, D3)
· v1 = **Phase 1 remote-Mac-over-SSH** · subscription billing is a hard MUST (D2, no API key)
· egress lockdown is a Phase-2/container capability, per-bead-configurable. Full Phase 1
design → `PHASE-1-DESIGN.md`. Phases renumbered (old Phase 0→1, 1→2, 2→3); remote crews dropped.

## The big early reframes (from research)

1. **The abstraction boundary already exists.** `handler.Substrate.SpawnWindow(Cwd,Env,Argv)`
   is transport-neutral; local-tmux is just one impl. Remote = a second impl behind the same
   seam. This is a *much* smaller change than "rearchitect for distribution."
2. **Subscription billing CAN survive remote** — two supported ways: (a) persistent remote Mac
   with a one-time interactive login; (b) `CLAUDE_CODE_OAUTH_TOKEN` (1-yr, headless,
   subscription-billed) injected into containers. The old "remote = API-credit burn" fear is
   only true if we use raw API keys. ⚠️ but the token can't do `--remote-control` → bears on
   where **crews** can run.
3. **Bead-work and crews have different remoting constraints.** Bead-work (headless `claude -p`)
   needs no comms bus and can use the OAuth token → easy to remote. Crews need the bus
   (network transport) AND `--remote-control` (so token won't work) → crews are tied to
   interactive-login boxes (a persistent remote Mac), and need bus-transport work.
4. **Prior art (Codex/Devin/Cursor/Jules/Copilot/OpenHands) converges on the exact shape we
   already have:** a control-plane/data-plane split — orchestrator stays put, the *execution
   substrate* is pluggable (OpenHands literally swaps Docker/E2B/Modal/Daytona behind one
   orchestrator). The dominant isolation unit is **ephemeral-per-task + fast snapshots**
   (Devin blockdiff, Codex 12h cache); repo ingress is **git-clone → branch → PR** (we
   already do this). Two NEW takeaways to bake in: (a) **two-phase egress** (internet ON for
   setup, restricted during the agent phase) and (b) **secrets brokered OUTSIDE the sandbox**
   via a proxy — both directly inoculate against our past credit-burn / API-key-leak incident.

## The decision axes

| Axis | Options | Early lean |
|---|---|---|
| **A. Connect-in vs spin-up** | connect to a pre-provisioned box / harmonik provisions a sandbox | one provider interface; **connect-in first**, spin-up as a later provider |
| **B. Unit of placement** | bead-work / crew / whole daemon / captain | **bead-work first** (biggest win, fewest constraints); crews later |
| **C. Isolation granularity** | per-bead / per-crew / per-box | per-box (remote Mac) first → per-bead (container) next |
| **D. Substrate OS** | remote macOS / Linux container / cloud sandbox | remote macOS first (billing+reuse); Linux container next; cloud later |
| **E. Auth model** | interactive login / OAuth token / API key | login (Mac) + OAuth token (container); **never** stray API key |

## Candidate architectures (phased — grounded in the seam at `handler.Substrate`)

### Phase 1 — SSH to a persistent remote Mac (connect-in · bead-work · per-box)  ← v1
*Smallest viable, lowest risk, immediate resource relief. Full design: `PHASE-1-DESIGN.md`.*
- New `handler.Substrate` impl: `SpawnWindow` runs `ssh worker-mac -- tmux new-window …`;
  paste-inject + liveness tunnel `tmux`/`ps` over the same SSH transport.
- Worktrees: remote Mac has its own clone; daemon runs `git worktree`/commit ops over SSH on
  the remote. Results return by pushing the task branch → box-A daemon fetches + merges
  (one-at-a-time flow unchanged).
- Auth: one-time interactive `claude` login on the worker Mac → **subscription billing**.
- Bus: untouched (bead-work doesn't use it). Defer transport work.
- Reachability: Tailscale tailnet (LAN today, internet later) + Tailscale SSH + ControlMaster.
- Wins: idle Mac absorbs beads; box A stops thrashing disk/RAM; reuses the entire tmux model.
- Costs/risks: SSH-tunneled liveness probes; worktree ops over the wire; one manual login.

### Phase 2 — ephemeral Linux container per bead (spin-up · bead-work · per-bead)
*True isolation + density + egress lockdown; the "harmonik spins something up" path.*
- `RemoteSubstrate` that per-bead `podman run --rm` a pinned `linux/arm64` devcontainer image,
  runs `claude -p` headless inside with `CLAUDE_CODE_OAUTH_TOKEN` injected → subscription-billed.
- gVisor optional hardening. Image defined by a Dockerfile/devcontainer (optionally authored in
  adze YAML via `adze render`).
- Runs on a dual-boot Asahi-Linux Mac (ARM64) **or** an x86 VPS at work (no arch issue).
- Wins: per-bead blast-radius containment; clean teardown; elastic-ish.
- Costs/risks: ARM64 image discipline; container build/cache mgmt; the OAuth-token lifecycle.

### Phase 3 — cloud sandboxes (connect-in/spin-up · bead-work · sandbox)
- ~~Remote crews~~ DROPPED (D3 — crews stay on box A; no bus transport / remote-control needed).
- Cloud sandbox provider for the "at-work, spin up in Azure/cloud, connect and run" scenario:
  harmonik calls a provider API. Top fits (R4, all support a long-lived interactive PTY):
  **E2B** (purpose-built SDK + PTY, self-hostable OSS), **Azure ACI** (your own tenancy/VNet —
  matches the "spin up in Azure at work" ask), **Morph** (can fork a *running* agent session).
  Fly.io Machines = best DX if 3rd-party SaaS OK; AWS Fargate+ECS-Exec = AWS equivalent.

## Where adze fits
Not the sandbox primitive. Use a pinned Dockerfile/devcontainer for disposable sandboxes
(Phase 1); use adze (or `adze render` → Dockerfile) to author/maintain the *persistent worker
Mac/VPS* (Phase 0/2) where drift-capture beats a Dockerfile.

## Open design questions (defer until problem+requirements locked)
- Worktrees over SSH vs a remote git clone the daemon drives remotely vs shared FS.
- Does the daemon merge from a remote-pushed branch, or pull the worktree back?
- How is a worker registered/capacity-modeled (config schema)? Scheduling across workers.
- Crew bus transport: TCP+TLS vs SSH-forwarded unix socket.
- Failure model: lease/heartbeat TTL, orphan-worktree cleanup across the wire.

## Still pending
- R1 prior-art (how Codex/Devin/Cursor/Jules do remote exec) — re-running.
- R4 managed sandbox-as-a-service survey — pending (informs Phase 2 cloud path).
