# 05 — Spec-Draft Changelog

> Pass 5 (Spec Draft). One additive AMENDMENT to an existing spec. No new spec files.
> Back-filled from a design-complete plan; grounded in DECISIONS D1/D3.

## Drafts produced (under `05-spec-drafts/`)

### 1. AMENDMENT — `specs/harness-contract.md` (prefix **HN**) — codex host posture
- Adds **§4.10 Codex host posture** with two new requirements:
  - **HN-025** — Codex MUST be launchable on the local host; no remote/ssh isolation-boundary
    worker is a launch precondition; supersedes the removed `CodexRequireIsolationBoundary`
    fail-closed rule (D1).
  - **HN-026** — Codex runs unsandboxed on the host (`danger-full-access`): `sandbox_mode =
    danger-full-access` on both argv branches, no `workspace-write`, no `writable_roots`, applied
    via the `-c` override (not `--sandbox`, which resume rejects). Cites PI-015 (sibling
    unsandboxed precedent) and defers the uniform sandbox to ON-024's parallel workstream (D3).
- **Additive** (satisfies HN-023): no existing requirement renamed or removed. HN-006/HN-021
  (credential) and HN-022 (billing) untouched.
- **Landing action:** IDs HN-025/HN-026 are the next free after HN-024; confirm free against
  `specs/_registry.yaml` at `kerf finalize`. Prefix HN already reserved (spec-id `harness-contract`,
  status draft) — no registry reservation needed, only the ID confirmation.

## Not drafted (deliberate)
- **No new spec file.** The behavior's home is the existing harness-contract.md; its surface is too
  small to justify a standalone spec (02-components §New Specs).
- **Code seams A–D** (`04-design/code-seams-design.md`) produce no spec text — they are the concrete
  edits a crew makes; captured for the Tasks pass, not the spec corpus.

## Validation/acceptance test beads — DEFERRED (not created)
The spec-draft pass normally files scenario-test + exploratory-test beads. They are **deliberately
NOT created here**: (a) the daemon is DOWN and this project's standing guidance is "no beads while
daemon off" (work tracked in the plan/COORD, not beads); (b) the operator REFRAMED verification for
this work (`_plan.md` §7.3) away from bead-shaped proof to **two layers — heavy in-crew subagent
testing during implementation + an assessor complete-system test as the gate**. The equivalent
acceptance is fully specified in `_plan.md` §5 (crit. 1–7) and §6 (Step 2 live-bead smoke +
assessor sign-off). When the daemon is back and a crew is staffed, the Captain designs that
testing; it need not be bead-shaped. This is a genuine human/operator call, surfaced not invented.
