# WS4-4 debug brief — CODEX runs but produces zero edits in the scratch test env

**For a fresh agent. You have zero prior context; this is self-contained.**
**Read `plans/2026-07-13-code-revamp/COORD.md` entries c063–c068 only if you want the full trail — not required.**

---

## 1. Your mission

In an isolated throwaway ("scratch") daemon, the **codex** agent is supposed to pick up one
trivial seed task and drive it to a terminal `pass`. It doesn't. codex launches, runs, and then
the run fails with **`run_failed`: "implementer exited without advancing HEAD"**. Your job is to
find out **why codex leaves the worktree with no committed work**, and either fix it or produce a
precise, evidence-backed root cause classified as (a) sandbox/permission, (b) auth/model,
(c) workflow/config, or (d) a genuine product defect.

You may NOT reach "green" by weakening a test, adding a SKIP, faking an event, or loosening an
assertion. A real green or an honest documented RED are the only acceptable outcomes.

## 2. What this env is (and why the failure matters)

Same scratch-oracle setup as the pi brief: a throwaway daemon
(`scripts/scratch-daemon.sh`) runs a fixed "core-loop-proof" matrix — one cell per agent type
(claude / **codex** / pi) — and each cell must take a seed bead from `launch` to `pass`. This
is the milestone's acceptance oracle. If codex can't complete a trivial task here, the oracle
can't certify codex.

codex in the scratch matrix resolves its model from the operator's `~/.codex/config.toml`
(reported as `o4-mini`). Auth is **ChatGPT-subscription-based** and credentialed: `~/.codex`
auth is present, `forced_login_method="chatgpt"`, and crucially the daemon runs under a
**credential fence** — `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` are unset on purpose; only the
mounted subscription creds are usable. Keep that fence intact; do not "fix" this by exporting an
API key.

## 3. The exact failure — and the reframe that matters most

The run ends with `no_commit_during_implementer: HEAD did not advance past parent <sha>`
(the message you'll see is "implementer exited without advancing HEAD"). Emitter:
`internal/daemon/workloop.go:5209` (and the reviewloop analog ~`:885`).

**Read this before you theorize — it changes the whole diagnosis:**

The daemon has a **fallback that auto-commits for codex** when codex *edits files but forgets
to commit them*. See `internal/daemon/codexcommit.go` (~line 238): if the worktree is **dirty**
after codex runs, the daemon stages everything and creates the commit itself → HEAD advances →
the run is NOT a failure.

Therefore, for `no_commit_during_implementer` to fire, the worktree must have been **clean** —
meaning:

> **codex produced ZERO file changes.** This is not "codex did the work but forgot to commit."
> codex either did nothing, exited early, was blocked from writing, or wrote to the wrong place.

That is the crux. Your investigation is: *why did a launched, running codex leave an empty
worktree?* (Contrast with pi, which never even reaches "ready" — a totally different failure.
Do not conflate them.)

## 4. Ranked hypotheses to work through

1. **Sandbox / write permission.** The scratch env may run codex under a sandbox (seatbelt or a
   workspace-restriction) that blocks writes to the worktree path, so codex's edits silently
   fail and it exits with a clean tree. Check what sandbox/policy codex runs under here vs. the
   in-fleet path where it historically worked. (Note: there's a known pre-existing seatbelt
   flake `TestSandbox*_WriteToMainDenied` / `hk-tch4t` — related territory, worth a look.)
2. **codex exited early / errored before editing.** Auth handshake, model resolution, a startup
   error, or a prompt/turn that returned no tool calls. Read codex's own session output, not
   just the daemon event.
3. **Workflow-mode mismatch.** A prior wave hit exactly this class of bug: the scratch daemon
   booted the wrong workflow mode (review-loop) while cells pin `dot`. Confirm codex is running
   the workflow mode the cell expects, end-to-end.
4. **Wrote to the wrong path.** codex edited *something* but outside the worktree the daemon
   inspects (wrong CWD / wrong worktree), so `git status` in the checked path is clean.
5. **Model/quota.** `o4-mini` via the ChatGPT subscription — a quota/entitlement or a
   `stale_wal` classification issue. Note the config requires `codex.stale_wal_max_bytes`
   (already provisioned); the codex harness fail-louds without it. Verify it's actually taking
   effect.

## 5. How to reproduce

- Scratch daemon launcher: `scripts/scratch-daemon.sh`.
- codex config the scratch daemon appends:
  `scenarios/core-loop-proof/scratch-config-overlay.yaml` (the `codex.stale_wal_max_bytes` key;
  the model is NOT set by harmonik — it comes from `~/.codex/config.toml`, and `cells.json`
  pins codex `model_selected.model = null`).
- Ground truth = the scratch daemon's `events.jsonl`: read codex's run
  (`launch_initiated` → `harness_selected` → `model_selected` (codex→o4-mini) → the
  `run_failed`). Then go one level deeper into codex's OWN session transcript to see what it
  actually did during the turn. Filter structurally; don't hand-grep by run_id alone.
- Right after codex exits, inspect the worktree directly: is it truly clean? Any staged/unstaged
  changes? Any evidence codex tried to write and was denied? That single check (dirty vs clean)
  splits hypothesis #1/#4 from #2/#3.

Provisioning + deterministic dispatch are ALREADY fixed (prior waves). Confirmed working before
your failure point: `harness_selected` + `model_selected` fire (codex→o4-mini) and the codex
process launches and runs. So you start from a running-but-empty-handed codex.

## 6. Deliverable

A short written root cause with evidence (the events + the worktree state you observed), plus
ONE of:
- A verified fix (sandbox/policy/workflow/config) with the scratch matrix showing codex→`pass`
  for real, OR
- A precise statement that it's a product code change, naming the file/mechanism, with a
  known-RED reproduction, OR
- A precise statement that it's operator/infra (auth, quota, model), naming exactly what the
  operator must do.

Log your finding as a COORD entry (append-only; verify the max entry number first) and, if it's
a defect, file it in the tracker.

## 7. Key pointers

- HEAD-advance gate / failure emitter: `internal/daemon/workloop.go:5209`, reviewloop analog
  `internal/daemon/reviewloop.go:885`.
- codex commit/auto-commit fallback (the "dirty → daemon commits" logic that proves your tree
  was clean): `internal/daemon/codexcommit.go` (~line 238).
- Credential fence: daemon runs with `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` unset; only mounted
  `~/.codex` subscription creds. Do NOT breach it to get green.
