# logmine — iter-12 findings (2026-06-20)

**Window:** line-anchored from iter-11 high-water `019edc1b-8b5e-77d3-b358-4bf081eaf882` →
290 events, lines 38802–39091, **2026-06-18T19:00Z → 2026-06-20T14:46Z** (~43.5h).
Method: frozen `/tmp/logmine-window.jsonl`, 5 read-only slices (failures, keeper/ctx-watchdog,
reconciliation+daemon-lifecycle, comms+incident, review-loop+churn+transcripts).

## Headline

**HEALTH GREEN.** A quiet/mostly-parked window bracketed by two operator-side incidents — **both
contained, production unharmed, ~zero downtime** — plus one genuine remote-substrate bug already
owned by chani. **Zero unreviewed merges. Zero lost work. No new beads warranted** — every finding
maps to an already-open bead (the mature-recurring-pipeline outcome). Net Wave-2 action: 3
enrichments + digest.

### The two "incidents" (untangled — the captain's 14:10Z boot conflated them)
- **(A) Keeper smoke/soak fork-bomb, 2026-06-20 ~05:45–06:18Z** — the hk-7myt class recurred:
  `go test TestSIGINTWedgedDaemonExit` + `/tmp/hk-smoke-keeper` self-replicated `*-flywheel`/`-default`
  tmux sessions at ~13/s (no cleanup on the `no_tmux_target`/wedge path), 1500+ leaked, load→42.
  Contained (binaries removed, kill-storm, sessions killed); load 42→1.9. **Production daemon + crew
  chani + keeper UNHARMED.** Fully characterized in **hk-7myt** (P1, filed 2026-06-20, root cause +
  fix scope complete). NOT a billing/OAuth burn.
- **(B) Host internet/auth outage, ~06:18Z→14:09Z (~8h overnight, operator-away)** — box-A
  connectivity, NOT a credential/subscription failure (worker gb-mbp correctly had `ANTHROPIC_API_KEY`
  unset → OAuth path). Resolved by operator; this is what "post-auth-recovery" refers to. Comms
  presence aged out → captain cleaned a logmine/flywheel ghost record on the 14:10Z boot.
- **(C) 2 dead-pane flywheel sessions** (dead since 23:17 Jun-19) = orphan residue, watchdog correctly
  skipped; cleaned by 14:10Z.

## Findings register

| F | Pri | Verdict | Description | Anchor | Lane → action |
|---|-----|---------|-------------|--------|---------------|
| F-recon-N5 | P3 | **RECURRING (3rd iter)** | Hourly reconciliation emits no `reconciliation_completed`: 43 `scheduled-hourly` started / 0 completed; only the 3 `startup` recs complete (instant no-ops, examined:0). Code-path asymmetry, NOT a leak/wedge. | hourly starts `019edc30-4dfe`…`019ee55e-8ac1`; startup pairs `019edc31-e31c`,`019ee339-30de`,`019ee564-3e96` | crew:paul → **ENRICH hk-v144** |
| F11-A | P1 | **RECURRING** | DOT/review reviewer dispatched-but-no-verdict → caught only at hard ceiling: hk-gu3v `reviewer_budget_exceeded` (632400ms) → `run_failed "verdict absent"`; commit stranded on dangling `run/019edc40` (`1b9751aa`), required manual re-commit salvage (`1ccc2b90`). | ev `019edc7b-29a3`, run `019edc40-632b` | crew:paul → **ENRICH hk-sj6a** |
| F12-WD1 | P1 | **NEW** | ctx-watchdog restart trigger (300k tokens) + 30m poll = zero headroom: chani burned **147k→304k OVERSIZED inside one blind 29-min interval**; watchdog fired one tick late. Live witness of the blind-keeper-backstop concern. | comms ticks 14:13:28Z→14:42:13Z; `.harmonik/cognition/ctx-watchdog-prompt.txt:1` (300000 + /loop 30m) | crew:logmine (script) → **ENRICH hk-34ac** + surface to captain (operator-owned band, no auto-edit) |
| F12-WD2 | P3 | NEW | Watchdog band (300k) vs keeper bands (215k/240k, `ctx-watchdog-launch.sh:6`) inconsistent; for crews the watchdog @300k is the ONLY guard and loosest. | `scripts/ctx-watchdog-launch.sh:6` | crew:logmine → digest note |
| F-S6.6 | P3 | RECURRING | `stale_intents_observed:21` identical across all 3 orphan-sweeps spanning 43h, `intents_gc_d:0` — static phantom counter. | sweeps `019edc31`,`019ee339`,`019ee564` | crew:paul → digest note |
| F-12.1 | P1 | NEW→already filed | Keeper fork-bomb (incident A). | hk-7myt | crew:paul — **no action** (hk-7myt comprehensive) |
| F-12.2 | P2 | RESIDUAL→tracked | 22 leaked idle `-default` tmux sessions left uncleaned pending operator "safe-to-kill" decision (one MIGHT be daemon spawn target — cf. NEVER-kill-`*-default`). | captain comms 06:18:27Z | tracked in hk-7myt RESIDUAL — operator decision |
| F12-A / F-12.4 | P1 | chani-owned | remote-substrate `agent_ready_timeout` @92s on implement node, **twice on a confirmed-stable link** (gap-#7 reverse-tunnel: worker engages but `agent_ready` never returns to box-A daemon). GENUINE bug, not connectivity. | runs `019ee56d-420d`,`019ee572-db4d`; ev `019ee56e-b7e6`,`019ee574-5745`; HC-056 90s | crew:chani — **no action** (live-owned, hk-rs-validate-remote-898a) |
| F-12.3 | P3 | NEW | Handoff-boundary relay seam: captain told chani "next captain will action your daemon restart" 2 min AFTER chani had already self-actioned it (watchdog auto-revive). Stale cross-session instruction, mild double-work risk. | comms 14:20:49Z vs 14:22:37Z | crew:logmine → digest note |
| F-S6.2 | P2 | RECURRING benign | run-context commit-pair churn (CHB-023 + hk-4je no-code bookkeeping pairs) continues. | pre-window git tail | crew:paul → digest note |

## FIXED-CONFIRMs (the recurring-pipeline payoff)

- **F66 → hk-gu3v** (ops-monitor crew-stale:captain false positive): **FIXED-CONFIRM.** `1ccc2b90`
  (agent_message-recency fallback) is HEAD, `scripts/ops-monitor-check.sh` mtime = fix commit. The two
  in-window digests (ctx-watchdog 14:21:15Z, chani 14:36:16Z) are **TRUE positives** (last messages
  7m45s / 214s old, both >150s). **Do NOT reopen.**
- **hk-2vpj** (review-gate bypass in multi-reviewer fan-out): **FIXED-CONFIRM, holding.** `4b1c7f91`
  on HEAD; its OWN merge this window (run `019edc05-3485`) was reviewed APPROVE via the repaired
  fan-out path — no short-circuit.
- **Daemon-restart hygiene:** 3 restarts in window (redeploy→debb1e45, post-auth-recovery bounce on
  same binary→pid 22178, redeploy→1ccc2b90), **all clean**: 0 live-work reaped, captain/crew sessions
  correctly skipped, zero config drift (`max_concurrent:4, workflow_mode:dot` stable).

## Metrics
- **False-fail rate: 1/3 run_failed (33%).** The 1 false-fail (hk-gu3v) auto-salvaged to `1ccc2b90`;
  the 2 genuine fails are chani's remote bug. The 2 `implementer_budget_exceeded` (hk-rty1) self-subsumed
  (`noChange-subsumed: bead found in main`) — daemon reconcile-on-ceiling working.
- **Review integrity: 0 unreviewed merges** (3 run_completed all accounted: 1 reviewed-APPROVE, 2
  noChange-subsumed).
- **Transcript errors:** only benign self-corrected Edit-before-Read in a keeper handoff-writer
  transcript (fe5efd0e); no CWD-drift / path-miss / same-Refs re-dispatch.

## Wave-2 actions taken
3 enrichments (hk-v144, hk-sj6a, hk-34ac), 1 cross-link (hk-7myt). **0 new beads.** **0 logmine-q
dispatch** (watchdog band = operator-owned; fork-bomb fix = crew:paul/test lane; remote = chani).
Digest delivered to captain `--topic findings`.

> **Window:** line-anchored from iter-11 high-water `019edc1b` → 290 events, lines 38802–39091,
> high-water: 019ee57f-aef4-79b0-bebd-e7caa9101ffc  (2026-06-20T14:46:50Z agent_presence — last line of the frozen iter-12 window; next run resolves THIS event_id to its line and slices forward, per F41a)
