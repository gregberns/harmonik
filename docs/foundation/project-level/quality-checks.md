# Quality Checks

> Go 1.25 MVH. Agent-coded: Claude Code sessions write the implementation. The single invariant: **an agent must not be able to land low-quality or rule-violating code that CI lets through.** Every gate is deterministic and machine-enforceable, and every CI gate is ALSO runnable locally under the same make-target name.

## Decisions

- Language/toolchain: **Go 1.25** pinned via `go.mod` `toolchain go1.25.x`.
- Formatter: **`gofumpt`** (superset of `gofmt`).
- Imports: **`gci`** with three groups (stdlib, third-party, `github.com/gregberns/harmonik`).
- Meta-linter: **`golangci-lint` v2.3+** (config uses `version: 2` schema explicitly; migrated March 2025 GA). Config at repo root `.golangci.yml`.
- Hook manager: **`lefthook`** (Go-native, single binary, no Python/Node dep).
- Enforcement: pre-commit hooks + **`agent-reviewer` on every non-trivial commit** (per `build-practices.md`) + **post-push CI status checks on `main`** via GitHub branch protection. No PR-based merge gate at MVH (direct-to-main); CI failures fix-forward.
- Tests: `go test ./... -race -count=1` required in CI; short subset pre-commit.
- **Local/CI parity:** every CI check is equivalently the local `make check-fast` / `make check` / `make check-full` target. See §Three-tier identical gauntlet.

## Formatter

- `gofumpt -l -w .` pre-commit on staged files; CI re-checks `gofumpt -l -d .` (fails on any diff).
- `gofumpt` chosen over `gofmt` because agents emit gofmt-valid-but-noisy code (redundant parens, multi-line literals that fit one line); gofumpt catches this deterministically.
- Imports: `gci write -s standard -s default -s 'prefix(github.com/gregberns/harmonik)' .` — pre-commit + CI.

## Vet and static analysis

- `go vet ./...` pre-commit and CI.
- `govet` via golangci-lint with `enable-all: true`, disable `fieldalignment` (noisy) and `shadow` (false positives). Keep `nilness`, `unusedresult`, `structtag`, `copylocks`, `printf`.
- `staticcheck` via golangci-lint: all `SA*` (correctness), `ST1000`/`ST1005`, all `S1*`. Disable `ST1003` (naming conflicts).

## Linter meta-runner

`.golangci.yml` — explicit enabled list, `disable-all: true`:

```yaml
version: "2"
run:
  timeout: 5m
  go: "1.25"
linters:
  disable-all: true
  enable:
    # Correctness
    - errcheck        # unhandled errors — CRITICAL for agent-coded
    - govet           # stdlib correctness analyzers
    - staticcheck     # SA/ST/S correctness + simplification
    - ineffassign     # writes with no read
    - unused          # dead code (replaces deadcode/structcheck/varcheck)
    - gosimple        # subsumed by staticcheck but explicit is clearer
    - errorlint       # %w wrapping + errors.Is/As correctness
    - nilerr          # returning nil when err is non-nil
    - copyloopvar     # Go 1.22+ loop var capture (redundant now, belt+suspenders)
    - testifylint     # testify misuse (wrong assertion fn, missing compare, etc.)
    - errchkjson      # ignored JSON marshal/unmarshal errors
    - exhaustive      # missing enum cases in switch statements
    # Resource / context
    - bodyclose       # http.Response.Body leaks
    - rowserrcheck    # sql.Rows.Err()
    - sqlclosecheck   # sql.Rows/Stmt close
    - contextcheck    # context.Context propagation
    - noctx           # http requests without context
    - containedctx    # context.Context stored in structs (ban)
    - fatcontext      # context.Context leaks in loops
    # Style / diagnostics
    - gocritic        # diagnostic + style bundle
    - revive          # replacement for golint; minimal ruleset
    - misspell        # typos in comments/strings
    - unparam         # always-same param values (catches dead branches)
    - unconvert       # unnecessary type conversions
    - prealloc        # slice prealloc hints
    - nakedret        # naked returns > 20 lines
    - nolintlint      # nolint directives must be justified
    - gosec           # security smells (advisory; see below)
    - forbidigo       # ban specific calls (see config)
    - depguard        # import-graph + component-layer rules (subsystem-organization.md)
linters-settings:
  errcheck: { check-type-assertions: true, check-blank: true, exclude-functions: ["(io.Closer).Close"] }
  revive:   { rules: [{name: exported}, {name: package-comments}, {name: var-naming}, {name: error-return}, {name: error-naming}, {name: if-return}] }
  gocritic: { enabled-tags: [diagnostic, performance, style], disabled-checks: [hugeParam, rangeValCopy] }
  nolintlint: { require-explanation: true, require-specific: true, allow-unused: false }
  forbidigo:
    forbid:
      - { p: '^fmt\.Print.*$', msg: "use the structured logger" }
      - { p: '^panic$',         msg: "return an error; panics only in main/init" }
  depguard:
    # Component-graph rules (subsystem layering) live here per subsystem-organization.md §Dependency layering enforcement.
    # One `rules.<name>` entry per component; `files:` scopes the rule, `allow:`/`deny:` set the allowed edges.
    # Full matrix is in subsystem-organization.md. Example stub:
    rules:
      core:
        files: ["**/internal/core/**"]
        deny:
          - { pkg: "github.com/gregberns/harmonik/internal/", desc: "core is a leaf; no subsystem imports" }
      # ... one rule per component (eventbus, policy, handler-contract, workspace, orchestrator, daemon, cmd, etc.)
  exhaustive:
    default-signifies-exhaustive: true
  testifylint:
    enable-all: true
issues: { max-issues-per-linter: 0, max-same-issues: 0, exclude-use-default: false }
```

Explicit **NO** on: `wsl`, `lll`, `funlen`, `gocyclo`, `cyclop`, `godox`, `tagliatelle`, `exhaustruct`, `gochecknoglobals`, `gochecknoinits`, `varnamelen`, `wrapcheck`, `nlreturn`, `goimports` (superseded by `gci`). Style-taste linters; noise without catching real defects at MVH scope.

## Error handling conventions

- **Always handle errors.** `errcheck` is blocking; only ignore with explicit `_ =` + `//nolint:errcheck // <reason>` (nolintlint forces justification).
- **Prefer `%w` wrapping at subsystem boundaries** (crossing S01..S09). `errorlint` enforces correctness WHEN wrapping (e.g., non-`%w` for an error arg, direct `==` comparison where `errors.Is` is required), but it CANNOT detect missing-wraps — that needs semantic boundary knowledge, which is a custom `go/analysis` pass (deferred). Until that analyzer ships: reviewer-agents flag missing-wraps on subsystem-boundary imports during review. Do NOT wrap within a subsystem; wrapping the same error up-and-up produces noise without new context.
- **Sentinel errors** as `var ErrFoo = errors.New("foo")`; typed errors as structs with `Error()`.
- **No `panic` in production paths** — `forbidigo` blocks it outside `main`/`init`. Run supervisor handles recovery.
- **`defer x.Close()` is acceptable** without error check (errcheck exclusion). When a close-error is material (commit, fsync) use named return + `defer func() { err = errors.Join(err, x.Close()) }()`.

## Logging

- **`log/slog`** is the structured logger. Emits the project's JSON schema to stdout (the daemon redirects to `.harmonik/logs/daemon.log`).
- `fmt.Print*`, `log.Print*` banned outside `main` and test code (`forbidigo` enforces).
- Subsystem loggers carry subsystem name + `run_id` (when applicable) as default attributes via `slog.With(...)`.
- Error-level logs include stack trace via `slog.Any("stack", debug.Stack())` for panic recovery paths.

## Three-tier identical gauntlet — local and CI run the same commands

**User-endorsed invariant (2026-04-24):** every check an agent needs to declare work complete MUST be executable locally. No CI-only checks. Agents run nearly everything all the time; CI is a mirror, not a moat.

`lefthook.yml` at repo root wires pre-commit / pre-push to the tiers below. Every tier is a `make` target; CI jobs invoke the same make targets — no CI-specific scripting.

**Tier 1 — `make check-fast` (<15s target).** Author-iteration speed. Pre-commit hook runs this subset on staged files:

- `gofumpt -l -d` / `gci diff` on STAGED files.
- `go vet ./...`, `go build ./...`.
- `golangci-lint run --new-from-rev=HEAD~1` (delta only).
- `go test -short` on packages with changed files.

**Tier 2 — `make check` (~3–5 min target).** Default pre-push + work-in-progress verification. Full linter + unit + property:

- Everything in Tier 1 on the full tree (not delta).
- `golangci-lint run` full tree (includes all component-graph `depguard` rules).
- `go test -race -count=1 ./...` (unit + property, `-short` off).
- `go mod tidy` diff check (`go.sum` cleanliness; fails if `go mod tidy` would change files).
- Coverage gate (`scripts/coverage-gate.sh`).
- Import allowlist linter (`tools/go-linters/forbid-import`).
- `govulncheck ./...`.

**Tier 3 — `make check-full` (~10–15 min target).** **Agent declared-done MUST pass this.** Everything in Tier 2 plus:

- `go test -race -tags=integration ./...`.
- `go test -race -tags=scenario ./test/scenario/...`.
- `go test -tags=crash ./test/crash/...` (fast subset).

CI runs **the same make targets** — no CI-only logic. An agent's local `make check-full` pass is a direct predictor of CI pass. If local passes but CI fails: environment drift; treat as a bug in setup, not a CI-specific behavior.

**Excluded from agent done-check** (CI-nightly or on-demand only, per `testing.md`): budget-capped real-agent smoke tests (Tier C), full property-test seeds (`HARMONIK_RAPID_SEED=auto` 10k iterations), full fault-injection site set, `govulncheck` weekly deep scan.

**The rule that makes this work:** agents MUST run `make check-full` before declaring work complete. Enforcement lives in `agent-configuration.md` (session-end ritual names it; `SESSION_HANDOFF.md` writeup records the outcome).

## Commit-blocking vs advisory

Framed as tiers, not "pre-commit vs CI":

- **Tier 2-blocking** (every push; required to merge): all formatter/import checks, `go vet`, every enabled golangci-lint linter (including `depguard` component-graph rules), `go build`, `go test -race` unit+property, `go mod tidy` cleanliness, coverage gate, `forbid-import`, `gosec` high-severity.
- **Tier 3-blocking** (declared-done; required to merge): integration, scenario, fast-crash suites.
- **Advisory (posts, does not block):** `gosec` medium/low, `prealloc` (`//nolint:prealloc` with justification allowed). `govulncheck` is blocking at Tier 2 for known-high CVEs; weekly deep scan is advisory. Promotion is a one-line `.golangci.yml` edit.

## Agent-enforceability

Threat model: agent runs `git commit --no-verify` or writes `//nolint:all` to escape local hooks. Counter-pattern:

1. **GitHub branch protection on `main` with required status checks on pushes.** Pushes that fail CI's `make check-full` are marked red but (MVH direction, per `build-practices.md`) are not rejected — the agent fixes forward with a corrective commit. Branch protection prevents force-push and admin-bypass. Not a merge gate (no PRs at MVH), but the same commands that run locally also run remotely; divergence surfaces as a red main.
2. **`nolintlint` blocks bulk suppression.** Every `//nolint` must name specific linters + carry an explanation + suppress something real. `//nolint:all` fails lint.
3. **CI posts nolint-density delta on every push** (`git diff HEAD~1 | grep -c //nolint`). Agent-driven spikes are visible without manual diff reading.
4. **No admin-bypass for branch protection** on `main`. Solo dev is not exempted; rule changes require an explicit config edit.
5. **Protected rule files.** The files that define the gates themselves are protected; edits MUST trigger dedicated attention:

   - `.golangci.yml`, `.depguard.yml` (if factored out)
   - `tools/go-linters/forbid-import.go` (library allowlist)
   - `scripts/coverage-gate.sh`
   - `.github/workflows/*.yml`
   - `Makefile` (the `check-*` targets)
   - `.lefthook.yml`
   - `CONSTITUTION.md` — additionally requires a `Constitution-Edit-Approved-By: <name-or-email>` commit trailer on any edit (per `agent-configuration.md §CONSTITUTION.md`); commits lacking the trailer are rejected by pre-commit hook and flagged red by post-commit CI.

   A commit that touches any of these MUST:
   1. Cite a kerf-codename in the commit body explaining the rule change (so edit motivation is traceable).
   2. Trigger a `rule-change` CI check that watches these file paths and surfaces the rule-file diff on the commit summary.
   3. NOT bundle rule changes with unrelated code changes — one commit = one concern (rule change OR code), so the rule-change diff is reviewable on its own merits by the post-commit agent-reviewer and the async human reader.

   Prevents "agent relaxes the gate, then passes its own gate." Rule-change commits are distinguishable from ordinary code commits; silent rule weakening becomes impossible.

**No PR-based gating at MVH.** Enforcement relies on: (a) the agent-declared-done ritual running `make check-full` locally before commit; (b) `agent-reviewer` running on every non-trivial commit (per `build-practices.md §Agent review on every commit`); (c) post-push CI re-running the same gauntlet and surfacing failure as a red main. Fix-forward is the recovery, not a block-on-red.

Invariant: **CI mirrors local `make check-full`; local pass predicts CI pass; rule weakening requires a single-concern commit that trips the rule-change surface.**

## ⚑ Assumptions worth user's eye

1. **⚑ `gofumpt` over `gofmt`** — Stricter; safe for solo-dev, flag if external contributions open up (PR friction).
2. **⚑ `depguard` v2 handles component-graph rules natively.** Previously proposed `go-arch-lint`; dropped per reviewer convergence. `.go-arch-lint.yml` removed; component-graph rules live in `.golangci.yml`'s `depguard` settings. Durable only if subsystem package layout is stable; update when the 10-component foundation lands as code.
3. **⚑ Branch protection without PR-based gating.** Solo-dev MVH: no PRs, no approval requirement. Branch protection prevents force-push + admin-bypass + requires status checks but cannot reject pushes outright. Revisit when PRs return (product has real users or multi-human team).
4. **⚑ `gosec` advisory, not blocking** — Elevate when the daemon handles secrets or opens network ports.
5. **⚑ No `wrapcheck`** — Omitted deliberately; forces wrapping at every package boundary, conflicting with "wrap only at subsystem boundaries." `errorlint` + review cover the real cases.
6. **⚑ Pre-commit 15s budget** — Split to `pre-commit` (format+vet) + `pre-push` (lint+test-short) if it passes 30s.
7. **⚑ Go 1.25 assumed.** Access to `log/slog` / `testing/synctest` / `os.Root` / `go.mod tool` depends on it. If bootstrap pins older Go, revise this doc.

## Deferred / follow-up

- Mutation testing (`gremlins`) — post-MVH once unit suite is substantive.
- Coverage gate — once testing methodology codifies targets; enforce via `go-test-coverage` in CI.
- Benchmark regression gate — `benchstat` diff once any hot path exists.
- `govulncheck` blocking — promote from advisory once a CVE-triage process is defined.
- **Custom analyzer for four-axis determinism tags** (architecture §1.1) — `go/analysis` pass verifying LLM-freedom / I/O / replay / idempotency tags on cross-subsystem types. Natural fit post-MVH.
- Supply-chain pinning — Dependabot for MVH; SLSA/sigstore later.
