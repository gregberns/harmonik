# R1 — Adversarial Fact-Check of Captain-Economy Findings

Verified against repo HEAD `3f60cf23` on `main` (no worktrees). Each claim independently re-derived from live repo state, not from the reports.

---

## Claim 1 (I5) — boot-digest scripts absent from git; only out-of-git ~/.claude copies  →  **CONFIRMED**

- `git ls-files | grep -i boot-digest` → **empty** (no tracked file).
- `ls scripts/` → only `ops-monitor-check.sh`; no `captain-boot-digest.sh`, `crew-boot-digest.sh`, or `README-boot-digest.md`.
- `ls ~/.claude/captain-tools/` → `captain-boot-digest.sh` (Jun 17 16:05) and `crew-boot-digest.sh` (Jun 17 16:06) exist out-of-git on this box only.
- Skill refs point at the non-versioned path: `.claude/skills/captain/STARTUP.md:105,202` → `~/.claude/captain-tools/captain-boot-digest.sh`; `.claude/skills/crew-launch/SKILL.md:46` → `~/.claude/captain-tools/crew-boot-digest.sh`.

Safe to build a bead: land the three files in-repo under `scripts/` and re-point both skills.

---

## Claim 2 (I5) — self-hint is passive, no `restart-now --agent SELF`, no C1 unsafe-condition list  →  **CONFIRMED**

`internal/keeper/watcher.go:1096` (`keeperHintText`):
> `"[KEEPER HINT] Context is at ~190K tokens. Consider wrapping up the current task and preparing a handoff soon."`

No `restart-now` token, no SELF, no enumeration of unsafe-to-self-fire conditions (armed Monitor / pending sub-agent / unverified edit). The `restart-now --agent <agent>` references elsewhere in watcher.go (lines 270-277, 514, 935-938) are comments about the captain-initiated path, NOT part of the injected hint string. Plumbing real, payload watered down — exactly as I5 Q5.2 states.

---

## Claim 3 (I5) — ops-monitor script exists + auto-registers every@5m, but STARTUP tick never reads latest.json  →  **CONFIRMED**

- `scripts/ops-monitor-check.sh` tracked in git.
- `internal/daemon/opsmonitor_schedule.go:29,41` registers job id `"ops-monitor"`, `Argv: ["bash","scripts/ops-monitor-check.sh"]`, fired `every@5m` (file header line 14), on every daemon startup (`ensureOpsMonitorSchedule`). Test coverage in `scheduletick_test.go:429-451`.
- `git grep "latest.json\|ops-monitor" .claude/skills/captain/STARTUP.md` → **NONE**. The `/loop 12m` tick at STARTUP.md:398 still runs all 8 checks inline (incl. its own daemon-up, paused-queues, crew-freshness, quality-check). Zero reads of the precomputed digest.

Safe to build a bead: re-point the tick to read `.harmonik/ops-monitor/latest.json`.

---

## Claim 4 (I3) — keeper-restart-verified.sh not in ~/.claude tools; captain-launch.sh doesn't route restart through it  →  **CONFIRMED**

- `scripts/captain-tools/keeper-restart-verified.sh` exists in git, but `ls ~/.claude/captain-tools/` does NOT contain it (only `captain-boot-digest.sh`, `captain-launch.sh`, `crew-boot-digest.sh`, `crewlog.sh`).
- `git grep "keeper-restart-verified\|restart-verified" scripts/captain-tools/captain-launch.sh` → **no match**. The launcher arms the keeper with `--respawn-cmd` only; it never wires restart-now through the verified wrapper.

---

## Claim 5 (I3) — skills cite stale `--warn-pct/--act-pct` while launcher uses `--warn-abs-tokens 200000 --act-abs-tokens 215000`  →  **CONFIRMED**

- Launcher (`scripts/captain-tools/captain-launch.sh:55-56,117`): `CAP_WARN_ABS=200000`, `CAP_ACT_ABS=215000`, invokes `harmonik keeper … --warn-abs-tokens $CAP_WARN_ABS --act-abs-tokens $CAP_ACT_ABS --respawn-cmd …`. No pct flags. (cmd/harmonik mirror identical.)
- `STARTUP.md:335,338` → `--warn-pct 30 --act-pct 35` (incl. "ALWAYS pass `--warn-pct 30 --act-pct 35`").
- `SKILL.md:221` → `--warn-pct 30 --act-pct 35` ("matching captain-launch.sh defaults" — wrong).

Mismatch is real; pct flags are inert on the 1M window so cosmetic-but-misleading. Build the doc-fix bead.

---

## Claim 6 (I5) — 3-tier handoff wired into STARTUP Step 0a/0b (commit 7930bf28)  →  **CONFIRMED**

- `STARTUP.md:47` "## Step 0a/0b — Read tier-3 then tier-2 context"; `:57` `cat .harmonik/context/project.yaml`; `:65` `cat .harmonik/context/captain-lanes.md`; `:96-101` Step 2 verifies tier claims against ground truth; `:70-72` update discipline.
- `git log --oneline 7930bf28` → "leanfleet LF-A: 3-tier handoff context, noise cuts, model-tiering policy". This deliverable genuinely landed and is wired into boot.

---

## Claim 7 (I2/I4) — quality-check greps `workflow_mode`, a field that does not exist on `run_started`  →  **CONFIRMED** (with nuance)

Two-layer finding:
- The Go *type* `core.RunStartedPayload` (`internal/core/runstartedpayload.go`) DOES declare `WorkflowMode *WorkflowMode json:"workflow_mode,omitempty"`. So a naive struct read would say "field exists."
- BUT the daemon's actual emit path uses a SEPARATE struct `workloopRunStartedPayload` (`internal/daemon/workloop.go`, `emitRunStarted` ~line 4442) which has **no WorkflowMode field at all** — only run_id, bead_id, workspace_path, started_at, queue_id, queue_group_index, worker_name, worker_os.
- Live confirmation from `.harmonik/events/events.jsonl` (latest run_started): payload keys are exactly `bead_id, queue_group_index, queue_id, run_id, started_at, worker_name, worker_os, workspace_path`. **No `workflow_mode`.**
- Additionally the captain grep targets the top-level (`select(.workflow_mode == "single")`) but the field, if present, would nest under `.payload` — so the jq is doubly wrong (also flagged by I2:224).

Net: the practical I2/I4 claim — "the check always returns null / permanent false all-clear" — is CORRECT. Caveat for the bead author: do NOT phrase the fix as "the field doesn't exist on the type"; it exists on `core.RunStartedPayload` but is never populated by the daemon's `workloopRunStartedPayload` emit. The correct review-gate check is per-`run_completed.run_id` assert a matching `reviewer_verdict`, reading under `.payload`.

---

## Verdict roll-up

| # | Claim | Verdict |
|---|-------|---------|
| 1 | boot-digest scripts absent from git | CONFIRMED |
| 2 | self-hint passive, no SELF/C1 | CONFIRMED |
| 3 | ops-monitor every@5m but tick ignores latest.json | CONFIRMED |
| 4 | verified-restart wrapper not deployed / not wired | CONFIRMED |
| 5 | stale pct flags vs abs-tokens launcher | CONFIRMED |
| 6 | 3-tier handoff wired (7930bf28) | CONFIRMED |
| 7 | quality-check greps non-existent run_started field | CONFIRMED (emit-path nuance) |
