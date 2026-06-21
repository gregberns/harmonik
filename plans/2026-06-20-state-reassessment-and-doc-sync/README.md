# State Reassessment & Doc Sync — 2026-06-20

**Why this exists.** In the last ~18 hours the fleet landed **135 commits across nine initiatives** — the biggest single-day burst in the project. The tracking docs (STATUS, ROADMAP, captain-lanes) fell behind, and the kerf store drifted (62 untriaged beads, a whole new fleet-state initiative unbound). We're about to enter a **testing phase** ("where are we at?"), so we need one authoritative reconciliation: what landed, what we wanted, where the gap is, and what must be live-validated before we trust any of it.

This plan is the captain-perspective synthesis. The doc updates it drives reconcile the three-kinds tracking model produced by the doc-instruction-audit (ROADMAP = long-term state, captain-lanes = medium-term lanes, STATUS = long-term behavioral, HANDOFF = session).

---

## 1. What landed today (by initiative)

| # | Initiative (plain English) | Codename / Epic | Status |
|---|---|---|---|
| 1 | **Keeper redesign** — per-project config, zero hardcoded thresholds, actionable-warn self-restart handshake, hold/release co-working override, configurable hard-ceiling backstop, durable tmux↔session-id identity | `hk-gffc` + keeper config work | ✅ COMPLETE |
| 2 | **Captain economy** — slim captain boot (~81k→~55-60k tokens), Sonnet ops-monitor offload, comms `--wake` fix, per-crew `--model` | `hk-unjy` | ✅ COMPLETE |
| 3 | **Doc & instruction audit** — three-kinds model (docs / behavioral-contracts=skills incl. new `orchestrator-rules` skill / operational-state tiers), AGENTS.md→router, new `harmonik sync-assets` + supervisor skew-notify | `hk-vk7b` | ✅ COMPLETE (1 deferred follow-up `hk-fozq`) |
| 4 | **Easy-start launchers** — native Go `harmonik start captain\|crew <name>`; bash `captain-launch.sh` retired; `captain respawn` self-heal; shared `agentlaunch` helper | `hk-kbjl`/`hk-bcd0`/`hk-sn4n`/`hk-z1rj` | ✅ SHIPPED (core) |
| 5 | **Tmux session organization** — unified `harmonik-<hash>-*` namespace, agent+keeper window-nesting, window-granular restart, `supervise reap` | `hk-0v9e` | ✅ COMPLETE |
| 6 | **Remote-control session prefix** — per-project RC session-label prefix (`hk-captain`) | `bf7d51f8` | ✅ CORE LANDED (4 tail beads open) |
| 7 | **Fleet sleep/wake (fleet-state Phase 0)** — sleep markers w/ source+level, IsSleeping fail-closed, live wake-pane resolution, boot reconcile of orphaned markers, ctx-watchdog skips parked sessions | `codename:fleet-state` | ✅ PHASE 0 LANDED |
| 8 | **Remote-node telemetry** — worker-report resource snapshots + problem flags (P1, WR1-5); live resource-breach detection (P2, PB1-4) | `worker-report`/`worker-breach` | ✅ P1+P2 COMPLETE (off-by-default, never live-run) |
| 9 | **Remote-substrate e2e** — SSHRunner quote fix, substrate-runner threading, agent_ready TCP loopback, SSH-direct branch fetch; e2e proof committed on gb-mbp | `hk-620j`/`hk-7bwx`/`hk-538l` | 🟡 PROOF ON MAIN, NOT LIVE-VALIDATED |

---

## 2. What we wanted vs. what we got — the gap

The nine initiatives above were the *intended* scope. Here is what remains open, deferred, or unvalidated — ordered by what blocks the testing phase.

### A. Must close before we can "test where we're at" (live validation gaps)
These shipped as code but were **never exercised live**. Testing the system means turning these on.

- **Remote-substrate e2e is unproven live.** The proof commits are on main but the path was never run end-to-end against a real worker under the daemon. Open: `hk-538l` (DOT remote run: `agent_ready` never received — P1 bug), `hk-4lrj` (in-progress validation), 6 concurrent worktree-create proofs (`hk-icdz`/`3zij`/`d2z1`/`tzfw`/`xbpm`/`k0pz`), `hk-tyyy` (auto-scp worker binary).
- **Remote-node telemetry is off-by-default, never live-run.** Worker-report + breach detection have zero live data. The Phase-3 autoscale pickup checklist (`hk-e6gs`) explicitly gates on "live Phase-1/2 validation vs. a real worker."
- **Fleet auto-sleep not live-confirmed.** Phase 0 daemon batch landed (and closed the 3 safety gaps flagged in the sleep-wake critique — wake-pane via `hk-fv40`, orphan-marker reconcile via `hk-x03v`, ctx-watchdog sleep-guard via `hk-jxcx`). But the auto-park→wake round-trip with a real session name has not been observed.
- **Easy-start native launchers — integration paths untested.** ~70% is unit-covered; tmux-takes-effect + live `/clear`-injection are integration-only and unverified on the new native path.
- **Keeper `89852bb3` fix is unmerged.** Reviewer-approved, smoke-tested, −542 net lines (restart-now direct path + ACK handshake). Needs a fresh-context re-read → `go install` → live `[KEEPER ACK]` verify → merge. Captured in `hk-5da7`.

### B. Deferred by design (parked, not lost)
- **Remote-node telemetry Phase 3 (autoscale)** — operator decision D1; AIMD autoscaler waits on live load-history. Pickup checklist `hk-e6gs`.
- **Fleet-state Phase 2 (`harmonik state` fold + system-state spec)** — `specs/system-state.md` is DRAFT. Open beads `hk-pfr4` (GatherDrainFacts), `hk-8lne`/`hk-9fvk` (spec), `hk-up4b` (fleet state model, supersedes the old sleep/wake `hk-rl4b`).
- **Flywheel v1** — 9 components on main, 0 daemon callers; ~28 completion beads open, keystone `hk-y9fn` (FW1 config adapter). Dispatch was blocked on remote-worker routing.
- **Codex harness soak** — PAUSED by operator 2026-06-19 (`hk-0639`).

### C. Bugs surfaced by log-mining (top of the kerf feed — P1/P2)
- `hk-53p3` (P1) — `harmonik promote` push-mode strands the salvaged bead in_progress (cherry-pick lacks Bead-ID trailer + isn't a merge commit → reconcile never closes it).
- `hk-gu3v` (P1) — ops-monitor crew-stale false positive: staleness fires while the agent is actively posting (`comms send` doesn't refresh presence).
- `hk-ijtw` (P2) — ops-monitor review-gate false positive (run_failed that emitted reviewer_launched trips review-bypass forever).
- `hk-ldzp` (P2) — daemon worktree/disk GC: ~28 stale worktrees, low free space.
- Plus loose daemon-reliability beads: `hk-sj6a` (DOT review-phase freeze, no self-recovery), `hk-g9zz` (rebase aborts on dirty integration-test worktree), `hk-z8fp` (flaky Manifest.Digest).

### D. Tail/cleanup
- **rc-prefix tail** (4 beads): `hk-dhe6` epic, `hk-w8ex` (CLI parity test), `hk-25bg` (live two-project validation), `hk-f4w7` (migration prompt).
- **kerf store drift**: 62 untriaged beads + ~396 changed externally; a whole fleet-state work unbound. Needs a binding pass + `kerf triage --ack`.
- **Doc-audit follow-ups**: `hk-fozq` (P3 supervisor auto-apply), retention-policy pruning (P2), P0 API-key hygiene in `*.env.txt`.

---

## 3. Operator decisions surfaced (genuine, only you can make)

1. **Testing posture** — do we declare a live-validation window now (turn on remote-substrate + telemetry + auto-sleep against a real worker), or keep scaling out features first? This plan assumes *testing next*.
2. **Keeper `89852bb3` merge** — go install + live ACK verify, then merge? And pct-vs-band: pin 200k/215k as the commit does, or graft the killed agent's clamp-to-tighten `derivePctThreshold`?
3. **P0 security** — `*.env.txt` holds a plaintext API key (locally-ignored, not in `.gitignore`). Rotate + delete + gitignore.

Everything else is captain-autonomous: doc-sync, kerf binding pass, dispatching the log-mine P1 bug fixes.

---

## 4. Doc-sync actions (this plan drives these)

Using the three-kinds tracking model:

- **ROADMAP.md** (long-term state) — fix captain-economy → ✅ COMPLETE; add fleet-state Phase 0, easy-start, rc-prefix, tmux-org, doc-audit as landed; mark remote-substrate e2e as proof-on-main/not-live-validated.
- **captain-lanes.md** (medium-term lanes) — re-derive the epics table: move keeper-redesign + captain-economy + tmux-org + doc-audit to a CLOSED section; correct fleet-state from "⛔ not built" to "Phase 0 landed"; correct remote-substrate from "not staffed" to "telemetry P1+P2 done, e2e validation pending"; refresh the operator-directive window.
- **STATUS.md** (long-term behavioral) — bump date to 2026-06-20; add a "Recently completed (2026-06-20 burst)" block listing the nine initiatives; keep locked decisions intact.
- **AGENT_INDEX.md** (reference map) — add the now-load-bearing `orchestrator-rules` skill; note the new initiatives' plan dirs.
- **kerf store** — binding pass for the unbound fleet-state / leanfleet / remote-substrate works, then `kerf triage --ack`.

---

## 5. Recommended next moves (post-doc-sync)

1. Dispatch the three log-mine P1 bugs (`hk-53p3`, `hk-gu3v`, `hk-ijtw`) — they corrupt our own tracking signals and will pollute the testing phase.
2. Stand up the remote-substrate live-validation lane (`hk-538l` first — the `agent_ready` blocker gates everything downstream).
3. Surface the three operator decisions in §3.
