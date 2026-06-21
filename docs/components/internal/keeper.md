# Session-Keeper: Component & Configuration Surface

Bead: hk-fgnk (this doc) · Codename: `codename:keeper-config`
Source of truth for defaults: [`internal/keeper/thresholds.go`](../../../internal/keeper/thresholds.go) (`Default*` consts).
Config loader: [`internal/daemon/projectconfig.go`](../../../internal/daemon/projectconfig.go) (`rawKeeperConfig` + sub-structs).
CLI flags: [`cmd/harmonik/keeper_cmd.go`](../../../cmd/harmonik/keeper_cmd.go).

> Precedence for every tunable: **CLI flag > `.harmonik/config.yaml` `keeper:` block > compiled default** (hk-lhu2 / PL-004b).
> The warn/act/force-act **band is operator-locked** — changing it is a deliberate retune, never a refactor side effect (see `thresholds.go` header + `codename:keeper-redesign`).

---

## The band (operator-locked, TA1 retune — hk-8hr1)

| gate | abs default | derivation |
| --- | --- | --- |
| warn | **200 000** | `defaultWarnAbsTokens` |
| act | **215 000** | `defaultActAbsTokens` |
| force-act | **240 000** | `act + 25 000` (`defaultForceActAbsOffset`) |
| hard-ceiling | **280 000** | `DefaultHardCeilingTokens` (independent SID-blind trip-wire) |

Invariant: `warn < act < force_act` (asserted in `thresholds_test.go`). The effective gate is
`min(absTokens, pctCeil × windowSize)` — on a 1M window the abs caps win; on a ~200k window the
pct ceils (`warn 0.70`, `act 0.85`, `force 0.95`) fire first.

---

## Configuration surface

Every key in the `.harmonik/config.yaml` `keeper:` block. **Applicability** marks keys that are
**CyclerConfig-only** — these drive the handoff→`/clear`→resume *cycle* and are therefore **INERT for
warn-only crew keepers** (`--warn-only`), which emit warn events but never run the cycle, respawn, or
live-pane recovery (hk-yfcc). Keys marked *Watcher* drive gauge polling / warn emission and apply to
both crew (warn-only) and captain keepers.

### `context_thresholds`

| config key (dotted) | CLI flag | default | applicability |
| --- | --- | --- | --- |
| `keeper.context_thresholds.warn_abs_tokens` | `--warn-abs-tokens` | `200000` (`DefaultWarnAbsTokens`) | Watcher + Cycler |
| `keeper.context_thresholds.act_abs_tokens` | `--act-abs-tokens` | `215000` (`DefaultActAbsTokens`) | Cycler (act gate) |
| `keeper.context_thresholds.force_act_abs_tokens` | — | `240000` (`act + DefaultForceActAbsOffset`) | Cycler-only |
| `keeper.context_thresholds.force_act_abs_offset` | — | `25000` (`DefaultForceActAbsOffset`) | Cycler-only |
| `keeper.context_thresholds.idle_floor_abs_tokens` | `--idle-floor-abs-tokens` | `150000` (`DefaultIdleRestartAbsTokens`) | Cycler-only (idle-restart) |
| `keeper.context_thresholds.act_pct_ceil` | `--act-pct` (fallback) | `0.85` (`DefaultActPctCeil`) | Watcher + Cycler |
| `keeper.context_thresholds.warn_pct_ceil` | `--warn-pct` (fallback) | `0.70` (`DefaultWarnPctCeil`) | Watcher + Cycler |
| (window cap) | `--window-size` | `200000` (`defaultFallbackWindowSize`) | Watcher |

### `hard_ceiling`

| config key (dotted) | CLI flag | default | applicability |
| --- | --- | --- | --- |
| `keeper.hard_ceiling.mode` | `--hard-ceiling-mode` | `alarm` (off\|alarm\|restart) | Watcher (mode-gated backstop) |
| `keeper.hard_ceiling.abs_tokens` | `--hard-ceiling-abs-tokens` | `280000` (`DefaultHardCeilingTokens`) | Watcher |
| `keeper.hard_ceiling.cooldown` | — | `5m` (`DefaultHardCeilingCooldown`) | Watcher |

### `timings` (all Go duration strings)

| config key (dotted) | CLI flag | default | applicability |
| --- | --- | --- | --- |
| `keeper.timings.poll_interval` | `--poll-interval` | `5s` (`DefaultPollInterval`) | Watcher |
| `keeper.timings.cycler_poll_interval` | — | `200ms` (`DefaultCyclerPollInterval`) | Cycler-only |
| `keeper.timings.idle_quiesce` | `--idle-quiesce` | `8s` (`DefaultIdleQuiesce`) | Watcher |
| `keeper.timings.staleness` | `--staleness` | `120s` (`DefaultStaleness`) | Watcher |
| `keeper.timings.handoff_timeout` | `--handoff-timeout` | `180s` (`DefaultHandoffTimeout`) | Cycler-only |
| `keeper.timings.clear_settle` | — | `3s` (`DefaultClearSettle`) | Cycler-only |
| `keeper.timings.boot_grace` | `--boot-grace` | `5m` (`DefaultBootGracePeriod`) | Watcher/Cycler (young-session guard) |
| `keeper.timings.max_boot_grace_total` | — | (derived; unset = no cap) | Cycler-only |

### `cadence` (all Go duration strings)

| config key (dotted) | CLI flag | default | applicability |
| --- | --- | --- | --- |
| `keeper.cadence.warn_cooldown` | — | `30s` (`DefaultWarnCooldown`) | Watcher |
| `keeper.cadence.no_gauge_backoff` | — | `30s` (`DefaultNoGaugeBackoff`) | Watcher |
| `keeper.cadence.respawn_grace` | — | `20s` (`DefaultRespawnGrace`) | Watcher (respawn) |
| `keeper.cadence.respawn_cooldown` | — | `90s` (`DefaultRespawnCooldown`) | Watcher (respawn) |
| `keeper.cadence.live_recover_grace` | — | `5m` (`DefaultLiveRecoverGrace`) | Watcher (live-pane recovery) |
| `keeper.cadence.live_recover_cooldown` | — | `5m` (`DefaultLiveRecoverCooldown`) | Watcher (live-pane recovery) |
| `keeper.cadence.force_retry_interval` | — | `120s` (`DefaultForceRetryInterval`) | Cycler-only |
| `keeper.cadence.idle_restart_cooldown` | — | `30m` (`DefaultIdleRestartCooldown`) | Cycler-only (idle-restart) |
| `keeper.cadence.hard_ceiling_cooldown` | — | `5m` (`DefaultHardCeilingCooldown`) | Watcher |
| `keeper.cadence.blind_keeper_threshold` | — | `5m` (`DefaultBlindKeeperThreshold`) | Watcher |
| `keeper.cadence.hold_ttl` | — | `45m` (`DefaultHoldTTL`) | Watcher (co-working hold backstop — hk-9waz) |

### `budgets`

| config key (dotted) | CLI flag | default | applicability |
| --- | --- | --- | --- |
| `keeper.budgets.heartbeat_max_misses` | — | `12` (`DefaultMaxHeartbeatMisses`) | Watcher |
| `keeper.budgets.max_handoff_timeouts` | — | `3` (`DefaultMaxHandoffTimeouts`) | Cycler-only (timeout escalation) |

### `self_service`

| config key (dotted) | CLI flag | default | applicability |
| --- | --- | --- | --- |
| `keeper.self_service.enabled` | — | `false` | Watcher (actionable-warn handshake) |
| `keeper.self_service.grace_seconds` | — | (unset → compiled default) | Watcher |
| `keeper.self_service.instruct_only_when_idle` | — | `false` | Watcher |
| `keeper.self_service.crews_enabled` | — | **`true`** (ABSENT ⇒ true; explicit `false` ⇒ false — hk-vs4u) | Watcher (crew self-restart) |

### `warn_messages`

| config key (dotted) | CLI flag | default | applicability |
| --- | --- | --- | --- |
| `keeper.warn_messages.default_warn_text` | — | `""` (compiled default) | Watcher |
| `keeper.warn_messages.actionable_warn_text` | — | `""` (compiled default) | Watcher (self-service advisory) |
| `keeper.warn_messages.on_demand_warn_text` | — | DEPRECATED alias of `actionable_warn_text` (hk-vs4u) | Watcher |

### Top-level CLI-only flags (no config key)

| CLI flag | default | applicability |
| --- | --- | --- |
| `--warn-only` | `false` | turns a keeper into a warn-only **crew** keeper (cycle/respawn/recovery INERT) |
| `--respawn-cmd` | `""` | supervised respawn launch command (Watcher; required for hard-restart escalation) |
| `--force-restart` | `false` | opt in to handoff-timeout hard-restart escalation (Cycler; needs `--respawn-cmd`) |

> **Why "INERT for warn-only crew keepers" matters:** a crew keeper runs with `--warn-only`, so it
> never enters the Cycler. Configuring `handoff_timeout`, `clear_settle`, `cycler_poll_interval`,
> `force_retry_interval`, `idle_restart_cooldown`, `idle_floor_abs_tokens`, `max_handoff_timeouts`,
> `force_act_abs_*`, or `max_boot_grace_total` on a crew keeper parses fine but has **no runtime
> effect** — the crew self-restarts via the actionable-warn handshake (see
> [`docs/keeper-restart-now-ack-protocol.md`](../../keeper-restart-now-ack-protocol.md) §"Actionable
> warn → self-service restart"), and the crew-launch skill's "§ Self-restart via the keeper" prose.

---

## Co-working hold/release override (hk-9waz)

`harmonik keeper hold --agent <name>` suspends the ACT/restart cutoff while an
operator/agent is actively co-working, so the keeper does not `/clear` a live
collaboration; `harmonik keeper release --agent <name>` clears it early
(idempotent). **WARN still fires under a hold** — only the restart action is
suspended. The hold is keyed by the live session-id
(`.harmonik/keeper/<agent>.hold.<sessionID>`), so it **auto-reverts on any restart**
(the session-id is re-minted on `/clear`), with `cadence.hold_ttl` (default `45m`)
as a walk-away/crash backstop. The **hard-ceiling restart deliberately overrides a
hold** (overflow protection wins). The verb surface and the version-gated caveat
(an older keeper binary silently ignores a hold) are documented in the
[`keeper` skill](../../../.claude/skills/keeper/SKILL.md) § "co-working override".

---

## Anti-drift guard

The default values in the tables above are pinned against the `Default*` constants by
`TestKeeperDocDefaultsNoDrift` in
[`internal/keeper/docdrift_test.go`](../../../internal/keeper/docdrift_test.go): it parses the
`default` column of this file (normalising `280k` / `280000` / `280_000`) and the reconciled
band comments, then fails if any number diverges from the constant. Update the constant **and** this
table together; the test is the lock.
