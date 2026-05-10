# Implementer Protocol

Standing rules for implementer agents dispatched by the orchestrator. The bead body is the work spec; this doc is the conventions layer that does not belong in every brief.

## Pre-flight

1. Read `CLAUDE.md` for project context.
2. Read the bead via `br show <bead-id> --format json`. The `description` field is the work spec; cited spec sections are normative.
3. Read the cited spec section(s).
4. Read 1–2 canonical sibling files in the target package for pattern conventions.
5. Then implement.

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

## Bead-close ownership (CLARIFIED — agent owns)

Per HANDOFF v20 directives + memory `feedback_br_ownership`, **the agent (you) owns `br close`**. After your commit lands and tests are green, run `br close <bead-id> -r "<one-line>"` yourself. Earlier protocol drafts said the orchestrator owned closes — that was wrong; not closing your own beads forces the orchestrator into per-bead cleanup passes that waste throughput. Close as you go.

For SUBSUMED beads where no commit is made, still close: `br close <id> -r "SUBSUMED: <file:line ref>"`.

## Continue claiming until 250k (HARD RULE)

The dispatch budget is **~250k tokens**, not "your initial bundle." After closing each bead:

1. Run `br ready --limit 0` (or filtered to your package: `br ready --limit 0 | grep <prefix>`).
2. If ready beads remain that match your dispatch scope (or any in-scope package per HANDOFF if your brief was open), claim and work the next one.
3. Stop ONLY when: (a) your context exceeds ~250k tokens, (b) the ready queue is empty of in-scope beads, OR (c) you hit a hard blocker that needs orchestrator intervention.

Implementers stopping at 80–160k tokens with ready queue still populated is **wasted dispatch budget**. The HANDOFF directive "implementer keeps claiming and working ready beads until its context exceeds ~250k tokens" is non-negotiable.

When you do hit ~250k, run the `/session-handoff` skill before exiting.

## Don't ask questions back

You will not get an answer — the orchestrator dispatches you and moves on. **Make the judgment call yourself** and document the reasoning in your commit body. Path discrepancies, ambiguous spec wording, type-naming choices, scope edges — decide. If you genuinely cannot proceed (hard blocker, e.g. a required upstream bead is missing code), surface it in the stopping report and stop on that bead, but keep working other in-scope beads first.

## Constraints

- Do NOT push.
- Do NOT merge.
- The orchestrator handles merge dance, push, and worktree cleanup after your dispatch returns.

## Run before committing

- `go build ./...`
- `go test ./<target-package>/...`
- `gofmt -d ./<target-package>/` (must show empty diff)
- Optional: `golangci-lint run ./<target-package>/...` if available locally

If any of the above fails, fix before committing. Do not commit broken code expecting the reviewer to flag it.
