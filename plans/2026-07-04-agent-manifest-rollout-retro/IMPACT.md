# Agent-manifest rollout — IMPACT tracking

## 2026-07-04 ~16:25Z — Pass #1 headline: ROLLOUT IS CAPTAIN-PATH ONLY (crews NOT wired)
- Baseline observation of crew `stilgar` (first "fresh" crew post-deploy) + code check proves crews
  boot the LEGACY mission-file path. `cmd/harmonik/crewstart.go` has NO manifest wiring; only
  `cmd/harmonik/captain.go` (T10) seeds `harmonik agent brief`.
- So agent-manifest content exists + the daemon carries it, but it only reaches CAPTAINS. Crews
  (stilgar/jessica) never load their `soul.md`/`operating.md`. The captain's "manifest boot
  validated" was a false positive.
- No agent is CURRENTLY exercising the manifest: crews can't, and the running captain session
  predates the deploy (still old boot).
- GATE: real crew rollout + crew observation are BLOCKED on hk-bl93n FIX #3 (wire `start crew` ->
  `agent brief`). Elevated to jessica's first fix.
- Presence gap (FIX #1) independently confirmed in stilgar: single boot `comms join`, no <120s
  refresh, went stale — matches the audit.
