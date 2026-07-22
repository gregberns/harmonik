# 04 — Change Design: codex-sandbox-posture (component B / D3)

> Pass 4. See also `code-seams-design.md` §B and research
> `03-research/codex-sandbox-posture/findings.md`.

## Current state
`codexlaunchspec.go:230,236` emit `-c sandbox_mode="workspace-write"` + a `.git` writable-roots
carveout (`wrArg`/`codexWritableRootsArg`/`codexExecWritableRoots`) on both the initial and resume
argv; the app-server twin injects `codexWorktreeWritableRoots`/`codexGitCommonDir`
(`substrate_select.go:286-320`).

## Target state
Change the value to `-c sandbox_mode="danger-full-access"` on BOTH branches; delete the
writable-roots derivation (exec + app-server helpers) as dead code; KEEP the `-c` mechanism (not
`--sandbox`, which `codex exec resume` rejects) and the app-server `codexHeadlessSandbox` constant
(already danger-full-access). Add a `TestBuildCodexLaunchSpec` asserting `danger-full-access`, no
`--sandbox`, no `writable_roots` on both branches.

## Rationale
D3: seatbelt gone ⇒ both the `.git` commit write and codex's own `exec_command` shell steps are
permitted; the prior EPERM denials were workspace-write writable-root artifacts (research/05
F1/F2/F3). `49d7fde3` is superseded, not reverted — the `-c` structural fix survives.

## Requirements traceability
02 area B "must be true" → this target state. Normative home: HN-026 (S1, S2).
