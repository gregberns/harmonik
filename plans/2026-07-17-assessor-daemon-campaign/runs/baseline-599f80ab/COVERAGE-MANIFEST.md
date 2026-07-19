# COVERAGE MANIFEST — baseline-599f80ab (authoritative)

Pin 599f80ab (release SHA 9152ea5c = pin + test-only, admiral diff-verified).
Legend: RUN=exercised w/ hard assertion · SKIP=logged reason · PEND=in flight.
Codex rows: SKIPPED — operator "codex minimal".

## Suites S1–S7
| Suite | Leg | Status | Evidence |
|---|---|---|---|
| S1 Lifecycle (startup/teardown/sleep/supervisor-revive/orphan-sweep) | XT/live | PEND | |
| S2 Handlers (claude,pi × local/remote; H13/H11/HC-004/A9) | LT+XT | PEND | |
| S3 Workflow matrix (DOT/single/review-loop × harness × local/remote) | LT | PEND | |
| S4 Comms (Hamlet 1.1; ordered, no drop/dup) | XT/live | PEND | |
| S5 Keeper (~30k WARN→ACT→handoff→/clear→resume; C4) | XT/live | PEND | |
| S6 Adversarial log watcher (always-on) | S6 | RUNNING | S6-watcher live pid tracked; LOG-WATCH-FINDINGS.md |
| S7 Fault injection (H2/H3/H4/H5/H6/H7/H8; revert-confirm-red) | XT | PEND | |

## Coverage-gap cells G1–G14
| Cell | Status | Evidence |
|---|---|---|
| G1 operator confirm/veto path | PEND | |
| G2 daemon crash-recovery re-adopt | PEND | |
| G3 DOT merge-vs-strand | PEND | |
| G4 review-loop feedback on remote | PEND | |
| G5 promote push+PR mode | PEND | |
| G6 supervisor revive race + false-reap | PEND | |
| G7 comms at-least-once redelivery dedupe | PEND | |
| G8 SSH drop mid-run | PEND | |
| G9 keeper hold/release | PEND | |
| G10 config honored not dropped | PEND | |
| G11 oversized valid event line | PEND | |
| G12 secret-field startup guard | PEND | |
| G13 version negotiation | PEND | |
| G14 positive worktree GC | PEND | |

## Delta-focused (a0591ba3..599f80ab risk surface)
| Area | Leg | Status |
|---|---|---|
| Green-tree: keeper/daemon/codexdriver/codexwire/workspace/sessioncapture/core go tests | REGRESSION | PEND |
| CR cold review of a0591ba3..599f80ab product diff | CR | PEND |
| hk-xrn8r subscribe-order flake disposition (product/test/pre-existing) | ROOT-CAUSE | PEND |

## Known un-isolable / SKIPPED
- Codex handler cells: SKIPPED — operator "codex minimal".
- Remote (runner!=nil) cells needing real remote substrate: SKIP-with-reason unless a throwaway remote is stood up.
- `harmonik usage`: NOT invoked (hardcodes real repo+$USER, usage.go:424).
