# C6 — Migration, back-compat & test harness — change spec

## Requirements (from 03-components.md)
R6.1 regression: no-selection bead/queue/workflow runs on claude byte-identically. R6.2 codex twin
(fake `codex` binary) emitting scripted JSONL + a `Refs:<bead>` commit. R6.3 operator docs + MUST-TEST
checklist (incl. #2000 org-key audit). R6.4 spec-first `specs/` artifact. R6.5 MUST-TEST codex
reviewer verdict path; else reviewer default falls back to claude.

## Research summary
harmonik already uses a **claude twin** binary for deterministic substrate tests
(`02-analysis.md`; `rc.handlerBinary` is opaque — `"claude"` or a twin). The codex adapter parses
JSONL, so a codex twin must emit `thread.started{thread_id}`, optional `item.*`, then
`turn.completed`, and make a worktree commit with the `Refs:<bead>` trailer — mirroring the claude-twin
pattern. The billing guard (C3) and `codex exec` env precedence are version-variable and undocumented
→ several items are MUST-TEST (empirical) rather than asserted in unit tests.

## Approach
- **Codex twin (R6.2):** a small Go/script `codex` twin that, given `exec [resume <id>] --json ...`,
  prints a scripted JSONL transcript to stdout (`thread.started` with a deterministic `thread_id`,
  a `turn.completed`), optionally makes a `Refs:<bead>` commit in `-C <worktree>`, and exits 0.
  Variants: commits-with-trailer / edits-no-commit / no-edits / `turn.failed` — to exercise C2 AC2.3–5.
  Lives beside the existing claude twin test fixtures.
- **Regression (R6.1):** a test that runs a no-selection bead and asserts `ClaudeHarness.LaunchSpec`
  argv/env/cwd equals the pre-change golden (C1 AC1.2) AND the daemon smoke produces the same event
  sequence as today.
- **Scenario + exploratory test beads (jig-required):** filed in this pass (IDs recorded below).
- **Operator docs (R6.3):** a doc covering `codex login` (subscription), the env-guard posture, the
  per-bead/queue/node/global selection surface, and the pre-production MUST-TEST checklist:
  1. does the pinned `codex exec` honor `OPENAI_API_KEY`/`CODEX_API_KEY`? (test with the key set)
  2. is `forced_login_method=chatgpt` honored by `codex exec`?
  3. audit the OpenAI org for a "Codex CLI (auto-generated)" key (#2000)
  4. does a codex reviewer reliably write the structured verdict? (R6.5)
- **Spec-first artifact (R6.4):** the normative `Harness` contract + selection precedence + billing
  guard published to `specs/` at the integration pass (`SPEC.md` → `specs/harness-contract.md`).

## Files & changes
- **NEW** test fixture: codex twin binary + its build wiring (mirror claude-twin location).
- **NEW** `internal/daemon/*_test.go` — codex twin scenario tests; regression golden test.
- **NEW (docs)** `docs/...` operator guide for the codex harness (path chosen at impl time).
- **NEW (spec)** `specs/harness-contract.md` (authored at integration; landed at impl).
- Test beads filed (below).

## Acceptance criteria
- AC6.1 Regression test passes: no-selection run is byte-identical-claude (argv golden + event seq).
- AC6.2 Codex twin scenario test passes for all four variants (trailer-commit / no-commit-fallback /
  no-edits-noChange / turn.failed).
- AC6.3 Operator doc exists and contains the 4-item MUST-TEST checklist.
- AC6.4 `specs/harness-contract.md` exists and matches the implemented interface (spec-first).

## Verification
```
go test ./internal/daemon/... -run 'CodexTwin|Regression|Golden'
# the scenario tests are //go:build scenario — run them explicitly (daemon gate skips them):
go test -tags scenario ./internal/daemon/... -run 'CodexScenario'
```

## Error handling / edge cases
- Twin must be deterministic (fixed `thread_id`, no `Date.now`/random) so scenario tests are stable.
- Scenario-test beads time out on the 30-min daemon commit budget if they boot real daemons → author
  via a worktree sub-agent + targeted fast gate (per `reference_scenario_test_authoring`), not the
  live daemon.

## Migration / back-compat
This component IS the back-compat proof (R6.1). Nothing here changes claude behavior.

---

## Jig-required test beads (filed this pass)
- **scenario:** `hk-vfmn9` — *scenario: codex-adapter — full lifecycle on twin substrate
  (run_started → reviewer_verdict → run_completed, Refs: commit on main)*
  (`--labels scenario-test,codename:codex-harness`). CLI under test: `harmonik queue submit` of a
  codex-selected bead against the twin substrate; terminal condition: a `run_completed` event +
  a `Refs:<bead>` commit on the target branch.
- **exploratory:** `hk-qxfj0` — *explore: codex selection — `harmonik queue dry-run` shows resolved
  harness per item (bead-label `harness:codex` overrides claude global default)*
  (`--labels exploratory-test,codename:codex-harness`). Command: `harmonik queue dry-run <file>`;
  expected side-effect: stdout/report names the resolved harness per item.
