# Harmonik — Major-Initiatives Registry (2026-07-09)

> Built by the admiral from a 7-agent read-only fan-out over all 49 `plans/` dirs + the admiral/captain
> tracking docs, status cross-checked against `br`, `git tag`, `git log`, `specs/`. This is the durable
> "what have we ever planned + where does it stand" map — track forward from here; supersedes scattered plans.
> STATUS: SHIPPED · PARTIAL (some landed, remainder open) · ACTIVE (in-flight) · PARKED (planned, no blocker,
> 0 ready beads now) · GATED (named/dated gate) · ABANDONED · UNKNOWN. Raw per-slice results: `RAW-agent-results.md`.

## The 8 themes (initiatives cluster into these)

1. **Quality-system + enforcement** — build a real daemon test harness (twin → substrate → chaos gen) AND flip every gate fail-closed. **Operator's declared SINGLE FOCUS.**
2. **Model-provider gateway / Pi** — make non-Claude models first-class + switchable, to survive the Claude token crunch.
3. **Remote / distributed fleet** — scale throughput onto gb-mbp; make remote testable + concurrent.
4. **Wake-economy / token-optimization** — cut the fleet's own Claude burn (watch tier, sleep-wake, codex/scavenger offload). Standing #1 priority.
5. **Daemon reliability** — kill false-positives, strands, wedges, races that corrupt the orchestration signal.
6. **Keeper / session continuity** — context-fill watcher + intent-preserving /clear→resume.
7. **Observability / eval / operator-facing** — eval-metrics, operator dashboard, agent-manifest/assessor, world-models research.
8. **Deterministic safety nets** — stall-sentinel, pre-deploy E2E gate, break-testing corpus (cross-cuts #1 and #5).

## Registry — grouped by theme

### Theme 1 — Quality-system + enforcement  (SINGLE FOCUS; the live front line)
| Initiative | codename / epic | Status |
|---|---|---|
| Quality-system (build test/validation system before deploys) | quality-system / hk-hcrvb (core-loop-proof), hk-zk0v2 (daemon-testbed) | **ACTIVE** — Phase-1 building on integration/core-loop-proof; matrix runner + conformance-blocks-gate landed; T9 full-matrix-green (hk-jjt6w) is the live remaining cell; live matrix blocked (only claude greens; pi/codex harness gaps) |
| Quality-enforcement (flip gates fail-closed) | quality-enforcement / hk-clska, WS-A hk-q6axs | **ACTIVE/PARTIAL** — WS-A bleeding-stoppers merged (real reviewer run, APPROVE-only commit hook, hook-drift CI, A5 continue-on-error dropped + check REQUIRED); WS B/C/D open |
| ├ scripted-twin (Phase 2) | scripted-twin / hk-ynyd3, ST5 hk-psrnc | ACTIVE — engine landed; ST5/ST6 in flight |
| ├ comms-test-harness | comms-test-harness / hk-7m7o2 | ACTIVE |
| ├ keeper-test-harden | keeper-test-harden / hk-193bo, hk-pp1in | ACTIVE |
| ├ test-daemon-harness (isolated scratch-clone) ⚠️no-plans-dir | test-daemon-harness / hk-zk0v2 (move ①) | ELEVATED→BUILD — operator wants it KEPT + reusable |
| assessor gate agent | agent-manifest / assessor | PARTIAL — designed + manifest approved, NOT launcher-wired |

### Theme 2 — Model-provider gateway / Pi
| Initiative | codename / epic | Status |
|---|---|---|
| Pi dual-provider / per-bead switch ⚠️no-plans-dir | pi-provider-switch / hk-fdbhf | **PARTIAL/ACTIVE** — core goal PROVEN in-daemon (canary 019f4365, commit e3c66024); epic OPEN for per-bead-switch + 4-corpus-harness remainder. **Leading re-stand candidate (direction-log, expires 07-11).** |
| Pi OpenRouter harness (pilot) | pilot / hk-94c3t | PARKED (proven e2e; exits-without-commit on real Go → pending sandbox) |
| Pi srt sandbox | pi-sandbox / hk-f39ny, hk-p7smp | ACTIVE — argv-wrap→config→acceptance chain underway (leto) |
| Model-routing / provider-spread MR1–MR3 | model-selection (shannon research) | ON-DECK/BUILD — operator token-crunch program; MR3 = dispatch-time model selection + auto-cut-Claude-when-low |
| De-hardcode-messages ⚠️tracking-only | — | PARKED (bundled w/ Pi gate, now expired) |

### Theme 3 — Remote / distributed fleet
| Initiative | codename / epic | Status |
|---|---|---|
| Remote-worker validation (gb-mbp SSH) | hk-gx0dl | **SHIPPED** — CLOSED; 98 jobs, proven conc-3 |
| Concurrency-split (local cap-4 + remote ceiling) | concurrency-split / hk-hs7ex, hk-5qp7z | **SHIPPED** — worktree-create race fixed + proven (ramp to 6, 7/7 clean) |
| Remote test-hardening pyramid L0–L5 ⚠️no-plans-dir | remote-test-pyramid / hk-6l941 | SHIPPED core (epic CLOSED, L0 landed); L2–L5 remainder no plans dir |
| Hot-reconfigure + multi-remote dispatch | hk-f10xl (landed), hk-xjbvi (open) | PARTIAL — routing landed; live on/off toggle OPEN; multi-remote still V1 |
| Remote node telemetry + autoscale | worker-report/breach | PARTIAL — P1+P2 code landed but off-by-default, never live-run; P3 AIMD deferred |

### Theme 4 — Wake-economy / token-optimization
| Initiative | codename / epic | Status |
|---|---|---|
| Captain wake-economy (watch-officer tier) | wake-economy / hk-var9b | **SHIPPED MVP** — 7 beads CLOSED, cutover LIVE; soak follow-ons (hk-we-soak1/2, hk-we10, hk-8yh32) PARKED/at-risk |
| Fleet sleep-wake | sleep-wake / hk-xjr1n | SHIPPED — symmetric sleep/wake merged |
| Boot-spike cost cut | boot-spike | SHIPPED — stagger rule + hk-hzj |
| Token-usage audit tooling (`harmonik usage`) | tokenaudit / TA1-4 | SHIPPED Phase-0 |
| Codex offload (vetting + on-remote) ⚠️no-plans-dir | codex / hk-0639 | PARKED/ON-DECK — local soak proven 5/5; codex-on-remote has NO bead yet |
| Scavenger (orphan-backlog drainer) ⚠️no-plans-dir | scavenger / hk-0kr4j | PARKED |

### Theme 5 — Daemon reliability
| Initiative | codename / epic | Status |
|---|---|---|
| Daemon-reliability lane (strands/wedges/false-positives) | hk-sfvc + many | ON-DECK/ongoing — highest fleet-signal leverage; recurring flagless-REQUEST_CHANGES wedge (hk-thbbv/hfmg6) still open |
| DOT review-graph (Phase-3) | dot | SHIPPED, actively bug-fixed on live path (hk-vv10r/ybuts/9w79a) |
| Handler pause-and-resume | handler-pause | SHIPPED |
| Workflow modes / bridge / extqueue / run-subcommand | 001-006 | SHIPPED (foundational, all closed) |

### Theme 6 — Keeper / session continuity
| Initiative | codename / epic | Status |
|---|---|---|
| Keeper reliability (restart-now, tmux-target, await-ack, hold/release) | keeper-redesign / hk-5266t, hk-uldg | SHIPPED lane (residual bugs → keeper-test-harden) |
| Keeper auto-attach (every crew auto-gets a keeper) | keeper-autoattach | **PARKED/PARTIAL — GAP: crew-start STILL has no auto-keeper watcher** |
| Tmux session organization | hk-0v9e | SHIPPED (epic CLOSED) |

### Theme 7 — Observability / eval / operator-facing
| Initiative | codename / epic | Status |
|---|---|---|
| Operator dashboard + mailbox | dashboard / hk-2exz9, hk-pltjs | SHIPPED — dashboard CLI + mailbox + staleness gate |
| Fleet-state "publish don't narrate" data surface | fleet-state | PARKED (partial) — snapshot landed, publish-seam never built |
| Eval-metrics / eval-program | eval-program / hk-9jdid | PARKED — WS1 landed+closed; WS2/WS3 unconfirmed; crew torn down |
| Cross-model eval matrix | eval-harness | PARTIAL — plumbing landed; run-matrix design-only |
| Agent-manifest (role identity, soul.md) | agent-manifest / hk-ncg9m | SHIPPED — rollout closed; assessor sub-item PARTIAL |
| Agent world-models (research) | — | ABANDONED-as-build — recommendation = don't build; folded into scenario-harness |
| Admiral framework (oversight role) | admiral-framework | SHIPPED (live) |

### Theme 8 — Deterministic safety nets
| Initiative | codename / epic | Status |
|---|---|---|
| Stall-sentinel (no-LLM wedge detector) | stall-sentinel / hk-r9n2s | PARTIAL/ON-DECK — detection core landed; full tiered escalation not staffed |
| Pre-deploy E2E test gate | (standing rule) | SHIPPED + ENFORCED — caught the v0.5.0 GATE-0 ship-blocker |
| Component liveness alerting | component-liveness | PARTIAL — ops-monitor liveness landed; no single tiered-escalation impl |
| Release protocol (dogfooding tag→certify) | release-protocol | SHIPPED — culminated in v0.5.0 |
| Plain-English comms by default | clear-communication-pattern | ACTIVE/UNKNOWN — rule exists, no landed enforcing mechanism |

## ⚠️ At-risk — live/important initiatives with NO plans/ dir (only in tracking docs/kerf-works)
- **pi-provider-switch** (hk-fdbhf remainder) — HIGHEST risk: the live re-stand candidate, only `.kerf/works/pi-provider-switch/` + a direction-log entry.
- **test-daemon-harness** (hk-zk0v2) — operator wants it kept+reusable; only a design doc + kerf work.
- **codex offload** — kerf work only; codex-on-remote has no bead.
- **scavenger** (hk-0kr4j), **remote-test-pyramid L2–L5**, **watchdog/supervisor rename**, **de-hardcode-messages** — tracking-doc/kerf-only.

## Focus recommendation (for operator decision)
The fleet is quiesced post-v0.5.0. Three coherent next-focus options, in recommended order:

1. **Resume the SINGLE FOCUS: quality-system + enforcement.** It's ACTIVE, mid-Phase-1, and it's what makes every future deploy safe (already earned its keep catching the v0.5.0 GATE-0 blocker). Highest strategic leverage. Concrete next: close core-loop-proof (hk-hcrvb / T9 hk-jjt6w full-matrix-green), then wire the assessor gate, then Phase-2 twin.
2. **Pi dual-provider / per-bead switch** (Theme 2) — the direction-log's standing live candidate (expires 07-11), directly attacks the Claude token crunch. Core proven; remainder = per-bead switching + the 4-bug-class harness coverage. Pairs naturally with #1 (the 4 bug classes become quality-harness cells).
3. **Daemon-reliability sweep** (Theme 5) — the recurring flagless-REQUEST_CHANGES wedge + strands still corrupt orchestration signal; high day-to-day leverage but less strategic than #1/#2.

**Recommendation: #1 as the spine, with #2 folded in as its Pi/codex matrix cells** — they converge (quality-system needs pi+codex harness cells green; pi-provider-switch needs exactly that coverage). One combined front line rather than two.
