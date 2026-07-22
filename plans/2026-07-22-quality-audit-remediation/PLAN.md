# Quality-audit remediation plan

Date: 2026-07-22
Source: `plans/2026-07-21-p2-extraction/QUALITY-AUDIT-2026-07-22.md`

## Objective

Reduce broad production lint debt and repair quality-suite blind spots without
colliding with the concurrent P2 extraction. The result must improve signal:
new gates may not be permanently red, and mechanical suppression removal may
not hide newly exposed findings.

## Collision contract

The P2 agent's `plans/2026-07-21-p2-extraction/PROGRESS.md` is authoritative for
its live file set. This stream will not edit `.golangci.yml`, `internal/daemon/**`,
`internal/harness/**`, `internal/transport/**`, `internal/queuewiring/**`,
`internal/crewrun/**`, or P2 plan artifacts while Phase 3 is active.

Every lane stages or reports only explicit files. Never use `git add -A`,
`git commit -a`, `git reset --hard`, or `git clean`.

## Parallel lanes

1. **Codex wire cleanup** — clear the scoped `internal/codexwire` lint set,
   starting with 74 `revive` findings and then correctness findings. Tests:
   scoped lint plus `go test ./internal/codexwire`.
2. **Agent manifest cleanup** — fix `errcheck` and other correctness findings in
   `internal/agentmanifest`; triage `gosec` individually rather than bulk-suppress.
   Tests: scoped lint plus package tests.
3. **Small dense packages** — fix correctness/context findings in
   `internal/hookrelay`, `internal/sessiondata`, and `internal/supervise`, with
   per-package commits/diffs and focused tests.
4. **Suite-boundary repair** — make formatting/typecheck/full-lint measurement
   ignore managed scratch/worktree artifacts without hiding tracked production
   files; design and test a baseline ratchet for `cmd/**` coverage, but do not
   impose the currently impossible 90% floor.

## Gates

- No edits inside the current P2 collision set.
- `git diff --check` and targeted package tests pass.
- Scoped full lint count does not increase.
- Semantic changes receive a fresh-context review including production call sites.
- Coverage enforcement remains passable from an honest baseline.
- Full-suite failures are classified as introduced, pre-existing, environmental,
  or deliberately failing evaluation fixtures.

## Done means

- Each lane has an exact before/after finding count and file list.
- Safe fixes are implemented and tested; unsafe/ambiguous findings are recorded.
- The live progress document identifies ownership and remaining blockers.
- P2-owned deferred fixes are handed back after Phase 3 rather than edited here.
