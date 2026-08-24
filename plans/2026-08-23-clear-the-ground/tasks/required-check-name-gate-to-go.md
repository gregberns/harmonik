---
id: required-check-name-gate-to-go
title: Parse the CI workflow files with a YAML parser instead of 2,400 lines of shell
type: task
priority: 2
labels: [scripts, shell-to-go, gate, ci, clear-the-ground]
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

`scripts/required-check-name-gate.sh` is 1,049 lines and its self-test
`scripts/required-check-name-gate-test.sh` is 1,372 — 2,421 lines for one gate. The self-test alone
is the third-largest shell file in `scripts/`, behind only `scripts/ops-monitor-check.sh` (2,058) and
`scripts/scratch-daemon.sh` (1,947).

What it does is read GitHub Actions workflow YAML and answer questions about it. From its own header:
branch protection on `main` requires one status context; a GitHub job reports under its `name:`, so
renaming the job means the required context never arrives, and GitHub reads a context that never
arrives as PENDING rather than as failing. Every later pull request then waits forever on a check
that cannot come. The header records that this already happened here — a commit renamed the job to
`make full` and would have wedged `main` the moment it landed.

The header also lists the ways the context is lost **without** a rename: a build matrix turns the
context into `check (Tier 2) (1.22)`, the workflow stops triggering on pull requests, or a path
filter skips the run. Each of those is a structured question about a YAML document — is there a
`strategy.matrix`, does `on.pull_request` exist, is there a `paths:` filter — and each is being
answered by text matching in bash.

**It is gate-critical.** It is a step of `freeze-gates`, so `make fast`, `make core` and `make full`
all run it. CI also depends on it in the other direction: `.github/workflows/ci.yml` carries a
comment saying this script holds the job name that branch protection asks for.

Go has a YAML parser. Bash does not, and the 1,372-line self-test is the price of pretending
otherwise — the test is larger than the thing it tests, which is what happens when the
implementation has no structure a test can address.

## Scope

- `scripts/required-check-name-gate.sh` — the required context name, the matrix check, the trigger
  check, the path-filter check, and the decoy-job check.
- `scripts/required-check-name-gate-test.sh` — the case corpus to port.
- `.github/workflows/*.yml` — read only. This task changes no workflow.
- A new Go program under `tools/`, invoked from `freeze-gates` in the `Makefile`.
- `make script-tests`, which names the self-test today.

## Done when

1. A Go program under `tools/` reads the workflow files with a real YAML parser and enforces every
   condition the shell enforced.
2. The `freeze-gates` target calls it and names no converted script; `script-tests` no longer names
   the converted self-test.
3. Both `.sh` files are gone.
4. A Go test asserts each condition fails on a synthesized workflow that violates it — a renamed job,
   a job that gained a matrix, a workflow that lost its `pull_request` trigger, a workflow that
   gained a path filter, and a decoy job. Fixture workflows live in `testdata/`, so the test never
   edits the real `.github/` tree.
5. A Go test asserts the gate passes on the current real workflow files.
6. `make fast` and `make full` are green.

## Limits

- **Do not change what the gate enforces, and do not relax it because YAML made a case easier to
  see.** If the parser reveals a condition the shell was checking only by accident, keep the
  condition and say so in the commit body.
- **Do not edit any file under `.github/`.** The required context name is set by branch protection,
  which is outside this repository; renaming a job here to make a test simpler wedges `main`.
- **Do not widen `tools/lintreport/allow.txt`.**
- **Do not convert and delete in one landing if that leaves `freeze-gates` unrunnable mid-way.**
- **Put it under `tools/`, not in `cmd/harmonik`** — W4 is shrinking `package main`.
- `gopkg.in/yaml.v3` is already in `go.mod`. Use it rather than adding a second YAML module —
  `make full` runs `module-hygiene`, and a new dependency needs a reason stated in the commit body.
