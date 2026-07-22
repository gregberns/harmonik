# T10 / hk-keeper-delivery-exploratory-w09ua — exploratory-test note

**By:** bravo · 2026-07-19 · HEAD binary built from `phase1-session-restart-substrate`
tip (has T5 `06552e18`, T1 `a7b0cbe7`, T2/T4 config `53cd703d`). The installed
`/Users/gb/go/bin/harmonik` is `894e2856` (pre-delivery) — do NOT test against it.

Validates the three operator surfaces of keeper-restart-delivery. Spec: session-keeper
SK-030/032/033/034; agent-input AIS-019. Accept: all three behave as specified; any
deviation → follow-up bug.

## Surface 3 — keeper as comms producer (AIS-019 / T1): **PASS** ✅

```
$ harmonik comms send --from keeper --to bravo --topic keeper -- "…probe"
019f77c8-db82-7e21-a873-b2ddb94d644f          # accepted, event_id minted
$ harmonik comms log --from keeper --json | tail -1
{"event_id":"019f77c8…","type":"agent_message","source_subsystem":"eventbus",
 "payload":{"body":"…probe","from":"keeper","to":"bravo","topic":"keeper"}}
```

- `--from keeper` + `--topic keeper` **accepted** (free-text producer identity/topic;
  T1 was documentation + the in-process presence read, not a gate change).
- Lands as a **durable `agent_message`** with `from:keeper`, `topic:keeper`.
- **Observable via `comms log`** (and, by the same durable record, via an armed
  `recv`). Matches AIS-019 exactly. No `event_id`-dedupe / at-least-once / subscribe
  contract change involved (fire-and-forget send).

## Surface 1 — `keeper restart-now --agent <name> --nonce <id>` (SK-030 / T5): **PARTIAL** ⚠️

CLI surface confirmed on the HEAD binary:
```
$ harmonik keeper restart-now --help
  -nonce string   provenance nonce carried on the [KEEPER ACK <nonce>] line and the
                  emitted session_keeper_restart_now event; carry-for-audit, never
                  validated (default: rn-<ms> timestamp)
```
Command accepts `--nonce` and logs it:
```
$ harmonik keeper restart-now --agent testagent --nonce hk-w09ua-nonce-abc123 --project <scratch>
INFO keeper: restart-now: request received agent=testagent op=restart-now nonce=hk-w09ua-nonce-abc123
WARN keeper: restart-now: aborted … reason=no_tmux_target
```
- ✅ `--nonce` flag present, accepted, echoed verbatim in the request log (carry-for-audit,
  never-validated per SK-030).
- ⚠️ **Cannot finish against a fake agent:** with no live tmux pane the command aborts
  `no_tmux_target` and the `session_keeper_restart_now` durable audit event does NOT land
  in events.jsonl (emit is downstream of tmux-target resolution). The acceptance legs
  "clean /clear+brief" and "nonce on events.jsonl" REQUIRE a real keeper-watched agent
  pane — **hand-driven live run pending** (see Deferral below).

## Surface 2 — on-the-fly `keeper.warn_messages` edit (SK-032/033 / T4): **PARTIAL** ⚠️

- ✅ Keys PARSE with strict unknown-key rejection: `internal/daemon/projectconfig.go`
  `rawKeeperWarnMessages` (`default_warn_text`, `actionable_warn_text`, `on_demand_warn_text`
  [deprecated alias], `leader_defer_text`, `crew_defer_text`), documented at
  projectconfig.go:89-94; `ErrUnknownConfigKey` still fires on an unknown sibling.
- ⚠️ **DEVIATION (filed):** `harmonik keeper config --example` does NOT emit a
  `warn_messages` block at all (`grep -c warn_messages` = 0) — it shows only
  `context_thresholds` + `self_service`. So the operator surface T10 exists to validate
  (edit warn_messages on the fly) is **undiscoverable from the canonical example**. The
  keys work if you already know them; the example doesn't teach them.
- ⚠️ The live-reload behaviors — (a) a wording edit reflected on the next nudge with NO
  keeper bounce (T4 mtime-reread), (b) a threshold edit NOT applying live (scope guard),
  (c) an unknown key rejected on re-read — all REQUIRE a running keeper watcher observed
  across an edit. **Hand-driven live run pending.**

## Deferral (load-relief + valid-results)

Surfaces 1 (full clean-restart cycle + events.jsonl audit) and 2 (live-reload observation)
require a live keeper watcher + a real agent pane. Running them now (a) adds load during
the admiral's active load-relief window (box at ~load 25, drained crews told to idle) and
(b) would give confounded restart-timing/live-reload results on a saturated box — the same
reason the assessor was told to run its real-agent cells only un-saturated. **Recommendation:
complete the two pane-driven legs in a headroom window** (a scratch project: init → arm a
keeper on one throwaway claude pane → `restart-now --nonce` and assert clean /clear+brief +
`session_keeper_restart_now` nonce on events.jsonl; then edit `warn_messages` live and assert
next-nudge-reflects-it / threshold-not-live / unknown-key-rejected). bravo to drive on the
operator's/captain's go.

## Follow-up bug to file
- `keeper config --example` omits the `keeper.warn_messages` block (default_warn_text /
  actionable_warn_text / leader_defer_text / crew_defer_text) though projectconfig.go
  parses + documents them — the on-the-fly-tunable keys are undiscoverable from the
  canonical operator example. Minor (keys work once known); hurts surface-2 discoverability.
