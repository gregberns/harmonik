---
id: coverage-gates-to-go
title: Rewrite the coverage gates in Go — the current ones need a bash the operating system does not ship
type: task
priority: 2
labels: [scripts, shell-to-go, coverage, portability, clear-the-ground]
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

Five scripts, 1,382 lines, all parsing `go test -coverprofile` output in bash:

| script | lines | called by |
|---|---|---|
| `scripts/changed-func-coverage-test.sh` | 504 | `make script-tests` — so `make fast` and `make full` |
| `scripts/changed-func-coverage.sh` | 314 | `make coverage-changed` |
| `scripts/coverage-gate.sh` | 285 | `make coverage-gates` |
| `scripts/cmd-coverage-gate.sh` | 203 | `make coverage-gates` |
| `scripts/codex-coverage-gate.sh` | 76 | `make test-codex-l012` |

**The portability defect is in the file, not hypothetical.** `scripts/coverage-gate.sh` opens with a
re-exec block: if `BASH_VERSINFO[0]` is under 4 it searches for `/opt/homebrew/bin/bash` and
`/usr/local/bin/bash`, and exits 1 with "requires bash >= 4 (associative arrays)" if neither exists.
It needs bash 4 for `declare -A`. Stock macOS ships bash 3.2. So the gate depends on a Homebrew
install at a hard-coded path, and on an Apple Silicon prefix at that — the same class of problem that
`f7e4a415f` had to fix in `scripts/agent-reviewer-prompt-parity-test.sh`, where `mapfile` (also bash
4) aborted `make fast` on stock macOS **before** the check it exists to perform could run.
`scripts/changed-func-coverage.sh` and `scripts/agent-session-watchdog.sh` use bash-4 constructs too;
`scripts/agent-session-watchdog.sh` goes further and hard-codes `#!/opt/homebrew/bin/bash` as its
shebang.

**And the guard is one file wide.** `scripts/changed-func-coverage-test.sh` actively rejects
`mapfile` — but only in `scripts/changed-func-coverage.sh`. The review on `f7e4a415f` recorded this:
"it guards only changed-func-coverage.sh, so nothing was watching this file".

**A second measured oddity.** `scripts/changed-func-coverage-test.sh` runs inside `make script-tests`,
which `make fast` and `make full` both reach — but the script it tests runs only from
`make coverage-changed`, which neither gate calls. The self-test is in the inner loop and the subject
is not. Whatever else happens, do not leave that inversion in place.

Go reads a coverage profile with `golang.org/x/tools/cover` and resolves a function boundary with
`go/ast`. Both are what this shell is approximating with `awk` and text matching.

## Scope

- The five scripts above.
- A new Go program under `tools/`, or one program with subcommands for the per-tree thresholds.
- The `coverage-gates`, `coverage-changed`, `script-tests` and `test-codex-l012` targets in the
  `Makefile`.
- The threshold rules `scripts/coverage-gate.sh` states in its header: 95.0% for the spec-named core
  subsystems and 90.0% for all other `internal/**` packages, with
  `docs/foundation/project-level/testing.md` §"Coverage numbers" as the authoritative package list.
- `tools/lintreport` and `tools/testreport` are the shape precedent for a Go tool in this repo.

## Done when

1. A Go program under `tools/` enforces the package thresholds, and a second entry point (or
   subcommand) produces the changed-function report.
2. The `coverage-gates`, `coverage-changed` and `test-codex-l012` targets call it; `script-tests`
   no longer names a converted self-test.
3. All five `.sh` files are gone.
4. The threshold list is data in one place, and a Go test asserts it agrees with the package list in
   `testing.md` — so the two cannot drift the way the script's header warns they can.
5. A Go test proves the gate **fails** for a package synthesized below its threshold, at both the
   95% tier and the 90% tier, and passes at exactly the threshold. A gate with only passing cases is
   the failure this task removes.
6. Nothing in `scripts/` or `tools/` requires bash 4, and no file hard-codes `/opt/homebrew`. Prove
   it with a grep in the commit body over `declare -A`, `mapfile`, `readarray` and `/opt/homebrew`.
7. The inversion is resolved: the changed-function report and its test are either both in a gate
   target or both out of one. State which, and why, in the commit body.
8. `make fast` and `make full` are green.

## Limits

- **Do not change any coverage threshold, and do not change which packages sit in the 95% tier.**
  Converting the implementation must not move a number. If the Go reader computes a slightly
  different percentage than the `awk` did, report the difference in the commit body and keep the
  threshold — do not lower a threshold to make a package pass.
- **Do not put the coverage gates into `make full`.** The `Makefile` labels `coverage-gates` a trend
  measure and explicitly not part of `make full`. Resolving the inversion in item 7 means fixing the
  self-test placement, not promoting the gate.
- **Do not widen `tools/lintreport/allow.txt`.**
- **Do not convert and delete in one landing if that leaves a coverage target unrunnable mid-way.**
- **Put it under `tools/`, not in `cmd/harmonik`** — W4 is shrinking `package main`.
- `scripts/agent-session-watchdog.sh` is named here only as evidence of the hard-coded-shebang
  pattern. It has no caller, and the dead-shell sweep already deletes it on its own branch — do not
  convert it and do not delete it here.
