# 04-Design — seam-contract (C1 input+ack contract; C3 observation-only tmux; C6 deletion)

> Elaborates D1/D2/D5/D7/D11 within the pins of `00-decisions.md`. Code facts from
> `03-research/seam-contract/findings.md`.

## 1. The interface change (C1)

### 1a. `internal/handler/substrate.go` diff surface
- Add `InputKind`, `InputMsg`, `InputAck`, `ErrInputUnsupported`, `ErrInputStale` (D1 shapes,
  verbatim). `InputKind` enum: `InputSeed`, `InputResumeBrief`, `InputControl`.
- Widen `SubstrateSession` with `Submit` (D1 doc-comment normative: nil error ⇒ real ack).
- Package doc (`substrate.go:1-12`) gains the IN spec citation next to PL-021b/HC-054.
- The stdin-ownership comment (`:97-98`) is REWRITTEN: "For tmux-hosted sessions the pty owns
  stdin and `Submit` returns `ErrInputUnsupported`. For driver-hosted sessions
  (specs/agent-input.md IN-002) the substrate owns the child's stdin exclusively and `Submit`
  is the only input channel."
- `substrateSessionAdapter`: `SendInput` (`:140`) and `CloseStdin` (`:173`) change
  `return nil` → `return ErrInputUnsupported` (with doc). Adapter gains a delegating
  `Submit` → `a.inner.Submit`. NOTE: audit `handler.Session` callers of
  `SendInput`/`CloseStdin` for nil-assumptions before flipping (grep task; the exec-path
  sessions keep their real implementations).
- `SubstrateSpawn` gains ONE field: `Input InputChannelSpec` — `{Mode: pty|stdio}` so a
  substrate that hosts both knows which channel this spawn uses. Default zero value = `pty`
  (today's behavior); the driver substrate requires `stdio`. `StdinDevNull` doc (`:63-74`)
  amended: meaningless under `stdio` mode (driver owns the pipe).

### 1b. Implementations
- `tmuxSubstrateSession` + the perRunSubstrate session + scenariotest doubles: 3-line
  `Submit` → `ErrInputUnsupported`.
- `internal/claudedriver` provides the genuine `Submit` (driver-design §5).

### 1c. Composition-root selection (SC2)
At `workloop.go:4346-4368`: read `input_driver` from project config
(`.harmonik/config.yaml`, same loader as `codex.stale_wal_max_bytes`,
`codexwalguard.go:88-102` precedent — REQUIRED-no-default once the flip task lands; absent
config file ⇒ `tmux`). `structured` ⇒ wire `claudedriver.New(...)` as the per-run substrate
for claude bead runs; `tmux` ⇒ today's `perRunSubstrate` path untouched. The consumer sites
(`workloop.go:4951`, `reviewloop.go:806,1510`, `dot_cascade.go:1740`, `dot_gate.go:691`)
migrate from `pasteInjectOnLaunch`/`WriteLastPane` to `session.Submit` behind ONE helper
(`submitAgentInput(ctx, sess, msg)`) so the call-site diff is uniform; under `tmux` config the
helper routes to the legacy paste path until C6 — the branch lives in the helper (composition
decision), not scattered type-asserts.

## 2. The ack path at the seam (D2 elaboration)
- `Submit` contract table (normative in IN spec):

| Outcome | Return | Driver-side event | Caller obligation |
|---|---|---|---|
| Correlated frame within bound | `(InputAck, nil)` | `input_acked` | proceed |
| Bound elapsed | `(zero, ErrInputStale)` | `input_stale` terminal path | policy: retry (same MsgID) once, else kill+reopen |
| Session lost mid-wait | `(zero, wrapped io err)` | `driver_exited` | existing ProcessExit handling |
| No input channel | `(zero, ErrInputUnsupported)` | none | config error — surfaced loudly |

- The 180s `launchHeartbeatTimeout` and heartbeat/commit watchers REMAIN as downstream
  liveness (D2.3); the pasteinject-side watchdog (`pasteInjectQuitOnCommit`) is superseded on
  the structured path by driver terminals + the existing workloop noChange handling — the
  driver does NOT reimplement quit-on-commit scraping; commit detection stays where it is
  (workloop), and `InputControl{shutdown}`/graceful shutdown replaces `/quit` sends.

## 3. Observation-only tmux (C3)
- Under `structured`, the bead-run agent is daemon-hosted (PL-021f); **no tmux window hosts
  it**. Operator observation = the C4 capture files (`wire-out.jsonl` tailable; a
  human-readable rendering is a nice-to-have task, not a gate) + existing events/logs.
- `Attach()` (HC-054): driver-hosted sessions stream the live `wire-out` capture (an
  `io.Reader` over the tee — same "closing MUST NOT terminate" rule). Pane-pty wording in
  HC-054 is scoped "tmux-hosted".
- `CapturePane` and the pane read seam remain ONLY for the surviving interactive family and
  crew liveness — the daemon's bead-run path performs **zero capture-pane calls** (SC6 grep
  gate).
- tmux write verbs: NOT globally retired (research overturned the blanket framing) —
  retired **from the bead-run input path**; survivors per D7. The IN spec's
  observation-only clause is stated as: "on the structured input path, tmux carries no agent
  input and no agent-acceptance signal."

## 4. Deletion plan (C6) — per-symbol table

Stage: **P** = prep (immediate), **F** = at config-flip (Stage A), **D** = physical delete
(post Stage-B bake).

| Symbol | Stage | Note |
|---|---|---|
| `osadapter.SendKeysLiteral` (`:432`) | P | zero callers today |
| `dot_gate.go:691` WriteLastPane | F | migrates to Submit |
| `pasteInjectOnLaunch` + phase workers + `injectAndVerifySeed` + `sendSubmitEnterWithRetry` | D | unreferenced after F |
| `pasteInjectQuitOnCommit` / `QuitOnReviewFile` | D | superseded by driver terminals |
| six side-interfaces (`pasteinject.go:187..493`) | D | die with their consumers |
| `perRunSubstrate.{WriteLastPane,SendEnterToLastPane,SendQuitToLastPane,CaptureLastPane,PaneOutputFingerprint,PaneHasActiveProcess}` (`tmuxsubstrate.go:2218..2348`) | D | input/watchdog decorator surface |
| `pasteinject.go` (file, 2633 LOC) | D | after all above |
| `launchHeartbeatTimeout` constants | D | move to workloop if still referenced |
| SURVIVORS (D7 list) | never | grep-verified at gate |

**Boundary-audit gate (blocks every D-stage task):** script asserting (a) `grep -rn` zero
references to each D-symbol outside its own file; (b) `go build ./...`; (c)
`go test ./internal/keeper/... ./internal/daemon/...` green; (d) survivor list intact
(each survivor symbol still present + its callers unchanged); (e) Stage-B bake evidence
attached. This is REVIEW-FINDINGS A11's intent implemented at the true call graph
(PLANNER-RECONCILE #1 in 00b).

## 5. Spec deltas owned by this component (D5)
- `specs/agent-input.md` (IN): §contract (IN-001 Submit semantics; IN-002 stdin ownership;
  IN-003 ack definition + bound; IN-004 idempotency/MsgID; IN-005 honest-error rule — silent
  no-op input APIs FORBIDDEN); §boundary (IN-006 observation-only clause; IN-007 survivor
  carve-out naming keeper/crew-mission/CLI-seed/wake; IN-008 deletion gates incl. boundary
  audit + Stage-B bake + abort criteria); §lifecycle (IN-009 graceful shutdown; IN-010
  pre-launch hygiene, cites codexwalguard); §capture (IN-011/012 from capture-tee design);
  §taxonomy (IN-013 by reference to RS-017..020); invariants IN-INV-001 (ack-or-stale, the
  RS-INV-003 instantiation), IN-INV-002 (no-double-delivery), IN-INV-003 (ack precedes any
  turn-output attribution).
- `process-lifecycle.md`: add **PL-021f** (structured-driver spawn path — sanctioned
  exec-owned spawn for agent runs when `input_driver: structured`; PL-021b §1 text gains the
  cross-ref); PL-021d scoped to interactive-session writes (its load-buffer/paste rules stay
  normative for the survivors — keeper conformance to PL-021d is unchanged).
- `handler-contract.md`: HC-054 driver-hosted Attach branch; HC-055a/b argv rules cited by IN
  (driver reuses them verbatim).

## 6. M3 export (D11)
One paragraph in the IN spec (§cross-work): the D1 types + D2 guarantees are the contract
M3-4's runexec reactor consumes (`SubmitSeed`/`SubmitBrief` Actions; `InputAcked`/`InputStale`
Events). Marked "proposed to M3; reconcile at M3 design" — PLANNER-RECONCILE #2.
