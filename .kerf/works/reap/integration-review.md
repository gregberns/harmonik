# reap — Integration Review

Fresh-context re-read of `06-integration.md` + `SPEC.md` against `05-specs/{C1..C6}`, `03-components.md`, `01-problem-space.md`, and the applied change-spec critical-review must-fixes (the spawning agent runs without the Agent tool; this is the project review-gate fresh-context re-read). **Verdict: PASS — no contradictions, all success criteria traced, all must-fix resolutions consistently propagated.** One advisory note carried to Tasks.

## Review checklist (jig Review Criteria)

### 1. Every success criterion → component → change-spec section — PASS
`06-integration.md` §8 maps all nine success criteria (01-problem-space) to a component + a change-spec section + an AC. Verified each: crit-1→C2/AC1, crit-2→PL-006d(existing)+C4 PL-019(i)/AC2, crit-3→C1/AC1, crit-4→C3/AC1-AC2, crit-5→C4/AC3-AC4, crit-6→C4/AC5, crit-7→C6/AC1, crit-8→C5/AC1, crit-9→all/negative-fixture. No orphan criterion.

### 2. Interface definitions consistent across components — PASS
The four shared resources (`06-integration.md` §2, `SPEC.md` §3) are stated identically in producers and consumers: (a) the pre-sweep `DiscoverActiveRuns` set is "produced once at PL-005 step 3, consumed by C1 at step 3 and C3 at step 8a" in C1 PL-006e (ii), C3 QM-002c (i), SPEC §3.1, and integration §2.1 — no divergence; (b) the `daemon_orphan_sweep_completed` additive payload (existing `bead_in_progress_reset`/`coordinator_sessions_skipped` + new `worktree_dirs_removed`/`dispatched_items_reconciled`) is consistent across C1/C2/C3 and §2.2; (c) the §8 exit-code ordered obligation (25→`CommandSupervise`→24→26) is identical in C5, C6, §2.3, SPEC §3.4; (d) the PL-005 step labels (1.5 flywheel-lock, 1.6 boot-backoff) match in C5, C6, §2.4, SPEC §3.3.

### 3. No contradictions between component specs — PASS (the must-fix that motivated this gate is closed)
- **C1↔C3 ordering (was the blocking contradiction):** RESOLVED. Both now cite the step-3 pre-sweep `DiscoverActiveRuns` set as the single survive-check source, NOT the step-7 model set. Grep confirms no remaining "re-attached set re-attached by PL-005 step 7" claim feeding a step-3 check; the only step-7 mention in C1 is the explanatory note stating the step-7 set does NOT exist at step 3 (the correct framing).
- **C5↔C6 step-1.5 collision:** RESOLVED. 1.5 = flywheel-lock, 1.6 = boot-backoff; distinct labels asserted in both specs + both integration artifacts.
- **C5↔C6 exit-code allocation:** RESOLVED. Both reference the shared ordered obligation; 26 explicitly "after C5's 25-absorption + `CommandSupervise`."
- **C1↔C3 sidecar/terminal-failure read (CF-3):** CONSISTENT. C1 PL-006e (iii) preserves unreconciled-sidecar worktrees; C3 (b) `dead_run_failed` reads that preserved evidence; `06-integration.md` §5.1 makes the ordering explicit.

### 4. Integration concerns addressed — PASS
Initialization order (`06-integration.md` §3 build steps A–G with the C5-before-C4 + step-1.5-before-1.6 + 25-before-24-before-26 hard constraints), shared state (§2), cross-component error propagation (§4: one `ErrBeadsUnavailable` degradation shared by C1+C3; conservative-skip on `ShowBead` failure; append-failure falls back to prior provenance), logging/config/idempotency cross-cuts (§4), runtime ordering dependencies (§5.1-§5.4). The `daemon_generation` counter ownership (C6 owns, C2/C5 read) is resolved with a documented degradation if C6 lands later (§2.5).

### 5. SPEC.md is a faithful assembly — PASS
`SPEC.md` carries the six normative clauses verbatim-in-substance from `05-specs/`, the shared integration contract from `06-integration.md` §2, and the ACs — it adds NO requirement and changes NO decision. The Axes lines, exclusion orderings, and exit-code values match the component specs. The Files table (§5) and testing (§6) are summaries pointing back to `05-specs/`/`06-integration.md`, not new contracts.

## Advisory (carry to Tasks / implementation, NOT blocking)
- **A1 — `daemon_generation` source if C6 lands separately.** SPEC §2.5 documents C2/C5 using a daemon-start-ns surrogate until C6's `bootlog.go` lands. The Tasks pass should ensure the C2 (hk-9eury) and C5 (hk-li14r) implementation tasks note this dependency on C6 (hk-7t9g1) so a partial merge does not produce two generation sources. This is a sequencing note, not a spec defect.
- **A2 — code-17 dual-meaning** (`start.go:19` local vs registry code-17) is deferred as non-blocking by C5 and the integration plan; it remains a documented reconciliation, correctly NOT bundled into the 24/25/26 obligation.

## Verdict
PASS. The integration plan and assembled SPEC form a coherent whole; the six change-spec must-fixes are resolved and consistently propagated; every success criterion traces; no inter-component contradiction remains. Advance to Tasks.
