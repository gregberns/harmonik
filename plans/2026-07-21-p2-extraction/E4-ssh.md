# Unit E4 — ssh / transport → `internal/transport/{tunnel,codesync}` behind `CommandRunner` + `workers.Registry`

**Status:** READY (for slices E4a + E4b + optional E4c). Slice **E4d is PARKED** — it belongs to E5, not to E4, and nothing E4 can do unblocks it.
**Depends on:** nothing. E4a/E4b have no prerequisite unit; they can land before, after, or alongside E1/E2/E3. (E4d depends on E5's run-machine lift.)
**Size:** ~1,702 LOC across 5 moved files (**619 non-test** across 2 files, **1,083 test** across 3 files) + 5 daemon files edited in place (3 production call-site files, 2 daemon test files that cannot move) + 4 stale doc-comment pointers. Optional E4c adds ~96 non-test LOC + 409 test LOC across 2 more test files.
**Risk:** **MEDIUM** — the moved code itself is a clean leaf (verified: zero daemon-internal free identifiers in both files), but the diff is *not* a bare `git mv`: it carries 13 exports, a forced doc-comment rewrite (revive `exported` + `package-comments`), two pre-existing errcheck findings that become "new" under the `--new-from-rev` ratchet, and two daemon test files that fail loudly if you forget them.

---

## 0. How the two input reports were reconciled

This plan takes the **adversarial challenge over the recon** on every point of disagreement, because in each case the challenge cited a concrete artifact I re-verified in the tree. Explicitly:

| Disagreement | Taken | Why |
|---|---|---|
| Feasibility label: recon `needs-a-preparatory-slice` vs challenge `move-with-modest-export-churn` | **Challenge** | Nothing must land before E4a/E4b — recon's own recipe says they ship immediately. Labeling the whole unit "needs a preparatory slice" would hold shippable work behind a phantom prerequisite. The deferred half is E5's scope, not an E4 prerequisite. |
| "`git mv` + rename, keep the header verbatim" (recon) vs "revive forces a full doc-comment rewrite" (challenge) | **Challenge** | Verified: `.golangci.yml:88` enables revive with `{name: exported}` and `{name: package-comments}`. `internal/daemon/reversetunnel.go:1` is `package daemon` with the 35-line header *below* it — so it is **not** a package comment and cannot be kept in place. The already-landed sibling extraction `internal/gitprobe/gitprobe.go` opens with a 19-line `// Package gitprobe …` block *above* the package clause and renamed every doc comment to the exported name. That is the established house convention; the recon contradicts it. |
| Verification gate `go build ./...` (recon) | **Challenge** | Verified broken today, and it has nothing to do with this unit: `plans/2026-07-15-agent-substrate-v2/investigate/10-zeromq-experiments/*.go` are untagged `package main` files importing `go-zeromq/zmq4`, `pebbe/zmq4`, `go.nanomsg.org/mangos/v3` — none in `go.mod`. `go build ./internal/... ./cmd/...` is green. Use the scoped form. |
| `indexOf` "latent duplicate-name hazard" (recon) | **Challenge** | Verified false. `internal/daemon/reversetunnel_test.go:134` is the only package-level `func indexOf`; `codexlaunchspec_test.go:496` declares `indexOf := func(...)` — a function-local closure. No collision exists and none is removed. The *substantive* half survives: nothing else in daemon uses the package-level `indexOf`, so the move is safe. |
| E4c rationale "avoids adding a handlercontract edge to the workers allow-list" (recon) | **Challenge** | Verified false: `grep -n 'internal/workers' .golangci.yml` returns nothing — there is no `workers:` depguard block. The real argument (workers today imports only `core` + `lifecycle/tmux`; adding `handlercontract` genuinely widens its closure) survives and is used below. |
| E4c "delete the now-duplicated `reportRunnerForWorker`" (recon) | **Challenge** | Verified false as a dedup. `internal/workers/report_poll.go:43 reportRunnerForWorker(w Worker)` is per-worker. `internal/daemon/workloop.go:1312 bootHealthRunner(cfg workers.Config)` iterates the config, `continue`s past disabled workers, and returns on the **first enabled** one — returning `nil` (not `continue`) when that first enabled worker is non-ssh. Different arity, different semantics. Collapsing them is a behavior change smuggled into a slice reviewed as a pure move. **Do not do it.** |
| E4c `bus.Emit` method value (recon) | **Challenge** | `workloop.go:1290-1295` guards `if bus != nil { emit = bus.Emit }`. Taking `bus.Emit` off a possibly-nil `handlercontract.EventEmitter` interface panics at the call site instead of degrading to no-emit. The nil guard must be preserved at the new call site (§4, step E4c-3). |
| `tunnel.ReverseTunnelRunner` cross-package-global **race** risk (recon) | **Challenge** | `internal/daemon` and `internal/transport/tunnel` compile into separate test binaries, so the two swaps can never interleave. Inside the daemon binary only `workloop_gate_n5md3_test.go` mutates it — identical to today. Still worth a `SetRunnerForTest` follow-up for legibility, but it is not a *new* hazard. |

Where they **agree** — and I re-verified — the recon's central claim stands: `reversetunnel.go` and `codesync_rs_b8.go` have **zero** free identifiers resolving inside `package daemon`, and every call-site line number the recon lists is correct.

---

## 1. What moves

### Moves out of `internal/daemon`

| File | LOC | Destination | Notes |
|---|---|---|---|
| `internal/daemon/reversetunnel.go` | 361 | `internal/transport/tunnel/tunnel.go` | **E4a.** Zero daemon-internal free identifiers (verified line by line: stdlib `context/fmt/net/os-exec/path-filepath/strconv/strings/sync/time` + `tmuxpkg.CommandRunner`/`SSHRunner` + `workers.Worker`/`workers.DefaultHarmonikPath`). Exports 11 symbols. Header comment must be **restructured above** the package clause as `// Package tunnel …`. |
| `internal/daemon/reversetunnel_test.go` | 540 | `internal/transport/tunnel/tunnel_test.go` | **E4a.** Imports stdlib + `lifecycle/tmux` only. Its `indexOf` helper (`:134`) is file-local. |
| `internal/daemon/reversetunnel_cnp17_test.go` | 95 | `internal/transport/tunnel/tunnel_cnp17_test.go` | **E4a.** Imports `strings` + `testing` only. |
| `internal/daemon/codesync_rs_b8.go` | 258 | `internal/transport/codesync/codesync.go` | **E4b.** Zero daemon-internal free identifiers (stdlib `bytes/context/errors/fmt/os/strings/time` + `tmux.CommandRunner`/`LocalRunner` + `workspace.TaskBranchName`). Exports 2 symbols. Same package-comment restructure. |
| `internal/daemon/codesync_rs_b8_test.go` | 448 | `internal/transport/codesync/codesync_test.go` | **E4b.** Imports stdlib + `lifecycle/tmux` + `workspace`. Helpers `newNoOpRecorder` (`:29`) and `argvSliceEqual` (`:438`) are used only inside this file (grep-verified across all of `internal/daemon`). |
| `internal/daemon/workerregistry_bootwire_test.go` | 215 | `internal/workers/bootwire_test.go` | **E4c only.** Self-contained (`oneWorkerCfg`/`passingRunner`/`failingRunner` are file-local; no name collision with any of `internal/workers`' existing `_test.go` files — checked). Also imports `internal/core` (workers already depends on core). |
| `internal/daemon/workerregistry_hkqmyis_test.go` | 194 | `internal/workers/bootwire_hkqmyis_test.go` | **E4c only.** Self-contained; `hkqmyisWorkerCfg` is file-local. |

Plus, **E4c only**, `internal/daemon/workloop.go:1227-1322` (~96 LOC: `buildWorkerRegistry`, `buildWorkerRegistryWithRunner`, `bootHealthRunner`) → `internal/workers/bootwire.go`.

### Files edited in place (do NOT move)

| File | LOC | Why it stays | Edit required |
|---|---|---|---|
| `internal/daemon/workloop.go` | 8,378 | This is E5's mass. Only three regions are transport-shaped, and the biggest of them is unmovable (see §3a). | 16 call-site rewrites + 1 import. |
| `internal/daemon/reviewloop.go` | 2,172 | Review run-loop; only three transport call sites. **HOT** (2,172 LOC, heavy churn) — land E4a promptly after branching. | 3 call-site rewrites (`:234`, `:1126`, `:1134`) + imports. |
| `internal/daemon/dot_cascade.go` | 2,683 | DOT cascade; one transport call site. **HOT** (94 commits/90d). | 1 call-site rewrite (`:226`) + import. |
| `internal/daemon/conformance_m4c7_test.go` | 433 | A structural audit that reads source files by hard-coded path. Cannot move. | **MUST** be edited (`:317-322`) or it `t.Fatalf`s on a missing path. |
| `internal/daemon/workloop_gate_n5md3_test.go` | 381 | Drives `beadRunOne` end-to-end inside the daemon package. | **MUST** be edited (`:237-239`) — swap `tunnelpkg.ReverseTunnelRunner` instead of the old package var. |
| `internal/daemon/sandboxgate.go` | 264 | References `resolveAgentDaemonSocket` in comments only (`:67`, `:77`). | Comment-name touch-up only (optional but do it). |
| `internal/lifecycle/socketpathlimit.go` | — | Not daemon code. | Comment path fix at `:23` (`daemon/reversetunnel.go` → `internal/transport/tunnel/tunnel.go`). |
| `internal/hookrelay/hookrelay.go` | — | Not daemon code. | Comment path fix at `:497` (`internal/daemon/reversetunnel.go tcpEndpointPrefix`). |
| `internal/workspace/remotematerialize.go` | — | Not daemon code. | Comment path fix at `:39-40` (names both `ensureWorkerHarmonikDir` **and** `fetchBaseOnWorker`). |

### Files that STAY untouched, and why

- **`internal/daemon/agentready.go` (207)** — `defaultRemoteAgentReadyTimeout` (`:80`) / `effectiveAgentReadyTimeout` (`:89`) have **five** consumers outside this unit (`workloop.go:4896`, `dot_gate.go:473`, `dot_cascade.go:1768`, `reviewloop.go:582`, `reviewloop.go:1363`). Five run-loop consumers ⇒ run-loop policy, not transport. Revisit at E5.
- **`internal/daemon/bootworkloop.go` (323)** — already calls *through* the workers seam (`workers.RunReportLoop`, `workers.ProductionRunnerForWorker`). Nothing to extract.
- **`internal/daemon/runports.go` (342)** — holds a `*workers.Registry` field (`:326`). That is the E5 ports surface, not transport code.
- **`internal/daemon/tmuxsubstrate.go` (2,909)** — subprocess hosting sits behind `handler.Substrate`, a *different* seam (plan §2 row 2). Explicitly **not** E4.
- **`internal/daemon/substrate_runner_parity_hkfxy9_test.go` (266)** — its three `fetchBaseOnWorker` hits (`:106`, `:117`, `:120`) are all inside comments / `t.Skip` strings. No edit.
- **`internal/daemon/scenario_remote_substrate_localhost_test.go`** — `fetchRunBranchBoxA` hit at `:52` is a comment. `//go:build scenario`, `package daemon_test`, so it cannot even reference the unexported symbols. No edit.
- **`internal/daemon/claudelaunchspec_remote_hkz8ek_test.go`** — `resolveAgentDaemonSocket` hit at `:64` is a comment. No edit.
- **`internal/daemon/agentspawnsem_hk5z1f0_test.go`** — pins the per-worker cold-start semaphore through the unexported `workLoopDeps` field. E5-bound.

---

## 2. The seam it exits behind

**No new seam is invented.** Both destination packages sit entirely behind two pre-existing interfaces:

1. **`tmux.CommandRunner`** — defined at `internal/lifecycle/tmux/runner.go:16`, owned by `internal/lifecycle/tmux`, with the two pre-existing implementations `LocalRunner` and `SSHRunner` in the same file. Every remote command in both moved files already goes through `r.Command(ctx, …)` on a `CommandRunner`. This is the local-vs-ssh execution seam named in plan §2 row 3.
2. **`workers.Registry` + the worker record** — `internal/workers/registry.go:10`; the moved tunnel code consumes only `workers.Worker` and `workers.DefaultHarmonikPath`. This is plan §2 row 4.

Nothing in E4a/E4b needs a port injected, a closure threaded, or a new interface declared. The exported surface after the move is 13 plain functions/consts/vars over `CommandRunner`, `workers.Worker`, `string`, and `int`.

**No RED FLAG for E4a/E4b.** The one place a new port would be needed — the workloop-embedded remote orchestration — is exactly the part deferred to E5 (§3a), where the port already exists in-flight (`RunPorts`/`LedgerPort`, the RT slice series).

`internal/transport/codesync` additionally imports `internal/workspace` for `workspace.TaskBranchName`. `internal/workspace` is verified daemon-free today (`go list` shows `core`, `handlercontract`, `lifecycle/tmux`, `uuid` and stdlib only).

---

## 3. Coupling to break

### 3a. Outbound (unit → daemon)

**Headline: for the two moving files, there is none.** I re-verified line by line — every identifier in `reversetunnel.go` and `codesync_rs_b8.go` resolves to stdlib, `internal/lifecycle/tmux`, `internal/workers`, or `internal/workspace`.

The rows below are the couplings of the workloop-embedded material the recon proposed for E4 and this plan **defers to E5**. They are listed so no implementer re-attempts them.

| Symbol | Defined in | Used by | Resolution |
|---|---|---|---|
| `remoteBeadCtx` | `internal/daemon/workloop.go:3527` — a type declared **inside the body of `beadRunOne`** | 20 `rbc != nil` / `rbc == nil` branch sites across `workloop.go:3554-4896` | **BLOCKER — the single reason the workloop half cannot ship.** A function-local type has no name outside `beadRunOne`; it cannot be referenced from another package until E5 lifts the run machine. Not fixable inside E4. |
| `workLoopDeps` | `internal/daemon/workloop.go` (~300-800) | tunnel-setup block `:3614-3708`; cold-start semaphore `:4663-4675` | **BLOCKER.** The block reads `deps.projectDir`, `.bus`, `.tidGen`, `.brAdapter`, `.intentLogDir`, `.brTimeoutCfg`, `.workerRegistry`, `.localInFlight`, `.runRegistry`, `.agentSpawnSem`. Gated on E5's `RunPorts`. |
| `brAdapter.ReopenBead` | daemon br-adapter seam | `workloop.go:3654`, `:3706` (both tunnel-failure reopen paths) | **BLOCKER.** The readiness gate's *failure action* is a bead-ledger mutation, not a transport concern. Rides E5's `LedgerPort`. |
| `tidGen.Next` | `internal/daemon` | `workloop.go:3653`, `:3705` | **BLOCKER.** Same reopen action; rides E5's ports. |
| `deps.runRegistry.Get` / `handle.Remote.Store` | `internal/daemon/runregistry.go` | `workloop.go:3583-3584` | **BLOCKER.** Remote/local slot accounting is run-registry state; rides E5. |
| `deps.localInFlight` | `internal/daemon/workloop.go` | `workloop.go:3577-3579` | **BLOCKER.** Local-vs-remote capacity accounting; rides E5. |
| `notifyWorkerOffline` | `workloop.go:3714` (closure over `rbc` + `deps`) | `workloop.go:3740`, `:3829`, the B11 mid-run liveness path | Moves with the E5 slice. |
| `deps.bus.Emit` | `workLoopDeps.bus` | `workloop.go:3650`, `:3702` (`workers.EmitWorkerTunnelFailedEvent`), `:3717` (`workers.EmitWorkerOfflineEvent`) | **inject-as-port (later).** `workers.EmitFunc` already *is* the port shape; when the block moves, pass a `workers.EmitFunc`, never the bus. |
| `effectiveAgentReadyTimeout` / `defaultRemoteAgentReadyTimeout` | `internal/daemon/agentready.go:89`, `:80` | `workloop.go:4896`, `dot_gate.go:473`, `dot_cascade.go:1768`, `reviewloop.go:582`, `:1363` | **Leave in daemon.** Five run-loop consumers ⇒ policy, not transport. Revisit at E5. |
| `hasAPIKeyInEnv` | `internal/daemon` | `workloop.go:4371` (the `rbc != nil` billing fail-closed chokepoint) | **Do not touch.** It is the D2 chokepoint asserted by `conformance_m4c7_test.go`. Out of E4 scope. |
| `lifecycle.ValidateSocketPathLength` | `internal/lifecycle` | `workloop.go:3646` | No work — already an external leaf; a future transport package could import it directly. |
| `handlercontract.EventEmitter` | `internal/handlercontract` | `buildWorkerRegistry` / `…WithRunner` (`workloop.go:1255`, `:1267`) | **E4c only — the ONE non-verbatim signature change in this unit.** `internal/workers` today imports only `core` + `lifecycle/tmux`; adding a `handlercontract` edge genuinely widens its dependency closure. Change the param to the existing `workers.EmitFunc` and build the func value **with the nil guard preserved** at the caller (§4, E4c-3). Isolate this in its own slice so the §5.1 pure-move review of E4a/E4b is not contaminated. |

### 3b. Inbound (daemon → unit)

**13 symbols need export** (11 in `tunnel`, 2 in `codesync`).

| Current unexported name | Proposed exported name | Call sites to rewrite |
|---|---|---|
| `workerHarmonikPath` | `tunnel.WorkerHarmonikPath` | `workloop.go:4014`, `:4165`, `:4333` |
| `workerSocketReadyTimeout` | `tunnel.WorkerSocketReadyTimeout` | `workloop.go:3697` |
| `reverseTunnelRunner` | `tunnel.ReverseTunnelRunner` (exported mutable var — see §7) | `workloop.go:3665`; `workloop_gate_n5md3_test.go:237-239` (swap) |
| `workerTCPEndpoint` | `tunnel.WorkerTCPEndpoint` | `workloop.go:3624` |
| `allocateReverseTunnelPort` | `tunnel.AllocatePort` | `workloop.go:3617` |
| `releaseReverseTunnelPort` | `tunnel.ReleasePort` | `workloop.go:3628` |
| `buildReverseTunnelArgs` | `tunnel.BuildArgs` | `workloop.go:3664` |
| `sshHostOpts` | `tunnel.SSHHostOpts` | `workloop.go:3660`, `:3736`, `:3808`; `reviewloop.go:1126` |
| `resolveAgentDaemonSocket` | `tunnel.ResolveAgentDaemonSocket` | `workloop.go:4295`; `reviewloop.go:234`; `dot_cascade.go:226` |
| `ensureWorkerHarmonikDir` | `tunnel.EnsureWorkerHarmonikDir` | `workloop.go:3631` |
| `waitWorkerSocketLive` | `tunnel.WaitWorkerSocketLive` | `workloop.go:3697` |
| `ensureBaseOnWorker` | `codesync.EnsureBaseOnWorker` | `workloop.go:3809` |
| `fetchRunBranchBoxA` | `codesync.FetchRunBranchBoxA` | `workloop.go:3737`; `reviewloop.go:1134` |

**Deliberately left unexported** (grep-verified: their only non-comment users are the in-package test files that move with them):
- tunnel: `workerSocketPollInterval`, `tcpEndpointPrefix`, `tcpEndpointAddr`, `reservedTunnelPorts`, `reservedTunnelPortsMu`.
- codesync: `fetchBaseOnWorker`, `pushBaseToWorker`, `workerSSHURL`, `isRefNotFoundError`, `errBaseSHAAbsent`, `fetchRunBranchRetryCount`.

E4c adds 3 more exports (`workers.BuildRegistry`, `workers.BuildRegistryWithRunner`, `workers.BootHealthRunner`) with one call site (`workloop.go:1128`).

---

## 4. Step-by-step recipe

Run everything from the repo root `/Users/gb/github/harmonik`. Never `cd` into a worktree; use `git -C`.

### Slice E4a — `internal/transport/tunnel`

**E4a-1. Create the package and move the three files.**
```bash
mkdir -p /Users/gb/github/harmonik/internal/transport/tunnel
git -C /Users/gb/github/harmonik mv internal/daemon/reversetunnel.go            internal/transport/tunnel/tunnel.go
git -C /Users/gb/github/harmonik mv internal/daemon/reversetunnel_test.go       internal/transport/tunnel/tunnel_test.go
git -C /Users/gb/github/harmonik mv internal/daemon/reversetunnel_cnp17_test.go internal/transport/tunnel/tunnel_cnp17_test.go
```
*Check:* `git -C /Users/gb/github/harmonik status --short` shows three `R` renames.

**E4a-2. Change the package clause and restructure the header into a package comment.**
In all three files, `package daemon` → `package tunnel`.

In `internal/transport/tunnel/tunnel.go` the 35-line header currently sits *below* `package daemon` (line 1) and is therefore **not** a package comment. Move it **above** the `package tunnel` clause and open it `// Package tunnel …`, matching `internal/gitprobe/gitprobe.go`. **Keep all the rationale text verbatim** — the hk-ege6 block (why TCP loopback, not a unix socket: macOS sshd runs as root, so a `-R` StreamLocal bind creates a root-owned 0600 socket the unprivileged hook user cannot connect to) and the hk-cnp17 block (`ControlMaster=no` forces a dedicated non-multiplexed connection) are load-bearing documentation, not decoration. Only the framing changes.

*Check:* `head -3 internal/transport/tunnel/tunnel.go` starts with `// Package tunnel`.

**E4a-3. Export exactly 11 identifiers, and rewrite each one's doc comment to lead with the new name.**
The 11 renames are the table in §3b. Because revive's `exported` rule is on (`.golangci.yml:88`), **every doc comment must be rewritten to begin with the exported identifier** — `// BuildArgs constructs the argv …`, `// AllocatePort picks a free TCP port …`, `// ReverseTunnelRunner is the seam …`, and so on. Also update the *in-prose* mentions of renamed symbols inside those comments (e.g. the `allocateReverseTunnelPort` / `releaseReverseTunnelPort` cross-references in the `reservedTunnelPorts` comment, and the `waitWorkerSocketLive` mentions inside the `AllocatePort` rationale). Rename nothing else.

*Check:* `grep -c 'reverseTunnelPort\|reverseTunnelRunner\|buildReverseTunnelArgs' internal/transport/tunnel/tunnel.go` returns 0.

**E4a-4. Decide the two errcheck findings up front.** The file has two pre-existing unchecked results that are grandfathered today only because CI lints with `--new-from-rev` (`Makefile:421`, `:442`); a moved file is 100% new lines, so both surface as fresh findings:
- `tunnel.go` (was `reversetunnel.go:162`): `port := l.Addr().(*net.TCPAddr).Port` — an unchecked type assertion, and `errcheck.check-type-assertions: true`.
- `tunnel.go` (was `reversetunnel.go:165`): `l.Close()` — unchecked; `exclude-functions: ["(io.Closer).Close"]` does **not** match a `net.Listener` receiver.

**Fix them, do not suppress them.** Preferred:
```go
tcpAddr, ok := l.Addr().(*net.TCPAddr)
if !ok {
    _ = l.Close()
    return 0, fmt.Errorf("tunnel.AllocatePort: listener address %v is not TCP", l.Addr())
}
port := tcpAddr.Port
_ = l.Close()
```
A `//nolint` is a poor second choice here: `nolintlint` is configured `require-explanation: true, require-specific: true`, so a bare directive fails anyway. **Call this out explicitly in the PR body** — it is a (tiny) logic addition on an otherwise-mechanical slice, and plan §5.1's pure-move review will reject an undeclared delta.

**E4a-5. Rewrite the daemon production call sites.** Add
`tunnelpkg "github.com/gregberns/harmonik/internal/transport/tunnel"` to the imports of `workloop.go`, `reviewloop.go`, and `dot_cascade.go`, then:

`internal/daemon/workloop.go` —
`:3617` `allocateReverseTunnelPort()` → `tunnelpkg.AllocatePort()`;
`:3624` `workerTCPEndpoint` → `tunnelpkg.WorkerTCPEndpoint`;
`:3628` `releaseReverseTunnelPort` → `tunnelpkg.ReleasePort`;
`:3631` `ensureWorkerHarmonikDir` → `tunnelpkg.EnsureWorkerHarmonikDir`;
`:3660`, `:3736`, `:3808` `sshHostOpts` → `tunnelpkg.SSHHostOpts`;
`:3664` `buildReverseTunnelArgs` → `tunnelpkg.BuildArgs`;
`:3665` `reverseTunnelRunner` → `tunnelpkg.ReverseTunnelRunner`;
`:3697` `waitWorkerSocketLive` + `workerSocketReadyTimeout` → `tunnelpkg.WaitWorkerSocketLive` + `tunnelpkg.WorkerSocketReadyTimeout`;
`:4014`, `:4165`, `:4333` `workerHarmonikPath` → `tunnelpkg.WorkerHarmonikPath`;
`:4295` `resolveAgentDaemonSocket` → `tunnelpkg.ResolveAgentDaemonSocket`.

`internal/daemon/reviewloop.go` — `:234` `resolveAgentDaemonSocket` → `tunnelpkg.ResolveAgentDaemonSocket`; `:1126` `sshHostOpts` → `tunnelpkg.SSHHostOpts`.

`internal/daemon/dot_cascade.go` — `:226` `resolveAgentDaemonSocket` → `tunnelpkg.ResolveAgentDaemonSocket`.

All 16 are one-line qualified-name rewrites; there is no shadowing at any site.

**E4a-6. Fix the two daemon tests that cannot move.**
- `internal/daemon/workloop_gate_n5md3_test.go:237-239`: save/swap/restore `tunnelpkg.ReverseTunnelRunner` instead of the old package var. Keep the existing `t.Cleanup` restore **and** keep the test NOT parallel (the comment at `:232` explains why — preserve it, and note in it that the seam now lives in `internal/transport/tunnel`).
- `internal/daemon/conformance_m4c7_test.go:317-322`: change `readRepoFile(t, "internal", "daemon", "reversetunnel.go")` to `readRepoFile(t, "internal", "transport", "tunnel", "tunnel.go")`; change the two audited literals from `"reverseTunnelRunner"`/`"buildReverseTunnelArgs"` to `"ReverseTunnelRunner"`/`"BuildArgs"`; update the `t.Errorf` message's path text. **If you skip this you get `read …/internal/daemon/reversetunnel.go: no such file` from a `t.Fatalf`, not a compile error** — a confusing failure that looks unrelated.
  The file's other two floors are unaffected and must stay green: `countViaHelpers` ≥ 12 (`:373`, scans `internal/daemon` + `internal/workspace` for `func …Via` decls — neither moving file declares one) and the `rbc != nil` floor of 8 against `workloop.go` (20 occurrences today, none removed).

**E4a-7. Update the doc pointers that go stale** (all comment-only):
- `internal/lifecycle/socketpathlimit.go:23` — `daemon/reversetunnel.go` → `internal/transport/tunnel/tunnel.go`.
- `internal/hookrelay/hookrelay.go:497` — `internal/daemon/reversetunnel.go tcpEndpointPrefix` → `internal/transport/tunnel/tunnel.go tcpEndpointPrefix`.
- `internal/daemon/workloop.go:3565` — `(mirroring reversetunnel.go's tunnel opts)` → `(mirroring internal/transport/tunnel's tunnel opts)`.
- `internal/daemon/sandboxgate.go:67`, `:77` — `resolveAgentDaemonSocket` → `tunnel.ResolveAgentDaemonSocket`.

### Slice E4b — `internal/transport/codesync`

**E4b-1. Move.**
```bash
mkdir -p /Users/gb/github/harmonik/internal/transport/codesync
git -C /Users/gb/github/harmonik mv internal/daemon/codesync_rs_b8.go      internal/transport/codesync/codesync.go
git -C /Users/gb/github/harmonik mv internal/daemon/codesync_rs_b8_test.go internal/transport/codesync/codesync_test.go
```

**E4b-2.** `package daemon` → `package codesync` in both files, and restructure the 39-line DD1 header (currently below `package daemon`) into a `// Package codesync …` block **above** the package clause — same reason as E4a-2. Keep the hk-7bwx rationale (why the branch is fetched directly from the worker repo over SSH instead of round-tripping through GitHub) verbatim.

**E4b-3. Export exactly two identifiers** and rewrite their doc comments to lead with the new names: `ensureBaseOnWorker` → `EnsureBaseOnWorker` (`codesync.go:133`), `fetchRunBranchBoxA` → `FetchRunBranchBoxA` (`codesync.go:214`). Leave `fetchBaseOnWorker`, `pushBaseToWorker`, `workerSSHURL`, `isRefNotFoundError`, `errBaseSHAAbsent`, `fetchRunBranchRetryCount` unexported. Update in-prose mentions of the two renamed names throughout the file's comments.

**E4b-4. Rewrite three call sites.** Add `codesyncpkg "github.com/gregberns/harmonik/internal/transport/codesync"` and:
`workloop.go:3737` `fetchRunBranchBoxA` → `codesyncpkg.FetchRunBranchBoxA`;
`workloop.go:3809` `ensureBaseOnWorker` → `codesyncpkg.EnsureBaseOnWorker`;
`reviewloop.go:1134` `fetchRunBranchBoxA` → `codesyncpkg.FetchRunBranchBoxA`.
No daemon test edits are needed — every remaining daemon-test hit on these names is inside a comment or a `t.Skip` string (`substrate_runner_parity_hkfxy9_test.go:106/117/120`, `scenario_remote_substrate_localhost_test.go:52`).

**E4b-5. Update `internal/workspace/remotematerialize.go:39-40`** — it names *both* `ensureWorkerHarmonikDir` and `fetchBaseOnWorker` against `internal/daemon`; point at `internal/transport/tunnel` and `internal/transport/codesync` respectively.

### E4a/E4b-6 — the depguard block

Add this to `/Users/gb/github/harmonik/.golangci.yml` immediately **after** the existing `gitprobe:` block (which ends at the `deny:` line beginning `- { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "gitprobe is a leaf: …`), preserving that block's indentation exactly:

```yaml
        # transport: the remote-execution transport leaves (P2 unit E4). tunnel owns
        # the per-run `ssh -N -R` reverse tunnel + its readiness gate; codesync owns
        # the DD1 worker<->box-A git sync. Both sit BEHIND the pre-existing
        # lifecycle/tmux.CommandRunner seam (runner.go:16) and address workers via
        # internal/workers. They MUST NOT import daemon: P3's remote/container
        # dispatch links this transport, and a daemon back-edge would drag the
        # 57k-LOC monolith into every dispatcher — exactly the coupling E4 exists to
        # remove. Note the self-allow is the "internal/transport" PREFIX, which does
        # not collide with the daemon deny (unlike the bootconfig caveat below), so
        # both sub-packages and their external _test packages resolve cleanly.
        # Rationale: plans/2026-07-21-p2-extraction/_plan.md unit E4.
        transport:
          files: ["**/internal/transport/**"]
          allow:
            - "$gostd"
            - "github.com/gregberns/harmonik/internal/lifecycle/tmux"
            - "github.com/gregberns/harmonik/internal/workers"
            - "github.com/gregberns/harmonik/internal/workspace"
            - "github.com/gregberns/harmonik/internal/transport"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "transport is a leaf: importing daemon would re-couple every remote dispatcher to the monolith (P2 E4)" }
```

**Also add a belt-and-braces daemon deny to the `workspace` rule** if one exists, or note in the PR that `internal/workspace` and `internal/workers` are the transport's only non-stdlib, non-tmux dependencies and are verified daemon-free today (`go list -deps` over both yields only `core`, `handlercontract`, `handlercontract/lifecycle`, `tmux`, `workers`, `workspace`). If either ever grows a daemon import, the `transport:` deny edge will **not** catch it — the transitive edge is invisible to depguard.

### Slice E4c (OPTIONAL, SEPARATE PR — do not fold into E4a/E4b)

**E4c-1.** Move `internal/daemon/workloop.go:1227-1322` (`buildWorkerRegistry`, `buildWorkerRegistryWithRunner`, `bootHealthRunner`) into a new `internal/workers/bootwire.go` as `BuildRegistry`, `BuildRegistryWithRunner`, `BootHealthRunner`. Destination is `internal/workers`, **not a new package**, because that package already owns `Registry`, `NewRegistry`, `RunHealthCheck`, and a sibling transport resolver (`ProductionRunnerForWorker`, `report_poll.go:51`).

**E4c-2.** Change the `bus handlercontract.EventEmitter` parameter to `emit workers.EmitFunc` on both `BuildRegistry` and `BuildRegistryWithRunner`, and delete the now-redundant `if bus != nil { emit = bus.Emit }` block from inside `BuildRegistryWithRunner`. `internal/workers` today imports only `core` + `lifecycle/tmux`; taking `handlercontract` would widen its closure for no gain.

**E4c-3. Preserve the nil-emitter semantics at the caller.** `workloop.go:1128` becomes:
```go
var workerEmit workers.EmitFunc
if bus != nil {
    workerEmit = bus.Emit
}
workerReg := workers.BuildRegistry(ctx, cfg.Workers, workerEmit)
```
**Do NOT write `workers.BuildRegistry(ctx, cfg.Workers, bus.Emit)`** — taking a method value off a nil `handlercontract.EventEmitter` interface panics at the call site, turning today's silent no-emit degradation into a boot-time crash.

**E4c-4. Do NOT delete or merge `reportRunnerForWorker`.** It is per-worker; `BootHealthRunner` is per-config with first-enabled-wins semantics and a `nil` (not `continue`) return on a non-ssh first worker. They are different functions. Leave both; at most, update `report_poll.go:40`'s comment to say it mirrors `workers.BootHealthRunner` in this same package.

**E4c-5.** Move the two test files:
```bash
git -C /Users/gb/github/harmonik mv internal/daemon/workerregistry_bootwire_test.go internal/workers/bootwire_test.go
git -C /Users/gb/github/harmonik mv internal/daemon/workerregistry_hkqmyis_test.go  internal/workers/bootwire_hkqmyis_test.go
```
Change `package daemon` → `package workers`, drop the now-redundant `workers.` qualifiers, and replace `handlercontract.CollectingEmitter` with a small local `EmitFunc` recorder. Name-collision check already done: `oneWorkerCfg`, `passingRunner`, `failingRunner`, `hkqmyisWorkerCfg` have no counterpart in `internal/workers`' existing `_test.go` files.

**E4c-6. Declare the signature change in the PR body.** This is the only place in E4 where the diff is not a move + rename. Keeping it out of E4a/E4b is the cheap way to protect the clean slices' §5.1 review verdict.

### Slice E4d — DEFERRED, DO NOT ATTEMPT

The workloop-embedded remote orchestration: `remoteBeadCtx` (`workloop.go:3527`) and worker selection (`:3533-3585`); tunnel setup + readiness gate + reopen action (`:3614-3708`); `notifyWorkerOffline` (`:3714-3722`); the `ensureBaseOnWorker`/worktree critical section (`:3783-3840`); per-run runner threading (`:4001-4020`, `:4154-4170`, `:4391-4400`); the per-worker cold-start semaphore (`:4655-4675`); the remote agent-ready window (`:4896`). All 20 `rbc` predicate sites live inside one function and depend on `workLoopDeps`, `brAdapter.ReopenBead`, `tidGen`, `runRegistry`, `localInFlight`. **This is E5's `RunPorts`/`LedgerPort` threading, not an E4 preparatory slice** — there is nothing E4 can commission to unblock it.

---

## 5. Freeze tripwire

Two mechanisms, both required by plan §3.2 (ratified as a **hard CI failure**, not a warning).

### 5a. The deny edge (machine-checked by golangci-lint)

The `deny:` row inside the `transport:` block in §4 is the boundary test of plan §3.4:

```yaml
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "transport is a leaf: importing daemon would re-couple every remote dispatcher to the monolith (P2 E4)" }
```

**Be honest in the PR about what is NOT enforced:** plan §5.2 also asks for a "daemon deny-back edge". **There is no such edge available in this repo.** `.golangci.yml`'s `daemon:` block allows the whole `github.com/gregberns/harmonik/internal/` prefix, so daemon→transport is permitted by design (it must be — daemon calls the transport). §5.2's middle clause is a no-op here; do not record a false pass on it. The one-directional deny above plus §5b is the whole enforcement.

### 5b. The "no new files in daemon for this concern" guard (grep ratchet)

depguard cannot forbid *creating* a file, so this is a grep gate, following the established `scripts/codex-capture-pane-gate.sh` idiom. Create `/Users/gb/github/harmonik/scripts/transport-freeze-gate.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

# transport-freeze-gate.sh — P2 unit E4 freeze tripwire (_plan.md §3.2).
# The remote-execution transport concern LEFT internal/daemon in E4a/E4b:
# `ssh -N -R` reverse tunnelling lives in internal/transport/tunnel, the DD1
# worker<->box-A git sync lives in internal/transport/codesync. This gate fails
# the build if a NEW transport-shaped file appears in internal/daemon, or if the
# retired reverse-tunnel/codesync symbols are re-declared there. Extracted
# concern = closed door: P3 and anyone else builds on the transport leaves.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# (1) No new transport-named source file in the daemon package.
while IFS= read -r f; do
    echo "transport-freeze-gate: FORBIDDEN transport file in internal/daemon: $f" >&2
    HITS=$((HITS + 1))
done < <(find internal/daemon -maxdepth 1 -type f \
             \( -name '*reversetunnel*.go' -o -name '*revtunnel*.go' -o -name '*codesync*.go' \))

# (2) No re-declaration of the symbols that moved to internal/transport.
for sym in buildReverseTunnelArgs allocateReverseTunnelPort releaseReverseTunnelPort \
           waitWorkerSocketLive ensureWorkerHarmonikDir resolveAgentDaemonSocket \
           workerTCPEndpoint sshHostOpts workerHarmonikPath \
           fetchRunBranchBoxA ensureBaseOnWorker fetchBaseOnWorker pushBaseToWorker; do
    if grep -rn --include='*.go' -E "^(func|var|const)[[:space:]]+${sym}\b" internal/daemon >/dev/null 2>&1; then
        echo "transport-freeze-gate: FORBIDDEN re-declaration of ${sym} in internal/daemon:" >&2
        grep -rn --include='*.go' -E "^(func|var|const)[[:space:]]+${sym}\b" internal/daemon >&2
        HITS=$((HITS + 1))
    fi
done

if [ "$HITS" -ne 0 ]; then
    echo "transport-freeze-gate: FAIL — the ssh/transport concern was extracted in P2 E4; build on internal/transport/{tunnel,codesync}, do not reopen internal/daemon" >&2
    exit 1
fi
echo "transport-freeze-gate: OK — transport concern stays out of internal/daemon"
```

Hook it into the Makefile as a `.PHONY` target and call it from **both** `check-fast` (Tier 1, `Makefile:417`) and `check-short` (Tier 2 CI gate, `Makefile:438`), immediately after the `golangci-lint run` line in each:

```makefile
.PHONY: transport-freeze-gate
transport-freeze-gate:  ## P2 E4: forbid new ssh/transport files or moved symbols in internal/daemon
	scripts/transport-freeze-gate.sh
```

`chmod +x scripts/transport-freeze-gate.sh`. Confirm it is red **before** the move (it will fire on `reversetunnel.go`/`codesync_rs_b8.go`) and green after — that is the proof it actually tests something.

---

## 6. Verification gate

Run in this order, from `/Users/gb/github/harmonik`.

**1. Build (scoped — see §0).**
```bash
go build ./internal/... ./cmd/...
```
*Pass:* exit 0, no output. **Do not use `go build ./...`** — it fails today on `plans/2026-07-15-agent-substrate-v2/investigate/10-zeromq-experiments/*.go` (untagged `package main` importing `go-zeromq/zmq4`, `pebbe/zmq4`, `go.nanomsg.org/mangos/v3`, none in `go.mod`). That failure predates this unit and is unrelated to it. (It also means `make check-fast` / `make check-short` are red at HEAD for an unrelated reason — file that separately; do not let it mask an E4 regression.)

**2. Vet, including the build-tagged set.**
```bash
go vet ./internal/... ./cmd/...
go vet -tags scenario ./internal/daemon/...
```
*Pass:* both exit 0. The second line is required because `internal/daemon` contains `//go:build scenario` files (`scenario_remote_substrate_localhost_test.go:1`, `scenario_remote_substrate_localhost_dot_test.go:1`) that a plain `go test ./internal/daemon/...` never compiles.

**3. Tests.**
```bash
go test ./internal/transport/... ./internal/daemon/... ./internal/workers/... ./internal/workspace/... ./internal/hookrelay/... ./internal/lifecycle/... -count=1
```
*Pass:* all green. Specifically confirm `TestM4C7_SeamSurvival_StructuralFloors` and `TestWorkloopGate_N5MD3*` pass — those are the two that break if E4a-6 was skipped, and the first fails with a file-not-found `t.Fatalf` rather than a compile error.

**4. The boundary test (plan §3.4) — the one that actually proves the extraction.**
```bash
go list -deps ./internal/transport/... | grep 'gregberns/harmonik/internal/daemon' || echo "BOUNDARY OK: transport does not depend on daemon"
```
*Pass:* prints `BOUNDARY OK`. Any other output is a hard fail.

**5. Lint (depguard + revive + errcheck).**
```bash
.tools/golangci-lint run ./internal/transport/... ./internal/daemon/... ./internal/workers/...
```
*Pass:* clean. Note this is the **full** run, not `--new-from-rev` — deliberately, because the moved files are 100% new lines under the ratchet and you want to see every finding now rather than have CI surface it. Expect zero after E4a-3 (doc comments), E4a-2/E4b-2 (package comments), and E4a-4 (errcheck) are done.

**6. Freeze tripwire.**
```bash
scripts/transport-freeze-gate.sh
```
*Pass:* prints `transport-freeze-gate: OK`.

**7. Bug scan.**
```bash
ubs internal/transport/tunnel/tunnel.go internal/transport/codesync/codesync.go \
    internal/daemon/workloop.go internal/daemon/reviewloop.go internal/daemon/dot_cascade.go
```
*Pass:* exit 0.

**8. Format.**
```bash
make fmt-check
```
*Pass:* exit 0 (run `make fmt` first if it complains — gci will reorder the new import groups).

**9. The success metric — measure before and after.**
```bash
# BASELINE (measured 2026-07-22, at HEAD of phase1-session-restart-substrate):
#   top-level internal/daemon non-test files : 126
#   top-level internal/daemon non-test LOC   : 57197
ls internal/daemon/*.go | grep -v _test | wc -l
ls internal/daemon/*.go | grep -v _test | xargs cat | wc -l
```
**Expected after E4a + E4b: 124 non-test files (−2), 56,578 non-test LOC (−619).** Test-side: 3 test files / 1,083 LOC also leave. With E4c: 123 files (the `workloop.go` region is a partial-file carve, ~−96 LOC → ~56,482) and 2 more test files / 409 test LOC leave.

Publish `619 non-test LOC and 2 non-test files leave internal/daemon (plus 1,083 test LOC across 3 test files)` in the bead close comment, per plan §5.6.

**10. Runtime proof (plan §5.4) — schedule it, do not skip it silently.**
E4 has a run surface, so the gate requires driving **one remote bead end-to-end** (dispatch → tunnel establishes → agent_ready arrives over the tunnel → run branch fetched back to box A → merge) and confirming identical outcome vs pre-extraction. This needs the daemon up and a real worker host reachable; it cannot be cleared at design time. **Either schedule it explicitly before merge, or state in the PR that E4a/E4b landed on unit-test evidence only and name the bead that owns the deferred runtime proof.** Do not record a §5.4 pass you did not run.

---

## 7. Risks and how each is mitigated

1. **An implementer reads plan §2's "MEDIUM-HIGH, one unit" framing and tries the workloop half too.** It will not compile: `remoteBeadCtx` is declared *inside the body of* `beadRunOne` (`workloop.go:3527`) and threaded through 20 branch sites. A function-local type has no name outside its function. *Mitigation:* §3a lists every blocker with a line number; §4's E4d section says DO NOT ATTEMPT. Treat any attempt to reference `rbc` from `internal/transport` as an immediate stop.

2. **The slice is described as a pure `git mv` and it is not.** The real diff is 13 exports + a full doc-comment rewrite of both files (forced by revive `exported` + `package-comments`) + two errcheck fixes + two unmovable daemon test files rewritten + four stale doc pointers. *Mitigation:* the PR body must enumerate exactly these five categories up front, or plan §5.1's pure-move review will (correctly) reject a slice that was mis-advertised.

3. **`conformance_m4c7_test.go` is a structural audit that reads source by hard-coded path and `t.Fatalf`s.** Skipping E4a-6 yields `read …/internal/daemon/reversetunnel.go: no such file` — a failure that looks nothing like the change that caused it. *Mitigation:* E4a-6 is a numbered step, and step 3 of the verification gate names the test explicitly.

4. **`tunnel.ReverseTunnelRunner` becomes an exported mutable package var that a daemon test reaches across a package boundary to swap.** This is ugly, but per the challenge it is **not a new race**: daemon and tunnel compile into separate test binaries, and inside the daemon binary only `workloop_gate_n5md3_test.go` mutates it — identical to today. The real cost is legibility: the "NOT parallel" constraint becomes invisible to someone reading the tunnel package. *Mitigation:* keep and expand the `// NOT parallel` comment at `workloop_gate_n5md3_test.go:232`; file a **separate follow-up slice** to replace the var with an explicit `StartFunc` parameter or a `tunnel.SetRunnerForTest(t, fn)` helper. Do not do that refactor inside E4a.

5. **The reserved-port set is process-global by design and must stay singular.** `reservedTunnelPorts` is the hk-cnp17 TOCTOU fix for concurrent remote waves — one set per process, guarded by `reservedTunnelPortsMu`. Moving it preserves that property only as long as no second copy of the logic appears. *Mitigation:* the §5b freeze gate greps for `allocateReverseTunnelPort` re-declaration; the PR must state that a P3 container dispatcher **calls** `tunnel.AllocatePort`, never forks it.

6. **The transitive dependency edge is not covered by depguard.** `internal/transport/tunnel` needs `internal/workers`; `internal/transport/codesync` needs `internal/workspace`. Both are verified daemon-free today. If a later change makes `workspace` import `daemon`, the `transport:` deny edge will **not** fire — depguard checks direct imports. *Mitigation:* verification step 4 (`go list -deps`) catches it transitively; run it on every transport-touching change, and consider adding an explicit daemon deny to a `workspace:` depguard block as belt-and-braces.

7. **Three homes for remote-command helpers.** `internal/workspace/remotematerialize.go`'s own comment says it mirrors the daemon remote-command idiom; after E4 there is a real risk of drift across `workspace`, `workers`, and `transport`. *Mitigation:* state the boundary explicitly in the E4a PR description — **transport owns the ssh tunnel and the box-A↔worker git sync; workspace owns worktree materialisation; workers owns addressing and health** — and enforce it at review.

8. **`reviewloop.go` (2,172 LOC) and `dot_cascade.go` (2,683 LOC, 94 commits/90d) are HOT.** E4a touches one line in each; a slow-landing branch eats a rebase conflict on files that churn daily. *Mitigation:* land E4a within a day of branching. If it slips, rebase before finishing rather than at the end.

9. **The E4c signature change alters nil-handling if done naively.** `bus.Emit` on a nil interface panics; today's code degrades to no-emit. *Mitigation:* E4c-3 pins the exact caller code. This is the reason E4c is a separate PR — if it is wrong, it must not take E4a/E4b's clean verdict down with it.

10. **E4c's "dedup" temptation.** `reportRunnerForWorker` and `bootHealthRunner` look alike and are not. Merging them changes which worker's transport is probed. *Mitigation:* E4c-4 says do not merge, and states the semantic difference.

11. **`go build ./...` / `make check-fast` are red at HEAD for an unrelated reason.** The habit of waving a red gate through is exactly how a real breakage ships. *Mitigation:* use the scoped commands in §6, and file the zmq-experiments build break as its own defect so the gate can be restored.

12. **The §5.4 runtime proof cannot be cleared at design time.** *Mitigation:* §6 step 10 — either schedule the remote-bead run before merge, or say plainly in the PR that the slice landed on unit-test evidence and name the bead that owns the deferred proof. Do not record a false pass.

---

## 8. Rollback

Each slice is independently revertible; nothing outside its own files changes.

**Mid-flight, before commit (E4a or E4b):**
```bash
git -C /Users/gb/github/harmonik checkout -- .golangci.yml internal/daemon internal/lifecycle internal/hookrelay internal/workspace
git -C /Users/gb/github/harmonik checkout HEAD -- internal/daemon/reversetunnel.go internal/daemon/reversetunnel_test.go internal/daemon/reversetunnel_cnp17_test.go internal/daemon/codesync_rs_b8.go internal/daemon/codesync_rs_b8_test.go
rm -rf /Users/gb/github/harmonik/internal/transport
rm -f  /Users/gb/github/harmonik/scripts/transport-freeze-gate.sh
```
Then confirm recovery with `go build ./internal/... ./cmd/...` and `go test ./internal/daemon/... -count=1`.

**After commit, before merge:** `git -C /Users/gb/github/harmonik revert <sha>` — a single commit per slice, no cross-slice dependency, so the revert is clean. If E4a and E4b landed as two commits, revert E4b first (it is the smaller and has no dependency on E4a).

**After merge:** revert the slice's merge commit. The only shared-file edits are (a) the `transport:` block in `.golangci.yml`, (b) the call-site lines in `workloop.go` / `reviewloop.go` / `dot_cascade.go`, (c) the two daemon test-file edits, (d) the Makefile hook + `scripts/transport-freeze-gate.sh`. All four are additive or one-line, so a revert conflicts only if someone edited the same call-site lines meanwhile — resolvable by taking the pre-move unqualified names back.

**Partial rollback is legitimate:** E4a and E4b share nothing but the `.golangci.yml` block (which covers both by prefix) and the freeze script. Reverting one and keeping the other is safe; the depguard `transport:` block and the gate script harmlessly apply to whichever sub-package remains, and the gate's symbol list simply keeps whichever names are still out of daemon.

**Do not roll back by hand-editing the new package back into daemon.** Revert the commit so the `git mv` rename history stays intact — the next attempt will want it.
