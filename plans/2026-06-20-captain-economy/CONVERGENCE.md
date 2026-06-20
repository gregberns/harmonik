# Captain Economy — Convergence (2026-06-20)

5 investigators (I1–I5) + 2 adversarial reviewers (R1 fact-check, R2 triage/dedup). All bead-driving facts independently CONFIRMED against main (HEAD 3f60cf23). R2 killed 4 false-positive conflicts. Result: **12 deduplicated issues**, 6 reliability-breaking + 5 efficiency.

## Root cause of the operator's complaint
The 2026-06-17 leanfleet/tokenaudit initiative fixed **keeper bands + crew sleep + crew model-tiering**, but the **two levers aimed at the captain itself were never wired in**:
- The TA2 boot-digest scripts (cheap resume) **do not exist in git** — only out-of-git copies on one machine; bead hk-n3w1 was closed falsely.
- The admin-offload ops-monitor exists as a daemon job but the captain's `/loop 12m` tick **still runs all 8 checks on Opus** and never reads its output.

So the captain still boots heavy (~81k ingested + reasoning = the >100k operator sees), still does no-op Opus busy-work, and reads several mutually-contradictory instructions.

## The 12 issues (from reviews/R2-triage.md, verified by reviews/R1-factcheck.md)

| ID | Issue | Sev | Effort | Bead |
|----|-------|-----|--------|------|
| M1 | Keeper band: stale/inert `--warn-pct` flags in STARTUP+SKILL+keeper; canonical = `--warn-abs-tokens 200000 --act-abs-tokens 215000` | reliability | doc | CE1 |
| M2 | Review-gate check greps top-level `workflow_mode` (must be `.payload.workflow_mode`; `omitempty`/daemon-emit gap); add `reviewer_verdict`-per-`run_id` fallback | reliability | doc(+code) | CE1 doc / CE4 |
| M3 | 600s `run_stale,heartbeat` standing subscribe — SKILL §0.5 arms it, STARTUP forbids it | efficiency | doc | CE1 |
| M4 | restart-earlier band vs "re-derive everything via full STARTUP on resume" — defeats the lower band; lean resume on the boot-digest | reliability | doc+script | CE1 |
| M5 | Boot bloat: STARTUP⇄SKILL dup (~6k), full AGENT_INDEX/STATUS/TASKS re-read (~9k), full keeper skill auto-inject (~6k), digest framed optional w/ raw cmds still inline | efficiency | doc+script | CE1 |
| M6 | `/loop 12m` tick = mostly no-op Opus wakes; move deterministic slices to a Sonnet ops-monitor (`ops-q`); only judgment wakes Opus | efficiency | **code** | CE4 |
| M7 | `br close` exception split across SHUTDOWN/SKILL/beads-cli/captain-lanes with no single reconciling cross-ref | reliability | doc | CE1 |
| M8 | Hardcoded `--from captain` literals vs lane-identity guard (uncommissioned `--from captain` freezes the fleet) | reliability | doc | CE1 |
| M9 | SKILL §A lane snapshot duplicates+contradicts captain-lanes.md; SHUTDOWN writes §A but STARTUP reads captain-lanes.md → drift | efficiency | doc | CE1 |
| M10 | Captain unreachable by `comms --wake` (pane-name mismatch) yet treated as comms-reachable; re-arming `recv --follow` after /clear is load-bearing+unflagged | reliability | **code**(+doc note) | CE5 |
| M11 | Verify compiled `on_demand_warn_text` injects captain-safe restart-now, not the shared fatal `/quit` | reliability(cond) | doc(verify) | CE1 |
| M12 | Stream-vs-wave guidance vs operational "concurrent REAL beads wedge, go serial" memory; mostly crew-facing | efficiency | doc | CE1 |

Plus I3 deploy-drift (not a conflict, a deploy gap): `keeper-restart-verified.sh` is in git but not copied to `~/.claude/captain-tools/`, and `captain-launch.sh` doesn't route restart through it → **CE6** (deploy action, local).

## Dropped (false-positive, no action — R2)
- Conflict #2 lean-vs-fill: moot — `project_3day_scaleout_directives` (2026-06-19) lifted lean-park.
- #4, #5, #15: already reconciled by memory / read-only carve-out a booting captain loads.

## Bead plan
- **CE1** (this session, worktree, doc+scripts): the whole skill-lean + de-conflict + boot-digest pass — M1, M2(doc), M3, M4, M5, M7, M8, M9, M11, M12 + I3 doc-drift. Zero runtime risk (instructions for future boots). **DISPATCHED.**
- **CE4** (M6 + M2 code half): Sonnet ops-monitor absorbs the deterministic tick checks. Daemon code. **HELD** until system stabilized (captain offline, keeper-fix agent active) — avoids piling concurrent runtime changes on an unstable fleet.
- **CE5** (M10): comms `--wake` captain pane-name fix. Code. **HELD** (same reason).
- **CE6** (I3 deploy-drift): ship `keeper-restart-verified.sh` to captain-tools + route `captain-launch.sh`. Local deploy. **HELD** (touches live restart path).

Rationale for holding CE4/CE5/CE6: they alter live runtime behavior; the operator said the captain is offline until the system is stabilized and another agent is mid keeper-fix. CE1 is the operator's core ask (boot cost, conflicts, restart-doc) and carries no runtime risk. Redirect if you want the code items dispatched now too.
