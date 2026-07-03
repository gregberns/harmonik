# Change Spec: T5 — Lint Audit + depguard Rule Activation

**Component:** T5  
**Date:** 2026-05-20  
**Research:** 04-research/T5/findings.md

---

## Requirements (from 03-components.md)

1. For each package that exists and has a commented-out depguard rule: uncomment and activate.
2. `golangci-lint run` exits 0 after all rules are activated.
3. `tools/forbid-import/` — either implement or file follow-up bead.
4. Document intentionally-disabled linters (verify .golangci.yml comments are complete).

---

## Research Summary

- Packages that exist and CAN be activated as-written: `workspace`, `daemon`, `cmd`.
- `eventbus` CANNOT be activated as-written: it imports `handlercontract`, which is not in the stub's allow list. This is a spec-drift issue (eventbus should be core-only per architecture, but code imports handlercontract).
- Packages whose stubs reference non-existent paths or packages: adapter-br, adapter-ntm, agentrunner, hook, memory (packages don't exist); scenario (references non-existent agentrunner/orchestrator); handler-contract (wrong path — stub says `internal/handler/contract/**` but package is `internal/handlercontract/`).
- `tools/forbid-import/` does not exist; Makefile gracefully skips with note.

---

## Approach

**Bead 1 (T5a): Activate workspace, daemon, cmd depguard rules**

Uncomment these three rules in `.golangci.yml`:
- `workspace`: allow `[$gostd, internal/core]` — matches actual imports (workspace only imports core in prod files).
- `daemon`: allow `[$gostd, internal/...]` — wide allow; matches composition root pattern.
- `cmd`: allow `[$gostd, internal/core, internal/daemon]` — matches cmd/ imports.

Run `golangci-lint run` to verify clean. If violations surface, fix them before activating.

**Bead 2 (T5b): File eventbus architecture bead**

File a P2 bead: "eventbus imports handlercontract — resolve: either update depguard allow list OR refactor eventbus to core-only". This is a spec-drift issue outside T5 scope.

Options for the bead to resolve:
  a. Allow `internal/handlercontract` in eventbus depguard rule (pragmatic, matches code)
  b. Move `RedactionRegistry` and `DeadLetterSink` to `internal/core` (spec-correct, larger refactor)

T5 does NOT resolve this — it just files the bead.

**Bead 3 (T5c): File tools/forbid-import follow-up bead**

File a P3 bead: "implement tools/forbid-import/ OR remove Makefile graceful-skip message". The Makefile should not permanently say "not yet present" if the tool is never coming.

**Bead 4 (T5d): Fix handler-contract stub path mismatch**

The commented `handler-contract` stub points to `internal/handler/contract/**` but the package is `internal/handlercontract/`. Fix the comment path. (This is a comment fix, no functional change — can be bundled with T5a.)

**Bead 5 (T5e): Audit .golangci.yml disabled-linter comments**

Verify that each disabled linter (gosimple removed, etc.) has a comment explaining why. From reading the file, the comments appear complete. This is a quick review + no-op commit if comments are already sufficient.

---

## Files & Changes

- `.golangci.yml` — uncomment `workspace`, `daemon`, `cmd` depguard rules (Bead 1). Fix `handler-contract` stub path comment (Bead 4).
- No source code changes in T5.

---

## Acceptance Criteria

1. `golangci-lint run` exits 0 after `.golangci.yml` changes.
2. `workspace` depguard rule is active — a test file that imports `internal/daemon` from workspace would be flagged (can verify with a scratch import).
3. `daemon` depguard rule is active — `internal/daemon` cannot import packages outside `internal/...`.
4. Bead filed for eventbus architecture question (T5b).
5. Bead filed for tools/forbid-import (T5c).
6. `handler-contract` stub path comment updated to `internal/handlercontract/**`.

---

## Verification

```bash
golangci-lint run ./...   # must exit 0
golangci-lint run --out-format=line-number 2>&1 | grep -i "depguard"  # should be empty
```

---

## Bead Candidates

- hk-T5a: `T5: activate workspace/daemon/cmd depguard rules` (type: chore)
- hk-T5b: `eventbus imports handlercontract — resolve spec drift or update depguard allow` (type: task, P2)
- hk-T5c: `implement or remove tools/forbid-import Makefile stub` (type: task, P3)
