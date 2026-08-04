# Harmonik — Works-Today Setup Agent Prompt

> **What this is:** A canned prompt to paste into a fresh Claude Code session when deploying harmonik on a new project. Copy the block below, substitute the two placeholders, and paste.
>
> **Corrected 2026-08-04.** This note said `$TARGET_BRANCH` MUST be `main`, and cited bead `hk-m8vy2` as the tracking bead for merge-retarget. That guard is gone from the code (see `specs/process-lifecycle.md`), and the bead is not in the ledger. `harmonik init --target-branch <branch>` accepts any branch and exits 0.
>
> **Set `$TARGET_BRANCH` to an integration branch, not `main`.** Then add `main` to `protect_branches` so the daemon fails closed and never pushes `main`. A person moves the integration branch into `main` with a pull request.

---

## Canned Setup Prompt

> Replace `$PROJECT_DIR` and `$TARGET_BRANCH` before pasting. Set `$TARGET_BRANCH` to your integration branch.

```
You are being set up as a harmonik orchestrator agent for a new project.

Project directory: $PROJECT_DIR
Target branch (harmonik merges completed work here): $TARGET_BRANCH

⚠️  FAIL-CLOSED CHECK: if $TARGET_BRANCH == "main", stop here and tell me.
Harmonik must merge completed work into an integration branch. It must never
merge into main. A person moves the integration branch into main with a pull
request. Also add main to protect_branches so the daemon refuses to push it.

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

  harmonik init --project $PROJECT_DIR --target-branch $TARGET_BRANCH

This writes .harmonik/branching.yaml with lands_on: $TARGET_BRANCH. Open that
file and add main to protect_branches, so the daemon fails closed. Add ONLY the
protect_branches key. Keep every other line, including version: 1 — the loader
accepts version 1 only, and the daemon refuses to start on any other value. The
result looks like this:

  version: 1
  defaults:
    start_from: $TARGET_BRANCH
    lands_on: $TARGET_BRANCH
    landing_strategy: squash
    protect_branches:
      - main

## Step 4 — Start the daemon (if not already running)

Check first:

  harmonik queue status   # exit 0 = daemon up, exit 17 = not running

If exit 17, start the daemon in a detached tmux session:

  tmux new-session -d -s harmonik-daemon \
    'harmonik --project $PROJECT_DIR --no-auto-pull --max-concurrent 4 \
       --target-branch $TARGET_BRANCH --protect-branch main'

The daemon refuses to start if the resolved target branch is protected. That is
the fail-closed guard that keeps work off main.

Then confirm it came up:

  harmonik queue status   # should now exit 0

Do NOT start a second daemon if one is already running — it collides on the
pidfile lock (exit 5).

## Step 5 — Read project state

  cat $PROJECT_DIR/AGENT_INDEX.md   # master knowledge-base map
  cat $PROJECT_DIR/STATUS.md        # current project state
  # this-session state — absent on projects inited before it was scaffolded
  cat $PROJECT_DIR/HANDOFF.md 2>/dev/null || echo "(no HANDOFF.md yet)"

The active work list is the bead ledger, not a file. Run:

  br ready               # unblocked beads
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
- Integration-branch mode has landed. There is no `--integration-branch` flag. Set the branch with `--target-branch` on `harmonik init`, or with `defaults.lands_on` in `.harmonik/branching.yaml`. Protect `main` with `--protect-branch main` or with `defaults.protect_branches`.

**Updating this prompt:**
The merge-retarget feature has landed, so the prompt no longer forces `main`. The `⚠️ FAIL-CLOSED CHECK` block now does the opposite job: it stops a setup that would target `main`. The rest of the prompt is target-branch-agnostic.
