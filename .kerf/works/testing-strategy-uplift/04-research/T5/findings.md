# T5 — Lint Audit + depguard Rule Activation: Research Findings

**Track:** T5 — Lint Audit + depguard Rule Activation  
**Date:** 2026-05-20  
**Status:** complete

---

## Research Questions

1. Which commented-out depguard stubs correspond to packages that NOW exist?
2. Would activating the stubs as-written cause immediate lint failures?
3. What is the status of `tools/forbid-import/`?
4. Are any active depguard rules currently generating violations?

---

## Findings

### Q1: Packages that exist vs. depguard stub state

Packages that exist in `internal/` (from `ls internal/`):
  branching, brcli, core, daemon, eventbus, handler, handlercontract,
  hookrelay, lifecycle, operatornfr, queue, release, scenario, specaudit,
  t5probe, testhelpers, workflowvalidator, workspace

Cross-referencing with `.golangci.yml` commented stubs:

| Stub Name | Package Path | Exists? | Can Activate As-Written? |
|-----------|-------------|---------|--------------------------|
| eventbus | internal/eventbus | YES | NO — imports handlercontract (not in allow list) |
| adapter-br | internal/adapter/br | NO | n/a — package doesn't exist |
| adapter-ntm | internal/adapter/ntm | NO | n/a — package doesn't exist |
| workspace | internal/workspace | YES | Likely YES — only imports core |
| agentrunner | internal/agentrunner | NO | n/a |
| hook | internal/hook | NO | n/a — internal/hookrelay exists but not hook |
| memory | internal/memory | NO | n/a |
| handler-impls | internal/handler/claudecode etc | PARTIAL (internal/handler exists) | Needs investigation |
| scenario | internal/scenario | YES | Needs investigation |
| daemon | internal/daemon | YES | Stub allows `internal/...` — low risk |
| cmd | cmd/ | YES | Low risk |
| policy | internal/policy | NO | n/a |
| handler-contract | internal/handler/contract | NO (internal/handlercontract exists) | Path mismatch |

### Q2: Violations if stubs activated as-written

**eventbus stub** (critical):
The stub allows: `[$gostd, internal/core]`
But `internal/eventbus/busimpl.go` imports: `internal/handlercontract`
(for `RedactionRegistry` and `DeadLetterSink`)

If activated as-written, golangci-lint will flag eventbus as violating its own depguard rule. Fix options:
  a. Update the stub's allow list to include `internal/handlercontract`
  b. Refactor eventbus to not import handlercontract (move RedactionRegistry to core)
  c. Defer activation of the eventbus stub until the import is resolved

The subsystem-organization.md design intent (eventbus = core only) is violated by the current code. This is a spec-drift issue. T5 must investigate which is correct — the spec or the code.

**workspace stub** (likely clean):
workspace imports only `internal/core` in prod files. The stub allows core. Should activate cleanly.

**daemon stub** (likely clean):
`allow: ["$gostd", "internal/..."]` — wide open; matches current daemon imports.

**cmd stub** (likely clean):
cmd only imports core + daemon per stub; likely matches actual usage.

### Q3: tools/forbid-import/ status

Makefile contains:
  `@if [ -f tools/forbid-import/main.go ]; then ... else echo "forbid-import not yet present, skipping"; fi`

`tools/forbid-import/` does not exist. The graceful skip message runs on every `make check`. This is low-priority noise. T5 requirement: file a follow-up bead rather than implement the tool within T5's scope.

### Q4: Current golangci-lint violations

Not measured in this research pass (would require running golangci-lint). From `02-analysis.md`, the active rules (beads-direct-access-ban, llm-sdk-ban, core, queue, handler-brcli-ban, lifecycle-tmux) are believed to be green (they would have blocked recent `harmonik run` commits). No new violations expected from the currently-active rules.

---

## Options and Tradeoffs

**eventbus rule fix approach:**

Option A: Add `internal/handlercontract` to eventbus allow list  
- Pros: matches actual code; activates rule immediately
- Cons: weakens the stated architectural isolation (eventbus was meant to be core-only)

Option B: Move RedactionRegistry to internal/core  
- Pros: restores the intended layering
- Cons: bigger refactor; out of T5 scope; requires separate bead

Option C: Defer eventbus rule activation, activate workspace/daemon/cmd only  
- Pros: safe, incremental
- Cons: eventbus layer violation persists undetected
- Verdict: RECOMMENDED for T5 — activate the clean rules, file a bead for the eventbus architecture question

**Recommended T5 scope:**
1. Activate `workspace`, `daemon`, `cmd` rules (low risk, packages exist, likely clean)
2. DO NOT activate `eventbus` until the handlercontract import question is resolved
3. File a separate bead for: eventbus architecture question (allow handlercontract OR refactor)
4. Note that adapter-br, adapter-ntm, agentrunner, hook, memory stubs cannot be activated (packages don't exist)
5. File follow-up bead for `tools/forbid-import/` instead of implementing
6. Check `handler-contract` stub — stub path is `internal/handler/contract/**` but the package is `internal/handlercontract/` — path mismatch, needs correction

---

## Risks and Unknowns

1. handler-contract stub has wrong path (`internal/handler/contract/` vs `internal/handlercontract/`) — activating it would silently scope to a path that doesn't exist
2. scenario stub's allow list references `internal/agentrunner` and `internal/orchestrator` — neither exists; stub cannot be activated as-written
3. Running golangci-lint on the full codebase may surface violations not caught in this research pass
