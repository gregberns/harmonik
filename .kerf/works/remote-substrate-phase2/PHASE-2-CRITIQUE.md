All claims now verified against the live code. The remaining `-p` hits are exactly the benign ones (`ps -o comm= -p <pid>`, tmux `display-message -p`, ssh `-p` port flag) — no `claude -p` in any dispatch path. Here is my adversarial ranking and synthesis.

---

# ADVERSARIAL ROBUSTNESS REVIEW — remote-substrate Phase 2 (3 plans)

## Ground-truth verification (all 3 plans cite the seam; I checked it)
- `tmux.CommandRunner` is the real single seam (`runner.go:15`); `SSHRunner` (`:84`), `LocalRunner` (`:21`) are the two impls. SSHRunner's doc explicitly says it **does not touch stdin** (`:81-83`) — the `load-buffer -` paste path depends on this. **A `DockerRunner` sibling is genuinely correct, and `docker exec -i` is required to preserve the stdin contract.**
- Dispatch site is `workloop.go:2132-2143`; `remoteBeadCtx{worker, sshRunner}`, `SelectWorker()` → `SSHRunner{Host: w.Host}`, `defer ReleaseSlot()`. The runner threads to code-sync (`:2163,2185`), worktree (`:2206`), dot (`:2565`), substrate (`:2740`). **This is the single insertion point — all 3 plans identify it correctly.**
- `ErrTooManyWorkers` caps `version:1` at 1 worker (`workers.go:121`); `Registry` is single-`worker`/single-`inFlight` (`registry.go:7-12`). Generalization is real work.
- **No `claude -p` anywhere in dispatch** — confirmed. Launch is `--session-id`/`--resume` (`claudelaunchspec.go:372-374`).
- **The env-strip block zeros `CLAUDE_CODE_OAUTH_TOKEN` (`:335`), always.** This is the "one fundamental change." All 3 plans found it.
- **Correction to Plan 1's claim of a separate `api_key_absent` boot probe inside the container:** the 4-probe set IS real (`health.go:101-106`, including `api_key_absent`) — Plan 1's grep-based doubt in my head was wrong; Plan 1 is right that it's a probe and right that it should run via the container runner. Plan 2 correctly proposes adding an `oauth_token_present` companion probe. **Plan 2 is the most accurate here.**

---

## RANKING

**#1 — Plan 2.** Most technically accurate, best sequencing instinct, only plan that catches the genuinely-hard hidden defect.
**#2 — Plan 3.** Strongest on the env/transport-failure mechanics and the most disciplined non-goals, but weaker on the per-container-tmux problem and over-commits to ephemeral.
**#3 — Plan 1.** Excellent prose and the clearest `DockerRunner`/factory code, but it has a latent correctness gap it never surfaces, and its pool-as-default recommendation is the riskiest.

---

## The decisive differentiator: the per-container tmux server

This is the single most important finding, and **only Plan 2 caught it** (its §3.2 "subtle point I'm calling out" + risk R-A).

Today the daemon targets ONE pre-frozen `harmonik-<hash>-default` tmux session as the spawn target for all runs (this is in project memory as a load-bearing invariant — "NEVER kill a `*-default` spawn session"). The spawn is `tmux new-window -t <session>:`. When you swap `SSHRunner` → `DockerRunner`, the `tmux` binary now runs **inside the container**, which has **no `-default` session** — there is no tmux server there at all until you start one.

So "just swap the runner and everything works unchanged" is **false** for the container case. You must additionally:
1. Start a tmux server + a named session inside each container at container-start (`docker exec <ctr> tmux new-session -d -s harmonik-ctr`).
2. Thread that per-container session name into the spawn target (the spawn currently hard-targets the box-A `-default` session).

- **Plan 2** names this exactly, adds a `sessionName` field to `remoteBeadCtx`, and flags it as risk R-A to assert in the e2e test. **This is the difference between a plan that works and one that wedges on first spawn.**
- **Plan 3** glances at it ("rc-prompt suppression," "real PTY tmux session") but never says the spawn target must change or that a per-container session must be created — its "swap `SSHRunner{Host}` → `DockerRunner{Container}` re-routes all of them with zero changes" (§3.1) is **the incorrect claim** for this case.
- **Plan 1** is the most emphatic that it's zero-change ("the same interactive-tmux-paste-inject machinery runs inside a container with ~zero changes") — and is therefore the **most wrong** on the load-bearing detail. It never mentions the session target at all.

This alone justifies ranking Plan 2 first.

---

## Constraint-by-constraint scorecard

| Stress axis | Plan 1 | Plan 2 | Plan 3 |
|---|---|---|---|
| **Interactive-not-`-p`** | Honored; explicit "no `-p`" | Honored | Honored |
| **Per-container tmux session** (the real trap) | **MISSED** ("zero changes") | **CAUGHT** (`sessionName` + R-A) | partial / claims zero-change |
| **Reuse CommandRunner seam** | Yes, cleanest factory code | Yes | Yes, names `IsTransportFailure` generalization best |
| **Token-auth / Keychain reality** | Good (`AuthMode` gate, env-file 0600) | Good (+ `oauth_token_present` probe — best) | Good (mirrors `buildCodexEnv`, narrowest exposure) |
| **arm64 / VM overhead** | `arch` attr; OrbStack | + **`uname -m == aarch64` health assert** (best) | `arch` field; gVisor ceiling noted |
| **Result-out (push→merge)** | Container pushes run-branch (sound) | **Bind-mount-fetch (no push cred in container)** — strongest security, but riskier mechanism | Container pushes (sound), rejects bind-mount cleanly |
| **Egress lockdown realism** | iptables `NET_ADMIN` lean | **Egress proxy** (brokers git secret outside sandbox) — strongest | Egress proxy lean; names `egress_blocked` loud-fail |
| **Multi-worker unification** | One registry, factory branch | One registry, factory branch | One registry, cleanest YAML v2 example |
| **Pool vs ephemeral** | **Pool default (riskiest)** | Ephemeral-first behind `Provider` iface (right call) | Ephemeral-first, pool opt-in (right call) |
| **Sequencing / incremental** | Good; C1+C4 gate | **Best — SSH-Linux track first (pure Go, de-risks registry-v2 independent of docker)** | Good; container chain first |
| **Scope discipline (no P3 leak)** | Clean | Clean | **Cleanest** (5 explicit NGs incl. microVM, names the `--remote-control` reason crews are P3) |

---

## Strongest elements to harvest (per plan)

**From Plan 2:**
- The per-container tmux session discovery + `sessionName` threading (the load-bearing fix).
- `oauth_token_present` health probe as a companion to `api_key_absent`.
- The `uname -m == aarch64` arm64-discipline assertion (catches silent QEMU 10× slowdown).
- "Build SSH-Linux + multi-worker FIRST" — it's pure box-A Go, reuses `SSHRunner` verbatim, and de-risks the `ErrTooManyWorkers`/registry-v2 lift **independently of any docker dependency**. This is the throughput-maximizing order.
- Egress proxy that brokers the git secret *outside* the sandbox (composes with no-push-cred-in-container).
- Risk R-C: egress must fail **loud** (`egress_blocked` signal), not hang — a too-tight allowlist that drops `api.anthropic.com` looks exactly like a hung agent.

**From Plan 3:**
- `IsTransportFailure(runner, err)` — the correct generalization of `IsSSHConnectionFailure` (docker exec returns 125/126/127, NOT 255). **All three plans need this; only Plan 3 names the exact exit codes and the call-site at `workloop.go:2167`.** Verified: that call site exists.
- Mirroring the proven `buildCodexEnv` strip/reinject pattern for the OAuth injection — concrete, low-risk template.
- The disciplined non-goals (microVM/Firecracker explicitly OUT; the `CLAUDE_CODE_OAUTH_TOKEN`-can't-`--remote-control` note as the *reason* crews are P3).
- Token delivery via the launchspec env path (`-e` on spawn) NOT the container's global env (`docker create -e`) — narrowest blast radius (Plan 3 OD5).

**From Plan 1:**
- Cleanest `runnerForWorker(w)` factory + the engine-agnostic `Bin` field (`docker`/`orb`/`container` as config, not a code branch).
- The `AuthMode: keychain|token` gate on `LaunchConfig` — the precise, minimal shape of the env-builder change at `:324-341`, riding the existing `cfg.SecretVars` override-last path (`:345`).
- Explicit reasoning for rejecting the bind-mount-working-tree (defeats isolation + reintroduces merge race) — the clearest articulation of the trust boundary.
- Container labels (`harmonik.run=<id>`/`harmonik.pool=`) for the orphan-GC sweep mirroring the existing tmux orphansession reap.

---

## REQUIRED additions/fixes the final plan MUST incorporate

1. **Per-container tmux session is NOT free.** The spawn currently targets the box-A `-default` session. The final plan MUST: (a) start a tmux server + named session inside each container at container-start, and (b) thread that session name into the spawn target via a new `remoteBeadCtx.sessionName` field. Reject any "just swap the runner, zero changes" framing — that is the #1 way this wedges on first spawn. Assert in the e2e test that the spawn targets the in-container session, never box A's `-default`.

2. **`IsTransportFailure(runner, err)` generalization is mandatory, not optional.** `docker exec` returns 125/126/127, not SSH's 255. The hard-coded `IsSSHConnectionFailure` at `workloop.go:2167` and `:2187` must become transport-neutral, or a dead container is misclassified as a remote-command failure and never triggers `worker_offline`/`run_stale` recovery. (Verified both call sites exist.)

3. **Egress must fail LOUD.** A blocked allowlist dial to `api.anthropic.com` is indistinguishable from a hung agent — the run will burn the full 30-min commit budget then `no_commit`. Emit a typed `egress_blocked` event. Mandatory allowlist for the agent phase: `api.anthropic.com` + the git remote/box-A mirror. (Plan 2 R-C.)

4. **Orphan-container GC is a security/billing concern, not hygiene.** A daemon crash mid-bead leaks a running container holding the OAuth token in its env. The sweep (label-filtered `docker ps`, sibling of the tmux orphansession reap) MUST reap on daemon boot. (Plan 1's labels + Plan 2's R-E.)

5. **Ephemeral is the v1 default, behind a `Provider`/`ContainerManager` interface; pool is a later latency optimization.** Plans 2 and 3 are right; Plan 1's pool-default is the riskiest choice (cross-bead state-bleed risk, reset-correctness burden) and contradicts the isolation thesis that justifies the whole phase. The `pool_size: 0 ⇒ ephemeral` config flag defers the operator A/B, but the *default* must be ephemeral.

6. **OAuth token delivery: narrowest blast radius wins.** Inject via the spawned session's env (the launchspec `-e` path, `AuthMode: token` gate), NOT `docker create -e` global env (which puts the token in every `docker inspect` for the container's whole life). Token read from a 0600 file on box A, never baked into the image, never in a worktree file. Add the `oauth_token_present` companion probe (Plan 2) + keep the fail-closed `api_key_absent` probe running **inside the container** via the DockerRunner.

7. **`uname -m == aarch64` health assertion** — cheap, prevents silent QEMU emulation 10×-slowing every build/test gate (Plan 2 R-B). Pin `linux/arm64`.

8. **Sequencing: ship the SSH-Linux + multi-worker track FIRST** (Plan 2). It's pure box-A Go, reuses `SSHRunner` unchanged, delivers two goals (multi-worker + Linux-over-SSH), and de-risks the `ErrTooManyWorkers`-lift + N-worker `Registry` generalization without any docker dependency on the critical path. Then the container chain (`DockerRunner` → token/strip → image → lifecycle-ephemeral → dispatch-wire → open-egress e2e), and only THEN layer two-phase egress on a proven spawn.

9. **Same-file serialization (all 3 plans flagged, correct):** `DockerRunner`, the dispatch-switch, and `IsTransportFailure` all touch `runner.go` + `workloop.go`. Dispatch serially behind merges; the new `internal/container/` and `internal/workers/` work runs concurrently.

10. **Decide the result-out mechanism explicitly (this is the one unresolved design fork).** Plan 1/3 = container pushes `run/<id>` (needs git egress in the allowlist; simplest; proven code-sync path). Plan 2 = bind-mount-fetch with no push credential in the container (tightest security, composes with the egress proxy, but couples the container FS to box A and is the less-proven mechanism). **Recommend the push path for v1** (reuses `pushRunBranchOnWorker` verbatim, git remote already in the allowlist), keeping Plan 2's no-push-cred bind-mount as the documented hardening follow-up — don't let the more elegant mechanism gate the headline.

**Bottom line:** Adopt Plan 2's skeleton (it alone is correct on the tmux-session trap and has the best build order), graft in Plan 3's `IsTransportFailure` exit-code precision + `buildCodexEnv` env template + tighter non-goals, and Plan 1's `runnerForWorker`/`Bin` factory shape + `AuthMode` gate + container-label GC. Make ephemeral the default, make egress fail loud, and treat the per-container tmux session as a first-class threaded field — not an afterthought.