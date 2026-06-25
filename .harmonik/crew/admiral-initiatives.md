# Admiral — Major-Initiatives Registry

> **Admiral-owned.** The complete set of major initiatives + their status. This is the
> admiral's oversight anchor: every audit reconciles it against ground truth (captain-lanes.md
> tier-2 + `kerf next` + comms). It complements captain-lanes.md (which tracks *which crew is on
> which lane right now*); THIS file tracks *all the big rocks + which are active / on-deck / parked*.
>
> **Maintenance:** the admiral updates this each audit when an initiative's status changes (a new
> one appears in comms/operator asks, an active one lands, an on-deck one is staffed). Re-read on
> every admiral restart. Keep it SHORT — one line per initiative + status; detail lives in the
> bead/epic/plan it points at.
>
> **Status vocabulary:** ACTIVE (a crew/queue is working it now) · ON-DECK (next to staff, no
> blocker) · PARKED (**zero ready beads right now — a FACT, not a hold**; a parked KNOWN lane is
> self-authorizable to resume the instant ready work appears) · GATED (**held by a NAMED, DATED,
> OWNED, EXPIRING gate** — mirrors `lanes.json.gate`; absence of a live named gate means
> KNOWN/resumable, see orchestrator-rules §Autonomy) · DONE (landed; kept briefly for context).
>
> Last reconciled: 2026-06-25 ~06:24Z (admiral). Fleet HEALTHY + ACTIVE — gap-time stall
> self-healed; the remote test-hardening program is filed and dispatching (see TOP/ACTIVE).

## TOP / ACTIVE (being worked now)

| Initiative (plain English) | Codename / key beads | Status | Notes |
|---|---|---|---|
| **Remote test-hardening program** — make the remote substrate thoroughly + cheaply testable WITHOUT real remote servers. Full test pyramid L0–L5 + impasse-plan moves ①②③ (skip ④). **#1 PRIORITY** (operator 2026-06-25, runs autonomously overnight) | kerf work `remote-test-pyramid` · epic **hk-6l941** · L0 **hk-hd2w6** (ready) → L1 **hk-52xnr** (blocked-by L0) on **gurney-q** LOCAL · plan: `designs/remote-test-strategy-plan.md` | **ACTIVE — dispatching** | Fleet REPOINTED LOCAL (gb-mbp disabled, daemon restarted, concurrency=4). FILED 06:23Z; **gurney re-tasked + adopting L0**. Seam-map found 3 more bare verdict reads (`dot_gate.go:551/686` + `autostatusmarker.go:70`) = unfixed remainder of the hk-f3u6o class → folded into L0. Order: L0 runner-seam contract + L1 separated-FS twin FIRST → L2 isolated-ssh → L4 fault/chaos → L5 scheduler-props (move ② per-queue routing) → **L3 LAST = DOCKER containers (NOT Lima VMs, operator 06:10Z) + Linux-remote support** (OrbStack daemon currently DOWN — start before L3; Linux-target + concurrent/multi-remote scheduler are real design gaps → captain to scope an L3 research kerf-work). Captain owns kerf planning+dispatch (local queue); admiral oversees + hourly unstick-watchdog. Operating rules: small bug fixes done DIRECT out-of-daemon, sub-agents OK w/ review, review=multi-agent consensus NOT human signoff. |
| **Wake-economy / token-optimization** — cut the captain's own wake + token burn; token-opt is the **#1 standing priority** (2026-06-23 operator directive, RE-CONFIRMED 2026-06-25; captain's own burn ranks highest) | `codename:wake-economy` · hk-var9b / hk-8yh32 / WE10 / hk-we-soak1/2 | **ACTIVE — but UNSTAFFED (at-risk)** | MVP all 7 WE beads CLOSED, watch-tier cutover LIVE. Soak/polish follow-on (hk-we-soak1/2, hk-we10, hk-8yh32) is ready + NOT staffed — paul (the crew) is DOWN. Directed captain 3× to re-staff (01:11/01:55/02:05). Escalate to operator if still idle next audit. |
| **Remote-worker validation** — distribute bead-work to the gb-mbp macOS SSH box; prove one full triple-reviewed run lands over the tunnel | hk-h106 (hostname proof) · hk-4lrj (triple-review capstone) · hk-f3u6o (reviewer consistency) · hk-t1t00 (durable HK_GATE_BASE_SHA) | **ACTIVE — last-mile** | priority #2. Routing/launch/implement/commit_gate ALL PROVEN green on gb-mbp. Gate-loop root cause was STALE worker origin/main (NOT cold cache) inflating the affected-set → fixed. ONLY remaining: remote-reviewer verdict consistency (run 3 hit reviewer agent_ready_timeout). Crew **gurney** (epic hk-gx0dl, queue gurney-remote) owns the last-mile: ROOT-CAUSED + reproduced the reviewer bug (daemon reads verdict from its LOCAL path; reviewer wrote it on gb-mbp). **hk-f3u6o LANDED on main 5999a39a (2026-06-25 ~05:30Z, OUT-OF-DAEMON per operator)** — isolated implementer mirrored gurney's diagnosis (runner-routed verdict + budget-sentinel reads, nil→local fallback) + fixed 3 sibling read sites (builtin review-loop + APPROVE merge-trailer); built, full daemon suite green, independently reviewed+APPROVED, fast-forward push. Bead OPEN (no daemon auto-close) → captain to reconcile/close on resume. **hk-t1t00 = HOLD/needs-rework** (premise OK in shell-script terms but a separate fix; deferred pending operator strategy decision). FLEET HELD pending operator remote-test-strategy decision. |

## ON-DECK (next to staff, no blocker)

| Initiative (plain English) | Codename / key beads | Status | Notes |
|---|---|---|---|
| **Codex-on-remote** — run the Codex implementer harness on gb-mbp (not just Claude); verify e2e launch + ChatGPT billing over the tunnel | (no bead yet — captain creates) | **ON-DECK** | Operator-requested this session. Sequenced AFTER the Claude remote ladder lands. Prereq: confirm ChatGPT/codex auth exists on gb-mbp. Watch reviewer-pin footgun hk-2jxqg. Lives only in comms until the bead exists. |
| **Codex-vetting (local)** — dedicated crew, DELIBERATELY serialized one-bead-at-a-time, quality-assess how Codex performs on THIS repo | epic hk-0639 (soak) · fed by hk-fgy9o (test-uplift) · local re-canary hk-n05u2 first | **ON-DECK** | Operator-requested this session. Lives only in comms until staffed. |
| **Daemon-reliability** — kill the false-positives/strands that corrupt the orchestration signal | epic hk-sfvc · hk-7xgu4 (iter-2 implementer thrash, burns 60-90min slots + blocks queue) · hk-u5tgh (restart leaves crews keeper-less) | **ON-DECK** | Lane 4. file-disjoint: internal/daemon. Highest fleet leverage. |
| **Watchdog session rename** — rename the mislabeled "flywheel" tmux session (it's the daemon-revive watchdog) to `-supervisor` | (captain scoping bead) | **ON-DECK** | Directed 2026-06-25. Load-bearing contract string (reaper matcher + spawn-exclusion move in lockstep). |
| **Standing test-daemon harness** — a separate worktree/clone running an ISOLATED test daemon pinned to the remote: run a test against it, submit issues to the main daemon. = MOVE ① (scratch-clone), NOT the skipped move ④ (two daemons on the SAME repo dir). The fast-loop unblock that makes remote bugs cheap to iterate | move ① in `designs/remote-iteration-impasse-plan.md` + `remote-test-strategy-plan.md` · captain scoping kerf-work + bead set | **ELEVATED → BUILD** (operator 2026-06-25 ~21:29Z, via admiral; directive comms 019f00af) | Operator clarified the long-misread "two daemons" idea = THIS (isolated scratch-clone test daemon), promote from scope-only side-quest to BUILT standing harness in the remote lane; accelerates the last-mile, does not compete. Staff gurney or a dedicated scratch-clone crew (restart authority on the SCRATCH daemon only). |

## GATED / WAIT (held by a NAMED, DATED, OWNED, EXPIRING gate — mirrors `lanes.json.gate`)

> "PARKED" (zero ready beads now) is a FACT and is NOT listed here — a parked KNOWN lane is
> self-authorizable to resume. ONLY a lane held by a named/dated/owned/expiring gate belongs in
> this section. On gate expiry the DEFAULT is LAPSE → revert to autonomous; the admiral audit
> owns re-confirming or striking an expired gate.

| Initiative (plain English) | Codename / plan | Status | Gate (owner · reason · expires) |
|---|---|---|---|
| **Pi universal-model-gateway** — universal model/provider gateway harness | `plans/2026-06-23-pi-openrouter-harness/` | **GATED** | operator · not before remote-worker proven reliable · expires 2026-07-09 |
| **De-hardcode-messages** — remove hardcoded message strings | — | **PARKED** | Bundled with Pi; same gate. |
| **Hot-reconfigure + concurrent/multi-worker dispatch** — change daemon remote/local config WITHOUT restart, run local + remote AT THE SAME TIME, and (phase 2) multiple remotes | routing LANDED: hk-f10xl (`Queue.LocalOnly`/`WorkerTarget`) · live-toggle: hk-xjbvi (OPEN, daemon-reliability) · multi-remote: research (`plans/2026-06-20-remote-node-telemetry-autoscale/phase2/`) | **ELEVATED into ACTIVE remote lane** (operator 2026-06-25 ~21:29Z, via admiral; directive comms 019f00af) | 3 parts: (1) **concurrent local+remote routing = LANDED today (hk-f10xl)** — per-queue `local_only`/`worker_target` gate the single `SelectWorker` call (workloop.go:2928-2936); one local queue + one remote queue dispatch concurrently. REMAINING = a LIVE e2e proof + the (2) live worker on/off toggle hk-xjbvi (OPEN, restart-only today). (3) multi-remote N-worker = still V1 single-worker (`ErrTooManyWorkers`); deferred. Captain scoping the live-validation + toggle into the remote lane. |

## DONE recently (context only — verify with `br show` if regression suspected)

- **leanfleet** (`hk-itoc`) — token-efficiency campaign. CLOSED.
- **Codex local soak** (`hk-0639`) — harness proven 5/5 e2e under load, ChatGPT-billed. Epic still OPEN by open-ended-soak charter (operator's close-or-keep call).

## Operator-pending decisions (admiral surfaces; operator settles)

- **hk-4u1mb** (reviewer diff-budget) — conflicts with shipped hk-sj6a heartbeat contract; captain recommends progress-aware discriminator. Operator leaning DEFER.
- **Governor threshold** (`liveness_no_progress_n`) = 10 (observe/emit-only). Operator's policy call; 10 stands unless operator says 0.
- **Close hk-0639 soak epic?** Functionally done; OPEN by charter. Operator's close-or-keep call.
