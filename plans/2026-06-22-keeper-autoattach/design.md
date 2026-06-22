# Keeper auto-attach for every crew (and captain) — design

**Date:** 2026-06-22
**Status:** design + review-ready bead (read-only investigation; no code edited)
**Operator priority:** "Captains keep messing this up… we should be able to spin up crew where harmonik will always have keepers monitor and have default warn/act bands. Doing this makes runs over time more consistent."

---

## 1. Problem (verified against source)

A crew can end up with a keeper **window** but no live keeper **watcher**, while still being recorded as keeper-managed. Trace:

- `harmonik crew start` → daemon `HandleCrewStart` (`internal/daemon/crewstart.go:151`) → `SpawnCrewSession` (`internal/daemon/tmuxsubstrate.go:1308`) → `spawnCrewKeeperWindow` (`:1430`) → `crewKeeperWindowArgv` (`:1402`) → `agentlaunch.KeeperWindowArgv` (`internal/agentlaunch/keeperargv.go:100`).
- `crewKeeperWindowArgv` hardcodes `WarnAbsTokens: 0, ActAbsTokens: 0` (sentinel = "unset"). `KeeperWindowArgv` then **omits** the `--warn-abs-tokens`/`--act-abs-tokens` flags (`keeperargv.go:110-116`). So the keeper subprocess must self-resolve its bands from the operator's `keeper:` block in `.harmonik/config.yaml` at *its own* startup, via `ResolveKeeperConfig` (`cmd/harmonik/keeper_cmd.go:323` → `cmd/harmonik/resolve_keeper_config.go:332`).
- If that config is missing/incomplete, `ResolveKeeperConfig` **fails loud and the keeper process exits** — but the failure prints **only inside the keeper window's tmux pane** and emits a `session_keeper_config_rejected` event that nobody is watching. The daemon's only check is "did the tmux window get created" (`tmuxsubstrate.go:1455-1459`), which is **non-fatal, stderr-logged, and its return value is ignored** by the caller (`:1346`).
- `createCrewManagedMarker` (`crewstart.go:460`) writes `.harmonik/keeper/<name>.managed` **unconditionally and independently** of whether the keeper actually lives (write failure is itself non-fatal, `:279-280`). Net: a crew shows up as "managed" with a dead/never-started watcher. This is the long-standing "SESSION-KEEPER NOT DEPLOYED FOR CREWS" symptom in `docs/known-workarounds.md` (since 2026-06-09).

**Key finding — this is NOT a crew-only bug.** The captain path is *structurally identical*: `runCaptainLaunchWithOps` (`cmd/harmonik/captain.go:454-478`) also passes the raw flag values (default `0`/unset), also routes through the same `agentlaunch.KeeperWindowArgv`/`SpawnKeeperWindow`, and also treats a keeper-window failure as **best-effort / WARN-to-stderr only** (no abort, no comms, no post-spawn liveness assert). The captain only differs by wiring a `--respawn-cmd` (crews have no `harmonik crew respawn` yet, TODO hk-lcga). The captain "gets away with it" because the operator's config is currently populated and a human is watching captain stderr — but the same silent-keeper-less failure mode exists there.

So the fix is **one shared spawn-time gate + one shared post-spawn liveness assert**, applied to both the crew and captain spawn paths.

---

## 2. The hard tension, and exactly how this design honors it

**Standing mandate** (`specs/system-state.md:825-835`, `.claude/skills/keeper/SKILL.md:75-104`): the product imposes **ZERO** default keeper thresholds. `ResolveKeeperConfig` is fail-loud — if a required value is unset it refuses to start with one aggregated error naming every missing dotted key and pointing at `harmonik keeper config --example`. Suggested numbers live only in `keeper config --example` (sourced from `internal/keeper/thresholds.go`), never as a runtime fallback.

**Operator's new ask:** "default warn/act bands" so every crew inherits them automatically.

**These are already reconciled by the existing architecture — and this design adds zero new defaults.** The reconciliation is: *project-level operator-configured defaults*. The operator's `keeper:` block in `.harmonik/config.yaml` (live today at `:57-104`, populated with the TA1 band warn=200K / act=215K / force_act=240K) **IS** the "default warn/act bands." It is set ONCE by the operator and EVERY crew (and the captain) inherits it automatically because the spawned keeper reads that same block. There is no per-crew configuration and no product-baked number anywhere in this design.

What this design changes is **not the source of the numbers** (still operator config, still zero product defaults) but **WHEN and WHERE the fail-loud is observed**:

- Today: the fail-loud happens lazily, inside the keeper subprocess, after spawn, invisible to the daemon → silent keeper-less crew.
- This design: the daemon calls the **same** `ResolveKeeperConfig` at **spawn time, before** launching the crew's keeper window. If the operator's config is missing/incomplete, the **same aggregated fail-loud error** is produced — but now it is surfaced to the operator over comms and (decision below) blocks/flags the spawn, so a silent keeper-less crew is impossible.

Concretely, the three honor-points:

1. **No new default.** The design calls the existing `ResolveKeeperConfig` and propagates its existing aggregated error verbatim. If a required key is unset, the crew keeper does **not** start with an invented number — it fails loud, exactly as the mandate requires. We do NOT pass a fallback band into the argv.
2. **Single source of "defaults" = operator config.** The "default warn/act bands every crew inherits" are literally the operator's `keeper.context_thresholds` block. Set once, inherited everywhere. This is the only mandate-compliant reading of "default bands," and it is already how the captain's live keeper gets 200K/215K.
3. **Fail-loud is now *loud to the operator*, not just to a dead pane.** The existing `session_keeper_config_rejected` event and stderr text are augmented with a `comms send --to operator --topic keeper-alert` escalation, so a misconfig is actionable instead of silent.

---

## 3. Design

### 3a. Resolve bands via the SAME `ResolveKeeperConfig` path the captain (will) use — at spawn time, daemon-side

Add a **pre-spawn config gate** in the crew-spawn path (and the captain path), in front of `spawnCrewKeeperWindow` / `SpawnKeeperWindow`:

- In `HandleCrewStart` (`crewstart.go`), before `SpawnCrewSession` arms the keeper window, call `ResolveKeeperConfig(KeeperFlags{}, cfg.Keeper, projectDir)` — passing **empty flags** so resolution comes purely from the operator's config block (the crew has no per-crew band flags; that is the point). This reuses the *exact* resolver the keeper subprocess uses, so there is no second code path and no drift.
- **On error** (`*KeeperConfigMissingError` or `*KeeperConfigError`): do NOT spawn a keeper-less crew. Apply the **fail-loud-and-surface** behavior (§3c). Decision: this is a **hard gate** for crews — `crew start` returns a non-zero/structured error to the CLI naming the missing keys, and the crew is **not** spawned keeper-less. Rationale: the operator explicitly wants "always have keepers"; spawning a crew the operator must then babysit is the failure we are eliminating. (The `keeper config --example` remedy is in the error, so recovery is one command.)
- **On success:** thread the *resolved* warn/act values into the keeper argv. We have two equally-correct options; pick **Option A** for minimal blast radius:
  - **Option A (chosen): keep deferring numeric resolution to the keeper subprocess, but gate on a successful resolve first.** The argv still omits the band flags (keeper re-resolves from the same config), so we add no new flag-plumbing and the keeper remains the single resolution authority. The daemon's pre-resolve is a *guard* ("would this keeper be able to start?"), not a second source of truth. This keeps `crewKeeperWindowArgv` unchanged except that we now only reach it when resolution is known-good.
  - **Option B (noted, not chosen): thread the resolved warn/act ints into `KeeperWindowOpts` so the argv carries explicit `--warn-abs-tokens/--act-abs-tokens`.** More explicit and removes the keeper's re-read, but duplicates resolution timing and changes the argv contract for both crew and captain. Reviewer may prefer this for determinism; flagged for scrutiny.

### 3b. Operator-configurable project-level defaults that all crews inherit

No new mechanism required — this already exists and is the linchpin of mandate-compliance:

- The `keeper:` block in `<project>/.harmonik/config.yaml` is the single operator-set source (`internal/daemon/projectconfig.go` raw structs; live values at `.harmonik/config.yaml:57-104`).
- Every crew keeper and the captain keeper resolve from it. "Defaults all crews inherit" = "the one operator block."
- Document explicitly (in the bead + `docs/known-workarounds.md` update) that there is **no per-crew keeper config** and that setting the project block is the supported way to give every crew bands. `harmonik keeper config --example` prints a complete starting block.

### 3c. Fail-loud + comms-surfaced; post-spawn liveness assert (silent keeper-less crew impossible)

Two independent guards, both required:

**Guard 1 — pre-spawn config gate (config missing):** §3a. On `ResolveKeeperConfig` error, the daemon:
1. Emits the existing `session_keeper_config_rejected` durable event (already wired in `resolve_keeper_config.go:620-647`).
2. Surfaces to the operator over the real comms bus using the established keeper-alert shape (`.claude/skills/captain/SKILL.md:655-656`):
   `harmonik comms send --to operator --topic keeper-alert --from daemon -- "crew <name> NOT spawned: keeper config incomplete — missing <dotted keys>; run 'harmonik keeper config --example'"`.
   (`--from` must be a real commissioned identity; the daemon uses its own bus identity.)
3. Returns the aggregated error to the `crew start` caller (non-zero), so the captain/operator sees it immediately.

**Guard 2 — post-spawn liveness assert (keeper died despite valid config):** after `spawnCrewKeeperWindow`, the daemon performs a bounded **liveness probe** of the keeper watcher, then asserts green:
- Probe = `keeper.LiveKeeperPresent(projectDir, agent)` — the **flock** check that `keeper doctor` uses as its `live-watcher` check (`cmd/harmonik/keeper_enable_doctor_cmd.go:683-696`). This confirms a live keeper process actually holds the exclusive lock (distinguishes a running watcher from a stale corpse lockfile / empty window). This is the precise, narrow signal we want — NOT a full `keeper doctor` run, because a fresh spawn legitimately fails `gauge`/`idle`/`sid` until the session has taken a turn.
- Timing: poll `LiveKeeperPresent` with a short backoff (e.g. up to ~`keeper.timings.boot_grace`-bounded, a handful of 1s polls) to allow the window's keeper to acquire the flock. Operator-tunable via the existing `keeper.timings` block (no new hardcoded number — reuse `boot_grace`/`poll_interval`).
- **On NOT-live after the grace window:** the keeper failed to come up despite valid config (crash, env, race). The daemon:
  1. Tears down the misleading `.managed` marker OR marks the crew keeper-unhealthy (so it does not falsely advertise managed) — reviewer to pick; recommended: keep marker but emit an unhealthy event so existing `keeper.IsManaged` readers are unaffected while the alert fires.
  2. Emits a durable `session_keeper_watcher_dead` event (new) for learnability.
  3. Surfaces to operator over comms: `comms send --to operator --topic keeper-alert --from daemon -- "crew <name> keeper window spawned but watcher NOT live (flock unheld after grace) — crew is monitor-less; investigate"`.
  4. Decision: crew start **still returns the live agent** (the agent pane is up and useful) but with a **non-zero keeper-health field / warning in the response**, so the captain knows to escalate rather than assume coverage. (We do not kill a working agent over a keeper-window race; we make the gap *loud*.)

The combination makes a *silent* keeper-less crew impossible: missing config → hard-blocked + comms; dead watcher → live-probe-detected + comms. Either way the operator is told.

### 3d. Captain path — unify (recommended; build the shared helper, apply to both)

Because the captain path has the identical gap (§1), factor the two guards into a shared helper used by both spawn sites:

- A `agentlaunch`-level (or `internal/keeper`-level) `EnsureKeeperReady(ctx, opts) (KeeperHealth, error)` that does: pre-spawn `ResolveKeeperConfig` gate → `SpawnKeeperWindow` → post-spawn `LiveKeeperPresent` assert → returns health + surfaces alerts.
- Crew calls it from `HandleCrewStart`; captain calls it from `runCaptainLaunchWithOps` (replacing the current best-effort block at `captain.go:454-478`).
- **Difference to preserve:** captain passes `RespawnCmd` (dead-pane self-heal); crews pass empty until `harmonik crew respawn` exists (hk-lcga). The helper takes `RespawnCmd` as an opt.
- **Policy difference to preserve:** for the *captain*, the pre-spawn config gate should be a hard fail (a captain with no keeper is the worst case); for crews it is also hard per §3a. So the helper's gate is hard for both; only the post-spawn liveness *response* (block vs warn-with-live-agent) differs and is parameterized.

Recommendation: **build the shared helper and apply to both** in this work — it is the same code, and leaving the captain on the old best-effort path reintroduces the exact silent-failure class for the most important session. If reviewer wants to scope down, crew-only is acceptable as a first slice with a follow-up bead for the captain, but the shared helper should land regardless to prevent drift.

---

## 4. Verification plan

Prove (a) every crew has a LIVE watcher at spawn and (b) it stays consistent across keeper-restart cycles.

**A. Unit / resolver-gate tests (`internal/daemon`, `cmd/harmonik`):**
1. With a complete operator `keeper:` block: `HandleCrewStart` reaches keeper spawn; assert no error, resolved bands == config values.
2. With a `keeper:` block missing `warn_abs_tokens`: assert crew is **NOT** spawned keeper-less, the returned error names `keeper.context_thresholds.warn_abs_tokens` (aggregated, all missing keys), a `session_keeper_config_rejected` event is emitted, and a `keeper-alert` comms send is attempted. (Inject `liveKeeperFn`/comms via the existing seams.)
3. With config present but a band value invalid (pct>1): assert `*KeeperConfigError` path, same surfacing.

**B. Liveness-assert tests:**
4. Inject `LiveKeeperPresent` (injectable `liveKeeperFn`, per doctor) to return true within grace → crew start returns keeper-healthy.
5. Inject it to return false through the grace window → `session_keeper_watcher_dead` event + `keeper-alert` comms + crew returned with a non-green keeper-health field; assert the agent pane is still reported live.

**C. End-to-end (live deployment, the real proof):**
6. `harmonik crew start <name>` on this repo (config block present) → then `harmonik keeper doctor --agent <name> --project <dir>; echo $?`. Required GREEN evidence: the `live-watcher` check `✓` (flock held by a live keeper). Note a brand-new spawn may show `gauge`/`idle`/`sid` not-yet-green until the crew takes a turn — so assert specifically on the `live-watcher` line, not full exit-0, for the *immediate* post-spawn check; re-run doctor after the crew has dispatched once to confirm full green.
7. **Negative e2e:** temporarily remove `act_abs_tokens` from `.harmonik/config.yaml`, `crew start` → assert it refuses with the aggregated key list and a `keeper-alert` lands on `comms log --topic keeper-alert`; restore config.

**D. Keeper-restart-cycle consistency (the "consistent over time" proof the operator wants):**
8. Spawn a crew, confirm `live-watcher` green. Drive a keeper restart cycle (or simulate via `keeper`’s respawn path) and re-probe `LiveKeeperPresent` after the cycle — assert the watcher flock is re-held (no orphaned window, no stale-marker drift). This is the cross-cycle invariant: managed ⇒ live, before and after a restart.
9. Run the existing keeper smoke suite scoped narrowly (`-run`) to avoid the known fork-bomb signature (`reference_keeper_smoke_forkbomb`); do NOT run an ad-hoc looping restart-now smoke.

**E. Embedded-asset / doc sync:** if the `keeper` or `crew-launch` skill text or `docs/known-workarounds.md` is updated to document "no per-crew config; set the project block," re-sync embedded copies (`cmd/harmonik/assets/skills/*`) or `TestSkillAssetsEmbedInSync` goes red; run full `go test ./cmd/harmonik/`.

**Pass bar:** every crew spawn either (i) comes up with a flock-held live watcher (doctor `live-watcher` ✓), or (ii) is loudly blocked/flagged with a `keeper-alert` comms message to the operator. No path produces a managed-but-watcher-dead crew silently.

---

## 5. Draft bead (review-ready)

> File under `codename:keeper` (primary) — cross-reference `codename:leanfleet`. Type **bug** (a silent-keeper-less crew is a defect against the "always monitored" intent). Priority **P1**.

**Title:** `Crew (and captain) spawn: gate keeper on ResolveKeeperConfig + assert live watcher post-spawn — no silent keeper-less sessions`

**Type:** bug · **Priority:** 1 (P1) · **Labels:** `codename:keeper`, `keeper`, `crew`

**Description:**

Crews (and, by the same mechanism, the captain) can end up keeper-window-present but keeper-watcher-DEAD while still recorded as `.managed`. Root cause: `spawnCrewKeeperWindow` (`internal/daemon/tmuxsubstrate.go:1402-1415`) passes `WarnAbsTokens:0/ActAbsTokens:0`, so band resolution is deferred to the keeper subprocess, which calls fail-loud `ResolveKeeperConfig` at *its own* startup — and any failure (missing/incomplete operator `keeper:` config, or a watcher crash) surfaces **only inside the keeper window's pane**. The daemon's only check is "tmux window created" (`:1455-1459`, non-fatal, return ignored at `:1346`), and `createCrewManagedMarker` (`internal/daemon/crewstart.go:460`) writes `.managed` unconditionally. The captain path (`cmd/harmonik/captain.go:454-478`) has the identical best-effort gap. Symptom doc: `docs/known-workarounds.md` "SESSION-KEEPER NOT DEPLOYED FOR CREWS" (ongoing since 2026-06-09).

Make a silent keeper-less session **impossible**:

1. **Pre-spawn config gate (daemon-side, shared helper):** before arming the keeper window, call the SAME `ResolveKeeperConfig(KeeperFlags{}, cfg.Keeper, projectDir)` the keeper subprocess uses (`cmd/harmonik/resolve_keeper_config.go:332`). On its aggregated `*KeeperConfigMissingError`/`*KeeperConfigError`: do NOT spawn keeper-less — emit the existing `session_keeper_config_rejected` event, `comms send --to operator --topic keeper-alert` with the missing dotted keys + the `harmonik keeper config --example` remedy, and return the aggregated error to the `crew start` caller.
2. **Post-spawn liveness assert:** after the keeper window is created, poll `keeper.LiveKeeperPresent(projectDir, agent)` (the flock-backed `live-watcher` check from `keeper doctor`, `cmd/harmonik/keeper_enable_doctor_cmd.go:683-696`) within a `keeper.timings.boot_grace`-bounded grace. If the flock is never held: emit a new `session_keeper_watcher_dead` event + `keeper-alert` comms, and return the live agent with a NON-GREEN keeper-health field so the captain escalates instead of assuming coverage.
3. **Operator-configured defaults (no product defaults):** the warn/act bands come ONLY from the operator's `keeper:` block in `.harmonik/config.yaml` (live precedent: TA1 warn=200K/act=215K at `.harmonik/config.yaml:57-104`). There is NO per-crew keeper config and NO product-baked fallback. Document this in `docs/known-workarounds.md` + the `keeper`/`crew-launch` skills.
4. **Unify captain + crew** via a shared `EnsureKeeperReady` helper (pre-resolve → spawn → live-assert → surface) so both spawn sites share one path and cannot drift. Captain keeps `RespawnCmd`; crew passes empty until `harmonik crew respawn` (hk-lcga) exists.

**⚠ CONFIG-TENSION — reviewers MUST scrutinize:** there is a STANDING OPERATOR MANDATE that "the product imposes ZERO default keeper thresholds; `ResolveKeeperConfig` fails loud naming every missing key; suggested numbers live only in `keeper config --example`, never as a runtime fallback" (`specs/system-state.md:825-835`, `.claude/skills/keeper/SKILL.md:75-104`). The operator ALSO now wants "default warn/act bands every crew inherits." This bead resolves the tension by treating the operator's project-level `keeper:` block as the single inherited default — it adds **zero** new defaults and introduces **no** runtime fallback. The change is purely about WHEN/WHERE the existing fail-loud is observed (move it to spawn time + surface to comms + assert liveness), NOT about supplying any number the product invents. Reviewer must confirm: (a) no literal band value is introduced into the spawn path; (b) the pre-spawn resolve uses the existing resolver and propagates its aggregated error verbatim; (c) on missing config the crew is NOT spawned with an invented band.

**Acceptance criteria:**
- [ ] Crew-spawn calls `ResolveKeeperConfig` (empty flags, operator config) BEFORE arming the keeper window; on error the crew is NOT spawned keeper-less and the aggregated missing-key error is returned + a `keeper-alert` comms is sent to the operator + `session_keeper_config_rejected` emitted.
- [ ] After window creation, `LiveKeeperPresent` is asserted within a `keeper.timings.boot_grace`-bounded grace; if never live, a `session_keeper_watcher_dead` event + `keeper-alert` comms fire and the crew-start response carries a non-green keeper-health field.
- [ ] No new hardcoded/product-default band value anywhere in the crew or captain spawn path; bands resolve only from the operator's `.harmonik/config.yaml` `keeper:` block. (grep-checkable: spawn path introduces no integer band literal.)
- [ ] Captain path migrated onto the same shared `EnsureKeeperReady` helper (or, if scoped down, a follow-up bead is filed and the helper still lands).
- [ ] Tests: resolver-gate (complete / missing-key / invalid-value), liveness-assert (live-in-grace / dead-after-grace), and an e2e where `harmonik keeper doctor --agent <crew>` shows the `live-watcher` check ✓ right after spawn; negative e2e where a deleted config key blocks spawn with the aggregated key list on `comms log --topic keeper-alert`.
- [ ] Cross-cycle invariant test: after a keeper restart cycle the watcher flock is re-held (managed ⇒ live, before and after restart).
- [ ] `docs/known-workarounds.md` "SESSION-KEEPER NOT DEPLOYED FOR CREWS" updated to "resolved"; `keeper`/`crew-launch` skills note "no per-crew config; set the project `keeper:` block"; embedded skill assets re-synced (full `go test ./cmd/harmonik/` green).

**Out of scope / follow-up:** `harmonik crew respawn` dead-pane self-heal (hk-lcga) — crews pass empty `RespawnCmd` until it exists.

---

## 6. File index (for the implementer)

- `internal/daemon/tmuxsubstrate.go` — `SpawnCrewSession` (1308), `spawnCrewKeeperWindow` (1430), `crewKeeperWindowArgv` (1402) — the 0/0 bands + spawn-and-forget site.
- `internal/daemon/crewstart.go` — `HandleCrewStart` (151), `createCrewManagedMarker` (460) — where the pre-spawn gate + post-spawn assert hook in.
- `cmd/harmonik/captain.go` — `runCaptainLaunchWithOps` (289-489; keeper block 454-478) — captain's identical best-effort gap.
- `internal/agentlaunch/keeperargv.go` (`KeeperWindowArgv` 100) + `keeperwindow.go` (`SpawnKeeperWindow`) — shared argv/spawn; home for the shared `EnsureKeeperReady` helper.
- `cmd/harmonik/resolve_keeper_config.go` — `ResolveKeeperConfig` (332), aggregated `*KeeperConfigMissingError` (116-128), `emitKeeperConfigRejected` (620-647) — the fail-loud resolver to reuse.
- `cmd/harmonik/keeper_enable_doctor_cmd.go` — `live-watcher` check / `LiveKeeperPresent` (683-696) — the liveness probe to reuse.
- `internal/daemon/projectconfig.go` — raw keeper config structs; `.harmonik/config.yaml:57-104` — live operator `keeper:` block (TA1 bands).
- `specs/system-state.md:825-835`, `.claude/skills/keeper/SKILL.md:75-104` — the no-hardcoded-defaults mandate.
- `.claude/skills/captain/SKILL.md:655-663` — the `keeper-alert` comms escalation shape.
