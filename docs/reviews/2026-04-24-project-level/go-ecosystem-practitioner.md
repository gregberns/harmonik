# Go Ecosystem Practitioner Review

## Verdict summary

The toolchain is ~90% current for early-2026 idiomatic Go. Most picks are defensible community standards (gofumpt, testify/require, rapid, gotest.tools, goreleaser, lefthook, govulncheck). The notable staleness is **golangci-lint versioning** — the config declares `version: "2"` but the prose and migration posture read as if v1.62+ is still current; v2 has been GA for most of 2025 and the setup needs to commit to it. A handful of 2024-2026-era tools are missing entirely (`go test -fuzz` seed policy, Go 1.22 range-over-func testing idioms, `slog`, `fieldalignment` handling for agent-generated structs), and a few linter disables are over-cautious.

## Tool calibration

- **Tool**: `gofumpt`
  **Verdict**: AFFIRM
  **Rationale**: Still the canonical "stricter gofmt" in early 2026. `mvdan/gofumpt` is actively maintained, tracks Go releases, and catches exactly the redundant-parenthesis / multi-line-literal noise agents produce. No serious challenger exists — `gofmt -s` is a subset.

- **Tool**: `gci`
  **Verdict**: AFFIRM with version note
  **Rationale**: `daixiang0/gci` is still the import-organizer of choice; `goimports` alone does not enforce group ordering. Pin to `gci` v0.13+ (the v0.12 series had a Go 1.22 tokenizer bug on generic type parameters). Config syntax is stable; the three-group shape shown is correct.

- **Tool**: `golangci-lint` (declared as "v1.62+", config says `version: "2"`)
  **Verdict**: FLAG
  **Rationale**: There is an inconsistency between the prose ("v1.62+") and the config (`version: "2"`). golangci-lint **v2.0 went GA in March 2025** and is the current mainline as of early 2026 (latest is v2.3.x). The `version: "2"` key in `.golangci.yml` is v2-only; v1.62 will reject it. Commit to v2. Migration implications: v2 renamed several settings (`run.skip-dirs` → `issues.exclude-dirs`, `linters-settings.errcheck.exclude` file lookup changed), `gosimple` was merged into `staticcheck` (it's no longer a separately-enableable linter in v2 — enabling it is a no-op or a config error depending on version), and `deadcode`/`structcheck`/`varcheck` references are rejected (the doc correctly notes `unused` subsumes them, good). Pin to v2.3.x explicitly in `tools.go` / CI.

- **Tool**: `testify/require`
  **Verdict**: AFFIRM
  **Rationale**: Still the dominant assertion library. v1.9+ (current is v1.10) is fine. The "no suite, no mock" stance is the 2026 consensus — `testify/mock` in particular has been superseded by hand-rolled fakes or stdlib `testing/synctest`-style patterns in most idiomatic projects.

- **Tool**: `pgregory.net/rapid`
  **Verdict**: AFFIRM
  **Rationale**: Correct pick. `rapid` is the maintained, shrinking-capable property library for Go in 2026. `testing/quick` is a toy; `gopter` has been moribund since 2022. Alternatives like `leanovate/gopter` remain abandoned — no new entrant worth considering.

- **Tool**: `gotest.tools/v3`
  **Verdict**: AFFIRM
  **Rationale**: Maintained by gotestyourself; `v3` is current. `golden`, `fs`, and `icmd` sub-packages are appropriate and widely used. No drift concern.

- **Tool**: `go-arch-lint` (fe3dback)
  **Verdict**: ALTERNATIVE
  **Rationale**: `fe3dback/go-arch-lint` works but is effectively single-maintainer and has had slow release cycles in 2024-2025. The document acknowledges this (`⚑` point 6). Current community momentum has shifted toward **`go-modiff`-adjacent approaches** and especially **`depguard` inside golangci-lint** — `depguard` v2 (shipped mid-2024) added package-pattern matchers that express component-to-component rules nearly as cleanly as go-arch-lint, and it's already in your linter bundle. Alternative worth considering: `loangoe/arch-go` (more active) or commit to `depguard` v2 as primary and drop the second tool. The current config effectively declares the component graph **twice** (once in `.go-arch-lint.yml`, once in `.golangci.yml` `depguard.rules`). Pick one.

- **Tool**: `lefthook`
  **Verdict**: AFFIRM
  **Rationale**: Single Go binary, no Python/Node dep, v1.7+ in early 2026. Correct pick over `pre-commit` (Python) or `husky` (Node) for a pure-Go solo project. Config format is stable.

- **Tool**: `goreleaser`
  **Verdict**: AFFIRM
  **Rationale**: v2 has been GA since mid-2024 and is the de-facto Go release tool. The cross-compile matrix (`linux/{amd64,arm64}`, `darwin/{amd64,arm64}`) is standard. Note: the doc should specify `goreleaser/v2` config schema (breaking changes from v1).

- **Tool**: `govulncheck`
  **Verdict**: AFFIRM
  **Rationale**: Correct inclusion. Advisory-only for MVH is reasonable. Pin v1.1+ for Go 1.23 symbol-level analysis.

- **Tool**: `gosec`
  **Verdict**: FLAG (mild)
  **Rationale**: Still widely used but increasingly noisy in 2026 with high false-positive rate. Advisory-only is the right posture. Consider also enabling **`errchkjson`** (JSON marshal-safety) — a real-bug linter agents miss, and a daemon that emits JSONL events heavily uses `json.Marshal`.

- **Tool**: `forbidigo` with `^fmt\.Print.*$` ban
  **Verdict**: AFFIRM with note
  **Rationale**: Correct for structured-logging discipline. Add `^log\.(Print|Fatal|Panic).*$` to also forbid the stdlib `log` package — harmonik should be on `log/slog` (Go 1.21+, now fully mainstream in 2026). Not mentioned anywhere in the docs; `slog` is a conspicuous omission.

## Missing tools / practices

- **`log/slog` policy.** The quality-checks doc forbids `fmt.Print*` but never names the replacement. In 2026 the answer is `log/slog` (stdlib since 1.21); specify the handler (JSON for structured, Text for dev), level conventions, and `slog.With` for subsystem-tagged loggers. This is load-bearing for an event-emitting daemon and its absence will produce ad-hoc logging across subsystems.

- **`go test -fuzz` seed corpus wiring.** The testing doc defers fuzz *infrastructure* but in 2026 `go test -fuzz` is table-stakes for boundary parsers (DOT, YAML, JSONL, commit-trailer). Committing a `testdata/fuzz/<FuzzTest>/` seed corpus costs ~nothing and turns `errcheck` regressions into demonstrable crashes. Close this gap now, not post-MVH.

- **`go.work` policy.** Not mentioned. For a single-module repo the correct answer is **do not use `go.work`** (the doc implicitly assumes this) but say so — agents will sometimes create one spuriously. A one-line "no `go.work` file; single module" kills that drift.

- **`fieldalignment` handling.** The doc disables `fieldalignment` as "noisy." In early 2026 this is reasonable for hand-written code but agent-generated structs are a known offender for padding; consider enabling it for `internal/core` only (where event envelopes live and alignment matters for log-volume) via `linters-settings.govet.settings.fieldalignment` scoped rules, or run it as a one-shot nightly advisory and auto-open PRs from `betteralign -apply`.

- **`errchkjson`.** For a JSONL-emitting daemon, `errchkjson` (from `breml/errchkjson`) catches `json.Marshal` calls on non-marshalable types. Very cheap win; not in the 25.

- **`testifylint`.** Meta-linter for testify misuse (wrong assertion function, swapped expected/actual, etc.). Maintained in 2025-2026, ships as a golangci-lint v2 linter. If you standardize on `require`, `testifylint` enforces it mechanically — directly relevant to the "no bulk assert" policy.

- **`go vet -vettool` for custom analyzers.** The quality-checks doc mentions "Custom analyzer for four-axis determinism tags" as deferred. The mechanism is `go/analysis` + `golang.org/x/tools/go/analysis/singlechecker`, wired via `go vet -vettool=./bin/myanalyzer`. Name the mechanism now so it's not re-litigated.

- **Go 1.22+ range-over-func / iter.Seq idioms.** `iter.Seq`/`iter.Seq2` (Go 1.23 stdlib) are now the idiomatic iterator pattern for things like event-bus fan-out, workflow-node traversal, and JSONL streaming. The testing doc's property generators would benefit. Not a tool per se, but a 2026 idiom worth naming in testing conventions.

- **`synctest` (Go 1.24 experimental, 1.25 stable).** For deterministic time-based tests (timeouts, rate-limits, retries) `testing/synctest` is the 2026 idiom replacing `clockwork`-style abstractions. If Go 1.25 lands during MVH, this is the test-time clock. Flag for radar.

## Version concerns

- **`go.mod toolchain go1.23.4`**: go1.23.4 shipped December 2024. As of April 2026, **Go 1.24 is stable (Feb 2025) and Go 1.25 is the current release (Aug 2025)**. Pinning to 1.23.4 in early 2026 means missing `testing/synctest`, `os.Root` (1.24 — useful for workspace sandboxing), and performance/stdlib improvements. Recommend bumping the floor to **Go 1.24** minimum, with `toolchain go1.24.x` or `go1.25.x` in `go.mod`. "Go 1.23+" as a project-wide baseline is already 18 months behind by the time code lands.

- **`golangci-lint` v1.62+ vs `version: "2"` config**: contradictory (see tool calibration). Pick v2.

- **`goreleaser` unversioned**: implicitly v2 from the timing, but `.goreleaser.yaml` schema version needs to be explicit (`version: 2` at top of file) or `goreleaser` will warn.

- **`gci` unversioned**: pin `>= v0.13.4` for Go 1.23+ generic support.

## Idiom concerns

- **`internal/` layout**: correct and idiomatic. `internal/core` as a leaf shared-types package is a well-established pattern (matches kubernetes, cockroachdb, etc.). AFFIRM.

- **`cmd/` with per-binary subdirectories**: correct and standard. AFFIRM.

- **`pkg/` deliberately empty**: the 2026 consensus is finally settled — **`pkg/` is an anti-pattern** for new projects and the golang-standards layout is not a standard. Deliberately leaving it empty is correct, but the doc should be more assertive: `pkg/` is not "revisited if an external consumer materializes" — it's "never used; if external consumers appear, expose via `api/` or a separate module." AFFIRM the policy, strengthen the justification.

- **`doc.go` convention**: not mentioned anywhere. Every `internal/<subsystem>/` package should have a `doc.go` with a package comment; `revive`'s `package-comments` rule (enabled in the config) will flag missing ones, but the convention should be explicit in subsystem-organization.md.

- **Error wrapping "at subsystem boundaries only"**: reasonable policy but under-specified. `errorlint` will flag un-wrapped `%v` of errors but won't enforce the *boundary* rule. This is a review-judgment call, not a lint rule — acknowledge that explicitly.

- **`errors.Join` pattern for close-errors**: the doc shows `defer func() { err = errors.Join(err, x.Close()) }()`. Correct 1.20+ idiom. AFFIRM.

- **Conventional Commits `spec:` type**: non-standard but reasonable; note that `commitlint`-style tooling won't recognize it out of the box. If a commit-message linter gets added later (not in the current stack — another gap), it'll need custom config.

- **Hand-written fakes in `faketest/` sub-package**: idiomatic and matches the 2026 "reject gomock" consensus. The sub-package convention (`internal/<pkg>/faketest/`) is correct — avoids import cycles and keeps test helpers out of production binaries. AFFIRM.

- **`faultpoint` package via build tags**: good idiom; the closest comparable is CockroachDB's `failpoints` and Kubernetes' `testing/utiltesting`. Using `//go:build crash` to dead-code-eliminate in production is textbook. AFFIRM.

- **`tools.go` pattern**: not mentioned explicitly. In Go 1.24+ the canonical tool-pinning mechanism is **`go.mod tool` directives** (new in 1.24), which supersedes the old `//go:build tools`/`tools.go` hack. If you bump the floor to 1.24, use `go tool add` for `golangci-lint`, `gofumpt`, `gci`, `goreleaser`, `lefthook`, etc. This is a genuine 2026 improvement over the pre-1.24 pattern.

- **Linter disables — `wrapcheck`, `funlen`, `wsl`, `lll`, `godox`, `tagliatelle`, `exhaustruct`, `gochecknoglobals`, `gochecknoinits`, `varnamelen`, `nlreturn`**: the disabled set is reasonable for MVH. One quibble: **`gochecknoinits`** is arguably worth enabling — agent-coded projects are prone to `init()` sprawl, which breaks test isolation and startup determinism; the daemon explicitly wants a single composition root in `internal/daemon`, and `init()` functions in subsystems violate that. Consider flipping it on.

- **Missing from the enabled list worth considering**: `containedctx` (ctx embedded in struct — a known agent anti-pattern), `fatcontext` (nested ctx in loops), `exhaustive` (switch-on-enum completeness — valuable for the node/event taxonomies in execution-model §2.1), `spancheck` (if OpenTelemetry lands later), `mirror` (bytes/strings mirror API misuse — cheap correctness win).
