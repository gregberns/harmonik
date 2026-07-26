# 03-research / driver — C2 structured-protocol driver, C7 WAL-guard fold-in

> Pass 3 (Research), driver component (C2 + C7). Grounds Change-Design for the second
> handler.Substrate implementation (claude headless stream-json / Agent SDK stdin) and the codex
> WAL-guard fold-in. All file:line verified against the tree on `phase1-session-restart-substrate`,
> 2026-07-14 (parent-written; sub-agent returned text).

## Research questions
1. Seam to instantiate — internal/substrate API + codex instantiation idiom (frameToEvent).
2. Wire protocol options — what the repo knows about claude headless input; codex app-server shape.
3. Composition root — how deps.substrate is built/injected; any selection mechanism; twin-binaries.
4. Process ownership — how claude is spawned today; direct-child piped-stdin patterns; the tmux
   inspectability constraint.
5. WAL-guard — the C7 target, what it compensates for, adapt-vs-delete.

---

## Q1 — Seam to instantiate + codex instantiation idiom

### Generic seam (`internal/substrate/`)
- `EventSource[E any]` (seam.go:7-9): `Events(ctx) <-chan E`.
- `Effector[A any]` (seam.go:13-15): `Execute(ctx, a A) error`.
- `Run[E,A any]` (seam.go:27-36) — normative driver, a **free function** (Go forbids generic
  methods): `func Run[E,A any](ctx, src EventSource[E], step func(E) []A, eff Effector[A]) error`.
  Ranges src.Events, applies step per event, executes actions in order; nil on channel close, first
  effector error otherwise. Run never closes the source's channel.
- `ReplayCodec[E]` (replay.go:59-75): vertical supplies `DecodeLine(line) (ev, emit, err)` (fuses
  decode + error-policy + filter + map), `ErrorEvent(msg) E`, `DisconnectEvent() E`. Codec may be
  stateful; seq/dedup state lives inside the codec (RS-008).
- `Twin[E]` (replay.go:82-121), `NewTwin[E](corpus, FaultConfig, codec, opts...)` (:104-110); 1 MB
  scanner buffer default. Four vertical-neutral fault modes (:22-41): DropAfter/Stall/Truncate/Dup.
- `ClockPort` (clock.go:10-18): Now/Since/NewTicker/Sleep(ctx,d) bool. `SystemClock` (:30-51),
  `FakeClock` (fakeclock.go:18-28, Advance-driven virtual time).
- `FakeEffector[A]` (doubles.go:14-41, "the graded artifact, not the grader"), `SyntheticSource[E]`
  (doubles.go:48-69).

### Codex instantiation (the template C2 mirrors)
- **`internal/codexreactor/reactor.go`**: flat `Event` struct (:66-77, Seq + Type + optional fields,
  "flat struct enables JSON round-trip for scenario files"); 8 EventTypes (:38-56); flat `Action`
  (:106-116), 5 ActionTypes (:84-100); **type ALIASES not defined types** (:130,:145: `type Effector
  = substrate.Effector[Action]` — RS-021 structural identity); `State` (:151-165, InFlight=I1,
  LastSeq=I2); `Step(ev) []Action` (:193-285, "pure: no goroutines, no I/O", returns nil not empty
  slice, unknown type dropped); `Run` one-liner (:297-299): `return substrate.Run(ctx, src, r.Step,
  eff)` — `r.Step` is a bound method value of type `func(Event) []Action`, no adapter needed.
- **`internal/codexwire/codexwire.go`**: two-layer (:1-27) — strict JSON-RPC 2.0 envelope, 5
  FrameKinds inferred from id/method/result/error (:39-45, incl FrameKindRaw), + per-method
  params/result structs each with `Extra map[string]json.RawMessage`. **Single method registry**
  `methodRegistry map[string]methodEntry` (:92-163) with Dir + MakeParams/MakeResult factories; new
  method = one entry + a Params/Result pair; unknown method → FrameKindRaw verbatim, never an error.
  `Parse` (:183-252), round-trip guarantee Parse→Marshal semantically equal (:22-24, Marshal :309-329).
- **`internal/codexdigitaltwin/twin.go`** — the frameToEvent idiom C2 must reproduce for claude:
  `codexCodec` implements ReplayCodec[Event] (:80-124): DecodeLine = codexwire.Parse → filter
  non-ServerNotification ⇒ skip (:102-103) → `frameToEvent(frame, &seq)` (:105); seq is codec-internal
  (:85-87). `frameToEvent` (:155-230) is a method-string switch mapping exactly 5 notifications
  (turn/started, turn/completed, item/agentMessage/delta, thread/status/changed,
  thread/tokenUsage/updated); each arm type-asserts frame.Params, `*seq++`, builds a flat Event;
  non-mapped → (zero,false) skip. Pipeline (:22-29): corpus JSONL → DecodeLine → Twin[Event] →
  channel → Reactor.Run → Actions → FakeEffector.

---

## Q2 — Wire protocol options

### Proven: codex app-server shape
- **Framing: newline-delimited JSON-RPC 2.0 over stdio.** `testdata/codex-app-server/T0-findings.md`:
  "Transport: stdio is the default … newline-delimited JSON-RPC 2.0 on stdin/stdout." Handshake:
  initialize → initialized → thread/start{cwd} → turn/start{threadId, input:[{type:"text",text,
  text_elements:[]}]} → stream item/*/thread/*/turn/* until turn/completed. `text_elements:[]`
  REQUIRED. Cancel = turn/interrupt; turn/steer appends to in-flight turn; thread/turn ids UUIDv7
  server-minted. Corpus: testdata/codex-app-server/corpus/raw-session-01.jsonl (23 frames); schema
  testdata/codex-app-server/protocol-schema.json + gen/.
- The codex sidecar design (`.kerf/works/codex-app-server/04-design/orchestrator-session-model-design.md`)
  already frames the driver concept: "an actual event loop that reacts to delivered messages without a
  keystroke — is exactly what dissolves the four Claude-specific items" (splash-dismiss, boot-seed
  paste, --wake nudge, keeper /clear cycle).

### Known about claude (repo evidence)
- **--output-format stream-json exists, documented as the post-MVH evolution**: `specs/claude-hook-bridge.md:684`
  — "Claude Code supports `--output-format stream-json --include-hook-events --include-partial-messages`
  … emits hook lifecycle events natively on stdout as NDJSON … eliminating .claude/settings.json
  materialization and the relay subprocess entirely." NOT adopted at MVH (:59); caveat (:688): "the
  stream-json event vocabulary is less documented than the hooks reference; the field schemas are
  subject to evolution." Review confirms forward-compat routing on (run_id, claude_session_id)
  (docs/review-claude-hook-bridge-spec.md:96-100).
- Programmatic drive (`docs/plans/captain/01-problem-space.md:46-52`): `claude -p "<task>" --resume <id>
  --output-format json`, Agent SDK (resume=<id>), `claude remote-control --name <n>`.
- **Agent SDK sessions API REJECTED on billing** (`docs/plans/captain/05-specs/c2-spec.md:104-109`):
  "the only docs path with a server-minted id + programmatic message send, BUT it bills the API credit
  pool, not the Max subscription … Considered and rejected." session_id is caller-minted via
  `--session-id <uuid>` (c2-spec.md:96-98; argv in claudelaunchspec.go:409-417: `--resume`/`--session-id`
  then `--model`/`--effort`).
- Decompose's own open question (02-components.md:76-81); census anchors TASKS.md:98 (M2-2/C2),
  :103 (M2-7/C7); ROADMAP.md:77; sizing PLAN.md:182 "replaces ~5.4k LOC" (verified: tmuxsubstrate.go
  2735 + pasteinject.go 2633).

### NOT known from the repo (assume / verify externally) — see PLANNER-RECONCILE
- **`--input-format stream-json` (bidirectional stdin NDJSON for a persistent headless session)
  appears NOWHERE in the repo** — no doc, spec, or capture. The repo proves the OUTPUT side
  (documented-but-unadopted); the INPUT side is entirely unproven here.
- No captured claude NDJSON corpus exists. A T0-style spike + apptap capture is the missing first
  artifact.
- Whether claude stream-json input supports mid-turn steering/interrupt (codex turn/steer,
  turn/interrupt analogs) — unknown.
- Whether headless stream-json claude bills the Max subscription like the interactive CLI (the c2-spec
  rejection was about the sessions HTTP API and predates headless-driver plans).

---

## Q3 — Composition root

- Seam field: `workLoopDeps.substrate handler.Substrate` (workloop.go:482-489), set from cfg.Substrate
  at :1142 ("nil falls back to exec.CommandContext; set by composition root").
- Per-run wrap: workloop.go:4346-4368 — `newPerRunSubstrate(deps.substrate, deps.handlerBinary,
  runRunner)` (:4352) for pane isolation (hk-012af), plus remote SSH routing + independent-run-session
  logic gated on `deps.substrate.(*tmuxSubstrate)` type-asserts (:4378,:4386).
- `extractTmuxAdapterFromSubstrate` (workloop.go:8062-8069): probes for the unexported
  `substrateWithAdapter` interface, used for run-session adoption (:1665).
- **Two composition roots, both hardcode tmux**: cmd/harmonik/main.go:1340 `Substrate:
  daemon.NewTmuxSubstrate(...)` (opts :1319-1326) and cmd/harmonik/run.go:708 (same shape).
- **No substrate-selection config/flag exists** (no env var, no config key). What DOES exist is
  HARNESS selection: `Config.DefaultHarness` (main.go:1350, flag --default-harness, hk-y01k6) via
  `handlercontract.HarnessRegistry` (workloop.go:480) + per-bead `resolveHarness` (claudelaunchspec.go:226).
  Harness selection (claude vs codex argv) is ORTHOGONAL to substrate selection (tmux vs direct) — C2
  needs a new axis or must be keyed off the harness.
- shutdown probes deps.substrate for optional `windowCleaner` (workloop.go:1634-1637); the tmux impl
  is festooned with optional side-interfaces the daemon type-asserts for — a second impl must decide
  which it deliberately does NOT satisfy.
- **Twin-binaries locked decision** (`.harmonik/context/project.yaml:22-27`: "twin-binaries, not
  in-process mocks"). Live twins: cmd/harmonik-twin-{claude,codex,generic,session}.
  claudelaunchspec.go:19-21: "twin-blind: the same code path is used whether Binary points to `claude`
  or `harmonik-twin-claude`." Constraint for C2: the stream-json driver must remain twin-blind — a
  `harmonik-twin-claude` that speaks the same NDJSON wire on stdio is the L2/L3 integration double, NOT
  an in-process fake; corpus-replay Twin[E] covers the unit tier.

---

## Q4 — Process ownership

- **Today: claude is spawned inside a tmux window, stdin owned by the tmux pty.**
  `tmuxSubstrate.SpawnWindow` (tmuxsubstrate.go:848-854) → spawnWindowVia (:868): shell-quotes Argv
  into one command string (:910-917) passed to `tmux new-window` (→ sh -c). StdinDevNull appends
  `< /dev/null` (:922-928) — codex only; handler/substrate.go:70-71: "MUST NOT be set for claude …
  claude does not read stdin and the paste-inject path is unaffected."
- The seam explicitly denies input: handler/substrate.go:97-98 "SendInput and CloseStdin are not part
  of this interface; the substrate owns the child's stdin." Stdout() "returns nil for tmux-hosted
  sessions — the bridge wire is a Unix socket, not a stdout pipe" (:95-96).
- **Direct-child piped-stdin pattern ALREADY EXISTS** in two places:
  1. `internal/handler/session.go:187-260` — NewSession opens cmd.StdinPipe (:196) + owned pipes;
     `handler.Session` has `SendInput(ctx, line)` ("writes line + '\n'", :44-47) and `CloseStdin`
     (:80-83). handler.Launch's non-substrate path already does NDJSON-over-stdin: HC-005 HandlerSpec
     marshaled to compact JSON, written via SendInput + CloseStdin (handler.go:265-288). **This is the
     daemon's existing native shape for "write a JSON line to a child's stdin."**
  2. `internal/apptap/tap.go` — transparent stdio splice built for codex capture (T1, hk-893ct):
     cmd.StdinPipe (:99) with MultiWriter tees; "protocol-agnostic" (:21). ROADMAP homes the apptap
     orphan into M2 (ROADMAP.md:94).
- **Locked decision, verbatim** (`.harmonik/context/project.yaml:26`): "tmux inspectability required".
  Implications for a stdin-owning driver:
  - Decompose already frames it (02-components.md:79-81, C3 :96-100): "does it still spawn via tmux
    (for observation) but write via stdin, or fully own the process?"
  - Existing tmux-hosted-but-not-tmux-fed patterns: (a) codex ProcessExit harness runs in a tmux
    window with `< /dev/null` stdin and argv-delivered input (tmuxsubstrate.go:918-928,
    codexharness.go:129-152 — Seed/Retask no-ops, "task delivered via seed-prompt argv"); (b) shell
    redirection inside the tmux command string already rebinds stdin — a FIFO redirect `claude … < fifo`
    is the same shape, though **no mkfifo/pipe-pane pattern exists in the Go code today** (grep: only
    an unrelated --notify-stream FIFO in cmd/harmonik/run.go:35). (c) A stream-json headless claude is
    NOT a TUI — a tmux window running it shows raw NDJSON, so "inspectability" likely shifts from
    "watch the TUI" to "tail the captured wire" (C4) — an operator/planner call, not settled by any
    repo doc. → PLANNER-RECONCILE.

---

## Q5 — WAL-guard (C7)

- **Name collision:** two WAL things in internal/daemon/: (a) walcheckpoint.go (152 lines, hk-5dewt) —
  a beads.db SQLite checkpoint pre-flight, NOT the target; (b) **`codexwalguard.go` — the C7 target,
  exactly 380 lines** (verified), matching census REPORT.md:35. Homing ROADMAP.md:98 (WAL-guard→M2);
  M2-7 card TASKS.md:103.
- **What it does** (codexwalguard.go:3-49 + `.memory/reference_codex_harness_stale_wal_fastfail.md`):
  codex persists session state in `$CODEX_HOME/state_*.sqlite` (+wal/shm). When a codex run is KILLED
  mid-flight (daemon SIGKILL of a hung implementer, fleet sleep), the leftover WAL corrupts the NEXT
  launch → "<10s 'exited without advancing HEAD'" fast-fail. `cleanCodexStaleWAL` (codexwalguard.go:104+)
  runs once per launch from CodexHarness.LaunchSpec (codexharness.go:91-100): for any present,
  lsof-verified-unheld state_*.sqlite-wal (size-independent since hk-xisvb — a 234 KB WAL fast-failed
  fleet-wide), back up to .wal-backup-<ns>/ (keep last 5) and remove wal+shm; base .sqlite untouched.
  Config codex.stale_wal_max_bytes demoted to a log-classification signal (:29-39,:88-102).
- **Would a real input ack make it redundant? Not directly — the causal fix is graceful lifecycle,
  not the ack.** The guard compensates for UNGRACEFUL TERMINATION OF A STATEFUL CHILD, not lost input.
  A structured driver that owns the child can (1) terminate turns via protocol (turn/interrupt) instead
  of SIGKILL, letting the child checkpoint; (2) detect the fast-fail positively (no initialize/first
  event within bound → structured launch-failure event) instead of the 8s-exit-0 silent no-op; (3) if
  codex moves to the resident app-server sidecar, mid-run kill of per-bead `codex exec` largely
  disappears. Then the guard shrinks to a boot-time recovery step or is deleted. But SIGKILL paths
  survive (hard hangs, daemon crash), so design should decide adapt-vs-delete per the M2-7 card, NOT
  assume delete. Note: the guard is claude-IRRELEVANT (codex-specific $CODEX_HOME state); C7's "fold
  into the rebuilt input path" only makes sense if M2's driver owns codex process lifecycle — otherwise
  its home is the codex sidecar work.

---

## Patterns to follow (codex instantiation idiom)
1. **Package quartet per vertical**: `<x>wire` (codec + method registry + Extra structs) → `<x>reactor`
   (flat Event/Action + pure Step + Run one-liner over substrate.Run) → `<x>digitaltwin` (ReplayCodec
   fusing Parse+filter+frameToEvent, wrapping substrate.Twin) → corpus under testdata/. C2 ⇒
   claudewire/claudereactor/claudedigitaltwin analogs (naming TBD).
2. **Type aliases, not defined types**, for seam instantiations (RS-021 structural identity).
3. **Flat structs with omitempty** for Event/Action (JSON round-trip for scenario files).
4. **Single method registry table** as the sole method-string truth; unknown = Raw passthrough;
   `Extra map[string]json.RawMessage` + parseExtra/mergeExtra on every payload.
5. **Seq-in-codec**: monotonic seq is codec-internal; Seq=0 reserved for lifecycle events bypassing dedup.
6. **Corpus-first**: T0 spike captures a real session via apptap → corpus JSONL → round-trip gate →
   twin replay → reactor scenarios. Claude has NO corpus yet — that spike is the first task.
7. **Ack shape precedent**: handler.Session.SendInput NDJSON line + HC-005 stdin-delivery goroutine
   (handler.go:265-288) is the house style for framed input to a child.

## Known vs assumed
**KNOWN (repo-proven):** codex app-server = JSONL JSON-RPC 2.0 over stdio (full handshake + corpus +
schema); the generic seam API + both instantiations (codex, keeper via keepertwin); claude
`--output-format stream-json` exists but deliberately unadopted at MVH; claude programmatic drive via
`-p --resume <id>` / caller-minted `--session-id`; SDK sessions HTTP API rejected on
subscription-billing grounds; substrate injection hardcoded tmux at two roots with zero selection
mechanism; the seam forbids input by design; direct-child piped-stdin machinery exists
(handler.Session, apptap); WAL-guard = 380-line codex-state kill-recovery.

**ASSUMED (needs planner/operator input):** existence/stability of claude `--input-format stream-json`
persistent-stdin mode and its message vocabulary; whether headless stream-json bills the Max
subscription; what "tmux inspectability required" means for a non-TUI NDJSON child; whether the C2
driver also fronts codex (resident app-server sidecar) or is claude-only with codex following later.

## Risks / conflicts
- **Locked-decision tension**: "tmux inspectability required" vs a stdin-owning driver. Bridgeable
  (tmux-hosted window + stdin redirect, or observation-only capture), but C3's "is the tmux window even
  retained?" edges toward reopening the decision — flag for operator before design lock.
  → PLANNER-RECONCILE.
- **Billing risk**: the project's strongest historical constraint (subscription-not-API billing,
  c2-spec.md:104-109) has never been evaluated against headless stream-json mode. → PLANNER-RECONCILE.
- **Optional-interface sprawl**: the daemon type-asserts ~6 unexported side-interfaces plus
  *tmuxSubstrate concrete asserts inside workloop (:3842,:4082,:4378); a second impl silently misses
  behaviors (window cleanup, run-session adoption, per-run wrap) unless audited — C1 territory but
  constrains C2.
- **No harness-vs-substrate selection axis**: DefaultHarness selects argv shape, not hosting; C2 needs
  a new composition-root selection mechanism where none exists (must also be selectable in L2/L3 test
  harnesses per twin-binaries).
- **WAL-guard scope trap**: C7 is codex-specific; folding it "into the rebuilt input path" only makes
  sense if M2's driver owns codex process lifecycle — otherwise its home is the codex sidecar work. The
  M2-7 card correctly defers adapt-vs-delete to design (rides M2-2/M2-5, off critical path).
