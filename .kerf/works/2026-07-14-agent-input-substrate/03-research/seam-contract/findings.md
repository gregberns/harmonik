# 03-research / seam-contract — C1 input+ack, C3 observation-only tmux, C6 deletion boundary

> Pass 3 (Research), seam-contract component. Grounds Change-Design for the seam contract change
> (the handler.Substrate input method + ack), the tmux demotion, and the deletion boundary. All
> file:line verified against the tree on `phase1-session-restart-substrate`, 2026-07-14
> (parent-written; sub-agent returned text).

## Research questions
1. SEAM TODAY — handler.Substrate/SubstrateSession, the stdin-ownership comment, the no-op
   adapter, the depguard boundary.
2. SPECS TODAY — PL-021b / PL-021d / HC-054 load-bearing sentences; RS/SK conventions for the new
   input-protocol spec.
3. INPUT PATH INVENTORY — side-interfaces, tmux write verbs, callers, M2-scope split.
4. KEEPER CONSUMER — where keeper consumes paste-inject; what it sends; migrate vs carve-out.
5. ACK COMPOSITION — how "input accepted" is learned today; P1's bounded-liveness pattern.

---

## Q1 — The seam today: `internal/handler/substrate.go`

- Package doc (:1-12): Substrate is "an optional alternative spawn mechanism for handler.Launch";
  the concrete impl is injected by the daemon composition root "so that internal/handler never
  imports internal/lifecycle/tmux (depguard: handler-tmux cross-import is forbidden)". Spec refs
  :10-11: "specs/process-lifecycle.md §4.7 PL-021b — 'Substrate seam'; specs/handler-contract.md
  HC-054."
- `Substrate` interface (:30-38): **exactly one method**, `SpawnWindow(ctx, SubstrateSpawn)
  (SubstrateSession, error)`. No input operation on the seam at all.
- `SubstrateSpawn` (:46-89): WindowName, Cwd, Env, Argv, StdinDevNull (hk-rpr6, "MUST NOT be set
  for claude (pane-paste harness)" :71), Terminal (spawn-semaphore reservation).
- `SubstrateSession` (:101-122): Kill / Wait / Outcome / PID / Stdout. The load-bearing stdin
  comment (:97-98): "SendInput and CloseStdin are not part of this interface; **the substrate owns
  the child's stdin** (typically the pty managed by tmux)."
- No-op adapter (:129-199): `substrateSessionAdapter` adapts SubstrateSession→handler.Session.
  `SendInput` (:137-142) "no-op for substrate sessions … Returns nil silently"; `CloseStdin`
  (:171-175) "no-op … stdin is owned by the pty managed by the substrate"; `Stderr()` returns nil
  (:165-169). `newSubstrateAdapter` (:187-199) eagerly transitions the lifecycle Machine to
  Initializing.
- **Depguard reality check:** `.golangci.yml` has NO machine rule enforcing handler→lifecycle/tmux.
  Only `handler-brcli-ban` (.golangci.yml:293-298) and `handler-impls` (:419-424, targets
  claudecode|pi|twin subdirs that "do not yet exist"). The "depguard: handler-tmux cross-import is
  forbidden" claim in `substrate.go:8` / `handler.go:130` is **doc-level convention, not an active
  deny**. (Contrast the P1 substrate leaf rule at .golangci.yml:180-190 which IS enforced.)

## Q2 — Specs today

**PL-021b** — `specs/process-lifecycle.md:728` "Direct-tmux substrate (MVH alternative to ntm
adapter)". §5 forbids the daemon reading pane output via pipe-pane (restated :774). Threaded
through WM-002a (workspace-model.md:174,185) and EM-015d-RIA paste steps (execution-model.md:375-403,
"PL-021b-PASTE" label).

**PL-021d** — `specs/process-lifecycle.md:770` "Daemon→pane write mechanism (tmux load-buffer +
paste-buffer)". Load-bearing (:774-786): PL-021b §5 forbids READING pane output; "This clause
addresses the symmetric case — the daemon *writing* content into a pane"; "it MUST use the `tmux
load-buffer` + `tmux paste-buffer` sequence rather than `tmux send-keys` with a bare string"
(5-step: temp file per WM-026 → load-buffer → paste-buffer → delete-buffer → remove temp);
"`send-keys -l` … permitted as a fallback when payload &lt;512 bytes and no newlines"; "The
`send-keys` bare-string form (without `-l`) is FORBIDDEN for daemon-injected payloads."
Buffer-name discipline `harmonik-<session-id>-<purpose>`; delete-buffer cleanup; `daemon_pane_write`
INFO audit (:800+). **PL-021d is exactly what C3 retires** — the new input spec must supersede/amend
it, not add alongside.

**HC-054** — `specs/handler-contract.md:1143-1151`: "`Session.Attach()` for
`agent_type=claude-code` … MUST return an `io.Reader` that streams the live contents of the pane's
pty — not a tail of a log file" (the observation half). The "HC-054 family" also includes HC-055
flag allow-list, HC-056 agent_ready timeout, HC-057 heartbeat ownership (changelog
handler-contract.md:1479). HC-056 measurement (:691): "the timeout window starts at `SubstrateSpawn`
return … waits for the relay-synthesized `agent_ready` (CHB-013, provenance
`claude_session_start`)."

**P1 spec conventions to copy** (new input-protocol spec pattern):
- YAML front-matter: title / spec-id / requirement-prefix / status:draft / spec-shape:
  requirements-first / spec-category / version / spec-template-version:1.1 / owner / last-updated /
  depends-on (`specs/replay-substrate.md:3-18`).
- Prefix reserved in `specs/_registry.yaml` in the SAME commit (registry lint rule,
  replay-substrate.md:6-8; procedure _registry.yaml:8-12; RS/SK reserved at :34-35).
- Section layout: 1 Purpose / 2 Scope / 3 Glossary / 4 Normative requirements / 5 Invariants /
  6 Schemas / 7 Protocols & state machines / 8 Error taxonomy / 9 Cross-references / 10 Conformance /
  11 Open questions / 12 Revision history (replay-substrate.md, session-keeper.md:22-518).
- Requirement IDs `RS-001…` with `#### RS-00x — <name>` + `Tags: mechanism` footers; invariants a
  separate `XX-INV-00n` series (SK-INV-001…005 = SR3/SR4/SR6/SR7/SR9, session-keeper.md:240-266).
- MUST/MUST NOT with `> RATIONALE:` blocks; RS-023 disambiguation of "substrate"
  (replay-substrate.md:47,242: MUST NOT conflate with "the process-spawn seam
  (`internal/handler.Substrate`, the PL-021b Substrate seam)"). The new input spec sits ON that
  process-spawn sense — cite RS-023, pick a distinct prefix.
- Port idiom: RS-004 "1–3-method, consumer-owned, narrow interface satisfied structurally … MUST
  NOT introduce Result/Option/Either"; RS-005 stdlib-only leaf + depguard leaf rule (.golangci.yml:180-190).

## Q3 — Input path inventory (end to end)

**Side-interfaces in `internal/daemon/pasteinject.go`** (type-asserted at runtime):
- `enterSender` :187-192 (SendEnterToLastPane, splash-dismiss + submit Enter, hk-rf4ux)
- `paneCapturer` :206-213 (CaptureLastPane, seed-paste land-verification, hk-zexsj)
- `quitSender` :236-241 (SendQuitToLastPane, /quit injection, hk-cmybm)
- `paneOutputSizer` :254-260 (PaneOutputFingerprint, activity-aware suppression, hk-ue0u2)
- `paneLivenessChecker` :280-285 (PaneHasActiveProcess, hk-fbydv)
- `commandRunnerProvider` :493-495 (commandRunner(), remote SSH routing, hk-rs-b9)
- Plus `pasteInjecter` (WriteLastPane, tmuxsubstrate.go:78-84) and `sessionKiller` (pasteinject.go:775-777).
- Assertion sites: pasteinject.go:887,965,981,989,998,1427,1438; dot_cascade.go:1749; dot_gate.go:656.

**Implementations** — all on `perRunSubstrate` in `internal/daemon/tmuxsubstrate.go`: WriteLastPane
:2218, SendEnterToLastPane :2229, SendQuitToLastPane :2240, CaptureLastPane :2264,
PaneHasActiveProcess :2295, PaneOutputFingerprint :2348, commandRunner :1321. `tmuxSubstrate` itself
does NOT implement them (:92-95). Kill paths fire best-effort SendKeysQuit (:1503, :1972).

**Tmux write verbs** — `internal/lifecycle/tmux/osadapter.go`: LoadBuffer :379, PasteBuffer :405,
SendKeysLiteral :432 (≤512B/no-newline guard :435-439), SendKeysEnter :464, SendKeysQuit :486,
WriteToPane :536-542 (LoadBuffer+PasteBuffer composite). Interface in adapter.go:212-290.
capture-pane read verbs are in the same adapter (keep for C3 observation).

**Callers (who injects input):**
- *Agent-input, M2 scope:* workloop.go:4951 (pasteInjectOnLaunch) + :4998 (pasteInjectQuitOnCommit);
  reviewloop.go:806/:837 (implementer resume) + :1510/:1525 (reviewer); dot_cascade.go:1740/:1764/:1785;
  dot_gate.go:642 (pasteInjectCognitionGate). Payloads: "Please read .harmonik/agent-task.md and
  begin." / review-target / combined task+feedback (phase map pasteinject.go:18-22).
- *Spawn/boot-adjacent, distinct consumers:* crewstart.go:470-527 (pasteCrewMission via
  crewPasteInjector's own impl :515-526); cmd/harmonik/captain.go:210-229 and crew.go:221-240
  (boot-seed paste directly via adapter, NOT through handler.Substrate).
- *Out of daemon entirely:* cmd/harmonik/comms.go:445-465 (commsInjectTmuxPane wake nudge, raw
  exec); keeper (Q4).
- *Out of scope (spawn, not input):* SpawnWindow, Kill/Wait/orphan sweep.

## Q4 — Keeper consumer

- `PanePort` declared in `internal/keeper/ports.go:27-33`: Inject / SendEscape / SetEnv / Capture /
  OperatorAttached; doc :24-26: "Inject MUST follow PL-021d (load-buffer + paste-buffer); Capture is
  keeper-only (PL-021b §5 forbids the daemon this read). SK-R11." Normative: `specs/session-keeper.md:70-79`
  (SK-002): "PanePort is the tmux write/read boundary. Its Inject method MUST follow the load-buffer +
  paste-buffer write discipline of PL-021d; the bare send-keys form is FORBIDDEN … PanePort.Capture …
  MUST NOT be extended into the daemon's process-spawn path."
- Production Inject = `keeper.InjectText` (`internal/keeper/injector.go:133-184`): load-buffer (buffer
  `hk-keeper-inject`, :152-154) → paste-buffer -d (:158) → 750ms settle → send-keys Enter + 2×400ms
  retry (:164-181). **Shells out to tmux directly via `tmuxRunFn`/`exec.Command` (:106-116), NOT via
  `tmux.OSAdapter`** — consistent with the keeper depguard allowlist (.golangci.yml:124-146, no
  lifecycle/tmux).
- What keeper sends: `/session-handoff` (after SendEscapeKey, :200), `/clear`, then
  `briefRestartCmd = "harmonik agent brief --wake keeper-restart"` (cycle.go:17-19), `/session-resume`
  (injector.go:131-132 doc), warn texts, and ACK line `[KEEPER ACK <nonce>]` (:70-72); also SendEscape
  and `tmux setenv` (:227-236).
- **Migrate vs carve-out:** keeper's payloads are slash-commands + human-readable nudges to arbitrary
  interactive Claude panes it did NOT spawn (captain/crew/orchestrator), addressed by tmux target
  string, off-daemon (keeper MUST NOT import daemon, .golangci.yml:142). A daemon-side structured
  input driver on handler.Substrate cannot serve keeper without a session handle it structurally
  lacks. Migration would require (a) the input protocol in a leaf package keeper may import, keyed by
  session-id not SubstrateSession, or (b) routing keeper injections through a daemon RPC. Carve-out
  (keeper keeps PL-021d-style paste per SK-002, freshly re-specced 2026-07-13) is the tree's current
  posture — but collides with C6's "keeper is IN the deletion boundary" (see Risks #1).

## Q5 — Ack composition today + P1 bounded-liveness pattern

How "input accepted" is learned (no real ack anywhere):
1. **tmux exit-0 is NOT an ack** — pasteinject.go:130-138 (hk-zexsj): exit 0 once tmux hands the
   buffer to the pane, NOT once claude's React/ink TUI rendered it.
2. **Render-verify (capture-pane marker)** — `injectAndVerifySeed` pasteinject.go:1691-1724: capture
   pane, check marker substring; ≤3 attempts, 1.5s backoff; failure ⇒ `pasteinject_failed`
   (emitPasteInjectFailed :1470).
3. **Submit-Enter retries, blind** — `sendSubmitEnterWithRetry` :1795 / `sendResumeSubmitEnter` :1814;
   :100-108: "no pane-capture primitive on the enterSender interface to detect 'input cleared', so we
   cannot positively confirm submission." One-shot reseed Enter after 75s (implementerReseedGrace :753).
4. **agent_ready** — `waitAgentReady` (`internal/daemon/agentready.go:154-`), HC-056: relay-synthesized
   `agent_ready` provenance `claude_session_start` (handler-contract.md:691); typed sentinel
   `ErrAgentReadyTimeout` (agentready.go:116). Acks process readiness, not any specific input.
5. **agent_heartbeat** — `RunHeartbeatLoop` (`internal/handler/claudehandler_chb006_024.go:663-670`):
   first heartbeat immediate; cadence HeartbeatInterval 300s (:49); daemon-emitted per HC-057. Consumed
   in pasteInjectQuitOnCommit's event loop (pasteinject.go:1055-1075).
6. **Commit as the ultimate ack** — pasteInjectQuitOnCommit polls `git rev-parse HEAD` vs initialSHA
   every 500ms (resolveWorktreeHEADVia :506; commitPollInterval :603) with the kill ladder:
   launchHeartbeatTimeout 180s (:685), launchSuppressionCeiling 12m (:719), heartbeatStalenessThreshold
   8m (:658), commitPollTimeout 30m progress-extended (:623), commitHardCeiling 90m (:639). Per-run
   event-tap dedupe (workloopeventsource.go:50-106) exists because a competing consumer once starved
   the watchdog of heartbeats (hk-jgxqc).

**P1 bounded-liveness pattern for C1's ack/liveness to mirror:**
- SK-015 (`specs/session-keeper.md:191-193`): every `handoff_started(c)` MUST reach exactly one
  terminal outcome within a bounded window ≈ HandoffTimeout(300s)+model_done_timeout(60s)+
  ClearConfirmBackstop(150s)+overhead ≈ 520s — or emit a `restart_failed`-class event. "Silence is
  FORBIDDEN. … every TimerFired edge MUST land in a state that has an outgoing action."
- SK-INV-005 / SR9 (:264-266): same, per-cycle.
- **STEP-0a is NOT in RS/SK specs** — it is the census plan item
  (`plans/2026-07-12-codebase-census/PLAN.md:85-100`): "a resumed pass must never go dead-silent …
  DoD: a single deterministic fault-injection test — stalled agent on relaunch → terminal signal
  (output OR run_stale) within a bounded window — plus N=10 consecutive clean
  commit-gate-fail→relaunch cycles with zero silent hangs." Code-revamp roadmap folds STEP-0a into M3
  as M3-5 (`plans/2026-07-13-code-revamp/TASKS.md:86`); **M3-4 depends on "M2-1 (seam input/ack
  contract)" (TASKS.md:85)** — the cross-work edge. Census names the M2 risk directly (PLAN.md:205-208):
  "a wrong ack/heartbeat contract re-imports the resume-hang on a substrate whose escape hatch is
  deleted."

## Patterns to follow
- Spec: RS/SK template exactly — front-matter v1.1, registry prefix same-commit, requirements-first,
  XX-INV series, RS-023-style "substrate" disambiguation, conformance section with baseline anchors.
- Ports: RS-004 consumer-owned 1-3-method narrow interfaces; the keeper T6 `ports.go` ports +
  fn-adapters (preserving construction sites) is the proven idiom for retiring the six type-asserted
  side-interfaces.
- Depguard: land a MACHINE rule with the seam (RS-005 leaf style + C-ratchet) — the existing
  handler-tmux "boundary" is comment-only.
- Liveness: SK-015-shaped clause — named window as a sum of configured timeouts, terminal-or-failure
  event, "silence is FORBIDDEN", every timer edge lands in an emitting state; DoD = fault-injection
  test + N=10 clean cycles.

## Risks / conflicts
1. **Keeper deletion hazard (biggest).** C6 puts keeper in the deletion boundary, but SK-002 (drafted
   2026-07-13, one day before this work) NORMATIVELY requires PanePort.Inject to follow PL-021d, and
   keeper's injector deliberately bypasses tmux.OSAdapter (own exec, own buffers, off-daemon,
   depguard-barred from daemon). Deleting PL-021d verbs from lifecycle/tmux does NOT remove keeper's
   paste path; retiring PL-021d itself contradicts SK-002 unless the input spec amends SK-002 in the
   same motion. → design must resolve migrate-vs-carve-out AND reconcile SK-002.
2. **Shared spawn/boot paths outside the daemon.** captain.go:210-229, crew.go:221-240 (boot-seed) and
   comms.go:445 (wake nudge) inject input without touching handler.Substrate. C1's "typed input op on
   the seam" covers only daemon runs; these CLI paths need their own migration or explicit carve-out,
   or C6's tmux-verb deletion breaks them.
3. **Depguard assumption mismatch.** Decompose treats handler↔tmux as depguard-declared; it is
   doc-comment-only. The new spec should make it a real rule.
4. **Ack entangled with the kill ladder.** pasteInjectQuitOnCommit (~600 lines) fuses input-ack,
   liveness, budget, teardown; C1's "real ack" removes only the front. M3 (run-state-machine) owns
   dissolving the watchdog. The M2/M3 cut line (M2-1 → M3-4, TASKS.md:85) MUST be drawn in the spec or
   the logic gets rebuilt twice.
5. **PL-021d has more consumers than the daemon.** Buffer-name discipline + daemon_pane_write audit are
   cited by execution-model (EM-015d-RIA/RFD, execution-model.md:375-403) and claude-hook-bridge CHB-028
   (claude-hook-bridge.md:469, agent-task.md as "the normative daemon→claude task-delivery channel").
   Retiring paste obsoletes the *delivery instruction*, not the artifact — CHB-028's file contract likely
   survives with a new delivery clause.
6. **StdinDevNull asymmetry.** SubstrateSpawn.StdinDevNull (substrate.go:63-74) exists because codex
   reads stdin and claude doesn't ("MUST NOT be set for claude (pane-paste harness)"). A real input
   protocol changes who owns stdin per harness; the spec must say what happens to this flag.
7. **Remote (SSH) substrate.** Every side-interface has a `…Via(runner)` twin (pasteinject.go:381,411,
   506,551; tmuxsubstrate.go:2157 spawnWindowRemote). The new input driver must carry the
   CommandRunner/remote seam or remote workers (M4) regress — keep the seam remote-capable even though
   M4 owns the transport.
