---
name: go-subsystem-add
description: >
  Package-scaffold skill. Run when an agent first adds a new top-level subsystem
  package under internal/. Covers the full scaffold surface: package layout per
  subsystem-organization.md, depguard component-matrix entry per .golangci.yml,
  and test-helper hookup per internal/testhelpers/. Not bootstrap — becomes
  load-bearing once Phase-2-and-beyond subsystems start being added.

  Sources: docs/foundation/project-level/subsystem-organization.md §Go module
  layout; docs/foundation/phase-1-readiness-gap-analysis.md §A4 skill-registry
  sub-bullet (candidate bead p1-skill-go-subsystem-add).
---

# go-subsystem-add

You are the `go-subsystem-add` skill. Load this skill when you intend to create a
new top-level subsystem package under `internal/` (e.g., `internal/agentrunner`,
`internal/hook`, `internal/memory`). Walk every step in order. Do not skip steps.

---

## Trigger condition

An agent triggers this skill when it first introduces a new directory at
`internal/<subsystem>/` that did not previously exist. "First add" means: the
directory is absent from `main`, and the agent is the one creating it.

This skill does NOT apply to:
- Sub-packages inside an existing subsystem (e.g., `internal/orchestrator/runner`).
- External adapter packages (`internal/adapter/br`, `internal/adapter/ntm`).
- The composition root (`internal/daemon`).
- `internal/core` (a leaf — its scaffold is already in place).
- Handler implementations (`internal/handler/{claudecode,pi,twin}`).

For sub-packages and adapters, follow the same file checklist below but skip the
depguard section (the parent subsystem's rule already covers the subtree) and
consult the parent's existing rule before adding edges.

---

## Step 1 — Confirm the subsystem ID and package path

Cross-reference `docs/foundation/project-level/subsystem-organization.md`
§Package-per-subsystem mapping. Find the row for the subsystem you are adding.
Verify:

- The subsystem identifier (e.g., S04 Agentrunner → `internal/agentrunner`).
- The `mayDependOn` column in the allowed-edge matrix in `subsystem-organization.md`
  §Dependency layering enforcement. This is the source-of-truth for which imports
  are legal.

If the subsystem is NOT listed in the table, stop and consult the spec or user
before proceeding. Do not invent a new package path.

---

## Step 2 — Create the required scaffold files

Every new subsystem MUST provide the following files. Create them in this order.

### 2a. `internal/<subsystem>/doc.go`

```go
// Package <subsystem> holds ... (one line description of the package's role).
//
// It is defined normatively in [specs/<spec-file>.md].
//
// The <SomeType> TYPE itself is owned by <bead-id> (not yet shipped). This
// package currently contains only test files: the only non-test Go file is
// this doc.go. Helpers, types, and placeholder classifiers that are used
// exclusively by tests are declared in *_test.go files in this package.
//
// See [specs/<spec-file>.md] §10.2 for the full test-surface obligations.
package <subsystem>
```

Rules:
- The package name is the last path element of the package path, lowercase,
  no underscores (e.g., `agentrunner`, `hook`, `workspace`).
- The spec file reference uses the Go `[doc-link]` syntax pointing to the
  normative spec.
- Replace the bead ownership line once the primary constructor bead is known.
  If it is not yet known, use "owned by <follow-up bead TBD>."

### 2b. `internal/<subsystem>/<subsystem>.go`

This file contains the primary exported type for the subsystem (the "envelope"
type per architecture.md §1.4). If the primary bead has not yet been dispatched,
the file is a stub with a godoc TODO citing the bead ID.

```go
package <subsystem>

// <SomeType> is the production record / handle for the <Subsystem Name>.
// It is defined normatively in specs/<spec-file>.md §<record-section>.
//
// TODO(<bead-id>): implement the full record and constructor when the primary
// bead is dispatched.
type <SomeType> struct{}
```

Rules:
- The exported type name is PascalCase, derived from the subsystem name.
- Use the typed-alias-deferral pattern for any cross-package type references
  not yet defined in `internal/core`: represent them as `*string` / `string`
  with a godoc TODO citing the spec section and a follow-up bead ID per
  `.claude/implementer-protocol.md §Typed-alias-deferral pattern`.
- Do NOT add interface types the bead body does not call for.

### 2c. `internal/<subsystem>/<subsystem>_test.go`

A minimal smoke test confirming the package compiles and the stub type is
accessible. Use `internal/testhelpers` for any shared assertions.

```go
package <subsystem>_test

import (
	"testing"
)

// Smoke test: confirms the package compiles and the <SomeType> zero value
// is allocatable. Substantive tests land with their owning beads.
func Test<SomeType>Compiles(t *testing.T) {
	t.Parallel()
	var _ <SomeType>
	_ = t // suppress "unused" warning on older analyzers
}
```

Rules:
- Use an external test package (`package <subsystem>_test`), not an internal
  `package <subsystem>` test, so the test exercises the exported surface.
- `t.Parallel()` is required on every test function per `docs/methodology/TESTING.md`.
- Do not import `internal/testhelpers` unless you actually call one of its
  helpers; the import will fail `unused` lint if the package is empty.

---

## Step 3 — Add a depguard rule to `.golangci.yml`

The `depguard` component-graph rules live under `linters-settings.depguard.rules`
in `.golangci.yml` at the repo root. You MUST add an entry for the new subsystem.

### 3a. Determine the allowed-edge set

Consult the allowed-edge matrix in `docs/foundation/project-level/subsystem-organization.md`
§Dependency layering enforcement. The row for your subsystem lists every package
it MAY import. Map each "✓" cell to the corresponding import path:

| Matrix label      | Import path                                           |
|-------------------|-------------------------------------------------------|
| core              | `github.com/gregberns/harmonik/internal/core`         |
| eventbus          | `github.com/gregberns/harmonik/internal/eventbus`     |
| policy            | `github.com/gregberns/harmonik/internal/policy`       |
| handler-contract  | `github.com/gregberns/harmonik/internal/handler/contract` |
| adapter-br        | `github.com/gregberns/harmonik/internal/adapter/br`   |
| adapter-ntm       | `github.com/gregberns/harmonik/internal/adapter/ntm`  |
| workspace         | `github.com/gregberns/harmonik/internal/workspace`    |
| hook              | `github.com/gregberns/harmonik/internal/hook`         |
| agentrunner       | `github.com/gregberns/harmonik/internal/agentrunner`  |
| orchestrator      | `github.com/gregberns/harmonik/internal/orchestrator` |
| memory            | `github.com/gregberns/harmonik/internal/memory`       |
| scenario          | `github.com/gregberns/harmonik/internal/scenario`     |
| improvement       | `github.com/gregberns/harmonik/internal/improvement`  |
| handlers          | `github.com/gregberns/harmonik/internal/handler/...`  |

Always include `$gostd` (Go standard library) in every `allow:` list.

### 3b. Write the rule entry

Find the `rules:` block in `.golangci.yml`. The existing rules are in commented
form; insert the new rule as an active (uncommented) entry immediately after the
`core:` rule and before the first block of commented rules, keeping the block
sorted from lowest-layer to highest-layer (core first, daemon last).

Example for `agentrunner` (imports core + eventbus + handler-contract + adapter-ntm):

```yaml
        agentrunner:
          files: ["**/internal/agentrunner/**"]
          allow:
            - "$gostd"
            - "github.com/gregberns/harmonik/internal/core"
            - "github.com/gregberns/harmonik/internal/eventbus"
            - "github.com/gregberns/harmonik/internal/handler/contract"
            - "github.com/gregberns/harmonik/internal/adapter/ntm"
```

### 3c. Verify the rule activates

After editing, run:

```
go tool golangci-lint run ./internal/<subsystem>/...
```

Expected: no depguard violations (only the new package exists, so no imports
yet). If `golangci-lint` is not available locally, at minimum confirm the
`.golangci.yml` is valid YAML and the rule name is unique across the `rules:`
block.

---

## Step 4 — Verify testhelpers hookup

`internal/testhelpers` provides package-level test helpers shared across all
subsystem tests. Confirm the following before committing:

1. The new `<subsystem>_test.go` compiles against the current `internal/testhelpers`
   surface — run `go test ./internal/<subsystem>/...`.
2. If you introduce a per-bead test helper for this subsystem, use the camelCase
   prefix convention from `.claude/implementer-protocol.md §Helper-prefix discipline`.
   Derive the prefix from the subsystem concept (e.g., `agentrunnerFixture`,
   `hookFixture`). Helpers MUST be declared in `*_test.go` files only — never
   in production files.
3. If you need a shared fixture file for the subsystem, name it
   `testfixture_test.go` (matching the convention in `internal/lifecycle` and
   `internal/workspace`).

---

## Step 5 — Pre-commit checklist

Run all of the following before committing. Fix any failure before committing.

```bash
go build ./internal/<subsystem>/...
go test ./internal/<subsystem>/...
gofmt -d ./internal/<subsystem>/
```

Expected for a fresh scaffold: `go build` and `go test` pass; `gofmt -d` output
is empty. If `go test` output shows `[no test files]`, ensure the `_test.go`
file was created in Step 2c.

For the `.golangci.yml` edit:

```bash
go tool golangci-lint run ./internal/<subsystem>/...
```

---

## Step 6 — Commit format

Follow `.claude/implementer-protocol.md §Commit format (REQUIRED — verbatim
HEREDOC pattern with quoted EOF)`. Use scope `feat(subsystem)` or
`feat(<subsystem-name>)` for the scaffold commit. Include a bullet per file
added. Include `Refs: <bead-id>`.

---

## Canonical file layout after scaffold

```
internal/
  <subsystem>/
    doc.go             # package doc comment + package declaration (no imports)
    <subsystem>.go     # primary exported type (stub or full, per bead state)
    <subsystem>_test.go  # external test package smoke test
```

All subsequent bead implementations for this subsystem add to this tree. Do NOT
create subdirectory packages at scaffold time unless the primary bead explicitly
calls for them.

---

## Reference: subsystem-to-package mapping (from subsystem-organization.md)

| ID  | Subsystem           | Package path                       |
|-----|---------------------|------------------------------------|
| S01 | Orchestrator Core   | `internal/orchestrator`            |
| S02 | Policy Engine       | `internal/policy`                  |
| S03 | Event Bus           | `internal/eventbus`                |
| S04 | Agent Runner        | `internal/agentrunner`             |
| S05 | Hook System         | `internal/hook`                    |
| S06 | Workspace Manager   | `internal/workspace`               |
| S07 | Scenario Harness    | `internal/scenario`                |
| S08 | Memory Layer        | `internal/memory`                  |
| S09 | Improvement Loop    | `internal/improvement`             |
| —   | Handler contract    | `internal/handler/contract`        |
| —   | Handler impls       | `internal/handler/{claudecode,pi,twin}` |
| —   | Composition root    | `internal/daemon`                  |

---

Sources: `docs/foundation/project-level/subsystem-organization.md` §Go module
layout, §Dependency layering enforcement; `.golangci.yml` `linters-settings.depguard`;
`internal/testhelpers/` (assert.go, tempdir.go); `docs/foundation/phase-1-readiness-gap-analysis.md`
§A4 (`p1-skill-go-subsystem-add` candidate bead).
