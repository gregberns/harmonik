# Quality Checks

> Go 1.25. Agent-coded: Claude Code sessions write the implementation. The single invariant: **an agent must not be able to land low-quality or rule-violating code that CI lets through.** Every gate is deterministic and machine-enforceable, and every CI gate is ALSO runnable locally under the same make-target name.

## Decisions

- Language/toolchain: **Go 1.25** pinned via `go.mod` `toolchain go1.25.x`.
- Formatter: **`gofumpt`** (superset of `gofmt`).
- Imports: **`gci`** with three groups (stdlib, third-party, `github.com/gregberns/harmonik`).
- Meta-linter: **`golangci-lint` v2.3+** (config uses `version: 2` schema explicitly; migrated March 2025 GA). Config at repo root `.golangci.yml`.
- Hook manager: **`lefthook`** (Go-native, single binary, no Python/Node dep).
- Enforcement: **`agent-reviewer` on every non-trivial commit** (per `build-practices.md`) + **post-push CI status checks**. CI runs on every branch, so it covers the integration branch. Fix a red integration branch forward. The one merge gate is the integration→`main` pull request. Branch model: `build-practices.md` §"Branch model — land on the integration branch".
- Tests: `go test ./... -race -count=1` required in CI; short subset pre-commit.
- **Local/CI parity:** CI runs `make full`, the same target a developer runs. See §Two gate targets.

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
  errcheck: { check-type-assertions: true, check-blank: true }  # no close exclusions — production Close() is errcheck-gated (P2 hk-8dtiv)
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

> **The block above is an illustrative excerpt, not a mirror.** `.golangci.yml` in the repo root is the authority and has moved on from it (v2 schema, an `exclusions.rules` block, path-scoped `forbidigo` entries, the complexity linters below). Read the file; do not treat this excerpt as the enabled set.

**Complexity ceilings are ENABLED** (Track C), contrary to earlier drafts of this section that listed them under "explicit NO":

- `funlen` — 100 lines / 60 statements, `ignore-comments: true`.
- `cyclop` — `max-complexity: 15` per function; `package-average: 0` (package averaging disabled).
- `gocognit` — `min-complexity: 20` (stricter than the golangci default of 30).

They ratchet via `--new-from-rev`, so existing functions are grandfathered but a function a diff rewrites is not. Excluded paths: `_test.go`, `internal/scenario/`, `internal/specaudit/`.

⚠ **Test code therefore has no complexity ceiling of any kind** — 488k lines of it accumulated with 1,035 functions over the 100-line `funlen` limit. Removing `_test.go` from the exclusion was attempted 2026-07-27 and **reverted**: validated against `--new-from-rev=HEAD~1` it reported clean, but the merge-blocking gate then compared against `origin/main` rather than `HEAD~1`, where the change took this branch from 163 to 181 findings — 13 `gocognit`, 4 `cyclop`, 1 `funlen`, several in ordinary table-driven tests rather than bead-named files. Deferred to `hk-csmfe` and sequenced **after** the 885-file bead-named-test deletion, since most of those 18 live in code that deletion removes. **Validate any retry at `origin/main`, never at `HEAD~1`.**

Declaration-line anchoring also grandfathers a function only while its declaration line is untouched — a rename or signature change re-anchors it.

⚠ **Known hole in the ratchet:** a function that was already over the ceiling when written never reports at all, and an explicit `//nolint` defeats it entirely. `beadRunOne` was born at 119 lines, is 2,289 today, and has never produced a finding — it also carries `//nolint:funlen,gocognit,cyclop`. Tracked as `hk-csmfe`.

Explicit **NO** on: `wsl`, `lll`, `gocyclo` (superseded by `cyclop`), `godox`, `tagliatelle`, `exhaustruct`, `gochecknoglobals`, `gochecknoinits`, `varnamelen`, `wrapcheck`, `nlreturn`, `goimports` (superseded by `gci`). Style-taste linters; noise without catching real defects.

**Path-scoped exclusions worth knowing** (`.golangci.yml §exclusions.rules`): `tools/` is excluded from the *whole* `forbidigo` and `noctx` linters; `internal/testhelpers/` from the whole `forbidigo` linter — so both get `panic` **and** `fmt.Print*` for free, not just one of them. `cmd/` gets a narrower third carve-out: only the `fmt.Print*` ban (printing to stdout is what a CLI does), so `panic` stays banned there.

## Error handling conventions

> **Source of truth for Go idioms:** `.claude/skills/agent-reviewer/SKILL.md §2 — Idiom compliance`, which is itself a description of the enforced `.golangci.yml`. Where this section and §2 disagree, §2 wins; where §2 and `.golangci.yml` disagree, the config wins and the prose is the bug. Never document an idiom the linter rejects — verify by running the pinned `.tools/golangci-lint` against the repo's own settings block.

- **Always handle errors.** `errcheck` is blocking and runs with `check-blank: true`, so `_ = f()` is **itself a finding**, not an escape hatch. Discarding an error requires an explicit `//nolint:errcheck // <reason>` (nolintlint forces justification), and the quality lanes operate under *add no new `//nolint`* — reach for one only when no code change clears the finding. See `agent-reviewer §2 — Suppression discipline`.
- **Comma-ok on every type assertion** — `v, ok := x.(T)`. `check-type-assertions: true` makes a bare `x.(T)` and `v, _ := x.(T)` findings.
- **Prefer `%w` wrapping at subsystem boundaries** (crossing S01..S09). `errorlint` enforces correctness WHEN wrapping (e.g., non-`%w` for an error arg, direct `==` comparison where `errors.Is` is required), but it CANNOT detect missing-wraps — that needs semantic boundary knowledge, which is a custom `go/analysis` pass (deferred). Until that analyzer ships: reviewer-agents flag missing-wraps on subsystem-boundary imports during review. Do NOT wrap within a subsystem; wrapping the same error up-and-up produces noise without new context.
- **Sentinel errors** as `var ErrFoo = errors.New("foo")`; typed errors as structs with `Error()`.
- **No `panic` in production paths** — `forbidigo` blocks it outside `main`/`init`. Run supervisor handles recovery.
- **Deferred `Close()` — errcheck gates it; the reviewer owns materiality.** `.golangci.yml` sets `errcheck: { check-blank: true }` with **no** close exclusions. The four `(io.Closer|*os.File|net.Conn|net.Listener).Close` `exclude-functions` entries that used to live here were dropped in P2 (hk-8dtiv) once the whole production tree was migrated to the house Close idiom. Every unchecked production close is now an errcheck finding, in both the `defer f.Close()` and the `defer func() { _ = f.Close() }()` form (`_ =` does not satisfy `check-blank`). Test noise is held at zero by one `_test.go`-scoped `exclusions.rules` entry — the same `text:`-plus-`path:` machinery the `SC6-DRIVER-CLOCKPORT` rules use, matching the finding text `Close` is not checked` on `_test.go` paths — so `*_test.go` closes stay silent while 100% of production closes are checked.

  ```go
  defer f.Close()                  // errcheck finding on production code
  defer func() { _ = f.Close() }() // likewise — check-blank: true
  ```

  errcheck gates the **presence** of a check, not its **correctness**: the finding text carries only the receiver name and `(*os.File).Close` is one method whether the file was opened for read or write, so the linter cannot tell a read close from a write close. **A close handled the wrong way — swallowed on a write/commit/fsync path, or closed in a `defer` that runs after the rename in a temp+rename sequence — can still be lint-green, and is still a defect the reviewer catches.** `cmd/harmonik/handler.go` `atomicWriteHandlerState` is the reference shape: it checks the close *before* the rename. Read-vs-write is expressible by a custom `go/analysis` pass that tracks a file's open flags forward to its close; nobody has written it — a cost, not an impossibility, the same shape as the deferred missing-wrap analyzer noted above.

  Use one of the three forms landed in this tree. Pick by whether the close error is material; each is cited to its real home:

  ```go
  // MATERIAL (write / commit / fsync) — join it into a named return.
  // internal/queue/cli/cancel.go (emitQueueCancelEvent) — deferred, verbatim.
  // internal/supervise/daemon_watchdog.go (openCrashLog) — same fold written
  // out non-deferred, because it runs on one early-return path only.
  defer func() { err = errors.Join(err, f.Close()) }()

  // MATERIAL but must not mask an earlier failure — first error wins.
  // internal/keeper/watcher.go (FileEmitter.EmitWithRunID), commit 5a199ed3
  defer func() {
      if closeErr := file.Close(); closeErr != nil && err == nil {
          err = closeErr
      }
  }()

  // IMMATERIAL (read-only open) but observable — log and continue.
  // WarnContext, not Warn: noctx reports "log/slog.Warn must not be called.
  // use log/slog.WarnContext". A defer usually has no ctx in scope — pass
  // context.Background(), as internal/keeper/tmuxresolve.go
  // (recentTranscriptTurn) already does for its scan-truncation warning.
  defer func() {
      if closeErr := f.Close(); closeErr != nil {
          slog.WarnContext(ctx, "keeper: close transcript", "err", closeErr, "path", path)
      }
  }()
  ```

  Note `internal/keeper` carries **no** `errors.Join` close — do not cite it for the first form. Its two third-form homes, `heartbeat.go` (`deriveContextTokens`) and `tmuxresolve.go` (`recentTranscriptTurn`), still call bare `slog.Warn` inside the defer; `--new-from-rev` grandfathers them, but the same lines in a new diff are a `noctx` finding. Copy the block above, not those call sites.

  When you need to open the file too, prefer `os.OpenRoot(dir)` + `root.Open`/`root.Create(name)` over `os.Open`/`os.Create` with a constructed path: the rooted form clears gosec **G304** by construction (verified against the pinned linter with this repo's settings block), where the plain form fires it and tempts a `//nolint`. That is a rule for **new** code — nothing in this tree uses `os.OpenRoot` yet, and both first-form exemplars above open under a justified `//nolint:gosec` because their paths are operator-supplied at runtime, so G304 fires however they are validated. They are cited for the close, not for the open. Full treatment, including which suppressions are legitimate: `agent-reviewer §2 — Deferred Close()`.

## Logging

- **`log/slog`** is the structured logger. Emits the project's JSON schema to stdout (the daemon redirects to `.harmonik/logs/daemon.log`).
- `fmt.Print*`, `log.Print*` banned outside `main` and test code (`forbidigo` enforces).
- Subsystem loggers carry subsystem name + `run_id` (when applicable) as default attributes via `slog.With(...)`.
- Error-level logs include stack trace via `slog.Any("stack", debug.Stack())` for panic recovery paths.

## Two gate targets — local and CI run the same commands

**User-endorsed invariant (2026-04-24):** every check an agent needs to declare work complete MUST be executable locally. No CI-only checks. Agents run nearly everything all the time; CI is a mirror, not a moat.

**Superseded 2026-08-03.** This section used to describe three tiers, `make check-fast` / `make check` / `make check-full`, plus `check-short`, `check-report`, `check-race-full` and `check-verdict` — seven names for one question. There are now two targets. Git hooks are retired; validation is agent-driven through `/check`.

**`make fast` — the inner loop.** Run it while you work.

- The format check (`gofumpt` + `gci`, fail-closed), `go build ./...`, `go vet ./...`, and the tagged vet.
- The subsystem freeze greps (`make freeze-gates`).
- `golangci-lint run --new-from-rev=HEAD~1` — the lines this commit changed.
- `go test -run='^$' ./...` — compiles every `_test.go` file and runs none. `go build` does not compile test files.
- `go test -short` over the major packages. The list is `FAST_PKGS` in the Makefile, with the reason for each package next to it.

`make fast` is not a merge verdict. It tests a chosen subset, so it can be green while the tree is red.

**`make full` — the merge decision.** It is what CI runs, and it is what an agent runs before declaring work complete.

- Everything in `make fast`, over EVERY package: `go test -short -count=1 ./...`.
- The whole-tree lint, judged against an allow list (`make lint-allow`).
- The tagged scenario tier (`make test-scenario`) and the crash tier.
- Module hygiene: `go mod tidy` diff check, the import allowlist (`tools/forbid-import`), `govulncheck ./...`.

No package scoping. No retry. No fail-open. A timeout, an out-of-memory kill, a compile failure or an exit code nothing recognises all BLOCK. `scripts/gate-fails-closed-test.sh` holds that property, and it runs inside both targets.

**The lint allow list.** A bare `golangci-lint run` over this tree reports more than a thousand findings, so it exits non-zero on every commit and cannot be a verdict. The allow list at `tools/lintreport/allow.txt` grandfathers what is already here and refuses what a change adds. It names each tolerated pair of file and linter on its own line, tab-separated:

```
internal/daemon/workloop.go	errcheck
```

- A finding whose file-and-linter pair is on the list passes.
- A finding whose pair is NOT on the list fails `make full`. Fix the findings you added. To see only those, run `.tools/golangci-lint run --new-from-rev=HEAD~1` — the same check `make fast` runs.
- You clean a file, you delete its line. Commit that deletion with the fix. That file can never bring the finding back, so the ground a fix wins is never given back.

Adding a line to grandfather a NEW finding is the one repair that is not allowed. The list is keyed on file and linter rather than on file and line because line numbers rot within days. A list keyed by line would churn on every unrelated edit, and a list nobody keeps current is a list people delete.

**Superseded 2026-08-03 — why a list and not a count.** This step used to compare one number against a committed ceiling in `scripts/lint-ceiling.baseline`, and it could only ratchet down. A count is a weak verdict for three reasons. It falls just as readily when somebody SILENCES a finding as when somebody FIXES one. It cannot tell "fixed two in the daemon, added two in the queue" from "nothing changed". And it never says WHERE the debt sits, so it gives a reader no way to aim.

**The run reports the debt out loud.** Every run prints what it is tolerating, broken down by package and by linter, including a run that passes. "No new lint findings" must never be readable as "this tree is clean". Measured 2026-08-03, the tree carries **1,187 findings across 636 file-and-linter pairs**. **615 of them are in `internal/daemon`** — the package the current program is decomposing. Next largest are `cmd/harmonik` at 159, `internal/core` at 48 and `internal/queue` at 35. By linter: errcheck 307, gosec 199, gocognit 188, gocritic 92, unused 72, revive 69.

`make lint-full-count` still prints a whole-tree finding count for a reader who wants one number. It is a measure and not a verdict. Nothing blocks on it.

`scripts/lint-allow.sh` carries the reasoning and reaches the verdict, `tools/lintreport` judges each finding against the list and prints the report, and `scripts/lint-allow-test.sh` holds the assertions inside `script-tests`.

**Why the scoping went.** The old gate asked git which files changed since main and tested only those packages. `scripts/scenario-gate.sh` implemented that and is deleted. Measured 2026-08-03, whole-repo `go test -short -count=1 ./...` costs about 13 seconds more than `internal/daemon` alone, so the scoping saved 13 seconds. In exchange it could not see a break in a package the change did not touch, which is the usual case because `internal/daemon` imports eight other packages. The same script also had five ways to APPROVE work that never passed — a compile failure, a timeout, a signal kill, an unrecognised exit code, and a retry that allowed when the second run passed — against one way to block.

**Lanes that are not the gate.** None can block a merge and none is a third tier: `make test-race-nightly` (the nightly `-race` lane, the only place a data race surfaces now that `make full` runs without `-race`), `make test-integration` (the integration-tagged tier; needs tmux and a live environment), `make coverage-gates` (the two coverage ratchets; a trend measure, not a verdict).

**`make agent-review`.** Invokes the `agent-reviewer` skill, which makes an LLM call. It enforces a hard wall-clock timeout (`AGENT_REVIEW_TIMEOUT`, default 60s) so an unbounded hang cannot tempt anyone into `--no-verify`. `make review-verdict` cross-checks the stored verdict.

**Excluded from agent done-check** (CI-nightly or on-demand only, per `testing.md`): budget-capped real-agent smoke tests (Tier C), full property-test seeds (`HARMONIK_RAPID_SEED=auto` 10k iterations), full fault-injection site set, `govulncheck` weekly deep scan.

**The rule that makes this work:** agents MUST run `make full` before declaring work complete. Enforcement lives in `agent-configuration.md`.

## Commit-blocking vs advisory

Framed as tiers, not "pre-commit vs CI":

- **Tier 2-blocking** (every push; required to merge): all formatter/import checks, `go vet`, every enabled golangci-lint linter (including `depguard` component-graph rules), `go build`, `go test -race` unit+property, `go mod tidy` cleanliness, coverage gate, `forbid-import`, `gosec` high-severity.
- **Tier 3-blocking** (declared-done; required to merge): integration, scenario, fast-crash suites.
- **Advisory (posts, does not block):** `gosec` medium/low, `prealloc` (`//nolint:prealloc` with justification allowed). `govulncheck` is blocking at Tier 2 for known-high CVEs; weekly deep scan is advisory. Promotion is a one-line `.golangci.yml` edit.

## Agent-enforceability

Threat model: agent runs `git commit --no-verify` or writes `//nolint:all` to escape local hooks. Counter-pattern:

1. **GitHub branch protection on `main` with required status checks on pushes.** Pushes that fail CI's `make full` are marked red but (per `build-practices.md`) are not rejected — the agent fixes forward with a corrective commit. Branch protection prevents force-push and admin-bypass. Not a merge gate (no PRs), but the same commands that run locally also run remotely; divergence surfaces as a red main.
2. **`nolintlint` blocks bulk suppression.** Every `//nolint` must name specific linters + carry an explanation + suppress something real. `//nolint:all` fails lint.
3. **CI posts nolint-density delta on every push** (`git diff HEAD~1 | grep -c //nolint`). Agent-driven spikes are visible without manual diff reading.
4. **No admin-bypass for branch protection** on `main`. Solo dev is not exempted; rule changes require an explicit config edit.
5. **Protected rule files.** The files that define the gates themselves are protected; edits MUST trigger dedicated attention:

   - `.golangci.yml`, `.depguard.yml` (if factored out)
   - `tools/forbid-import/main.go` (library allowlist)
   - `scripts/coverage-gate.sh`
   - `.github/workflows/*.yml`
   - `Makefile` (the `check-*` targets)
   - `lefthook.yml`
   - `CONSTITUTION.md` — additionally requires a `Constitution-Edit-Approved-By: <name-or-email>` commit trailer on any edit (per `agent-configuration.md §CONSTITUTION.md`); commits lacking the trailer are rejected by pre-commit hook and flagged red by post-commit CI.

   A commit that touches any of these MUST:
   1. Cite a kerf-codename in the commit body explaining the rule change (so edit motivation is traceable).
   2. Trigger a `rule-change` CI check that watches these file paths and surfaces the rule-file diff on the commit summary.
   3. NOT bundle rule changes with unrelated code changes — one commit = one concern (rule change OR code), so the rule-change diff is reviewable on its own merits by the post-commit agent-reviewer and the async human reader.

   Prevents "agent relaxes the gate, then passes its own gate." Rule-change commits are distinguishable from ordinary code commits; silent rule weakening becomes impossible.

**No PR-based gating.** Enforcement relies on: (a) the agent-declared-done ritual running `make full` locally before commit; (b) `agent-reviewer` running on every non-trivial commit (per `build-practices.md §Agent review on every commit`); (c) post-push CI re-running the same gauntlet and surfacing failure as a red main. Fix-forward is the recovery, not a block-on-red.

Invariant: **CI mirrors local `make full`; local pass predicts CI pass; rule weakening requires a single-concern commit that trips the rule-change surface.**

## ⚑ Assumptions worth user's eye

1. **⚑ `gofumpt` over `gofmt`** — Stricter; safe for solo-dev, flag if external contributions open up (PR friction).
2. **⚑ `depguard` v2 handles component-graph rules natively.** Previously proposed `go-arch-lint`; dropped per reviewer convergence. `.go-arch-lint.yml` removed; component-graph rules live in `.golangci.yml`'s `depguard` settings. Durable only if subsystem package layout is stable; update when the 10-component foundation lands as code.
3. **⚑ Branch protection without PR-based gating.** Solo-dev: no PRs, no approval requirement. Branch protection prevents force-push + admin-bypass + requires status checks but cannot reject pushes outright. Revisit when PRs return (product has real users or multi-human team).
4. **⚑ `gosec` advisory, not blocking** — Elevate when the daemon handles secrets or opens network ports.
5. **⚑ No `wrapcheck`** — Omitted deliberately; forces wrapping at every package boundary, conflicting with "wrap only at subsystem boundaries." `errorlint` + review cover the real cases.
6. **⚑ Pre-commit 15s budget** — Split to `pre-commit` (format+vet) + `pre-push` (lint+test-short) if it passes 30s.
7. **⚑ Go 1.25 assumed.** Access to `log/slog` / `testing/synctest` / `os.Root` / `go.mod tool` depends on it. If bootstrap pins older Go, revise this doc.

## Deferred / follow-up

- Mutation testing (`gremlins`) — deferred until the unit suite is substantive.
- Coverage gate — once testing methodology codifies targets; enforce via `go-test-coverage` in CI.
- Benchmark regression gate — `benchstat` diff once any hot path exists.
- `govulncheck` blocking — promote from advisory once a CVE-triage process is defined.
- **Custom analyzer for four-axis determinism tags** (architecture §1.1) — `go/analysis` pass verifying LLM-freedom / I/O / replay / idempotency tags on cross-subsystem types. Natural fit later.
- Supply-chain pinning — Dependabot; SLSA/sigstore later.
