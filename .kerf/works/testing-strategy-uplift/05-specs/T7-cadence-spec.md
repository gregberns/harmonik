# Change Spec: T7 — Cadence: make test-all + Lefthook + Session Checklist

**Component:** T7  
**Date:** 2026-05-20  
**Research:** 04-research/T7/findings.md

---

## Requirements (from 03-components.md)

1. `make check-full` serves as `make test-all` — alias or document.
2. `make check-full` output includes a coverage-delta summary line.
3. Confirm `lefthook run pre-push` calls `make check`.
4. Add per-session checklist to HANDOFF.md template or AGENTS.md.
5. Local only, no GitHub Actions. Makefile/CI parity invariant maintained.

---

## Research Summary

- `make check-full` already defined (Tier 3: check + integration + scenario + crash).
- Gate script does NOT emit a delta summary line today.
- Lefthook is CONFIGURED but unverified (pre-push hook will fail on floor gates after T6).
- Session checklist belongs in AGENTS.md (agent-facing doc).

---

## Approach

**Change 1: make test-all alias (Makefile)**

Add to Makefile after the check-full target:
```makefile
.PHONY: test-all
test-all: check-full  ## Alias for check-full: run all test tiers
```

**Change 2: Coverage delta line in scripts/coverage-gate.sh**

After computing all per-package coverages, compute:
- `TOTAL_CURRENT` = weighted average of all measured packages
- `TOTAL_BASELINE` = weighted average of baseline entries
- Emit: `coverage delta vs baseline: +X.Xpp` (or `-X.Xpp` for regressions)

Implementation: add ~10 lines of bash to the existing script's summary section. Use integer arithmetic (multiply by 10, divide) for pp precision without floating-point.

**Change 3: Document lefthook pre-push as gate**

No code change. Add a note to AGENTS.md §"Session end" that `lefthook run pre-push` (or equivalently `make check`) is the expected pre-session-end check. Note that after T6, this will fail on absolute coverage floors — this is expected and intentional signal, not a blocker for session-end.

**Change 4: AGENTS.md session checklist addition**

Add to AGENTS.md:
```
## Session End Checklist

Before ending a session:
1. Run `make check` (Tier 2 gate). Failures on absolute coverage floors are expected until testing-gap beads land.
2. Run `br sync --flush-only` to export bead state.
3. Commit all staged changes.
4. Run `kerf triage --ack` if kerf work is in progress.
5. Write or update `HANDOFF.md` via `/session-handoff`.
```

---

## Files & Changes

- `Makefile` — add `make test-all` alias (3 lines)
- `scripts/coverage-gate.sh` — add delta summary line (~10 lines in summary section)
- `AGENTS.md` — add Session End Checklist section

---

## Acceptance Criteria

1. `make test-all` is a valid target and runs all tiers (equivalent to `make check-full`).
2. `scripts/coverage-gate.sh` output includes a line matching `coverage delta vs baseline: [+-]\d+\.\dpp`.
3. `AGENTS.md` contains a "Session End Checklist" section with the 5-item list.
4. `lefthook.yml` is unchanged (pre-push already configured).

---

## Verification

```bash
make test-all 2>&1 | head -5  # must start running check-full
scripts/coverage-gate.sh 2>&1 | grep "delta"  # must show delta line
grep -n "Session End Checklist" AGENTS.md  # must find the section
```

---

## Dependencies

- T6 (delta output requires a populated baseline)

---

## Bead Candidates

- T7: `T7: make test-all alias + coverage delta line + session checklist in AGENTS.md` (type: chore)
