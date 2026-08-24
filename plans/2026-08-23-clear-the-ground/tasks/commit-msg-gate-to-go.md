---
id: commit-msg-gate-to-go
title: Finish the commit-message gate conversion that already has a Go validator behind it
type: task
priority: 2
labels: [scripts, shell-to-go, gate, clear-the-ground]
depends_on: []
blocks: []
workstream: unassigned
batch: 6
---

> **Workstream unassigned.** These shell-to-Go conversions do not belong to `PLAN.md` §W6,
> which is the `crew-cleanup` skill. Whether they form a workstream of their own is an open
> operator decision, so this field reads `unassigned` rather than carrying a number that is
> already taken. Do not invent one. This task is parked until that ruling lands.

## Problem

This one is half converted already, and the half that remains is the half that fails silently.

`scripts/commit-msg-gate.sh` (279 lines) and `scripts/commit-msg-gate-test.sh` (345 lines) do not
implement any commit-message rule. The rules live in Go — in `internal/commitmsg`, compiled into
`./cmd/harmonik` — and the `commit-msg-check` target in the `Makefile` builds that binary, passes its
path down as `COMMIT_MSG_VALIDATOR`, and runs the shell over real commits. The 624 lines of shell are
a range selector, a loop, and a result formatter around a Go program.

**The `commit-msg-check` target itself carries a comment recording a silent-empty failure of exactly
the kind shell invites.** Quoting it: `-run` applies to every package on the same command line, so
folding two `go test` invocations together made one line report `[no tests to run]` for the package
holding all 163 rules — "a target that claimed to run the rules and ran none". The target had to be
split into two invocations to work around it. That is a `go test` argument problem being managed by
hand in a recipe.

The script's own header records the same class of defect from the other side: the validator refused
every fabricated-reviewer trailer on record, and **nothing ran it on a real commit** — three places
in `docs/foundation/project-level/build-practices.md` said it ran "via the `/check` flow", which
`.claude/commands/check.md` never mentioned and no target invoked. Commits carrying an approval
nobody gave landed after the rule that refuses them.

The gate is in `make full` only, not in `make fast` — an operator ruling of 2026-08-23 took it out of
the inner loop, and the `Makefile` says the saving was measured at 0.5 s for the check plus 98 s for
the two self-tests that came with it. That ruling stands and this task does not touch it.

## Scope

- `scripts/commit-msg-gate.sh` — the `--head-only` and range modes, the commit iteration, the report.
- `scripts/commit-msg-gate-test.sh` — the case corpus to port.
- `internal/commitmsg` and the validator entry point in `./cmd/harmonik` — the rules already live
  here and stay here.
- The `commit-msg-check` target in the `Makefile`, including the two-invocation `go test` split.

## Done when

1. Selecting the commits and reporting the result happens in Go — as a subcommand of the existing
   validator or as a program under `tools/` — and no shell stands between the `Makefile` and the
   rules.
2. `commit-msg-check` calls it, and the `COMMIT_MSG_VALIDATOR` environment-variable hand-off is gone.
3. Both `.sh` files are gone.
4. A Go test proves the gate **rejects** a real commit carrying a fabricated reviewer trailer and a
   malformed verdict, constructed in a scratch git repository in the test — not a string fixture.
   The header's recorded defect was that the rules ran on fixtures and never on a commit.
5. A Go test proves the gate reports non-zero when the range contains a bad commit and zero when it
   does not, in both `--head-only` and range scope.
6. A Go test in `internal/commitmsg` asserts the rule count is what the package believes it is, so
   the `[no tests to run]` failure the `Makefile` comment describes cannot recur silently.
7. `make full` is green.

## Limits

- **Do not change any commit-message rule.** No rule added, removed, or loosened. This changes who
  invokes the rules, not what they say. In particular, do not relax the `Reviewed-By:` /
  `Review-Verdict:` trailer requirement or the reviewer-name check.
- **Do not put `commit-msg-check` back into `make fast` or `gate-static-product`.** That is a
  standing operator ruling with a measured cost, recorded in the `Makefile`.
- **Do not widen `tools/lintreport/allow.txt`.**
- **Do not convert and delete in one landing if that leaves `make full` unable to check commit
  messages.**
- Keep the two-invocation `go test` split in `commit-msg-check`, or remove it only with a test that
  proves the rule package actually ran. Merging the lines back is the defect the comment records.
- The validator is built from `./cmd/harmonik`, which W4 is shrinking. Do not add bulk to
  `package main`; put new code in `internal/commitmsg` or under `tools/`.
