# Spec-parent implementation epics — batch 2 of 2 (assessor: spec2)

**Cluster headline:** All 5 epics are OPEN with **100% of their children CLOSED** (0 open children each). The implementation landed — commits reference the child bead IDs, and the subsystem code exists under `internal/`. These are textbook **stale-open epics** by the rubric's own precedent (hk-uxm0j was closed this session for exactly this: all tasks landed → close the epic). Verdict for all 5 is **DONE** (close the epic with an "epic complete: all N children landed" reason).

**One material nuance (applies to all 5):** every epic was beaded against a *fixed* spec version, and each spec has since advanced past that version (WM v0.4.2→0.4.5, PL v0.4.1→0.4.8, ON v0.4.1→**0.5.3**, RC v0.4.0→0.4.5, SH v0.2.0→0.2.2). The post-epic spec increments are **not tracked by these epics** (their child sets are frozen and fully closed) and I found **no separate open bead** covering them. So closing each epic is correct for the scope-as-beaded; the spec-drift delta, if it needs implementation, belongs in a **fresh** spec-drift epic, not in reopening these. Flagging so the apply phase doesn't read "DONE" as "spec fully implemented at HEAD."

Contrast anchor: the sibling spec-parent epic hk-8i31 (Handler Contract) is genuinely incomplete — it still has an in-progress child (hk-8i31.61). None of our 5 do. That asymmetry is the cleanest signal these 5 are done-and-stale.

---

### hk-8mwo — Workspace Model spec implementation
- VERDICT: DONE
- ACTION: br close (all 72 children closed; epic is stale-open — reason "Workspace-model epic complete: 72/72 children landed in internal/workspace/, spec impl as-beaded done")
- NEW_PRIORITY: -
- EVIDENCE: `.beads/issues.jsonl` — 72 children `hk-8mwo.1..72` all status=closed (latest close 2026-06-01); impl in `internal/workspace/` (92 .go files); e.g. hk-8mwo.65 closed "WM-001..004 foundation harness in internal/workspace/". Epic beaded against workspace-model.md v0.4.2; spec now `version: 0.4.5` (drift unbeaded — note above).
- CONFIDENCE: high

### hk-8mup — Process Lifecycle spec implementation
- VERDICT: DONE
- ACTION: br close (all 63 children closed; epic stale-open — reason "Process-lifecycle epic complete: 63/63 children landed; daemon now live (pidfile/orphan-sweep/set-concurrency/supervise) — the daemonization deferral is resolved")
- NEW_PRIORITY: -
- EVIDENCE: 63 children `hk-8mup.1..63` all closed (latest 2026-05-27). The STATUS.md §"Phase-1 scope: daemonization deferred (2026-05-08)" (lines 117-152) concern is now MOOT: daemon is live in production — `internal/daemon/orphansweep.go`, `concurrencycontroller.go`, `set_concurrency_hkohiaf_test.go`, `internal/supervise/{supervisor,daemon_watchdog}.go`, pidfile lock (hk-li14r). The PL children that were daemon-dependent landed; not deferred. Spec drift v0.4.1→0.4.8 (unbeaded — note above).
- CONFIDENCE: high

### hk-sx9r — Operator NFR spec implementation
- VERDICT: DONE
- ACTION: br close (all 84 children closed; epic stale-open — reason "Operator-NFR epic complete: 84/84 children landed in internal/operatornfr/ + daemon"); FILE follow-up: spec drifted v0.4.1→**v0.5.3** (largest drift in cluster) — if those increments need impl, open a fresh ON spec-drift epic, do NOT reopen this.
- NEW_PRIORITY: -
- EVIDENCE: 84 children `hk-sx9r.1..84` all closed (latest 2026-06-01); impl in `internal/operatornfr/` (43 .go) + daemon (ready/shutdown/drain/budget). Commit 459ad3bd "ON-041 multi-daemon commands (hk-sx9r.57)", 06d4934f "ON-048 exhaustion protocol", da049da2 "ON-047 budget defaults". Daemonization-deferred items (drain/RTO/attach) landed via live daemon. Largest version drift: epic v0.4.1 vs spec `version: 0.5.3`.
- CONFIDENCE: high

### hk-63oh — Reconciliation spec implementation
- VERDICT: DONE
- ACTION: br close (all 82 children closed; epic stale-open — reason "Reconciliation epic complete: 82/82 children landed; reconciler reaps orphans + verdict-executor live")
- NEW_PRIORITY: -
- EVIDENCE: 82 children `hk-63oh.1..82` all closed (latest 2026-06-01); also standalone dependent hk-zixbp closed. Commits: 2fd35ed2 "RC-INV-004 evidence-corroboration sensor (hk-63oh.45)", 96b84631 "RC-025a daemon-side verdict-executor 7-step (hk-63oh.36)", e351a93d "RC-002a per-run flock lock (hk-63oh.4)", 246aad17 "RC-020a detector cadence (hk-63oh.21)". Recon code in `internal/lifecycle/` (7 recon files) + `internal/brcli` + `internal/core`. Orphan-reap is live (hk-5pg37 / ProvenanceChecker landed, commit 5c51df8f). Spec drift v0.4.0→0.4.5 (unbeaded — note above).
- CONFIDENCE: high

### hk-i0tw — Scenario Harness spec implementation
- VERDICT: DONE
- ACTION: br close (all 54 children closed; epic stale-open — reason "Scenario-harness epic complete: 54/54 children landed in internal/scenario/")
- NEW_PRIORITY: -
- EVIDENCE: 54 children `hk-i0tw.1..54` all closed (close window 2026-05-08..2026-05-11); standalone dependent hk-ido0 (FailureClass enum) also closed. Impl in `internal/scenario/` (51 .go files). Smallest spec drift: epic v0.2.0 vs spec `version: 0.2.2`.
- CONFIDENCE: high

---

## Cluster summary

| Epic | Title | Children (open/total) | Verdict | Spec drift (epic→HEAD) |
|------|-------|----------------------|---------|------------------------|
| hk-8mwo | Workspace Model | 0/72 | DONE | v0.4.2 → 0.4.5 |
| hk-8mup | Process Lifecycle | 0/63 | DONE | v0.4.1 → 0.4.8 |
| hk-sx9r | Operator NFR | 0/84 | DONE | v0.4.1 → **0.5.3** |
| hk-63oh | Reconciliation | 0/82 | DONE | v0.4.0 → 0.4.5 |
| hk-i0tw | Scenario Harness | 0/54 | DONE | v0.2.0 → 0.2.2 |

**Verdict counts:** DONE ×5. (No KEEP / OBSOLETE / APPROACH-STALE / DUPLICATE / REPRIORITIZE.)

**Cross-bead themes:**
1. **All 5 are stale-open epics — single clean pattern.** 355 children total, 100% closed, 0 open. Precedent hk-uxm0j (agent-comms epic) was closed this session under identical conditions with reason "epic complete: all tasks landed." Apply phase should batch-close all 5 the same way.
2. **The daemonization-deferral premise (rubric's special flag) is fully resolved.** PL (hk-8mup) and ON (hk-sx9r) were the most entangled with STATUS.md §"daemonization deferred 2026-05-08." Re-examined: the deferred children did NOT remain parked — they landed via the now-live real daemon (pidfile lock hk-li14r, orphan sweep, set-concurrency RPC, supervise watchdog, drain/shutdown). Same for RC's orphan-reap (hk-5pg37, commit 5c51df8f). So these are **DONE-via-real-daemon**, not APPROACH-STALE — the work shipped, it just shipped through infrastructure the original beads predated, and the child closures already reflect that.
3. **Spec drift is the only loose thread, and it is NOT tracked by these epics.** Each epic froze a child set against a pinned spec version; every spec has since advanced (ON most: v0.4.1→0.5.3). No open bead covers the deltas. Recommendation for apply phase: close all 5 epics as DONE, and separately decide whether the post-epic spec increments warrant a new "spec-drift implementation" epic per spec (esp. operator-nfr). Do NOT reopen/hold-open these 5 to absorb drift — that re-creates the stale-open anti-pattern.
4. **Confidence high on all 5** — verified three ways each: child-status count from `.beads/issues.jsonl`, subsystem code presence under `internal/`, and commit messages referencing the child bead IDs.
