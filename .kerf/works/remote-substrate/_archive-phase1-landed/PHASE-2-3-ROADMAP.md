# remote-substrate — Phase 2 vs Phase 3 roadmap (DRAFT for operator review)

> Consolidates the earlier brainstorm (BRAINSTORM.md) + RESEARCH-NOTES + what the live
> Phase-1 deploy taught us. Short bullets; the P2/P3 boundary is a proposal to react to.

**Dividing principle (REVISED after operator swap 2026-06-15):** Phase 2 = **powerful tools on
hardware we already control** — multi-worker, Linux-over-SSH, AND local-container bead-isolation
(with egress lockdown). Phase 3 = **the remote/cloud/elastic frontier** — remote crews + networked
comms, cloud-provisioned sandboxes, auto-scaling, distributing the control plane.
**SWAP:** local-container isolation P3→P2 (operator wants it sooner — high value, tractable now);
remote crews P2→P3 (the comms-bus-over-network work makes it heavier). Keeps P2 light + high-value.

## Finishing Phase 1 (NOT P2 — context)
- **hk-230h** — remote shell-node env propagation (DOT commit-gate / auto-status); last gap before full DOT runs work remotely.
- **Worker hardening** — `-default` orphan-on-restart (hk-9ptu), offline detect/recover, reconnect.
- **Test plan** (building now) executed to a real end-to-end remote completion.

## Phase 2 — connect-in (boxes that already exist)
- **Multiple workers + scheduling** — N workers in workers.yaml + a scheduler (round-robin / least-loaded / capacity-aware) vs one. Spread load across both spare Macs + a VPS.
- **Linux worker over SSH** — same OS-agnostic SSHRunner, a Linux box (Asahi Mac / x86 VPS), `os: linux`, auth via CLAUDE_CODE_OAUTH_TOKEN. Mostly works; needs Linux PATH/dep handling + arch (arm64 vs x86). Cheaper/denser than macOS.
- **Local container bead-isolation** *(MOVED from P3 — operator wants it sooner; the headline P2 item)* — isolate each bead's workflow in a LOCAL docker container on box A. Use the daemon's EXISTING INTERACTIVE claude model (tmux + paste-inject), NOT `claude -p` (subscriptions may drop headless `-p`). On macOS, docker = Linux containers in a VM (OrbStack/Docker Desktop/Apple container). Reuse the Phase-1 seam (a `DockerRunner` sibling of `SSHRunner`). Auth via `CLAUDE_CODE_OAUTH_TOKEN`. Per-bead-ephemeral or a warm pool. Real blast-radius isolation WITHOUT needing a remote box.
- **Two-phase / locked-down egress** *(MOVED from P3 — comes with containers)* — internet ON for setup, allowlist-restricted during the agent phase, per-bead-configurable; inoculates the credit-burn/exfil class.
- **Worker provisioning runbook reuse** — one declarative setup (adze manifest / script) to stand up a new worker repeatably.

## Phase 3 — spin-up / cloud / elastic (harmonik provisions)
- **Remote crews + networked comms** *(MOVED from P2 — the heavier lift)* — run a crew (orchestrator Claude) on a remote box; requires making the comms bus network-reachable (it's a local unix socket today — see the deep-dive below) + an interactive-login box (the OAuth token can't do `--remote-control`). The comms-transport work is what makes this P3-not-P2.
- **Cloud sandbox providers** — harmonik calls a provider API per bead: **E2B** (purpose-built, self-hostable), **Azure ACI** (your tenancy — the "spin up in Azure at work" ask), **Morph** (fork a running agent), Fly Machines / Fargate. The work-deployment frontier.
- **Auto-provisioning + elastic fleets** — spin workers up/down with queue depth; cost-aware; vs static registry.
- **Snapshot / fork running agent state** — checkpoint a live bead/crew (Morph-style) for resume/branch; ties to flywheel.
- **Distribute the control plane** — move daemon/captain off box A; multi-box HA. Biggest step.

## The comms question (remote crews) — cheapest → cleanest
- **(a) SSH-forwarded unix socket** — tunnel the existing socket over tailnet SSH. Least code, reuses everything; fragile (tunnel lifecycle), 1:1. → prototype.
- **(b) TCP+TLS bus listener** — daemon serves the bus over the tailnet (mTLS / Tailscale identity). Real networked bus, the right long-term answer; needs a new transport + auth on the socket layer (today unix-only, chmod 0600). → real design.
- **(c) Relay/broker** — small bus-relay per remote box. Most flexible (NAT, many crews), most moving parts.
- `--remote-control` constraint → remote crews ride persistent login boxes regardless of transport.

## Open boundary questions for the operator
- Where does **ephemeral containers** land — late P2 (it's on a box you own) or P3 (it's spin-up)?
- Is **remote crews** P2 (next) or deferred — given the comms-transport work it requires?
- Priority order within P2: multi-worker vs Linux vs remote-crews-comms?
