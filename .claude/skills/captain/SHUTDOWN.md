# Captain Shutdown Instructions (fleet handoff runbook)

> **Run this at every clean session end, before any extended break, or when the
> operator signals a consolidation.** This is the symmetric counterpart to
> [`STARTUP.md`](STARTUP.md) — boot builds the fleet, shutdown lands work cleanly
> and leaves the next captain a verified starting point.
>
> Codename glossary: **captain** = this session, the orchestrator. **crew** =
> a long-lived `claude --remote-control` session owning one epic + one named queue.
> **daemon** = the persistent Go dispatcher. **lane** = one initiative = one epic =
> one crew. **banked commit** = a reviewed worktree-authored commit on a
> `worktree-agent-*` / `bank/*` branch awaiting cherry-pick. **PIN** = an
> operator-gated item the captain cannot resolve alone; must be recorded durably.

---

## When to wind down vs. hand off vs. leave running

Choose one of three postures **before executing any step below**:

| Situation | Posture |
|---|---|
| Operator ending the session; all lanes drained or cleanly blocked | **Full shutdown**: stand down complete-lane crews, deploy banked commits, write HANDOFF.md, `comms leave`. |
| Operator leaving for a break; active lanes still in flight | **Hand off**: record state in HANDOFF.md + crew missions, do NOT stop healthy crews; daemon keeps draining. The next captain runs STARTUP.md. |
| Context approaching limit (~80–90%); fleet still healthy | **Captain-initiated restart (ON-059)**: do NOT stop crews manually AND do NOT self-`/quit`. At a clean idle point (no in-flight dispatch/merge/crew-spawn): write `HANDOFF-captain.md` with the KEEPER nonce, run `harmonik keeper restart-now --agent captain`, keep the turn OPEN, stop typing. The keeper fires the cycle on its next tick. Skip to Step 5 (state capture). On resume: re-drain comms + re-ground via STARTUP.md Steps 2–6 — do NOT snapshot live queue/daemon state in the handoff body (STARTUP.md re-derives it). |

> **The daemon almost always keeps running.** The daemon process is supervisor-managed
> and independent of your session. Only crews need explicit stand-down for complete
> lanes. A crew on a live/in-flight lane MUST NOT be stopped — that is a
> surface-and-await judgment (captain SKILL.md §8).

---

## Step 1 — Drain pending messages and confirm live state

Before touching anything, catch up:

```bash
# Drain any unread messages (bounded — quit after backlog)
harmonik comms recv --follow --json | head -60

# Re-verify fleet state (same as STARTUP.md Step 2, abbreviated)
harmonik comms who --json
harmonik crew list --json
harmonik queue status --json
git -C $HARMONIK_PROJECT log --oneline -3   # confirm main is current
```

Note any crew messages received that require action (bead banked, lane complete,
error) before proceeding. Attribute run events via `br show <epic_id> --format json`
→ `.assignee` (never guess).

---

## Step 2 — Deploy banked commits (before standing down any crew)

Any commit on a `worktree-agent-*` or `bank/*` branch that passed review but was
not yet cherry-picked to `main` must be deployed **before** the session ends. A
stood-down crew cannot re-bank or re-review; deploy now.

**Deploy only in a TRUE lull (0 reviewers active, 0 in-flight merges).**

```bash
# List banked branches
git -C $HARMONIK_PROJECT branch --list 'bank/*' 'worktree-agent-*'

# Confirm lull: no active reviewer panes
harmonik subscribe --types heartbeat --heartbeat 1s --json | head -1
harmonik queue status --json    # check "active_runs" count
```

### Temp-worktree cherry-pick SOP (bypass-SOP, used when the daemon is live):

```bash
# 1. Fetch and create a detached deploy worktree off origin/main
git -C $HARMONIK_PROJECT fetch origin main
git worktree add --detach /tmp/cap-deploy origin/main

# 2. Cherry-pick reviewed SHAs in order (oldest first)
git -C /tmp/cap-deploy cherry-pick <sha1> <sha2> ...

# 3. Merged-tree gate
go build ./... && go vet ./internal/daemon/... && go vet ./internal/queue/...

# 4. Announce before push (crews need to know main is advancing)
harmonik comms send --from captain --broadcast --topic announce -- \
  "DEPLOY: cherry-picking <shas> to main — brief push window"

# 5. Push and ff-update local main (CRITICAL — skipping wedges the daemon)
git -C /tmp/cap-deploy push origin HEAD:main
git -C $HARMONIK_PROJECT merge --ff-only origin/main

# 6. Verify no divergence
git -C $HARMONIK_PROJECT rev-parse main HEAD origin/main
#    All three must agree.

# 7. Clean up
git worktree remove --force /tmp/cap-deploy

# 8. Close manually deployed beads
br close <bead_id> --reason "Manually deployed: <sha> on main (bypass-SOP)"
```

> **NEVER redeploy the daemon mid-run** (while a bead is merging or a reviewer is
> active). It strands the in-flight bead. Deploy binaries only in a true lull;
> restart the daemon (supervisor auto-revives) and wait for the supervisor restart
> backoff (~30s–1m) before considering it down.

---

## Step 3 — Stand down complete-lane crews

Only stand down crews whose lane is **fully complete** — every dispatchable bead
closed, no banked commits outstanding, no open operator-decision blocking the lane.

```bash
# For each complete-lane crew:

# a) Announce the stand-down so peers don't send it work mid-stop
harmonik comms send --from captain --to <crew> --topic status -- \
  "Lane complete — standing you down cleanly. Mission file persists for respawn."

# b) Stop the crew (removes registry record + pane; mission file is preserved)
harmonik crew stop <crew>

# c) Confirm it left the bus
harmonik comms who --json    # <crew> should be absent
harmonik crew list --json    # no registry record for <crew>
```

> **Do NOT stand down a crew whose lane is blocked (OAuth, operator decision, etc.).**
> A blocked-but-live crew means the lane holds state and can self-resume the moment
> the block clears. Stand down only lanes that are DONE. Blocked lanes get a PIN
> (Step 4) and remain in the handoff.

> **Mission files persist across `crew stop`.** `.harmonik/crew/missions/<crew>.md`
> is NOT deleted by `crew stop`. The next captain can respawn the crew with the same
> mission file via `harmonik crew start <crew> --queue <crew>-q --mission ...`.

---

## Step 4 — Record PINs for operator-gated work

A **PIN** is an item the captain cannot resolve alone — it needs an operator action
(OAuth scope grant, next-phase initiative ranking, a risky op like session-keeper
arming) before work can resume.

### What to record

For each PIN, capture ALL of the following (in HANDOFF.md §Open/next — see Step 5):

```
⚠️ OPERATOR ACTION: <plain-English description of what the operator must do>
  Blocks:         <bead IDs or lane names that unblock on this action>
  Unblock steps:  <exact commands the captain runs the moment the operator acts>
  Context:        <why this can't be done autonomously — 1 sentence>
```

### Examples from a real session (2026-06-10 fleet consolidation)

**OAuth workflow scope:**
```
⚠️ OPERATOR ACTION: run `gh auth refresh -s workflow`
  Blocks: chani release.yml (hk-jdesv, hk-o4j13), liet ci.yml (hk-jzepv), stilgar hk-4mten
  Unblock steps: after re-auth, redeploy daemon (go install + supervisor restart) so
                 the running process picks up the new credential; then re-queue blocked beads.
  Context: the daemon's git credential is cached at startup; a scope grant does NOT
           take effect in the running process — daemon restart is required.
```

**Next-phase initiative ranking:**
```
⚠️ OPERATOR ACTION: rank the next phase (standard-bead-dot vs. flywheel smoke vs. pilot)
  Blocks: all lanes after chani/liet complete their current epics
  Unblock steps: captain receives operator decision → re-task crews to new epics
  Context: standard-bead-dot is the top KNOWN candidate per kerf next, but it is a
           NEW initiative not yet in the known feed — cannot rank autonomously (§8).
```

**Session-keeper arming:**
```
⚠️ OPERATOR ACTION: wire session-keeper hooks + decide full-cycle vs. warn-only
  Blocks: context-flood toil (crew reseeds require manual captain intervention)
  Unblock steps: `harmonik keeper enable <crew> --project ... --scripts-dir ... --tmux <handle>`
                 then `harmonik keeper doctor <crew>` (must pass all checks).
  Context: `.managed` markers already exist for all crews — Phase-2 (destructive /clear
           cycle) ACTIVATES the moment hooks are wired; don't arm full-cycle unsupervised
           on a live crew. The captain cannot wire hooks in its own session without risk.
```

### Session-keeper safety note (LOAD-BEARING)

Do NOT arm session-keeper full-cycle (`--act-pct 90`) on a live crew without operator
awareness. The `.managed` marker makes the watcher immediately Phase-2-live (it will
`/clear` the crew mid-dispatch if the threshold is crossed). Warn-only is safe
(rename `<crew>.managed` → `<crew>.managed.disabled` before starting the watcher).
File a PIN; the operator decides whether to proceed.

---

## Step 5 — State capture (before writing HANDOFF.md)

Update durable artifacts so the next captain starts clean:

### 5a — Update SKILL.md §A lane roadmap

The "Current lanes" table in `.claude/skills/captain/SKILL.md` §A is a live snapshot.
Update it to match the actual state at shutdown:

- Stood-down crews → strikethrough row + `STOOD DOWN — <reason>`
- Remaining crews → current epic, status, blocker if any
- "Prioritized NEXT work" → update with any new beads filed or priorities shifted

> This is the one doc agents read without running ground-truth — it must reflect
> reality, not the state from the prior session.

### 5b — Refresh crew mission files

For each crew that is NOT being stood down, refresh its mission file with current
state so a keeper restart re-hydrates correctly:

```
.harmonik/crew/missions/<crew>.md   (gitignored; write via Write tool)
```

The YAML frontmatter is the machine contract (`schema_version, crew_name, queue,
epic_id, goal, captain_name`). The free-text body is the crew's working context —
update it with current ordered beads, any caveats, and the next action.

### 5c — Record banked branches

```bash
git -C $HARMONIK_PROJECT branch --list 'bank/*' 'worktree-agent-*'
```

Any remaining banked branches (not yet deployed) must be listed in HANDOFF.md with
their SHA and the review verdict so the next captain can deploy them.

### 5d — Clear staged debris (if safe)

If `git -C $HARMONIK_PROJECT status` shows staged changes in the main working
tree (e.g., leftover from a same-package bead merge), verify provenance before
clearing:

```bash
git -C $HARMONIK_PROJECT diff --cached --stat
# Confirm: are these removals of a specific bead's code, or wanted changes?
```

If confirmed debris (leftover from a merge conflict resolution, not wanted):

```bash
# Surgical restore — do NOT use git reset --hard (preserves untracked files like .beads/)
git -C $HARMONIK_PROJECT restore --staged internal/queue/cancel.go  # example
git -C $HARMONIK_PROJECT checkout -- internal/queue/cancel.go
```

---

## Step 6 — Write HANDOFF.md

Write HANDOFF.md using the **captain handoff format** (tiered model):

```markdown
<!-- PP-TRIAL:v2 <date> <branch> — CAPTAIN handoff. <1-line fleet status>.
     Load $HARMONIK_PROJECT/.claude/skills/captain/STARTUP.md FIRST
     (project-local path — NOT ~/.claude/skills/), then this. HANDOFF.md is gitignored. -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT -->
<verbatim from prior handoff's DIRECTIVES block — these are durable>
<!-- END DIRECTIVES -->

# LATEST DELTAS (<timestamp>)
<bullet per major event since the prior handoff>

# STATE (<timestamp>)
Daemon UP/DOWN, --workflow-mode <mode>, -c<N>, supervisor-managed (pid <N>).
main == origin/main in sync (or: main is at <sha>, origin at <sha> — divergence noted).
<N> crews live.

## Lanes (one line per crew)
- **<crew>**: <status 1 line> → <next action>

# Open / next
0. ⚠️ OPERATOR ACTIONs (list PINs verbatim from Step 4)
1. <next-captain priorities in order>

# Deploy procedure
<only include if it changed this session; otherwise reference STARTUP.md>

# Translations
<every bead ID, codename, and jargon term in the body, one line each>
```

**Format discipline:**
- Body ≤ 50 lines (fleet state belongs in crew mission files, not here).
- Per-crew state = one line each in the Lanes section.
- Translations: every `hk-xxxxx`, codename, and abbreviation that appears in the body.
- Never embed live claims about daemon state that STARTUP.md will re-measure anyway;
  note the state at write-time but flag it as stale input (STARTUP.md Step 2 wins).

---

## Step 7 — Leave the bus and final checks

```bash
# Signal departure to any agents still online
harmonik comms send --from captain --broadcast --topic status -- \
  "Captain session ending. Fleet state in HANDOFF.md. Daemon up; crews <list> live."

# Leave the bus
harmonik comms leave
```

---

## Fleet-safe-to-leave glance check

Run this final check before the session exits. All six must hold:

1. **Daemon up:** `harmonik queue status` exits 0 (not 17). If the daemon is down,
   note it in HANDOFF.md — do NOT attempt to restart it yourself without surfacing
   (supervisor should revive it; see STARTUP.md §2.1).

2. **No stranded in-flight beads:** `harmonik queue status --json` shows no
   `active_runs` that will be orphaned. If a bead is mid-merge or a reviewer is
   active, wait for completion before exiting (or note it explicitly as a risk in
   HANDOFF.md).

3. **PINs recorded:** every operator-gated item has a PIN entry in HANDOFF.md §Open
   with exact unblock steps. No implicit "the operator will know what to do."

4. **Banked commits deployed or recorded:** `git branch --list 'bank/*' 'worktree-agent-*'`
   is either empty (all deployed) or each remaining branch is listed in HANDOFF.md
   with its SHA and review verdict.

5. **Crews healthy or cleanly stood down:** `harmonik crew list --json` shows only
   records for crews that are (a) online in `comms who` (healthy) or (b) were
   cleanly stood down this step (not in `crew list` and not in `comms who`). No
   zombie/ghost records.

6. **SKILL.md §A is current:** the lane table matches actual state (stood-down lanes
   struck through; blocked lanes show their blocker; next-phase candidates listed).

```bash
# Quick one-liner for check #5 (registered-but-offline = zombie remaining)
comm -23 \
  <(harmonik crew list --json | jq -r '.name' | sort) \
  <(harmonik comms who --json | jq -r '.agent' | sort)
# Any name printed = unresolved zombie/ghost — reconcile before exiting
```

---

## Load-bearing gotchas (surfaced from the 2026-06-10 session)

- **`gh auth refresh` does NOT take effect in the running daemon.** The daemon caches
  its git credential at startup. After any OAuth scope re-auth, the daemon MUST be
  restarted (supervisor auto-revives; wait for socket-bind ~30–60s before assuming
  it is down). Verify the new scope is live by re-running the blocked bead as a
  canary.

- **Never redeploy the daemon while a bead is in-flight.** A mid-merge or mid-review
  daemon restart strands the run (the daemon loses the pane handle; bead fails at
  `run_stale`). Deploy only in a true lull (0 `active_runs` in `queue status --json`).

- **The ff-after-push step is load-bearing for captain cherry-pick deploys.** After
  pushing banked commits out-of-band, run `git -C <repo> merge --ff-only origin/main`
  to advance the daemon's local `refs/heads/main`. Skipping this leaves the daemon's
  local main behind origin → every subsequent daemon merge push is rejected as non-ff
  → daemon wedge (hk-svieq, `b4858a3c` now auto-recovers, but the ff step is cheaper).

- **Don't arm session-keeper full-cycle unsupervised.** `.managed` markers make
  the watcher immediately Phase-2-live. The operator must confirm full-cycle vs.
  warn-only before hooks are wired (see Step 4, PIN example).

- **The captain MUST NOT self-`/quit` on a keeper context-warning.** The captain's
  keeper injects a specific warn: *"[KEEPER WARNING — automated] Proactive context checkpoint — you have ample buffer remaining. Keep working. At a clean checkpoint only: write HANDOFF-captain.md (include the KEEPER nonce), then run: harmonik keeper restart-now --agent captain. Do NOT /quit or stop."* Follow that procedure exactly (see
  STARTUP.md "On-WARN procedure"). A captain that obeys `/quit` exits permanently —
  there is no supervised respawn wrapper. Launch via
  `~/.claude/captain-tools/captain-launch.sh` so the session has a stable
  `--session-id` (the keeper rebinds to this). A bare `claude --remote-control
  captain` with no `--session-id` cannot be cycled and is the historical
  dead-captain bug. The **keeper band is UNCHANGED** — `restart-now` bypasses only
  the act-pct idle gate; warn/act thresholds are not widened.

- **`gh auth` workflow scope requires the `workflow` scope specifically** — it is NOT
  included in the default `repo` scope. Beads touching `.github/workflows/` will
  push-reject silently without it regardless of other scopes granted.

- **Staged debris in the main working tree does not block daemon merges.** The daemon
  merges from worktrees (not the main tree's index), so staged-but-uncommitted changes
  in the main tree are latent-harmless for the daemon. However, they can conflict with
  captain cherry-pick deploys that touch the same files — clear them surgically with
  `git restore --staged` before deploying any bead in the same package.

---

## References

- `.claude/skills/captain/STARTUP.md` — the boot counterpart to this file.
- `.claude/skills/captain/SKILL.md` — §0 autonomy bright-line; §8 surface-and-await;
  §A lane roadmap (update in Step 5a).
- `docs/retro/2026-06-10/A5-formalize-process.md` — tiered handoff model that
  motivated this doc (§3.3 captain handoff rules, tiered-handoff table).
- `specs/crew-handoff-schema.md` — six-field mission handoff contract.
- `.claude/skills/agent-comms/SKILL.md` — comms CLI surface + N3 dedupe requirement.
- `.claude/skills/beads-cli/SKILL.md` — write discipline (captain MUST NOT issue
  terminal transitions; `br close` on manually-deployed beads is the one exception,
  sanctioned by the bypass-SOP).
