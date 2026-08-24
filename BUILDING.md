# Building Harmonik

## Prerequisites

- Go 1.25+
- git

Dev tools (gofumpt, gci, golangci-lint, govulncheck) are installed locally via `make tools`; no global installs required.

## First-time setup

```sh
git clone https://github.com/gregberns/harmonik
cd harmonik
make bootstrap      # installs pinned dev tools
```

`make bootstrap` is the single command for a fresh clone: it runs `make tools`
(pins gofumpt, gci, golangci-lint, govulncheck into `.tools/`).

Git hooks are **retired**: lefthook was removed (it re-armed itself on every
commit). Validation — format/lint gates, the secret scan, and commit-message
trailers — runs from the gate targets rather than from a
pre-commit/pre-push/commit-msg hook. `gate-static-product`, which `make fast`,
`make full` and `make core` all reach, calls
`scripts/secret-scan.sh --head-only`, which reads the commit just made. The
gates run after the commit, when the ordinary flow leaves nothing staged, so an
index scope there would read whatever happened to be staged rather than the
change under test.
**The commit-message gate is not on that line.** Since 2026-08-23 it runs in
`make full` alone, through the `commit-msg-check` target, which calls
`scripts/commit-msg-gate.sh --head-only`. `make fast` and `make core` do not
read a commit message at all.
`make full` adds `scripts/secret-scan.sh --range`, which reads every line the
branch adds on top of the baseline named in that script and FAILS on a finding.
**That scope narrows when the baseline does not resolve.** When the baseline
commit is missing, is not an ancestor of `HEAD`, or IS `HEAD`, the scan falls
back to `HEAD^1..HEAD` and reports clean if that is clean. **Both baselines
reach `main` now**, so this narrowing does not apply there: measured 2026-08-23,
`main` == `origin/main` == `aa423dcc0`, and `git merge-base --is-ancestor`
returns 0 for the credential baseline `0743e4dec` and for the message baseline
`4102b4e2e`. This paragraph said the opposite until the merge that superseded
`main` with the integration branch carried both baselines over. A lane cut from
a point before a baseline still gets the narrow scope, and the message gate
still demotes itself to advice on such a lane (bead
`hk-commit-msg-gate-advisory-on-main-ap068`, a condition that has cleared on
`main`). **What has NOT cleared is that the message check reads one commit.**
`--head-only` reads `git rev-parse HEAD`; a push of five commits is one CI run,
so four of the five messages are never read at all — the same gap bead
`hk-254ea` records for the credential scan. See
[`docs/foundation/project-level/build-practices.md`](docs/foundation/project-level/build-practices.md)
§"Git hooks are retired" for the full statement of both gates.
`scripts/secret-scan.sh` also runs on its own with no argument. That scope is
the staged index — the right one before a commit exists, and the reason no gate
calls it that way. `scripts/commit-msg-gate.sh` with no argument does something
different: it reads every commit from the baseline forward and reports what it
finds. **No target calls it that way any more.** It read 236 commits, took 47
seconds, named 109 rejections and exited 0 regardless, so `make full` dropped
it and keeps `--head-only` alone.
To check one message before you commit it, call the validator directly:
`harmonik commit-msg validate <file>`. It exits 0 on a clean message and 1 with
every problem numbered on stderr. The rules live in the Go package
`internal/commitmsg`; the shell validator it replaced is gone.

## The check targets

> **Superseded 2026-08-08.** This section used to list four targets —
> `check-fast`, `check-short`, `check` and `check-full`. **None of the four
> exists.** The `Makefile` has no target by any of those names, so every command
> in the old table failed. The canonical description lives in
> [`docs/foundation/project-level/quality-checks.md`](docs/foundation/project-level/quality-checks.md)
> §"Two gate targets"; this table is a summary of it. Read the `Makefile` before
> you name a target here.

| Target | When to run | What it does |
|---|---|---|
| `make fast` | While you work | Format check (gofumpt + gci), `go build ./...`, `go vet ./...`, the tagged vet, the freeze greps (including the allow-list ratchet), `golangci-lint --new-from-rev=HEAD~1`, a compile of every test file, `go test -short` over `FAST_PKGS`, and last the whole-tree lint allow list |
| `make core` | The hard gate — "does the build work" | `gate-static-product` plus the core package set (the `CHARTER.md` §3 pipeline: config, branching, event bus, queue, bead-ledger adapter, worktrees, harness registry, work loop, merge) |
| `make full` | The merge decision, and what CI runs | Everything in `fast` over EVERY package, the whole-tree lint allow list, the scenario tier, and module hygiene (`go mod tidy` diff, `tools/forbid-import`, `govulncheck`) |

`make core` calls `gate-static-product`, not `gate-static`. The one difference
is `script-tests`, the self-tests for the shell scripts the gates depend on:
`gate-static-product` does not run them, by operator decision (D3=v3,
`internal/daemon/standard-bead.dot`). `make core` is the per-bead commit gate,
so that gate no longer proves the gate tooling itself fails closed. `make fast`,
`make full` and CI run `gate-static` and still run `script-tests`, so that proof
lives there.

Run `make fast` while you work (via `/check`). Run `make full` before anyone
accepts the work — it is the merge decision. `make fast` is not a merge verdict:
it tests a chosen subset, so it can be green while the tree is red.

`make fast` ends with the whole-tree lint allow list. The changed-line lint
before it matches a finding's LINE against the lines you changed, and a
whole-function linter reports at a function's declaration, which is usually
outside the hunk. Those findings used to reach the tree unseen and wait for the
next person to run `make full` (hk-dp69a, five times).

`scripts/lint-allow-ratchet.sh` keeps the allow list from absorbing the finding
instead. It fails when a pair appears in `tools/lintreport/allow.txt` that the
same list did not hold before. Removing a pair passes. It is a per-commit
ratchet and not a history-wide invariant: it catches the edit in the working
tree and the commit that makes it, and its base is one commit back. The script
header names what that leaves open.

The extra step is cheap because `--new-from-rev` does not scope the analysis:
the changed-line step already analysed every package and only filtered what it
printed, so the whole-tree run reads the linter's cache. Measured on 2026-08-11
on a box with no other gate running, the three steps this target used to have
took 333 s and the added step took 5 s, 3 s and 4 s. The whole target measured
338 s.

These were formerly wired as pre-commit / pre-push git hooks. Hooks are retired
and the checks run from the two gate targets instead.

> **No fail-open.** `make full` does no package scoping, no retry, and no
> fail-open. A timeout, an out-of-memory kill, a compile failure, or an exit code
> nothing recognises all BLOCK. `scripts/gate-fails-closed-test.sh` holds that
> property and runs inside both targets.

## Declared-done ritual (agents)

Agents MUST run `make full` before declaring any work complete. The local invocation of the reviewer skill is:

```sh
make agent-review
```

This runs the `agent-reviewer` skill against the diff from the last commit. See `docs/foundation/project-level/build-practices.md` §Agent review on every commit for the full protocol, including the required `Reviewed-By:` and `Review-Verdict:` commit trailers.

## Commit conventions

Every non-trivial commit carries a structured JSON `Review-Verdict:` trailer emitted by `agent-reviewer`. Schema and validation details: `docs/foundation/project-level/build-practices.md` §Commit conventions.

## Where to go next

- `specs/` — normative specs; the spec is always right, code must match it.
- `AGENT_INDEX.md` — master map of the knowledge base; every doc is reachable within two hops.
- `CLAUDE.md` — kerf workflow, planning conventions, and what not to do.
