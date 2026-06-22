# Crew-Arrangement Review — are we making too many crews for too few things?

**Date:** 2026-06-22 · **Trigger:** operator token-conservation directive ("do more with less"). **Status:** findings delivered, decisions pending operator.
**Method:** Sonnet sub-agent reading captain SKILL/STARTUP, crew-launch SKILL, orchestrator-rules, captain-lanes.md, crew registry JSONs, and the token-usage-audit README.

---

## Root cause: the "fill every lane" mandate forces over-provisioning

captain SKILL §0 (verbatim): *"Establish + verify a crew per KNOWN ready lane. No lane left idle while ready work exists."* / *"Fill every non-conflicting free slot. Keep the fleet moving; do NOT park it 'in case.'"*

It's a **fill-every-slot** rule (holding back is a forbidden failure). Lane model = **1 lane = 1 epic = 1 crew**, staffed autonomously every boot. That structurally maximizes the number of always-on Opus orchestrator sessions — which the token audit identifies as the dominant cost (~96% of spend is 24/7 long-lived session cache-read, NOT the daemon runs).

## Current crews vs. real work (mismatch)

| crew | epic | age | note |
|---|---|---|---|
| paul | daemon reliability L1 | ~18h | legitimate active judgment work |
| stilgar | fleet-state model L4 | ~18h | P2 slice blocked behind design keystone; P1 may be thin |
| leto | flywheel wiring L5 | ~18h | research-heavy build/test |
| logmine | logmine harvest | ~23h | ad-hoc; possibly a one-shot bead, not epic-scope |
| admiral | **(empty epic)** | restarted | oversight-only; pure standing token cost, zero code output |

Plus stale mission files for chani/gurney/irulan/jamis (not live). captain-lanes.md still claims "0 crews (lean park)" — tier-2 state is out of sync with the 5-crew reality.

## What a standing crew adds over daemon-direct dispatch
Only three things: (1) epic-level judgment (which bead next, failure triage, re-dispatch), (2) progress reporting, (3) comms reactivity for re-task. For a **flat clean lane-drain** (ready beads, no inter-dependencies, no expected failures), (1) and (3) are near-zero value (the daemon already dispatches `br ready` in order) and (2) is pure cost. A crew earns its keep only on **keystone-gated sequences, failure-classification, or design/investigation** lanes.

## Recommendations (each tied to token impact)

| # | Change | Savings mechanism | Effort |
|---|---|---|---|
| **A** | Cap standing crews at **2–3**, gated on epic type | Kills always-on cache-read for idle/one-shot/oversight sessions | ops |
| **B** | Default crew model to **Sonnet**; Opus only for judgment-heavy lanes (paul; stilgar while design-active) | Cache-read at Sonnet rate vs Opus — cheapest immediate win | 5 lines/mission |
| **C** | Crew lifetime = epic lifetime — stop any crew idling >30 min with empty epic | Kills zombie crews | skill rule |
| **D** | Enable **sleep-wake** when unattended (mechanism built, `hk-rl4b`; "park when queue depth=0 >15 min") | Eliminates idle-session spend entirely | 1 operator decision + policy |
| **E** | Replace *"one crew per lane"* with *"one crew per JUDGMENT-requiring lane"*; flat drains → captain `queue submit`, no standing session | Structural cap on fleet size (collapses ~14 potential lanes to ~3–4) | captain SKILL change |

**Cheapest first move:** model downgrade (B). **Highest-leverage architectural move:** sleep-wake (D). Both target the same lever the token audit names: *the always-on Opus orchestrators' cache-read burn, not the daemon runs.*
