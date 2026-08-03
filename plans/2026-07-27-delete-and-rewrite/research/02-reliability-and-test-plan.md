# Keeper reliability and test plan

## First repair

`make test-keeper-conformance` is false green. Its `internal/keeper` command
matches no tests. The integration-tagged
`internal/keeper/conformance_keeper_integration_test.go`
`TestKeeperConformanceCorpus_Integration` is the only matching corpus
registrar. The target therefore runs only the `cmd/harmonik` migration item,
even though it claims six scenarios.

Repair this before using the target as evidence. Restore a normal registrar or
change the target to its real tests. Then parse `go test -json` and fail when
any expected test name did not run. `[no tests to run]` must be a failure.

## Existing strengths to retain

- `internal/keepertest/l0_step_test.go` tests the pure transition ladder.
- `internal/keepertest/l1_contract_test.go` replays 507 recorded Claude cycles
  and checks strict event decoding and expected outcomes.
- `internal/keepertest/l2_fault_matrix_test.go` injects drop, stall, truncate,
  and duplicate faults. It asserts one bounded terminal and no half-open
  journal.
- `internal/keepertest/l3_live_test.go` checks real tmux injection with a
  disposable pane.
- `scripts/keeper-coverage-gate.sh` protects the core source files.

These are valuable. They do not prove the watcher races, every crash cut, or a
future Codex continuation policy.

## Required claims and tests

| Claim | Required proof |
| --- | --- |
| One keeper owns one session | Invalid, foreign, stale, or duplicate identity never receives an action. Lock and target changes cannot cross agents. |
| A Claude reset is safe | No `/clear` occurs before fresh handoff evidence and model-done evidence. Nonce scrubbing preserves handoff prose. |
| A started cycle ends | Every started cycle emits exactly one terminal result inside its virtual-time bound. |
| Human control wins | Recent operator activity, active attachment, hold, or an open decision suppresses the applicable action. Hard ceiling behavior stays explicit. |
| Recovery is exact | Restart after every journal phase neither repeats a completed action nor leaves a started cycle silent. |
| Delivery is idempotent | Comms-first delivery writes no pane text. Its fallback writes once. Retried ticks do not duplicate a message. |
| Health action is safe | Dead pane, live-but-stale pane, absent gauge, and blocked decision select the intended cooldown-gated response. |
| Each adapter is honest | A hook, status line, transcript reader, or pane command is tested with its real wire shape, not only a Go spy. |

## Missing test work

1. Add crash-cut tests around every effect in `Cycler.execute` in
   `internal/keeper/shell.go`. Cut after each journal write, handoff scrub,
   pane command, managed-session update, event append, and timer arm. Start a
   new cycler and prove one safe outcome.
2. Add deterministic watcher tests for cooldowns and races. Use a fake clock,
   fake pane, fake files, and fake process ports. Do not add more wall sleeps
   to `Watcher.Run` tests.
3. Expand replay selection. The L2 fault matrix uses one cycle from each
   stratum. It should cover every distinct action-path fingerprint in the 507
   cycles, or all cycles if the cost is acceptable.
4. Add mutation probes for critical claims. The suite must fail if `/clear`
   moves before model-done, the conformance registrar disappears, a stale stop
   becomes nudgeable, or a continuation cap is removed.
5. Add one opt-in real-harness canary per supported harness. The existing tmux
   smoke proves text reached a shell. A harness canary must prove that the
   harness consumed the action and produced the next expected signal.

## Codex test requirements

Build a sanitized fixture corpus. Do not commit raw private session records.
For the Stop-hook primary path, cover valid input, partial JSON, malformed
input, unknown fields, duplicate and foreign `(session, turn)` pairs,
`stop_hook_active`, operator questions, each durable work state, cooldown, and
continuation cap. The hook output must be exact JSON on stdout.

If the rollout-file fallback remains, cover `task_complete`, a partial final
line, record-shape drift, missing final message, file rotation, two sessions in
one CWD, and a wrong pinned file. Unknown shape must alert and must not inject.

Use a tmux Codex simulator before live use. It must consume a continuation,
produce a realistic next signal, and prove the journal joins observation,
decision, delivery, and outcome. Then run a disposable live canary on the
pinned CLI version. Only after a bounded soak and review of decision events can
the route run unattended.

## Observability and replay

Emit one structured decision record for every non-noop action. Include harness,
agent, session and turn identity, evidence kind and offset, decision, veto or
block reason, action ID, cooldown state, and observed result. Do not log handoff
or prompt bodies.

Replay the joined input stream, not just keeper events: gauges or hook input,
pane state, operator state, work state, timers, and delivery outcome. Keep raw
sources in the local private store. Keep only sanitized fixtures in the repo.

## Acceptance sequence

1. Fix the false-green conformance target.
2. Keep L0, L1, L2, coverage, and tmux conformance green.
3. Add crash-cut and deterministic watcher safety tests.
4. Add the Codex policy and hook codec fixtures plus fault matrix.
5. Add the tmux simulator and supervised live canary.
6. Review a bounded soak. Enable unattended continuation only after its events
   show the expected caps, vetoes, and terminal stops.
