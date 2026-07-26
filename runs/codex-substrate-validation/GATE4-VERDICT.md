# Live Gate #4 — codex-substrate DOT gate (yankee iso)

**Run:** `019f7cad-cad4-7a5c-a50c-da020c6ce358`
**Iso daemon binary:** commit `37651569` (`--version` confirmed; BOTH fixes compiled)
- Fix 1 = hk-qxvc2 @ `37651569` — route claude reviewer onto tmux substrate (not codex JSON-RPC driver)
- Fix 2 = hk-daegv @ `49d7fde3` — codex-exec sandbox writable git-common-dir so codex's own commit lands
**Substrate:** `HARMONIK_SUBSTRATE=codexdriver` · **Worker:** gb@100.87.151.114 (codex-cli 0.142.0)

## Verdict: Proof B GREEN · Proof A RED → HARD STOP invoked (no 4th patch)

### Proof B (hk-daegv — codex's OWN commit lands) = GREEN
Independently verified from `/tmp/h-yankee/proj/.harmonik/events/events.jsonl`:
- `implementer_phase_complete` (run 019f7cad): `commit_landed=true`, exit 0, ~142s.
- Worker worktree HEAD = `46f8f0b Add counter function` — a real codex commit subject, NOT
  "auto-committed by daemon fallback". `run/019f7cad-…` branch created; `counter.py` = `return 2`.
- Clean reversal of prior gate 019f7c46 (`commit_landed=false`, seatbelt-denied `.git`).
- **Fix 2 HOLDS on the real `codex exec` path.**

### Proof A (hk-qxvc2 — claude review reaches agent_ready → close) = RED
Timeline (UTC): `harness_selected` claude-code tier3 (23:22:20) → `reviewer_launched` (23:22:21) →
`agent_ready_stall_detected` stall 208s (23:23:24) → `agent_ready_timeout` 150000ms (23:25:53) →
`run_failed` "dot: agentic node \"review\" failed: node \"review\" agent_ready_timeout" (23:25:54).
- DOT bead `yri-wq7` stays OPEN; queue `paused-by-failure`. No `run_completed`.
- **Routing fix DID take effect:** a tmux session `harmonik-a3dc45482890-crew-yankee` was created on
  the worker at the exact `reviewer_launched` instant — the claude reviewer was routed onto the tmux
  substrate, NOT the codex JSON-RPC driver. Behaviorally confirmed.
- **But routing was necessary, NOT sufficient:** the review claude still never emits `agent_ready` on
  the remote worker even via tmux transport. Residual wedge is DOWNSTREAM of substrate routing.

## Residual wedge (for captain re-plan — NOT patched per HARD STOP)
The remaining stall maps to the SECOND half of Ruling-A fix #1 that was NOT in the committed patch:
stand up the hook reverse-tunnel (`tcp:59963`) on the remote worker for the tmux-substrate remote claude
launch, and/or remote onboarding/trust propagation, and/or the reviewer getting no task/context. The
worktree was auto-cleaned on `run_failed`, so the review claude's session jsonl is gone.
**Recommendation:** captain-orchestrated fanout on "why does the tmux-routed remote claude reviewer not
reach agent_ready" rather than a blind 4th patch.

## Preservation (iso LEFT UP this time)
- Iso daemon pid 64223 + supervisor 64871; worker repo `/Users/gb/harmonik-yankee-iso/repo` intact.
- Fresh binary staged to yankee-specific `/Users/gb/harmonik-yankee-iso/bin/harmonik` + `harmonik_path:`
  in iso `workers.yaml` — prod `/Users/gb/go/bin/harmonik` deliberately NOT overwritten.
- Prod untouched: `/Users/gb/github/harmonik` HEAD `37651569`; prod worker + assessor iso never touched.

## Key paths
- Events: `/tmp/h-yankee/proj/.harmonik/events/events.jsonl` (gate run = lines 55–104)
- Worker codex rollout: `~/.codex/sessions/2026/07/19/rollout-2026-07-19T16-19-56-019f7cad-dab5-…jsonl`
- Worker run branch (survives cleanup): `run/019f7cad-…` tip `46f8f0b` in `/Users/gb/harmonik-yankee-iso/repo`
- Iso daemon log: `/tmp/h-yankee/daemon-run2.log`
