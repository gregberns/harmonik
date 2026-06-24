<!-- TIER: 2 (operational state, days cadence)
     LOADED BY: captain @ STARTUP Step 0b; NOT loaded by crews or implementers
     OWNER: captain, updated at session end (before HANDOFF.md) or on any crew/epic change
     DO NOT PUT HERE: standing behavioral rules (→ orchestrator-rules skill);
                      this-session salvage / run-id play-by-play (→ HANDOFF.md tier-1);
                      durable phase/locked decisions (→ project.yaml tier-3) -->

# Tier-2 context: captain lane registry + medium-term tracker (days cadence)
# Captain reads on every boot (STARTUP.md Step 0b) BEFORE re-deriving lanes.
# Stable across /clear cycles; verify every claim against live ground-truth at Step 2.

## active_lanes  (as of 2026-06-24 — 4-lane fleet, concurrency=4 per operator directive)

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

| crew | epic_id / scope | lane (plain English) | queue | model |
|---|---|---|---|---|
| paul | hk-var9b / codename:wake-economy | captain wake-economy + watch-officer tier — DESIGN as kerf work (spec-first). Design draft DONE 2026-06-24; critic review dispatched (operator hard-req critics-before-build). Net-new = a Sonnet watch-officer SESSION (CE4 already offloaded the deterministic checks). Await critic verdicts → build-or-revise. hk-drygf HELD (awaiting operator) | paul-logmine | opus |
| jamis | hk-98jju / codename:supervisor-revive | scenario-harness DONE 15/15 → re-tasked: mute supervisor-down page (hk-xr46t) FIRST, then investigate supervise-dies-on-launch + daemon-auto-revive (hk-f2j0o) + flywheel keep-vs-remove (hk-zv6j3, hk-drygf folded/HELD). Adopts on G-15 land | jamis-sh | — |
| gurney | hk-b89kk + hk-z8fp | cmd/harmonik tools: `harmonik usage` verb + Manifest.Digest map-order flaky fix | gurney-cmd | sonnet (escalate-on-fail) |
| leto | epic hk-0639 codename:codex | codex re-canary PASSED 2026-06-24 (hk-n05u2 e2e GREEN through production DOT; ef64h validated) → re-tasked to the codex SOAK (hk-0639). PROVEN RECIPE (load-bearing): select codex per-node via tier-3 DOT node attr (implement node harness=codex) + reviewer_harness=claude-code, NOT the harness:codex bead label (that forces the reviewer to codex → no verdict; durable fix filed hk-2jxqg P2). ChatGPT-billing guard ON every run | leto-ev | opus |

- **Fleet = daemon + captain + 4 work-crews + admiral (operator-engaged) + ctx-watchdog + ops-monitor.**
- All 4 lanes are file-disjoint: paul=`internal/daemon/*`, jamis=`scripts/ops-monitor` + supervisor subsystem, gurney=`cmd/harmonik/*`, leto=codex harness (LOCAL run). HARD GUARD: jamis must not edit paul's daemon hold files.

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
