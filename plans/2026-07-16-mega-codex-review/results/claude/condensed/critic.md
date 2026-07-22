# Completeness Critic — what the condensers dropped or under-ranked

Method: read all 31 raw RU files + the 3 condensed files (correctness / architecture / coverage), then cross-referenced every raw finding by `file:line` against the condensed set. Below are real raw findings that **no** condensed file carried, plus RU-by-RU coverage-confidence.

Headline: the correctness condenser dropped **three HIGH findings and two MEDIUM nil-deref crashes** entirely. The biggest systemic miss is RU-04: the condensers carried RU-04**b**'s highs (gitignore, discoverworktrees) but silently dropped RU-04's two HIGH SSH-swallow findings — they appear to have conflated the two review units (RU-04 vs RU-04b share the `workspace/` package). The condensers even carried a LOW *cosmetic* finding about `reviewverdict.go`/`autostatusmarker.go` (the diffhash local/remote classification nit, arch #32) while dropping the HIGH correctness bug in those same two files.

---

## Recovered / under-ranked findings

### Dropped HIGH (carried by no condensed file)

1. **HIGH — SSH transport failure on verdict read swallowed as "verdict absent"** — `internal/workspace/reviewverdict.go:176` (RU-04). A transport/SSH error reading the reviewer verdict is treated as "no verdict present," so an inconclusive-because-unreachable read is mapped to the same outcome as a genuinely-absent verdict → wrong review-gate decision on a remote worker. **Not in any condensed file.**

2. **HIGH — SSH transport failure on `auto_status` read swallowed as absent → wrong outcome gate** — `internal/workspace/autostatusmarker.go:99` (RU-04). Same failure class as #1 on the auto-status marker path: an unreachable remote read is indistinguishable from "marker not written," silently mis-gating the run outcome. **Not in any condensed file.**

3. **HIGH — staleness re-capture silently swallows a beads-audit fetch error, defeating RC-024** — `internal/daemon/verdictexecutor_rc025a.go:151` (RU-02). The condenser carried this file's *`:439`* medium (resume-here/reset-to-checkpoint no-op = M11) but dropped the HIGH at `:151`: the RC-024 staleness re-capture eats the audit-fetch error, so a stale verdict can execute against out-of-date state — the exact condition RC-024 exists to block. **Not in any condensed file.**

### Dropped MEDIUM (nil-deref crashes — no condensed file carried these)

4. **MEDIUM nil-deref — `stepIdleGaugeTick` dereferences `ev.CF` with no nil guard** — `internal/keeper/step.go:447` (RU-13). Unlike the precompact entry (which nil-checks), the idle-gauge tick path dereferences `ev.CF` unconditionally → keeper crash on a CF-less event. **Not in any condensed file.**

5. **MEDIUM nil-deref — `Handle` after `Close` nil-derefs the log file** — `internal/structuredlog/handler.go:264` (RU-23). A structured-log `Handle` call after `Close` dereferences the closed file pointer instead of returning an error → panic during shutdown/teardown ordering. **Not in any condensed file.**

### Dropped LOW/real (no condensed file carried these)

6. **LOW nil-deref — `computeHookEnvelopeHash` derefs `cp.Evaluator.DelegationPath` with no nil guard** — `internal/hooksystem/cognition_cp017.go:78` (RU-14). Crash surface on a hook envelope with a nil evaluator/delegation path.

7. **LOW nil-deref — human digest slices `EventID[:8]`/`RunID[:8]` without a length guard** — `cmd/harmonik/digest.go:243` (RU-16b). Panics on a short id in `harmonik digest` human output.

8. **LOW — `List` silently drops registry records that fail to load** — `internal/run/registry.go:121` (RU-03). Masks corruption from the post-SIGKILL adoption sweep (the condenser carried `registry.go:52` durability but not this record-drop).

9. **LOW error-handling — cognition-gate ready-wait uses `==` on the timeout sentinel** — `internal/daemon/dot_gate.go:402` (RU-02). A wrapped/ctx-cancel return falls through to paste-inject on a dead session (real correctness bug, not cosmetic).

10. **LOW error-handling — event payload marshal errors silently drop lifecycle events** — `internal/queue/rpc.go:1023` (RU-09). Integrity gap: a marshal failure drops a queue lifecycle event with no surfaced error.

11. **LOW error-handling — pervasive silent discard of `bus.Emit`/`Marshal`/`ReopenBead` errors across the loop and run paths** — `internal/daemon/workloop.go:1849` (RU-01a). Systemic; the condensers carried two other workloop LOWs (1668, 1202) but not this cross-cutting one.

12. **LOW efficiency — cross-queue duplicate scan is O(queues·groups·items) under the write lock every dispatch** — `internal/daemon/workloop.go:2532` (RU-01a). Real perf finding on the hot dispatch path, dropped. (The condensers dropped efficiency findings as a class — see also #13.)

13. **LOW efficiency — `reapOrphanWorktreesFromArchives` re-reads and re-parses every archive file per Class-B orphan bead** — `internal/lifecycle/startup_pl005_qm002.go:783` (RU-12). O(archives·beads) startup cost.

14. **LOW — `dot_gate.go:473`** run-context JSON marshal error ignored when building the gate-task brief (RU-02).

15. **LOW — `jsonlwriter.go:383`** Filter logs a spurious error for a missing JSONL file, inconsistent with ScanAfter/replay (RU-11).

### Dropped minor (batched — real but low-impact, each in exactly zero condensed files)
- `internal/lifecycle/tmux/osadapter.go:651` — isNotFoundErr/isNoSessionErr/isWindowCollisionErr rely on brittle substring matching (RU-05b).
- `internal/keeper/tmuxresolve.go:312` — recentTranscriptTurn ignores scanner error, silently truncating on an over-long line (RU-05b).
- `internal/daemon/projectconfig.go:1923` — daemonExpandHomePath error swallowed in parseHarnessesBlock (RU-06).
- `cmd/harmonik/promote_cmd.go:349` — bead-ID auto-detection error fully swallowed, no diagnostic (RU-17).
- `internal/brcli/dblockretry.go:152` — escalation diagnostic reports empty stderr for the BrUnavailable-timeout exhaustion path (RU-18).
- `internal/eventbus/jsonlwriter.go:331` — ScanAfter can't distinguish a torn tail from genuine corruption; logs every replay (RU-11, nit).

**Note on carried-but-grep-missed:** `rpc.go:1092`, `agentevents_hqwn59.go:570` (deferred-typing), `dispatcher.go:123`, `supervise_cmd.go:68`, `keeper-statusline.sh:53` ARE carried in the architecture nits section (cited by file, not line) — not dropped.

### Ranking observation
No finding was buried as low that should be critical/high — the severity mapping the condensers *did* carry is sound. The failure mode here is **omission**, not misranking: 21 real findings (3 HIGH, 2 MEDIUM, 16 LOW/nit) fell out entirely, concentrated in (a) RU-04 vs RU-04b conflation and (b) the condensers' apparent policy of dropping efficiency/error-swallow LOWs.

---

## Coverage-confidence per RU

| RU | scope | status | confidence | note |
|----|-------|--------|-----------|------|
| RU-01a | workloop.go 1-4100 | reviewed | **high** | two god-fns fully engaged |
| RU-01b | workloop.go 4100-end + 5 files | reviewed | **LOW** | self-noted: 4000-line workloop body read only selectively, not line-by-line |
| RU-02 | dot_cascade/reviewloop/gate/verdict | reviewed | **medium-LOW** | read dot_cascade to ~1693/2669 and reviewloop to ~997/2000; **tails of both god-functions unaudited** — and its own HIGH (:151) was then dropped by the condenser |
| RU-03 | runexec/mergeq/run | reviewed | high | thorough |
| RU-04 | workspace remote/worktree + workers/tmux | reviewed | high (review) / **LOW (survival)** | good review but **both its HIGHs were dropped downstream** |
| RU-04b | workspace merge/conflict/lease | reviewed | high | strongest workspace pass |
| RU-05a/05b | tmux substrate/adapter | reviewed | high | dense, specific |
| RU-06 | daemon/config/socket/router | reviewed | high | |
| RU-07 | codex driver/wire/twin | reviewed | high | |
| **RU-08** | substrate/handler/handlercontract | **PARTIAL** | **LOW** | ~50 HC-xxx one-clause files + ~14k LOC tests sampled, not line-audited. Only RU with non-reviewed status. Under-reviewed surface for further test-theater/dead-code/duplication. |
| RU-09 | queue rpc/append/state/persist | reviewed | high | |
| RU-10 | core eventregistry | reviewed | high | |
| RU-10b | core 237 type/payload files | reviewed | **LOW** | explicit skim/classify over ~26k LOC; systemic-pattern focus, not a line audit — dead/deferred-typed fields likely under-counted |
| RU-11 | eventbus | reviewed | high | |
| RU-12 / RU-12x | lifecycle sweeps + reconcile-close | reviewed | high | |
| RU-13 | keeper watcher/step/cycle | reviewed | high (review) | but its MEDIUM nil-deref (step.go:447) was dropped |
| **RU-14** | hook/hooksystem/hookrelay/policy/orchestrator (13 files) | reviewed | **LOW/thin** | only 5 findings for 13 files; **`internal/policy/` (ratelimit, gate, autoresume, drain) and `internal/orchestrator/` (groupadvance, eagerfill, select) produced ZERO findings** — large behavior-critical surface, suspiciously clean |
| RU-15 | workflow/dot + validators + goalstate | reviewed | high | strong dead-code catches |
| RU-16a | cmd/harmonik core verbs (18 files) | reviewed | high | found the dead confirm/veto surface |
| **RU-16b** | cmd/harmonik eval + lifecycle verbs (20 files) | reviewed | **medium/thin** | self-noted: dashboard_cmd, ops_monitor_cmd, release_cmd, sync_assets_cmd, keeper_*_cmd, resolve_*.go, sleepwake helpers, asset_manifest/reconcile only **skimmed, not line-audited** |
| RU-17 | supervise/workers_boot/promote | reviewed | high | |
| RU-18 | brcli (20 files) | reviewed | high | |
| RU-19 | keepertest/codexdigitaltwin | reviewed | high | classify-lens, sound |
| RU-20 | operatornfr | reviewed | high | decisive delete verdict |
| RU-21 | specaudit | reviewed | high | |
| RU-22 | scenario harness | reviewed | high | three strong dead-subsystem catches |
| **RU-23** | 37-file supporting grab-bag | reviewed | **medium/thin** | broad-but-shallow ("code quality high overall"); many packages got zero findings (apptap, branching, crew/registry, dashboard, presence, release, replay, schedule, sessiondata) — a 37-file scope in one unit is inherently a skim, and it dropped a MEDIUM nil-deref of its own (structuredlog:264 survived here but was dropped by the condenser) |
| RU-25 | 39 specs / ~2.5MB + drift spot-checks | reviewed | **medium** | explicitly sampled; verification hand-done on highest-risk clauses only — undetected spec drift likely remains across the other ~30 specs |

**Thin-coverage units (large/critical scope with shallow or zero-finding coverage):** RU-08 (partial), RU-14 (policy+orchestrator zero findings), RU-16b (skimmed lifecycle verbs), RU-23 (37-file grab-bag), RU-10b (237-file skim), RU-01b (workloop body not line-audited), RU-02 (god-function tails unaudited), RU-25 (spec verification sampled).
