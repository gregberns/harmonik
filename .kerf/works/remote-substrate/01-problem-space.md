# remote-substrate — M4 Problem Space (code-revamp)

> **AUTHORED 2026-07-16** onto the code-revamp M4 framing, replacing the Phase-1 copy (archived at
> `_archive-phase1-landed/01-problem-space.PHASE1.md`). The six operator decisions below were
> confirmed 2026-07-16 and are DURABLE CONSTRAINTS — not to be re-opened or re-derived.
>
> **What changed vs the archived Phase-1 doc.** Phase-1 asked "how do we add remote SSH
> execution?" and answered it — that code is MERGED and validated on a real remote Mac. M4 asks a
> narrower, downstream question: **now that the code-revamp rebuilt the input seam (M2) and the
> merge queue (M3), wire the already-landed SSH-runner substrate at the composition root so the
> harmonik daemon on the mac-mini drives the three agent harnesses' processes on a remote box —
> proven end-to-end, Claude first.** This is composition-root wiring + hardening, NOT a
> from-scratch build (§"Why this is mostly wiring").

## One-paragraph summary

The harmonik daemon runs on the **mac-mini**; the agent PROCESSES (the implementer Claude Code
sessions, and next the Codex and Pi harnesses) run on a **remote box (`gb-mbp`)**, driven remotely
by the daemon over SSH. "Where a session's process runs" is behind the harness-agnostic
`handler.Substrate` seam, with an SSH `CommandRunner` threaded through the dispatch path as the
remote transport. The remote transport LARGELY EXISTS already (Phase-1 landed `SSHRunner`, the
worker registry, code-sync, remote workspace materialization, and the per-run reverse tunnel). M4
v1's job is to (a) confirm/harden that path on the AS-BUILT M2 `InputPort`/`Ack` and M3 `mergeq`
seams, (b) close the composition-root gaps so all three harnesses — not just Claude/tmux — can be
selected onto the SSH runner, (c) prove the mac-mini→`gb-mbp` topology end-to-end for the Claude
slice, and (d) relocate the merge `push` out of the exclusive section (fork F4).

## Locked decisions (operator, 2026-07-16 — DURABLE, do not re-open)

1. **Topology.** The harmonik daemon runs on the **mac-mini**. The agent processes run on a
   **remote box (`gb-mbp`)**. The daemon drives them remotely. This is M4's whole point. (This is
   the same dispatching-host / remote-agent-host split Phase-1 built, with the concrete hosts
   named; it is NOT "move the daemon or crews off-box.")
2. **All THREE harnesses must be remote-supported** in the end-state — Claude (tmux substrate),
   Codex (`internal/codexdriver`), Pi (`daemon/piharness.go`) — via the harness-agnostic
   `handler.Substrate` seam. M4 wires an SSH runner behind that seam; "run on `gb-mbp`" is the same
   wiring for all three.
3. **v1 first remote slice = Claude** (most important to the operator). Prove the remote seam on
   the Claude/tmux harness first; Codex + Pi ride the same seam next.
4. **Architecture = Option A, runner-threaded (SSH `CommandRunner`).** NOT a worker-resident
   network agent. The gRPC/tailnet worker agent is a **Phase-3 cloud concern**, kept behind the
   same seam for later. Matches locked D1/D5 (v1 = single remote Mac over SSH, ship it working
   before any Phase-2).
5. **Defer the DEC-A dual-path cleanup.** Do NOT rip out the ~98 `runner!=nil`/`IsRemote`
   conditional branches across 17 files in M4 v1 — pure refactor risk, zero capability gain. Fold
   into a later evidence-backed cleanup once remote proves out.
6. **Pi remote composes with existing Pi provider config.** The Pi process running on `gb-mbp`
   (M4 = where the process runs) is independent of Pi pointing `base_url` at the DGX/OpenRouter LLM
   endpoint (already landed via `pi-provider-switch`). Don't redesign Pi provider config; just note
   the composition: Pi's harness carries `{Provider, BaseURL, API}` (see `piharness.go:71,149`),
   and M4 only changes WHICH host the Pi process runs on.

## The AS-BUILT seams M4 consumes (build onto these, not the Phase-1 speculative model)

These are verified against the merged tree. M4 designs onto them; it does not re-invent them.

- **Input seam (M2).** `handler.InputPort.SubmitInput(ctx, InputRequest{Payload,TurnIntent})
  (Ack, error)`, obtained via the `handler.AsInputPort` structural assertion
  (`internal/handler/input_port.go:29-47,111-119`). The `Ack` is **BINARY** —
  `Outcome ∈ {Delivered, Rejected}`, plus codec-owned `Seq`/`Token`
  (`input_port.go:59-103`). There is **NO** `Degraded`/`Accepted` tri-state. Positive acceptance is
  the asynchronous `agent_input_acked` event; a never-confirmed submission reaches
  `agent_input_stale` (bounded-liveness AIS-INV-001). **Remote Claude stays `Delivered`** —
  tmux/paste has no structured protocol, which is expected and fine for v1
  (`internal/daemon/tmuxsubstrate.go:2245-2250`).
- **Runner seam (AIS-016), already remote.** The interim tmux `SubmitInput` ALREADY routes over the
  SSH `CommandRunner` (the per-run substrate holds the runner; `tmuxsubstrate.go:2245-2308` and the
  `WriteLastPane`/paste path run on the worker's tmux server over that runner). The runner type is
  `internal/lifecycle/tmux.CommandRunner` with `LocalRunner` / `SSHRunner` implementations
  (`internal/lifecycle/tmux/runner.go:16-121`). `codexdriver.Options.Runner` is the declared
  AIS-016 remote seam for the structured driver (`internal/codexdriver/driver.go:52-89`). So the
  remote seam largely EXISTS.
- **Merge exclusion (M3).** `internal/mergeq` is landed (`mergeq.Queue.Submit(ctx, label,
  critical)` — `internal/mergeq/mergeq.go:121`). Per RSM-019 (`specs/run-state-machine.md:187-191`)
  the `git push` currently stays INSIDE the exclusive section; **M4 owns relocating `push` out**
  (fork F4).
- **Already-landed remote plumbing** (Phase-1, verified in tree): the worker registry +
  `SelectWorker` + `workers.yaml` (`internal/workers`, `daemon.go:548-555`), per-run
  `SSHRunner{Host}` selection in the workloop (`internal/daemon/workloop.go:3463,3490`), code-sync
  (`internal/daemon/codesync_rs_b8.go`), remote workspace materialization (`*Via(runner)` helpers,
  `internal/workspace/remotematerialize.go`), the STEP-0c honest-probe worktree guard
  (`internal/workspace/createworktree.go`), and the per-run SSH **reverse tunnel** that relays the
  worker-side agent's hooks (agent_ready / agent_input_acked) back to the daemon's hook socket
  (`internal/daemon/reversetunnel.go`).

## Why this is mostly wiring, not a build

The Phase-1 remote-substrate work merged substantially MORE than its own B1–B12 task list
describes: the worker registry, dispatch-time worker selection, code-sync, remote materialization,
and the reverse tunnel are all in the tree. Independently, the code-revamp's M2 rebuilt the input
path (`InputPort`/`Ack`) and M3 introduced `internal/mergeq`. M4 sits at the intersection: the
landed remote path must be confirmed to still work on the rebuilt M2/M3 seams, the composition root
must select the SSH runner for **all three** harnesses (today `substrate_select.go:40` hardcodes
`LocalRunner` for the Codex driver — a real gap), and the mac-mini→`gb-mbp` topology must be proven
end-to-end for Claude. The guardrail (below) exists precisely because "mostly wiring" invites the
temptation to also do the DEC-A cleanup — which is explicitly deferred.

## Hard constraints (carried from Phase-1, still binding)

- **Interactive auth, never `claude -p`; subscription billing is a MUST (D2).** Never set
  `ANTHROPIC_API_KEY`; the boot health-check fails closed if it is present on the worker. A
  persistent remote Mac with a one-time interactive login preserves subscription billing.
- **The dispatching host keeps merge authority (DEC-B).** Base-SHA resolution and the final
  merge-to-main run on the mac-mini; the worker fetches the base, the implementer commits, and the
  worker pushes a `run/<id>` branch that the mac-mini fetches and merges. (F4 relocates only the
  *timing* of the merge push relative to the exclusive section.)
- **NFR7 — zero-workers is byte-identical.** With no `workers.yaml` (or all workers disabled),
  every path is byte-identical to local-only operation. This is the regression floor for all M4
  wiring.
- **Do NOT delete the remote seam (M4 guardrail).** AIS-016 requires the `CommandRunner` /
  `…Via(runner)` seam and the M2 input path rides it. "Collapse dual paths" (deferred anyway) is
  NOT "remove remote capability." Deleting `SSHRunner`, the `*Via(runner)` helpers, or the
  reverse tunnel is out of bounds.
- **Carry the STEP-0c honest-probe worktree guard** forward as an M4 acceptance item
  (ROADMAP §STEP-0; `createworktree.go`).

## Success criteria (verifiable)

- **S1 — Claude remote, end-to-end.** A bead dispatched by the daemon on the mac-mini executes its
  implementer Claude session's process on `gb-mbp`, the agent's hooks (agent_ready /
  agent_input_acked) relay back over the reverse tunnel, the run commits on `gb-mbp`, and the
  `run/<id>` branch merges into main on the mac-mini — no manual per-bead step. (First slice.)
- **S2 — Codex + Pi ride the same seam.** After the Claude slice, selecting the Codex driver or the
  Pi harness onto a worker routes its process to `gb-mbp` through the same `CommandRunner` seam,
  with no per-harness special-casing of the transport. Pi composes with its landed `base_url`
  provider config (decision 6).
- **S3 — Ack semantics correct on remote.** Remote Claude `SubmitInput` returns `Ack{Delivered}`;
  positive acceptance arrives as the async `agent_input_acked` over the tunnel; a dropped worker
  reaches `agent_input_stale` (never a silent wedge).
- **S4 — F4 landed.** The merge `push` executes OUTSIDE the `mergeq` exclusive section, with the
  RSM-019 retry/taxonomy semantics preserved and the exclusion invariants (RSM-018) intact.
- **S5 — NFR7 preserved.** Zero/disabled workers ⇒ byte-identical local operation; the DEC-A dual
  paths remain (cleanup deferred), so no `runner!=nil` branch is removed in M4 v1.
- **S6 — Billing safety.** No remote run ever sets `ANTHROPIC_API_KEY`; the health-check fail-closed
  and the spawn-env strip both hold on the remote path.

## Non-goals (out of scope for M4 v1)

- The **worker-resident network agent** (gRPC/tailnet), containers, cloud-provisioned sandboxes —
  all Phase-2/Phase-3, kept behind the same seam (decision 4).
- The **DEC-A dual-path cleanup** — the ~98 `runner!=nil`/`IsRemote` branches stay; deferred to a
  later evidence-backed pass (decision 5).
- **Redesigning Pi provider config** — it already landed (`pi-provider-switch`); M4 only changes
  where the Pi process runs (decision 6).
- Moving the **daemon, captain, or crews** off the mac-mini — only per-bead agent processes go
  remote (Phase-1 D3, unchanged).
