---
description: Validate the code you just committed. `make fast` while you work, `make full` before anyone accepts it.
---

# /check — validation gate

Git hooks are out. Validation is agent-driven, and this command is the replacement: run it after every non-trivial `git commit`.

There are two targets. There is no third.

## While you work

```bash
make fast
```

`make fast` runs the format check, `go build ./...`, `go vet ./...`, the tagged vet, the subsystem freeze greps, the changed-line lint (`golangci-lint --new-from-rev=HEAD~1`), a compile of every `_test.go` file in the repo, and `go test -short` over the major packages — `internal/core`, `internal/daemon`, `internal/queue`, `internal/queuewiring`, `internal/brcli`, `internal/eventbus`, `internal/runloop`, `internal/workflow` and `cmd/harmonik`. The package list is in the Makefile as `FAST_PKGS`, with the reason for each one.

It is not a merge verdict. It tests a chosen subset, so it can be green while the tree is red.

## Before anyone accepts the work

```bash
make full
```

`make full` is the merge decision, and it is what CI runs. Everything `make fast` does, over EVERY package, plus the whole-tree lint ceiling, the tagged scenario tier, the crash tier and the module hygiene checks (`go mod tidy`, forbidden imports, `govulncheck`).

No package scoping. No retry. No fail-open. A timeout, an out-of-memory kill, a compile failure or an exit code nothing recognises all BLOCK. `scripts/gate-fails-closed-test.sh` holds that property and runs inside both targets.

## Reading the result

- **Green (exit 0):** done.
- **Red (non-zero):** fix the root cause, then re-commit (amend or a follow-up commit). Do not suppress the finding, do not lower the gate, do not `--no-verify`. Re-run until green.

`make full` is red on this tree today. `internal/daemon`, `cmd/harmonik` and `internal/keeper` all fail real tests under `go test -short`. That is the gate working. A gate that reports green on a tree with known failures is the defect this one replaced.

## Lanes that are not the gate

None of these can block a merge, and none of them is a third tier:

```bash
make test-race-nightly   # -race over everything, no -short; the nightly CI lane
make test-integration    # the integration-tagged tier; needs tmux and a live environment
make coverage-gates      # the two coverage ratchets; a trend measure, not a verdict
```
