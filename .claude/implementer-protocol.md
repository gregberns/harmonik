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
- Worktree path and branch name
- Commit SHA
- Files added/modified with brief descriptions
- Test output summary (PASS counts; failure modes if any)
- `gofmt -d <package-path>` output (must be empty — confirm explicitly)
- Any follow-up beads you created (with their IDs)
- Any deviations from the bead body or brief, with reasoning

## Constraints

- Do NOT push.
- Do NOT merge.
- Do NOT close the bead.
- Do NOT update the bead's status (the orchestrator owns claim/close transitions).
- The orchestrator handles merge dance, push, worktree cleanup, and bead closure after review.

## Run before committing

- `go build ./...`
- `go test ./<target-package>/...`
- `gofmt -d ./<target-package>/` (must show empty diff)
- Optional: `golangci-lint run ./<target-package>/...` if available locally

If any of the above fails, fix before committing. Do not commit broken code expecting the reviewer to flag it.
