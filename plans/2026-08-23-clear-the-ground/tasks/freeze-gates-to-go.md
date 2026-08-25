---
id: freeze-gates-to-go
title: Turn the fifteen structural freeze gates into one Go tool with a declared rule table
type: task
priority: 1
labels: [scripts, shell-to-go, gate, clear-the-ground]
depends_on: [dispatch-activation-guard]
blocks: []
workstream: unassigned
batch: 6
---

> **Workstream unassigned.** These shell-to-Go conversions do not belong to `PLAN.md` §W6,
> which is the `crew-cleanup` skill. Whether they form a workstream of their own is an open
> operator decision, so this field reads `unassigned` rather than carrying a number that is
> already taken. Do not invent one. This task is parked until that ruling lands.

## Problem

`make freeze-gates` runs fifteen shell scripts, 2,127 lines in total, and every one of them does the
same thing: `find` or `grep` the source tree for a shape that must not come back, print a message,
increment a hit counter, exit 1 if the counter is non-zero.

Measured 2026-08-23 from the `freeze-gates` recipe in the `Makefile`:

| script | lines |
|---|---|
| `scripts/workloop-scheduler-freeze-gate.sh` | 439 |
| `scripts/runloop-emitter-gate.sh` | 369 |
| `scripts/readywait-freeze-gate.sh` | 224 |
| `scripts/runloop-freeze-gate.sh` | 140 |
| `scripts/harnesspi-freeze-gate.sh` | 130 |
| `scripts/harnessclaude-freeze-gate.sh` | 118 |
| `scripts/runmerge-freeze-gate.sh` | 116 |
| `scripts/runlaunch-freeze-gate.sh` | 105 |
| `scripts/harnesscodex-freeze-gate.sh` | 97 |
| `scripts/crewrun-freeze-gate.sh` | 83 |
| `scripts/workersbootwire-freeze-gate.sh` | 80 |
| `scripts/projectconfig-freeze-gate.sh` | 71 |
| `scripts/queuewiring-freeze-gate.sh` | 71 |
| `scripts/transport-freeze-gate.sh` | 50 |
| `scripts/dispatch-activation-gate.sh` | 34 |

**This is gate-critical.** `freeze-gates` is a step of `gate-static-product`, which
`gate-static` calls, which `make fast` and `make full` both call — and `make core` calls
`gate-static-product` directly. Every one of the three gates runs all fifteen.

Three measured facts make shell the wrong tool here:

1. **Not one of the fifteen has a self-test.** `make script-tests` names twenty-one self-tests and
   none of them is a freeze gate. A gate that has stopped matching anything looks exactly like a
   gate that passes. This class of silent-empty failure is documented in this repo twice already —
   in the header of `scripts/go-test-must-match.sh` (a `-run` filter that matched nothing and
   reported `ok ... [no tests to run]` for many commits) and in the header of
   `scripts/secret-scan.sh` (a `grep -q` behind a pipe under `pipefail` that read a match as a
   no-match, so the private-key pattern had never matched anything in seventy-three days).
2. **The rule and the plumbing are tangled.** Each file re-implements the same `HITS=0` /
   `while IFS= read -r` / `exit $((HITS>0))` frame around one or two greps. The thing a reader needs
   — which shape is forbidden where — is a few lines buried in a hundred.
3. **The bash-version trap is live in this exact tree.** `f7e4a415f` had to replace two `mapfile`
   calls in `scripts/agent-reviewer-prompt-parity-test.sh` because `mapfile` is bash 4 and stock
   macOS ships bash 3.2, so `make fast` aborted before reaching the check. Nothing watches the
   freeze gates for the same thing.

There is already a precedent for the shape this should take: `scripts/queue-status-writer-ratchet.sh`
is seven lines that `exec go run` a Go program next to it, and `module-hygiene` runs
`go run ./tools/forbid-import ./...`.

## Scope

- The fifteen scripts listed above.
- A new Go program under `tools/`, invoked from the `freeze-gates` target in the `Makefile`.
- `scripts/gate-fails-closed-test.sh` — its structural pass expands `make -n` over the gate step
  list and refuses status-swallowing constructs. Changing the recipe changes what it reads.
- `scripts/queue-status-writer-ratchet.sh` and `scripts/queue-status-writer-ratchet.go` are the
  reference for how a Go gate is wired into this Makefile. Read them; do not change them here.

Each rule the Go tool carries names the concern it freezes, the paths it scans, the forbidden shape,
and the message it prints. Keep the explanation each script's header carries — the header of
`scripts/transport-freeze-gate.sh` explains *why* the door is closed, and that reasoning is the most
valuable thing in the file.

## Done when

1. `go run ./tools/<name>` exists and runs every rule the fifteen scripts ran.
2. The `freeze-gates` target in the `Makefile` invokes it, and names no converted script.
3. Each converted `.sh` file is gone from `scripts/`.
4. A Go test asserts, for every rule, both directions: it passes on the current tree, **and** it
   fails on a synthesized tree that contains the forbidden shape. A rule that only ever passes is
   the failure mode this task exists to remove, so a rule with no failing-direction case does not
   count as converted.
5. A Go test asserts the rule table is non-empty and that every rule has a message — so a rule
   silently dropped during conversion goes red rather than quiet.
6. `make fast` and `make full` are green, and `scripts/gate-fails-closed-test.sh` still passes
   against the rewritten recipe.

## Limits

- **Do not change what any gate enforces.** This converts how a rule is checked, never which shapes
  are forbidden. A conversion that quietly narrows a pattern is worse than the shell it replaced,
  because the gate then reports green for a door that is open. For each rule, record in the commit
  body the shell pattern and the Go pattern side by side.
- **Do not widen `tools/lintreport/allow.txt`.** New Go files must pass the linter on their own.
- **Do not convert and delete in one landing if that leaves `freeze-gates` unrunnable part-way.**
  Convert in slices if you like — but every commit must leave `make fast` green and every rule
  covered by exactly one implementation. Never two, and never zero.
- **Put the new code under `tools/`, not in `cmd/harmonik`.** W4 exists to shrink `package main`;
  adding a gate to it works against that.
- Do not fold in `scripts/required-check-name-gate.sh`, `scripts/pipefail-grepq-gate.sh`,
  `scripts/comment-only-commit-gate.sh` or `scripts/lint-allow-ratchet.sh`. They are in the same
  Makefile target but they read different inputs — CI configuration, shell source, a commit diff and
  a lint report — and each is its own task or belongs to W1.
