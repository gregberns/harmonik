# Beads & kerf — issue-tracking workflow

> Moved out of `AGENTS.md` on 2026-07-28 to keep the router short. `AGENTS.md` §"Issue tracking" keeps
> only the traps you cannot look up; everything below is reference material an agent reads on demand.
> The `bv-agent-instructions-v2` marker pair and the maintainer note that guards it stay in
> `AGENTS.md` — that is the file `br agents` reads and writes, so the guard only works there.

This project uses [beads_rust](https://github.com/Dicklesworthstone/beads_rust) (`br`) for issue
tracking and [kerf](components/internal/kerf.md) for spec-first planning and drift triage. Priority
does not come from kerf — see §"Priority" below.

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

## Priority: stated intent first, then the ledger

Work the named initiatives of the operator and the admiral first. They live in the active plan's
order, in the dated directives in `.harmonik/context/captain-lanes.md`, and in the direction-log
RETURN-PATH. Nothing in the ledger outranks them.

Below that line, order the unclaimed backlog from the ledger itself:

```bash
br ready --sort priority --limit 0            # The whole unclaimed backlog, highest priority first
br ready --sort priority --parent <epic_id>   # The same, scoped to one lane
br ready --sort oldest                        # Surface the work that is starving
```

Pass `--limit 0`. `br ready` returns 20 rows by default and sorts by `hybrid`. A short default
listing is not evidence of a short backlog, and reading it as one has hidden real work.

`kerf` plans work. It does not rank work. Use `kerf map` to see which work owns a bead and what
context that bead carries. Do not take an order from `kerf next`. Its score comes from graph
structure and never reads the `br` priority field, so a P0 bead and a P3 bead come back the same.
It also reports empty for a work that has no `bead_filter`. Ranking what matters is judgment, and a
graph metric cannot do it for you.

`kerf triage` still earns its place. It reports drift — untriaged beads, multi-matched beads, and
beads that moved out from under a work:

```bash
kerf triage                      # Drift report: untriaged, multi-matched, external drift
kerf triage --ack                # Advance the baseline after you act on the report
kerf map                         # Works grouped by area
```

`bv` (beads_viewer) is installed, and it is not a priority source either. It is useful for
graph-metric analysis (`--robot-insights` for PageRank and betweenness) and for dependency-graph
export (`--robot-graph`), which kerf does not cover. **Use only the `--robot-*` flags with `bv`. A
bare `bv` launches an interactive TUI that takes over the terminal and blocks your session until
someone kills it.**

## br command surface

```bash
br ready              # Show issues ready to work (no blockers) — 20 rows, hybrid sort
br ready --sort priority --limit 0   # The whole ready backlog, highest priority first
br ready --sort oldest               # Ready work, oldest first — finds starving beads
br ready --parent <epic_id>          # Ready work inside one epic
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
- **Write discipline:** the predicate is **did you submit this bead to a queue?** — not "is it
  dispatched", which you cannot observe. On a bead you submitted, agents MUST NOT issue
  terminal-transition writes: the daemon owns those, `queue submit` refuses a bead already pre-set
  to `in_progress` (`bead_already_dispatched`, `-32015`, exit 1), and a hand `br close` from a
  worktree leaks to the parent repo before code lands. A bead you worked by hand is closed by
  whoever worked it, because nothing else will — `harmonik reconcile` closes only beads whose
  commit carries a `Harmonik-Bead-ID:` trailer, and a hand commit never carries one. See the
  `beads-cli` skill and `beads-integration.md` §4.4.

Loop for hand-run work: `br ready --sort priority --limit 0` to find the work →
`br update <id> --status=in_progress` to claim → implement → `br close <id>` →
`br sync --flush-only` at session end. This is the loop for work you run yourself and submit to no
queue. Once you submit a bead to a queue, both ends of that loop become the daemon's. One warning
about the claim step: claim a bead and then neither submit it nor work it through, and nothing
reports anything — no run exists for any watcher or sweep to notice.

## Session protocol

```bash
git status              # Check what changed
git add <files>         # Stage code changes (explicit pathspec; never `git add -A`)
br sync --flush-only    # Reconcile local DB → JSONL. Stages NOTHING: .beads/ is gitignored.
git commit -F msg.txt   # `-m "..."` alone omits the required trailers
git push
```

### Commit-message validation

Required policy, agent-enforced rather than hook-enforced (git hooks are retired; `make full` runs
`scripts/commit-msg-gate.sh`, which calls `harmonik commit-msg validate` over the commit just made).
It reads the review trailers and nothing else — the subject shape, the closed type set and the
72-character ceiling went out with the shell validator on 2026-08-23. It enforces:

1. On every **non-trivial** commit, both a `Reviewed-By:` trailer and a `Review-Verdict:` trailer
   whose value is well-formed JSON with `schema_version: 1` and a `verdict` of `APPROVE` /
   `REQUEST_CHANGES` (agent-reviewer) or `CLEAN` / `DRIFT_MINOR` / `DRIFT_MAJOR`
   (agent-config-reviewer). A `BLOCK` verdict is rejected outright — fix the code, don't commit it.
2. An `APPROVE` or `CLEAN` must name a reviewer this repo ships and carry `flags`; a `NOT_REVIEWED`
   must name none; and no verdict may be self-authored.

Because those trailers are multi-line-ish and JSON-quoted, write the message to a file and use
`git commit -F`. Check the file first with `go run ./cmd/harmonik commit-msg validate <file>`. Run
it from source: the subcommand landed on 2026-08-23, and an older installed `harmonik` exits 2.
The only bypasses
are a literal `Trivial: true` trailer (typos and whitespace only), merge / `fixup!` / `squash!`
subjects, and GitHub's synthetic pull-request merge commit. `--no-verify` is forbidden.

### Validate after committing

After a non-trivial commit run **`/check`** (`make fast`) to verify the committed code
passes; if red, fix the root cause and re-commit. `make fast` lints `--new-from-rev=HEAD~1`, so the
lint judges your commit, and it unit-tests the major packages. `make fast` is not a merge verdict:
it tests a chosen subset, so it can be green while the tree is red.

Before push, and before you declare work done, run `make full`. That is the merge decision and it is
what CI runs. It tests every package, it never scopes by what changed, and it never approves on a
timeout, an out-of-memory kill, a compile failure or a passing retry.

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

**Speed:** Scope to the changed files. `ubs src/file.ts` takes under a second and `ubs .` takes about 30 seconds, so a whole-project scan is a checkpoint you run on purpose, not the move for each edit.

**Bug Severity:**
- **Critical** (always fix): Null safety, XSS/injection, async/await, memory leaks
- **Important** (production): Type narrowing, division-by-zero, resource leaks
- **Contextual** (judgment): TODO/FIXME, console logs

**Anti-Patterns:**
- ❌ Ignore findings → ✅ Investigate each
- ❌ Full scan per edit → ✅ Scope to file
- ❌ Fix symptom (`if (x) { x.y }`) → ✅ Root cause (`x?.y`)
