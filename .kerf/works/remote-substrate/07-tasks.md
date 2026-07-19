# remote-substrate — M4 Tasks (build order)

> **AUTHORED 2026-07-16** onto the code-revamp M4 framing, replacing the Phase-1 copy (archived at
> `_archive-phase1-landed/07-tasks.PHASE1.md`).
>
> **Beads B1–B12 are RETIRED.** They describe **already-shipped Phase-1 code** — the
> `CommandRunner`/`LocalRunner`/`SSHRunner` seam, the `internal/workers` registry, worker
> selection, code-sync, remote worktree ops, liveness probes, and the ssh-to-localhost scenario
> all MERGED (verify in tree: `internal/lifecycle/tmux/runner.go`, `internal/workers/`,
> `internal/daemon/codesync_rs_b8.go`, `internal/workspace/remotematerialize.go`,
> `internal/daemon/reversetunnel.go`). Do NOT re-implement them. The M4 tasks below consume that
> landed code and wire it onto the post-revamp M2/M3 seams.
>
> **This planning session is daemon-OFF and no-beads** (operator directive). The tasks below are a
> BUILD ORDER for the implementer, not beads. Same-file tasks MUST serialize (merge-conflict
> discipline). Component IDs map to `03-components.md`.

## Retired Phase-1 beads (describe shipped code — do NOT rebuild)

| Bead | Was | Status |
|---|---|---|
| B1 | `CommandRunner` + `LocalRunner`; OSAdapter runner field | **SHIPPED** — `internal/lifecycle/tmux/runner.go:16-27` |
| B2 | `SSHRunner` + OSAdapter wrapping | **SHIPPED** — `runner.go:72-121` (incl. the shell-quote hardening) |
| B3 | `workers.yaml` schema + loader | **SHIPPED** — `internal/workers/` |
| B4 | workers.yaml boot-wiring + CLI override | **SHIPPED** — `daemon.go:548-555` |
| B5 | worker registry + selection + live-disable | **SHIPPED** — `SelectWorker`, `workloop.go` |
| B6 | boot health-check (incl. API-key fail-closed) | **SHIPPED** — worker health path |
| B7 | remote worktree ops (runner-parametrized) | **SHIPPED** — `internal/workspace/createworktree.go` |
| B8 | code-sync fetch/push/box-A-fetch | **SHIPPED** — `internal/daemon/codesync_rs_b8.go` |
| B9 | remote liveness probes | **SHIPPED** — pasteinject runner-routed |
| B10 | substrate wiring + run-metadata worker identity | **SHIPPED** — `workloop.go:3463/3490`, reverse tunnel |
| B11 | offline detection → recover | **SHIPPED** — `IsSSHConnectionFailure` (`runner.go:128`), `worker_offline` |
| B12 | ssh-to-localhost scenario | **SHIPPED** — Phase-1 scenario test |

## M4 build order

### SLICE 1 — Claude remote, proven (decision 3: Claude first)

**T1 (M4-C1) — Claude remote e2e + hardening on the post-M2/M3 seams.**
- **Do:** Confirm the landed tmux/SSH path drives a Claude implementer process on `gb-mbp`,
  end-to-end, on the rebuilt seams. Exercise: worker-selected dispatch → remote worktree/materialize
  → spawn Claude on the worker's tmux → reverse-tunnel relays `agent_ready` and `agent_input_acked`
  back → commit-detect over SSH → `run/<id>` push → mac-mini fetch + merge. Fix whatever the M2
  input-seam / M3 mergeq rebuild broke in this path.
- **Files:** `internal/daemon/tmuxsubstrate.go`, `workloop.go`, `reversetunnel.go`,
  `internal/workspace/remotematerialize.go`.
- **Accept:** a worker-selected Claude run completes end-to-end against a real (or localhost-SSH)
  worker; the async `agent_input_acked` is observed over the tunnel; no seam deleted.

**T2 (M4-C2) — Ack-on-remote conformance** *(after T1; touches tmuxsubstrate → serialize with T1)*.
- **Do:** Assert remote Claude `SubmitInput` returns `Ack{Outcome: Delivered}` (never a synthesized
  positive); positive acceptance is the async `agent_input_acked`; a dropped/partitioned worker
  reaches `agent_input_stale` within the AIS-INV-001 bound (never a silent wedge).
- **Files:** `internal/daemon/tmuxsubstrate.go:2245-2258` + input-path tests.
- **Accept:** the three assertions above are covered by gate-runnable tests.

**T3 (M4-C6) — STEP-0c honest-probe carry-forward.**
- **Do:** Keep the `createworktree.go` honest-probe worktree guard as an explicit acceptance item
  on both the local and SSH-runner paths (do not let the remote path bypass it).
- **Files:** `internal/workspace/createworktree.go` + test.
- **Accept:** the guard fires identically under `LocalRunner` and `SSHRunner`.

**T4 (M4-C8) — end-to-end remote proof (Claude slice).**
- **Do:** The mac-mini→`gb-mbp` proof for Claude: a bead's Claude process runs on the worker,
  commits, and the branch merges on the mini. Author as a scenario test (`//go:build scenario`,
  `t.Skip` if `ssh <worker> true` fails) plus the operational runbook update.
- **Files:** `internal/daemon/scenario_*_test.go`, `WORKER-SETUP-macos.md`.
- **Accept:** the commit lands on the mini's `main` via the real `ssh -- tmux/git` path with no
  manual step. **SLICE 1 DONE — the operator's v1 goal.**

### SLICE 2 — Codex + Pi ride the same seam (decision 2)

**T5 (M4-C3) — composition-root runner selection for the Codex driver** *(after slice 1)*.
- **Do:** Replace the hardcoded `ltmux.LocalRunner{}` at `cmd/harmonik/substrate_select.go:40` with
  a per-run runner selected from the same worker registry the tmux path reads, so
  `HARMONIK_SUBSTRATE=codexdriver` + a selected worker routes the codex process to `gb-mbp`. Keep
  the driver blind to the selection axis (RS-017 twin-blindness): selection stays at the wire/root.
- **Files:** `cmd/harmonik/substrate_select.go`, Codex spawn/dispatch wiring (NOT
  `internal/codexdriver/driver.go` — `Options.Runner` already exists).
- **Accept:** zero workers → local codex (byte-identical); one healthy worker → codex process on
  the worker via `SSHRunner`; driver has no worker/test branch.

**T6 (M4-C4) — Pi harness onto the SSH runner** *(parallel with T5; different files)*.
- **Do:** A worker-selected Pi run spawns the Pi process on `gb-mbp` via the same `SSHRunner`,
  composing with Pi's landed `{Provider, BaseURL, API}` config (unchanged — decision 6). M4 changes
  only WHICH host the Pi process runs on, not the provider wiring.
- **Files:** `internal/daemon/piharness.go`, dispatch path that builds the Pi run's substrate.
- **Accept:** Pi process runs on the worker; `base_url` still points at the configured LLM
  endpoint; provider config untouched.

### SLICE 3 — F4 push relocation (independent of harness work)

**T7 (M4-C5) — relocate merge `push` outside the `mergeq` exclusive section** *(after slice 1)*.
- **Do:** Per the F4 resolution in `03-components.md`: move `git push origin <target>` OUT of the
  `mergeq.Queue.Submit` critical section; keep the local ref-advance/working-tree reset and the
  RSM-018 exclusions inside. Preserve the RSM-019 taxonomy: a lost-race (non-FF) push RE-ENTERS the
  exclusive section, re-prepares, and re-attempts up to the retry cap; exhaustion → rejected +
  reopen + failed terminal. Update RSM-017/019 spec text to record the relocation + the
  re-enter-on-conflict rule.
- **Files:** `internal/daemon/workloop.go` (merge/push path ~6353-6723), `internal/mergeq` call
  sites, `specs/run-state-machine.md` (RSM-017/019).
- **Accept:** no build-class or push I/O inside the exclusive section for the relocated push; a
  forced lost-push race test re-prepares and succeeds; taxonomy byte-identical; RSM-018 exclusions
  intact.

### CONTINUOUS — conformance gate

**T8 (M4-C7) — NFR7 + guardrail conformance** *(gates every M4 merge)*.
- **Do:** Prove zero/disabled workers ⇒ byte-identical local operation. Grep-assert the
  `CommandRunner` / `…Via(runner)` / reverse-tunnel seam is NOT deleted and that no
  `runner!=nil`/`IsRemote` branch was removed (DEC-A cleanup deferred, decision 5). Assert the
  `ANTHROPIC_API_KEY` fail-closed + spawn-env strip hold on ALL THREE remote harness paths.
- **Accept:** NFR7 test green; seam-survival grep green; billing fail-closed covered for
  Claude/Codex/Pi.

## First task

**T1 (M4-C1) — Claude remote e2e + hardening.** It is the head of the Claude-first slice and the
prerequisite for T2/T3/T4; nothing else in M4 should start before the landed remote path is
confirmed working on the post-M2/M3 seams.

## Guardrails (binding on every task)

- **Do NOT delete the remote seam** (`CommandRunner`, `SSHRunner`, `*Via(runner)`, reverse tunnel).
  AIS-016 requires it and the M2 input path rides it.
- **Do NOT do the DEC-A dual-path cleanup** — keep the `runner!=nil`/`IsRemote` branches; deferred
  to a later evidence-backed pass (decision 5).
- **NFR7:** zero/disabled workers ⇒ byte-identical local.
- **Never set `ANTHROPIC_API_KEY`** on any remote path (D2 subscription-billing MUST).
- **Same-file tasks serialize** (T1/T2 both touch tmuxsubstrate; T5/T7 both touch workloop —
  order them, wait for each merge).
