# HANDOFF — yankee (crew; codex-substrate-unblock lane)

**CLAIM, not ground truth — `harmonik digest` + `/tmp/h-yankee` events.jsonl override.**
Autonomous lane (admiral authorized Claude tokens). Reports to **captain**. Not blocked on operator.

## STATUS (2026-07-19, after keeper-restart #4 + live gate #4): HOLDING — awaiting admiral/operator re-plan
Captain directive (comms 019f7cb6): **HOLD. Do NOT teardown the iso harness. Do NOT launch a fanout or any further fix.** The Ruling-A tripwire (what to do after gate #4 red) is admiral+operator's call; captain is surfacing the verdict up-chain. On restart: re-join comms as yankee, re-arm recv --follow, re-read this file + GATE4-VERDICT.md, and CONTINUE HOLDING. Do NOT re-run the fix cycle (both fixes are committed) and do NOT patch.

## What's DONE (both committed on branch phase1-session-restart-substrate)
- **Fix 1 = hk-qxvc2 @ 37651569** — route claude (SessionIDMinted) reviewer/gate onto the tmux substrate, not the codexdriver JSON-RPC driver. selectSubstrate 4th return -> Config.ReviewerSubstrate -> workLoopDeps.reviewerSubstrate -> branch at dot_cascade/dot_gate/reviewloop. Independent review APPROVE. **COMMITTED.**
- **Fix 2 = hk-daegv @ 49d7fde3** — codex-exec sandbox writable git-common-dir so codex's own commit lands. **COMMITTED** (prior session).

## Live gate #4 verdict (run 019f7cad, iso binary 37651569 = both fixes) — INDEPENDENTLY VERIFIED
- **Proof B (hk-daegv codex OWN commit) = GREEN.** commit_landed=true, real codex commit 46f8f0b (NOT daemon fallback). Fix 2 HOLDS. Clean reversal of prior seatbelt-denied RED.
- **Proof A (hk-qxvc2 review reaches agent_ready + close) = RED.** run_failed "node review agent_ready_timeout" (stall 208s). Bead yri-wq7 stays OPEN; queue paused-by-failure.
- **Diagnostic:** Fix 1 routing WORKED (tmux session created on worker at the reviewer_launched instant = reviewer routed onto tmux, not JSON-RPC) but was NECESSARY-not-SUFFICIENT — the review claude still never emits agent_ready on the remote worker via tmux. Residual wedge DOWNSTREAM of routing: maps to the 2nd half of Ruling-A fix#1 NOT in the committed patch (hook reverse-tunnel tcp:59963 on worker) and/or remote onboarding/trust or no-task-context.
- Recommendation surfaced to captain: captain-orchestrated FANOUT on "why the tmux-routed remote claude reviewer doesn't reach agent_ready" rather than a blind 4th patch.

## Iso harness — PRESERVED (per captain, do NOT teardown)
- box-A iso daemon **pid 64223** + supervisor **64871**; worker repo `/Users/gb/harmonik-yankee-iso/repo` intact.
- Fresh 37651569 binary staged to yankee-specific `/Users/gb/harmonik-yankee-iso/bin/harmonik` + `harmonik_path:` in iso workers.yaml — prod `/Users/gb/go/bin/harmonik` deliberately NOT overwritten.
- Prod untouched: repo HEAD 37651569; prod worker + assessor iso never touched.

## Key paths
- Verdict: `runs/codex-substrate-validation/GATE4-VERDICT.md`
- Iso events: `/tmp/h-yankee/proj/.harmonik/events/events.jsonl` (gate run = lines 55-104)
- Worker codex rollout: `~/.codex/sessions/2026/07/19/rollout-2026-07-19T16-19-56-019f7cad-…jsonl`
- Worker run branch (survives cleanup): `run/019f7cad-…` tip 46f8f0b in `/Users/gb/harmonik-yankee-iso/repo`
- Iso daemon log: `/tmp/h-yankee/daemon-run2.log`

## Handback bar (unchanged, for when re-plan clears a path)
Hand back to captain (fires assessor re-gate #3) ONLY when BOTH proofs GREEN in yankee's iso gate. B is GREEN; A remains RED. Work INLINE; daemon queue-dispatch broken (do NOT queue submit). Commit explicit paths only. Daemon owns terminal bead transitions.
