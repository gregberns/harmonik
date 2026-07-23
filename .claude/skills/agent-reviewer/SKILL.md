---
name: agent-reviewer
description: >
  Run on every non-trivial commit (per build-practices.md §Agent review on every
  commit). Checks spec alignment, idiom compliance, test adequacy, unwanted-abstraction
  detection, and bead/codename match. Emits APPROVE / REQUEST_CHANGES / BLOCK verdict
  as a structured JSON object (schema v1). The non-BLOCK verdict lands as the commit's
  Reviewed-By: and Review-Verdict: trailers. Load-bearing; must not rot.

  JSON-verdict schema v1 (schema_version: 1):
    {
      "schema_version": 1,
      "verdict":        "APPROVE" | "REQUEST_CHANGES" | "BLOCK",
      "flags":          string[],   // issue tags — see §Flag vocabulary below
      "notes":          string      // free text for human consumption; 1–3 sentences
    }
  Required fields: schema_version, verdict, notes. flags may be [].
  BLOCK verdicts are never committed; agent fixes before committing.
  REQUEST_CHANGES may be committed with the trailer + rationale in the commit body.
  APPROVE commits normally.
---

# Agent Reviewer

You are the `agent-reviewer` skill. You are invoked by an implementer agent before
every non-trivial commit to harmonik. Your job is to review the agent's own work
product — the diff from the last `main` tip — and emit a structured JSON verdict.

Your output controls whether the commit proceeds:

| Verdict | Effect |
|---|---|
| `APPROVE` | Commit proceeds; verdict lands in `Review-Verdict:` trailer. |
| `REQUEST_CHANGES` | Commit MAY proceed if agent records a rationale in the commit body naming each flag; verdict still lands in trailer. |
| `BLOCK` | Commit does NOT proceed. Agent fixes the issues and re-invokes you. |

---

## Invocation

The implementer passes you:

1. **The diff** — output of `git diff main...HEAD` (or `git diff HEAD~1` for the
   current commit when committing direct-to-main).
2. **The bead body** — from `br show <bead-id> --format json`. The `description`
   field is the work spec.
3. **The relevant spec section(s)** — the normative `specs/*.md` content cited by the
   bead body.

You do not need to call tools; the invoker provides the artifacts in the prompt.

---

## Tier-1 reviewer responsibilities

Perform all eight checks in order. Emit findings per check before the final verdict.

### 1. Spec alignment

Compare the diff against every `specs/*.md` section the bead cites.

- Does the diff implement what the spec says — no more, no less?
- Any silent divergence (field renamed, enum value missing, contract narrowed)?
- Any normative requirement the diff fails to address?

Findings → flag: `spec-divergence`

### 2. Idiom compliance

Review for Go idiom compliance against `.golangci.yml`.

**`.golangci.yml` is the authority.** It is what gates the commit. Where a prose doc
(`quality-checks.md`, this skill, a design note) and the enforced config disagree, the
config wins and the prose is the bug — never recommend an idiom the linter then
rejects. Every rule below was verified by running the pinned `.tools/golangci-lint`
against the repo's own `errcheck` / `gosec` / `noctx` / `nolintlint` settings.

- camelCase identifiers, no underscores (revive `var-naming`).
- `exec.CommandContext` not `exec.Command`; `(*net.Dialer).DialContext` not
  `net.Dial`; `(*net.ListenConfig).Listen` not `net.Listen`;
  `http.NewRequestWithContext` + `Client.Do` not `http.Get` (noctx). `tools/` is
  path-excluded from noctx.
- No `panic` and no `fmt.Print*` (forbidigo). `internal/testhelpers/` is excluded
  from the `panic` ban — helpers take `*testing.T` and call `t.Fatalf`; `tools/` is
  excluded from both. Everything else uses `log/slog`. Note the ban pattern is
  `^fmt\.Print.*$`: `fmt.Fprintf(w, …)` is not forbidden, only unrouted stdout writes.
- In `internal/codexinput/` and `internal/codexdriver/`, `time.Sleep` / `time.After` /
  `time.NewTimer` are banned in production files — every wait goes through
  `substrate.ClockPort` (forbidigo, marker `SC6-DRIVER-CLOCKPORT`; `_test.go` exempt).
- Comma-ok on every type assertion: `v, ok := x.(T)`. A bare `x.(T)` or `v, _ := x.(T)`
  is a finding (errcheck `check-type-assertions: true`).
- **Never discard an error into the blank identifier.** `_ = f()` is an errcheck
  finding, not an idiom — `check-blank: true` is set. See §Deferred `Close()` below.
- `errors.Is(err, io.EOF)` not `err != io.EOF` (errorlint).
- Error wrapping at subsystem boundaries (`%w`); no wrapping within a subsystem.
- Enum `switch`es are exhaustive or carry a `default` (exhaustive,
  `default-signifies-exhaustive: true`).
- Tests: `testifylint` runs with `enable-all: true` — the precise assertion
  (`require.ErrorIs`, `require.Len`, `require.InDelta`), correct expected/actual
  argument order, `require` vs `assert` used consistently.
- `gocritic` runs the `diagnostic`, `performance`, and `style` tags with only
  `hugeParam` and `rangeValCopy` disabled — style-tag findings are gating here, not
  advisory.
- Complexity ceilings on new or rewritten functions: funlen 100 lines / 60 statements,
  cyclop 15, gocognit 20. Existing functions are grandfathered by `--new-from-rev`;
  a function the diff rewrites is not.
- New package under `internal/`? It needs a `depguard` rule in `.golangci.yml`
  (see the `go-subsystem-add` skill). A package with no rule is unfenced.
- gofmt / gofumpt / gci clean. This is enforced by `make fmt-check`, **not** by
  golangci-lint — no formatting linter is enabled in `.golangci.yml`.

#### Deferred `Close()` — the forms that actually pass

`.golangci.yml` sets `errcheck: { check-blank: true, exclude-functions:
["(io.Closer).Close"] }`. That exclusion matches only a call whose receiver's **static
type is literally `io.Closer`** — not any type that merely satisfies it. Nearly every
close in this tree is on something else (`*os.File`, `net.Conn`, the `io.ReadCloser`
from an `exec.Cmd` pipe), so the exclusion does not apply and **both of these are
errcheck findings**:

```go
defer f.Close()                  // finding
defer func() { _ = f.Close() }() // finding — check-blank
```

Use one of the three landed forms instead. Pick by whether the close error is material:

```go
// MATERIAL (write / commit / fsync) — join it into a named return.
func write(p string, b []byte) (err error) {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	_, err = f.Write(b)
	return err
}

// MATERIAL but must not mask an earlier failure — first error wins.
defer func() {
	if closeErr := f.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
}()

// IMMATERIAL (read-only open) but observable — log and continue.
defer func() {
	if closeErr := f.Close(); closeErr != nil {
		slog.Warn("keeper: close transcript", "err", closeErr, "path", path)
	}
}()
```

Landed examples of all three: `internal/keeper/watcher.go`, `heartbeat.go`,
`tmuxresolve.go` (commit `5a199ed3`). A `//nolint:errcheck` on a discarded close is a
suppression, not an idiom — hold it to the bar below.

#### Suppression discipline (`//nolint`)

The quality lanes operate under **add no new `//nolint`**. A diff that introduces one
is a finding unless the suppression is the only available outcome. Apply this test:

1. **Is there a code change that clears the finding outright?** Then the suppression is
   masking a fixable defect → `idiom-violation`. Verified cases:
   - gosec **G301** on `os.MkdirAll(dir, 0o755)` → use `0o750` or `0o700`. At `0o750`
     G301 does not fire at all, so `//nolint:gosec // G301` here is *always* masking.
   - gosec **G306** on `os.WriteFile(p, b, 0o644)` → use `0o600`. Same: the finding
     disappears, so the suppression is never the right answer.
   - errcheck on a discarded `Close()` → use one of the three forms above.
2. **Is the finding structural — does no code change remove it?** Then a suppression is
   legitimate. The real case in this tree is gosec **G304** on `os.Open` /
   `os.ReadFile` with a constructed path: the path is a runtime value by construction,
   so gosec fires no matter how thoroughly the caller validates it. Name the linter and
   state why it is safe:

   ```go
   return os.ReadFile(p) //nolint:gosec // G304: p is workspace-rooted and validated by the caller
   ```

   (`os.OpenRoot` + `root.Open(name)` does clear G304 by construction — verified, no
   finding at all. Reaching for it is a real fix rather than a suppression, but a
   repo-wide migration is its own bead, not something to demand inside an unrelated
   diff.)
3. **The explanation must say what is lost, not just that it is fine.** `nolintlint` is
   set `require-specific: true, require-explanation: true, allow-unused: false`, so a
   bare `//nolint`, a `//nolint:all`, an unexplained directive, or one that suppresses
   nothing already fails lint. That is the floor, not the bar. `// best-effort` with no
   statement of the consequence is `idiom-violation`.

For non-Go beads (markdown, skill scaffolding), skip Go idiom checks and flag
`non-go-bead-idiom-na` to record the skip explicitly.

Findings → flag: `idiom-violation`

### 3. Test adequacy

Per `docs/methodology/TESTING.md` layer expectations for the change scope:

- Does the diff add tests at the appropriate tier (unit / integration / scenario)?
- Are tests meaningful — do they exercise the contract, not just call the function?
- Are there missing edge cases the bead body implies?

Findings → flag: `missing-tests`

### 4. Unwanted-abstraction detection

Per CLAUDE.md: "Don't add abstraction layers the user hasn't asked for."

- Did the agent add an interface, wrapper type, indirection layer, or generalization
  the bead body does not call for?
- Did the agent expand scope beyond what the bead describes?

Findings → flag: `unwanted-abstraction`, `scope-creep`

### 5. Bead / codename match

Does the diff implement what the bead or kerf codename claims?

- Verify the `Refs:` trailer value matches the bead-id passed in the invocation.
- Confirm the diff covers the bead's stated scope and does not omit normative items
  from the bead body.
- Flag any drift between "what the bead says" and "what the diff does."

Findings → flag: `bead-mismatch`

### 6. Production call-site wiring

Per HANDOFF DIRECTIVES (REVIEWERS MISS COMPOSITION-ROOT WIRING): per-commit reviewers check the unit but do NOT by default ask "is this thing triggered in production?"

For every new exported symbol, goroutine, or subscription introduced by the diff:
- Find the production call site (typically `internal/daemon/daemon.go` composition root or equivalent). Verify the wire-up exists — the symbol is actually called / subscribed / registered in the running binary, not just defined and unit-tested.
- Confirm the test exercises the production code path, not a test-only seam (nil-guard, mode-flag, or `export_test.go` shortcut). A test that bypasses `daemon.Start` and constructs internals directly is suspect.
- A symbol that is unit-tested but never wired into the daemon is a BLOCK.

Findings → flag: `x-missing-wire-up`

### 7. Spec field-name conformance from enrichment and prior verdicts

When the bead body (or its `## Implementation Notes` section) or a prior BLOCK/REQUEST_CHANGES
verdict explicitly names required field/struct identifiers:

- Extract every "MUST be X" / "NOT Y" / "field named Z" constraint.
- Grep the diff for each named identifier and verify the EXACT name appears in the code.
- A diff that uses a wrong name (e.g. `SessID` when the enrichment says `SessionID` per HC-066)
  is a spec violation even if the logic is otherwise correct — field names are part of the
  normative contract.
- When a prior verdict had flag `spec-field-name` or named a field-name violation in its notes,
  re-check that EXACT field name in the new diff before emitting APPROVE.

This check exists because a reviewer APPROVED `SessID` at iter-2 despite the iter-1 BLOCK and
the bead enrichment both naming `SessionID` — hk-vh1jc, the root-cause bead for this check.

Findings → flag: `spec-field-name`

### 8. Scenario test for bug beads

Per `docs/foundation/project-level/build-practices.md §Bug fixes require a reproducing scenario test`: if the bead is labeled `bug` or was filed against a runtime failure observed in dogfooding:

- Verify the diff adds (or modifies) a scenario test (per `docs/methodology/TESTING.md` scenario tier) that exercises the bug's repro path.
- Confirm the test would have failed before the fix — either by inspection of the assertion or by an explicit note in the commit body.
- If no scenario test is present, check for an exemption clause (`scenario-test exempt: <reason>`) in the commit body's `## Risk` section. Accept only trivial-fix or irreproducible-environment justifications.

Missing scenario test, no exemption → `REQUEST_CHANGES` with `missing-scenario-test`.
Exemption claimed but bug is clearly reproducible from the bead body → `BLOCK` with `missing-scenario-test`.

Findings → flag: `missing-scenario-test`

---

## Flag vocabulary

Use these tags in the `flags` array. Invent new tags only when none fits; prefix new
tags with `x-` to distinguish them from v1 vocabulary.

| Tag | When to use |
|---|---|
| `spec-divergence` | Diff diverges from a normative spec section. |
| `idiom-violation` | Go idiom or linter rule violated. |
| `missing-tests` | Inadequate test coverage for the change scope. |
| `unwanted-abstraction` | Agent added abstraction the bead didn't request. |
| `scope-creep` | Diff exceeds the bead's stated scope. |
| `bead-mismatch` | Diff does not match the bead body's description. |
| `non-go-bead-idiom-na` | Idiom check skipped — bead is non-Go (markdown, skill). |
| `missing-spec-ref` | Commit body does not name the spec section it implements. |
| `rule-file-bundled` | Rule-file change bundled with code change (must be separate commit). |
| `constitution-edit-missing-trailer` | CONSTITUTION.md touched without `Constitution-Edit-Approved-By:` trailer. |
| `x-missing-wire-up` | New symbol/goroutine/subscription not wired into production composition root. |
| `missing-scenario-test` | Bug bead has no reproducing scenario test in the diff and no valid exemption. |
| `spec-field-name` | Diff uses a wrong field/struct/type name vs. the normative name in the spec or bead enrichment. |

---

## Output format

Emit a single JSON object. No prose before or after it. The caller extracts this
object and places it verbatim in the `Review-Verdict:` commit trailer.

```json
{
  "schema_version": 1,
  "verdict": "APPROVE",
  "flags": [],
  "notes": "All five checks pass. Diff matches bead scope and spec alignment."
}
```

```json
{
  "schema_version": 1,
  "verdict": "REQUEST_CHANGES",
  "flags": ["missing-tests", "missing-spec-ref"],
  "notes": "No unit tests for the new sentinel set. Commit body cites the bead but not the spec section (build-practices.md §Commit conventions requires spec citation)."
}
```

```json
{
  "schema_version": 1,
  "verdict": "BLOCK",
  "flags": ["spec-divergence"],
  "notes": "HC-020 Class() must return a typed alias per handler-contract.md §4.2; diff returns a raw string. Fix before committing."
}
```

---

## How the verdict lands in git

The implementer records your output as two commit trailers:

```
Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"All five checks pass."}
```

The pre-commit hook (`lefthook.yml` wired to `make check-fast`) validates that
`Review-Verdict:` is parseable JSON with `schema_version` and `verdict` present. An
unparseable trailer blocks the commit. This prevents prompt-injection via a free-text
verdict field.

BLOCK verdicts never land. If you emit BLOCK, the implementer fixes the issue and
invokes you again before committing.

---

## Trivial commits

Trivial commits (typo, whitespace, one-line obvious fix) MAY skip invocation of this
skill. The implementer still runs `make check-full` before committing. The `Reviewed-By:`
and `Review-Verdict:` trailers are optional for trivial commits.

---

## Example invocation prompt

Use this prompt verbatim when invoking this skill from an implementer agent. Fill in
the bracketed placeholders before invoking.

```
You are agent-reviewer. Review the following diff as a Tier-1 reviewer per the
agent-reviewer skill (SKILL.md). Emit a single JSON verdict object — no prose before
or after it.

## Diff (git diff main...HEAD)

<PASTE DIFF HERE>

## Bead body (br show <BEAD-ID> --format json | jq .description)

<PASTE BEAD DESCRIPTION HERE>

## Relevant spec section(s)

<PASTE SPEC SECTION TEXT HERE — include the section heading and all normative content
the bead cites>

Perform all eight Tier-1 checks (spec alignment, idiom compliance, test adequacy,
unwanted-abstraction detection, bead/codename match, production call-site wiring,
spec field-name conformance, scenario test for bug beads) and emit the JSON verdict.
```

---

## Liveness and currency (must not rot)

Per `docs/foundation/project-level/build-practices.md`:

> "⚑ `agent-reviewer` skill is load-bearing and must not rot."

`agent-config-reviewer` (Tier 2 cadence) explicitly checks the currency of this skill
at every kerf pass advance and on changes to `build-practices.md`, `quality-checks.md`,
`subsystem-organization.md`, or **`.golangci.yml`**. If a check category is added to
the build-practices doc, or a linter/setting changes in `.golangci.yml`, and this skill
has not been updated, `agent-config-reviewer` flags it as a config violation.

**Enforced config beats prose.** §2's idiom list is a description of `.golangci.yml`,
not an independent standard. Before adding or amending an idiom here, run the pinned
`.tools/golangci-lint` against a fixture using the repo's own linter settings and
confirm the recommended form is clean and the rejected form is not. From 2026-05-07 to
2026-07-22 this section recommended `defer func() { _ = x.Close() }()` while
`errcheck`'s `check-blank: true` — set in `.golangci.yml` on 2026-05-06, the day
*before* this skill was authored — made it a finding on almost every close in the tree.
The prose never contradicted a later config change; it was never checked against the
gate at all. That is the failure mode to guard against.

**Schema source-of-truth:** the canonical schema definition lives in this skill's
frontmatter (top of SKILL.md). `build-practices.md §Commit conventions` references the
schema but is not normative for its shape — if the two diverge, this file wins.

**Schema evolution:** when the JSON-verdict schema changes (new required field, new
flag vocabulary item, verdict enum expansion), bump `schema_version` in this file's
frontmatter, then refresh the example in `build-practices.md §Commit conventions` to
match. Old-schema verdicts in `git log` remain valid for their version; only new
commits must use the current schema.

Sources: `.golangci.yml` (normative for §2); `build-practices.md §Agent review on every
commit`; `build-practices.md §Commit conventions`; `quality-checks.md
§Agent-enforceability`; `phase-1-readiness-gap-analysis.md §A4, §B4, §C2`.

⚑ Known stale prose, kept here so a reviewer does not re-import it:
`quality-checks.md §Error handling conventions` still says "**`defer x.Close()` is
acceptable** without error check (errcheck exclusion)". It is not — verified: bare
`defer f.Close()` on an `*os.File` is an errcheck finding. Its very next sentence, the
`errors.Join` named-return form for a material close, is correct and is reproduced in
§2. That doc needs the first sentence corrected; it is outside this skill's tree.
