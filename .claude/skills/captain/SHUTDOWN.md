---
name: captain-shutdown
description: >
  The captain's session-end runbook — the counterpart to the boot runbook. Lands
  in-flight work, updates the tier files (captain-lanes.md and the direction log),
  writes HANDOFF-captain.md, and leaves the next captain a verified state rather than a
  claim. Also carries the verified-manual-cherry-pick bypass, the one case where the
  captain may close a bead itself.
---

<!-- SOURCE OF TRUTH: cmd/harmonik/assets/skills/captain/SHUTDOWN.md (Go //go:embed).
     The copy at .claude/skills/captain/SHUTDOWN.md is GENERATED OUTPUT — `harmonik sync-assets`
     overwrites it from the embed and there is NO reverse sync, so an edit made
     only there silently drifts and is eventually reverted. To change this skill:
     edit the cmd/harmonik/assets/ copy, then mirror it byte-for-byte into
     .claude/skills/ in the SAME commit. The two paths must stay byte-identical. -->

# Captain Shutdown (fleet handoff runbook)

Run this at a clean session end, before an extended break, or when the operator
signals a consolidation. It is the counterpart to [`STARTUP.md`](STARTUP.md):
boot builds the fleet, shutdown lands work and leaves the next captain a verified
starting point.

---

## Pick a posture first

| Situation | Posture |
|---|---|
| Operator ending the session; all lanes drained or cleanly blocked | **Full shutdown**: stand down complete-lane crews, deploy banked commits, write HANDOFF.md, `comms leave`. |
| Operator leaving for a break; active lanes still in flight | **Hand off**: record state in HANDOFF.md + crew missions, leave healthy crews running; the daemon keeps draining. The next captain runs STARTUP.md. |
| Context filling; fleet still healthy | **Captain-initiated restart**: do not stop crews and do not exit your own session. At a clean idle point write `HANDOFF-captain.md` with the KEEPER nonce, run `harmonik keeper restart-now --agent captain`, keep the turn open, and stop typing. The keeper drives the cycle. Skip to Step 5. |

The daemon is supervisor-managed and independent of your session — it keeps
running. Only crews need an explicit stand-down.

Do not stop a crew that is actively working: stopping it throws away the in-flight
turn. "Actively working" is a pane finding, not a lane-status one — capture the
pane and look for an advancing spinner or an empty `❯ ` input box. A lane can read
live on paper while its crew is dead or wedged; that is a zombie, and reconciling
it (`harmonik crew stop <name>`, then re-establish the lane) is routine
(STARTUP.md Step 3).

---

## Step 1 — Drain messages and confirm live state

```bash
harmonik comms recv --follow --json | head -60

harmonik comms who --json
harmonik crew list --json
harmonik queue status --json
git -C $HARMONIK_PROJECT log --oneline -3
```

Act on any crew message that needs it (bead banked, lane complete, error) before
proceeding. Attribute run events via `br show <epic_id> --format json` → `.assignee`.

---

## Step 2 — Deploy banked commits (before standing down any crew)

Any commit on a `worktree-agent-*` or `bank/*` branch that passed review but has
not reached the integration branch must be deployed before the session ends. A
stood-down crew cannot re-bank or re-review.

Deploy to the integration branch, never to `main`. Read the branch name from
`.harmonik/branching.yaml`, key `defaults.lands_on`. A human moves it into `main`
later with `harmonik promote --pr --from "$TARGET" --target main`.

Deploy in a lull (no active reviewers, no in-flight merges) so nothing in flight
is stranded.

```bash
git -C $HARMONIK_PROJECT branch --list 'bank/*' 'worktree-agent-*'
harmonik queue status --json    # check "active_runs"
```

### Temp-worktree cherry-pick

```bash
# 0. Resolve the integration branch — never hard-code it.
TARGET=$(awk -F'lands_on:' '/^ *lands_on:/{sub(/#.*/,"",$2); gsub(/["'"'"'[:space:]]/,"",$2); print $2; exit}' \
  "$HARMONIK_PROJECT/.harmonik/branching.yaml" 2>/dev/null)
test -n "$TARGET" || { echo "no lands_on in branching.yaml — STOP, ask the operator"; exit 1; }
test "$TARGET" != "main" || { echo "lands_on is main — STOP, ask the operator"; exit 1; }

# 1. Detached deploy worktree off the integration branch
git -C $HARMONIK_PROJECT fetch origin "$TARGET"
git worktree add --detach /tmp/cap-deploy "origin/$TARGET"

# 2. Cherry-pick reviewed SHAs, oldest first
git -C /tmp/cap-deploy cherry-pick <sha1> <sha2> ...

# 3. Merged-tree gate
go build ./... && go vet ./internal/daemon/... && go vet ./internal/queue/...

# 4. Announce before push — crews need to know the branch is advancing.
#    --from is your verified lane identity, not a hardcoded "captain".
harmonik comms send --from "$HARMONIK_AGENT" --broadcast --topic announce -- \
  "DEPLOY: cherry-picking <shas> to $TARGET — brief push window"

# 5. Push, then fast-forward the local integration branch. Skipping the ff leaves
#    the daemon's local branch behind origin and its next merge push is rejected.
git -C /tmp/cap-deploy push origin "HEAD:$TARGET"
git -C $HARMONIK_PROJECT merge --ff-only "origin/$TARGET"

# 6. Verify no divergence — all three must agree
git -C $HARMONIK_PROJECT rev-parse "$TARGET" HEAD "origin/$TARGET"

# 7. Clean up
git worktree remove --force /tmp/cap-deploy

# 8. Close manually deployed beads
br close <bead_id> --reason "Manually deployed: <sha> on $TARGET (bypass-SOP)"
```

---

## Step 3 — Stand down complete-lane crews

Stand down only crews whose lane is fully complete: every dispatchable bead
closed, no banked commits outstanding, no open operator decision blocking the
lane. A blocked-but-live crew holds state and can self-resume when the block
clears — leave it up, give it a PIN (Step 4), and keep it in the handoff.

```bash
# a) Announce so peers don't send work mid-stop
harmonik comms send --from "$HARMONIK_AGENT" --to <crew> --topic status -- \
  "Lane complete — standing you down cleanly. Mission file persists for respawn."

# b) Stop the crew (removes registry record + pane)
harmonik crew stop <crew>

# c) Confirm it left the bus
harmonik comms who --json    # <crew> should be absent
harmonik crew list --json    # no registry record for <crew>
```

`crew stop` does not delete `.harmonik/crew/missions/<crew>.md`. The next captain
respawns the crew with the same mission file via
`harmonik crew start <crew> --queue <crew>-q --mission ...`.

---

## Step 4 — Record PINs for operator-gated work

A PIN is an item the captain cannot resolve alone — it needs an operator action
before work resumes. Record each one in HANDOFF.md §Open/next with enough for the
next captain to act the moment the operator does: what the operator must do, what
it blocks, the exact unblock commands, and why it can't be done autonomously.

```
⚠️ OPERATOR ACTION: run `gh auth refresh -s workflow`
  Blocks:         <bead IDs or lane names>
  Unblock steps:  redeploy the daemon so the running process picks up the new
                  credential, then re-queue the blocked beads
  Context:        the daemon caches its git credential at startup, so a scope
                  grant does not reach the running process
```

Session-keeper arming is operator-gated: a `.managed` marker makes the watcher
immediately live, so it can `/clear` a crew mid-dispatch. Do not arm full-cycle on
a live crew without the operator; warn-only is safe (rename `<crew>.managed` →
`<crew>.managed.disabled` before starting the watcher).

---

## Step 5 — State capture

### 5a — Update `.harmonik/context/captain-lanes.md`

STARTUP.md Step 0b reads this file at boot without running ground-truth, so it
must reflect reality at shutdown, not the prior session's state. Do not write a
point-in-time lane table back into SKILL.md §A — that holds the durable lane model
only.

- `active_lanes` → stood-down crews removed; remaining crews get current epic,
  queue, model, status, blocker.
- `parked` / `operator_initiatives` / `pipeline` → any new beads filed or
  priorities shifted.

### 5b — Refresh crew mission files

For each crew still running, refresh `.harmonik/crew/missions/<crew>.md` so a
keeper restart re-hydrates correctly. The YAML frontmatter is the machine
contract; the body is the crew's working context — current ordered beads,
caveats, next action.

### 5c — Record banked branches

```bash
git -C $HARMONIK_PROJECT branch --list 'bank/*' 'worktree-agent-*'
```

List any branch not yet deployed in HANDOFF.md with its SHA and review verdict.

### 5d — Clear staged debris

If `git -C $HARMONIK_PROJECT status` shows staged changes in the main working
tree, check provenance before clearing:

```bash
git -C $HARMONIK_PROJECT diff --cached --stat
```

If it is debris, restore surgically. Do not use `git reset --hard` — it destroys
untracked files such as `.beads/`.

```bash
git -C $HARMONIK_PROJECT restore --staged internal/queue/cancel.go  # example
git -C $HARMONIK_PROJECT checkout -- internal/queue/cancel.go
```

---

## Step 6 — Write HANDOFF.md

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
integration branch in sync with origin (or note the divergence).
<N> crews live.

## Lanes (one line per crew)
- **<crew>**: <status> → <next action>

# Open / next
0. ⚠️ OPERATOR ACTIONs (PINs from Step 4)
1. <next-captain priorities in order>

# Translations
<every bead ID, codename, and jargon term in the body, one line each>
```

Keep it short — detailed fleet state belongs in the crew mission files. Anything
you write about live daemon or queue state is a stale input; STARTUP.md Step 2
re-measures it and wins.

---

## Step 7 — Leave the bus

```bash
harmonik comms send --from "$HARMONIK_AGENT" --broadcast --topic status -- \
  "Captain session ending. Fleet state in HANDOFF.md. Daemon up; crews <list> live."

harmonik comms leave
```

---

## Safe-to-leave glance check

1. **Daemon up:** `harmonik queue status` exits 0 (not 17). If it is down, check
   whether the supervisor is already reviving it — restart backoff can delay
   socket-bind for a minute or so — and let the supervisor win if it is. If the
   supervisor is dead, restart the daemon yourself (STARTUP.md §2.1). Note the
   state in HANDOFF.md either way.

2. **No stranded in-flight beads:** `harmonik queue status --json` shows no
   `active_runs` that will be orphaned. Wait out a mid-merge bead or an active
   reviewer, or record it as a risk in HANDOFF.md.

3. **No queue stopped:** `harmonik queue list --json` shows no queue whose
   `status` is `paused-by-failure`. Nothing else will tell you. Check 2 misses it
   because a stopped queue has no `active_runs`, so it passes green while
   dispatching nothing. Ops-monitor misses it because its `paused-queues` check
   fires only when the owning crew is online. And the queue itself emits nothing
   after the pause, so it crosses the session boundary in silence. That is how a
   lane sits dead for a day. Sweep it yourself before you leave:

```bash
harmonik queue list --json | jq -r '.queues[] | select(.status == "paused-by-failure") | .name'
# any name printed → harmonik queue recover --queue <name>
```

   Recovery refuses if a failed bead was closed in the meantime — the
   **harmonik-dispatch** skill, § Restart a queue that stopped. If you choose to
   leave a queue parked, name it in HANDOFF.md with the reason and the recover
   command.

4. **PINs recorded** in HANDOFF.md §Open, with exact unblock steps.

5. **Banked commits** either deployed or listed in HANDOFF.md with SHA and verdict.

6. **No zombie crew records** — every registry record has a matching live agent:

```bash
comm -23 \
  <(harmonik crew list --json | jq -r '.name' | sort) \
  <(harmonik comms who --json | jq -r '.agent' | sort)
# Any name printed = unresolved zombie — reconcile before exiting
```

7. **`.harmonik/context/captain-lanes.md` is current** — it is what STARTUP.md
   Step 0b reads.

---

## References

- `.claude/skills/captain/STARTUP.md` — the boot counterpart, and the owner of the
  on-WARN procedure and the crew process-liveness sweep. The keeper's bands and config
  live in the `keeper` skill.
- `.claude/skills/captain/SKILL.md` — autonomy bright-line, surface-and-await, the
  lane model.
- `specs/crew-handoff-schema.md` — mission handoff contract.
