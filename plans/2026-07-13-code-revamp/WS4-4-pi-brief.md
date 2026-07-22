# WS4-4 debug brief — PI never reaches `pass` in the scratch test env

**For a fresh agent. You have zero prior context; this is self-contained.**
**Read `plans/2026-07-13-code-revamp/COORD.md` entries c063–c068 only if you want the full trail — not required.**

---

## 1. Your mission

In an isolated throwaway ("scratch") daemon, the **pi** agent is supposed to pick up one
trivial seed task and drive it to a terminal `pass`. It doesn't. Your job is to find out
**why pi never gets there, and either fix it or produce a precise, evidence-backed root cause**
that says whether the fix is (a) a config/threshold change, (b) an environment/tunnel fix, or
(c) a genuine product defect that needs a code change.

You are NOT allowed to reach "green" by weakening a test, adding a SKIP, faking an event, or
loosening an assertion. A real green or an honest documented RED are the only acceptable
outcomes.

## 2. What this env is (and why the failure matters)

Harmonik runs coding agents against a task queue. To prove the whole loop works end-to-end
WITHOUT touching the live fleet, we boot a **scratch daemon** (`scripts/scratch-daemon.sh`)
against a temp checkout and feed it a fixed "core-loop-proof" matrix of cells — one cell per
agent type (claude / codex / **pi**). Each cell should take a seed bead from `launch` all the
way to `pass`. This is the acceptance oracle for the milestone. If pi can't complete a trivial
task here, the oracle can't certify pi.

pi = one of the real coding-agent harnesses. In the scratch matrix it is wired to the
**`ornith` model**, a locally self-hosted reasoning model reached over an SSH loopback tunnel
to a DGX box. (This is the operator's "pi probably has a local-model problem" — confirm or
refute it.)

## 3. The exact failure

pi's run dies with the event **`agent_ready_stall_detected`** after roughly **205 seconds**.

Read what that event means before theorizing: `internal/core/agentreadystall_hk1s1or.go`
(payload + doc comment) and the stall watcher that emits it (grep
`agent_ready_stall_detected` in `internal/daemon/`). The key fact:

> This event fires in the gap between **`launch_initiated`** and **`agent_ready`** — i.e. the
> agent process launched but **never signaled it was ready to work** within the watch window.
> pi is failing *upstream of doing any task work at all.* It is not "pi did the task wrong" —
> it's "pi never got to the starting line."

This is a fundamentally different failure from codex's (see the codex brief — codex DOES start
but produces no edits). Do not conflate them.

## 4. Prime hypothesis (operator's, worth testing first)

**ornith is a slow reasoning model and its time-to-ready exceeds the stall window.** A
reasoning model can spend a long time before its first streamed token; if "ready" is inferred
from first output, a genuinely-working-but-slow ornith looks identical to a hung one.

If that's the cause, the honest fix is likely one of:
- Raise the ready-stall threshold **for the pi/ornith harness specifically** (not globally —
  don't blind the detector for fast agents).
- Or fix whatever readiness signal pi uses so a slow-first-token model isn't mislabeled stalled.

But **verify before you patch.** A raised threshold that just moves a real hang later is worse
than the current loud failure. Prove ornith actually *does* become ready and complete the task
if given more time, before recommending a bigger window.

## 5. Ranked hypotheses to work through

1. **ornith latency > stall window** (above). Time it: how long until ornith produces its
   first token / pi's readiness signal? Is it ~205s+, or does pi hang forever?
2. **Tunnel / reachability.** pi points at `http://127.0.0.1:8551/v1` (loopback → DGX vLLM).
   The infra was reported present (`ssh -f -N -L 8551:localhost:8551 dgx`, key at
   `~/.config/harmonik/ornith.key`). Confirm the tunnel is actually up and ornith answers a
   raw completion request in-band, independent of harmonik.
3. **API-format mismatch.** The config uses `api: openai-completions` (NOT bare `openai`) and
   `api_key_env: ORNITH_API_KEY` / `api_key_file: ~/.config/harmonik/ornith.key`. A wrong
   request shape could make ornith never respond usefully → looks like a stall.
4. **Readiness-signal contract.** How does pi tell the daemon it's "ready"? If ornith's output
   format differs from what the readiness parser expects, the signal never arrives even though
   the model is answering.
5. **Model wedged / cold.** DGX/vLLM cold-start or a wedged model server. Check the DGX side.

## 6. How to reproduce

- Scratch daemon launcher: `scripts/scratch-daemon.sh` (its `init` provisions the config).
- Pi harness config that the scratch daemon appends:
  `scenarios/core-loop-proof/scratch-config-overlay.yaml` (the `harnesses.pi` block — provider
  `ornith`, model `ornith`, base_url loopback tunnel, api `openai-completions`).
- The matrix + cell pins live under `scenarios/core-loop-proof/` (`cells.json` pins pi
  `model_selected.model = "ornith"`).
- Ground truth is the scratch daemon's own `events.jsonl` — read pi's run from there
  (`launch_initiated`, `harness_selected`, `model_selected`, then the
  `agent_ready_stall_detected`). Do NOT hand-grep by run_id alone; filter structurally.

Provisioning is ALREADY fixed (a prior wave). Confirmed working before your failure point:
`harness_selected` + `model_selected` fire (pi→ornith), and the pi process actually launches.
So you start from a launching-but-not-ready pi, not from a mis-configured daemon.

## 7. Deliverable

A short written root cause with evidence (the actual events + timing you observed), plus ONE of:
- A verified fix (config/threshold/env) with the scratch matrix showing pi→`pass` for real, OR
- A precise statement that the fix is a product code change, naming the file/mechanism, with a
  known-RED reproduction, OR
- A precise statement that the fix is operator/infra (e.g. DGX wedged, wrong model deployed),
  naming exactly what the operator must do.

Log your finding as a COORD entry (append-only; verify the max entry number first) and, if it's
a defect, file it in the tracker — don't just leave it in chat.

## 8. Key pointers

- Event meaning: `internal/core/agentreadystall_hk1s1or.go`; emitter: grep
  `agent_ready_stall_detected` under `internal/daemon/`.
- Stall-threshold config: `sentinel.liveness_no_progress_n` is a *different* knob (G-liveness,
  set to 0 = off in scratch); the ready-stall window is its own thing — trace it from the
  emitter, don't assume it's the sentinel value.
- Pi commit/fallback path (for reference, downstream of your failure):
  `internal/daemon/picommit.go`.
- The daemon **auto-commits** if pi edits-but-doesn't-commit — so a *dirty-worktree* pi is NOT
  a failure. Your failure is earlier (never ready), so this won't save you, but know it exists.
