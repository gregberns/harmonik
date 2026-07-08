# Building Harmonik

## Prerequisites

- Go 1.25+
- git

Dev tools (gofumpt, gci, golangci-lint, govulncheck, lefthook) are installed locally via `make tools`; no global installs required.

## First-time setup

```sh
git clone https://github.com/gregberns/harmonik
cd harmonik
make bootstrap      # installs pinned dev tools + wires git hooks
```

`make bootstrap` is the single command for a fresh clone: it runs `make tools`
(pinned gofumpt, gci, golangci-lint, govulncheck, lefthook into `.tools/`) then
`make install-hooks` (wires `lefthook.yml` pre-commit / pre-push / commit-msg
hooks into `.git/hooks/`).

To re-run either step independently:

```sh
make tools          # re-pin tools only (skips hook re-registration)
make install-hooks  # re-wire hooks only (requires .tools/lefthook to exist)
```

## The three-tier check gauntlet

| Target | When to run | What it does |
|---|---|---|
| `make check-fast` | During authoring, on every save cycle | gofumpt + gci diff, go vet, go build, golangci-lint --new-from-rev, go test -short on changed packages (<15s target) |
| `make check` | Default; pre-push | Full golangci-lint, go test -race, go mod tidy check, coverage gate, govulncheck (~3–5 min) |
| `make check-full` | Before declaring work done | Everything in `check` + integration + scenario + crash test suites (~10–15 min) |

The pre-commit hook runs `check-fast` on staged files. The pre-push hook runs `check`.

## Declared-done ritual (agents)

Agents MUST run `make check-full` before declaring any work complete. The local invocation of the reviewer skill is:

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
