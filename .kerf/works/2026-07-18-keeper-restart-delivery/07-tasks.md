# Implementation Tasks

Codename: `2026-07-18-keeper-restart-delivery` · pass 7 (tasks)
Direct input to bead creation. Every bead created from a task below carries the label
`codename:keeper-restart-delivery`.

## Guardrails (apply to every task)

- **ZERO keeper threshold changes.** No task may alter warn / act / force-act / hard-ceiling
  values, the band constants, or the gate-ladder order. SK-016 and [operator-nfr.md §4.13
  ON-059] remain in force verbatim (SK-028, NG1/SC-9). Any diff touching a threshold constant
  in `internal/keeper/thresholds.go` is out of scope and a review-block.
- **Preserve the retry-Enter loop (hk-89g / hk-ip33d / hk-7rgqs).** The 750ms-settle + 3×
  retry-Enter injection loop in `injector.go` is a load-bearing reliability fix; no task may
  reduce it to a single-Enter send (SK-025, NG3).
- **Preserve SK-INV-001.** No path may inject `/clear` without a confirmed, fresh handoff
  (SK-029, NG5).
- **Twin correction.** The pane/timing/handoff/operator-typing tests ride the keeper's
  **session-twin integration tier** (`cmd/harmonik-twin-session` + real tmux + real injector),
  NOT the `cmd/harmonik-twin-claude` scenario-harness YAML twin (SH-035).

---

## Task List

### T1 — Agent-input substrate: keeper as comms producer + presence reachability read

- **What:** Record and wire the two thin agent-input facts the delivery path depends on.
  (a) Recognize `keeper` as a `--from` producer identity and `keeper` as a `--topic` value on
  the `harmonik comms send` surface, so a `harmonik comms send --from keeper --to <agent>
  --topic keeper -- <body>` invocation is accepted and routable. (b) Expose the presence-Online
  reachability read the keeper will consume in-process: confirm `presence.ComputePresenceRegistry`
  + `GetPresenceState` are reachable to `internal/keeper` under depguard and return an
  Online/Stale/Offline state with age vs `presence.TTL` (120s). No bus redesign — the keeper is
  a NEW caller on existing surfaces; the send path is fire-and-forget (no join, no subscription).
- **Spec sections:** agent-input.md §4.10 (AIS-019 producer identity/topic; AIS-020 presence-Online
  reachability, necessary-but-not-sufficient).
- **Deliverables:** `cmd/harmonik/comms.go` (accept + document `--from keeper` / `--topic keeper`
  if not already unrestricted); depguard allow-check in `.golangci.yml` confirming
  `internal/keeper` → `internal/presence` import is permitted (per `.golangci.yml:178`); no change
  to `event_id` dedupe (N3), at-least-once delivery, or the subscribe contract.
- **Acceptance:** `harmonik comms send --from keeper --to <agent> --topic keeper -- "x"` produces a
  durable `agent_message` recorded on the bus with producer=`keeper`, topic=`keeper`, observable via
  `comms log`. A unit/integration probe imports `internal/presence` from a keeper-package test and
  reads an Online state with age < 120s without a depguard lint failure. No new bus port / event
  class / `InputPort` widening.
- **Depends on:** none.

### T2 — Config surface: `keeper.warn_messages` leader defer + crew keys, threaded to WatcherConfig

- **What:** Extend the existing `.harmonik/config.yaml` `keeper.warn_messages` block (today
  `default_warn_text` / `actionable_warn_text`) with (a) the leader **defer-message** keys and
  (b) a **crew-message** key defaulted empty/off (K7 config hook). Thread the new keys through
  `projectconfig.go` → `WatcherConfig` exactly as the existing two texts are threaded. Editing the
  YAML must require no rebuild. Strict unknown-key validation (`ErrUnknownConfigKey`) continues to
  apply to the new keys.
- **Spec sections:** session-keeper.md §4.14 (SK-032 external config home, edit-without-rebuild);
  park-resume-protocol.md §9 "Crew keeper-message disposition (K7 — DEFERRED)" items 1–2
  (crew-message key default-off + `self_service.crews_enabled` as the on/off switch).
- **Deliverables:** `internal/daemon/projectconfig.go` (parse the new `warn_messages` keys; keep
  strict unknown-key rejection at `projectconfig.go:113`); `internal/keeper/watcher.go` (thread the
  new keys into `WatcherConfig`, alongside `watcher.go:380-401`); `internal/keeper/ports.go` or the
  config struct that carries the texts.
- **Acceptance:** A config with the new leader-defer keys and a crew-message key parses and reaches
  `WatcherConfig` with no rebuild; an unknown sibling key still yields `ErrUnknownConfigKey`; the
  crew-message key defaults empty/off and does not fire any crew behavior (K7 is config-only).
  `self_service.crews_enabled` remains default-off and is the sole gate for the crew actionable form.
- **Depends on:** none.

### T3 — Templated message slots + extended structural-completeness validation (K2 body, K4 structure)

- **What:** Define the four normative structural elements of the leader defer template as fixed
  templated slots — (1) defer-condition-A (finish operator exchange), (2) defer-condition-B (finish
  in-flight unit), (3) the four-part good-stopping-point self-test, (4) the `keeper restart-now`
  command carrying the cycle nonce — templated the way `restartNowCmdToken` is
  (`injector.go:23,42-48`). Extend the existing `containsRestartNowCmd` validation
  (`watcher.go:893`) so an operator override that DROPS any of the four required slots falls back to
  the compiled default rather than shipping an incomplete nudge. Author the compiled-default prose
  for each slot, including the concrete four-part self-test text of SK-027 (i between discrete units;
  ii in-flight work saved/re-derivable; iii no unanswered operator question held; iv next session
  resumes from handoff + substrate with no redo).
- **Spec sections:** session-keeper.md §4.12 (SK-026 four required elements; SK-027 the four-part
  self-test); §4.14 (SK-033 structure-normative / prose-tunable slots + extended validation).
- **Deliverables:** `internal/keeper/injector.go` (add the three new slot tokens beside
  `restartNowCmdToken`); `internal/keeper/watcher.go` (extend `containsRestartNowCmd` →
  validate-all-four-slots + compiled-default fallback); compiled-default template strings for the
  leader defer body.
- **Acceptance:** The compiled default body contains all four elements and the verbatim self-test
  criteria. An override missing slot (1), (2), (3), or (4) each independently falls back to the
  compiled default (four table-driven cases). An override that fills all four slots ships the
  operator's prose. The restart-now slot renders `harmonik keeper restart-now --agent <name> --nonce
  <cycle_id>` with the nonce placeholder present.
- **Depends on:** T2 (keys must exist to be validated/filled).

### T4 — mtime-gated per-tick re-read of `warn_messages` (on-the-fly editing)

- **What:** Add a per-poll `stat` of the config file and re-parse ONLY the `keeper.warn_messages`
  sub-block when the mtime advances, so wording edits take effect without a keeper bounce. Scope the
  live-reload strictly to `warn_messages` — thresholds, bands, and `self_service` flags stay
  startup-bound (read once at `keeper_cmd.go:272`); no load-bearing decision constant is
  live-reloaded. `ErrUnknownConfigKey` continues to apply on each re-read.
- **Spec sections:** session-keeper.md §4.14 (SK-034 mtime-gated per-tick `warn_messages` re-read,
  scoped away from thresholds).
- **Deliverables:** `internal/keeper/watcher.go` (mtime cache + per-tick stat + scoped re-parse in
  the poll loop); reuse the `projectconfig.go` `warn_messages` parser from T2.
- **Acceptance:** Editing `warn_messages` text mid-run changes the next tick's nudge body with no
  keeper restart; editing a threshold/`self_service` key mid-run has NO effect until a keeper bounce
  (proves scoping); an unknown key introduced by the live edit is rejected (`ErrUnknownConfigKey`),
  not silently absorbed; a poll with unchanged mtime does not re-parse (stat-gated).
- **Depends on:** T2, T3 (re-reads the same keys and validated structure).

### T5 — `restart-now --nonce` flag + carry-for-audit provenance

- **What:** Add a net-new `--nonce <id>` flag to `keeper restart-now` (copy `ping`'s existing
  `--nonce`, `keeper_cmd.go:833`). `RestartNow` records the supplied nonce on its emitted events and
  cycle journal so a self-restart is traceable to the originating keeper cycle in `events.jsonl`.
  Semantics are **carry-for-audit, NOT hard-validate**: the separate restart-now process does not
  hold the keeper's live cycle id, so it MUST NOT reject on a nonce mismatch (matches how `ping`
  treats its nonce). The restart-now path is unchanged otherwise: fully synchronous verify →
  freshness-check → ACK → `/clear` → brief in its own process, wholly independent of the cycle's 300s
  `HandoffTimeout` watch window, and it upholds SK-INV-001 (no `/clear` without a fresh handoff).
- **Spec sections:** session-keeper.md §4.13 (SK-029 restart-now as default payload, independent of
  the 300s watch, upholds SK-INV-001; SK-030 net-new `--nonce`, carry-for-audit).
- **Deliverables:** `cmd/harmonik/keeper_cmd.go` (parse `--nonce` on the restart-now subcommand at
  `keeper_cmd.go:791`); `internal/keeper/restartnow.go` (thread the nonce into emitted events + the
  cycle journal at `restartnow.go:69`); no change to the verify/ACK/clear ordering.
- **Acceptance:** `harmonik keeper restart-now --agent <name> --nonce cyc-x-1` runs to a clean
  `/clear`+brief and the emitted `events.jsonl` records carry `nonce=cyc-x-1`. A nonce that does not
  match any live cycle is NOT rejected (carry-for-audit). A restart-now invoked after the cycle's
  300s watch has aborted still completes cleanly (does not consult the aborted cycle timer, SC-4).
  Omitting `--nonce` preserves today's behavior.
- **Depends on:** none (independent of the message/config cluster; the nonce string is supplied by
  the caller).

### T6 — Nonce provenance channel: mint → embed → record

- **What:** Wire the provenance channel with no shared runtime state. The keeper mints the
  `cyc-<ts>-<seq>` `cycle_id` at cycle entry (§7.1, D7, `cycle.go:491`); the leader comms message
  (T7) embeds that value verbatim in the `restart-now --nonce <cycle_id>` command string it renders;
  the agent runs that string verbatim; restart-now (T5) echoes the same value on its emitted events.
  Ensure the keeper's auto-cycle `KEEPER:<cycleID>` marker and the restart-now `--nonce` echo carry
  the SAME `cycle_id`, so restart-now events join to the originating cycle.
- **Spec sections:** session-keeper.md §4.13 (SK-031 provenance channel: mint at cycle entry → embed
  in message → record on events/journal; single `cycle_id` value across the marker and the echo).
- **Deliverables:** `internal/keeper/cycle.go` (surface the minted `cycle_id` to the message
  renderer); `internal/keeper/watcher.go`/`injector.go` (render `--nonce <cycle_id>` into the
  restart-now slot from T3); join-key verification that the auto-cycle marker and the restart-now
  echo share the value.
- **Acceptance:** For a single cycle, the `KEEPER:<cycleID>` marker, the rendered command string's
  `--nonce`, and the restart-now emitted event's `nonce` all show one identical `cyc-<ts>-<seq>`
  value; a query of `events.jsonl` by that nonce joins the restart-now events to the originating
  cycle. No shared mutable state is introduced between the keeper process and the restart-now
  process — the value flows only through the command string.
- **Depends on:** T3 (the restart-now slot), T5 (`--nonce` on restart-now records the value).

### T7 — K1 delivery decision: presence pre-check + comms send vs terminal fallback (SK-INV-006)

- **What:** Implement the deterministic delivery decision at the leader warn tick. On the warn tick,
  run the SK-023/SK-036 reachability pre-check (presence-Online, age < 120s, read in-process via
  `presence.ComputePresenceRegistry` + `GetPresenceState`). Then route by the total decision:
  **Leader + Online → comms** (shell `harmonik comms send --from keeper --to <agent> --topic keeper
  -- <body>` carrying the K2 defer template + the K3 restart-now command; NO `PanePort.Inject` /
  `SendEscape` pane write for that cycle; MUST NOT use `comms send --wake`). **Leader + Stale/Offline
  → terminal fallback** (the existing `injectTextClocked` warn path, operator-attached-gated text,
  full retry-Enter loop preserved). Every fired leader warn tick MUST resolve to exactly one of
  {comms, terminal-fallback} — never a silent no-op (SK-INV-006). Crew role is unchanged this work.
- **Spec sections:** session-keeper.md §4.11 (SK-022 comms not pane-paste, no `--wake`; SK-023
  presence-Online reachability read; SK-024 deterministic decision table, no silent no-op; SK-025
  terminal fallback preserves `injectTextClocked` + retry-Enter loop); §4.15 (SK-036 reachability
  pre-check feeds the decision); §5 (SK-INV-006 delivery totality). Consumes agent-input.md
  AIS-019/AIS-020.
- **Deliverables:** `internal/keeper/watcher.go` (the decision branch at the warn tick, ~
  `watcher.go:1424,1450-1451`; presence read; comms-send subprocess; role gate); `internal/keeper/
  injector.go` (guarantee the comms branch issues ZERO pane write; fallback branch keeps the
  750ms-settle retry loop at `injector.go:144-184`); no `--wake` anywhere in the comms branch.
- **Acceptance:** Leader + presence-Online → a `harmonik comms send --from keeper ... --topic keeper`
  subprocess fires with the defer body + restart-now command, and the pane receives ZERO
  inject/escape write that cycle (assert via swapped `tmuxRunFn`, `injector.go:106`). Leader +
  Stale/Offline → the existing `injectTextClocked` path runs with the retry-Enter loop intact and no
  comms send. No `comms send --wake` is ever issued on the leader nudge path. Every fired leader warn
  tick resolves to exactly one channel; a tick producing neither is a conformance failure. Crew warn
  behavior is byte-identical to pre-change.
- **Depends on:** T1 (producer identity + presence read), T3 (the templated body), T6 (the nonce in
  the body).

### T8 — K5 in-cycle operator-attached TOCTOU re-check

- **What:** Re-sample the operator-attached signal DURING the handoff wait, not only once at cycle
  entry. Today operator-attached is read once at cycle entry (`ports.go:190`) and not re-checked
  across the up-to-300s wait (SK-011/SK-017); re-sample it so an operator who starts typing AFTER
  cycle entry is respected on the terminal-fallback path. (On the comms path a present operator is
  already harmless — no pane write occurs — so this sharpening is scoped to the fallback path.)
  Note: SK-037 (hook-bridge keystroke signal) is OUT OF SCOPE — no task; it is a named external
  dependency on claude-hook-bridge.md, made non-fatal by T7's comms path.
- **Spec sections:** session-keeper.md §4.15 (SK-035 in-cycle operator-attached re-check). (SK-037 —
  external dependency, explicitly NO task.)
- **Deliverables:** `internal/keeper/cycle.go` / `internal/keeper/watcher.go` (re-sample
  operator-attached during the wait loop, feeding the terminal-fallback text/gating); reuse the
  existing operator-attached resolver (`tmuxresolve.go`, `ports.go:190`).
- **Acceptance:** An operator who becomes attached AFTER cycle entry but before the terminal-fallback
  injection is respected (advisory text / gating reflects the re-sampled state), whereas today's
  single entry-sample would miss it. No threshold or timing constant changes. Comms path is unaffected.
- **Depends on:** T7 (the delivery decision provides the terminal-fallback path this re-check feeds).

### T9 — Scenario-test task (MANDATORY)

- **Title (for the bead):** `scenario: keeper-restart-delivery — session-twin integration tier (5 failures)`
- **What:** Build the failing-before / passing-after tests for the five target failures. Per
  scenario-harness-design.md, the pane/timing/handoff/operator-typing coverage rides the keeper's
  **session-twin integration tier** (`cmd/harmonik-twin-session` + real tmux + the real keeper
  injector + a real `HANDOFF-<agent>.md`), NOT the `cmd/harmonik-twin-claude` YAML wire twin (SH-035).
  The comms-fallback failure (c) MAY additionally get at most ONE wire-observable scenario ONLY IF
  the fallback emits an assertable bus event (SH-036); it MUST NOT be promoted into the §10.1
  three-scenario conformance floor. Enumerate and implement all five (copy the named existing
  patterns):
  - **(a) operator-typing collision** — integration (real tmux) + unit adjunct. Pattern:
    `internal/keeper/cycle_operator_attached_integration_test.go` (pty-attach). Put partial input on
    the pane (no Enter), trigger the leader warn, assert the operator line is NOT submitted. Unit
    adjunct: swap `tmuxRunFn` (`injector.go:106`) to assert ZERO pane write when comms is taken.
    Validates: T7, T8.
  - **(b) late-handoff after 300s + T+301 restart-now** — harness unit (abort) + integration.
    Pattern: wire `substrate.FakeClock` into `CyclerConfig.Clock` (`cycle.go:97`), drive to
    AwaitingHandoff with `writeNonce=false`, `Advance(300s+)`, assert
    `cycle_aborted{reason=handoff_timeout}`; then `internal/keeper/restartnow_smoke_integration_test.go`
    for the T+301 self-restart completing a clean `/clear` (SC-4). Validates: T5, T6.
  - **(c) comms-unreachable fallback** — integration (comms/daemon). Pattern:
    `cmd/harmonik/comms_recv_follow_hk5xuvc_test.go` (in-process daemon + UDS) + the `comms who`
    presence registry. Seed target ABSENT → assert delivery resolves to terminal-fallback, NEVER a
    silent no-op (SK-INV-006/SC-2); positive control seeds target PRESENT → comms. Validates: T1, T7.
  - **(d) operator-present misread** — unit (primary) + integration adjunct. Pattern:
    `internal/keeper/tmuxresolve_operator_test.go` (`operatorActiveSince` table). Feed a
    stale-but-present `client_activity`; assert the entry-only sample misses a mid-cycle operator
    (fail-before) and the re-sampled signal catches it (pass-after). Validates: T8.
  - **(e) FORCE-ACT still cuts a never-idle session** — existing-level. Pattern: extend
    `internal/keeper/cycle_scenario_reactive_wave2_test.go` (`ForcedClearAboveHardThreshold`) +
    `internal/keeper/backstop_test.go` (hard-ceiling). Assert the K2 deferral does NOT weaken the
    FORCE-ACT / hard-ceiling backstop — a never-idle session is still cut unconditionally. Validates:
    the SK-028 guardrail (no threshold change).
- **Spec sections:** scenario-harness.md §4.14 (SH-035 session-twin tier; SH-036 at-most-one
  wire scenario, floor untouched); session-keeper.md §4.11–§4.15 + SK-INV-006.
- **Deliverables:** new/extended integration + unit tests in `internal/keeper/` (following the five
  named patterns above) exercising `cmd/harmonik-twin-session`; optionally ONE
  `cmd/harmonik/scenarios/regression/*.yaml` for failure (c) IF K1 emits a wire-observable event.
  NO extension of the `harmonik-twin-claude` wire twin with a tmux/operator-typing surface (SH-035).
  NO addition to the §10.1 conformance floor.
- **Acceptance:** All five failures fail before their implementation task and pass after. Each rides
  the correct tier per the table (a/d unit + real-tmux integration; b harness-abort + restart-now
  integration; c comms/daemon integration; e existing backstop level). The comms path asserts ZERO
  pane write; the fallback path asserts the retry-Enter loop is preserved. (e) proves the backstop is
  not weakened and no threshold constant changed.
- **Depends on:** T7, T8 (a, c, d, e delivery/situational paths); T5, T6 (b restart-now + nonce).

### T10 — Exploratory-test task (MANDATORY)

- **Title (for the bead):** `explore: keeper-restart-delivery — operator CLI surface & on-the-fly config`
- **What:** Operator-facing CLI-surface validation of the three human-driven surfaces this work
  adds, exercised by hand end-to-end (not just automated tests):
  1. `harmonik keeper restart-now --agent <name> --nonce <id>` — the flag is accepted, the run
     completes a clean `/clear`+brief, and the nonce appears on the emitted events (`events.jsonl`).
  2. Edit-on-the-fly of `keeper.warn_messages` while the keeper runs — change the leader defer wording
     and confirm the next nudge reflects it with no keeper bounce; confirm a threshold edit does NOT
     take effect live (scoping); confirm an unknown key is rejected.
  3. `harmonik comms send --from keeper --to <agent> --topic keeper -- <body>` — the send is accepted
     with the `keeper` producer identity and topic, lands as a durable `agent_message`, and is
     observable via `comms log` / an armed `comms recv --follow`.
- **Spec sections:** session-keeper.md §4.13 (SK-030 `--nonce`), §4.14 (SK-032/SK-033/SK-034 config +
  live re-read); agent-input.md §4.10 (AIS-019 producer identity/topic).
- **Deliverables:** an exploratory-test note recording the commands run, observed output, and
  pass/fail per surface (kerf work bench or the bead's close comment — no product-code change).
- **Acceptance:** All three surfaces behave as specified when driven by hand; any deviation is filed
  as a follow-up bug. Confirms the operator ergonomics the specs promise (edit-without-rebuild,
  nonce traceability, keeper-as-producer).
- **Depends on:** T5 (restart-now `--nonce`), T4 (live re-read), T1 (comms producer identity).

---

## Scope explicitly EXCLUDED (named, so beads are not created for them)

- **SK-037 hook-bridge keystroke signal** — external dependency on claude-hook-bridge.md; OUT OF
  SCOPE (K5 note only). No task.
- **Crew-side behavioral implementation (K7)** — only the config hook ships (T2, default-off). No
  crew keeper-message send path, no crew self-restart wiring. Activation is gated EXTERNALLY on
  **hk-220lv** (dead watcher, no auto-revive) and **hk-4tjyj** (reboot discards handoff) landing;
  those `keeper-reliability` bugs are the captain-delegated bug track (NG2) — not tasks here.
- **Any threshold / band / gate-ladder change** — forbidden (SK-016 / SK-028 / NG1 / SC-9).
- **§10.1 three-scenario conformance-floor changes** — a foundation amendment; not in this work.

---

## Dependency Graph (DAG — adjacency list, `A -> B` = B depends on A)

```
T1 -> T7
T1 -> T10
T2 -> T3
T2 -> T4
T3 -> T4
T3 -> T6
T3 -> T7
T4 -> T10
T5 -> T6
T5 -> T9
T5 -> T10
T6 -> T7
T6 -> T9
T7 -> T8
T7 -> T9
T8 -> T9
```

Roots (no dependencies): **T1, T2, T5**.
Leaves (nothing depends on them): **T9, T10**.
No cycles: the agent-input/config roots feed the message+delivery cluster, which feeds K5, which
feeds the tests — a strict topological order exists (e.g. T1,T2,T5 → T3 → T4,T6 → T7 → T8 →
T9,T10).

## Parallelization Plan

- **Wave 0 (fully parallel, no deps):** T1 (agent-input substrate), T2 (config keys), T5
  (restart-now `--nonce`). Three independent lanes.
- **Wave 1:** T3 (templated slots + validation) once T2 lands; T6 (nonce provenance) once T3 + T5
  land. T4 (live re-read) once T2 + T3 land — T4 runs in parallel with T6.
- **Wave 2:** T7 (delivery decision) once T1 + T3 + T6 land — the integration point where the
  substrate, the body, and the nonce converge.
- **Wave 3:** T8 (TOCTOU re-check) once T7 lands.
- **Wave 4 (parallel):** T9 (scenario/integration tests) once T5,T6,T7,T8 land; T10 (exploratory CLI)
  once T1,T4,T5 land. T10 can start as soon as its narrower deps (T1,T4,T5) are in — it does not need
  T7/T8 — so it may begin during Wave 2/3 while T9 waits for the full delivery path.

Serialization spine (longest path): **T2 → T3 → T6 → T7 → T8 → T9** (six deep). Everything else
hangs off it in parallel.
