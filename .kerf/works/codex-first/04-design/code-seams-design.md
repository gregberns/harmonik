# 04 — Change Design: code seams (A/B/C/D)

> Pass 4. Current→target for the four code-change components. This is the real behavioral design;
> the HN amendment documents it. Sourced verbatim-of-intent from `_plan.md` §3, DECISIONS D1/D3.
> These are NOT new specs — they are the concrete edits a crew will make; captured here so the
> Tasks pass (07) can decompose them.

## A. isolation-fence (D1)
**Current:** `codexdriver` selection forces `requireIsolationBoundary=true` and a
`codexWorkerRoutingRunner{requireBoundary:true}` (`substrate_select.go:74`); with no ssh worker
bound the runner emits `refusedIsolationBoundaryArgv0` (`substrate_select.go:123`); `beadRunOne`
independently refuses via `deps.codexRequireIsolationBoundary` (`workloop.go:3626`), fed from
`Config.CodexRequireIsolationBoundary` (`daemon.go:586`, set `main.go:1357`, `run.go:715`).
**Target:** `requireIsolationBoundary=false` and `requireBoundary:false` → a nil/disabled/non-ssh
registry state falls through to `LocalRunner`. Remove the `refusedIsolationBoundaryArgv0`
diagnostic + doc block (dead). Remove the `workloop.go:3626` guard block and the now-always-false
`codexRequireIsolationBoundary` plumbing (Config field, `workloop.go:759,1198`, both call sites).
Leave `codexWorkerRoutingRunner`/SSHRunner **inert, marked deprecated** (P3 removes it).
**Result:** `HARMONIK_SUBSTRATE=codexdriver` + no worker → codex on `LocalRunner`, host-local.

## B. codex-sandbox-posture (D3)
**Current:** `codexlaunchspec.go:230,236` emit `-c sandbox_mode="workspace-write"` + a `.git`
writable-roots carveout (`wrArg`/`codexWritableRootsArg`/`codexExecWritableRoots`) on both the
initial and resume argv; the app-server twin injects `codexWorktreeWritableRoots`/`codexGitCommonDir`
(`substrate_select.go:286-320`).
**Target:** change the value to `-c sandbox_mode="danger-full-access"` on BOTH branches; delete the
writable-roots derivation (exec + app-server helpers) as dead code; **keep** the `-c` mechanism
(not `--sandbox`) and the app-server `codexHeadlessSandbox` constant (already danger-full-access).
Add a `TestBuildCodexLaunchSpec` asserting `danger-full-access`, no `--sandbox`, no `writable_roots`
on both branches.
**Result:** uniform unsandboxed host posture; seatbelt gone ⇒ both the `.git` commit write and
codex's own `exec_command` shell steps are permitted (research/05 F1/F2/F3).

## C. commit-path (confirmation, no change)
**Current == Target:** `ensureCodexRefsTrailer` (`codexcommit.go:204`, per implement-node exit
`dot_cascade.go:1938`) commits codex's edits from outside any sandbox with a `Refs:<bead>` trailer;
de-facto committer today. Under danger-full-access codex may self-commit; the fallback stays as an
idempotent backstop. Acceptance accepts EITHER committer. No edit.

## D. codex-crew-staffing (3d)
**Current:** reviewer hard-coded to claude (`reviewerSubstrate`, `substrate_select.go:76`);
implement harness resolves via `harnessRegistry.ForAgent(...)` (`dot_cascade.go:1446`). The
Captain crew-start env path may or may not thread `HARMONIK_SUBSTRATE=codexdriver`.
**Target:** verify the crew-start path threads `HARMONIK_SUBSTRATE=codexdriver`; add it if absent.
Keep codex-implements / claude-reviews. (De-hard-coding per-node harness = out-of-scope fast-follow.)
**Result:** Captain can staff a Codex crew that implements on codex, reviews on claude.

## Requirements traceability
- 02 area A → this §A; 02 area B → §B; 02 area C → §C; 02 area D → §D. Every 02 code-component
  "must be true" is realized by exactly one target state above; no target lacks a backing
  requirement. Sequencing (A→B→C→D) matches `_plan.md` §6.
