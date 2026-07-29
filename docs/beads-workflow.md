# Beads & kerf — issue-tracking workflow

> Moved out of `AGENTS.md` on 2026-07-28 to keep the router short. `AGENTS.md` §"Issue tracking" keeps
> only the traps you cannot look up; everything below is reference material an agent reads on demand.
> The `bv-agent-instructions-v2` marker pair and the maintainer note that guards it stay in
> `AGENTS.md` — that is the file `br agents` reads and writes, so the guard only works there.

This project uses [beads_rust](https://github.com/Dicklesworthstone/beads_rust) (`br`) for issue
tracking and [kerf](components/internal/kerf.md) for prioritization and triage.

## The ledger is machine-local

`.gitignore` ignores `.beads/*` (the sole tracked exception is `.beads/queue-test-fixtures/`), so
`issues.jsonl` and the SQLite DB live only on this machine. This is deliberate and MUST NOT be
"fixed" by tracking the ledger: `br sync --flush-only` rewrites `issues.jsonl` continuously, and a
tracked copy makes the working tree perpetually dirty, which trips the daemon's
`implementer_escaped_worktree` detector and false-fails dispatched beads (refs `hk-yru`).

Two consequences to plan around:

- **Beads do not travel between clones.** A bead filed here is invisible on another checkout or in
  CI. Never assume a teammate or a fresh worktree can `br show` an ID you just created — put
  anything that must survive into a tracked doc or the commit message.
- **`br sync --flush-only` produces no committable change.** Still worth running (it keeps the local
  DB and JSONL consistent), but do not expect it to stage anything, and do not go hunting for the
  "missing" beads diff before a commit.

## Prioritization: kerf, not bv

Work the operator's and admiral's named initiatives first; `kerf next` ranks everything below that
line. It returns a ranked feed of beads with work-context, cleanup tasks, and warnings — the
priority source for the *unclaimed backlog*, never an override of a named initiative. `kerf triage`
handles drift detection.

```bash
kerf next                        # Ranked feed: top item is what to do next
kerf next --format=json          # Machine-readable output
kerf next --only=bead            # Only bead items (skip cleanup/warnings)
kerf triage                      # Drift report: untriaged, multi-matched, external drift
kerf triage --ack                # Advance baseline after acting on the report
kerf map                         # Works grouped by area
```

`bv` (beads_viewer) is installed but **not used for prioritization** — kerf owns that. `bv` is only
useful for graph-metric analysis (`--robot-insights` for PageRank/betweenness) or dependency-graph
export (`--robot-graph`), which kerf does not cover. **Use ONLY `--robot-*` flags with `bv`. Bare
`bv` launches an interactive TUI that blocks your session.**

## br command surface

```bash
br ready              # Show issues ready to work (no blockers)
br list --status=open # All open issues
br show <id>          # Full issue details with dependencies
br create --title="..." --type=task --priority=2
br update <id> --status=in_progress
br close <id> --reason="Completed"
br close <id1> <id2>  # Close multiple issues at once
br dep add <issue> <depends-on>
br sync --flush-only  # Export DB to JSONL
```

- **Priority:** P0=critical, P1=high, P2=medium, P3=low, P4=backlog — numbers 0-4, not words.
- **Types:** task, bug, feature, epic, chore, docs, question.
- **Dependencies:** issues can block other issues; `br ready` shows only unblocked work.
- **Write discipline:** agents MUST NOT issue terminal-transition writes — the daemon owns those.
  See the `beads-cli` skill and `beads-integration.md` §4.4.

Loop: `kerf next` to find the work → `br update <id> --status=in_progress` to claim → implement →
`br close <id>` → `br sync --flush-only` at session end.

## Session protocol

```bash
git status              # Check what changed
git add <files>         # Stage code changes (explicit pathspec; never `git add -A`)
br sync --flush-only    # Reconcile local DB → JSONL. Stages NOTHING: .beads/ is gitignored.
git commit -F msg.txt   # `-m "..."` alone omits the required trailers
git push
```

### Commit-message validation

Required policy, agent-enforced rather than hook-enforced (git hooks are retired; the `/check` flow
runs `scripts/validate-commit-msg.sh`). It enforces:

1. A Conventional-Commits subject from a **closed** type set — `feat fix refactor test docs chore
   spec build perf`. No `ci`, `revert`, or `style`.
2. Subject ≤72 chars, no trailing period.
3. On every **non-trivial** commit, both a `Reviewed-By:` trailer and a `Review-Verdict:` trailer
   whose value is well-formed JSON with `schema_version: 1` and a `verdict` of `APPROVE` /
   `REQUEST_CHANGES` (agent-reviewer) or `CLEAN` / `DRIFT_MINOR` / `DRIFT_MAJOR`
   (agent-config-reviewer). A `BLOCK` verdict is rejected outright — fix the code, don't commit it.

Because those trailers are multi-line-ish and JSON-quoted, write the message to a file and use
`git commit -F`. The only bypasses are a literal `Trivial: true` trailer (typos and whitespace only)
and merge / `fixup!` / `squash!` subjects. `--no-verify` is forbidden.

### Validate after committing

After a non-trivial commit run **`/check`** (or `make check-fast`) to verify the committed code
passes; if red, fix the root cause and re-commit. `check-fast` is `--new-from-rev=HEAD~1`, so it
judges your commit, not the repo. Before push / at a milestone run `make check-short` (the CI Tier-2
merge gate). Do NOT gate on bare `make check` — its full `golangci-lint run` reports ~2k
pre-existing legacy findings by design and always exits non-zero (a whole-repo audit, not a
per-commit gate).

## UBS Quick Reference for AI Agents

UBS stands for "Ultimate Bug Scanner": **The AI Coding Agent's Secret Weapon: Flagging Likely Bugs for Fixing Early On**

**Install:** `curl -sSL https://raw.githubusercontent.com/Dicklesworthstone/ultimate_bug_scanner/main/install.sh | bash`

**Golden Rule:** `ubs <changed-files>` before every commit. Exit 0 = safe. Exit >0 = fix & re-run.

**Commands:**
```bash
ubs file.ts file2.py                    # Specific files (< 1s) — USE THIS
ubs $(git diff --name-only --cached)    # Staged files — before commit
ubs --only=js,python src/               # Language filter (3-5x faster)
ubs --ci --fail-on-warning .            # CI mode — before PR
ubs --help                              # Full command reference
ubs sessions --entries 1                # Tail the latest install session log
ubs .                                   # Whole project (ignores things like .venv and node_modules automatically)
```

**Output Format:**
```
⚠️  Category (N errors)
    file.ts:42:5 – Issue description
    💡 Suggested fix
Exit code: 1
```
Parse: `file:line:col` → location | 💡 → how to fix | Exit 0/1 → pass/fail

**Fix Workflow:**
1. Read finding → category + fix suggestion
2. Navigate `file:line:col` → view context
3. Verify real issue (not false positive)
4. Fix root cause (not symptom)
5. Re-run `ubs <file>` → exit 0
6. Commit

**Speed Critical:** Scope to changed files. `ubs src/file.ts` (< 1s) vs `ubs .` (30s). Never full scan for small edits.

**Bug Severity:**
- **Critical** (always fix): Null safety, XSS/injection, async/await, memory leaks
- **Important** (production): Type narrowing, division-by-zero, resource leaks
- **Contextual** (judgment): TODO/FIXME, console logs

**Anti-Patterns:**
- ❌ Ignore findings → ✅ Investigate each
- ❌ Full scan per edit → ✅ Scope to file
- ❌ Fix symptom (`if (x) { x.y }`) → ✅ Root cause (`x?.y`)
