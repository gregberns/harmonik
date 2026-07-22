# Plan — Codex-first: make Codex a reliable local bead-runner (P0)

**Status:** kerf-ready. Daemon/comms DOWN — this is design + code-change spec, not an execution log.
**Owner on execution:** Captain (take through kerf, then staff a crew).
**Priority:** Priority-0 (operator, `plans/2026-07-21-platform-architecture/DECISIONS.md` §PRIORITY-0).
**Grounded in:** `plans/2026-07-20-codex-strategy-realignment/DECISIONS.md` (D1, D3, D4) and `research/05`, `research/06`.

---

## 1. Goal + why now

Make **local Codex** run a bead end-to-end through the **existing DOT flow** — implement → commit → review → close — reliably, with **no ssh and no per-harness native sandbox**. Once local Codex reliably clears beads, the Captain can staff Codex implementer crews to carry the P1/P2/P3 platform work, which **conserves Claude tokens**. Codex is both the first thing to get working AND the harness the other crews should run on. This is the operator's stated top priority: token-offload is the enabler for everything else.

The realignment already did the hard analytical work (D1–D4). This plan is the **simple, ASAP execution of those locked decisions** — it flips a small number of code seams and proves the result on one real bead. No new architecture.

---

## 2. Scope

**IN**
- Local Codex as an implementer harness (runs on the daemon host, exactly like Claude does today).
- Native sandbox OFF → `danger-full-access`, uniform with Claude's host posture (D3).
- No ssh / no remote worker requirement (D4; drop the D1 fail-closed fence).
- The **daemon commits the diff** — the existing `ensureCodexRefsTrailer` fallback is the reliable committer (`research/05`); codex self-commit is a bonus, not a requirement.
- Run through the **existing DOT flow** (implement node → commit_gate → review node → close). No DOT changes.
- Codex implementer crews selectable/staffable by the Captain.

**OUT (explicit)**
- **Containers** — that's P3 (platform-architecture C5).
- **Remote / multi-machine execution** — P3; ssh-per-node is scrapped (D4), replacement designed in `plans/2026-07-21-platform-architecture/`.
- **Scheduling / leases / orphan-recovery** — P3 (platform C5).
- **The uniform harmonik-level sandbox** — a SEPARATE PARALLEL workstream (D3; `FOLLOWUP-TOPICS.md` §A). We run `danger-full-access` now; secure-by-default is designed and landed later, in parallel, and must not block this.

---

## 3. The concrete changes

Three seams flip. All are named to `file:line` from the research + a fresh read of the tree on `phase1-session-restart-substrate`.

### 3a. Drop the fail-closed isolation fence (D1) — the linchpin for LOCAL codex

The fence refuses to launch codex unless an enabled ssh worker is bound; today that makes local codex impossible. Three enforcement points, all keyed off the same `codexdriver` selection — flip them so the LocalRunner fallback is permitted:

1. `cmd/harmonik/substrate_select.go:74` — `selectSubstrate` returns `requireIsolationBoundary = true` and constructs `codexWorkerRoutingRunner{requireBoundary: true}` on the codexdriver path. Set both to `false` (local fallback allowed).
2. `cmd/harmonik/substrate_select.go` — `codexWorkerRoutingRunner.Command` (and `refusedIsolationBoundaryArgv0`, line 123): with `requireBoundary=false`, a nil/disabled/non-ssh registry state now falls through to `LocalRunner` instead of the refused-argv0 binary. The refused-argv0 diagnostic becomes dead code — delete it and its doc block.
3. `internal/daemon/workloop.go:3626` — the `deps.codexRequireIsolationBoundary` refusal in `beadRunOne`. Fed by `Config.CodexRequireIsolationBoundary` (`daemon.go:586`), set at `cmd/harmonik/main.go:1357` and `run.go:715` from `codexRequireBoundary`. Once `selectSubstrate` returns `false`, this whole guard block is inert; remove the guard block and the now-always-false plumbing (`codexRequireIsolationBoundary` field `workloop.go:759,1198`; `Config` field; the two composition-root call sites).

**Net:** `HARMONIK_SUBSTRATE=codexdriver` with no worker bound → codex runs on `LocalRunner`, on the daemon host, like Claude. The SSHRunner routing seam (`codexWorkerRoutingRunner`) can stay for now (it's inert with no registry) or be removed as cleanup — removing it is aligned with D4 (ssh scrapped) but is NOT required for this plan; recommend leaving it inert to keep the diff small, and letting the platform-architecture P3 work delete it.

### 3b. Turn the native sandbox OFF on the codex-exec path → `danger-full-access` (D3)

The implement node runs `codex exec` via `buildCodexLaunchSpec` (`internal/daemon/codexlaunchspec.go`; `research/05` confirms this is the path implement nodes take). Today both argv branches (initial + resume) emit `-c sandbox_mode="workspace-write"` plus the `.git` writable-roots carveout (49d7fde3). Change:

- `internal/daemon/codexlaunchspec.go:230,236` — change `sandbox_mode="workspace-write"` → `sandbox_mode="danger-full-access"` on BOTH the initial and resume branches.
- Drop the writable-roots injection (`wrArg`, `codexWritableRootsArg`, `codexExecWritableRoots`) — under `danger-full-access` there is no seatbelt, so writable_roots is inert (`research/06` §"Reconcile with danger-full-access": moot when the path runs danger-full-access).
- **Keep the structural fix from 49d7fde3**: use the global `-c` override, NOT the `--sandbox` flag. `codex exec resume` rejects `--sandbox`/`--add-dir`/`-C` (arg-parse error); only `-c` is accepted on resume. This part of 49d7fde3 is a real bug fix and must survive.

The app-server path already stamps `danger-full-access` (`codexHeadlessSandbox` constant, `substrate_select.go:131`) — leave the constant; delete its now-obsolete `WritableRoots: codexWorktreeWritableRoots` hook and the `codexWorktreeWritableRoots`/`codexGitCommonDir` helpers (`substrate_select.go:286–320`), which are the app-server twin of the dead exec carveout.

### 3c. Confirm the daemon-fallback commit path is the committer

No code change — a confirmation the plan depends on. `ensureCodexRefsTrailer` (`codexcommit.go:204`), invoked per implement-node exit (`dot_cascade.go:1938`), stages + commits codex's edits from OUTSIDE any sandbox and adds the `Refs:<bead>` trailer. It fires for `CompletionProcessExit` harnesses (codex/pi), no-op for claude. `research/05` proves it is the de-facto committer today. Under `danger-full-access` codex CAN also self-commit; the fallback still runs as backstop and idempotently no-ops when HEAD already carries the trailer. **Either way the diff lands.** Acceptance (§5) accepts either committer.

### 3d. How Codex implementer crews get selected/staffed

- **Substrate:** the crew's daemon runs with `HARMONIK_SUBSTRATE=codexdriver` (`substrate_select.go:31,76`). With 3a done, that no longer requires a worker.
- **Per-node harness:** implement nodes resolve their harness via `harnessRegistry.ForAgent(artifactAgentType(...))` (`dot_cascade.go:1446`), so the implementer agent-type maps to codex while the **reviewer stays claude** (`reviewerSubstrate` is always the tmux/claude substrate — `substrate_select.go:76`). This is the desired split: codex implements, claude reviews the diff. Keep it.
- **Captain lever:** staffing a "Codex crew" = starting a crew whose daemon is launched with `HARMONIK_SUBSTRATE=codexdriver`. The Captain's crew-start path must thread that env; verify it does (or add it) as part of §6 hardening.

---

## 4. Resolving the known open facets

**(a) Does the `exec_command` / "CreateProcess Operation not permitted" shell-denial vanish under `danger-full-access`?**
**Yes — reasoned, high confidence, live-verification owed.** The denial was a *seatbelt writable-root* artifact, not a subprocess ban: `research/06` §exec-facet confirms subprocess spawn IS allowed under `workspace-write`; the failure was a spawned git/test step trying to write a lockfile *outside* the writable roots (the resolved `.git`). `danger-full-access` removes the seatbelt entirely — no writable-root restriction, full FS + exec (`research/05` finding #10; D3 §48 "dissolves the entire commit/worktree/shell-step problem"). With no seatbelt there is nothing to deny either the `.git` write OR the `exec_command` spawn. (Caveat noted in `research/06`: the literal string `CreateProcess` is a *Windows* API name; harmonik runs macOS/Linux, where the denial would be a bare EPERM. Either way, no seatbelt ⇒ no denial.) → prove in §5 acceptance by observing codex run its own `git status`/gate command successfully in-run.

**(b) Is the `.git` writable-root fix (49d7fde3) now unnecessary — keep or revert?**
**Neither "keep" nor "revert" — supersede.** Under `danger-full-access` the writable_roots injection is inert dead code (`research/06`: moot on any danger-full-access path). Do NOT `git revert` the commit wholesale: 49d7fde3 also carried a real, still-needed bug fix — switching from the `--sandbox` flag (which `codex exec resume` rejects) to the `-c sandbox_mode` override. **Action:** keep the `-c` mechanism, change the value to `danger-full-access`, and delete only the now-dead writable-roots derivation (`codexExecWritableRoots`, `codexWritableRootsArg`, `codexWorktreeWritableRoots`, `codexGitCommonDir` and their tests/call sites). This is a clean forward change, not a revert.

**(c) What is the live-verification that proves success?**
A **real bead runs end-to-end on local Codex** with the daemon system up (this plan is design-only; verification happens at execution): `HARMONIK_SUBSTRATE=codexdriver`, no worker bound, `danger-full-access`. Success signals, all observed on one run: (1) implement node's codex process exits 0 and its edits land as a commit carrying `Refs:<bead>` (self-commit OR daemon fallback — either counts); (2) codex's own in-run shell step (`git status` / the commit_gate command) runs without an "Operation not permitted" denial (proves facet (a)); (3) the review node (claude) writes `.harmonik/review.json` and the DOT advances; (4) the bead reaches closed via the normal daemon terminal-transition. `research/05` finding #12 flags that the danger-full-access self-commit path is coded but **not yet live-verified on deployed codex 0.142.0** — this run retires that debt.

---

## 5. Acceptance criteria (concrete, testable)

1. **Local codex, no worker:** with `HARMONIK_SUBSTRATE=codexdriver` and zero workers configured, the daemon launches a codex implement node on `LocalRunner` — no refusal, no `refusedIsolationBoundaryArgv0`, no "isolation-boundary guard" stderr.
2. **A real bead completes end-to-end on local codex:** implement → commit (Refs trailer present) → review verdict → bead closed, via the unmodified DOT flow.
3. **Shell facet clear:** the run log shows codex's own `exec_command` / gate shell step executing without an EPERM/"Operation not permitted" denial (facet (a) retired).
4. **Uniform posture:** the effective codex sandbox on both argv branches is `danger-full-access`; no `workspace-write` and no `writable_roots` remain in the codex-exec argv (`grep` the launch spec / a `--strict-config` echo).
5. **Codex crew staffed:** the Captain can start a crew that runs its implementer beads on codex and its reviewer on claude, and that crew clears ≥1 bead.
6. **Token-offload demonstrated:** a bead that would otherwise consume Claude implementer tokens is instead cleared by codex (claude spend limited to the review node) — show the before/after split qualitatively (implementer turns on codex, not claude).
7. **No dead code / green build:** removed guard + writable-roots helpers leave `go build ./...` and `go vet ./...` green; deleted-symbol tests removed or updated; `ubs` clean on changed files.

---

## 6. Sequencing

**Smallest first step that PROVES it (one kerf task, one crew):**
- Step 1 — **Flip the fence + posture (3a + 3b), minimal diff.** Land the three-seam guard drop and the exec-path `danger-full-access` switch. Ship with unit coverage at the argv tier (a `TestBuildCodexLaunchSpec` asserting `danger-full-access`, no `--sandbox`, no writable_roots on both branches — mirroring the existing hk-daegv test shape).
- Step 2 — **Live-prove one bead (acceptance 1–4).** Bring the daemon up locally, `HARMONIK_SUBSTRATE=codexdriver`, no worker, run one trivial real bead through DOT. This is the linchpin proof and retires `research/05` #12 + facet (a). Do this BEFORE any hardening.

**Then harden:**
- Step 3 — **Delete the now-dead code** (refused-argv0, `codexWorktreeWritableRoots`/`codexGitCommonDir`, exec writable-roots helpers, `CodexRequireIsolationBoundary` plumbing) and their tests. Keep the inert `codexWorkerRoutingRunner` unless the P3 crew removes it.
- Step 4 — **Crew staffing (3d, acceptance 5–6).** Verify/thread `HARMONIK_SUBSTRATE=codexdriver` through the Captain's crew-start path; run a crew that clears a bead with codex-implements / claude-reviews; capture the token-offload split.
- Step 5 — **Widen** to a couple more beads of varying shape (a multi-node DOT, a review back-edge/resume turn) to confirm the resume argv branch and the fallback-committer both behave under `danger-full-access`.

Each step is independently landable; Step 2 is the go/no-go gate — if the local bead doesn't clear, stop and fan out on the failure before hardening.

---

## 7. Operator resolutions (2026-07-21) — questions ANSWERED

1. **Reviewer stays Claude — YES, for now.** Keep codex-implements / claude-reviews while
   Codex is unproven (independent-eyes review gate). **BUT the real issue is that the reviewer
   harness is HARD-CODED** (`reviewerSubstrate` always tmux/claude). Operator: DOT nodes should
   take the **model/harness as a config parameter** — for implement, review, and any other node
   — so a DOT node can be named in config and customized, and the DOT gets processed against
   that config (relates to a recent "rerun" discussion, possibly `plans/2026-07-19-ralph-
   autonomous-loop/` or nearby). **This is a FAST-FOLLOW: pursue de-hard-coding per-node
   model/harness config immediately after Codex is fully supported.** It is NOT part of this
   plan (don't block Codex on it), but it is the correct fix and supersedes the reviewer
   question permanently. Tracked as a follow-up (see platform DECISIONS.md).
2. **Inert ssh routing seam — LEAVE for P3.** Confirmed. `codexWorkerRoutingRunner`/SSHRunner
   stays inert; P2 E4 / P3 owns final extract-or-delete disposition. Mark it deprecated.
3. **Verification — REFRAMED (this was the wrong framing).** It is NOT "pick one proof bead."
   The verification process is two layers:
   - **(a) Heavy in-crew testing during implementation** — the crew runs a VERY significant
     testing process directly via its own SUBAGENTS (beads may NOT be the right vehicle for this
     testing; use subagents to exercise the change thoroughly).
   - **(b) Assessor complete-system test as the gate** — when implementation is complete it is
     handed to the ASSESSOR, who performs a full system test before it's considered done.
   Running real beads end-to-end is part of (a), but the bar is "thoroughly tested by the crew +
   assessor-signed-off," not "one bead cleared." Update §5 acceptance / §6 sequencing to reflect
   this: Step-2's live bead is the smoke test, but the real gate is crew-subagent testing +
   assessor sign-off. The Captain designs the testing; it need not be bead-shaped.
