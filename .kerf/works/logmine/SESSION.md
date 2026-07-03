# logmine — Session state (shelved 2026-06-11, liet)

**Status:** `tasks` — design COMPLETE through the tasks pass + reviewed. Only the operator-gated install remains. Advance to `ready`/`finalize` after the 3 sign-off flags are answered and T1 is installed.

## What's done (full recurring-pipeline design)
- **Iteration 2 harvest:** `04-research/findings-iter2.md` — F23–F36, 12 priors FIXED-confirmed, high-water cursor footer set (`019eb861-…`). Beads hk-3dz/z16/4je/nun/yru + 5 enrichment comments. **hk-4je (F29 CHB-023) LANDED 2884b5aa.**
- **Method (frozen):** `pipeline.md` — 6-slice harvest + the high-water-footer cursor requirement (T2 applied).
- **Trigger (resolved + reviewed):** `05-specs/recurring-pipeline-spec.md` · `06-integration.md` (ready-to-install script/plist) · `SPEC.md`. DAILY · SUBSCRIPTION · crew-style; `harmonik crew start liet --queue liet-q --mission …` (`--remote-control`, billing code-verified). Review fixed 2 P1 (`--queue` required; `status==online` guard) + 2 P2 (flock; 24h first-run fallback).
- **Tasks:** `07-tasks.md` — T0 sign-off gate · T1 install (gated) · T2 footer (DONE) · T3 native-scheduler follow-up (**filed hk-0es**) · T4 missed-run meta-monitoring (future hitl tie-in).

## Next steps (gated)
1. **Operator answers 3 sign-off flags:** (a) fresh-spawn vs persistent crew; (b) daily clock time (placeholder 09:30 local); (c) install ownership (operator runs the 3 install cmds vs liet-q bead).
2. On sign-off → install T1 (script + launchd plist, `06-integration.md`) → the pipeline self-runs daily. → `kerf square`/`finalize`.
3. **T3 (hk-0es)** native scheduler — daemon-infra lane, route via captain; supersedes the OS scheduler later.
4. Install is operator/ops lane — this work edits neither `~/Library/LaunchAgents` nor the repo tree.

## Context
- Epic hk-mhmaw (`codename:logmine`). Iter-2 + integration + tasks recorded as epic-journal comments.
- Once installed, the daily trigger re-runs the whole harvest→document→file→confirm loop autonomously, subscription-billed.
- PARKED externally-gated: hk-3dz (F25 gh workflow-scope) + hk-4mten (F16 CI) — blocked on the OAuth workflow-scope credential.
