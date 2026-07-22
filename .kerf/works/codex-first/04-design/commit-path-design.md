# 04 — Change Design: commit-path (component C — confirmation)

> Pass 4. See also `code-seams-design.md` §C and research
> `03-research/commit-path/findings.md`.

## Current state == Target state (no code change)
`ensureCodexRefsTrailer` (`codexcommit.go:204`, per implement-node exit `dot_cascade.go:1938`)
stages+commits codex's edits from OUTSIDE any sandbox with a `Refs:<bead>` trailer; de-facto
committer today (research/05 F2). Under danger-full-access codex may self-commit; the fallback
stays as an idempotent backstop that no-ops when HEAD already carries the trailer.

## Rationale
Landing the diff does NOT depend on codex self-committing (D2-direction finding, still valid).
Acceptance accepts EITHER committer.

## Requirements traceability
02 area C "must be true" → this confirmation. Recorded in HN-026's INFORMATIVE note. No spec change.
