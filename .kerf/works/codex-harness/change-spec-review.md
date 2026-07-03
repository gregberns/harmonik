# Change-spec review — codex-harness

Reviewer: independent sub-agent (validated all paths against the live tree). **Verdict: APPROVE**
with 1 BLOCKING anchor fix + 4 MINOR. All decompose-review fixes (B1/B2/M1–M4) confirmed carried in.
Resolved in-place below.

## Requirement coverage + path validation — PASS
All 23 requirements (R1.1–R6.5) map to spec sections; no orphan spec content. Reviewer independently
validated: `deps.launchSpecBuilder` seam (`dot_cascade.go:~520-523`), `AgentType` enum
(`internal/core/agenttype.go:14-18`, `AgentTypeClaudeCode="claude-code"`, no codex yet), env-strip
(`claudehandler_chb006_024.go:196-204/292-296`), `RunHeartbeatLoop` CHB-019 (`:588`), reviewer reuses
`buildClaudeLaunchSpec` (`reviewloop.go:227`/`:814`), DOT generic attr-map (`dotparser.go`
parseAttrList — "parsing is free" correct), `HandlerRef` does not select a binary
(`dot_cascade.go:801-805`), queue `Item`/`Group` (`queue/types.go:132,205`), twin-binary pattern real
(`internal/handler/twinlaunch.go`, HC-045).

## Findings + resolutions

### [BLOCKING] C5 — completion-mode gate pointed at the wrong file
The `/quit`+heartbeat-staleness+kill machinery is entirely inside `pasteInjectQuitOnCommit`
(`pasteinject.go`), **launched from `dot_cascade.go:643`** (`go pasteInjectQuitOnCommit(...)`), NOT
`workloop.go` (which holds only the bare `sess.Wait`, `:2265`). An implementer following the spec
would edit the wrong file and the codex stale-kill bypass (R2.7/B1) would silently not take effect.
**Resolution:** C5 retargeted — the `Completion()==ProcessExit` branch skips/replaces the
`go pasteInjectQuitOnCommit(...)` call at `dot_cascade.go:643` (and the analogous reviewer site);
`workloop.go` only holds `sess.Wait`.

### [MINOR] C1 — DetectReady + MintClaudeSessionID anchors drifted
Actual `DetectReady` = `internal/handler/adapter_claudecode.go:99` (impl lives in `internal/handler/`,
not `internal/handlercontract/`); `MintClaudeSessionID` = `:119` not `:147`. **Resolution:** corrected
in C1 + the current-harness research note.

### [MINOR] C1 AC1.2 — "byte-identical" golden test unachievable as worded
`buildClaudeLaunchSpec` performs file-materialization side effects today; a golden test can't be
byte-identical to a function with side effects. **Resolution:** AC1.2 scoped to the pure
`(argv,env,cwd)` return; new AC1.6 asserts the shared scaffolding
(`MaterializeClaudeSettings`/`EnsureWorktreeTrust`/`WriteAgentTask`/`PreExecMessages`) still fires
from the caller (side-effect parity).

### [MINOR] C3 — make the inherited-CODEX_HOME coverage explicit
R3.4 fixes `$CODEX_HOME` but doesn't assert against inheriting a pre-existing `CODEX_HOME` pointing at
a logged-out home; `assertChatGPTPlan` (R3.3) catches it transitively (login status fails → fail
closed). **Resolution:** C3 states this explicitly so it's not read as an assumption.

### Adversarial re-challenge outcomes
- **Billing (C3):** sufficient as defense-in-depth, NOT over-engineered. The one residual hole (#2000
  auto-generated org key a subscription login can silently route to) is honestly externalized to the
  C6 pre-production org-audit rather than claimed safe. Approved.
- **Seam (C1):** right size. `Completion()`/`SessionIDPolicy()` correctly capture the divergence; the
  claude-iter≥2-is-already-a-fresh-process insight is sound. The only true leak (per-handler heartbeat
  emitter) is the BLOCKING C5 anchor — fixed.

## Outcome
APPROVE; all findings resolved in-place. No DAG/structure change. Advancing to integration.
