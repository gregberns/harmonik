# 11 — Keeper Test-Validation System — Design

> Scope: a test-validation system for the **keeper** — the per-session context-fill watcher that drives
> `handoff → /clear → /session-resume` before a long-lived Claude pane overflows. Grounded in the live
> code under `internal/keeper/` + `cmd/harmonik/keeper_*`, the operating contract in
> `.claude/skills/keeper/SKILL.md`, the bug corpus (`02-bug-corpus-classification.md`), the shared
> Phase-2 spine (`06-phase2-plan.md`), and the running defect log `BUGS.md` (B3, B4).

## TL;DR for the reader

**The keeper is already the single most-tested subsystem in the tree** — ~50 test files under
`internal/keeper/`, spanning pure-unit (thresholds/gates/sid/nonce/hold/tmuxresolve/awaitack),
an in-process **reactive harness** (`cycle_scenario_reactive_*`, `cycle_reactive_harness`), and a
**real-tmux session-twin** integration layer (`cmd/harmonik-twin-session` + `cycle_twin_e2e_integration`).
This design is therefore **not "build a harness from scrat"** — the keeper-specific harness *exists*. It is:
(1) an audit that names the harness seams already in place; (2) a gap list tied to the **real incidents
that still bit us this month** (B3 watch-restall, B4 `restart-now no_tmux_target`, binary-upgrade
migration); (3) the cheapest faithful test for each gap; (4) a **hard verdict that the keeper test-system
is buildable fully in parallel NOW** — it depends on the keeper-local twin, **not** on the shared Phase-2
daemon substrate.

---

## 1. Problem space — what actually breaks in the keeper

Grouped by failure class, each with the real incident(s). Citations are file:concept.

### G1 — Band / threshold math
The keeper fires on `min(absTokens, pctCeil·windowSize)` (`internal/keeper/thresholds.go`,
`cycle.go:39-43`). Historic breakage:
- **`90%` gate only tripping at ~900k on a 1M window** — the reason the `min(abs, pct·window)` formula
  exists (hk-cl74g). On `[1m]` models the abs cap must win; pct flags are inert and must *warn if passed*
  (hk-odhh, `keeper_inert_warning_hkcu7g_test.go`).
- **Band retune drift** — warn/act were re-tuned to 200k/215k (hk-8hr1/M1); the operator HARD-NO on
  widening the band and on baked-in runtime defaults (every value operator-required or the keeper *refuses
  to start*, `resolve_keeper_config.go`). Memory: *NO hardcoded keeper thresholds*, *NO band retune*.
- **Force-act (240k) must bypass CrispIdle; hard-ceiling (280k) must be SID-independent** (`thresholds.go:72`,
  hk-0uu, hk-34ac).

### G2 — Restart-now cycle: tmux-target resolution + session-id rebind  ← **live pain (B4)**
The `restart-now` in-process path resolves the pane, verifies sid, freshness-checks the handoff, injects
ACK → `/clear` → `/session-resume` (`internal/keeper/restartnow.go`). Real breakage:
- **B4 / hk-pp1in: `restart-now` aborts `no_tmux_target` despite a healthy pane-bound watcher.** The
  RunOnDemand core (`restartnow.go:83-86`) aborts when `cfg.TmuxTarget==""`. The **resolution seam** that
  fills that field lives one layer up (`cmd/harmonik/keeper_restart_now_*` → `tmuxresolve.go`), and it
  produced empty even though a live watcher was already bound to a real pane. This stranded the admiral
  this session (BUGS.md B4: "the keeper must have fucked up" → manual `harmonik agent brief` reboot).
- **tmux `:a`-style target mangling (hk-5266t class)** — a mis-formed `session:window` target silently
  fails to inject; the `:agent` convention derivation (`tmuxresolve.go`) must be exercised against the
  real live-session naming, not a hand-passed literal.
- **session_id flips on `/clear` (memory: `keeper_sessionid_flips_on_clear`)** — the resume must rebind the
  SAME conversation; the sid file is re-minted each cycle (`sessionid_test.go`), and the anti-loop gate
  keys off `lastFiredSID` (`export_test.go:SetCyclerLastFiredSID`).

### G3 — Nonce-confirmed-handoff gate (the "never `/clear` without a confirmed nonce" invariant)
The cycle truncates the handoff, injects `/session-handoff`, polls for the `<!-- KEEPER:<nonce> -->`
marker, and ONLY then `/clear`s (`cycle.go`, `docs/captain-restart.md`). Real breakage:
- **hk-vpnp / Bug 3: the auto ACT cycle LOOPED and truncated the handoff to 0 lines** — it truncated
  BEFORE nonce confirmation, timed out, aborted, re-armed, and truncated again, destroying on-disk state
  (`act_loop_hkvpnp_test.go`). Fixed contract: a non-empty handoff that fails to confirm is NOT truncated;
  no second nonce re-fires on the same un-cleared session.
- **restart-now nonce mismatch must abort** (`restartnow.go` freshness + nonce check; hk-uldg made the
  MANUAL path verifiable via `await-ack`, but explicitly left the AUTO cycle's own await-ack OUT OF SCOPE →
  companion hk-vpnp owns that seam).

### G4 — Hold / release co-working override
`hold` suspends ACT while WARN still fires (hk-9waz). Invariants: session-id-keyed marker
(`.hold.<sessionID>`) so it **can never survive a restart**; 45m TTL backstop; hard-ceiling overrides a
hold; an **older binary silently ignores the marker** (`keeper_hold_test.go`, SKILL §hold). Memory:
`project_keeper_hold_landed`.

### G5 — Doctor / live-watcher liveness + binary-upgrade migration
- **Gauge writer and watcher are decoupled** — a fresh `.ctx` does NOT mean a keeper is alive; only the
  `live-watcher` flock probe (`LiveKeeperPresent`, `live_keeper_present_hkx7s_test.go`) proves it.
- **binary-upgrade required-keys landmine (memory `keeper_binary_upgrade_required_keys_landmine`)** — a new
  binary added operator-required config keys; an in-place daemon swap left the keeper **refusing to start**
  on the next restart because the deployed `config.yaml` lacked the new keys. `keeper config --example`
  must always emit a COMPLETE block (`keeper_config_example.go`, `resolve_keeper_required_test.go`).
- **statusLine stanza must carry `"type":"command"`** or Claude Code rejects the whole settings.json and
  disables ALL hooks (hk-hs1, `keeper_enable_doctor_cmd.go:610`).

### G6 — Live-pane recovery + the auto-recover gap  ← **live pain (B3)**
- **B3: `watch` re-stalls every ~5–25 min with NO self-healing path.** Root cause historically = keeper
  can't inject a restart into a mangled tmux target (G2 / hk-5266t) **plus no auto-recover hook**, so every
  recovery costs a manual captain restart — inverting the watch's purpose (BUGS.md B3).
- The gauge-independent **live-pane ForceRestart via `--respawn-cmd`** exists (`respawn.go`,
  `watcher_live_pane_recover_test.go`, hk-75mr): stale-gauge + pane-alive + not-operator-attached +
  cooldown + valid-UUID-sid → run the operator respawn command. **But the B3 loop shows this path is not
  wired for the `watch` session / not proven end-to-end for the re-stall class.**
- **`no_gauge:stale` flood** was the dominant failure mass (~2699 events, `heartbeat.go:38`) — the
  heartbeat/suppression seam (`watcher.go:SuppressNoGauge`, hk-F21) is the anti-noise fix; a regression
  here re-drowns real alerts.
- Related: smoke fork-bomb (memory `keeper_smoke_forkbomb`), watch-restart loop survives `/clear`
  (memory `watch_loops_survive_clear`), restart-now needs a tmux target (memory).

---

## 2. What to test, and at what layer (cheapest faithful test per class)

Three layers already exist in-tree — use them; do not invent a fourth:

- **L-unit** — pure Go, table tests. No tmux, no daemon.
- **L-fake-tmux** — the `tmuxRunFn` package-var seam (`injector.go:104`) + `InjectFn`/`ReadCtxFn`/gate-fn
  seams on `CyclerConfig` (`export_test.go`) + the in-process **reactiveSession** fake
  (`cycle_reactive_harness_test.go`) that closes the causal loop (gauge reacts to injected `/clear`).
- **L-twin** — the **real-tmux session-twin**: `cmd/harmonik-twin-session` driven through REAL
  `keeper-statusline.sh` → `.ctx` → real `InjectText` (tmux load-buffer/paste/send-keys) → real
  `Cycler.MaybeRun`. Only the wall-clock and the LLM are faked (`cycle_twin_e2e_integration_test.go`,
  build-tag `integration`).

| Class | Cheapest faithful layer | Concretely |
|---|---|---|
| **G1 band math** | **L-unit** | `min(abs,pct·window)` across 200k & 1M windows; force-act bypasses CrispIdle; hard-ceiling SID-independent; pct-flag-inert-warns-on-1m. (Mostly covered — `thresholds_test`, `watcher_1m_warn_*`, `keeper_inert_warning_*`; add a 1M-vs-200k matrix guard.) |
| **G2 tmux resolve + sid rebind** | **L-fake-tmux for resolution; L-twin for rebind** | The **B4 gap** is a *resolution* test: stand up a live-named tmux session (or fake `tmuxRunFn` returning the real `list-sessions` shape), assert `ResolveTmuxTarget` returns the pane and RunOnDemand does NOT abort `no_tmux_target`. sid-rebind (same conversation survives `/clear`) is an L-twin assertion. |
| **G3 nonce gate** | **L-fake-tmux (reactive)** | Real on-disk handoff I/O + spy injector (`act_loop_hkvpnp` / `realHandoffCycler` pattern): non-empty handoff never truncated pre-confirm; no double-nonce re-fire; nonce-mismatch aborts before `/clear`. Full covered — keep as regression floor. |
| **G4 hold/release** | **L-unit + L-fake-tmux** | session-id-keyed marker dies on restart; 45m TTL; hard-ceiling overrides hold; WARN still fires under hold; **older-binary-ignores-hold** as a version-gated assertion. (`keeper_hold_test` covers most; add the hard-ceiling-overrides-hold case explicitly.) |
| **G5 doctor + migration** | **L-unit (cmd-level)** | `config --example` emits every required key (round-trip: example → resolve → no missing-key error); `live-watcher` flock probe distinguishes live vs corpse lock; statusLine `"type":"command"` normalization. (`resolve_keeper_required_test`, `live_keeper_present_hkx7s_test`, `keeper_enable_doctor_cmd_test` cover much; add a **binary-upgrade migration** test: old config.yaml + new required key → refuse-to-start with the full aggregated missing-key list, then `example`-merge → starts.) |
| **G6 auto-recover / B3** | **L-fake-tmux for the gate; L-twin for the loop** | The B3 fix is a **new** deterministic scenario: stale gauge over an ALIVE pane whose target was *mangled* → the recover path must (a) re-resolve the target, (b) fire the gated ForceRestart, (c) NOT loop. Gate logic = L-fake-tmux (`IsPaneAlive`/`IsPaneIdle` fns are already injectable). The end-to-end "watch re-stalls, keeper auto-heals without a human" proof = **L-twin** with a twin scripted to wedge (stop emitting gauge) on cue. |

**Which need the SHARED Phase-2 substrate?** — **None of the core keeper classes.** Every keeper failure
is a *pane × gauge-file × marker-file × tmux* interaction, all faithfully reproducible with the
keeper-local twin + fake-tmux seam that already ships. The shared scratch-daemon/Docker substrate
(`06-phase2-plan.md` chunks 2/3) is about the **task-processing loop** (harness→model→provider→commit) —
a different surface. The **one** place they touch: a crew keeper is auto-armed by the daemon
(`HandleCrewStart → SpawnCrewSession`, hk-rmy1); proving *crew-restart re-hydration end-to-end* (crew
`/clear`s, resumes same name, re-drains its queue) would reuse the scratch-daemon. That single scenario is
the only substrate-gated keeper item; everything else is parallelizable now.

---

## 3. Acceptance corpus — top 6 scenarios the keeper test-system MUST assert green

Each is a real incident, expressed as a deterministic green/red the harness reproduces at **zero Claude
tokens**.

1. **`restart-now` with a healthy pane-bound watcher does NOT abort `no_tmux_target`** (B4 / hk-pp1in).
   A live watcher is bound to `harmonik-<hash>-<agent>:agent`; `restart-now` resolves that pane and drives
   ACK→`/clear`→resume. Red = the historical abort. **Layer: L-fake-tmux resolution (+ L-twin for the full
   drive).**

2. **`session_id` survives a `/clear` cycle and the resume rebinds the SAME conversation**
   (`keeper_sessionid_flips_on_clear`). After `/session-resume <agent>`, the `.sid` is re-minted but the
   agent wakes as the same lane; the anti-loop gate (`lastFiredSID`) does not immediately re-fire.
   **Layer: L-twin.**

3. **A handoff that fails nonce-confirmation is NOT truncated to 0 and the cycle does not loop**
   (hk-vpnp / Bug 3). Non-empty handoff + a `/session-handoff` whose `/clear` never lands → on-disk handoff
   survives; no second nonce fires on the un-cleared session. **Layer: L-fake-tmux reactive
   (`realHandoffCycler`). Already green — lock it as a floor.**

4. **Watch re-stall auto-heals without a human** (B3). Gauge goes stale over an ALIVE pane whose target
   needs re-resolution → gated ForceRestart via `--respawn-cmd` fires exactly once (cooldown honored,
   valid-sid required, not-operator-attached), pane comes back, no escalating alert storm. Red = today's
   "#2…#32 no self-healing path." **Layer: L-fake-tmux gate + L-twin loop.** *(Primary new build.)*

5. **A hold can never survive a restart, and hard-ceiling overrides a hold** (hk-9waz). A
   `.hold.<sessionID>` from a prior session is dead after `/clear`; a held session at ≥280k is
   force-restarted anyway; WARN still fires while held. **Layer: L-unit + L-fake-tmux.**

6. **Binary-upgrade migration: a new required config key makes the keeper refuse-to-start with a complete
   aggregated missing-key error, and `keeper config --example` restores it** (binary-upgrade required-keys
   landmine). Old `config.yaml` + new binary → one error listing every missing key; example-block merge →
   clean start. **Layer: L-unit (cmd-level).**

Supporting floor (keep green, already covered): band `min` formula on 200k & 1M; force-act bypasses
CrispIdle; hard-ceiling SID-independent; pct-flags inert-warn on `[1m]`; `live-watcher` flock vs corpse;
operator-attached warn-only suppression (hk-6qf).

---

## 4. Shared-infra dependency + parallelizability — the verdict

**Verdict: the keeper test-system is BUILDABLE IN FULL, IN PARALLEL, NOW — it is NOT gated on the shared
Phase-2 substrate.** Its faithful harness (fake-`tmuxRunFn` seam + injectable `CyclerConfig` fns + the
`harmonik-twin-session` real-tmux twin) already exists in-tree and is self-contained to the
`pane × gauge × marker × tmux` surface. The daemon-dispatch lane (`core-loop-proof` / `scripted-twin` /
`scratch-substrate`) touches a disjoint surface (harness→model→provider→commit) and shares no code the
keeper tests need. A keeper crew and a dispatch crew can run concurrently with zero contention.

**The one substrate-gated item:** crew-restart *end-to-end* re-hydration (daemon auto-arms a crew keeper,
crew `/clear`s, resumes same name, re-drains its named queue) reuses `scripts/scratch-daemon.sh`. Defer
that single scenario behind `core-loop-proof`; build everything else immediately.

### How to kerf it — one epic, tranched tasks

**Epic: `codename:keeper-test-harden`** — *"Close the keeper test-validation gaps the live incidents (B3/B4)
exposed; lock the acceptance corpus as a permanent regression floor."* Integration branch
`epic/keeper-test-harden`. Build-in-own-worktree. Disjoint from the dispatch epics → captain staffs a
dedicated crew that runs concurrent with `scripted-twin`/`scratch-substrate`.

| Tranche | Task | Layer | Substrate? | Corpus item |
|---|---|---|---|---|
| **T1 (parallel now)** | **B4 fix + test:** `restart-now`/`ping` tmux-target *resolution* seam returns the live pane for a healthy watcher; RunOnDemand does not abort `no_tmux_target`. | L-fake-tmux (+ L-twin drive) | no | #1 |
| **T1** | **B3 auto-recover scenario:** stale-gauge + alive-pane + mangled-target → gated ForceRestart fires once, re-resolves target, no alert storm, no loop. | L-fake-tmux gate + L-twin | no | #4 |
| **T2** | **sid-rebind twin scenario:** same conversation survives `/clear`→resume; anti-loop gate holds. | L-twin | no | #2 |
| **T2** | **hold invariants:** session-id-keyed death-on-restart + hard-ceiling-overrides-hold + WARN-under-hold + older-binary-ignores. | L-unit + L-fake-tmux | no | #5 |
| **T2** | **binary-upgrade migration:** new required key → aggregated refuse-to-start; `config --example` round-trips complete. | L-unit (cmd) | no | #6 |
| **T3** | **regression-floor lock:** register the corpus as a named conformance set (band `min` matrix, force-act, hard-ceiling SID-independent, pct-inert-warn, live-watcher flock, operator-attached warn-only, hk-vpnp no-truncate-no-loop). | L-unit + L-fake-tmux | no | floor |
| **T4 (gated)** | **crew-restart e2e re-hydration:** daemon auto-arms crew keeper → crew `/clear`→resume same name → named queue re-drains. | scratch-daemon substrate | **yes — after `core-loop-proof`** | crew re-hydration |

T1–T3 (the whole acceptance corpus except crew-e2e) ship without touching the shared substrate. Only T4
waits. Recommend the captain staff `keeper-test-harden` **immediately alongside** the dispatch lane and
hold only T4 until `core-loop-proof` merges.

---

## Exec summary (for the admiral)

- **Keeper is already the most-tested subsystem in the tree** (~50 files: pure-unit + in-process reactive
  harness + a real-tmux session-twin, `cmd/harmonik-twin-session`). This is gap-closing, not greenfield.
- **Problem groups:** G1 band math · G2 restart-now tmux-resolve + sid-rebind (**B4/hk-pp1in, live pain
  this session**) · G3 nonce-confirmed-handoff gate (hk-vpnp truncate-loop) · G4 hold/release · G5
  doctor/live-watcher + binary-upgrade migration landmine · G6 live-pane auto-recover (**B3, watch
  re-stalls with no self-healing**).
- **Acceptance corpus (6):** (1) restart-now with a healthy watcher does NOT abort `no_tmux_target`;
  (2) session_id survives `/clear`, resume rebinds same conversation; (3) unconfirmed handoff not
  truncated + no loop; (4) watch re-stall auto-heals once, no alert storm; (5) hold dies on restart +
  hard-ceiling overrides it; (6) binary-upgrade refuse-to-start + `config --example` restores.
- **Layer routing:** band/hold/migration = pure unit; nonce-gate + tmux-resolve + auto-recover gate =
  fake-`tmuxRunFn`/reactive-session seam; sid-rebind + full B3/B4 drive = the existing real-tmux twin.
- **Can-build-in-parallel verdict: YES — the keeper test-system does NOT depend on the shared Phase-2
  daemon substrate.** Its harness (fake-tmux seam + session-twin) already exists and covers a surface
  disjoint from the dispatch lane. The ONLY substrate-gated item is crew-restart *end-to-end*
  re-hydration (reuses `scratch-daemon.sh`), which slots behind `core-loop-proof`.
- **Kerf shape:** one epic `codename:keeper-test-harden` (branch `epic/keeper-test-harden`), tranches
  T1 (B4 fix + B3 auto-recover) → T2 (sid-rebind, hold, migration) → T3 (regression-floor lock) all
  parallel-now; **T4 (crew e2e) gated on `core-loop-proof`.** Staff a keeper crew concurrent with the
  dispatch crews — disjoint surfaces, zero contention.
</content>
</invoke>
