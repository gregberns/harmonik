# Implementer Protocol

Standing rules for implementer agents dispatched by the orchestrator. The bead body is the work spec; this doc is the conventions layer that does not belong in every brief.

## Pre-flight

1. Read `CLAUDE.md` for project context.
2. Read the bead via `br show <bead-id> --format json`. The `description` field is the work spec; cited spec sections are normative.
3. Read the cited spec section(s).
4. Read 1–2 canonical sibling files in the target package for pattern conventions.
5. Then implement.

## Commit-early discipline (REQUIRED — protects against budget exhaustion)

You have a finite wall-clock budget (currently ~10 min). If you spend most of it exploring and then `/quit` interrupts you mid-edit, the run produces **zero commits** and the daemon classifies the bead as failed — even though you did meaningful work.

**Rule:** after your FIRST file modification (Edit or Write), immediately stage and commit a WIP checkpoint:

```bash
git add -A && git commit -m "WIP: <bead-id> — checkpoint after first edit" --no-verify
```

Then continue. You can amend (`git commit --amend`) or squash (`git rebase -i HEAD~N`) at the end before exit — see the "Commit format" section below for the final commit shape. The WIP commit is a safety net: if `/quit` arrives unexpectedly, at least the daemon sees progress and your partial work is preserved.

**Why `--no-verify` here:** the WIP isn't expected to pass tests/lint; the final commit (after amend/squash) does. The pre-exit verification (`go build`, `go test`, `gofmt -d`) gates only the final commit.

Skip the WIP only when the bead is a one-line typo/cross-reference fix.

## Helper-prefix discipline

When adding tests to an existing Go package, package-level test helpers MUST use a per-bead camelCase prefix (e.g., `leaseFixtureWriteLockAtomic`, NOT `leaseFixture_writeLockAtomic`). The brief tells you the prefix; if it doesn't, derive one from the bead's concept (e.g., `auditFixture`, `pidfileFixture`). Never collide with sibling-bead helpers.

## Lint compliance (project `.golangci.yml` enforces)

1. **camelCase**, NO underscores in identifiers (revive var-naming).
2. **`exec.Command` is forbidden** (noctx) — use `exec.CommandContext(t.Context(), ...)`.
3. **`net.Listen` / `net.Dial`** must use context-aware forms: `(&net.ListenConfig{}).Listen(t.Context(), ...)` and `(&net.Dialer{}).DialContext(t.Context(), ...)`.
4. **NO `panic(...)`** outside `internal/testhelpers/` — helpers MUST take `*testing.T` and call `t.Fatalf`.
5. **`os.ReadFile` / `os.Open` on constructed paths**: add `//nolint:gosec // G304: <reason citing path provenance>` immediately above.
6. **`os.MkdirAll` 0o755**: add `//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions`.
7. **Cleanup discards** like `defer x.Close()`: use `defer func() { _ = x.Close() }()` (errcheck-clean).
8. **`err != io.EOF`** must be `errors.Is(err, io.EOF)` (errorlint).
9. **gofmt-clean** struct field alignment — when a struct has columns of varying-length types, ALL columns must align uniformly. Run `gofmt -d <files>` before committing; output must be empty.

## Worktree discipline (CRITICAL — read first)

The orchestrator creates an isolated git worktree for you at a path like `/Users/gb/github/harmonik/.claude/worktrees/agent-<id>` on a branch named `worktree-agent-<id>`. **You commit on that branch, in that path. Never on `main`. Never in `/Users/gb/github/harmonik` directly.**

Before EVERY `git commit`, verify:

```
pwd                                    # MUST be your worktree path, not /Users/gb/github/harmonik
git branch --show-current              # MUST start with "worktree-agent-", NOT be "main"
git rev-parse --show-toplevel          # MUST equal your worktree path
```

If `git branch --show-current` returns `main`, you have escaped your worktree. STOP. Do not commit. Report the escape in your final summary so the orchestrator can recover.

The directives in `HANDOFF.md` describe a "merge dance" run from the main repo dir — that is the **orchestrator's** job after your review, NOT yours. You stay in the worktree for the entire dispatch. Read-only inspection of files under `/Users/gb/github/harmonik` is fine; ANY git write operation (commit, branch, reset, checkout) MUST happen from the worktree path.

**F18 — `.harmonik/` is per-tree.** Your worktree has its OWN `.harmonik/` directory (agent-task.md, reviewer-feedback files). The MAIN repo's `.harmonik/` (queue.json, events.jsonl, daemon.sock) is a DIFFERENT tree. Never read or write the main-repo `.harmonik/`.

## Edit discipline — unicode comment tables (F20)

When a Go file contains aligned Unicode box-drawing characters (e.g. `│`, `─`, `┌`, `└`) in comment tables, the Edit tool's `old_string` matching fails on the multi-byte sequences. Remedy: Read the exact bytes around your target anchor first, then use the shortest purely-ASCII substring in that row as the `old_string` anchor (e.g. the label text or an adjacent Go keyword) rather than the full aligned row.

## Commit format (REQUIRED — verbatim HEREDOC pattern with quoted EOF)

```
git commit -m "$(cat <<'EOF'
<type>(<scope>): <subject ≤72 chars>

- <file>: <one-line bullet>
- <file>: <one-line bullet>

Refs: <bead-id>
EOF
)"
```

The quoted `'EOF'` prevents shell expansion. After committing, verify with `git show HEAD --format='%s'` — output MUST be ONLY the subject line, NOT bullets collapsed in.

Do NOT add `## Why / ## What / ## Spec alignment / ## Test plan / ## Risk` sections. Do NOT add `Reviewed-By:` or `Review-Verdict:` trailers. The orchestrator-directive commit format overrides any `build-practices.md` template.

## Typed-alias-deferral pattern

When the bead's record references a type not yet in `core/` (or the relevant package): use `*string`/`string` placeholder + godoc TODO citing the spec section + create a follow-up bead via `br create` for the typed wrapper, and substitute the returned bead ID into the godoc.

The brief will name the follow-up `br create` command and the godoc shape. Do not invent your own placeholder schemes.

## Path-discrepancy resolution

When the bead body and a referenced doc disagree on a path or identifier: **bead body wins.** Patch the inconsistent file in the same commit and surface the still-stale doc to a follow-up bead.

EXCEPTION — for **spec content** (enum values, regex shapes, RECORD field-types), the spec wins per CLAUDE.md ("specs are normative"); the bead body gets a follow-up note instead.

## Reporting format

At the end of every dispatch, report:
- Worktree path and branch name (branch MUST start with `worktree-agent-`; flag if `main`)
- Commit SHA
- Files added/modified with brief descriptions
- Test output summary (PASS counts; failure modes if any)
- `gofmt -d <package-path>` output (must be empty — confirm explicitly)
- Any follow-up beads you created (with their IDs)
- Any deviations from the bead body or brief, with reasoning

## Bead-close ownership (DAEMON-OWNED — do NOT close beads)

DO NOT run `br close`, `br update --status closed`, or any terminal bead transition.
The daemon owns all bead lifecycle transitions (open -> in_progress -> closed/failed).
Running `br close` from the worktree causes premature closure that leaks to the parent
repo even when no implementation has landed — this was the root cause of the ~80%
"close-without-impl" failure rate in v60.

Your job: implement, commit, and `/quit`. The daemon closes the bead after verifying
your commit landed and passed review.

If the work appears already done (SUBSUMED), commit a verification test or a
documentation note rather than closing the bead and exiting. The daemon will detect
a no-commit exit and classify it as a failure.

## Do your assigned bead(s) and exit (HARD RULE — replaces "continue claiming until 250k", L-015)

**You work the beads named in your brief's SCOPE line. When those are closed, you exit.**

Do NOT free-claim additional beads from `br ready` after your assigned scope is done — even if the queue still has work, even if you have lots of context budget left. The orchestrator (main thread) owns refill: when your dispatch returns, it will spawn a fresh implementer on the next bead. Free-claiming caused L-013 (two implementers grabbing the same bead via overlapping in-scope rules) and the L-015 collision (one implementer crossing spec boundaries to claim a bead the orchestrator was simultaneously dispatching to a sibling).

The "until 250k tokens" budget rule is for the **main orchestrator thread** (it keeps the slot floor saturated and writes a fresh HANDOFF when it approaches its own context ceiling). It is NOT for you. Earlier drafts of this doc copied that rule into the implementer surface — that was the drift that L-015 captures.

If your assigned scope drains very fast (e.g., everything was SUBSUMED), report and exit. The orchestrator will refill faster than you can free-claim safely.

## Don't ask questions back

You will not get an answer — the orchestrator dispatches you and moves on. **Make the judgment call yourself** and document the reasoning in your commit body. Path discrepancies, ambiguous spec wording, type-naming choices, scope edges — decide. If you genuinely cannot proceed (hard blocker, e.g. a required upstream bead is missing code), surface it in the stopping report and exit.

## Constraints

- Do NOT push.
- Do NOT merge.
- The orchestrator handles merge dance, push, and worktree cleanup after your dispatch returns.

## Run before committing

- `go build ./...`
- `go test ./<target-package>/...`
- `gofmt -d ./<target-package>/` (must show empty diff)
- Optional: `golangci-lint run ./<target-package>/...` if available locally

**F19 — Never run daemon-booting or scenario-tagged suites.** Tests tagged `//go:build scenario` spin up real daemons and routinely exceed your wall-clock budget, causing Exit-137 (SIGKILL). Run only the targeted fast unit-test gate for your package. If a scenario test is the natural gate for your bead, skip it here and note in your commit that the scenario-test gate is deferred.

If any of the above fails, fix before committing. Do not commit broken code expecting the reviewer to flag it.

## Pre-exit rebase (REQUIRED — run after committing, before /quit)

After your commit, rebase your branch against the run's configured base to pick up any upstream advances during your work:

```bash
BASE_BRANCH=$(grep '^base_branch:' .harmonik/agent-task.md | awk '{print $2}')
BASE_BRANCH=${BASE_BRANCH:-main}
git fetch origin
git rebase origin/$BASE_BRANCH
```

The `base_branch` value is in the `agent-task.md` header (e.g. `base_branch: main` or `base_branch: integration/my-feature`). Use it verbatim — do NOT hardcode `main`. If `base_branch` is absent, default to `main`.

Rationale: the base may have advanced while you worked. Rebasing here avoids a daemon-side rebase conflict during `mergeRunBranchToMain`. If the rebase conflicts, resolve them and re-run the pre-commit checks before exiting.

## Appendix — Brief template (orchestrator-facing)

The orchestrator composes briefs as a parameter-fill against this template. The implementer reads the bead body via `br show`; the brief should NOT paraphrase it. Target ≤15 lines.

```
WORKTREE: /Users/gb/github/harmonik/.claude/worktrees/agent-<id>
BRANCH:   worktree-agent-<id>
SCOPE:    <starting bead-id> + any in-scope ready beads in <package-path>
SIBLING:  <file:path>:<line> — pattern for <acronym-id> from prior wave (omit only when no prior sibling exists)
DEFERRAL: <one-line shape for follow-up `br create` if the bead requires typed-alias deferral; omit otherwise>
PROTOCOL: read .claude/implementer-protocol.md — it is authoritative; do not re-state rules in this brief.
```

Rules for filling the template:

1. **Do NOT paraphrase the bead body.** Implementer fetches via `br show <id> --format json`.
2. **SIBLING line is required when a prior sibling exists** — empirical SUBSUMED-detection rate jumps from ~5% to ~30% when present (L-008).
3. **DEFERRAL line names the exact `br create` shape** — flags, labels, parent — so the implementer doesn't invent its own placeholder scheme (see "Typed-alias-deferral pattern" above).
4. **No reporting-format reminder, no no-ask reminder, no commit-format reminder.** All in this protocol; the brief points, doesn't repeat.

Brief composition is structurally deterministic (bead → package → sibling-pattern → worktree-name). When harmonik's daemon ships, the daemon composes briefs; the orchestrator-agent's job collapses to dispatch decisions, not template-filling. See L-005 in `docs/orchestration-learnings.md`.
