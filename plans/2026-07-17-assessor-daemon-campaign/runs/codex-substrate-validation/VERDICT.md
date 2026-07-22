# Codex-Substrate Pre-Deploy Gate — ASSESSOR VERDICT

**Verdict: BLOCK.** Do NOT greenlight the prod codex-daemon reboot on the codex substrate.

- **Pin:** `9db85569` (== origin/main; contains hk-fufel PR#33). Gate scope: prove the codex substrate (`HARMONIK_SUBSTRATE=codexdriver`) works end-to-end, local + remote, before prod reboots on it.
- **Task:** operator-directed, admiral-relayed. Prod codex-daemon reboot GATED on this PASS.
- **Assessor:** independent (did not build the substrate). Legs delegated to subagents; verdict is my own reasoned fold.

## Evidence fold (four proof points across two legs)

| # | Proof | Result | Leg |
|---|-------|--------|-----|
| — | Fail-closed isolation guard (unit `TestCodexIsolationGuard_HK5H759`, 5 cases) | **GREEN** | A |
| — | Fail-closed real-dispatch refusal (`run_started=0`, `run_failed` reason contains `isolation-boundary`) | **GREEN** | A |
| — | ssh worktree-create + commit-land git lifecycle (localhost, **stub handler**) | GREEN (fidelity-limit: not a real codex exec) | A |
| 1 | **Real remote codex exec** on gb-mbp (exit 0, 99s, real `codex_core` rollout session) | **GREEN** | B |
| 2 | Fresh worker repo (not prod) | GREEN | B |
| 2 | **Commit lands** | **RED** — sandbox denies `git commit` → exit-0 silent no-op | B |
| 3 | Routing (`run_started.worker_name=gb-mbp`, `workflow_mode=dot`) | **GREEN** | B |
| 4 | **hk-g5wkt review node relay** on remote onboarded worker | **STALL** — `agent_ready_timeout` (201s) | B |

## Why BLOCK (reasoned judgment, not a bead tally)

The substrate's two load-bearing critical-path behaviors both FAIL on the remote worker:

1. **The crew's own commit is blocked; work only lands via a fallback and never reaches the target branch.** The remote codex crew runs under `sandbox_mode=workspace-write`, so codex's own `git commit` is denied by the seatbelt sandbox ("Operation not permitted / unable to index file / fatal: updating files failed") and `commit_landed=false`. The isolation-guard's premise ("danger-full-access would run unsandboxed") is not what actually runs. → `hk-daegv` (P0, remediation:blocking).
   - **Accuracy refinement (independent Leg-B repro, run `019f7bc5`):** the daemon **fallback** commits codex's applied patch to the `run/<id>` branch (HEAD `123b2ae`, `counter.py` `return 1→2`), so this is **not** a literal "nothing committed anywhere" no-op — the patch reaches the run branch. But it **never merges to the target branch or reaches `close`**, because the review node stalls (defect 2) and the run fails before merge. So the deploy-blocking consequence stands; the daemon-fallback safety net lowers the practical blast radius of defect 1 alone.

2. **It cannot pass the review node.** The pinned claude-code review node (`agent_type=claude-code tier=3`) stalls to `agent_ready_timeout` on gb-mbp **even though gb-mbp is claude-onboarded** — onboarding alone did not make it relay (hk-g5wkt confirmed on a remote onboarded worker). Review is the sole inbound edge to `close`, so **every DOT bead is blocked from completing** on the codex substrate. → `hk-qxvc2` (P1, remediation:blocking).

**Claimed-vs-reality reconciliation:** the "codex substrate works end-to-end local+remote" claim is REFUTED. Local self-contained proves the guard + git lifecycle but not a real codex exec; remote proves a real codex exec + routing but cannot get work through review to `close`. End-to-end is not demonstrated.

**Corroboration:** two independent Leg-B subagents (runs `019f7bbc` and `019f7bc5`) reached the same BLOCK. The review-node stall reproduced on **both** runs (201s and 189s). Their duplicate beads (hk-36xy5 → hk-qxvc2; hk-wwyse → hk-daegv) are commented for consolidation.

## What IS proven (for the fix track)
- The fail-closed isolation guard is solid (both unit and real-dispatch).
- Real codex processes execute and route correctly to the remote ssh worker.
- The ssh git lifecycle (worktree-create + commit-land over a real ssh boundary) works with a stub handler; the fresh-repo empty-HEAD race (hk-iaj1w) did not recur.

So the failures are two specific, addressable defects (sandbox mode; remote review relay), not a broken substrate skeleton. Re-gate after both are fixed.

## Defects filed (found-by:assessor, remediation:blocking)
- **hk-daegv** — P0 — codex crew commits blocked by `workspace-write` sandbox → exit-0 silent no-op.
- **hk-qxvc2** — P1 — pinned claude-code review node stalls on remote onboarded worker (hk-g5wkt confirmed).

## Non-blocking notes
- `codex.stale_wal_max_bytes` must be set or the implement node hard-fails at `buildCodexRoutedLaunchSpec` (config gap, not a defect).
- codex-cli version observed 0.142.0 on worker vs 0.144.5 local — non-blocking (no version binding).
- Baseline reconciliation: earlier handoff cited the live fleet as "12 sessions"; both legs observed **6** under `a3dc45482890` (unchanged before/after). Fleet supervise 21849 + watcher 38536 verified alive throughout; not gate-relevant.

## Teardown — CONFIRMED CLEAN
- Local iso daemons (codexA, codexB pid 53081), their supervisors, reverse-ssh tunnel, and monitor shells all killed; `pgrep h-assessor/codex` empty.
- Remote fresh worker repo `/Users/gb/harmonik-assessor-iso` removed (guarded).
- **Prod safe:** `/Users/gb/harmonik-worker/repo` present (HEAD 0553d4b); box-A prod `.harmonik/workers.yaml` sha `ef91bf42` UNCHANGED.
- **Live fleet untouched:** supervise pid 21849 + preempt watcher pid 38536 alive.

## Reports
- `runs/codex-substrate-validation/LEG-A.md` — self-contained (guard + git lifecycle).
- `runs/codex-substrate-validation/LEG-B.md` — remote codex exec + review-node crux.
- `runs/codex-substrate-validation/RUN-LOG.md` — scope + prerequisite probes.
