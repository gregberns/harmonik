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
commit). Validation — format/lint gates, secret scan, and commit-message
trailers — now runs via the agent-driven validation command rather than a
pre-commit/pre-push/commit-msg hook. The underlying scripts
(`scripts/validate-commit-msg.sh`, `scripts/secret-scan.sh`) remain callable
directly.

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
| `make core` | The hard gate — "does the build work" | `gate-static` plus the core package set (the `CHARTER.md` §3 pipeline: config, branching, event bus, queue, bead-ledger adapter, worktrees, harness registry, work loop, merge) |
| `make full` | The merge decision, and what CI runs | Everything in `fast` over EVERY package, the whole-tree lint allow list, the scenario tier, and module hygiene (`go mod tidy` diff, `tools/forbid-import`, `govulncheck`) |

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
and the checks run through the agent-driven validation command instead.

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
