# Pi-in-a-sandbox — Analysis (chosen tool, code seams, risks)

> Formalized from `plans/2026-07-02-pi-sandbox/HANDOFF.md` §2–§6 and `README.md`. Authoritative.

## The chosen tool: `@anthropic-ai/sandbox-runtime` (`srt`)

Verified from `github.com/anthropic-experimental/sandbox-runtime` on 2026-07-02.

- **Platforms:** macOS (`sandbox-exec` + dynamically generated Seatbelt profiles, no extra deps),
  Linux (bubblewrap + seccomp BPF for unix-socket blocking, x64+arm64). Windows alpha — ignore.
- **Install:** TypeScript; `npm install -g @anthropic-ai/sandbox-runtime`. Node dependency at the
  launch site — accepted per operator steer.
- **Invocation:** `srt "<command>"`, `srt --debug <command>`, `srt --settings /path/to/settings.json
  <command>`. Wraps the entire process tree.
- **Execution model:** a wrapper that execs a subprocess — starts proxy servers, applies OS-level
  restrictions, then launches the target inside the sandbox.
- **Filesystem reads:** allow-by-default; `denyRead: []` blocks; `allowRead: []` re-allows within a
  denied region (allowRead wins).
- **Filesystem writes:** **deny-by-default**; list `allowWrite: []`; `denyWrite: []` exceptions beat
  allowWrite. **macOS supports glob patterns; Linux requires literal paths only** — the profile
  generator must emit literals for cross-platform safety.
- **Auto-protected (mandatory deny):** shell configs (`.bashrc`/`.zshrc`), git files (`.gitconfig`,
  `.git/hooks/`), IDE dirs. (See git-worktree subtlety below.)
- **Network deny-by-default.** `allowedDomains: []` (wildcards ok), `deniedDomains` wins. Enforced by
  an HTTP proxy (validates HTTP/HTTPS) + a SOCKS5 proxy (other TCP). macOS routes allowed traffic to
  localhost proxy ports via Seatbelt; Linux removes the net namespace and uses unix sockets.
- **Config shape:**
  ```json
  {
    "filesystem": { "denyRead": ["~/.ssh"], "allowWrite": [".", "/tmp"], "denyWrite": [".env"] },
    "network":    { "allowedDomains": ["github.com", "*.npmjs.org"], "deniedDomains": [] }
  }
  ```
- **Load-bearing gotchas:**
  - **Go binaries** need `enableWeakerNetworkIsolation: true` to verify TLS via the macOS Security
    framework when the MITM proxy is on — docs flag it "opens a potential data exfiltration vector
    through the trustd service." This directly affects us: `br`, `harmonik`, `gh` are Go binaries.
  - Jest needs `--no-watchman`. Docker on Ubuntu 24.04+ needs
    `sysctl kernel.apparmor_restrict_unprivileged_userns=0` for bwrap (Linux nodes note).

## Code seams (grounded, file:line)

**Pi is already built — context only, nothing to do:**
- `internal/core/agenttype.go:19` — `AgentTypePi AgentType = "pi"`.
- `internal/daemon/piharness.go`, `pilaunchspec.go` (`buildPiLaunchSpec`), `pijsonlparser.go` — full
  `Harness` impl; Pi is `CompletionProcessExit` + `SessionIDCaptured`, a structural clone of codex.
- `internal/daemon/harnessresolve.go:53` `resolveHarness` — 4-tier precedence (bead label `harness:pi`
  → queue → DOT node → `Config.DefaultHarness` → claude fallback). Selection solved.
- `internal/daemon/harnessregistry.go:47` `newHarnessRegistry` registers claude/codex/pi;
  `buildCodexRoutedLaunchSpec` (~:155) is the routed spec assembly Pi reuses.

**The isolation seam (this is the work):**
- `handler.Substrate` — `internal/handler/substrate.go:30` — 1-method `SpawnWindow(ctx,
  SubstrateSpawn) → SubstrateSession`. Only prod impl is `tmuxSubstrate`.
- `tmuxSubstrate` — `internal/daemon/tmuxsubstrate.go` — runs agent argv in `tmux new-window`.
  `perRunSubstrate` (`:1058`) + `spawnWindowVia(...)` (`:733`) already parameterize the runner and
  have a `remote=true` SSH branch. **This is where the `srt` wrap goes.**
- Launch argv assembly — routed launch-spec path (`buildCodexRoutedLaunchSpec` / Pi path in
  `harnessregistry.go`) → flows as `handler.LaunchSpec`.
- Worktree — `internal/workspace/createworktree.go` (`git -C <repo> worktree add --detach <path>
  <sha>`; path under `<repo>/.harmonik/worktrees/<run-id>-...`). Flows as `RunCtx.WorkspacePath` →
  `SpawnSpec.WorkDir` → `SubstrateSpawn.Cwd`.
- Config — `internal/daemon/projectconfig.go` (the `harnesses:` block, `PiHarnessConfig`). Add a peer
  `sandbox:` block.
- Composition root — `cmd/harmonik/main.go` / `run.go` construct the `Substrate`.
- Threading — `internal/daemon/workloop.go` (~:984 `newWorkLoopDeps`, and the launch-spec path).
- Test reference — `internal/daemon/scenario_container_l3_hkyflqo_test.go` (isolated-exec pattern).

**v1 wiring approach (argv wrap, NOT a substrate swap):** because `srt` is a command prefix, no
container substrate and no worktree relocation are needed. Instead of `pi <args>`, launch
`srt --settings <generated>.json pi <args>` inside the *existing* local tmux worktree. Cleanest
placement: do the wrap **inside the substrate spawn** (any harness can opt in via config), gated by
the `sandbox:` config. The worktree stays a plain host subfolder; `srt`'s `allowWrite` confines it.

## RISK §4 — the git-worktree writable-set subtlety (get this right)

The sandbox must let Pi **commit to its own branch** but **not touch main's working tree or other
worktrees**. In a git worktree, `.git` is a *file* pointing at the main repo's
`.git/worktrees/<id>/`; commits write objects into the **shared** `.git/objects` and refs into
`.git/worktrees/<id>/` (+ possibly `.git/refs` / `.git/packed-refs`). So `allowWrite` must include at
minimum:

- the run's **worktree directory** (`<repo>/.harmonik/worktrees/<run-id>-*`),
- the main repo's **`.git/worktrees/<run-id>/`** (this run's HEAD, index, logs),
- the shared **`.git/objects/`** (commit/tree/blob writes),
- the ref path the branch update lands in (**`.git/refs/`** and/or `.git/packed-refs`) — scope as
  tightly as possible; ideally only this run's branch ref,
- `$TMPDIR` and the toolchain caches (§6).

`denyWrite` (or not-allow) must cover: the **main working tree files**, other worktrees' dirs, and
`.git/hooks/` (srt auto-denies hooks anyway). **Validate empirically** that a commit + branch-create
succeeds inside the sandbox and a write to a main-repo file is *denied* — that pair is the acceptance
gate for the isolation guarantee. srt's mandatory-deny includes `.gitconfig` and `.git/hooks/`;
confirm that doesn't block Pi reading `.gitconfig` (reads allowed by default — should be fine, verify).

## RISK §5 — network + the Go-CLI TLS problem (the fiddliest real-world part)

`srt` denies network by default and MITM-proxies allowed domains. The sandboxed Pi must reach:
- its **model API** (OpenRouter `openrouter.ai` / provider host) — or nothing runs,
- **GitHub** (`github.com`, `api.github.com`) for `gh`/pushes/fetches if used,
- and it shells out to `br`, `harmonik comms`, `queue submit` — which mostly talk to the **local
  daemon over a unix socket / localhost**, not the internet. Confirm whether they need network
  allowlisting or just local-socket access.

**Go-TLS gotcha:** `br`, `harmonik`, `gh` are Go binaries and fail TLS verification under the MITM
proxy unless `enableWeakerNetworkIsolation: true` (mild exfil risk per docs). Options — decide during
the spike:
- (a) set `enableWeakerNetworkIsolation: true` (accept the caveat), or
- (b) keep those Go CLIs talking to the local daemon over the unix socket (no MITM, no TLS issue) and
  only allowlist the genuinely-remote domains, or
- (c) run the local-only tools *outside* the network sandbox.

**Resolution for v1 [LOCKED]:** v1 network mode = **OPEN** — rely on the filesystem boundary as the
core isolation guarantee; tighten to an egress allowlist in a later pass. The spike still de-risks the
mechanism: prove a sandboxed process can (1) reach the local daemon over the unix socket for `br` /
`comms` and (2) make one OpenRouter call. Everything else is wiring.

## §6 — warm build-cache design

- Go: `GOCACHE`, `GOMODCACHE` (Rust `CARGO_HOME`; cabal/stack; opam; dune — later). Content-addressed
  → safe read-only warm base, unsafe as a concurrent shared writer.
- Simplest correct v1 on mac: caches live on native host FS; add to `allowRead` (warm reuse); if the
  build must *write* new cache entries, give each run a **private writable cache dir** (per-run
  overlay, or per-run `GOCACHE` seeded from the warm one via `cp --reflink` on APFS) so concurrent
  runs never share a writable cache. Never let multiple sandboxes write one shared cache dir — that is
  the cache-reaper TOCTOU footgun again.
- Seatbelt on native FS → no VirtioFS penalty; that is the whole reason Seatbelt beats a container
  here. Keep it that way.

## Config surface (proposed) — §7

A `sandbox:` block in `projectconfig.go`, peer to `harnesses:`:

```yaml
sandbox:
  backend: srt            # srt | none  (none = today's behavior; orbstack later)
  harnesses: [pi]         # which harnesses run sandboxed (start with pi only)
  network:
    mode: open            # open | allowlist   (v1 = open — LOCKED)
    allowed_domains: ["openrouter.ai", "github.com", "api.github.com"]
    weaker_network_isolation: false   # set true only if Go-CLI TLS forces it (§5)
  cache:
    warm_read: ["${GOCACHE}", "${GOMODCACHE}"]
    private_write: true   # per-run writable cache seeded from warm base
```

Fail-loud on missing required keys (locked no-hardcoded-defaults principle). The profile generator
turns this + the run's worktree/git paths into the concrete `srt` settings JSON.

## Provisioning note

Node dependency at the launch site (srt is npm/TS): the sandbox box needs Node + `srt` installed.
Add to worker fingerprint / dependency-drift checks à la `docs/remote-substrate/WORKER-SETUP-macos.md`.
