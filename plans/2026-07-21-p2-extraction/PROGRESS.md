# P2 EXTRACTION — live progress + file ownership

**Owner of this document:** the P2 extraction agent (Claude Opus 4.8, session `59707ade`).
**Last updated:** 2026-07-22 — P2 core COMPLETE (9/9) + RT13 + E4c landed. 13 commits.

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
| **6. E5 RT stream + E4c** | RT13, E4c landed; RT14/16/19b + RT15/17/18/19/lift planned | **IN PROGRESS** |
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

`internal/daemon` at `34509e60` + the staged base, before any extraction:
**126 top-level non-test files / 57,197 LOC** (recursive: 134 / 58,971).

**Two test oracles, both already red before I started — see `00-test-oracle-baseline.md`:**

- `internal/daemon` suite: 1 hard pre-existing failure (`TestThroughput_TenBeadsAtMaxFour`) + 5
  load-sensitive flakes. Gate is "**no NEW failures**", not zero.
- `specaudit`: 7 top-level failures at HEAD. Same rule.

Do not run the full daemon suite concurrently with agent fan-out — that is what manufactured five of the
six original failures. Serialize it.
