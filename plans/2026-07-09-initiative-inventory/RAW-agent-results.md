# Initiative Inventory — RAW agent results (2026-07-09, admiral)

> Durable capture of the read-only research fan-out (7 agents over plans/ + admiral files) enumerating
> every major initiative + status. Synthesis (dedup → themes → focus rec) happens from this file.
> STATUS vocab: SHIPPED / PARTIAL / ACTIVE / PARKED / GATED / ABANDONED / UNKNOWN.

## Slice: foundational plans (001–009, captain-economy, clear-comms) — DONE

| Initiative | plan/codename | What | Beads | STATUS |
|---|---|---|---|---|
| Claude-hook bridge into daemon | 001_bridge_integration | tmux real-claude + Stop-hook completion | bridge-followup epic (closed) | SHIPPED (smokes green, specs landed) |
| claude-hook-bridge spec corpus | 002_claude_hook_bridge | twin Stop-hook, settings materialization, hook-relay | CHB-001..027 | SHIPPED (spec normative, 0 open) |
| External-orchestrator queue control | 003_extqueue | queue submit/append/status over socket | epic hk-lj0pb | SHIPPED (queue-model.md v0.1.1) |
| Phase-3 DOT review-graph | 004_phase_3_dot | DOT review-gate + sub-workflow dispatch | hk-vv10r/ybuts/9w79a = live bugs | SHIPPED, actively bug-fixed on live path |
| Workflow modes (single vs review-loop) | 005_workflow_modes | mode types/events, daemon driver | hk-7uasg, hk-02sp0 | SHIPPED |
| `harmonik run <bead>` one-shot | 006_harmonik_run_subcommand | single-bead queue + exit | hk-icecw, hk-ajchp | SHIPPED |
| Handler pause-and-resume | 007_handler_pause_and_resume | pause failing handler, freeze beads | hk-107gz +15 | SHIPPED (handler-pause.md normative) |
| Phase-2 dogfood scaleout | 008_phase2_dogfood_scaleout | dispatch-via-daemon by default | hk-rp48p/sc3o4/lgtq2 | SHIPPED (is now the daily loop) |
| Self-teaching CLI help | 009_cli_help_redesign | --help on every subcommand | hk-vudz0 | SHIPPED (core) |
| Captain economy (leaner boot) | 2026-06-20-captain-economy | cut boot cost, fix review-gate | epic hk-unjy | SHIPPED (epic CLOSED) |
| Plain-English comms by default | 2026-06-20-clear-communication-pattern | translate codes, ask real Qs | (behavioral) | ACTIVE/UNKNOWN — no landed mechanism; open behavioral initiative |

## Slice: 06-20 keeper/remote/doc cluster — DONE

| Initiative | plan/codename | What | STATUS |
|---|---|---|---|
| Doc & instruction audit | doc-instruction-audit / hk-vk7b | split state/rules/priorities; AGENTS.md→router + orchestrator-rules skill + sync-assets | SHIPPED (epic CLOSED; hk-fozq P3 deferred) |
| Easy-start launchers | easy-start-commands / hk-bcd0/z1rj | native `harmonik start captain\|crew`, keeper auto-arm, retire bash | SHIPPED core (integration paths unit-only, not e2e) |
| Fleet sleep/wake | fleet-sleep-wake / hk-s8qi etc | quiesce idle sessions; daemon auto-park + sleep/wake | SHIPPED Phase-0 (auto-park→wake round-trip not live-confirmed; policy layer declined) |
| Keeper arch critique r1 | keeper-architecture-critique | fan-out critique → restart-now simplification | FED FIXES → SHIPPED (hk-5da7 restartnow.go on main) |
| Keeper critique r2 | keeper-critique-round2 | ops/open-loop risks: supervisor, crew watcher, liveness | FED FIXES → SHIPPED (await-ack, crew keeper watcher, self-heal) |
| Keeper investigation recovery | keeper-investigation-recovery | forensic recover lost fan-out | COMPLETE (recovered hk-5da7, shipped) |
| RC session prefix | remote-control-session-prefix | per-project prefix on RC label | SHIPPED core (4 tail beads open) |
| Remote node telemetry + autoscale | remote-node-telemetry-autoscale | worker resource snapshots + breach detect; autoscale deferred | PARTIAL — P1+P2 code CLOSED but off-by-default, never live-run; P3 AIMD deferred |
| State reassessment & doc-sync | state-reassessment-and-doc-sync | reconcile 135 commits/9 initiatives → STATUS/ROADMAP | COMPLETE (synthesis; flags remote-substrate as live-validation gap) |
| Tmux session organization | tmux-session-organization / hk-0v9e | unified namespace, window-nesting, window-restart, supervise reap | SHIPPED (epic CLOSED, 6 slices merged) |

## Slice: 06-25 → 07-02 — DONE

| Initiative | plan/codename | What | STATUS |
|---|---|---|---|
| Admiral role/framework | 2026-06-25-admiral-framework | fleet-oversight admiral role + registry | SHIPPED (live) |
| State source-of-truth & sync | 2026-06-25-state-sync | where state lives + liveness/mutual-exclusion | PARTIAL — state-map normative (system-state.md); sync mechanism unbuilt |
| Transcript retro tool | 2026-06-25-transcript-retro-tool | reusable transcript-extract + incident retro | PARTIAL — retro ran once; no reusable tool committed |
| Distributed / multi-node fleet | 2026-06-30-distributed-fleet | p2p comms, container sandbox, Pi crew, node-agent | PARTIAL/ACTIVE — remote SSH path + test pyramid hk-6l941 CLOSED; node-agent/multi-remote/p2p DESIGN-ONLY |
| Model-eval / eval program | 2026-07-02-eval-harness | run tasks per-model, grade, feed task→model router | ACTIVE (substantial landed: eval-bead.dot, evaltask, rubric-v1 judge, metrics) |
| Pi in srt sandbox | 2026-07-02-pi-sandbox | daemon runs Pi inside sandbox-runtime, can't write main | SHIPPED (srt wired, WriteToMainDenied gate, Pi proven green in-daemon) |
| Stall/wedge sentinel | 2026-07-02-stall-sentinel | deterministic stall detector + tiered nudge | PARTIAL — detection core landed (config+signals); full tiered escalation unconfirmed |

## Slice: 07-03 / 07-04 — DONE

| Initiative | plan/codename | What | STATUS |
|---|---|---|---|
| Agent role-identity + layered context | 2026-07-03-agent-identity-and-context / agent-manifest | per-role soul.md+operating.md re-pinned on boot | SHIPPED (T3–T12 merged, agents/*/soul.md live, crew brief wired hk-ncg9m) |
| Cross-model eval + session-data | 2026-07-03-eval-program / sessiondata | route problem-set through N (harness,model); normalized per-run record | PARTIAL — collector + per-node attribution merged; cross-model run-matrix design-only |
| Fleet-state "publish don't narrate" | 2026-07-03-fleet-state-and-dashboard-data | structured state store + generated views | PARKED (partial) — system-state snapshot landed; publish-seam never built |
| Operator dashboard + mailbox | 2026-07-03-operator-dashboard | JOIN over live state + planning tier + hitl mailbox | SHIPPED (dashboard CLI + Tier-B + mailbox + staleness gate merged) |
| Agent-manifest rollout retro | 2026-07-04-agent-manifest-rollout-retro | 12h behavior-improvement retro | SHIPPED (found crews unwired → gate fix hk-ncg9m landed) |
| Quality loop — fail-closed gates | 2026-07-04-quality-loop | flip 11 advisory/fail-open gates to blocking | ACTIVE — evolved into SINGLE-FOCUS quality-system; #27 dropped continue-on-error; most gates still converting |

## Slice: 06-22 / 06-23 — DONE

| Initiative | plan/codename | What | STATUS |
|---|---|---|---|
| Boot-spike cost cut | 2026-06-22-boot-spike | stagger captain/crew boots so cache warms once | SHIPPED (5c stagger rule + hk-hzj) |
| Component liveness alerting | 2026-06-22-component-liveness-alerting | tiered IMMEDIATE→escalation→operator alerts | PARTIAL — ops-monitor liveness commits landed; no single tiered-escalation impl |
| Keeper auto-attach | 2026-06-22-keeper-autoattach | spawn-time gate so every crew auto-gets a keeper | PARKED/PARTIAL — only design commit; crew-start STILL has no auto-keeper watcher |
| Keeper coverage RCA | 2026-06-22-keeper-coverage-investigation | RCA of crews-without-keepers crisis | SHIPPED as investigation (crisis mitigated; deploy-lag root cause) |
| Overnight umbrella (workflow-mode/supervision/routing/release) | 2026-06-22-overnight | 4-cluster brief | PARTIAL — C1 dot-default + C3 routing (hk-f10xl) SHIPPED, C4→v0.5.0, C2 supervision partial |
| Dogfooding release model | 2026-06-22-release-protocol | tag→soak→captain-certify, no auto-yank | SHIPPED (release-pipeline.md; culminated in v0.5.0) |
| Token-usage audit tooling | 2026-06-22-token-usage-audit-tooling | `harmonik usage` join transcripts×events | SHIPPED Phase-0 (TA1-4 + keeper-band cuts) |
| Captain wake-economy | 2026-06-23-captain-wake-economy / wake-economy | Sonnet watch tier; captain wakes event-driven only | ACTIVE — MVP 7 beads CLOSED + cutover LIVE; soak follow-on (hk-we-soak1/2, hk-we10, hk-8yh32) UNSTAFFED/at-risk |
| Pi universal model/provider gateway | 2026-06-23-pi-openrouter-harness / pi-provider-switch | run beads on any Pi-driven model (OpenRouter/DeepSeek/ornith) | PARTIAL/GATE-LIFT — Pi per-bead PROVEN in-daemon (hk-fdbhf CLOSED, specs landed); gate (expires 07-09) effectively MET; multi-provider-concurrent (MR2) is the live remainder |

## Slice: 07-05 → 07-07 (quality cluster + research) — DONE

| Initiative | plan/codename | What | STATUS |
|---|---|---|---|
| Agent world-models research | 2026-07-05-agent-world-models | should harmonik use a learned world-model simulator | ABANDONED-as-build — RECOMMENDATION.md says don't build; folded into scenario-harness/quality-system |
| Model-selection/routing research | 2026-07-05-model-selection | per-task model quality/cost/routing policy | PARTIAL — research drafted + eval-metrics WS1 landed; routing policy not impl; prod routing shipped separately (hk-62w9a stratified routing + budget kill-switch) |
| Quality process (shift-left diagnosis) | 2026-07-05-quality-process | prevention-first staff diagnosis (source doc) | SUPERSEDED/folded into codename:quality-system |
| Captain's-crew teardown record | 2026-07-06-crew-teardown | parked-lane record when fleet torn down | DONE (documentation) |
| **Quality-system (build test/validation system)** | 2026-07-06-quality-system / codename:quality-system | SINGLE-FOCUS: vet daemon BEFORE it replaces live binary — testbed + assessor gate | **ACTIVE / Phase-1 building** — epic hk-hcrvb OPEN; many core-loop-proof T-tasks landed (matrix runner hk-g6plo, conformance gate BLOCKS); T9 full-matrix-green (hk-jjt6w) live in kerf feed; assessor designed+manifest-approved but not launcher-wired; Phase-2 twin blocked on Phase-1 merge |
| **Quality-enforcement (fail-closed gates)** | 2026-07-07-quality-enforcement / codename:quality-enforcement | enforcement-first pivot: flip advisory gates non-bypassable BEFORE more test-authoring | **ACTIVE / WS-A largely landed, epic hk-clska OPEN** — real agent-reviewer run (b95f4b2c), APPROVE-only commit hook, hook-drift CI, A5 drop continue-on-error all merged; branch-protection + WS B/C/D open |

## Admiral-files mine (initiatives only in tracking docs) — PENDING (agent a5ece913)
> Awaiting last agent. From admiral-initiatives.md (admiral has read fully this session), the ON-DECK/GATED
> items to fold in: Codex-on-remote, Codex-vetting-local (hk-0639 soak), Daemon-reliability lane (hk-sfvc),
> Watchdog session rename, Standing test-daemon harness (move ①, ELEVATED→BUILD), Hot-reconfigure +
> concurrent/multi-worker dispatch, remote-substrate last-mile. Operator-pending decisions: hk-4u1mb
> reviewer diff-budget, governor threshold, close hk-0639 soak epic.
