# 04 — Change Design: `specs/harness-contract.md` (HN) amendment

> Pass 4. Design of the ONLY normative spec change. Grounded in
> `03-research/harness-contract/findings.md`, `02-components.md`, DECISIONS D1/D3.

## Current state
`specs/harness-contract.md` (prefix HN, status draft) governs harness billing/credential posture
(HN-006, HN-021, HN-022) and harness selection (HN-012). It says **nothing** about: whether codex
may run locally vs. must bind a remote worker, or codex's native sandbox mode. Those behaviors
live only in code (the fail-closed fence; `sandbox_mode=workspace-write` + `.git` carveout). No
requirement pins either (research F1/F2).

## Target state
Add a small HN-series block (next free IDs; verify `specs/_registry.yaml` at draft) stating:

1. **Local-first launch (D1).** Codex MUST be launchable on the local daemon host via the
   local runner, with NO requirement to bind an enabled remote/ssh worker. Harmonik MUST NOT
   fail closed on codex launch for lack of an isolation-boundary worker.
2. **Native sandbox off / uniform host posture (D3).** Codex MUST run unsandboxed on the host
   (`danger-full-access`), uniform with the unsandboxed Claude posture and with the **PI-015**
   precedent. The codex-exec argv MUST NOT carry `workspace-write` or a per-harness
   `writable_roots` carveout.
3. **Mechanism (D3).** The sandbox mode MUST be set via the `-c sandbox_mode` config override,
   NOT the `--sandbox` flag, because `codex exec resume` rejects `--sandbox`.
4. **Security deferral (non-drop).** A NOTE MUST cross-reference **ON-024** stating that a
   uniform harmonik-level sandbox is a separate, parallel workstream that supersedes this
   per-harness posture when it lands; "native sandbox off" MUST NOT be read as "security dropped."

## Rationale
- Reqs 1–3 give the code-only behavior a normative home (research F3); no existing requirement is
  contradicted (research F1/F2), so this is purely additive.
- Req 2 cites PI-015 because it is the tree's existing "harness runs unsandboxed on host" precedent
  (research F4) — codex becomes uniform, not exceptional.
- Req 4 anchors on ON-024 (research F5) to keep the deferral explicit and auditable, matching D3's
  "security re-homed, not dropped."

## Requirements traceability (02 → target)
- 02 HN-req 1 → target req 1. 02 HN-req 2 → target req 2. 02 HN-req 3 → target req 3.
- 02 HN-req 4 → target req 4.
- Untouched: HN-006/HN-021 (credential), HN-022 (billing) — no target state edits them.

## Non-contradiction
No conflict with the code-seam design (`code-seams-design.md`): the spec target states exactly the
behavior those seams produce (local runner permitted; `danger-full-access`; `-c` mechanism).
