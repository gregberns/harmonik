# remote-substrate — Research Notes (2026-06-14)

> Condensed findings from the kickoff research fan-out (8 agents). Full sources per
> section. Feeds `REQUIREMENTS.md` constraints and `BRAINSTORM.md` synthesis.

## R5 — Claude Code auth/billing on remote machines  ⭐ make-or-break, RESOLVED

- Credential store: macOS Keychain; Linux `~/.claude/.credentials.json` (0600). OAuth,
  auto-refresh ~5 min / on 401. Treated as **device-bound**; copying across machines is
  **not officially supported** (fraud-detection risk).
- **`CLAUDE_CODE_OAUTH_TOKEN`** (the key finding): 1-year token from `claude setup-token`
  on an authenticated machine, scoped to inference, **subscription-authenticated and
  subscription-billed**, injectable as env var into Linux/ephemeral containers. This is a
  *supported* headless subscription path.
  - ⚠️ **Cannot establish Remote-Control sessions; unsupported in `--bare`.** Crews use
    `claude --remote-control` → crews can't use this token for remote-control.
- `ANTHROPIC_API_KEY` = API credit pool (NOT subscription). If set alongside OAuth, the
  key WINS → silent API billing + auth failures. Keep it OUT of remote envs unless
  intended.
- Billing by model: persistent remote **Mac** w/ interactive login → subscription;
  ephemeral **container** w/ `CLAUDE_CODE_OAUTH_TOKEN` → subscription (headless only);
  `ANTHROPIC_API_KEY` → credits; Bedrock/Vertex/Foundry → cloud-provider billing.
- ToS red lines: no reverse-engineering OAuth into 3rd-party tools; don't co-set API key.
- Sources: code.claude.com/docs/en/authentication; github.com/anthropics/claude-code
  issues #7100 (headless auth NOT_PLANNED for browser-OAuth); various guides.

## R6 — harmonik's local/tmux coupling  ⭐ the seam already exists

- **`handler.Substrate` (`internal/handler/substrate.go:30`)** is already the seam:
  `SpawnWindow(ctx, SubstrateSpawn{Cwd,Env,Argv}) (SubstrateSession, error)` — transport-
  neutral. Local-tmux is just the impl (`tmuxSubstrate`, `internal/daemon/tmuxsubstrate.go:101`
  → `tmux.OSAdapter` shelling `exec.Command("tmux", …)`).
- Four things to make remote work:
  - **(A)** add a 2nd `handler.Substrate` impl (SSH/container) — daemon already injects it
    via `LaunchSpec.Substrate` (`handler.go:114`). *This single seam makes remote spawn work.*
  - **(B)** fold paste-inject + liveness into the interface — `pasteinject.go` optional
    ifaces (`pasteInjecter`, `enterSender`, `quitSender`, `paneLivenessChecker`) + raw
    local `pgrep`/`ps` (l.253-294) + `tmuxSubstrateSession.Wait` local-PID poll must get a
    remote (over-the-wire) equivalent.
  - **(C)** `Workspace`/Worktree seam — `workspace.CreateWorktree`/`WorktreePath` are free
    funcs hardwired to local git+FS (`.harmonik/worktrees/<run_id>/`). Remote needs
    worktree creation *there* or a shared FS.
  - **(D)** pluggable bus transport — bus is **unix socket only** (`.harmonik/daemon.sock`,
    `socket.go:290`), chmod 0600, every client dials `"unix"`, **zero TCP/SSH** in tree. A
    remote **crew** can't reach the bus as-is; bead-work doesn't need the bus.
- **Smallest viable first cut:** SSH `Substrate` whose `SpawnWindow` runs
  `ssh host -- tmux new-window …`, paste/liveness tunnel tmux/ps over the same transport;
  defer worktrees-on-shared-FS (C) and bus-transport (D).

## R2 — macOS / Apple-Silicon remote-exec toolbox

- **Recommendation: persistent remote Mac as bare-metal SSH+tmux host (stay macOS)** —
  lowest friction, highest density, *subscription-billing-preserving*, architecturally
  identical to today (point spawn target at `ssh worker-mac tmux …`). Harden: low-priv
  user, per-worktree `sandbox-exec` profile (deprecated but Anthropic itself ships on it),
  keep `ANTHROPIC_API_KEY` out.
- Tart (Apple Virtualization.framework): full-VM macOS guests, native Claude Code + OAuth,
  **but Apple EULA caps 2 macOS VMs per physical Mac** — 2nd-tier isolation, not primary fan-out.
- OrbStack / Apple `container` (macOS 26): best for ephemeral but **Linux-only guest** (so
  `CLAUDE_CODE_OAUTH_TOKEN` needed for subscription). Skip Docker Desktop (4GB idle, paid),
  Lima/Colima superseded.
- Sources: tart.run, apple/container, orbstack docs, eclecticlight (EULA), infralovers
  sandboxing-claude-code.

## R3 — Linux container/VM substrate (for spin-up path)

- **Primary: rootless Podman (or Docker) running ephemeral OCI containers, with a
  `devcontainer.json` as the reproducible image/env definition** — `podman run --rm img cmd`
  *is* "image+cmd, stream, destroy"; sub-second start, high density on 32-64GB.
- **gVisor (`runsc`)** = one-line OCI-runtime hardening upgrade (~18% syscall overhead).
- Firecracker/Kata microVMs only if you need true HW isolation (untrusted code) — overkill
  for our own beads. k3s/Kubernetes overkill for single-box. Nix = build images, not sandbox.
- ⚠️ **Apple Silicon = ARM64**: Linux images must be `linux/arm64` (else slow QEMU). Asahi
  Linux gives native KVM. An x86 VPS sidesteps the arch issue entirely.
- Sources: northflank (kata/firecracker/gvisor), morphllm docker-vs-podman, devcontainers/cli,
  asahilinux.org.

## R8 — networking / security / code-sync between boxes

- **Overlay: Tailscale** for both LAN and cloud (WireGuard + NAT traversal + identity ACLs +
  MagicDNS). **Ephemeral tagged nodes** purpose-built for spin-up-sandbox-then-logout.
- **Control transport:** LAN → Tailscale SSH + OpenSSH `ControlMaster` multiplexing (cheap
  spawn/monitor chatter). Cloud → a small box-B **agent daemon (gRPC over tailnet)** for
  typed events/heartbeats; SSH as bootstrap/fallback.
- **Code sync = git is the spine** (matches worktree+branch+merge): clone in (fresh per cloud
  sandbox; `--reference` a local bare mirror on LAN), push task branch out → daemon merges.
  rsync = LAN optimization. **Avoid NFS/SMB** (lock/consistency hazards).
- **Secrets:** short-lived per-task minted tokens injected as env, never baked into images;
  SSH agent-forwarding only to trusted LAN hosts; sops+age for at-rest.
- **Liveness:** TTL lease + heartbeat → cleanup (maps onto harmonik's `run_stale`).
- Top risks: (1) credential leak into remote env; (2) split-brain/orphaned worktrees on
  partial failure; (3) Tailscale control-plane dependency / over-broad ACL blast radius.
- Sources: tailscale.com (SSH, ephemeral-nodes, ACLs/grants, auth-keys, OAuth clients),
  smallstep (agent-forwarding risk), gitguardian (sops+age).

## R7 — adze (../machine-setup) evaluation

- adze = declarative single-machine config CLI (Go), YAML desired-state, convergent
  idempotence (NOT hermetic), targets **macOS (brew) + Ubuntu (apt)** real machines/VMs.
  Bidirectional `status`/`capture` (folds manual installs back). `render` → standalone bash.
- **Not a container/image builder; no lockfile/hermetic guarantee.** Claude Code install +
  headless auth would be hand-authored custom steps.
- **Verdict:** Dockerfile/devcontainer wins for *disposable identical sandboxes* (the dominant
  offload case). adze wins for a *persistent pet worker VM/box* you SSH into and hand-tweak
  (drift detection a Dockerfile can't give). Middle path: author in adze YAML, `adze render`
  the bash into a Dockerfile `RUN` (validate — rendered steps are best-effort equivalent).

## R4 — managed sandbox-as-a-service (Phase 2 cloud path)

- Discriminator for harmonik: needs a **long-lived interactive PTY** (hours, ideally tmux),
  not one-shot exec. That rules out several "sandbox" products.
- **Top picks:**
  1. **E2B** — purpose-built; first-class PTY API (`pty.create/send_stdin/resize`), ~80-200ms
     cold start, 24h sessions, cheapest (~$0.10/hr), **self-hostable (OSS)**. Best drop-in SDK fit.
  2. **Azure ACI** — directly satisfies the "spin up in Azure at work, connect, run" ask: runs
     in *your own* Azure subscription/VNet, real `az container exec` PTY, per-sec billing, $0
     when deleted. Slower (~10-60s) but right tenancy. (Fly.io Machines = better DX if 3rd-party
     SaaS OK; AWS Fargate + ECS-Exec = AWS-tenancy equivalent.)
  3. **Morph** — can snapshot/**fork a *running* Claude Code session** (live RAM) in <250ms;
     official tmux+Claude-Code example. Useful if we ever checkpoint/branch agent state mid-task.
- **Avoid:** Modal (no true PTY, 5-min default), GCP Cloud Run (no exec/PTY), Codespaces/Dev Box
  (human-grade, slow), Blacksmith/Depot (CI-shaped, no generic exec API).
- **Volatility:** Coder Tasks winding down (~Sep 2026); Gitpod→Ona acquired by OpenAI (Jun 2026),
  roadmap uncertain; verify pricing before committing.
- Sources: e2b.dev/docs, learn.microsoft.com/azure/container-instances, cloud.morph.so/docs,
  fly.io/docs/machines, docs.aws.amazon.com ECS-Exec, northflank.com sandbox comparisons.

## R1 — prior art: how Codex/Devin/Cursor/Jules/Copilot/OpenHands do remote exec ✅

- **Control-plane / data-plane split is near-universal for cloud agents.** LLM loop +
  orchestration stays in the vendor cloud; only code execution runs in the sandbox.
  Explicit architectural seam in Cursor, OpenHands, Devin. **harmonik already has this**
  (Go daemon = control plane; Claude Code in worktrees = data plane) — validates R6's
  "make the data-plane substrate pluggable behind `handler.Substrate`." OpenHands is the
  closest analog: one orchestrator, swappable Docker/E2B/Modal/Daytona runtime backends.
- **Ephemeral-per-task is the default isolation unit**, with **fast snapshots** to fight
  cold-start/state cost: Devin blockdiff (~200ms/20GB), Codex 12h container cache,
  Cursor hibernate/fork, Claude-web 7-day FS cache, Jules reusable setup snapshot.
  Pattern = "ephemeral execution, persistent *state via snapshots*." (Morph from R4 offers
  this — fork a running session.)
- **Repo ingress = git clone via GitHub App → fresh branch → draft PR; human merge gate.**
  harmonik's branch→merge-one-at-a-time / integration-branch / `promote` already matches —
  a remote runner just pushes a branch the daemon merges.
- ⭐ **Egress is default-deny + allowlist, often TWO-PHASE**: internet ON during
  setup/dependency-install, restricted during the agent phase (when prompt-injection
  exfil risk peaks). Codex + Claude-web do this explicitly. **Directly addresses harmonik's
  credit-burn / API-key-leak incident**: keep ANTHROPIC creds in the setup/broker layer,
  NEVER inside the agent sandbox; proxy git auth so tokens never enter the worktree.
- **Secrets brokered OUTSIDE the sandbox via a proxy** (Claude-web: git token never enters
  container; Devin credential isolation; Codex scrubs secrets before agent phase).
- Substrate is container OR VM (rarely a real microVM publicly); most reuse existing CI/cloud
  primitives (Copilot=GitHub Actions runners, Replit=Nix containers, OpenHands=Docker+pluggable)
  rather than building a hypervisor. Amp = local-only, no sandbox.
- Sources: openai.com/codex, cognition.ai/blog/blockdiff, cursor.com/docs/cloud-agent,
  code.claude.com/docs/sandboxing + claude-code-on-the-web, jules.google/docs,
  docs.openhands.dev/architecture/runtime, github.blog copilot-coding-agent.
