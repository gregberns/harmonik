# P2 EXTRACTION — live progress + file ownership

**Owner of this document:** the P2 extraction agent (Claude Opus 4.8, session `59707ade`).
**Last updated:** 2026-07-23 — P2 core COMPLETE (9/9) + RT13 + E4c + RT19.0 + RT19b + RT14 + RT15 + **RT19c** landed.
The concurrent quality lane's working tree is fully drained to disk (§8).

> ## ⚠️ We nearly collided at 08:00 — read this
>
> My E3a agent was about to run `git add -A` and commit, which would have swept **49 of your
> uncommitted files** (`cmd/harmonik`, `keeper`, `lifecycle`, `supervise`, `workspace`,
> `sessiondata`, `core`) into my extraction commit. I killed the workflow before it committed.
> Nothing of yours was lost or committed — I verified all three of my earlier commits are clean too.
>
> **Fixed on my side, permanently:** `git add -A`, `git commit -a`, `git reset --hard` and `git clean`
> are now hard-forbidden in my execution workflow. Every slice stages by explicit pathspec and reviews
> only its own staged diff. If a slice fails, it now restores only its own files by
> `git checkout HEAD -- <path>` instead of resetting the tree.
>
> **What I'd ask of you:** commit your work in reasonably small batches rather than accumulating a
> large uncommitted set, and never `git add -A` / `git reset --hard` while §2 shows Execute IN
> PROGRESS. I hold `internal/daemon/**` and `.golangci.yml` (see §3).

> **Read this if you are another agent working in this repo right now.** §3 lists the files I hold
> exclusively while the extraction chain runs, and §4 lists the work I am deliberately NOT doing so you
> can take it without stepping on me. I update this file at every phase boundary and after every slice.

---

## 1. What I am doing

Executing the P2 god-package extraction end to end: plan it, then refactor and test it. Started from
`plans/2026-07-21-p2-extraction/_plan.md` (strategy, pre-existing) and produced executable per-unit
plans, then began landing them one commit at a time.

**Branch:** `phase1-session-restart-substrate`. **Base commit for all P2 work:** `34509e60`.

---

## 2. Phase status

| Phase | What | Status |
|---|---|---|
| **1. Recon** | 8 units analyzed, each adversarially challenged | **DONE** |
| **2. Plan** | 9 executable per-unit plans | **DONE** |
| **3. Execute (P2 core)** | 9 slices, sequential | **DONE — 9/9, every verify `is_pure_move: true`** |
| **4. Punch list** | verifier findings applied | **DONE** — `ffc5415a` |
| **5. Differential verification** | clean before/after pair, identical scope | **DONE — no regression** (see below) |
| **6. E5 RT stream + E4c** | RT13, E4c, RT19b, RT14, RT15, **RT19c** landed; RT16/17/18/19/lift planned | **IN PROGRESS** |
| **7. E4d re-plan** | overturned "impossible"; 3 prep slices ready, E4d-3 parked | **DONE** |

### Verification verdict (Phase 5)

Clean pair, identical package scope, healthy disk: **pre-P2 6 failures / P2 HEAD 3 failures.**
Three runs of the SAME commit gave 123 / 3 / 9 — runs B and C share **1 of 11** failures. The only
stable failure is `TestThroughput_TenBeadsAtMaxFour`, which **also fails at pre-P2 baseline**.
**No failure is attributable to P2.** The 123-failure run was the disk watermark pausing dispatch.

Consequence for the oracle: compare **stable intersections across repeated runs**, never one run
against one run. Recorded in `00-test-oracle-baseline.md`.

### Slice-by-slice

| # | Slice | What leaves `internal/daemon` | Status | Commit |
|---|---|---|---|---|
| 0 | base | `internal/gitprobe` + `internal/harness/shared/seedprompt.go` | **LANDED** | `805a9d76` |
| 1 | **E4a** | `reversetunnel.go` → `internal/transport/tunnel` | **LANDED — verify PASS** | `20cbd18d` |
| 2 | **E4b** | `codesync.go` → `internal/transport/codesync` | **LANDED — verify PASS** | `222ff36f` |
| 3 | **E3a** | queue store + ledger bridge + operator-event-consumer → `internal/queuewiring` | **LANDED** (salvaged by hand after the near-collision; reviewer APPROVE, 36 tests in → 36 out) | `bfb87bfd` |
| 4 | **E2a** | crew launch contract → `internal/crewrun` | **LANDED** — boundary OK, no foreign files swept | `12663fac` |
| 5 | **E1a-0** | `harness-shared`/`harness-codex` depguard blocks + cross-harness refs-trailer leaf → `harness/shared` | **LANDED — verify PASS**, pure move, 0 logic changes | `9b682ae2` |
| 6 | **E1a-1** | codex harness → `internal/harness/codex` | **LANDED** — `go list -deps ./internal/harness/codex \| grep daemon` is **empty**; specaudit path-allowlist updated; tagged builds clean | `5f7762c3` |
| 7 | **E1b-prep** | evict the mis-named `claudeRunCtx` universal launch DTO → `shared.LaunchCtx` (85 daemon call sites) | **LANDED — verify PASS** | `c3fff27d` |
| 8 | **E1b** | claude harness → `internal/harness/claude` | **LANDED — verify PASS**; the path-pinned `hc045a` specaudit sensor updated and passing under its tag | `db2f4752` |
| 9 | **E1c** | pi harness → `internal/harness/pi` | **LANDED — verify PASS**; no `harness/pi → harness/codex` back-edge | `66e0041d` |
| 10 | **RT13** | run-branch merge path → `internal/runmerge` (E5 stream) | **LANDED — verify PASS**, `is_pure_move: true`, freeze gate proven with 4 probes | `379fca71` |
| 11 | **test fix** | two unsound source-text conformance assertions replaced | **LANDED** | `b423081f` |
| 12 | **E4c** | worker-registry boot wiring → `internal/workers` | **LANDED — verify PASS**; `pure_move: false` **by design** (one declared signature change, confirmed the only delta) | `646748f3` |
| 13 | **RT19b** | the stranded run-path helpers → `internal/substrate`, `internal/harness/shared`, new `internal/runlaunch` | **COMPLETE — 3/3 commits, reviewer APPROVE on each**, pure move throughout | `e82311b9` · `efeeb047` · `fd608c01` |
| 14 | **RT14** | *nothing* — the open-coded agent_ready WAIT is RETIRED, not relocated. Both call sites bind onto the pre-existing `dispatchSegment` seam | **COMPLETE — 3/3 commits, reviewer BLOCK→APPROVE on A, APPROVE on B and C.** Metric is **seam uniformity, not LOC** (see below) | `229e6e91` · `cb89e35e` · `7d448afb` |

### Result

`internal/daemon`: **126 files / 57,197 LOC → 103 / 49,012** — **−23 files, −8,185 LOC (−14.3%)**.

Seven new fenced leaf packages, each with a depguard deny edge **and** a grep freeze gate wired into
both `check-fast` and `check-short`: `gitprobe`, `harness/{shared,claude,codex,pi}`,
`transport/{tunnel,codesync}`, `queuewiring`, `crewrun`, `runmerge`.

The P3-enabling property, measured — all three harness packages are daemon-free and pi has no
back-edge to codex:

```
go list -deps ./internal/harness/{claude,codex,pi} | grep internal/daemon   ->  empty
go list -deps ./internal/harness/pi | grep harness/codex                    ->  empty
```

**Failure policy:** each slice commits independently. A slice that fails verification gets one repair
attempt; if that fails the tree resets to the last good commit and the chain continues past it. So a bad
slice cannot cascade, and everything before it stays landed.

---

## 3. Files I hold EXCLUSIVELY until Phase 3 ends

> **Correction (08:35).** My first cut of this section claimed whole directories — including
> `cmd/harmonik/**` — as one agent's or the other's. That is too coarse and would manufacture false
> conflicts. E2a legitimately had to edit `cmd/harmonik/captain.go`, `captain_respawn.go` and
> `captain_twoproj_hk25bg_test.go` as call sites for the crew move; none of the three was among your
> dirty files, and your count stayed at exactly 49. **The rule that actually works is per-file, not
> per-directory: stage only files whose diff your own change explains, and never `git add -A`.** The
> table below is therefore a list of files where a collision is *likely*, not a claim of ownership over
> every path under them.

Please do not edit these while §2 shows Execute IN PROGRESS. Conflicts here are guaranteed, not merely
possible — every slice touches the first one.

| Path | Why I hold it | Released when |
|---|---|---|
| **`.golangci.yml`** | every slice adds its depguard allow/deny block here — this is the freeze tripwire and the whole point of the exercise | end of Phase 3 |
| `internal/daemon/**` | slices move files out of it and rewrite the call sites left behind | end of Phase 3 |
| `internal/daemon/workloop.go` | **especially this** — E1b-prep rewrites ~182 references through it | after slice 7 (E1b-prep) |
| `internal/daemon/harnessregistry.go`, `export_test.go` | all three E1 slices edit both; this is why E1a/E1b/E1c are sequential and not parallel | after slice 9 |
| `internal/harness/**`, `internal/transport/**`, `internal/queuewiring/**`, `internal/crewrun/**` | packages this work creates | end of Phase 3 |
| `internal/gitprobe/**` | created by slice 0 | end of Phase 3 |
| `internal/specaudit/hc045a_*_test.go`, `wminv003_*_test.go` | path-pinned sensors that E1a/E1b must update in the same commit as their move | after slice 9 |
| `plans/2026-07-21-p2-extraction/*` except `QUALITY-AUDIT-*.md` | my plan artifacts | — |

**Three collision points the quality audit's §5 suggestions hit directly.** Its recommendations 1, 2 and
5 land inside my exclusive set:

- **rec 2** (fix the `queue` / `core` depguard allow-lists) → edits `.golangci.yml`. **Please defer or
  hand to me** — I will apply it at the end of Phase 3, where it composes cleanly with the blocks I am
  adding. It is a good fix; it just cannot land concurrently.
- **rec 5** (replace `beadRunOne`'s `//nolint:gocognit,cyclop,funlen`) → edits
  `internal/daemon/workloop.go:3167`. **Please defer** until after slice 7.
- **rec 1** (delete the 242 dead `//nolint:gosec` directives) → 869 of the 2,605 directives are in
  `internal/daemon`. **Safe to do everywhere EXCEPT `internal/daemon/**`.** The other 1,736 across
  `lifecycle` (442), `workspace` (162), `keeper` (150), `cmd/harmonik` (147), `specaudit` (141) are all
  yours with no conflict.

---

## 4. What I am deliberately NOT doing — free for you to take

These are all outside P2's scope as planned, and I am not touching them:

- **`cmd/harmonik`** — the audit's §4 gap 1, and I agree it is the largest hole. 26,830 non-test LOC,
  952 findings, no coverage gate. No P2 unit owns it. Entirely yours.
- **`internal/codexwire`, `internal/keeper`, `internal/lifecycle`, `internal/workspace`,
  `internal/eventbus`** — audit gaps 2–6. Referenced by P2 documents, owned by no P2 unit.
- **The small dense packages** — `agentmanifest`, `cmd/harmonik/supervise`, `queue/cli`,
  `internal/supervise`, `hookrelay`, `sessiondata` (audit gap 7).
- **The 10 production `nilerr` bugs** (audit §3c) — except any inside `internal/daemon`
  (`commscursor.go:246`, `pasteinject.go:2267`); the other 8 in `lifecycle`, `supervise`, `workspace`
  are free. These are live swallowed-error bugs; worth doing.
- **The `core` → yaml/expr layering breach** (audit gap 9). E6 is parked by operator decision, and the
  audit is right that this should not wait for a package split.
- **The suppression debt / coverage-gate wiring** (audit gap 8) — no owner, not mine.
- **`internal/daemon/pasteinject.go`** — deferred into E5 by plan, and E5 is not in this execution chain.
- **E2b (`crewstart.go` handler)** — BLOCKED, needs an operator waiver (see §6).
- **E5 (DOT run-loop) and E6 (core split)** — E5 is a multi-slice prep stream not staffable until
  E1+E4 land; E6 is parked by operator decision.

---

## 5. Open punch list — mine to fix in Phase 4

1. **[behavior] `internal/transport/tunnel/tunnel.go:176-178`** — E4a turned a previously-ignored
   `l.Close()` error into a hard `AllocatePort` failure. The plan said `_ = l.Close()`; that form is
   rejected by this repo's errcheck (`.golangci.yml:84` `check-blank: true`), but check-and-log
   satisfies errcheck *and* preserves behavior. Revert to check-and-log.
2. **[process] File a bead** owning E4's deferred `_plan.md` §5.4 runtime proof (drive one remote bead
   end-to-end). Cannot run now — daemon is down.
3. **[gate] `scripts/transport-freeze-gate.sh`** has two proven blind spots: `find -maxdepth 1` misses
   `internal/daemon/<subpkg>/`, and the `^(func|var|const)` symbol regex misses re-declaration inside a
   grouped `var ( … )` block — the exact form the original `reservedTunnelPorts` used.
4. **[baseline] Record the `specaudit` failure set** in `00-test-oracle-baseline.md` as a second
   differential oracle. It is already red at HEAD (7 top-level failures, identical set at `20cbd18d^`).
   Two of those sensors scan *source by hardcoded path*, so an extraction can legitimately break them —
   which makes this oracle load-bearing for E1a/E1b, not decorative.
5. **[doc]** Two files the E4 plan marked "No edit" got comment-only edits
   (`claudelaunchspec_remote_hkz8ek_test.go:64`, `workloop_gate_n5md3_test.go:35/210/233`). Reconcile
   the plan table.
6. **[baseline] A seventh pre-existing failure found.**
   `TestMergeToMain_RealConflictWithBeadsLedger_Escalates` fails in isolation at `34509e60` (pre-P2),
   `805a9d76`, `20cbd18d` and `bfb87bfd` — identical 30s timeout at every one. It **passes inside the
   full suite**, so it is *isolation*-sensitive, the mirror image of the five load-sensitive flakes.
   Add it to the known-flaky list with that distinction noted, because the two classes need opposite
   confirmation procedures: load-sensitive flakes are re-run *in isolation* to confirm, this one must
   be re-run *in the full suite*.

---

### From E1a-0 (`9b682ae2`) and E1a-1 (`5f7762c3`)

7. **[ratchet hole — the most actionable item here] No `scripts/harness-freeze-gate.sh`, and no
   Makefile wiring for one.** Every prior P2 unit shipped its grep gate in the same commit as its
   depguard block (`Makefile:243` transport, `:253` queuewiring, plus crewrun). The harness unit did
   not. depguard fences the *import edge*; only the grep gate fences the *creation of a new file* —
   which is the literal freeze the operator confirmed as a **hard CI failure**. Without it, nothing
   stops a new `internal/daemon/*harness*.go` / `*launchspec*.go` from appearing. Write the gate and
   wire it into `check-fast` + `check-short`.
8. **[suppression debt] Two new `//nolint:gosec` directives added during a declared pure move** —
   `internal/harness/shared/refstrailer.go:179` and `:229`. Neither existed on the corresponding
   `exec.CommandContext` calls in `internal/daemon/codexcommit.go`. Comment-only, so non-behavioral,
   but it is the same "lint fix taken as license" pattern as the E4a `l.Close()` item, and it adds to
   the 2,605-directive pile the quality audit is trying to drain. Re-check whether they are needed.
9. **[speculative fences] Widen-only-on-proof.** `harness-shared`'s allow list names `internal/handler`
   and `internal/handlercontract` which it does not currently import, and `harness-codex` pre-allows
   `github.com/google/uuid` + `gopkg.in/yaml.v3`. This contradicts the implementer's own stated
   principle ("a speculative allow weakens the fence" — its reason for omitting `internal/workspace`).
   Now that E1a-1 has landed, tighten both lists to the measured import set.
10. **[flaky] A genuine non-deterministic flake, not reported by the implementer.**
    `TestCodexHarness_LaunchSpec_ResumeDelegates`, `_InitialDelegates` and `_CustomBinary` (now in
    `internal/harness/codex`) fail intermittently under parallel load. Add to the known-flaky list.
11. **[plan drift] `_plan.md` §4 overstates E1's result.** It promises `internal/harness/codex` will
    have "a core/handler/workspace-only closure." Measured closure also includes `brcli`, `queue`,
    `lifecycle` and `handlercontract/lifecycle` (transitively). The **daemon-free** property — the one
    P3 actually needs — holds exactly as promised, but P3 should not size a container against the
    plan's "minimal" claim. Correct the sentence.

## 5b. ⚠️ Environmental blocker — needs the operator, I will not act on it unilaterally

**The box is below the daemon's own disk watermark.** `internal/daemon` tests log:

```
disk-check: available=7626MiB watermark=10240MiB — dispatch paused
```

`df`: **228 Gi volume, 96% full, 7.5 Gi free.** This *manufactures* dispatch-timing test failures
(E2a's verifier hit `TestWorkLoop_SubmittedPendingGroupBootstrapsToActive` failing for exactly this
reason) and it will corrupt the Phase-5 differential run — the whole point of which is to distinguish
real regressions from noise.

Where the space went — **I am not deleting any of it, because none of it is mine to delete**:

| Consumer | Size | Note |
|---|---:|---|
| `/private/tmp/claude-502/…/76db3286-…` | 2.9 G | another session's scratchpad worktrees |
| `…/8df832ad-…` | 2.4 G | " |
| `…/9fb43744-…` | 2.0 G | " |
| `…/9a480b03-…`, `…/c8d1d438-…`, `…/a5b9a7a8-…` | ~2.7 G | " |
| `/private/tmp/claude-502` total | **16 G** | 54 registered git worktrees |
| **this session** | 184 M | mine; keeping `head-baseline` for bisects during E1a/E1b |
| `.claude/worktrees/agent-*` | — | **abandoned 104–129 h ago** (4–5 days), inside the repo |

Two things worth your call:

1. **Reclaiming the ~10 G of stale scratchpads** would need someone to confirm those sessions are dead.
   `git worktree prune` will NOT help — every registered directory still exists, so nothing is prunable
   until the directories themselves go.
2. **The `.claude/worktrees/agent-*` set is 4–5 days abandoned and lives inside the repo**, which is
   also why `make fmt-check` currently fails: it scans them and reports `gofumpt` diffs in
   `agent-a54270358debb6a7c/internal/daemon/workloop.go` and friends. That failure has nothing to do
   with anyone's real work, and it makes a fail-closed formatting gate useless.

Until this is resolved, treat any dispatch/throughput/timing failure in the differential run as
**suspect rather than real**, and re-confirm it in isolation.

## 6. Decisions — settled vs. still open

**Confirmed by the operator 2026-07-22:**

1. **Package home = top-level `internal/harness/{claude,codex,pi}`**, not a daemon sub-package.
   Rationale given: modularize as much as reasonable and possible. (Also what P3 needs — a container
   that links the harness without dragging the 57k-LOC monolith.)
2. **Freeze tripwire = hard CI failure**, not a review-gate warning.

**Assumed and proceeding** (operator may still veto; each `E*.md` recipe is written against these):

3. **E6 (core split) parked** — name the type-families, do not cut them.
4. **Guardrails ratified** — substrate-v2 kill-criteria + boundary tests are the ground rule for P2
   scope disputes. "Is this in the unit?" is settled by `go list -deps | grep daemon`, not by argument.
5. **P2 crew runs on Codex** once E1a proves Codex operational; Claude reserved for the pure-move
   review gate.

**Still open — needs the operator, and I have NOT assumed an answer:**

6. **E2b requires an explicit waiver of the no-new-seam rule.** `crewstart.go`'s handler needs a
   brand-new `crewSpawner` port, and `_plan.md` §5.1 makes inventing a seam a review-gate rejection.
   I excluded E2b from this execution chain rather than guess. E2a (the portable half) proceeds.

---

## 7. Measured baselines (so later numbers are comparable)

## 7b. The commit-drain wave — 2026-07-22 15:00 onward

**Operator directive:** get the concurrent quality lane's ~155 uncommitted files onto disk, keep
parallelising, and keep this file current.

The near-collision in §3 is now resolved in the opposite direction from what that section assumed:
rather than defending my files from the quality agent, I am **committing its work for it**, batch by
batch, while it keeps working. Every commit is staged by explicit pathspec. No `git add -A`.

**Standing rule for this wave — never commit a package the other agent is currently inside.** Its
lanes move; I re-check file mtimes before every batch and skip anything written in the last ~15
minutes. As of 15:20 it is live in `internal/scenario`, `internal/release`, `internal/eventbus`,
`internal/brcli`, `internal/digest` — all excluded. It also rewrites
`plans/2026-07-22-quality-audit-remediation/PROGRESS.md` as it goes, so that file will need a
follow-up commit after the one already landed.

Every non-trivial batch goes through an independent reviewer before it is committed. That gate is
**earning its keep** — three of the first four reviews came back REQUEST_CHANGES with real defects,
not style nits:

| Batch | Verdict | What review caught |
|---|---|---|
| `internal/codexwire` | REQUEST_CHANGES → APPROVE | two `RawItem` doc comments described an `Extra`-merging mechanism the type does not have. Fixed, re-reviewed, landed `e2d79039` |
| `internal/agentmanifest` + `cmd/harmonik/agent.go` | APPROVE | landed `3213baf6` |
| `hookrelay`/`supervise`/`sessiondata` | REQUEST_CHANGES | **claimed watchdog liveness bug does not exist** — HEAD already ignored the probe `Close` error. And the real reap fix introduced two regressions: a kill error now aborts the pass so already-killed sessions emit no `tmux_orphan_reaped` event, and a missing tmux binary flips `supervise reap` from exit 0 to exit 1. Fix in flight |
| `Makefile` + `.golangci.yml` + `scripts/**` | REQUEST_CHANGES | **P2 coordination verified clean** — all eight freeze gates byte-identical and passing, no depguard deny edge touched, the four real `core` YAML/Expr breaches still visible. But `cmd-coverage-gate.sh` was wired into **nothing** despite PROGRESS claiming it enforced, and its bare `join` inherited the ambient locale while its inputs are `LC_ALL=C` sorted — under a UTF-8 locale it would drop rows and print "all package baselines held" having compared nothing. Both fixed by me |

The freeze-gate verification is the load-bearing result for P2: the quality lane edited `Makefile`
and `.golangci.yml` concurrently and **did not weaken a single extraction fence**.

## 8. Landed in this wave

| Commit | What |
|---|---|
| `20aa1172` | zeromq research programs preserved as `//go:build ignore` — they were breaking whole-tree typechecking |
| `3213baf6` | agentmanifest renderer write-failure propagation (129 findings → 0) |
| `63c38c1e` | disk-reclaim runbook, bead hygiene, wedge incident, quality-wave plan |
| `e2d79039` | codexwire checked writer errors + frame-parsing decomposition (87 findings → 0) |
| `0fe9486c` | 275 fleet artifacts: 14 kerf works, 22 crew missions, 3 skills, gate verdicts |
| `3b92142b` | daemon: DOT transport failures separated from "no verdict"; D2 credential guard moved to the launch boundary; both `nilerr` sites |
| `42010150` | build: git-aware formatter, GOCACHE isolation, `cmd/**` coverage ratchet — **all eight P2 freeze gates verified byte-identical and passing** |
| `1b6172ed` | lifecycle/keeper/workspace/queue/core: tmux failures no longer masquerade as an empty session list |
| `89b514c9` | supervise: keep reaping after a kill failure; tolerate absent tmux (three regressions closed) |
| `4082056a` | **RT19.0** — the 10 grandfathered findings in `export_test.go`, plus these recipe corrections |
| `7273e95d` | cmd: SH-033 made deterministic; 8 `noctx` findings converted to `exec.CommandContext` |

**Wave result: 11 commits, ~350 files, tree green.** `go build ./...` and `go vet` clean at every
commit. Everything still uncommitted belongs to the quality agent's live lanes (`brcli`, `eventbus`,
`scenario`, `sentinel`, `workers`, `hooksystem`, `digest`, `watch`, `presence`, `release`,
`structuredlog`, `codextest`, `cognition`, `t5probe`) — deliberately untouched.

### Scorecard for the review gate

Six of eight substantive reviews returned REQUEST_CHANGES or BLOCK, every one for a real defect:

- a **claimed** watchdog liveness bug that **did not exist** (HEAD already discarded that error)
- a reap loop that abandoned the pass on first failure, losing events for sessions it HAD killed
- `supervise reap` exiting 1 on any host with tmux installed but no server running — a fresh box
- a coverage ratchet claimed "enforced" that was wired into nothing
- a locale-dependent `join` that would print "all package baselines held" having compared nothing
- two gate-red findings in `internal/daemon` and eight more in `cmd/harmonik`
- two `RawItem` doc comments describing a mechanism the type does not have

Not one was style. **The pattern worth carrying forward: the lane's own PROGRESS claims were the
least reliable input.** Three separate claims ("enforced", "liveness bug fixed", "12 of 13 verified")
did not survive independent checking. Verify against code, not against the progress log.

### The two gate regressions review caught, and the lesson

Both would have turned `make check-fast` red on the next commit, and neither was visible to
`go build` or `go test`.

1. **`nakedret` in `beadRunOne`.** The D2 guard moved, so its bare `return` moved with it.
   `--new-from-rev` anchors findings to changed lines, so a return that was grandfathered at its old
   position counted as new at its new one. Now an explicit `return false` (verified safe —
   `succeeded` has no assignment before that point).

2. **Eight `noctx` findings in `cmd/harmonik` with no new code at all.** This is the sharper lesson
   and it generalises to every RT chunk ahead. The quality lane deleted *trailing*
   `//nolint:gosec` comments that gosec no longer needed. Deleting a trailing comment **modifies the
   line**, which un-grandfathers every OTHER finding anchored to that same line — here, long-standing
   `exec.Command` findings that had been sitting underneath the gosec directive.

   **Generalised rule for the RT stream: touching a line for ANY reason — even deleting a comment on
   it — re-exposes every grandfathered finding on that line.** The catalogue §0a already warns about
   this for function *declaration* lines and `funlen`/`cyclop`/`gocognit`. It is broader than that:
   it applies to every line and every linter. Budget for it on any chunk that rewrites comments.

### RT19.0 — the E5 prerequisite

Landed as a clean ~12-line in-place cleanup, exactly as the catalogue sized it. All 10 grandfathered
findings in `export_test.go` are gone (`golangci-lint` scoped to the file: **0**). Real fixes where
possible — the four `unnamedResult` findings got meaningful names, `importShadow` renamed a
parameter, `paramTypeCombine` applied, and the `ineffassign` turned out to be genuinely dead code (a
local `adapterReg` built from `p.AdapterRegistry` and never read, because the struct it fed now uses
`p.AdapterRegistry2`; the `handler.NewHandler` call that consumed it is gone). Justified suppressions
only where a real fix would be worse: `containedctx` on a field that mirrors `daemon.Config` by
design, `errcheck` on a seam whose four callers use statement position, and `tooManyResultsChecker`
on a flat result tuple that exists precisely to avoid re-exporting an unexported struct.

**Follow-up, recorded not fixed:** deleting the dead local leaves the exported `AdapterRegistry`
params field with zero readers — vestigial. Removing the field itself would touch other test files,
so it is out of RT19.0's charter.

**This unblocks 8 of the 20 `export_test.go` split chunks (RT19.1–RT19.20).**

### Four corrections to the recipes — apply these to every remaining chunk

Measured while running RT19.0. Each one cost time that the next chunk should not have to spend.

1. **The known-flaky set in `00-test-oracle-baseline.md` is stale.** `TestThroughput_TenBeadsAtMaxFour`
   did **not** fail. The pair that actually fails is `TestPasteInjectCommitBudget_IdleActivePane_HKukx`
   and `TestPasteInjectQuitOnCommit_NewCommitNoKill` — both timing-sensitive (`kill fired after 274ms
   — too close to hard ceiling (120ms)`), both already red at HEAD. Proven pre-existing by A/B: the
   pre-edit file gave 6 failures over `-count=8`, the edited file 5. Use that pair as the known-red set.
2. **`go test -short ./internal/daemon/` exceeds Go's default 10-minute timeout** and dies with a
   goroutine dump that reads like a real failure but is not. It needs `-timeout 45m` (~340s warm,
   600s+ cold). Add the flag to every chunk's gate.
3. **`gofumpt` requires a blank `//` line between a doc comment and a following `//nolint:` directive**
   on a function. Every later chunk that adds a function-level suppression — the catalogue names
   RT15.5, RT18.5, RT18.7, RT18.8, RT18.10 and LIFT.13 — will hit this.
4. **`golangci-lint` takes a global lock.** With another agent active you get `Error: parallel
   golangci-lint is running`. This is a DIFFERENT failure from the shared-`GOCACHE` `no export data
   for "encoding/json"` one, and `with-isolated-gocache.sh` does **not** prevent it. Retry in a loop.

**Deliberately left uncommitted, needs an operator call:** `.memory/` (263 files, 1.2 MB, zero
tracking precedent) and `testdata/codex-app-server/gen/` (852 generated files, 5.7 MB). Also
`scenarios/core-loop-proof/testdata/.harmonik/keeper/bravo.ctx` — a keeper gauge file with a live
session id, written into a fixture directory by a scenario run on 2026-07-18. That is runtime
leakage and wants a `.gitignore` entry, not a commit.

**Drain complete.** Every lane listed above is now on disk — the quality wave's remaining working-tree
state landed as 18 further reviewed commits (`ac644bf8` … `e422c6eb`), interleaved with RT19b. Nothing
of the quality lane's work is left uncommitted.

### RT19b — the stranded run-path helpers (COMPLETE)

Three commits, each independently reviewed APPROVE, each a pure move:

| # | Commit | What moved |
|---|---|---|
| 1 | `e82311b9` | `clockAfter` → `substrate.After`, 5 call sites re-qualified |
| 2 | `efeeb047` | `artifactAgentType` → `shared.ArtifactAgentType`, and `beadAlreadySubsumedInMain` → `shared.MainHistoryHasRefsTrailer` — **collapsed onto the `RefsTrailerLine` / `ContainsExactLine` primitives `harness/shared` already owned**, instead of landing a second copy of the same rule |
| 3 | `fd608c01` | new leaf package `internal/runlaunch` (`doc.go` / `deadlines.go` / `events.go` / `teardown.go` + the relocated `deadlines_test.go`): **13 symbols**, **55 production call sites** re-qualified (reviewloop 22, dot_cascade 11, workloop 11, dot_gate 7, agentready 2, bootstate 2), plus a depguard block, `scripts/runlaunch-freeze-gate.sh`, and Makefile wiring into `check-fast` + `check-short` |

**Measured, commit 3:** `internal/daemon/workloop.go` **6,705 → 6,473 (−232)**, `agentready.go`
**207 → 131 (−76)**; top-level non-test LOC **48,947 → 48,643 (−304)**; no whole file left, so the
file count is unchanged at 103. `internal/runlaunch` is a **daemon-free leaf** —
`go list -deps ./internal/runlaunch | grep internal/daemon` is empty.

`bootstate.go`'s two call sites are why this had to be a leaf rather than a runloop-private file:
`EmitSpawnCapBlocked` / `EmitTmuxNewWindowTimeout` are also boot-time spawn-semaphore instrumentation,
and `bootstate.go` never moves.

**Two recipe corrections — the same two classes keep recurring:**

1. **Every line number in the recipe was stale. Again — the third time in this one slice.** §1a's
   baseline was measured on the post-RT13 tree (49,064 LOC / `workloop.go` 6,854); by the start of
   commit 3 the tree was 48,947 / 6,705, because commits 1 and 2 of *this same slice* had already
   shifted it. Every symbol and call site was re-located by grep. **Treat recipe line numbers as
   commentary and grep for anchors — this is now the default, not the exception.**
2. **Step 17's freeze-gate check (3) was wrong as written and would have failed the moment it was
   wired.** Its own header prose said `dot_gate.go` was carved out until RT14, but the grep it
   specified had no carve-out and fires on `dot_gate.go`'s surviving
   `case <-time.After(runlaunch.KillReapTimeout)`. Converting that site inside an extraction is a logic
   change `_plan.md` §5.1 forbids and RT14 owns, so the gate ships with a named, temporary
   `--exclude='dot_gate.go'`. **The reviewer approved the carve-out but flagged that the
   delete-instruction lived only in the script and the commit body — neither of which an RT14
   implementer reads. Now recorded in `RT14-dispatchsegment-conversion.md` §0a, §1, §4 B4 and §4 B7.**

**Follow-ups recorded, not fixed:** `EmitAgentReadyTimeout`'s doc comment still says the zero fallback
is "30s" while the constant is 150s — wrong already in `internal/daemon`, moved verbatim under the
pure-move rule. And the gate's symbol ratchet keeps the bare name `After`, which matches nothing today
but is generic enough to false-positive later; kept for fidelity, recorded so the next maintainer
knows it was a choice.

### RT14 — retire the open-coded ready wait (COMPLETE)

Three commits, each independently reviewed. **This slice moves nothing out of `internal/daemon`** and
its LOC delta is −52 non-test lines. Reading that as the outcome misreads the slice.

| Metric | Before | After |
|---|---:|---:|
| agent-launch sites hand-rolling their own ready wait | 2 | **0** |
| raw wall-clock sites on the dispatch path | 1 | **0** |
| `dispatchSegment` production consumers | 3 files / 3 segments | **4 files / 5 segments** |
| top-level non-test files | 103 | 102 |
| top-level non-test LOC | 48,643 | 48,591 |

| # | Commit | What changed |
|---|---|---|
| A | `229e6e91` | single-mode `beadRunOne` → `&dispatchSegment{…}`, `reviewloop.go` as template |
| B | `cb89e35e` | cognition gate → `&dispatchSegment{…}`, `dot_cascade.go` as template; **closes the `--exclude='dot_gate.go'` carve-out RT19b-3 had to leave open** in `runlaunch-freeze-gate.sh`, making that check absolute |
| C | `7d448afb` | delete `agentready.go` (whole file) + `agentready_hkgql2018_test.go` + 2 export shims + `chanAgentEventSource`; add `scripts/readywait-freeze-gate.sh` |

**The reviewer earned its keep on phase A — one real, silent, untested regression caught.** The
recipe's `killAbort` template is `if sess != nil { sess.Kill(Background) }`, copied from the two
existing consumers. `driveDispatchOnce` maps `ctx.Done()` onto `EvAborted`, whose uniform edge returns
`ActKillAgent`, so `killAbort` fires **exactly when `ctx.Err() != nil`** — performing the same kill
that `runlaunch.ForceTeardownSession` performs. But at THIS site that teardown is guarded by hk-o85ye
(`!useIndepSession || ctx.Err() == nil`): on daemon shutdown a local independent-session run must
SURVIVE, because the session outlives SIGKILL and the next boot's adoption pass monitors it, which is
why the shutdown branch returns without `ReopenBead`. Unguarded, a shutdown landing in the
launch/ready/brief window would SIGKILL the session and strand the bead `in_progress` with nothing to
adopt. `killAbort` now carries the same guard. `reviewloop.go` / `dot_cascade.go` need none — their
teardown is unconditional and neither has a `useIndepSession` concept.

**Three recipe corrections, all of the same two recurring classes:**

1. **Line numbers stale again — fourth consecutive slice.** Assumed nothing; every symbol re-anchored
   by `grep -n`.
2. **§0's "do NOT delete `agentready.go`" correction was ITSELF stale.** It was written pre-RT19b-3
   and warned that four of the file's six symbols were live in production. RT19b-3 had already moved
   all four to `internal/runlaunch`, so at HEAD the file declared exactly `agentEventSource` +
   `waitAgentReady` and the whole-file delete was correct. **Both the original E5 step 17 AND its
   correction were wrong, in opposite directions** — which is precisely why §3c's
   comment-and-string-filtered grep has to be re-run rather than read. Re-run it: all four
   `runlaunch` symbols are still live (1/1/6/6 production references), and every surviving textual
   `waitAgentReady` is inside a string literal.
3. **§4 B5 listed `dot_gate.go`'s two cleanup defers in the REVERSE of that site's own order.** Its
   pre-RT14 order is `close(gateHBDone)` then `ForceTeardownSession`, so under LIFO the session is
   torn down BEFORE the heartbeat stops. Following the recipe would have silently inverted it.

**Two deliberate deviations in `readywait-freeze-gate.sh` vs recipe §4 C7:** `agentready.go` is
dropped from the pinned-file list (it no longer exists; pinning it would make the gate exit 1 on its
own landing commit), and a fifth check was ADDED pinning all four production launch sites to the
seam — the recipe stops at "the seam still exists," which passes even if every site quietly left it.
Proven in four failure directions before wiring.

**Descoped, filed not forgotten:** the Working-phase watchdogs still carry **30 raw wall-clock sites**
(`pasteinject.go` 22, `dot_gate.go`'s `pasteInjectQuitOnGateFile` 6, `waitsocketgrace.go` 1,
`postreadyhang.go` 1). `dispatchsegment.go` places them outside the RT8 segment boundary; E5 step 16's
"eight sites" conflated them with the segment. Now
[`RT19c-workingphase-watchdogs.md`](RT19c-workingphase-watchdogs.md).

**Coverage cost, stated not buried:** `twinparity_timing_property_test.go`'s stage-1 boundary race
disappears with `waitAgentReady`. It was an artifact of that function's wall-clock select over two
channels; the machine resolves the same edge deterministically on one goroutine. Stage 1 still drives
the REAL emitter. The alternative — a segment-level fixture — was named and NOT built; what was
rejected outright is keeping dead production code alive to serve one test.

### RT15 — `RunEnv` / `SharedHandles` threaded into `beadRunOne` (COMPLETE)

Seven commits, one per chunk, each ending green. **This slice moves nothing out of `internal/daemon`;
it makes RT18 possible.** Reading its `+124` LOC as the outcome misreads the slice — the added lines
are two constructors, an alias preamble and their godoc.

| Metric | Before | After |
|---|---:|---:|
| `beadRunOne` parameters | 16 | **6** |
| `deps.` reads inside `beadRunOne` | 185 | **126** |
| `RunEnv` / `SharedHandles` references in production | 0 (declared, dead) | **live, constructed per run** |
| `export_test.go` diff across the whole slice | — | **zero** (exit gate) |
| top-level non-test files | 102 | 102 |
| top-level non-test LOC | 48,591 | 48,715 |

| # | Commit | What changed |
|---|---|---|
| C1 | `f33af3a5` | `(*workLoopDeps).runEnv` in `runports.go` (all 19 fields) + `env :=` in `beadRunOne` + 2 readers |
| C2 | `7f32b420` | `targetBranch` ×3, `protectBranches` ×2 |
| C3 | `d04c0438` | `projectDir` ×20 (+4 comments) |
| C4 | `4a70d2be` | `defaultHarness` ×2, `projectCfg` ×3 |
| C5 | `e3d014a5` | **the signature**: 11 params → `env RunEnv` + alias preamble; production call site + 6 test call sites |
| C6 | `7794ab53` | `(*workLoopDeps).sharedHandles` (all 5 fields) + `localInFlight` ×4, `agentSpawnSem` ×3, `budgetPort` ×1 |
| C7 | `3ea214fb` | `runRegistry` ×6, `workerRegistry` ×8 |

**Why `export_test.go` never moved.** The 157 files referencing `ExportedWorkLoopDeps` /
`WorkLoopDepsParams` drive `ExportedRunWorkLoop(ctx, deps)`, never `beadRunOne`. Because `env` is a
DERIVED LOCAL rather than a replacement for `deps`, and no `workLoopDeps` field is deleted, the shim
needs no compatibility `RunEnv` and those files needed zero edits. The real blast radius was 7 call
sites, not 156 files.

**The trap held.** `itemWorkflowRef` is the only one of the eleven values `beadRunOne` reassigns —
`resolveWorkflowRef` applies the EM-012a tier-0/tier-1 resolution ~110 lines into the body and two
later dot-path readers depend on the resolved value. C5's preamble keeps it a LOCAL; reading
`env.ItemWorkflowRef` at either reader would have silently dropped the resolution with a green build.
Gate `grep -c 'env\.ItemWorkflowRef' internal/daemon/workloop.go` = 0 through C4 and 1 after C5, as
specified.

**Three recipe corrections — the same recurring classes, fifth consecutive slice:**

1. **Every line number stale again.** Every symbol was re-located by `grep -n`. The recipe's *counts*
   were right for C2/C4/C6/C7 but wrong for C3 (`projectDir` is 24 hits: 20 code + 4 comments, not
   20), and it never mentioned the six prose comments across C2/C3 that name the converted fields.
2. **§C6's prescribed local name does not compile.** The recipe says
   `shared := deps.sharedHandles()`. `shared` is the `harness/shared` package, which `beadRunOne`
   references eleven times below that point (`shared.LaunchCtx`, `shared.ArtifactAgentType`). The
   local is named `handles`, with a comment at the site saying why.
3. **C5 re-anchors three grandfathered complexity findings.** Rewriting the declaration line makes
   `funlen`, `gocognit` (398) and `cyclop` (223) "new" under `--new-from-rev`. They ship as one
   justified, specific `//nolint:funlen,gocognit,cyclop` naming the RT stream that retires them —
   the only honest fix is splitting a 2,280-line function, which is exactly what a
   behaviour-preserving slice must not do inline and what RT16–RT20 exist to do.

**One deliberate logic delta, in C3.** The run-registry removal defer discarded `runpkg.Remove`'s
error into the blank identifier; touching that line re-exposed the grandfathered `errcheck` finding.
Per the standing instruction it was FIXED rather than suppressed: `ErrNotFound` (the already-cleaned
case) stays silent as before, any other failure now reports to stderr in the shape
`adoptDeadRunSessions` already uses for the identical call. Control flow unchanged.

**What RT15 deliberately did NOT do.** No `shared SharedHandles` parameter (all five fields have
readers outside `beadRunOne`, so passing the bundle while `deps` is still passed is pure duplication
across a signature boundary — **RT18 owns it**). None of the six copy-mutation sites (`workloop.go`
3179/3977/3987 pre-slice, `dot_cascade.go` 219/1259, `reviewloop.go` 229) — **RT17 owns the two
`launchSpecBuilder` ones, RT18 the four `clock` ones**. `deps.tidGen` stays on `deps` (RSM-011;
**RT18's call**). `RunEnv.BrPath` is populated but has zero readers in `beadRunOne` — **RT18 surface**.

**Three newly-observed load-sensitive flakes**, all passing strictly in isolation and all added to
`00-test-oracle-baseline.md`: `TestWorkLoop_ShutdownDrainsCommittedRun_hkdnrg`,
`TestT2_ExitZeroNoSignal`, `TestWorkLoop_ClaimSemaphore_BoundsClaimConcurrency`.

**Review gate honesty note.** The Agent tool was not exposed to the session that ran RT15, so no
independent `agent-reviewer` sub-agent pass was possible on any of the seven commits. Each carries
`Reviewed-By: self` and a verdict that says so explicitly rather than claiming independence. **These
seven commits are the ones in the E5 stream that still want an independent pair of eyes.**

---

## 9. Measured baselines (so later numbers are comparable)

`internal/daemon` at `34509e60` + the staged base, before any extraction:
**126 top-level non-test files / 57,197 LOC** (recursive: 134 / 58,971).

**Two test oracles, both already red before I started — see `00-test-oracle-baseline.md`:**

- `internal/daemon` suite: 1 hard pre-existing failure (`TestThroughput_TenBeadsAtMaxFour`) + 5
  load-sensitive flakes. Gate is "**no NEW failures**", not zero.
- `specaudit`: 7 top-level failures at HEAD. Same rule.

Do not run the full daemon suite concurrently with agent fan-out — that is what manufactured five of the
six original failures. Serialize it.
