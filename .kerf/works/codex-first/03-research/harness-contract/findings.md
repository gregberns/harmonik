# Research — `specs/harness-contract.md` (HN) normative surface

> Source: a fresh survey of `specs/` (this work's Pass-3 survey) + `specs/_registry.yaml`.
> Point-to, not re-derive.

## Questions
1. Does any existing spec requirement pin the codex "fail-closed / must-bind-remote-worker" rule?
2. Does any spec pin codex's native sandbox mode (`workspace-write` / `danger-full-access`)?
3. What is the natural normative home for the new local + `danger-full-access` posture?
4. Is there a sibling precedent for "a harness runs unsandboxed on the host"?
5. What must NOT be disturbed in that file?

## Findings (with evidence)
- **F1 — the fence is NOT in any spec.** Grep of `specs/` for `CodexRequireIsolationBoundary` /
  `requireIsolationBoundary` / `codexWorkerRouting` / `refusedIsolation` = **zero hits**. The
  fail-closed rule exists ONLY in code. No HN/EM/PL/CI/HC requirement says codex MUST run remote
  or fail closed when unbound. (Note: HN-012 "fail-closed resolver" is about harness *selection*
  precedence, unrelated.)
- **F2 — the sandbox mode is NOT pinned.** Strings `danger-full-access`, `workspace-write`,
  `sandbox_mode`, `writable_roots` appear **nowhere** in `specs/`. No requirement dictates the
  codex-exec posture or the `.git` carveout. There is no codex-adapter / codexlaunchspec spec.
- **F3 — home = `specs/harness-contract.md` (prefix HN, status draft).** The closest governing
  requirement on the edited file (`codexlaunchspec.go`) is **HN-022** (codex MUST run on the
  ChatGPT-subscription billing path) — orthogonal to sandbox; both changes just live in the same
  launch-spec builder. A new HN-series requirement is the natural landing for the posture.
- **F4 — sibling precedent: PI-015** (`specs/pi-harness.md`): "The harness MUST NOT pass a
  `--sandbox` flag (Pi is unsandboxed)." This is the tree's one true per-harness unsandboxed-on-
  host requirement; the codex change makes codex uniform with it. Cite it.
- **F5 — the deferral anchor: ON-024** (`specs/operator-nfr.md` §4.7, "Command-execution sandbox
  invariant"), referenced by workspace-model §7. This is the harmonik-level sandbox invariant the
  *parallel* uniform-sandbox workstream engages (D3; FOLLOWUP-TOPICS §A). The amendment must point
  here so "native sandbox off" is not read as "security dropped."

## Patterns to follow
- Requirements-first HN idiom; RFC2119 language; assign IDs from the next free HN number
  (verify against `specs/_registry.yaml` at draft time).
- Keep HN-006/HN-021 (credential guard) and HN-022 (billing) untouched — same file, distinct
  concern.

## Risks / conflicts
- None normative (nothing to contradict). Only risk is **omission** — if the posture is not
  recorded in HN, the local + danger-full-access behavior stays a code-only fact with no spec
  home, which is the gap this amendment closes.
