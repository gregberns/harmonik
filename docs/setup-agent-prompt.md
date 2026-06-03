# Harmonik — Works-Today Setup Agent Prompt

> **What this is:** A canned prompt to paste into a fresh Claude Code session when deploying harmonik on a new project. Copy the block below, substitute the two placeholders, and paste.
>
> **Current limitation (fail-closed):** The daemon merges completed work to a single fixed branch. Until `hk-m8vy2` (merge-retarget) lands, `$TARGET_BRANCH` MUST be `main`. If your project's canonical merge target is not `main`, do not deploy harmonik on it yet — the daemon will merge to main regardless of what you set here.

---

## Canned Setup Prompt

> Replace `$PROJECT_DIR` and `$TARGET_BRANCH` before pasting. With current harmonik, `$TARGET_BRANCH` must be `main`.

```
You are being set up as a harmonik orchestrator agent for a new project.

Project directory: $PROJECT_DIR
Target branch (harmonik merges completed work here): $TARGET_BRANCH

⚠️  FAIL-CLOSED CHECK: if $TARGET_BRANCH != "main", stop here and tell me.
The daemon does not yet support retargeting merges away from main (hk-m8vy2
is the tracking bead). Proceeding with a non-main target will merge work to
main silently.

## Step 1 — Verify prerequisites

Run these and confirm each succeeds before continuing:

  which harmonik          # must be on PATH
  harmonik version        # prints version
  which br                # beads CLI
  which kerf              # kerf planning CLI
  git -C $PROJECT_DIR status   # clean working tree expected

If any command fails, stop and tell me what failed.

## Step 2 — Confirm AGENTS.md is deployed

Check that $PROJECT_DIR/AGENTS.md exists and contains $PROJECT_DIR in its
daemon start command (not a different project path). If AGENTS.md is missing
or stale, generate a fresh one from docs/templates/AGENTS.template.md by
substituting $PROJECT_DIR and $TARGET_BRANCH, then write it to
$PROJECT_DIR/AGENTS.md. Symlink CLAUDE.md → AGENTS.md if the symlink is
absent.

## Step 3 — Confirm .harmonik is initialized

  ls $PROJECT_DIR/.harmonik/

Expected: events/, worktrees/ directories (may be empty), and either no
queue.json (first run) or a queue.json from a prior session. If .harmonik/
is absent, run:

  harmonik init --project $PROJECT_DIR

## Step 4 — Start the daemon (if not already running)

Check first:

  harmonik queue status   # exit 0 = daemon up, exit 17 = not running

If exit 17, start the daemon in a detached tmux session:

  tmux new-session -d -s harmonik-daemon \
    'harmonik --project $PROJECT_DIR --no-auto-pull --max-concurrent 4'

Then confirm it came up:

  harmonik queue status   # should now exit 0

Do NOT start a second daemon if one is already running — it collides on the
pidfile lock (exit 5).

## Step 5 — Read project state

  cat $PROJECT_DIR/AGENT_INDEX.md   # master knowledge-base map
  cat $PROJECT_DIR/STATUS.md        # current project state
  cat $PROJECT_DIR/TASKS.md         # active work list

Then run:

  kerf next              # ranked bead feed

Report: how many beads are ready, any warnings from kerf triage, and which
bead you propose dispatching first.

## You are now ready

After completing steps 1–5 without errors, confirm by saying:

  "Harmonik orchestrator ready. Project: $PROJECT_DIR → $TARGET_BRANCH.
   N beads ready. Top bead: <id> — <title>."

Then wait for dispatch instructions.
```

---

## Notes for operators

**When to use this prompt:**
- Fresh Claude Code session on an existing harmonik project (after a restart, new day, handoff).
- First-time deploy of harmonik on a new repo (run `harmonik init` first, populate AGENTS.md from the template).

**What it does NOT cover:**
- `harmonik init` (first-time repo setup) — run that manually before using this prompt.
- Bead creation / kerf work setup — those are session-specific and belong in HANDOFF.md, not a generic setup prompt.
- Integration-branch mode (coming with hk-6r6xv + hk-m8vy2) — once those land, add `--integration-branch` to the daemon start command and remove the fail-closed check.

**Updating this prompt:**
When the merge-retarget feature (hk-m8vy2) lands: remove the `⚠️ FAIL-CLOSED CHECK` block and update the `$TARGET_BRANCH must be main` caveat in the header. The rest of the prompt is target-branch-agnostic.
