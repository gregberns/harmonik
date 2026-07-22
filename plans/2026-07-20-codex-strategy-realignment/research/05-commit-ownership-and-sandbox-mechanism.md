# 05 — Commit ownership & the true mechanism of the codex commit failure

Verification task under daemon/comms DOWN. Every claim is grounded in `file:line` / SHA / bead / run-log path. Code state = branch `phase1-session-restart-substrate` (HEAD chain includes `49d7fde3`).

---

## Bottom line (5 sentences)

1. In the DOT flow the **agent is the designed committer per node** (the implement-node seed prompt instructs codex to `git commit` with a `Refs:<bead>` trailer — `internal/daemon/codexlaunchspec.go:68`, `agentseedprompt.go:37`), and the daemon requires HEAD to advance after each implement node (`dot_cascade.go:1970,1979`), so commits are **per-node, not one end-of-bead commit**.
2. For process-exit harnesses (codex, pi) the daemon runs a **deterministic fallback** (`ensureCodexRefsTrailer`, `codexcommit.go:204`) that stages+commits codex's edits **outside the sandbox** when codex did not self-commit — designed as a *backstop* (`codexcommit.go:36-40`) but, because deployed codex 0.142.0 self-commit fails 100% under the sandbox (`dot_cascade.go:1933`), the fallback is the *de-facto primary* committer for codex today (proven: run `019f7bc5` HEAD `123b2ae` "auto-committed by daemon fallback", `LEG-B.md:135-137`).
3. So the operator's conclusion **holds for the commit dimension**: the daemon already lands codex's diff outside the sandbox, so "codex must self-commit ⇒ needs danger-full-access" is an **unnecessary** justification *for landing the commit* — but it is **only partially** exculpatory, because the same sandbox also blocks codex's own `exec_command` shell steps (tests, `git status`, `rm -rf`), which the commit-fallback does **not** rescue.
4. On mechanism, the evidence is unambiguous: the failure is a **kernel/Seatbelt filesystem EPERM** — "Operation not permitted … unable to index file … fatal: updating files failed" and `Unable to create '…/.git/worktrees/…/index.lock': Operation not permitted` (`LEG-B.md:64-68`, `LIVE-GATE-yankee.md:75`) — because the linked-worktree's `.git` common dir is **outside** the workspace-write writable root; it is **NOT** an approval-policy denial (`approval_policy=never` was already in force, so codex *ran* the command and it failed at the FS layer, `LIVE-GATE-yankee.md:24`).
5. `sandbox_mode=workspace-write` + `approval_policy=never` **is a valid, real combination and is exactly what the crew ran in practice** (`LIVE-GATE-yankee.md:24`); the correct fix is the **narrow, safe** `sandbox_workspace_write.writable_roots += <repo>/.git` carveout (now committed on both launch paths — `substrate_select.go:286`, `codexlaunchspec.go:152-172`, commit `49d7fde3`), which opens *only* the git object store and **not** network or full-FS/arbitrary-exec the way danger-full-access does.

---

## Q1 — WHO commits: daemon vs agent

### The DOT per-node model
`driveDotWorkflow` walks the graph node-by-node (`dot_cascade.go:5-40`). The relevant node classes:

- **Implement node (agentic, committing).** The seed prompt tells the agent to produce the commit itself:
  - codex initial: *"commit ALL your changes in a single git commit, and the commit message MUST include the line `Refs: %s`"* — `codexlaunchspec.go:68`.
  - codex/pi resume (DOT back-edge): *"commit ALL your changes in a single NEW git commit … You MUST produce a new commit: if HEAD does not advance, the workflow will loop back to you again."* — `agentseedprompt.go:37`.
  - The daemon then **enforces** a per-node commit: `if postHeadSHA == preHeadSHA && !node.NonCommitting … node "%q" (implementer) exited without advancing HEAD` is a hard fail on iteration 1 (`dot_cascade.go:1970,1977-1979`). So the design's contract is **agent-commits, one commit per implement node**.
- **Review node (agentic, NON-committing).** The reviewer writes a **verdict file** (`.harmonik/review.json`), not a git commit; the node outcome is derived from that file (`dot_cascade.go:16-20`, `1915-1930`). There is **no "review diff" commit** the agent must produce in the DOT review path. (The `commitVerdictEmitted/Executed` helpers in `verdictexecutor_rc025a.go:310,347` belong to the separate *reconciliation* workflow, not the DOT review node — and there the **daemon** commits.)
- **commit_gate node (deterministic).** A tool-command gate (`test -f counter.py`, or go build/vet/test) — no commit, just pass/fail (`dot_gate.go`, `LEG-B.md:9,37`).

### Primary vs fallback
- **Designed intent:** agent self-commits (happy path); the daemon fallback "only fires when codex disobeyed" (`codexcommit.go:36-40`). For claude (interactive TUI) the fallback is a genuine no-op — claude self-commits (`workloop.go` §"Fires only for CompletionProcessExit harnesses … no-op for claude").
- **The daemon fallback** (`ensureCodexRefsTrailer`, `codexcommit.go:204-255`), invoked per implement node after the process exits (`dot_cascade.go:1938-1950`; peer in the builtin loop at `workloop.go:5127-5145`), runs git **outside** the sandbox and:
  - HEAD already has trailer → no-op (agent self-committed).
  - HEAD advanced, no trailer → **amend** to add trailer.
  - HEAD unchanged, worktree dirty → **stage all + create** the trailer commit (`commitAllWithHarnessRefsTrailer`, `codexcommit.go:264-286`).
  - HEAD unchanged, clean → `no_change` → routed to the standard no-commit failure path.
- **Empirical reality on deployed codex 0.142.0:** codex self-commit **fails 100%** — the code itself asserts this: *"codex --sandbox workspace-write cannot commit inside a worktree (.git points outside the sandbox root → self-commit fails 100%)"* (`dot_cascade.go:1933`). Proven live: the diff lands **only** via the fallback — run `019f7bc5` worktree HEAD `123b2ae` = *"feat(codex): codex turn output (auto-committed by daemon fallback)"*, `counter.py: return 1 → return 2` (`LEG-B.md:135-137`); re-gate HEAD `cfcd2057 …(auto-committed by daemon fallback) Refs: yri-wq7` (`LIVE-GATE-yankee.md:71-72`). So for codex the "backstop" is the **de-facto primary** committer.
- **Per-node, not end-of-bead:** each implement node must advance HEAD (`dot_cascade.go:1970`); the fallback runs once per implement-node exit. There is no single end-of-bead commit — the daemon later *merges* the run branch to target (a distinct step).

### Conclusion on whether danger-full-access is even needed
**Partially true, leaning toward the operator.** For the narrow purpose the hk-daegv "codex must self-commit ⇒ danger-full-access" chain names — **landing the code diff** — danger-full-access is **not needed**: (a) the daemon fallback already commits codex's applied edits outside the sandbox (proven, above), and (b) even for codex's *own* commit, granting `<repo>/.git` as a workspace-write writable root is sufficient (least-privilege). The caveat on hk-g0ror.4 is exactly this — *"commits LAND inside boundary … CAVEAT: commit is daemon-FALLBACK auto-commit, not codex self-commit"* (`br show hk-g0ror.4`). **But it is not a clean "unnecessary":** the *same* workspace-write sandbox also blocks codex's own `exec_command` shell tool (`/bin/zsh -lc 'git status --short'` → CreateProcess "Operation not permitted", `LEG-B.md:48,68`, hk-daegv comment 19:20Z), so codex loses self-sufficiency for tests / multi-step git even though the final diff is rescued. That second facet is a reason to widen codex's execution posture that is **independent** of who commits — but it is *not* solved by danger-full-access-for-commit framing either, and the project has already pivoted away from danger-full-access (below).

---

## Q2 — the true mechanism of the failure

### Exact error evidence (quoted)
From the codex rollout log on the worker (`LEG-B.md:62-68`, run `019f7bbc`, codex-cli 0.142.0):
```
Operation not permitted
error: counter.py: failed to insert into database
error: unable to index file 'counter.py'
fatal: updating files failed
exec_command failed for `/bin/zsh -lc '…'`: CreateProcess { … Operation not permitted
```
And the sharper `.git` locus (`LIVE-GATE-yankee.md:75`, run `019f7c46`):
```
git add && git commit → exit 128,
Unable to create '…/.git/worktrees/019f7c46-…/index.lock': Operation not permitted
```
The posture codex **actually ran** (rollout `environment_context`, `LIVE-GATE-yankee.md:24-26`, `LEG-B.md:61`):
`sandbox_mode = workspace-write`, `approval_policy = never`, `network_access = false`, **writable roots = [worktree cwd, /private/tmp]** — `<repo>/.git` **ABSENT**; `<worktree>/.git` listed **read-only** (`LIVE-GATE-yankee.md:74`).

**Adjudication:** this is a **kernel/Seatbelt filesystem EPERM** on writing `.git` (index.lock, object insert), *not* a codex "command requires approval / not permitted by approval policy" message. `approval_policy=never` was already set, so codex was *allowed to execute* `git commit` and *did* — the command ran and failed at the FS layer. **The operator's hypothesis (approval-execution issue, not FS) is REFUTED by the evidence.** The wall is the writable-root boundary.

### The two independent axes (confirmed from repo plumbing)
- **`sandbox_mode`** = FS / network / exec confinement (the Seatbelt profile codex applies). Values: `read-only | workspace-write | danger-full-access`. Under `workspace-write` only the declared writable roots are writable; network off; `.git` outside the root ⇒ EPERM.
- **`approval_policy`** = whether codex **pauses to ask a human** before running a command or escalating out of the sandbox. Values: `untrusted | on-failure | on-request | never`. Harmonik is headless and **auto-declines** any approval request (`driver.go:130-136`), so it must use `never` or writes never land.

Harmonik's set values:
- Composition root: `codexHeadlessSandbox = "danger-full-access"`, `codexHeadlessApprovalPolicy = "never"` (`cmd/harmonik/substrate_select.go:131-132`), stamped as `Sandbox`/`ApprovalPolicy` on the driver (`substrate_select.go:252-253`) and carried on the app-server `thread/start`/`thread/resume` wire via `postureExtra` (`internal/codexdriver/session.go:802-813`).
- App-server args also carry `-c sandbox_mode="danger-full-access"` (`substrate_select.go:240`).
- **codex-exec path** (the path implement nodes actually take): `sandbox_mode="workspace-write"` **plus** `-c sandbox_workspace_write.writable_roots=[<worktree>,<repo>/.git]` (`codexlaunchspec.go:230-238`, `codexExecWritableRoots` `:152-162`, `codexWritableRootsArg` `:164-173`). This is the committed least-privilege fix (`49d7fde3`).

### The three direct answers

**(a) FS-sandbox, approval, or both?**
**Fundamentally an FS-sandbox writable-root problem**, with an approval-axis interaction. The seatbelt EPERM on `.git` writes is a kernel FS denial (evidence above). The approval axis matters only for *recoverability*: with `approval_policy=on-failure/on-request` an interactive codex would surface a "command failed in the sandbox — retry without sandbox?" escalation a human could approve; under harmonik's headless `approval_policy=never` that escalation is disabled, so the FS denial is **fatal**. Net: **FS-denial that is unrecoverable specifically because approval=never** — but the root wall is FS, not approval. Fixing approval alone would not help a headless fleet (no human to approve); fixing the writable root does.

**(b) Can `workspace-write` + `approval=never` run together, and is it wired in harmonik?**
**Yes on both.** It is a valid codex combination and is *exactly what the crew ran in practice* (`LIVE-GATE-yankee.md:24`: `sandbox_mode=workspace-write, approval_policy=never`). It is the "real FS sandbox, no human prompting" (Claude-bypass-style) posture. It is wired in harmonik today on the codex-exec launch path (`codexlaunchspec.go:230,236` set `workspace-write`; `ApprovalPolicy="never"` at `substrate_select.go:132`). The *only* gap that made it fail was the missing `.git` writable root, now added (`49d7fde3`). (Note a live tension: the app-server driver path still stamps `danger-full-access` as forward-intent — `substrate_select.go:240,252` — while the exec path harmonik actually uses is `workspace-write`+writable_roots. The realignment can safely drop the danger-full-access forward-intent.)

**(c) How narrow/dangerous is granting `<repo>/.git` as a writable_root?**
**Very narrow and safe — a tight carveout.** It adds exactly one path — the repo's git common dir `<repo>/.git` (object store, refs, `worktrees/<id>/` incl. `index.lock`) — derived deterministically from the linked-worktree layout (`codexGitCommonDir`, `substrate_select.go:306-320`; `codexExecWritableRoots`, `codexlaunchspec.go:152-162`). It does **not** open network (`network_access=false` stays), does **not** open the full filesystem, and does **not** grant arbitrary out-of-cwd writes beyond `.git`. Contrast **danger-full-access**, which removes the Seatbelt entirely: full FS + network + arbitrary exec (`driver.go:135-142`, guard rationale `workloop.go:3632` "would run unsandboxed on the daemon host"). The writable-roots grant is strictly tighter and is why yankee pivoted to it: *"Pivot from force-danger-full-access (dead-end on installed codex 0.142.0 headless) to least-privilege: per-thread runtimeWorkspaceRoots=[worktreeCwd,<repo>/.git]"* (`br show hk-daegv`, comment 21:22Z).

### Live-verification status (flag: PARTIALLY UNVERIFIED)
- The **app-server** `.git`-root fix (`44831898`) was proven **inert on the runs that failed**: the implement node ran via `codex exec`, not the app-server `thread/start` path where the root was stamped, so `<repo>/.git` never reached the sandbox (`LIVE-GATE-yankee.md:70-79`); and on the remote path the grant wasn't forwarded at all (`LIVE-GATE-yankee.md:21-33`).
- The **codex-exec** `.git`-root fix (`49d7fde3`, `codexlaunchspec.go`) is **NEWER than every failing gate log reviewed** (all failures ran binaries `9db85569` / `44831898` / `fff3d937`). **`commit_landed=TRUE` live proof is still owed** (hk-daegv is OPEN, `br show hk-daegv`; comment 22:17Z "Fix planned: stamp the same writable roots onto the codex-exec launch path"). So the least-privilege fix is *coded and committed* but **not yet live-verified** on the deployed 0.142.0.
- **Separate open facet (UNVERIFIED root cause):** codex `exec_command` shell spawn failures (`/bin/zsh -lc … CreateProcess { … Operation not permitted }`, `LEG-B.md:48,68`) are a *distinct* seatbelt exec denial not addressed by the `.git` writable-root grant; the daemon commit-fallback masks its effect on *landing the diff* but not on codex running its own tests/multi-step git (hk-daegv comment 19:20Z). Whether the exec-spawn denial is fixed by the same posture is **not yet proven**.

---

## Classification table

| # | Finding | Evidence | Class |
|---|---------|----------|-------|
| 1 | DOT design contract = **agent self-commits, one commit per implement node** | seed prompts `codexlaunchspec.go:68`, `agentseedprompt.go:37`; HEAD-advance enforce `dot_cascade.go:1970,1979` | ACTUAL-LIMITATION (of the design contract) |
| 2 | Daemon fallback commits codex's edits **outside the sandbox**, per implement node | `codexcommit.go:204-286`; `dot_cascade.go:1938-1950`; `workloop.go:5127-5145` | ACTUAL-LIMITATION (mechanism exists & works) |
| 3 | For deployed codex 0.142.0, codex self-commit **fails 100%** → fallback is de-facto primary | `dot_cascade.go:1933`; run `019f7bc5` HEAD `123b2ae` `LEG-B.md:135-137`; `cfcd2057` `LIVE-GATE-yankee.md:71` | ACTUAL-LIMITATION |
| 4 | Review node produces a **verdict file, not a commit** — no per-node "review diff" commit expected of the agent | `dot_cascade.go:16-20,1915-1930` | ASSUMPTION-CORRECTION (removes a supposed reason codex must commit) |
| 5 | "codex must self-commit ⇒ needs danger-full-access" for **landing the diff** | daemon fallback lands it; hk-g0ror.4 caveat "daemon-FALLBACK, not codex self-commit" | ASSUMPTION (false for the commit dimension) |
| 6 | The commit-wall is a **Seatbelt FS EPERM** on `.git`, not an approval denial | `LEG-B.md:64-68`; `LIVE-GATE-yankee.md:24,75` | ACTUAL-LIMITATION |
| 7 | Operator's "it's a command-approval, not FS" hypothesis | `approval_policy=never` already set, command ran & failed at FS layer `LIVE-GATE-yankee.md:24` | ASSUMPTION (refuted) |
| 8 | `workspace-write` + `approval=never` is valid and is what the crew actually ran | `LIVE-GATE-yankee.md:24` | ACTUAL (valid combination) |
| 9 | Headless `approval=never` (auto-decline) makes the FS denial **unrecoverable** (no human to escalate) | `driver.go:130-136` | SELF-IMPOSED-CONSTRAINT (required by headless operation) |
| 10 | `danger-full-access` posture on the codex crew | `substrate_select.go:131,240,252`; guard `workloop.go:3632` | SELF-IMPOSED-CONSTRAINT (over-broad; superseded by writable-roots) |
| 11 | `<repo>/.git` writable-root carveout = tight & safe (no net/full-FS/exec) | `substrate_select.go:286-320`; `codexlaunchspec.go:152-173`; commit `49d7fde3` | ACTUAL (the correct least-privilege fix) |
| 12 | codex-exec `.git`-root fix **committed but not yet live-verified** on 0.142.0; commit_landed=TRUE owed | hk-daegv OPEN `br show hk-daegv` comment 22:17Z; `49d7fde3` newer than all failing gates | ASSUMPTION (pending verification) |
| 13 | `exec_command` shell-spawn seatbelt denial is a **separate** unfixed facet | `LEG-B.md:48,68`; hk-daegv comment 19:20Z | ACTUAL-LIMITATION (root cause UNVERIFIED) |
