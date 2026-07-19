# remote-substrate — M4 Decompose (components + F4 resolution + blast radius)

> **AUTHORED 2026-07-16** onto the code-revamp M4 framing, replacing the Phase-1 copy (archived at
> `_archive-phase1-landed/03-components.PHASE1.md`, whose DEC-A "worker-resident, collapse the
> dual paths" framing is SUPERSEDED by the operator lock — see `01-problem-space.md` §Locked
> decisions and `RECONCILE.md`). Same-file tasks MUST serialize (merge-conflict discipline).

## Framing: M4 is composition-root wiring + hardening

The SSH remote transport already exists in the merged tree (`SSHRunner`, the worker registry,
code-sync, remote materialization, the reverse tunnel). M4 v1 composes it onto the post-revamp M2
(`InputPort`/`Ack`) and M3 (`mergeq`) seams, closes the composition-root gaps so all three
harnesses can select the SSH runner, proves the mac-mini→`gb-mbp` topology for Claude first, and
relocates the merge `push` (F4). It KEEPS the runner-threading (DEC-A retained) and DEFERS the
dual-path cleanup.

## Components (buildable units)

| # | Component | Responsibility | Where |
|---|---|---|---|
| M4-C1 | **Claude remote e2e proof + hardening** | Confirm the landed tmux/SSH path drives a Claude process on `gb-mbp` end-to-end on the post-M2/M3 seams: reverse-tunnel hook relay (agent_ready + `agent_input_acked`), code-sync, merge-back. Fix whatever the revamp's M2/M3 rebuild broke. | `internal/daemon` (tmuxsubstrate, workloop, reversetunnel), `internal/workspace` |
| M4-C2 | **Ack-on-remote conformance** | Assert remote Claude `SubmitInput` returns `Ack{Delivered}`; positive acceptance is the async `agent_input_acked` relayed over the tunnel; dropped worker → `agent_input_stale`, never a wedge. | `internal/daemon/tmuxsubstrate.go`, input path |
| M4-C3 | **Composition-root runner selection for the Codex driver** | Today `substrate_select.go:40` hardcodes `ltmux.LocalRunner{}` into `codexdriver.Options.Runner`, so the Codex driver can never go remote. Make the Codex substrate's runner **per-run selectable** (from the same worker registry the tmux path reads), so `HARMONIK_SUBSTRATE=codexdriver` + a selected worker routes the codex process to `gb-mbp`. | `cmd/harmonik/substrate_select.go`, Codex spawn/dispatch path |
| M4-C4 | **Pi harness onto the SSH runner** | The Pi harness produces a `SpawnSpec` that runs UNDER the tmux substrate; make a worker-selected run spawn the Pi process on `gb-mbp` via the same `SSHRunner`, composing with Pi's landed `{Provider,BaseURL,API}` config (unchanged). | `internal/daemon/piharness.go`, dispatch path |
| M4-C5 | **F4 — relocate merge `push` outside the `mergeq` exclusive section** | Move `git push origin <target>` out of the `mergeq.Queue.Submit` critical section while preserving RSM-019 retry/taxonomy semantics and the RSM-018 exclusions. | `internal/daemon/workloop.go` (merge path ~6353-6723), `internal/mergeq` usage, `specs/run-state-machine.md` |
| M4-C6 | **STEP-0c honest-probe carry-forward** | Keep the `createworktree.go` honest-probe worktree guard as an explicit M4 acceptance item across the remote path (both local and SSH runner). | `internal/workspace/createworktree.go` |
| M4-C7 | **NFR7 + guardrail conformance** | Prove zero/disabled workers ⇒ byte-identical local; assert the `CommandRunner`/`…Via(runner)`/reverse-tunnel seam is NOT deleted and no `runner!=nil` branch is removed (DEC-A cleanup deferred). | tests across `internal/daemon`, `internal/workspace` |
| M4-C8 | **End-to-end remote proof (scenario / operational)** | The real mac-mini→`gb-mbp` proof for the Claude slice: a bead's Claude process runs on `gb-mbp`, commits, merges on the mini. Authored as a scenario/operational proof (not a daemon-gated bead). | scenario test + operational runbook (`WORKER-SETUP-macos.md`) |

## Build order (drives dispatch; same-file work serializes)

```
M4-C1 (Claude remote e2e + hardening) ─┬─ M4-C2 (Ack conformance)
                                       ├─ M4-C6 (STEP-0c carry) ─┐
                                       └─ M4-C8 (e2e remote proof, Claude) ─ [SLICE 1 DONE]
                                                                  │
M4-C3 (Codex runner at root) ── rides same seam ─────────────────┤
M4-C4 (Pi onto SSH runner)  ── rides same seam ─────────────────┤
                                                                  │
M4-C5 (F4 push relocation) ── independent; land after slice 1 ───┘
M4-C7 (NFR7 + guardrail conformance) ── continuous, gates every merge
```

- **Claude first (decision 3):** M4-C1 → C2 → C6 → C8 is the v1 slice; it must land and be proven
  before Codex/Pi.
- **Codex (C3) and Pi (C4)** ride the same seam next; C3 and C4 touch different files (Codex
  composition root vs. `piharness.go`) so they can proceed in parallel AFTER slice 1, but each is
  serialized against slice-1 changes to the shared dispatch path.
- **F4 (C5)** is independent of the harness work and lands after slice 1 (it touches the merge
  path, not the spawn path).
- **C1, C2, C8 all touch `internal/daemon` core files** (tmuxsubstrate, workloop) → dispatch
  SERIALLY, waiting for each merge, to avoid same-file merge-conflict auto-skips.

## Fork F4 — resolution (RSM-019 push relocation)

**Fork.** RSM-019 (`specs/run-state-machine.md:187-191`) and RSM-017 (`:175-180`) currently keep
`git push origin <target>` INSIDE the `mergeq` exclusive section and explicitly defer relocating it
"to the remote-execution work" — which is M4. The fork is: does M4 relocate it, and how, without
breaking the merge taxonomy or the exclusions?

**Resolution (M4 owns F4; relocate the push OUTSIDE the exclusive section):**

1. **Keep inside the exclusive section:** the local ref-advance (index restore, working-tree reset,
   the fast-forward/merge of `run/<id>` onto the local target) and the escaped-worktree /
   base-sync exclusions (RSM-018 unchanged). These mutate shared local state and MUST stay serial.
2. **Move OUTSIDE the exclusive section:** the network `git push origin <target>` itself. The push
   publishes an ALREADY-committed local target ref; it does not mutate the merge queue's shared
   local state. Under the M4 topology the push crosses the network from the mac-mini to origin, so
   holding the exclusive section across it needlessly serializes network I/O behind unrelated
   merges (the exact motivation RSM-019 names).
3. **Preserve RSM-019 taxonomy + retry:** a non-fast-forward push failure is a **retryable** merge
   outcome. When the push (now outside the section) loses a race — origin advanced under it — the
   run MUST **re-enter the exclusive section**, re-prepare (re-fetch, re-rebase/re-advance the
   local target), and re-attempt, up to the existing per-mode retry cap. Exhaustion → rejected
   outcome, reopen bead, failed run terminal — byte-identical to today's taxonomy. The retry loop
   is what makes an outside-the-section push safe: correctness comes from re-validating inside the
   section on conflict, not from holding the lock across the network.
4. **`br sync` reconciliation** (RSM-017) stays where it is relative to the (now-relocated) push:
   it follows a successful publish.
5. **Spec touch:** RSM-019 and RSM-017's "relocating the push … is deferred to the remote-execution
   work" clauses are updated to record that M4 performed the relocation, with the
   retry-re-enters-the-section rule as the new normative text. (Spec edit is part of M4-C5's
   change spec.)

**Why safe:** the invariant `mergeq` protects is *serial mutation of the local target ref +
working tree*, not *serial publication to origin*. Publication is idempotent-on-success and
retry-safe-on-conflict; moving it out trades a spurious network serialization for one extra
re-prepare on the rare lost race.

## Blast radius / file-touch list (ALIGNMENT-REVIEW criterion 3 — checkable)

The composition-root wiring points and the files each component touches. M4 v1 is **additive
wiring + a merge-path relocation + a spec edit**; it deletes no remote seam and removes no
`runner!=nil` branch (DEC-A cleanup deferred).

| Component | Files touched (primary) | Nature |
|---|---|---|
| M4-C1 Claude e2e/harden | `internal/daemon/tmuxsubstrate.go`, `internal/daemon/workloop.go` (worker selection ~3463/3490), `internal/daemon/reversetunnel.go`, `internal/workspace/remotematerialize.go` | verify + fix on post-M2/M3 seams; no deletions |
| M4-C2 Ack conformance | `internal/daemon/tmuxsubstrate.go:2245-2258`, input path + tests | assert `Delivered`; async-acked over tunnel; stale terminal |
| M4-C3 Codex runner at root | **`cmd/harmonik/substrate_select.go:31-43`** (the `ltmux.LocalRunner{}` at :40), Codex spawn/dispatch wiring, `internal/codexdriver/driver.go` (Options.Runner already exists — no change to the driver) | per-run runner selection replaces the hardcoded local runner |
| M4-C4 Pi onto SSH | `internal/daemon/piharness.go`, dispatch path that builds the Pi run's substrate | route Pi process to worker via same `SSHRunner`; provider config untouched |
| M4-C5 F4 push relocation | `internal/daemon/workloop.go` (merge/push path ~6353-6723), `internal/mergeq` call sites, `specs/run-state-machine.md` (RSM-017/019 text) | relocate push; preserve taxonomy; spec edit |
| M4-C6 STEP-0c carry | `internal/workspace/createworktree.go` | acceptance guard, both runners |
| M4-C7 NFR7 + guardrail | tests across `internal/daemon`, `internal/workspace`; grep-assert the seam survives | conformance only |
| M4-C8 e2e proof | `internal/daemon/scenario_*_test.go` (`//go:build scenario`) + `WORKER-SETUP-macos.md` | proof + runbook |

**Composition-root wiring points (the load-bearing seams M4 changes):**
1. `cmd/harmonik/substrate_select.go` — the AIS-015 substrate-selection axis; **the Codex driver's
   `Runner` is hardcoded to `LocalRunner` here (:40) and must become worker-selectable** (M4-C3).
2. **SSH-runner selection** — the tmux path already builds `SSHRunner{Host}` per selected worker in
   `internal/daemon/workloop.go:3463,3490` from the worker registry; M4 extends the SAME selection
   to the Codex driver (C3) and the Pi harness (C4).
3. **Workspace materialization path** — `internal/workspace/remotematerialize.go` `*Via(runner)`
   helpers + `createworktree.go` already route worktree/file writes through the runner; M4 confirms
   they hold for all three harnesses and carries STEP-0c (C6).

## Risks carried into change-spec

1. **Reverse-tunnel hook relay under the rebuilt M2 event path.** The worker-side agent's
   `agent_input_acked` must reach the daemon over the per-run tunnel (`reversetunnel.go`); the M2
   rebuild changed the input/ack event shape. C1/C2 must prove the async-acked event still lands.
2. **Codex per-run runner injection.** The Codex driver takes its runner at `Options` construction
   (`driver.go:88`), but `substrate_select` builds ONE substrate at boot. C3 must make the runner
   per-run without a runtime test-branch inside the driver (RS-017 twin-blindness): selection stays
   at the wire/root, the driver stays blind.
3. **F4 lost-push race.** The retry-re-enters-the-section rule (F4 resolution ¶3) must be covered by
   a test that forces origin to advance between prepare and push.
4. **NFR7 drift.** Every wiring change must keep the zero-workers path byte-identical; C7 guards it.
5. **Billing leak.** The `ANTHROPIC_API_KEY` fail-closed (health-check) + spawn-env strip must hold
   on all three remote harness paths, not just Claude.
