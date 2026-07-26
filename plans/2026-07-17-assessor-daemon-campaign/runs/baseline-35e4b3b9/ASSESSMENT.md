# Assessor Baseline Verdict — pin 35e4b3b9

- **Pass:** baseline-35e4b3b9 (merge-gate)
- **Pin:** `35e4b3b91d9961274646a2575a4c108e1d99d35f` on `phase1-session-restart-substrate`
- **Verdict:** **BLOCK** (at pin)
- **Scope note:** This verdict is valid ONLY for PIN_SHA 35e4b3b9. As of close, HEAD is `a0591ba3` (21 commits ahead) and **every gate-blocking finding below has been remediated** on HEAD. The authoritative release verdict is the successor **a0591ba3 full baseline** (admiral, 2026-07-18 22:32Z).

## Verdict basis (my reasoned judgment over the legs — not a bead tally)

**BLOCK** at pin 35e4b3b9 on the strength of two runtime-reachable P1 dispatch wedges confirmed by cold code review and independently reproduced by the fix crew's discriminating regression tests:

- **hk-l5saf (P1)** — workloop local-only queue item stranded `Dispatched` by the hk-hs7ex secondary-cap guard (comment "no state to undo" is wrong; the item was already stamped + persisted). Permanent group stall until daemon restart. Core-loop-adjacent.
- **hk-3hozm (P1)** — workloop remote worker-slot leak on refuse-before-launch early returns; wedges remote dispatch after MaxSlots refusals.

Both are gate-blocking (`remediation:blocking`). A single permanent dispatch wedge on the core loop fails the good-enough bar regardless of the rest of the grid.

## Leg summary

| Leg | Status | Result |
|-----|--------|--------|
| **Green-tree precondition (§0.D.4)** | DONE | **GREEN.** BUILD+VET clean. Every daemon-isolation red root-caused non-product by 3 converging investigations (srt i0377/hki0377 + SocketBinds = campaign `TMPDIR=/tmp/h-assessor` defeated the test preconditions, PROVEN pass at real `/var/folders` TMPDIR; ShutdownDrains/Throughput/ClaimSemaphore/SubscribeStream = load-flakes, invariants intact; SSH ReviewVerdict = stale pre-fix-D test). Only deterministic real red = known P3 hk-uhxwd corpus-lint (non-product). No product regression, no srt bypass. |
| **LT — live core-loop (S1)** | DONE | **LT-RED, no daemon-workflow defect.** `pi:local` GREEN — core-loop proven end-to-end with a real agent on phase1 code (gap1/gap3/gap4/t10 all pass, landed d0a83299). `pi-dot:local` RED = ornith (weak same-model reviewer) can't converge a multi-turn DOT round-trip; the DOT mechanism fired correctly and the daemon failed LOUDLY — coverage gap (needs stronger model), not a defect. `claude:local` RED = agent_ready_timeout + fixture gaps (hk-oga33). codex/remote SKIP (operator codex-minimal / no remote substrate). Reds = model/fixture/harness; daemon behaved correctly throughout. Harness findings: hk-es4f7, hk-xy9ym. |
| **XT — exploratory break-testing** | **DONE (post-restart)** | The session-4 XT subagent completed after the keeper restart. **XT-CLEAN.** Restart-substrate angles all PASS: event_id HWM cross-restart monotonicity + graceful degradation on a corrupt HWM file (EV-002c, never fatal); socket-bind-before-restart-backoff (backoff throttles dispatch, not liveness); drain integrity; NO lost-wakeup; NO double-run on SIGKILL + supervisor auto-revive (stranded item reset `dispatched→pending`, re-dispatched with a distinct run_id, 2:47 apart). **Blast-radius refinement:** both P1 wedges (l5saf/3hozm) proven **remote-worker-gated** — with no `workers:` config, `HasFreeSlot()` is always false and the strand/leak preconditions are structurally unreachable, so the wedges are **inert in any local-only deployment**. Static control-flow at pin still matches the beads exactly; the fix crew's discriminating tests independently reproduced the pre-fix bug. |
| **CR — independent cold review** | DONE | 5-subagent cold review of the branch diff; 14 `found-by:assessor` findings filed. Positives confirmed: merge-vs-strand/DOT wiring intact, H13 lost-wakeup NOT present, socket-bind ordering correct, hk-ky7ye supervisor fixes correct, H8 remote-kill correct. Full table: `CR-FINDINGS.md`. |

## Findings (all filed `found-by:assessor`, scoped `assessor-campaign-35e4b3b9`)

**P1 (3):** hk-l5saf, hk-3hozm (gate-blocking) · hk-nddg1 (bootreconcile main-only-provenance crew double-dispatch — judged **LATENT/pre-existing at merge-base, non-gating for THIS branch**).

**P2 (5):** hk-cjqyn (SSH-drop false-green), hk-9ngiv (lost ErrProtocolMismatch), hk-hi53s (unbounded stdout buffer), hk-13ff4 (scrub secret-tail leak), hk-bzol4 (keeper self-hint sleep-gate bypass).

**P3 known-issue (6+2):** hk-nqkoz, hk-n8yha, hk-3edb1, hk-btl1n, hk-o66xy, hk-3dn16 + test-hygiene hk-vbkv1, hk-0oebl. Known corpus-lint hk-uhxwd (pre-existing, reproduced).

## Remediation status at close (HEAD a0591ba3)

The captain's crews drained the fix queue. Ledger-confirmed **CLOSED** on HEAD: all 3 P1 (`3a391dc9` fixes l5saf/3hozm/nddg1 with discriminating regression tests + adversarial-review APPROVE), all 5 P2 (hk-cjqyn/9ngiv/hi53s/13ff4/bzol4), and 8 P3 (nqkoz/n8yha/3edb1/btl1n/o66xy/3dn16/vbkv1/0oebl). **Still OPEN:** hk-uhxwd (P3 corpus-lint, passive known-issue) + the 3 LT harness findings hk-es4f7 / hk-xy9ym / hk-oga33 (P2 — not daemon-workflow defects; LT-provisioning/fixture/claude-launch).

The drain trigger for a full re-pin baseline is therefore MET → this pass is closed and superseded by the **a0591ba3 full baseline**, which carries the full XT leg and is the authoritative release verdict.
