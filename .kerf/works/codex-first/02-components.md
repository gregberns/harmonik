# 02 — Components (Affected Areas)

> **Pass 2 (Decompose).** Autonomous back-fill from `_plan.md` §3 + DECISIONS D1/D3/D4.
> Requirements below state **what must be true after the change**, not how the text reads.

## Shape of this work

This is a **code-seam change with a thin normative surface**. The spec survey confirmed that
**no existing spec requirement pins either the isolation fence or the codex sandbox mode** — so
there is exactly **one** normative spec area to touch, plus **four code-change components** that
carry the research/design/task load. The table separates the two.

| Area | Kind | Prefix/Path | Role |
|------|------|-------------|------|
| `specs/harness-contract.md` | **AMEND** | **HN** | Record codex's local + `danger-full-access` posture as normative; cite the PI-015 sibling; defer uniform sandbox to ON-024 |
| A. isolation-fence | CODE | `substrate_select.go`, `workloop.go` | Drop the fail-closed remote-worker fence (D1) |
| B. codex-sandbox-posture | CODE | `codexlaunchspec.go` | Native sandbox OFF → `danger-full-access` (D3) |
| C. commit-path | CODE (confirm) | `codexcommit.go`, `dot_cascade.go` | Confirm daemon-fallback is the committer (no change) |
| D. codex-crew-staffing | CODE | Captain crew-start env path | Thread `HARMONIK_SUBSTRATE=codexdriver` (3d) |

## Affected existing specs

### `specs/harness-contract.md` — prefix **HN** (AMEND)

- **Change summary:** add HN-series requirement(s) codifying the codex host posture D1+D3 lock,
  so the new behavior has a normative home (today it lives only in code).
- **Requirements (what the spec must describe after the change):**
  1. Codex MUST be launchable **locally on the daemon host** (via `LocalRunner`), with **no**
     requirement to bind an enabled remote/ssh worker. The prior fail-closed isolation boundary
     is removed (D1).
  2. Codex's **native sandbox is OFF** — it runs `danger-full-access` on the host, uniform with
     Claude and with the **PI-015** precedent ("harness runs unsandboxed"). No `workspace-write`
     and no per-harness `writable_roots` carveout on the codex-exec path (D3).
  3. The `-c sandbox_mode` config-override is the required mechanism (NOT the `--sandbox` flag),
     because `codex exec resume` rejects `--sandbox`; the sandbox value is `danger-full-access`.
  4. The spec MUST cross-reference **ON-024** and state that a **uniform harmonik-level sandbox**
     is a separate, parallel workstream that supersedes the per-harness `danger-full-access`
     posture when it lands — this change MUST NOT be read as "security dropped."
- **Dependencies:** none blocking. This area DOCUMENTS the code change; it does not gate it.
  The amendment MUST NOT touch HN-022 (billing) or HN-006/HN-021 (credential-isolation).

## New Specs

None. No new spec file is warranted — the change has a normative home in the existing
`harness-contract.md`, and its behavior surface is too small to justify a standalone spec.

## Code-change components (carry research → design → tasks)

### A. isolation-fence (D1) — the linchpin for LOCAL codex
Three enforcement points keyed off the same `codexdriver` selection must permit the LocalRunner
fallback: `substrate_select.go:74` (`requireIsolationBoundary` /
`codexWorkerRoutingRunner{requireBoundary}` → false), the `codexWorkerRoutingRunner.Command` /
`refusedIsolationBoundaryArgv0:123` fall-through, and `workloop.go:3626`
(`deps.codexRequireIsolationBoundary` refusal + its now-always-false plumbing: `daemon.go:586`,
`main.go:1357`, `run.go:715`, `workloop.go:759,1198`).
**Must be true:** codexdriver with no worker → runs on `LocalRunner`; refused-argv0 diagnostic is
dead and removed; the inert `codexWorkerRoutingRunner`/SSHRunner routing seam MAY stay (marked
deprecated) — P3 owns its final removal (leaving it keeps this diff small).

### B. codex-sandbox-posture (D3)
`codexlaunchspec.go:230,236` — `sandbox_mode="workspace-write"` → `"danger-full-access"` on BOTH
argv branches; drop the writable-roots injection (`wrArg`, `codexWritableRootsArg`,
`codexExecWritableRoots`); delete the app-server twin helpers (`codexWorktreeWritableRoots`,
`codexGitCommonDir`, `substrate_select.go:286-320`) and their `WritableRoots` hook.
**Must be true:** both branches emit `danger-full-access`, no writable_roots; the `-c` mechanism
(not `--sandbox`) survives; the app-server `codexHeadlessSandbox` constant stays.

### C. commit-path (confirmation, no code change)
`ensureCodexRefsTrailer` (`codexcommit.go:204`), invoked per implement-node exit
(`dot_cascade.go:1938`), stages+commits codex's edits from OUTSIDE any sandbox and adds
`Refs:<bead>`. **Must be true:** acceptance accepts EITHER committer (codex self-commit under
danger-full-access OR the fallback); the fallback idempotently no-ops when HEAD already carries
the trailer.

### D. codex-crew-staffing (3d)
The reviewer stays claude (`reviewerSubstrate`, `substrate_select.go:76`); implement nodes
resolve harness via `harnessRegistry.ForAgent(...)` (`dot_cascade.go:1446`). **Must be true:** the
Captain's crew-start path threads `HARMONIK_SUBSTRATE=codexdriver` (verify or add); a Codex crew
implements on codex and reviews on claude.

## Dependency Map

The HN spec amendment DOCUMENTS the code change and does not gate it. Among code components the
landing order is **A → B → (C confirm) → D**: A (fence drop) must precede a meaningful B
live-proof because without A codex never launches locally; C is a read-only confirmation that
holds regardless; D (crew staffing) comes after A+B are proven on one bead. This mirrors
`_plan.md` §6 sequencing (Step 1 = A+B minimal diff, Step 2 = live proof, Step 3 = dead-code
delete, Step 4 = crew staffing).

## Goal → Area Traceability

- Goal 1 (local, no worker) → **A** + HN req 1.
- Goal 2 (danger-full-access both branches, no writable_roots) → **B** + HN reqs 2,3.
- Goal 3 (real bead end-to-end, Refs trailer, closed) → **A**+**B**+**C** (proof, not spec text).
- Goal 4 (exec/shell facet clear of EPERM) → **B** (removing the seatbelt removes the denial).
- Goal 5 (Codex crew staffed, offload split) → **D** + HN req 1.

No area is listed that isn't justified by a goal. Non-goals (containers, remote, uniform
sandbox, per-node harness config) are explicitly out and get no area.
