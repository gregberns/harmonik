<!-- TIER: 2 (operational state, days cadence)
     LOADED BY: captain @ STARTUP Step 0b; NOT loaded by crews or implementers
     OWNER: captain, updated at session end (before HANDOFF.md) or on any crew/epic change
     DO NOT PUT HERE: standing behavioral rules (→ orchestrator-rules skill);
                      this-session salvage / run-id play-by-play (→ HANDOFF.md tier-1);
                      durable phase/locked decisions (→ project.yaml tier-3) -->

# Tier-2 context: captain lane registry + medium-term tracker (days cadence)
# Captain reads on every boot (STARTUP.md Step 0b) BEFORE re-deriving lanes.
# Stable across /clear cycles; verify every claim against live ground-truth at Step 2.

## ⭐⭐ OPERATOR DIRECTIVE 2026-06-30 (via admiral) — FRESH WAKE, STAFF 3 LANES · expires: 2026-07-04
> Fleet woke from a ~4-day sleep onto the security-fix daemon (7a9bf2e5, deploy daemon-20260630-01).
> Operator confirmed: **staff all THREE lanes below; REMOTE is the top priority.** This block
> SUPERSEDES every 2026-06-25 priority/lane block below (those directives are EXPIRED — treat as history).
> 1. **REMOTE-WORKER e2e proof** (`hk-nepva`, epic remote-hardening `hk-gx0dl`) — #1 PRIORITY.
>    Blocker `hk-t1t00` is CLOSED, so nepva is UNBLOCKED.
>    **OPERATOR TESTING CONSTRAINT (load-bearing):** do **VERY THOROUGH LOCAL testing** first via the
>    L0–L5 test pyramid / isolated test-daemon harness — testing that does **NOT require restarting the
>    live daemon**. **gb-mbp is UP and available for the live-remote portion** once local is solid.
>    Confirm routing via events.jsonl `run_started.worker_name=gb-mbp`, NOT daemon stderr.
> 2. **Pi-harness core build** (`hk-4rmj1`, codename:pilot) — the new OpenRouter-based implementer harness,
>    Phase-0 (mirrors codex). Operator-UNGATED now (was parked behind remote-reliable). Blocked-by `hk-1c16h`
>    (pilot B2) — verify that landed before dispatching B3; if not, staff B2 first.
> 3. **Keeper reliability** — `hk-u5tgh` (watchdog tmux-restart bypasses daemon → crews come back keeper-LESS)
>    + `hk-xxcv9` (crew boot doesn't auto-arm keeper). ⚠️ `hk-u5tgh` carries `hold:operator-design-decision`
>    — CHECK whether that design decision is settled (plans/2026-06-22-keeper-coverage-investigation/00-SYNTHESIS.md)
>    before dispatching it; `hk-xxcv9` (P2) is clean to staff now.
> Lanes run PARALLEL (file-disjoint). Stale `paused-by-failure` queues (main, paul-q, leto-codex) =
> pre-sleep cruft; reconcile, do NOT resume main.

## ⭐ OPERATOR-CONFIRMED PRIORITY SEQUENCE (set:2026-06-25 expires:~2026-06-29 — via admiral, survives /clear)
> The fleet ALREADY honors this — recorded so it survives a captain restart; NO reshuffle on read.
> Lanes run PARALLEL where slots + disjoint work allow. WIP-first is a TIEBREAKER, never serial gating.
> **This is a PRIORITY order, NOT a sequence: the #1 headline being blocked/gated NEVER idles #2–4.**
> If the remote-worker headline is parked behind a substrate/quiet-window gate, the OTHER lanes keep
> running — a single gated lane must never sequence or idle the rest of the fleet (orchestrator-rules
> §ANTI-IDLE). A lane is only idle on zero ready beads OR a named/dated/owned/unexpired gate.
> 1. **REMOTE-WORKER RELIABLE** — the headline (gurney STAGE-3 real e2e on gb-mbp). GOAL behind it:
>    once remote is PROVEN reliable, raise daemon `max_concurrent` 4→8 (remote adds the capacity).
>    **Do NOT bump concurrency now — GATED on remote-reliable.** Un-park gb-mbp + re-validate serialized
>    only after BOTH remote code bugs land (review.json read-retry = LANDED e4122ac9; worktree-HEAD race
>    = hk-iaj1w, gurney fixing offline).
> 2. **TOKEN-OPT** (epic hk-var9b / wake-economy) — currently ZERO ready beads (all blocked/deferred/in-flight)
>    = correctly idle, NOT a stall. Resume the moment ready file-disjoint work appears.
> 3. **CODEX** (leto pilot, queue leto-codex) — correctly filling idle local slots + offloads model cost.
> 4. **FLYWHEEL/FRAMEWORK** (admiral/captain framework: PLAN-v2 + stall-detector) — admiral drives;
>    artifact + skill edits, does NOT compete for daemon slots.

## ⭐ CURRENT TRUTH (2026-06-25 ~19:07Z — gb-mbp DISABLED again; fleet LOCAL; 2 lanes)
> **Live remote STAGE-3 (gb-mbp re-enabled 18:46Z for the headline) found TWO remote bugs that
> paused BOTH lanes — a RECURRENCE of the 2026-06-23T21:55Z concurrent-remote-failure pattern:**
> 1. 3/3 leto codex beads raced worktree-create on the worker → `git rev-parse HEAD returned
>    empty` (concurrent-worktree-HEAD race; proof beads hk-k0pz/xbpm/tzfw).
> 2. gurney hk-h106 → truncated remote `review.json` ErrMalformed (bead **hk-clrts**; root cause
>    diagnosed: `reviewverdict.go:126-139` does ONE cat-over-SSH, no retry/completion-wait).
>
> **RECOVERY (captain, this session):** disabled gb-mbp in workers.yaml (gitignored — persists, NOT
> committed), restarted daemon PID3214→28967 (brick-safe: config has `max_concurrent:4` +
> `liveness_no_progress_n:10`), routed LOCAL, resumed both queues. Execution+routing on gb-mbp WERE
> proven (commit a4ec1612) before the race surfaced — the headline's core proof holds.
>
> **CURRENT LANES (both LOCAL):**
> - **leto** (codex concurrency pilot, epic hk-8dtyk, queue leto-codex): ACTIVE — hk-vo31l + hk-dyqy
>   + hk-gx46b in implement LOCAL; standing lane, pulls file-disjoint clean P3/P2 when it drains.
> - **gurney** (remote-test-pyramid epic hk-6l941 / hardening hk-gx0dl, queue gurney-q): pivoted to
>   OFFLINE remote-bug fixes — hk-clrts (review.json, FIX-READY) + the worktree-HEAD race — fixed
>   out-of-daemon (isolated worktree→review→ff-land) + local L-series test layers.
> - **Headline hk-nepva (live remote e2e) PARKED — explicit un-gate condition:** un-park the
>   MOMENT (a) both remote bugs are landed on main (review.json read-retry = e4122ac9 LANDED;
>   worktree-HEAD race = hk-iaj1w) AND (b) a quiet window is available for the SERIALIZED
>   (max_slots:1) re-validation. When both hold, gurney RE-ENABLES gb-mbp and GOES — this is a
>   KNOWN ranked lane, not an operator escalation. This park is a substrate/quiet-window gate, NOT
>   "wait for captain to say go"; it must never sequence the whole fleet (leto + STAGE-1/local
>   gurney work run in parallel meanwhile).
> - watch = online, triaging correctly (escalated + confirmed recovery). admiral = oversight (no beads).
>
> ---
> ## (SUPERSEDED) CURRENT TRUTH (2026-06-25 ~06:28Z — COURSE CHANGE: remote test-hardening program)
> **Operator settled a new strategy (asleep now; admiral oversees, captain OWNS execution).**
> Stop chasing real-remote runs (feedback loop too slow). Instead build a **test pyramid
> (L0–L5)** that reproduces remote "separation" (filesystem / git-ref / tmux-process /
> SSH-transport) cheaply at rising fidelity. Daemon repointed **LOCAL** (gb-mbp DISABLED in
> workers.yaml, concurrency=4). Plan: `.harmonik/crew/designs/remote-test-strategy-plan.md`
> (+ `remote-iteration-impasse-plan.md` for moves ①②③, ④ skipped). **THIS block is authoritative;
> the lane table + directives below are PRE-COURSE-CHANGE and STALE.**
>
> **THE PROGRAM — kerf work `remote-test-pyramid`, epic `hk-6l941` (assignee gurney). Bead set filed + ranked:**
> - `hk-hd2w6` **L0** — runner-seam contract: add `daemon.Config.Runner`, thread to DOT gate/cascade,
>   add the 3 missing `…Via` read variants (gate-verdict `dot_gate.go:551/686` + `autostatusmarker.go:70`
>   are STILL bare `os.*` = unfixed hk-f3u6o class), + static no-bare-os.* audit + RecordingRunner contract test. **✅ LANDED 2026-06-25 (02da6...c940a8a5, DOT triple-review clean) — CLOSED.**
> - `hk-52xnr` **L1** (←L0) — twin harness on SEPARATED worker FS + Runner injection; deterministic verdict/gate-bug reproduction. **IN PROGRESS on gurney-q.**
> - `hk-8u2al` **L2** (←L1) ssh-localhost isolated · `hk-3q92c` **L4** (←L1) fault/chaos+replay ·
>   `hk-f10xl` **L5** (←L0) per-queue routing (move ②, gate `SelectWorker` workloop.go:2813) + scheduler property tests ·
>   `hk-yflqo` **L3** (←L2, P2, **LAST**) Docker/Lima containers + Linux-remote · `hk-o85ye` **move-③** (←L0, P2) bead-runs survive daemon restart.
> - `hk-t1t00` REWRITTEN (premise was wrong: affected-set is `headSHA..HEAD` per scenariogate.go:325-333, `HK_GATE_BASE_SHA` doesn't exist) — folded in, blocked-by L1.
>
> **OPERATING RULES (operator, this program):** all TESTING → low blast-radius, keep moving.
> Blocking bug mid-stream → SMALL fixes done DIRECTLY out-of-daemon (isolated worktree→review→ff-land),
> NOT the slow pipeline. Crews MAY use sub-agents but EVERY change reviewed. Review gate = multi-agent
> consensus of ≥2 DIVERSE agent types, NOT human signoff; split → admiral adjudicates.
>
> **Crew state:** gurney = remote-test-pyramid (LOCAL gurney-q; L0 landed, NOW running L1+L5 = 2 workers, M3 held).
> admiral = oversight + hourly watchdog (holds no beads). **watch = OFFLINE/wedged** (absent from comms who,
> pane frozen at "Sautéed 4m36s" w/ unsubmitted prompt @114k — flagged, NOT critical-path; captain armed
> direct on comms recv; let ops-monitor crew-fresh flag catch it). **paul DOWN — HELD** (wake-economy parked). All other
> initiatives PARKED behind this program (Pi gateway, codex-on-remote, de-hardcode-messages, wake-economy).
> hk-f3u6o LANDED on main (5999a39a) + CLOSED. gb-mbp re-enable is a later phase (L2/real-remote smoke), not now.
>
> **Staffing decision (RESOLVED 2026-06-25 on L0 land):** did NOT split to a 2nd crew. L5/M3 are children
> of gurney's epic hk-6l941, so a 2nd crew would collide on epic ownership + cost an Opus boot for marginal
> gain (daemon parallelizes either way at max-concurrent=4). Instead gurney fans out the file-disjoint
> unblocked layers into gurney-q: L1 (test-harness) ‖ L5 (daemon-routing) running concurrently; M3
> (hk-o85ye, runs-survive-restart) sequenced AFTER L5 since both touch internal/daemon (worktree-merge
> collision guard). L2/L4 unblock when L1 lands — gurney continues them on the same queue.

## active_lanes  (PRE-RESTART — STALE as of the 22:50Z restart; see CURRENT TRUTH block above)

> **DURABILITY NOTE:** this file MUST be COMMITTED, not left uncommitted-modified — an
> uncommitted tier-2 edit was clobbered 2026-06-24 when a worktree merge (a8d4591b) reset
> the working tree. Commit tier-2 changes the same session you make them.

> **OPERATOR DIRECTIVE 2026-06-24 (standing, via admiral):** the daemon must run **4
> concurrent instances always** on this box — "1-2 is not enough." Live `queue
> set-concurrency 4` is set. There is NO config field for default concurrency (reverts on
> restart), so the RESTART RECIPE must re-apply `set-concurrency 4` AND raise the spawn cap
> (HARMONIK_MAX_CONCURRENT_SESSIONS 8 → ~16) at the next daemon restart so 4 concurrent DOT
> triple-review runs don't hit spawn_cap_blocked (memory hk-vfeeo). Keep 4 file-disjoint
> lanes staffed.

> **RESTART-RECIPE MANDATORY STEP — hk-drygf deploy landmine (added 2026-06-24, captain):**
> hk-drygf LANDED on main (SHA a0af5152) made `liveness_no_progress_n` a **REQUIRED** config
> key, but live `.harmonik/config.yaml:63` has it COMMENTED OUT (`# liveness_no_progress_n: 10`).
> The running daemon + auto-revive use the OLD installed binary so it is NOT bricking now — but
> the moment this binary is `go install`ed and the daemon restarts (the SAME lull-deploy restart),
> `GovernorConfig()` fails loud and **the daemon REFUSES TO BOOT** until the key is set. BEFORE that
> restart: (1) set `config.yaml:63` to `liveness_no_progress_n: 10` (jamis RECOMMEND — enables the
> liveness detection axis at historical default, observe/emit-only, low risk) OR `: 0` (explicit
> keep-disabled). The VALUE is the operator's threshold-policy call by design; default to 10 unless
> operator says 0. (2) Fix the now-false `'all fields optional'` block-header comment.

> **OPERATOR DIRECTIVE 2026-06-23 (STANDING, via admiral, recorded 2026-06-24):**
> **TOKEN-OPTIMIZATION IS NOW THE PRIMARY PRIORITY — especially the CAPTAIN'S OWN token
> burn (ranks HIGHEST of all).** Reprioritize kerf-next ranking + crew staffing so
> token-burn-reducing work LEADS; token-opt WINS staffing/sequencing ties. Order:
> (1) **WATCH** initiative (captain's own wake-burn cut, hk-var9b) = TOP — sequence its MVP
> (coupled watch-standup + sender-redirect) ahead of other new work; (2) leanfleet
> token-efficiency (epic hk-itoc: restart-earlier, idle-restart, model-tiering); (3) keeper
> restart-earlier (hk-8hr1 — NOTE now CLOSED as no-op, band already on main); (4) per-crew
> model-fit; (5) codex re-prove→soak (ChatGPT-billed = throughput OFF the Anthropic budget).
> Other lanes MAY continue but yield ties to token-opt. Apply going forward.

| crew | epic_id / scope | lane (plain English) | queue | model |
|---|---|---|---|---|
| paul | hk-var9b / codename:wake-economy | **WATCH** (captain wake-burn cut) — TOP PRIORITY per token-opt directive. Design revised for operator's 4 rulings + the A/B follow-ups (WE1 = COUPLED watch-session-standup + sender-redirect MVP so captain never goes blind; scheduled-send = NATIVE comms-send action, bash-wrapper dropped). Critic gate on revised sequencing → build WE1-8. hk-8hr1 CLOSED (reconcile no-op, band already warn200K/act215K/force240K on main). hk-drygf governor HELD | paul-logmine | opus |
| jamis | hk-e3fy / daemon run_failed auto-reset | daemon: run_failed STILL strands bead in_progress on uncovered terminal paths (reviewer-pane-hang/verdict-silence) — auto-reset is partial post-c7062bb7; writing the real fix. `internal/daemon` | jamis-sh | — |
| gurney | hk-tef1s / codename:leanfleet | TOKEN-OPT (re-tasked from idle 2026-06-24): Mission front-matter `model:` field — make clean-drain crews explicitly Sonnet. Parser already exists (crewstart.go:494); WORK = markdown edits to `.harmonik/crew/missions/*.md`. hk-vfeeo (spawn-cap refuse-oversubscription) LANDED+CLOSED 7a8db433; live-resize half captured as backlog hk-omvan | gurney-q | sonnet |
| leto | epic hk-0639 codename:codex / Pi-design-on-hold | HELD for the Pi universal-model-gateway DESIGN crew (greenlit-pending-operator-go; brief plans/2026-06-23-pi-openrouter-harness). Codex soak CONCLUSIVE all shapes + ChatGPT-billing proven; no file-disjoint bead left to feed codex (gurney took the one available) | leto-ev | opus |

- **Fleet = daemon + captain + 4 work-crews + admiral (operator-engaged) + ctx-watchdog + ops-monitor.**
- Lanes file-disjoint: paul=`internal/keeper` + watch design, jamis=`internal/daemon`, gurney=`.harmonik/crew/missions/*.md` (markdown), leto=codex harness (LOCAL, held). HARD GUARD: gurney's daemon ledger follow-up hk-zgt4u is internal/daemon = collides with jamis — do NOT staff until jamis hk-e3fy lands.

### 2026-06-24 closures (reconciled this session)
- **hk-98jju (supervisor-revive epic) CLOSED:** daemon/supervisor auto-revive fix complete + live (watchdog-only mode decouples watchdog from the dropped flywheel; 5f18ba5e+d2b4f020; supervisor up). Operator DROPPED the flywheel keep-vs-remove scope → hk-zv6j3 CLOSED. Governor hk-drygf stays PARKED; decouple-idea captured as hk-qaqtl (low-pri, NOT dispatched). FLYWHEEL = dead cognition-loop, do NOT investigate; distinct from the live Pi gateway harness.

### 2026-06-24 admiral directive resolutions (operator-authorized)
- **Remote worker re-enable — APPROVED**, GATED on hk-92ih3 (paul) landing+verified. Then flip `workers.yaml` enabled:true + daemon restart + fix the stale re-enable comment (real gate = hk-scndr DONE + hk-92ih3, NOT hk-9a7rt) + raise spawn cap in that restart. Prove ONE remote DOT run on gb-mbp before scaling remote. Local stays 4.
- **Codex — UNPAUSED** (the "not daemon-runnable" framing was stale; ran e2e via daemon 2026-06-17). One LOCAL re-canary first (hk-n05u2 via leto). Codex bills ChatGPT, LOCAL-only (not on gb-mbp).
- **Supervisor / hk-drygf:** epic hk-98jju investigates (i) why `harmonik supervise` (the daemon auto-revive watchdog) dies on launch — REAL reliability gap, daemon has NO auto-revive now — and (ii) flywheel/sentinel keep-vs-remove. hk-drygf (governor-liveness) FOLDED in + stays HELD; do NOT apply FIX-A/FIX-B.
- **Supervisor-down alert spam:** mute the every-5m CRITICAL page only (hk-xr46t); the underlying check STAYS.

## Recently CLOSED / COMPLETED (2026-06-20 burst)

These epics are fully landed — moved out of the in-progress table. Verify against `br show <epic>` only if a regression is suspected.

| Initiative (plain English) | Epic / codename | Status |
|---|---|---|
| keeper-redesign — per-project config, zero hardcoded thresholds, actionable-warn self-restart handshake, hold/release co-working override, configurable hard-ceiling backstop, durable tmux↔session-id identity | `hk-gffc` | ✅ COMPLETE — ALL remaining beads landed (the earlier "operator-gated remaining" claim is stale; zero open) |
| captain-economy — slim captain boot (~81k→~55-60k tokens), Sonnet ops-monitor offload, comms `--wake` fix, per-crew `--model` | `hk-unjy` (CE1/CE4/CE5/CE6) | ✅ COMPLETE |
| tmux-session-organization — unified `harmonik-<hash>-*` namespace, agent+keeper window-nesting, window-granular restart, `supervise reap` | `hk-0v9e` | ✅ COMPLETE |
| doc-instruction-audit — three-kinds model (docs / skills incl. new `orchestrator-rules` / state tiers), AGENTS.md→router, new `harmonik sync-assets` + supervisor skew-notify | `hk-vk7b` | ✅ COMPLETE (Phase A+B); deferred follow-up `hk-fozq` (P3 supervisor auto-apply) |
| easy-start native launchers — native Go `harmonik start captain\|crew <name>`; bash `captain-launch.sh` retired; `captain respawn` self-heal; shared `agentlaunch` helper | `hk-kbjl`/`hk-bcd0`/`hk-sn4n`/`hk-z1rj` | ✅ CORE SHIPPED — integration paths (tmux-takes-effect, live `/clear`-injection) untested; see live-validation lane |
| remote-control session prefix — per-project RC session-label prefix (`hk-captain`) | `bf7d51f8` | ✅ CORE LANDED — 4 tail beads still open: `hk-dhe6` (epic), `hk-w8ex` (CLI parity test), `hk-25bg` (live 2-project validation), `hk-f4w7` (migration prompt) |

## LIVE FLEET STATE (snapshot 2026-06-20 ~22:30 — verify at boot)

> ⚠️ **STALE (2026-06-20) — SUPERSEDED. As of 2026-06-24: daemon UP & healthy (concurrency=4,
> spawn cap 16), NOT wedged; the `chani` session and the disk-90% firefight are over; the only
> live infra flag is `supervisor-down` (no daemon auto-revive) owned by jamis hk-f2j0o. Trust the
> active_lanes table + 2026-06-24 admiral directives above, not this block.**
>
> **Daemon is UP but WEDGED — clear before staffing crews.**
> - Main queue is `paused-by-failure` on `hk-tagp` (old remote e2e). Submit fresh named queues per lane; do NOT resume main.
> - Disk at **90% (≈19 GiB free; daemon paused dispatch at 2.5 GiB earlier and self-GC'd)**. `.claude/worktrees` = 3.1 GiB across **88 registered git worktrees** — worktree GC (lane L2) is a real prerequisite for reliable spawns.
> - Daemon's `br show` is erroring (exit 3) on a batch of beads — daemon-side; `br` from repo root is healthy (86 ready).
>
> **Other agents working NOW (coordinate, do NOT collide):**
> - **keeper polish** — an agent has uncommitted edits to `cmd/harmonik/captain.go` + `keeper_enable_doctor_cmd.go` and just landed the keeper "no-defaults" commit. → overlaps lanes **L7, L9**.
> - **remote-substrate e2e + disk firefighting** — the "chani" session owns the running daemon. → owns lane **L3** entirely; shares the daemon with **L1/L2** (merge/GC timing).

## Dispatch-ready lanes — captain → crew staffing (re-verified 2026-06-20)

One crew per lane (file-disjoint). Buckets: TEST=live-validate shipped code · BUILD=new code · BUGFIX · HYGIENE. Verify IDs at boot.

| # | Lane (plain English) | Bucket | Value | Epic / key beads | Ready? | Overlap |
|---|---|---|---|---|---|---|
| **L1** | **Daemon-reliability bugfixes** — kill the false-positives/strands that corrupt the orchestration signal | BUGFIX | **HIGH** | epic `hk-sfvc`; `hk-sj6a` (DOT review-phase freezes ~40m, no self-recover), `hk-53p3` (promote strands bead in_progress), `hk-ijtw` (review-gate false-pos sticks forever), `hk-gu3v` (crew-stale false-pos on active agent), `hk-vx1i`≡`hk-5zmz` (no_progress false-fire — DEDUP), `hk-g9zz` (rebase aborts on dirty worktree), `hk-guez` (cache-reaper races merge-build) | ✅ | merge-timing w/ chani |
| **L2** | **Disk / worktree GC** — reclaim space; 88 worktrees, 90% disk | HYGIENE | **HIGH** | `hk-ldzp` | ✅ | ⚠️ chani owns daemon — coordinate so GC doesn't race in-flight runs |
| **L3** | **Remote-substrate e2e proofs** — live-prove the remote path end-to-end | TEST | **HIGH** | `hk-4lrj` (DOT triple-review remote run lands on main — the only unproven variant), `hk-tagp`/`hk-h106` (agent_ready-over-tunnel proofs), 6× worktree-create proofs (`hk-icdz`/`3zij`/`d2z1`/`tzfw`/`xbpm`/`k0pz`), `hk-tyyy` (auto-scp binary), `hk-vjsv` (commit_gate stale-window loop) | ✅ | ⚠️ **IN-FLIGHT (chani)** — hand over / coordinate, don't re-staff |
| **L4** | **Fleet-state model + ZFC wind-down** — demote drain-oracle to a fact-tool, decisions back to captain | BUILD | **HIGH** | epic `hk-up4b` (supersedes `hk-rl4b`); `hk-pfr4` (GatherDrainFacts), `hk-kj7d` (delete auto-park tick), `hk-zqb3` (veto-on-execute), `hk-9mdz` (spec reword); **P2 gated behind design keystone `hk-9fvk`** → then `hk-8lne` (spec), `hk-gv04`/`hk-jay1` (aggregator + context-into-state) | P1 ✅ / P2 blocked on `hk-9fvk` | none |
| **L5** | **Flywheel wiring + validation** — wire `sentinel.Evaluate` into the tick, OBSERVE-only first, then staged ACT | BUILD+TEST | MED | epic `hk-0oca` (~28 beads); keystone `hk-y9fn` (FW1 config adapter) → FW2-6, AC1-5, BT1-6 tests, CD1-4 deploy, `hk-m8zqv` (4h soak) | ✅ keystone | none |
| **L6** | **rc-prefix tail** — finish CLI test, migration prompt, live 2-project validation | TEST+BUILD | MED | epic `hk-dhe6`; `hk-w8ex`, `hk-25bg`, `hk-f4w7`, `hk-kqra` | ✅ | none |
| **L7** | **Easy-start launcher validation** — integration tests + swap remaining doc examples to native | TEST | MED | epic `hk-q1ll`; `hk-dyqy` (doc swap) | ✅ | ⚠️ keeper tranche touches `captain.go` (in-flight keeper edits) |
| **L8** | **Codex-harness soak** — exercise the Codex implementer + verify ChatGPT billing each run | TEST | MED | epic `hk-0639`; `hk-cr31`/`y3g5`/`84c2`/`4cop`/`ngnv` | ⛔ operator-PAUSED 2026-06-19 | none |
| **L9** | **Crew keeper-gauge wiring** — wire the context gauge for crews on the live deploy | BUILD | MED | `hk-tt9q` | ✅ | ⚠️ keeper subsystem (in-flight) |
| **L10** | **Kerf binding + triage hygiene** — wire bead_filters, then `triage --ack` to restore drift detection | HYGIENE | MED | 63 untriaged; ~22 unwired works; baseline ~397 behind | ✅ | none |
| **L11** | **Doc-audit / security follow-ups** — close the tail; scrub plaintext API key | HYGIENE | LOW-MED | `hk-fozq` (asset-sync auto-apply); **P0: scrub any `*.env.txt` API key**; retention pruning | ✅ | none |
| **L12** | **Flaky-test stabilization** — deterministic-sort digest; fix watchdog regression; CI timeouts | BUGFIX | LOW-MED | `hk-z8fp` (Manifest.Digest map-order), `hk-7m39` (3 `TestDaemonWatchdog_*` RED on main), `hk-963f` (Tier-3 CI timeouts) | ✅ | none |
| **L13** | **leanfleet / token-burn efficiency** — restart-earlier, idle-restart, model-tiering | BUILD | LOW-MED | epic `hk-itoc`, `hk-bsdr` (analysis) | ✅ (research-heavy; partly superseded by L4) | none |
| **L14** | **Tooling capture (passive)** — aggregate emergent tooling patterns | HYGIENE | LOW | `hk-nlhys` — **CAPTURE-ONLY, do NOT formalize/dispatch** | n/a | none |

### START FIRST
**L1 (daemon-reliability, `hk-sfvc`)** — highest leverage: every bead is a false-positive/strand that corrupts the signal the whole fleet runs on; fixing them makes every other lane's dispatch trustworthy. All ready, no operator decision, distinct from the two in-flight areas (it's `internal/daemon/dot*` + reconcile). Dedup `hk-vx1i`≡`hk-5zmz`; coordinate merge timing with chani.
Runner-up: **L2 disk GC** — urgent (90% disk) but must coordinate with chani.

### DO NOT DISPATCH YET
- `hk-cyec` (fleet-state P4 desired-state reconcile loop) — operator HOLD.
- **L4 P2 code** (`hk-gv04`/`hk-jay1`/`hk-8lne`) — blocked behind design keystone `hk-9fvk`; operator said "nail the model first."
- `hk-1nwt` (paul ideation placeholder), `hk-nlhys` (L14) — capture-only / no-dispatch by design.
- **L3 (remote-substrate)** & **L7/L9 (keeper)** — owned by in-flight agents; coordinate before staffing.
- `hk-77x1` — Claude Code core RC regression, NOT harmonik-fixable; tracking note only.
- `hk-tagp` / main queue — paused-by-failure; do NOT re-dispatch to main.

### RESOLVED since last reassessment (do NOT re-open)
- `hk-538l` (remote review-loop e2e `agent_ready`) — **CLOSED**, proven end-to-end (`15ca1eb3`); only the DOT-mode variant (`hk-4lrj`, in L3) remains.
- `hk-5da7` / keeper `89852bb3` restart-now fix — **CLOSED on main** (`4128d760`). The earlier "unmerged, needs ACK verify + pct-vs-band operator decision" is RESOLVED; the active keeper-polish edits are separate follow-on work.

## Active operator directives (dated)

> **EXPIRY MECHANISM (admiral-framework, 2026-06-25 — ships now, independent of any ranker lock):**
> EVERY dated directive in this file MUST carry `expires:` AND an owner. **ON EXPIRY the DEFAULT is
> LAPSE → revert to the standing autonomous posture — NEVER a hold.** The admiral audit OWNS flagging
> an expired-but-present directive and either re-confirming with the operator or striking it. An
> expired block left in place is a FINDING (this is exactly how the 2026-06-19 scale-out block lapsed
> into a silent lean-park). A dated directive with no matching `direction-log.md` entry is also a
> FINDING. See `.harmonik/context/AGENTS.md` (forced-write/forced-read) + orchestrator-rules §Autonomy.

> set: 2026-06-19 · expires: ~2026-06-22 (3-day scale-out push — re-confirm or expire after the window)
> STANDING for EVERY captain across ALL restarts within the window. These OVERRIDE any
> stale "lean park / one-at-a-time / operator away" posture in a handoff. On conflict, THESE win.
>
> NOTE (2026-06-20): a TESTING / live-validation phase is IMMINENT ("where are we at?"). Much of the
> 2026-06-20 burst shipped as code but was never exercised live. Expect the scale-out directive to
> yield to a live-validation posture when the operator opens that window (operator decision §3.1 of the
> state-reassessment plan). Until then the scale-out directives below remain in force.

- **set:2026-06-19 expires:~2026-06-22** — ONE-AT-A-TIME IS RETIRED. Run multiple lanes/crews in parallel (file-disjoint). The prior "work one item at a time" directive is NO LONGER in effect.
- **set:2026-06-19 expires:~2026-06-22** — Scale OUT across many sessions over ~3 days. Lots of context budget is available, but do NOT run too much at once — stage lanes; don't blast the whole fleet up at once.
- **set:2026-06-19 expires:~2026-06-22** — The daemon MUST dispatch EVERY bead through DOT — the SONNET triple-review graph (repo-root workflow.dot == sonnet-triple-review). NEVER the opus DOT. NEVER single/no-review mode. Verify via run_started workflow_mode (STARTUP 5c grep).
- **set:2026-06-19 expires:~2026-06-22** — Captain ORCHESTRATES; it does NOT do the work and does NOT micromanage. Allocate + direct; crews own their own tasks. Once the fleet is running, the captain is mostly QUIET — occasionally check crews; use FRESH-CONTEXT sub-agents to verify crew work.
- **set:2026-06-19 expires:~2026-06-22** — Captain ensures PROCESS, not task content: (1) double-check crew work, (2) ensure reviewers are ACTUALLY used, (3) ensure work is ACTUALLY TESTED before integration. Set a ~30-min check-in loop; do not babysit between ticks.
- **set:2026-06-19 expires:~2026-06-22** — A DAEMON issue takes PRECEDENCE over everything else.
- **set:2026-06-19 expires:~2026-06-22** — EVERY session (captain + crews + flywheel + watchdog) MUST stay under 300k tokens of context. The built-in keeper has reliability issues. Run an INDEPENDENT Sonnet context-watchdog: a 30-min loop that idles between ticks, checks every session's context, and FORCE-restarts any that exceed the cap.
- **set:2026-06-19 expires:~2026-06-22** — Internet is FLAKY. The agent<->Anthropic API loop works, but other internet calls (WebFetch, gh, package downloads, SSH to the remote box) may fail. Propagate this caveat to EVERY crew, especially remote-substrate.

> Staffing order is in the dispatch table above (START FIRST → L1, runner-up L2). The remaining
> live-validation gaps (remote-node telemetry live-run, fleet auto-sleep round-trip, easy-start
> integration tests) are folded into lanes L3/L4/L7. The "remote-substrate live-validation first"
> ordering is RELAXED now that `hk-538l` is closed — L1 daemon-reliability leads.

## Operational caveats (still live)

- **STRANDED in_progress/open, pending `hk-53p3`** (do NOT raw `br close` — would reverse the locked beads-own-transitions decision): `hk-gu3v` (fix on main, in_progress) + `hk-nlio` (prior promote-salvage, open). Both auto-close once `hk-53p3` lands and `harmonik reconcile` runs.
- `hk-rty1` (P1): stranded in_progress one-liner (default→triple-review); needs split/reset to unstick.

## paused

- **codex (hk-0639)** — ⚠️ UNPAUSED 2026-06-24 (admiral/operator). LOCAL re-canary first (hk-n05u2 via leto), report before any soak. Codex bills ChatGPT, LOCAL-only.
- **gh-bugs** — only do beads that ALREADY EXIST and do NOT need GitHub (no gh access / flaky internet).
