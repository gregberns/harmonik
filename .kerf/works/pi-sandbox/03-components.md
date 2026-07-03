# Pi-in-a-sandbox — Components / v1 build order

> Formalized from `plans/2026-07-02-pi-sandbox/HANDOFF.md` §8 (7-step build order) and §3 seams.
> These map 1:1 to the task beads created under label `codename:pi-sandbox`.

The v1 decomposes into 7 sequential-ish components. The **spike (1) gates everything else** — it
de-risks the network + Go-CLI-TLS mechanism and lands the working `srt` settings recipe.

## 1. SPIKE — de-risk network + Go-CLI TLS  [GATE, P1]

Manually `srt`-wrap a shell that runs `br ready`, `harmonik comms recv --json` (against the live
daemon over the unix socket), and one OpenRouter model call. Decide the Go-CLI-TLS approach (weaker
isolation vs. unix-socket-only vs. tools-outside-sandbox). Land the working `srt` settings recipe.
Output: a documented recipe + the TLS decision. **This gates every other bead.**

## 2. Profile / settings generator — `internal/daemon/sandboxprofile.go`  [P1]

Given a run's worktree path + git dirs (§4 writable set) + cache config (§6), emit the `srt` settings
JSON. Unit-tested: writable-set is exactly right (worktree + `.git/worktrees/<id>` + `.git/objects` +
scoped ref path + `$TMPDIR` + caches), and it emits **literal paths** (Linux-bwrap-compatible), no
globs. New file.

## 3. Argv wrap in the substrate — `internal/daemon/tmuxsubstrate.go`  [P1]

When `sandbox.backend=srt` and the harness is in `sandbox.harnesses`, launch
`srt --settings <profile> <agent-argv>` instead of the bare agent argv, inside the existing local
tmux worktree. Gated so `backend: none` is a no-op. Wrap inside the substrate spawn so any harness can
opt in.

## 4. Config block + threading — `internal/daemon/projectconfig.go` (+ workloop, composition root)  [P1]

Add the `sandbox:` block (§7) peer to `harnesses:`; fail-loud on missing required keys. Thread it
through `workloop.go` (~:984 `newWorkLoopDeps` + launch-spec path) and the composition root
(`cmd/harmonik/main.go` / `run.go`) into the substrate at construction.

## 5. Acceptance scenario test  [P1]

Mirror the L3 pattern (`internal/daemon/scenario_container_l3_hkyflqo_test.go`). Dispatch a mechanical
bead to a sandboxed Pi run; assert: **(a)** it commits to its branch, **(b)** a write to a main-repo
file is **denied**, **(c)** the result branch **merges**. Run on macOS; add a Linux variant (see #7).
This trio is the acceptance gate for the isolation guarantee.

## 6. Attachability check  [P2]

Confirm `tmux attach` into a running sandboxed Pi session behaves normally (native host process, low
risk). ~10-minute validation, not a design risk.

## 7. Linux pass  [P2]

Confirm the same profile generator emits literal-path bwrap-compatible settings; run the acceptance
scenario on a Linux node. Note the Ubuntu 24.04+ `apparmor_restrict_unprivileged_userns` bwrap caveat.

## Dependency graph

```
(1 spike) ── gates ──▶ (2 generator), (3 argv-wrap), (4 config)
(2)(3)(4) ─────────▶ (5 acceptance scenario)
(5) ───────────────▶ (6 attachability), (7 Linux pass)
```

- 2, 3, 4 all depend on the spike (they need its resolved settings recipe + TLS decision).
- 5 depends on 2, 3, 4 (needs the wired path).
- 6 and 7 depend on 5 (need a green macOS path first).

## Escalation backend (NOT v1)

OrbStack container path — promote the L3 `hkyflqoDockerExecRunner` to a production `DockerExecRunner`
implementing `tmux.CommandRunner`, routed via `spawnWindowVia` like the SSH branch — is the later
escalation backend behind the same `sandbox.backend` enum. Keep the enum open for it; do not build.
