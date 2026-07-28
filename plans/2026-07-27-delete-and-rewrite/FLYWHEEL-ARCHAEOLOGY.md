# Flywheel archaeology — what it was, what survived, what to keep

Read-only investigation, 2026-07-28. Repo `/Users/gb/github/harmonik`, branch `phase1-session-restart-substrate`.

---

## The single most important correction up front

**"Flywheel" is two different systems built a fortnight apart, and the operator's question conflates them.**

| | **Flywheel v0 — the self-driving agent** | **Flywheel v1 — the stall detector** |
|---|---|---|
| Codename in repo | `flywheel` (kerf work), spec `cognition-loop.md` | `flywheel-motion` (kerf work), spec `flywheel-motion.md` |
| Built | 28–31 May 2026 | 14–21 June 2026 |
| Language / home | TypeScript, `.pi/extensions/flywheel/` | Go, `internal/sentinel/` + `internal/daemon/` |
| Size | 9,038 lines across 21 files | ~1,700 lines prod + ~3,700 lines test |
| What it was | An LLM agent that *replaced the human-driven orchestrator* — it woke on daemon events, picked work, dispatched it, managed its own context window and spend | A deterministic tripwire that *watches the human-driven orchestrator* and blocks it from saying "nothing to do" while work is stalled |
| Status | **DELETED 2 July 2026** (commit `353fc3c1e`) | **STILL IN THE TREE, RUNNING, in observe-only mode** |

The commit that deleted 9,038 lines killed **v0**. It did not touch v1. `specs/flywheel-motion.md` §0.2 — the section the operator asked about — is v1's spec **forbidding a rebuild of v0**. It is not a self-prohibition. Quoted in §4 below.

So the operator's instinct ("it turned into several things and we only want maybe a part of it") is exactly right, but the split is not where they think. The interesting live question is not "should we rebuild the deleted thing" — that is already answered no, twice, by two separate documents. The live question is **"should the movement governor that is still running in this repo be kept, and should it ever be allowed to act?"**

---

## 1. WHAT FLYWHEEL WAS

### 1a. v0 — the self-driving orchestrator (`.pi/extensions/flywheel/`, deleted)

Origin: `b821af581` (30 May 2026), "docs(flywheel): bootstrap kerf flywheel work — 12 beads + Pi extension scaffold". Feature-complete by `975164ae6` (31 May). Two days of work, then nothing for a month, then deletion.

It was a **Pi coding-agent extension** — a plugin loaded into every `pi` session started in this repo. Its `package.json` declared `"pi": {"extensions": ["./index.ts"]}`, which is why it auto-loaded, which is why it eventually fork-bombed the machine.

Mechanically, per file:

**`index.ts` (721 lines) — the harness.** Registered three tools the LLM could call: `note` (append a durable note to `.harmonik/cognition/notes.jsonl` that survives a context reset), `reset_context` (request a deferred context wipe at the next turn boundary), and `read_skill` (lazily fetch a procedure file from `.flywheel/skills/`, the "fat skills" pattern — the model decides when to read a procedure instead of it being baked into the prompt). It injected context-fullness percentage into every model call and force-reset the session at 100% fullness. It seeded a byte-identical `STABLE_PREFIX_TEXT` on every recycle with an Anthropic `cache_control` breakpoint, and probed `cache_read_input_tokens` to alert after 3 consecutive cache misses.

**`bridge.ts` (591 lines) — the nervous system.** Spawned `harmonik subscribe` as a child process and tailed its NDJSON event stream. On each event it: classified it (`wake-filter.ts`), debounced bursts (`debounce.ts`), advanced a durable watermark (`watermark.ts`), and either handled it in pure code or woke the LLM. Reconnected with exponential backoff. Filed an "investigate" bead on `run_failed`, idempotent via a `reaction:<event_id>` label, and routed it to a dedicated `investigate` queue — the queue *was* the billing mechanism (a subscription-billed Claude daemon pulled from it).

**`dispatcher.ts` (458 lines) — the pure-code refill.** On any slot-releasing event (`run_completed` / `run_failed` / `run_canceled`): run `kerf next --format=json --only=bead`, pre-screen candidates against three guards (already in the queue, already landed on origin/main, already failed twice), and dispatch via `harmonik queue append` (refill) or `submit` (first fill). **No LLM in this path.** Only when `kerf next` came back empty did it wake the model.

**`wake-filter.ts` (98 lines) — the wake economy.** A static three-tier table: *ignore* (`state_entered`, `node_dispatch_requested`), *deterministic* (`run_completed`, `heartbeat`, `queue_*`), *wake-LLM* (`merge_conflict`, `decision_required`, `escalate_user`). `reviewer_verdict` was discriminated on payload: APPROVE → deterministic, REQUEST_CHANGES/BLOCK → wake. Unknown event types defaulted to wake-LLM (fail toward judgment).

**`router.ts` (123 lines) — model stratification.** Tier 0 routine wake → skip entirely, no LLM. Tier 1 normal → Haiku. Tier 2 triage → Sonnet. Tier 3 judgment → Sonnet by default, Opus only if `FLYWHEEL_MODEL_TIER3` was set. A one-shot `exception_flag` forced a single Opus turn and cleared, specifically to prevent runaway Opus chains.

**`budget.ts` (156 lines) — the cost kill-switch.** Per-day USD tracking with a graceful-downgrade ladder: 80% → Opus downgrades to Sonnet, 90% → Sonnet downgrades to Haiku, 100% → hard halt and emit `flywheel_budget_exhausted`. Daily reset deliberately manual (`harmonik supervise resume`).

**`circuit-breaker.ts` (104 lines) — the fork-bomb guard.** Tracked its own reaction rate; >10 turns/min over a 60s sliding window tripped the breaker and required an operator resume.

**`watchdog.ts` (95 lines) — the liveness checks.** Three timers: `quiet` (no event in 5 min while runs are active), `run_stall` (a run active >600s), `daemon_down` (no heartbeat for 3× the interval).

**`watermark.ts` (117 lines) — the exactly-once ledger.** UUIDv7 cursor plus a reacted-ledger map, written atomically (temp + fsync + rename + parent-dir fsync), with a never-regress invariant and cold-start fallback on corrupt state.

**`tui-panel.ts` (284 lines) — the dashboard.** Polled `harmonik digest --json` at 1 Hz and rendered a live status widget above the Pi editor.

The trigger was **the daemon's own event stream**. The output was **dispatched work and durable notes**. The whole design was: *let deterministic code handle everything it can, and spend LLM tokens only on genuine judgment.*

**Also still on disk:** `.flywheel/` — v0's operator surface. `goals.md` (an operator-edited "what to focus on" file, currently empty of content), `skills/` (7 fat-skill procedure files), `README.md`. See §6 — one of these files is load-bearing for v1 and must not be swept up in a delete.

### 1b. v1 — the movement governor (`internal/sentinel/`, still live)

Origin: kerf work `flywheel-motion`, created 14 June. First code `5febb8fd7` (15 June), "feat(sentinel): flywheel V1 movement governor (LLM-free)". Feature-complete by 21 June. Last feature commit 24 June. Everything after that is lint and hygiene.

Two coupled loops, per `specs/flywheel-motion.md` §0.1:

**The negative loop (stop the system stopping).** Every ~2 minutes the daemon work loop computes a *movement score* by scanning `.harmonik/events/events.jsonl` over a 30-minute sliding window. Only terminal-progress events score: `bead_closed`, `run_completed{success}`, `reviewer_verdict{APPROVE}`, and HEAD-advances on `origin/main` — each weight 10. Starts and chatter score **zero**, explicitly so the orchestrator "cannot game the metric by talking". The score maps through a discrete staircase to one of four levels: Dormant / Watching / Active / Halt. Gates: an *opportunity gate* (won't fire with no ready beads and no undeployed tail), a *warmup gate* (30 min from daemon start), and a *sustained gate* (2 consecutive low windows). On a sustained trip in ACT mode it writes a `decision_required` exception into `.harmonik/decision_acks/`, adds a queue block via `DecisionBlocker.AddQueueBlock("sentinel")` that structurally prevents dispatch, and spawns a **fresh-context adversary session** to adjudicate. The trip can only be cleared by observed real movement or by a recorded legitimate-halt reason that the next adversary pass re-examines — never by the orchestrator's own say-so.

**The positive loop (make the system speed up).** When a bead closes whose class opts into "Phase-2 done" (deployed + verified, not merely merged), the daemon deterministically creates a follow-up `deploy+verify` bead — `stagedBeadGeneratorEval` in `internal/daemon/eagerfill_em063.go`. Four guardrails: rule-only (never LLM-invented), lands `open` and is never auto-dispatched the same tick, refill ceiling equals `max_concurrent`, and an at-most-once durable ledger keyed on `(bead, class)`. It gates on a confirmed `origin/main` landing of the run's `Refs:` SHA, not a local success flag. The generated bead carries a `needs-greenlight` label that blocks dispatch until a human runs `harmonik greenlight <id>`.

**A third piece, goal persistence** (§4): `harmonik goal-keeper` — a scheduled, minimal-context, one-shot agent that reads `harmonik comms log --from operator` since a cursor, distils it into `.harmonik/intent/goal-state.json` (objectives, antigoals, verbatim operator directives), and exits. The orchestrator re-reads it on every session restart. §4.5 explicitly **rejects** per-turn goal re-injection.

---

## 2. WHAT IT TURNED INTO

Every v0 capability, traced forward. Verdicts: **REBUILT** (genuine Go reimplementation, wired, running) / **PARTIAL** / **NAME ONLY** / **GONE**.

| v0 capability | Successor today | Verdict |
|---|---|---|
| **Eager pure-code refill** (`dispatcher.ts`: on slot free, run `kerf next`, pre-screen, append) | `eagerRefillEval` + `preScreenCandidates` + `kerfNextBeads` + `beadLandedOnOriginMain` in `internal/daemon/eagerfill_em063.go`; mirrored in `internal/orchestrator/eagerfill.go`. Same three guards, same kerf source, same append-vs-submit split. Called from the work-loop tick. | **REBUILT** — the single most valuable thing v0 contributed, and it is fully alive. |
| **Per-day USD budget kill-switch** (`budget.ts`) | `DaemonSpendMeter` in `internal/daemon/spendmeter_hkk3f8g.go` + `internal/daemon/perqueuespendmeter_tigaf11.go`. **Still reads the env var `FLYWHEEL_BUDGET_USD_PER_DAY`**, default 20 USD. Constructed and subscribed at daemon boot (`internal/daemon/bootstate.go`). | **REBUILT** (graceful-downgrade ladder dropped; the hard cap survived). |
| **Event stream ingestion** (`bridge.ts` spawning `harmonik subscribe`) | `harmonik subscribe` is a first-class CLI surface consumed by the watch role and by agents directly. The *bridge* — a process that turns events into agent wakes — is now the watch session (`internal/watch/`: `escalation.go`, `ledger.go`, `markers.go`) plus the `watch` skill. | **REBUILT, differently** — a supervised LLM session replaced a code shim. |
| **Wake filter / three-tier event classification** (`wake-filter.ts`) | The `wake-economy` kerf work (24 June) — "Watch-officer tier + captain wake-economy". Its problem statement: the orchestrator burns context because it wakes 240–370×/day, 80–90% churn. Landed as `internal/watch/` + `scripts/ops-monitor-check.sh` (deterministic checks moved out of the LLM tick) + the escalate-only watch contract. | **REBUILT** — same idea, better placed. |
| **Watchdog timers** (`watchdog.ts`: quiet / run-stall / daemon-down) | `internal/sentinel/signals.go` `ComputeSnapshot` + `internal/sentinel/layera_hkl087e.go` `DetectLayerA` — heartbeat-gap, review-stall, run-age. Landed 5 July from the **`stall-sentinel`** kerf work. **Verified: zero production callers.** `grep` for `DetectLayerA`/`ComputeSnapshot` outside `internal/sentinel` and its tests returns nothing. | **PARTIAL — built, never wired.** ~633 lines of prod code + ~980 lines of test, dead. |
| **Model stratification / tiered routing** (`router.ts`) | Nothing equivalent in Go. The current system routes by *role*, not by event tier: implementer crews go to Codex, oversight sessions to Claude. That is a coarser, human-managed version of the same idea. | **GONE as a mechanism**, surviving as a manual convention. |
| **Context-fullness management + forced reset** (`index.ts` MemGPT 70/90/100 pattern) | `internal/keeper/` — the session keeper. Warn/act/force bands (200k/215k/240k tokens in this repo's config), drives handoff → `/clear` → `/session-resume`. Far more developed than v0's version. | **REBUILT and surpassed.** |
| **Durable notes across context resets** (`note` tool → `notes.jsonl`) | Replaced by `HANDOFF.md` + the session-handoff/session-resume skills + `.harmonik/context/` tiers + goal-state. | **REBUILT, differently.** |
| **Fat skills fetched on demand** (`read_skill` tool → `.flywheel/skills/`) | The Claude Code skill system: `.claude/skills/` + the `Skill` tool. Same pattern (procedures loaded on demand, not baked into the prompt), provided by the harness instead of by us. | **REBUILT by the platform** — we no longer own this. |
| **Reaction-rate circuit breaker** (`circuit-breaker.ts`) | Nothing. | **GONE.** |
| **Watermark + exactly-once reacted ledger** (`watermark.ts`) | The comms bus's at-least-once + dedupe-on-`event_id` contract (N3, the `agent-comms` skill) covers the messaging case. The follow-up ledger (`internal/daemon/followup_ledger_ac1.go`) covers the bead-generation case. No general-purpose event watermark survives. | **PARTIAL** — the idea survives per-consumer, not as shared infrastructure. |
| **Live status TUI panel** (`tui-panel.ts` polling `harmonik digest --json`) | `harmonik digest` and `harmonik digest watch` (`cmd/harmonik/digest/watch.go`) — the Go side is fully built. The Pi widget is gone. | **REBUILT** (the data surface, not the widget). |
| **Investigate-bead on failure** (`bridge.ts` SC4) | Nothing automatic. Failures are triaged by the orchestrator or by the `major-issue-fanout` protocol. | **GONE.** |
| **Two-phase done / git-is-truth** (CL-051, EV-040, EV-041) | `internal/cognition/` — `twophasedone.go`, `gitdone_ev041.go`, `missingheartbeat_ev040.go`. **Zero production importers** (verified by grep for `internal/cognition`). | **PARTIAL — built, never wired.** The *concept* did survive, reimplemented independently inside `eagerfill_em063.go` (`beadLandedOnOriginMain` checks `origin/main`, not local). |
| **The supervisor process** (`harmonik supervise`, designed as v0's lifecycle manager) | Alive and load-bearing — it is the daemon auto-reviver. **But it is still built entirely around a tmux session literally named `harmonik-<project_hash>-flywheel`** (`cmd/harmonik/supervise/config.go` `FlywheelSessionName`). `start`, `stop`, `attach`, `logs`, `reap`, exit code 24 `ExitCodeFlywheelSessionExists`, and the daemon's orphan sweep all key on that name. When `Command` is nil it runs in "watchdog-only mode" — which is the only mode used today. | **NAME ONLY.** The process survived; the thing it was built to supervise did not. |

### v1's own status: wired, running, and taking no action

The v1 governor is **not dead code**. It is called from `internal/daemon/workloop.go` inside the poll tick, twice (an observe block and an act block), seeded at boot by `seedGovernorDeps` in `internal/daemon/bootworkloop.go`. `sentinel.EmitTrip`, `ClearTrip`, `SpawnAdversary`, `IsTripAcknowledged` all have real call sites. `DecisionBlocker` gates dispatch at four places. Three CLI commands are registered in `main.go`: `harmonik sentinel` (emit-trip / clear-trip / record-halt), `harmonik goal-keeper`, `harmonik greenlight`.

But **`.harmonik/config.yaml` sets `sentinel: mode: observe`**, and `done_definition` is commented out. Consequences, measured:

- **34,125 `governor_signal` events** in `.harmonik/events/events.jsonl`, spanning 21 June → 22 July. That is **12.4% of all 275,539 events** in a 102 MB file.
- **Zero trips.** `.harmonik/decision_acks/` does not exist on disk.
- **Zero follow-up beads.** No `done_definition` classes configured → `stagedBeadGeneratorEval` returns at guardrail 1. No `follow-up-ledger.jsonl`.
- **No goal state.** `.harmonik/intent/` does not exist. `.harmonik/schedules.json` has no `goal-keeper` job (this project was initialised before the seeder existed), and every job in it is `enabled: false` anyway.

Spec §9's status table is stale: it lists FW4 (adversary spawn) and AC4 (legitimate-halt clear) as PENDING; both are landed in code.

### The measurement that decides this

Replaying the 34,125 observe-mode signals by activation level:

| Level | Count | Share | What ACT mode would have done |
|---|---:|---:|---|
| Dormant | 19,266 | 56% | nothing |
| Watching | 1,620 | 5% | nothing |
| **Active** | **8,719** | **26%** | **blocked all dispatch and spawned an adversary session** |
| **Halt** | **4,520** | **13%** | **killed the daemon and paged the operator** |

Only ~4.5% of signals were suppressed by any gate (the rest have `SuppressedBy: ""`).

Had the ACT flip ever happened, this governor would have halted the daemon roughly four and a half thousand times in five weeks. That is not a tuning problem. The metric counts only commits, merges and bead-closes; it therefore reads *design work, spec work, planning, a quiet weekend, and a deliberate operator pause* as identical to *stalled*. The governor's own comments concede the cost: a bug fix (`hk-usn8o`, 23 June) records that scanning `events.jsonl` every tick "caused 25–50% daemon CPU on large logs" and had to be cadence-gated — and the governor is itself generating an eighth of that log.

---

## 3. WHAT DIED AND DID NOT COME BACK

| Capability | What it did | Does anything need it? |
|---|---|---|
| **Event-tier model routing** (`router.ts`) | Routed each wake to Haiku / Sonnet / Opus by event class, with a one-shot escalation flag to prevent Opus chains | **Maybe.** The `wake-economy` work identified LLM cost as the top constraint, and the operator's standing preference is to route implementation to Codex and reserve Claude for oversight. That is the same policy applied by hand. A per-event tier table only pays off if something other than a human is choosing the wakes — which nothing currently is. **Not needed today.** |
| **Reaction-rate circuit breaker** | Tripped at >10 agent turns/min to stop a runaway loop | **No.** The thing it protected against was v0's own auto-wake loop. Nothing auto-wakes now. Ironically the fork bomb that killed v0 was an *unguarded `kerf next` poll*, not a model loop — the breaker did not cover it. |
| **Auto-filed investigate bead on `run_failed`** | Deduped on `reaction:<event_id>`, submitted to a dedicated queue | **Arguably yes.** This is the one genuinely missing behaviour: failures currently need a human or an orchestrator session to notice them. But it is a 50-line feature against the existing bead + queue surface — it does not require flywheel. |
| **Shared event watermark / exactly-once reacted ledger** | Cursor + ledger so a restarting consumer never double-reacts or skips | **Not as shared infrastructure.** Each consumer that needed it grew its own. |
| **Durable-notes-across-reset tool** | Agent-callable `note()` writing to an append-only log | **No.** Handoff files and the keeper cycle cover it. |
| **Context-fullness injection into every model call** | Told the model how full its own window was | **No.** The keeper watches from outside, which is strictly better — the model cannot lie to it. |
| **The whole "agent replaces the operator" thesis** | An LLM loop that picked and dispatched work indefinitely with no human in the loop | **No — and it is locked shut.** See §4. |
| **Stall detection (`DetectLayerA`)** | Per-run heartbeat-gap / review-stall / run-age detection → `stall_detected` | **YES — this is the real gap.** It is built (5 July), it is good, and it is unwired. Silent run hangs are a named recurring failure class in the quality-system program map (S1/S6: "unbounded `sess.Wait` idle hang", "flagless REQUEST_CHANGES wedge"). See §5. |

---

## 4. WHY IT WAS KILLED

Four separate documents, four different reasons, and they are not in conflict — they are four stages of the same retreat.

### 4a. v0 was killed for being **actively dangerous**, not for being wrong

Commit `353fc3c1e`, 2 July 2026, in full:

> chore: remove dead flywheel pi extension (auto-loaded on pi start, fork-bombs kerf next; flywheel retired, replaced by captain)
>
> The .pi/extensions/flywheel/ cognition-loop extension auto-loaded on every pi
> session in this repo and polled 'kerf next' with no backoff -> fork bomb (load 249).
> It is git-tracked so every daemon worktree inherited it, making it a Pi-lane blocker
> (hk-9s5fx). Flywheel is retired (locked don't-revive). Removing it entirely.

Load average 249. Because it was declared in `package.json` as a Pi extension, it loaded into *every* Pi session, and because it was git-tracked, *every daemon worktree inherited it*. The quality-system program map lists this under sandbox/egress failures as "hk-9s5fx (dead-flywheel-ext fork bomb)".

Note the ordering: it says the code was **already dead and already retired** — the deletion was cleanup of a landmine, not the decision itself. The decision ("replaced by captain") had already happened, tacitly, when the captain-and-crew system was built in June.

The bead behind it, `hk-9s5fx` (closed 10 July), records both the authority and the loose end:

> Primary fix DONE: deleted+pushed `.pi/extensions/flywheel/` (commit 353fc3c1) **per operator order**; also rm-rf'd on-disk remnants. Worktrees no longer inherit it. … **Broader flywheel teardown handed to admiral.**

**That broader teardown was never written.** It is the document this investigation is standing in for.

`plans/2026-07-06-crew-teardown/PARKED-INITIATIVES.md` records the same, tersely:

> **flywheel** tmux session — dead (Pi-harness superseded it); torn down.

### 4b. v1's spec forbids rebuilding v0 — this is §0.2, and it is not what it looks like

`specs/flywheel-motion.md` §0.2, verbatim:

> ### 0.2 GRAFT posture [blocker A — locked]
> The system MUST be built as an **incremental graft onto the live captain+daemon**, reusing shipped
> primitives. It MUST NOT rebuild the Architecture-B cognition-loop that *replaces* the interactive captain.
> The captain REMAINS the judgment organ (rank a brand-new initiative, name a lane, adjudicate drift); the
> flywheel is the deterministic skeleton that *forces judgment to be exercised*, not a substitute for it.
> The thinnest negative-loop slice MUST ship first (§8) so the whole approach is falsified early if
> over-deference proves un-fixable from outside the captain's context.

**This is v1's spec prohibiting v0.** "The Architecture-B cognition-loop that *replaces* the interactive captain" is precisely the TypeScript extension. §0.2 is not flywheel-motion forbidding itself; it is flywheel-motion declaring itself the *modest* successor to an ambition already judged too large.

And §0.3, which is the durable insight worth extracting regardless of what happens to the code:

> **Drift and over-deference are beaten by INDEPENDENCE or DETERMINISM — never by more prompt text in the
> same context.**
>
> Every mechanism in this spec is either (a) deterministic Go in the digest/daemon layer, or (b) a
> fresh-context independent session. No part of the fix is "stronger wording" in the captain's own context;
> any such proposal MUST be rejected as the historically-failing class (≥3 regressions in a month).

Note also the falsification clause in §0.2: *"so the whole approach is falsified early if over-deference proves un-fixable from outside the captain's context."* The spec asked to be tested. §2 above is the test result, and it came back negative — but nobody ever wrote that down.

### 4c. v1 was not killed. It was **never turned on**, and the gate was never closed

`HANDOFF-flywheel.md` (20 June, still in the repo root) opens:

> **State: BLOCKED (infra), not broken.** The flywheel code is fine; nothing can be dispatched right now.

The blocker described there was unrelated to flywheel — every bead was being routed to a remote worker whose exec path was broken. That cleared. Then, two days later, `plans/2026-06-22-admiral-suggestions-central.md` lists the ACT flip as the **top open operator decision**:

> | A1 | **Flywheel live ACT-mode flip** — turning the governor from observe-only to actually intervening on the live fleet | Pending — operator-gated throughout session | Operator signs off; captain dispatched as CD3/CD4 |

and separately:

> | A4 | **C2-governor ownership decision** — who owns the flywheel governor in the production fleet | Pending — surfaced from MORNING-NOTE.md | Two options surfaced by the captain; awaiting operator choice |

Both were still open at the end of that session, and the decision log records "Flywheel live ACT-mode flip (CD3/CD4) — mentioned throughout session". `plans/2026-06-22-admiral-retro.md` mentions "the approaching flywheel act-flip operator-gate."

**The operator never said yes, and never said no.** Feature work stopped on 24 June. Everything touching `internal/sentinel` since then is lint cleanup, permission tightening, and error-check drains. The governor has been idling in observe mode for five weeks, writing an eighth of the event log, waiting for a decision that was never made.

Two beads name exactly what never happened. `hk-tu3i` — **"CD4: promote `sentinel.mode: act` on the LIVE FLEET daemon (LAST)"** — was closed without ever being executed. And `hk-xq1i` — **"DV3: FULLY-WORKING acceptance sign-off"** — carries the operator's own definition of done:

> Confirm the flywheel demonstrably keeps itself moving / self-corrects on the real fleet WITHOUT a human re-spinning it.

**That sign-off never happened either.** The programme was never accepted, and never rejected.

### 4c-bis. Then the record was accidentally erased

On 12 July a "freeze-and-carve" clean-slate bulk-closed the entire pre-pivot backlog — including all remaining flywheel beads and the epic `hk-0oca`, with the close reason:

> freeze-and-carve clean-slate: pre-pivot backlog superseded by census; re-derive from PLAN.md.

Among the beads swept was **`hk-n6rb` — "ED2: epic `hk-0oca` closeout/redefine + record v2 deferrals"**. The one task whose entire purpose was to write down the flywheel's disposition was closed, unstarted, by the sweep. **That is why no decision record exists.** It is a bookkeeping accident, not a judgement.

The same freeze erased the lane from live context: `.harmonik/context/captain-lanes.md`, `project.yaml` and `direction-log.md` contain **zero** occurrences of "flywheel" today. The last surviving posture is in the archive, `.harmonik/archive/2026-07-12-freeze-and-carve/`:

> **flywheel** (30/39) — **NEEDS A COMPLETE RE-ASSESSMENT before any more work** — untouched a long time; do not resume blind.

> **DEPRIORITIZED — do NOT staff:** eval-program · flywheel (needs full re-assessment before any work) · dehardcode.

That is a deferral, not a verdict — and it is the last thing anyone wrote before the topic vanished.

That is the honest answer to "why was it killed": **v0 was killed for setting the machine on fire; v1 was never killed at all — it stalled at an operator gate, its closeout task was swept away by an unrelated backlog reset, and it has been quietly running ever since.**

### 4d. The current rewrite has already ruled on the v0 spec, and has not looked at v1

`plans/2026-07-27-delete-and-rewrite/NEXT_STEPS.md` — yesterday's plan — audits spec drift and rules:

> | `cognition-loop` | 49% | **DELETE** — a faithful map of a subsystem deleted 2026-07-02. `specs/flywheel-motion.md` §0.2 explicitly forbids rebuilding it. Salvage CL-030..033, CL-051, CL-083, CL-090/090a. |

and names the deletion as a root cause of the repo's spec rot:

> **Only 37% is a tagging fix. 36% (B+C) is actively wrong — and stale dominates aspirational.** The specs are not wishful; they are **rotted**. Two deletions caused most of it: commit `353fc3c1e` (2026-07-02) removed the entire cognition-loop implementation (9,038 lines of `.pi/extensions/flywheel/`) …

`flywheel-motion.md` itself is measured at a 25% orphan rate — the "thin but not empty" middle band — and given **no verdict**. It has not been triaged. The rewrite's `KEEP-DELETE.md` touches flywheel only mechanically: two scenario tests (`scenario_flywheel_bt5_hk5pcr_test.go`, `scenario_sentinel_bt4_trip_clear_hk5v3r_test.go`) land in the acceptance-tier carve-out purely because they match a filename pattern, not because anyone decided the behaviour matters.

**So the decision the operator is being asked to make is genuinely open. Nobody has made it.** `br list --status=open` returns zero flywheel, sentinel or governor beads — not because the work is finished, but because it was administratively closed. There is no open task tracking any of this.

### 4e. The one contradiction to resolve explicitly

The commit says flatly *"Flywheel is retired (locked don't-revive)."* The spec says narrowly *"MUST NOT rebuild the Architecture-B cognition-loop."* **These have never been reconciled in writing.** Read broadly, the commit retires everything including the Go governor that is currently running in the daemon. Read narrowly — which is how the July rewrite plan reads it — only the TypeScript agent is locked out. A subsystem the commit calls "retired" is executing in production on every daemon tick. Whichever way this analysis lands, that sentence needs to be made unambiguous.

---

## 5. THE RECOMMENDATION

### Keep one thing, and it is not the flywheel

**Keep `internal/sentinel/signals.go` + `layera_hkl087e.go` — the per-run stall detectors — and wire them. Delete the movement governor and the positive loop. Do both as part of the work-loop decomposition, not after it.**

Detail:

**KEEP AND WIRE — per-run stall detection (`ComputeSnapshot` + `DetectLayerA`).** This came from a *different* kerf work (`stall-sentinel`, July) that borrowed the `sentinel` package name. It answers a question that is genuinely hard and genuinely recurring: *is this specific run hung?* Three signatures — heartbeat gap, reviewer-verdict-with-no-terminal-event, run-age backstop. All three map to named production failures in the quality-system program map. It is ~630 lines, already written, already tested, and has **zero production callers**. Wiring it into the work loop is a small, bounded, high-value change. Crucially, its judgment is **per-run and evidence-local** — "this run emitted nothing for 20 minutes" is a fact, not an inference about intent — which is exactly where the movement governor fails.

**DELETE — the movement governor** (`internal/sentinel/governor.go`, `trip_ev043b.go`, `adversary.go`, the two work-loop blocks, `internal/digest/sentinelconfig.go`, `internal/digest/resolver.go`, `cmd/harmonik/sentinel_cmd.go`, the `sentinel:` config block, `.flywheel/skills/sentinel-adversary.md`). The measurement in §2 is decisive: 13% of its own signals were "kill the daemon" and 26% were "block all dispatch", against a metric that cannot distinguish a design session from a stall. Its two live costs today are 12% of a 102 MB event log and a documented 25–50% daemon-CPU hazard that had to be papered over with a cadence gate. It has produced exactly one useful artifact in five weeks: the proof that it does not work.

**DELETE — the positive loop** (`stagedBeadGeneratorEval` and its ledger in `internal/daemon/eagerfill_em063.go`, `followup_ledger_ac1.go`, `cmd/harmonik/greenlight_cmd.go`, the `needs-greenlight` label handling). It has never fired. It requires a `done_definition` class config that nobody has ever written, and the bead it would create is one a human must green-light anyway — so it saves one `br create` in exchange for a durable ledger, a provenance check against `origin/main`, a WIP ceiling, and a label-gated dispatch path threaded through the work loop. **Careful:** `eagerRefillEval` lives in the same file and is genuinely valuable. The file's own header says the two "share the file, not the pipeline". Split, don't delete wholesale.

**DELETE — goal-keeper** (`cmd/harmonik/goalkeeper_cmd.go`, `internal/goalstate/`, the init seeder). Never run in this project. `.harmonik/context/project.yaml` and `captain-lanes.md` are the goal-state that actually gets read, maintained by hand, and they work.

**DELETE — `internal/cognition/`.** Three source files, zero importers, from the deleted v0 spec.

**KEEP, unchanged — the parts already rebuilt, where the question is moot:** eager refill (`eagerRefillEval`), the daemon spend meter, the keeper, the watch role, `harmonik subscribe`, `harmonik digest`. Nobody is proposing to touch these; they are listed so they do not get swept up when someone greps for "flywheel" and finds `FLYWHEEL_BUDGET_USD_PER_DAY`.

**KEEP, and this is the load-bearing subtlety — `DecisionBlocker`** (`internal/daemon/decision_block_ev043a.go`, `.harmonik/decision_acks/`, `buildPendingDecisions` in `internal/digest/builder.go`). This is *not* flywheel. It is the general human-in-the-loop decision mechanism (spec `hitl-decisions.md`), used by `decisionshandler_*.go` and `cmd/harmonik/decisions.go`. The governor was one of several callers. Removing the governor must remove only the `AddQueueBlock("sentinel")` call site, not the blocker.

### Why this recommendation and not "keep it in observe mode, it's harmless"

Because it is not harmless, and because the work loop is being decomposed right now. `internal/daemon/workloop.go` is 6,656 lines, and the governor occupies roughly 190 of them as two near-identical blocks — the observe block and the act block differ only in a `switch`. Every line of that has to be carried through the decomposition by hand. Carrying dead-by-measurement code through a refactor is how the code becomes permanent: after the move it will look load-bearing, and nobody will re-derive the 4,520 halts.

### What it costs to be wrong

**If I am wrong about deleting the governor:** the failure mode it was built for — the orchestrator declaring "nothing to do" while ready work sits in the backlog — recurs, and there is no automatic tripwire for it. Recovery: the code is in git history at `e2252c3a`/`5febb8fd7`, the spec is preserved (§6), and the honest replacement is cheaper anyway — a scheduled script that checks "ready beads > 0 AND no `run_completed` in 2 hours" and posts to comms. That is a dozen lines against the existing `harmonik schedule` + `ops-monitor` surface, and it escalates to a human rather than killing the daemon. **The cost of being wrong is low and the recovery is well-understood.**

**If I am wrong about deleting the positive loop:** nothing happens, because it has never fired.

**If I am wrong about keeping stall detection:** ~630 lines survive a rewrite for one more cycle and get deleted later.

**If I am wrong in the other direction — if the governor is kept and someone flips ACT:** the daemon self-kills. Repeatedly. Mid-rewrite. That asymmetry is the whole argument.

### The one thing worth extracting before deleting anything

§0.3 — *"Drift and over-deference are beaten by independence or determinism — never by more prompt text in the same context"* — is a genuine, hard-won design principle backed by "≥3 regressions in a month". It belongs in the orchestrator-rules skill or in `docs/`, in one sentence, permanently. It is the most valuable output of the entire flywheel programme and it is currently buried in §0.3 of a spec that is about to be deleted.

---

## 6. DISPOSITION OF EVERY REMAINING FLYWHEEL ARTIFACT

| Artifact | Disposition | Reasoning (one line) |
|---|---|---|
| `specs/flywheel-motion.md` | **GUT to a stub** | It is normative and it describes behaviour that will no longer exist; but §0.2 and §0.3 are the two conclusions worth keeping, so replace the body with a dated "retired — never left observe mode, 13% of signals were spurious daemon-halts" note that preserves those two paragraphs. |
| `specs/cognition-loop.md` | **DELETE** | The rewrite plan already ruled on it — a faithful map of a subsystem deleted a month ago; salvage the named IDs first. |
| `HANDOFF-flywheel.md` (repo root) | **DELETE** | A 20 June session handoff whose blocker resolved and whose next step was overtaken; it is untracked session state that outlived its session and now misleads (it asserts the code is unwired, which stopped being true the next day). |
| `.flywheel/skills/sentinel-adversary.md` | **DELETE — but only with the governor, and check first** | It is not documentation: `internal/sentinel/adversary.go` `DefaultAdversaryMissionRelPath` points at this exact path, so it is a runtime dependency of FW4. Delete it and the governor in the same commit, or neither. |
| `.flywheel/` — everything else (`goals.md`, `README.md`, 6 remaining skills) | **DELETE** | v0's operator surface; the extension that read it has been gone for four weeks, `goals.md` is empty of content, and `README.md` documents install steps for a directory that no longer exists. |
| `internal/sentinel/governor.go`, `trip_ev043b.go`, `adversary.go` (+ their 6 test files) | **DELETE** | The measured verdict in §2. |
| `internal/sentinel/signals.go`, `layera_hkl087e.go` (+ tests) | **KEEP — and file a bead to wire them** | Different work, different question, genuinely useful, currently dead; this is the one salvage. Rename the package if the "sentinel" name is now confusing. |
| `internal/daemon/workloop.go` — the two governor blocks | **DELETE during the decomposition** | ~190 lines of near-duplicated code in the file being decomposed; removing it now is strictly cheaper than moving it and removing it later. |
| `internal/daemon/eagerfill_em063.go` | **SPLIT** | `eagerRefillEval` is live and valuable; `stagedBeadGeneratorEval` has never fired — the file header itself says they share a file, not a pipeline. |
| `internal/daemon/followup_ledger_ac1.go` | **DELETE** | Only consumer is the staged-bead generator; no ledger file has ever been written. |
| `internal/daemon/decision_block_ev043a.go`, `.harmonik/decision_acks/`, `buildPendingDecisions` | **KEEP** | General human-in-the-loop decision machinery per `hitl-decisions.md`; the governor was one caller among several — remove only the `AddQueueBlock("sentinel")` site. |
| `internal/digest/sentinelconfig.go`, `internal/digest/resolver.go` | **DELETE** | Config adapter and suppression resolver that exist solely to feed the governor. |
| `cmd/harmonik/sentinel_cmd.go` | **DELETE** | `emit-trip` / `clear-trip` / `record-halt` are the operator escapes for a trip that will no longer exist. |
| `cmd/harmonik/goalkeeper_cmd.go` + `internal/goalstate/` + the `harmonik init` seeder | **DELETE** | Never run here; `.harmonik/context/` is the goal state that is actually maintained and read. |
| `cmd/harmonik/greenlight_cmd.go` + the `needs-greenlight` label paths | **DELETE with the positive loop** | Sole purpose is releasing beads that only the staged-bead generator creates. |
| `internal/cognition/` (`twophasedone.go`, `gitdone_ev041.go`, `missingheartbeat_ev040.go`) | **DELETE** | Zero importers; the surviving idea (git is truth, check `origin/main`) is independently implemented in `eagerfill_em063.go`. |
| `sentinel:` block in `.harmonik/config.yaml` | **DELETE** | Configures a governor that will be gone; leaving it means the next person re-derives all of this. |
| `internal/core` event types `governor_signal`, `liveness_halt` | **DELETE the emitters, keep the type constants until the log rotates** | 34k historical events reference them; removing the constants while `events.jsonl` still holds them breaks replay and the digest reader. |
| `harmonik supervise`'s `-flywheel` tmux session name | **KEEP the behaviour, RENAME the string — separately, and not now** | It is load-bearing (`FlywheelSessionName`, exit code 24, the daemon's orphan sweep all key on it) and renaming it is a live-fleet migration hazard; a directive to rename it to `-supervisor` already exists in the 12 July archive ("it's the daemon-revive watchdog"), so this is a known, already-decided naming wart that should not be bundled into this decision. |
| `.claude/skills/harmonik-lifecycle/SKILL.md` + `keeper/SKILL.md` + `captain/SHUTDOWN.md` flywheel references | **EDIT when the name changes, not before** | They correctly document today's behaviour; the `harmonik-lifecycle` ones must be mirrored byte-for-byte into `cmd/harmonik/assets/skills/` per this repo's embed rule. |
| `internal/daemon/scenario_flywheel_bt5_hk5pcr_test.go`, `scenario_sentinel_bt4_trip_clear_hk5v3r_test.go` | **DELETE — and remove from the rewrite's acceptance carve-out** | They are in `KEEP-DELETE.md` carve-out B because they matched a filename pattern, not because anyone judged the behaviour worth keeping; they test the governor. |
| `docs/flywheel-self-reinforcing-design.md` | **KEEP, add a dated header** | It is the design vision `flywheel-motion.md` cites, and it already carries a "supersedes the older cognition-loop change-design; operator is not bound to it" note — one more line recording the observe-mode outcome makes it a complete record. |
| `.kerf/works/flywheel-motion/06-completion-plan.md` | **CORRECT or delete** | Its "Live status — 2026-06-20" block asserts `sentinel.Evaluate()` has zero production callers and the loop is dead code; both became false the next day. It is the most actively misleading document in the tree — the previous investigation started from it and got the wrong answer. |
| `.harmonik/archive/2026-07-12-freeze-and-carve/*` | **LEAVE** | Archive; the only surviving record of the "needs complete re-assessment, do not staff" posture. |
| `.kerf/works/flywheel/`, `.kerf/works/flywheel-motion/` (rest) | **KEEP as-is** | Kerf process artifacts are a historical record, not normative; they cost nothing and they are the only place the 18-reviewer convergence and the (A)/(B)/(C) decision record survive. |
| `FLYWHEEL_BUDGET_USD_PER_DAY` env var | **KEEP** | Despite the name, this is the live daemon spend cap wired at boot; renaming it is a breaking config change for zero benefit. |

---

## Appendix — key file paths

- `/Users/gb/github/harmonik/specs/flywheel-motion.md` — v1 spec, normative, §0.2 quoted above
- `/Users/gb/github/harmonik/specs/cognition-loop.md` — v0 spec, already ruled DELETE
- `/Users/gb/github/harmonik/HANDOFF-flywheel.md` — 20 June session handoff, stale
- `/Users/gb/github/harmonik/.flywheel/` — v0 operator surface + the live adversary mission file
- `/Users/gb/github/harmonik/internal/sentinel/` — governor (delete) + stall detectors (keep)
- `/Users/gb/github/harmonik/internal/daemon/workloop.go` — the two governor blocks
- `/Users/gb/github/harmonik/internal/daemon/eagerfill_em063.go` — split: keep eager refill, drop staged-bead
- `/Users/gb/github/harmonik/internal/daemon/decision_block_ev043a.go` — keep, not flywheel
- `/Users/gb/github/harmonik/internal/digest/sentinelconfig.go`, `resolver.go` — governor-only adapters
- `/Users/gb/github/harmonik/cmd/harmonik/sentinel_cmd.go`, `goalkeeper_cmd.go`, `greenlight_cmd.go`
- `/Users/gb/github/harmonik/cmd/harmonik/supervise/config.go` — `FlywheelSessionName`, the vestigial name
- `/Users/gb/github/harmonik/plans/2026-07-27-delete-and-rewrite/NEXT_STEPS.md` — the cognition-loop verdict
- `/Users/gb/github/harmonik/plans/2026-06-22-admiral-suggestions-central.md` — the ACT-flip operator gate, never closed
- `/Users/gb/github/harmonik/.kerf/works/flywheel-motion/06-completion-plan.md` — actively wrong ("zero production callers"); correct or delete
- `/Users/gb/github/harmonik/.harmonik/archive/2026-07-12-freeze-and-carve/admiral-initiatives.md` — "needs a complete re-assessment before any more work"
- `/Users/gb/github/harmonik/docs/flywheel-self-reinforcing-design.md` — the design vision behind v1

## Appendix — the two traps in this material

**Trap 1: `sentinel.Evaluate()` has zero production callers.** This is stated in `HANDOFF-flywheel.md` and in the kerf completion plan, both dated 20 June. It was true when written and became false on 21 June. It is now the most-cited wrong fact about this subsystem. Verified today: `internal/daemon/workloop.go` calls `sentinel.Evaluate` at two sites inside the poll tick, seeded by `seedGovernorDeps` in `bootworkloop.go`.

**Trap 2: "all the beads are closed, so it must be done."** All 26 flywheel beads are closed. Some were closed on merge with real commits behind them; the rest were bulk-closed on 12 July with the reason "pre-pivot backlog superseded by census". `br` state is not evidence of completion here — the commit log is.
