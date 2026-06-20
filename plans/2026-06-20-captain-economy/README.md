# Captain Economy — Plan (2026-06-20)

**Operator brief:** The captain does too much busy-work. Its startup procedure alone pushes context past 100k (target ceiling 200k), restart has issues, and conflicting instructions make it unreliable. Investigate, find the inefficiencies and contradictions, make the captain leaner and more reliable. Converge → beads → fix in worktrees → merge.

## Status: ✅ COMPLETE — all 4 beads merged to main

Epic **hk-unjy** CLOSED. CE1/CE4/CE5/CE6 all landed + pushed. Reviews R1–R7 in reviews/.
Follow-up queue DRAINED:
- **hk-orni** (P2) — DOT short-circuit detection in the review-gate. **LANDED 96bcb8ce** (R8 APPROVE). The CE4 limitation is now closed.
- **hk-umhp** — captain quality-check false all-clear. **CLOSED** (resolved by CE1+CE4; was a duplicate).
- **hk-z365** (P3) — comms `--wake` hash symlink-parity nit (filepath.Abs vs EvalSymlinks; affects crew+captain). FILED, left for its own reviewed change (pre-existing, shared comms hashing).
- doc-sweep: dangling `token-burn-analysis.md` ref fixed (11383c85); the `.harmonik/config.yaml keeper:` block was verified REAL (R3 nit was wrong), skills left as-is.

| Bead | Landed | Reviewer |
|------|--------|----------|
| CE1 hk-039z | 8ccae3e6 + e418223b (asset sync) | R3 APPROVE-WITH-NITS |
| CE4 hk-ayvx | a4a98f94 + 3ef6fb6f (review-gate fix) | R6 REQUEST_CHANGES → R7 APPROVE-WITH-LIMITATION |
| CE5 hk-y7v8 | 09c112c5 | R5 APPROVE-WITH-NITS |
| CE6 hk-9mpk | d47ebab4 + local deploy | R4 APPROVE-WITH-NITS |

---
### (historical) earlier status

Epic **hk-unjy**. Investigation + review COMPLETE — see CONVERGENCE.md. Beads:
- CE1 hk-039z — captain skill-lean + de-conflict + real boot-digest. **✅ MERGED to main** (8ccae3e6 + asset re-sync e418223b; reviewer APPROVE-WITH-NITS R3; guard test green; pushed). Boot cost ~81k→~55-60k.
- CE4 hk-ayvx — Sonnet ops-monitor (code). HELD pending stabilization.
- CE5 hk-y7v8 — comms `--wake` pane fix (code). HELD.
- CE6 hk-9mpk — keeper-restart-verified.sh deploy-drift (local deploy). HELD.

**CE1 delivered:** canonical keeper-band flags (no inert `--warn-pct`), de-duplicated STARTUP⇄SKILL, stopped full-reading AGENT_INDEX/STATUS/TASKS at boot, mandatory in-git boot-digest scripts (scripts/captain-boot-digest.sh + crew-boot-digest.sh — the falsely-closed hk-n3w1 was out-of-git only), dropped the 600s heartbeat subscribe, fixed the always-null review-gate check to a `reviewer_verdict`/`run_id` join, consolidated the `br close` exception, `$HARMONIK_AGENT` instead of hardcoded `--from captain`, §A→captain-lanes.md single source, restart-now `/quit`-footgun verify step, keeper positional-arg→flag fixes.

## Prior work (consolidated)

This is NOT greenfield. There is a converged design and substantial landed work. This plan FINISHES the captain-specific slice and attacks what the prior initiatives left open.

### Epics
- **hk-itoc — leanfleet** (`codename:leanfleet`, P1, OPEN). The central efficiency epic. Operator priority order: (1) cut restart/startup cost, (2) cut captain+crew noise, (3) offload admin/checks to a cheaper model, (4) clear 3-tier handoffs (short tasks / mid epics / long goals; mid+long are stable, don't re-derive each restart). Converged design via 5-agent fan-out + 2 adversarial critics. Authoritative record: epic comment + `docs/plans/leanfleet/design.md`.
- **hk-bsdr — tokenaudit** (`codename:tokenaudit`, P1, OPEN). Token-burn audit. Report: `docs/retro/2026-06-17/token-burn-analysis.md`. FINDING: 95.9% of fleet spend = cache-read = context-size × turns × sessions. The bill is the long-lived OPUS captain+crew sessions.
- **hk-rl4b — fleet sleep/wake** (P1, paul). MECHANISM COMPLETE on main (drain-oracle, daemon quiesce/wake, session park/resume, keeper sleep-gate, CLI). POLICY layer (captain protocol) DEFERRED for operator knobs. Research: `docs/ideas/fleet-sleep-wake-research.md`.

### Landed (don't redo)
- TA1 restart-earlier (hk-8hr1): keeper band warn=200K/act=215K/force=240K + young-session + clean-handoff guards. On main (97e1787c, fa98f7c1).
- TA2 boot-digest (hk-n3w1): `captain-boot-digest.sh` / `crew-boot-digest.sh` — bundle deterministic boot discovery into ONE read. CLOSED.
- LF-keeper-noise (hk-sol6): 30s inject back-off + no_gauge re-emit 120→300s + dip-rise cooldown. CLOSED (d83bc987).
- LF-keeper-selfhint (hk-lsk5): one-time 190K self-restart hint w/ unsafe-conditions text. CLOSED (d83bc987).
- Sleep/wake mechanism (hk-rl4b M0–M4). On main.

### Still OPEN from prior design (candidates to fold in)
- hk-9j3z — LF-daemon-model: per-crew `--model` injection from mission `model:` field (stilgar lane). P2.
- hk-ee81 — LF-keeper-idlerestart: restart idle large-ctx crews to small. P3.
- hk-umhp — captain quality-check false all-clear (`run_started` has no `workflow_mode`; review-gate bypass undetected). P2.
- hk-gfpd — KEEPER F47: captain never holds a readable gauge. P2.

## What THIS plan adds (the unaddressed captain slice)

The prior work attacked keeper bands + sleep/wake + crew model-tiering. It did NOT close the operator's three live complaints:
1. **Boot context blowout** — `STARTUP.md` (567 lines) + `SKILL.md` (772 lines) = 1339 lines read every boot, plus inline ground-truthing. Did boot-digest actually shrink the captain's boot reads, or is the captain still re-deriving?
2. **Conflicting instructions** — STARTUP.md vs SKILL.md vs captain memory vs orchestrator-rules.md vs CLAUDE.md. Contradictions = unreliable behavior.
3. **Restart unreliability** — captain quits-and-stays-dead without `--session-id`; keeper-restart continuity gaps; what else breaks.

## Investigation workstreams (fan-out)

| ID | Angle | Output file |
|----|-------|-------------|
| I1 | Boot context cost — what the captain reads/derives at boot, line-by-line, est. tokens, what's offloadable | findings/I1-boot-cost.md |
| I2 | Conflicting instructions — diff STARTUP/SKILL/memory/orchestrator-rules/CLAUDE for contradictions | findings/I2-conflicts.md |
| I3 | Restart reliability — every documented + code-path failure mode on captain restart | findings/I3-restart.md |
| I4 | Busy-work / admin-offload — inline checks/polls/heartbeats the captain runs that a script or cheap model could do | findings/I4-busywork.md |
| I5 | Prior-work gap audit (CRITIC) — what leanfleet/tokenaudit promised vs landed; is anything claimed-done actually not? | findings/I5-prior-gap.md |

## Reviews
Each finding gets an adversarial review (reviews/) before consolidation.

## Convergence → beads → fix
After review+consolidate: distinct issues → beads under `codename:leanfleet` (or a new sub-epic) → dispatch fixes in worktrees → merge to main.
