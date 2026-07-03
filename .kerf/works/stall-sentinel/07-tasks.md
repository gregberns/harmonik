# Stall-Sentinel — Task decomposition (beads)

> The brief §7 build order as concrete beads. All carry label `codename:stall-sentinel`
> (kerf `bead_filter: label=codename:stall-sentinel`). None are assigned — the daemon/captain staffs
> them later. The signal library gates the detectors; acceptance depends on the detectors.

| Bead | Title | Pri | Depends on |
|---|---|---|---|
| `hk-mxxsl` | Signal library (events.jsonl + run registry → per-run last-event-age/phase) | P1 | — |
| `hk-l087e` | Layer A detectors (heartbeat-gap / review-stall / run-age → emit `stall_detected`) | P1 | `hk-mxxsl` |
| `hk-r9n2s` | Layer B detector (lane no-forward-progress + expectation-of-progress guard + NEGATIVE idle test) | P1 | `hk-mxxsl` |
| `hk-hm09z` | Config block (X/Y/Z + `*_stall` thresholds, fail-loud) | P1 | — |
| `hk-u9thq` | Tiered escalation (comms send per tier + keeper send-keys pane path) | P2 | `hk-l087e`, `hk-r9n2s`, `hk-hm09z` |
| `hk-vnsme` | Acceptance suite (replay the 3 stall classes + negative idle test) | P2 | `hk-l087e`, `hk-r9n2s` |
| `hk-sljlg` | Watch/ops-monitor consumption of `stall_detected` (display only) | P3 | `hk-l087e`, `hk-r9n2s` |

## Dependency graph

```
hk-mxxsl (signal library, GATE) ──┬──▶ hk-l087e (Layer A) ─┐
                                  └──▶ hk-r9n2s (Layer B) ─┤
hk-hm09z (config, parallel) ───────────────────────────────┼──▶ hk-u9thq (escalation)
                                                            ├──▶ hk-vnsme (acceptance)
                                                            └──▶ hk-sljlg (watch/ops consume)
```

Ready to dispatch now: `hk-mxxsl` (signal library) and `hk-hm09z` (config). The rest unblock as their
dependencies close.

## Discipline notes

- No bead was set to `in_progress` and none was assigned — the daemon owns terminal transitions; the
  captain staffs.
- Beads are standalone tasks (no epic parent) to avoid the epic-dep-blocks-dispatch footgun. The
  `codename:stall-sentinel` label is the grouping mechanism; deps are task→task only.
- Priorities encode dependency order: signal library + detectors + config P1; escalation + acceptance
  P2; watch/ops-monitor display consumption P3.

## Open decision for the design pass (from brief §4)

Fully daemon-integrated deterministic goroutine vs. a small standalone Go sidecar with its own timer
reading the socket/event log. **Lean daemon-integrated** — flagged for the design pass, not a blocker
to these tasks (all 7 hold under either home).
