# Claude Session Lifecycle + Bidirectional Messaging — Audit

Date: 2026-05-13
Author: research agent (post-smoke-v3 RED analysis)
Status: audit; informs new umbrella bead

## 0. TL;DR — the gaps

1. **No task is ever delivered to claude.** `buildClaudeLaunchSpec` (`internal/daemon/claudelaunchspec.go:209-214`) constructs argv as exactly `["--session-id", <uuid>]` or `["--resume", <uuid>]`. There is no prompt, no task file, no piped input. Claude spawns into an interactive idle TUI. HC-056 fires `agent_ready_timeout` because nothing ever gives claude something to do.
2. **The specs never define a daemon→claude task channel for the interactive substrate.** CHB and HC describe the bridge wire (hook-relay → daemon socket) and the daemon's pre-exec emission ordering, but neither spec normatively defines *how the daemon hands work to claude*. Under the headless `--print` model this is `claude -p '<prompt>'`. With `--print` forbidden by HC-055 and headless mode user-locked out, no replacement path is specified.
3. **`SubstrateSession.SendInput` is a silent no-op.** `internal/handler/substrate.go:110-112`. The tmux substrate has no `send-keys`, `load-buffer`, or `paste-buffer` operation. The tmux adapter interface (`internal/lifecycle/tmux/adapter.go`) exposes only `NewWindowIn`, `KillWindow`, `WindowPanePID`, `KillSession`, `List{Sessions,Windows}`. There is no way to write to a tmux pane's stdin from go.
4. **No claude→daemon channel beyond hook events.** Hooks (`SessionStart`, `Stop`, `SessionEnd`, `StopFailure`, `Notification`) are the only events claude sends back. `Stop` carries an outcome but only at the end of a turn. There is no intermediate progress feed, no per-message stream, no per-tool-call signal that the daemon can use for impl→reviewer cycle handoff or operator observation.
5. **Twin-claude is fed a YAML script at launch time.** `cmd/harmonik-twin-claude/main.go:148-156`. It does NOT receive a task from the daemon — it has a pre-baked scenario. The real-claude path therefore has nothing to mirror; the twin papers over the gap.
6. **`.claude/settings.json` is the only file the bridge currently materializes.** No "task file" mechanism exists. `internal/workspace/MaterializeClaudeSettings` only writes settings.json. No other file is written into the worktree for claude to read.
7. **The impl→reviewer cycle assumes a verdict file but does not specify how reviewer is *told what to review*.** `reviewloop.go` launches reviewer with the same argv as implementer (just `--session-id <fresh-uuid>`). The reviewer has no way to know which diff to review, which bead, which spec, what the prior verdict said. The verdict file is the output; there is no input artifact.
8. **No follow-up message channel between iterations.** `implementer_resumed` event carries `prior_verdict_summary` on the bus (event-model.md §8.1a.1), but this is daemon→bus, NOT daemon→claude. The resumed claude session resumes its *own* session transcript via `--resume <uuid>`, but the daemon never sends "here's the reviewer's feedback, address these flags."
9. **No `--dangerously-skip-permissions` / trust mechanism in the daemon's launch path.** HC-055 deny-list rejects `--permission-mode` flag, but trusts the worktree implicitly. claude will prompt for trust on a never-before-opened directory. With no human at the keyboard the session hangs at the trust prompt and HC-056 fires.
10. **Hook-relay back-channel is sparse: 5 event kinds, no payload from daemon.** The daemon-to-handler control messages catalog at handler-contract.md §6.4 (`version_selected`, `cancel`, `shutdown`, `rotate_account`) is defined but is over the *handler subprocess stdin pipe* — which under the tmux substrate doesn't exist (the substrate owns the pty; `SendInput` is a no-op).

## 1. Spec summaries

### 1.1 `specs/claude-hook-bridge.md` (CHB-001..027)

**Covers:** settings.json materialization (§4.1); env-var schema (§4.2); pre-generated `claude_session_id` flow (§4.3); the `harmonik hook-relay <event-kind>` subcommand (§4.4); hook→progress-message mapping for the 5 supported hook events (§4.5); daemon-socket protocol (§4.6); handler-process pre-exec emission ordering and terminal events (§4.7); twin parity (§4.8); stop-hook dedup (§4.10). It is normative for the claude-code agent type.

**What it does NOT cover:**
- **How the daemon delivers a task / prompt / message to claude.** Section 2 ("In scope") lists settings.json, env-var schema, the relay subcommand, hook-event mapping, the session-id flow, twin parity. Nothing about prompts, task files, or daemon→claude messaging. There is no mention of `--print`, no mention of a task-file path, no mention of `tmux send-keys`. Section 11 ("Informative — alternative architecture") discusses the `--print` stream-json path as a *post-MVH evolution* — implying MVH bridges hook events of an *already-running interactive claude session*, but never says how that session is told what to do.
- **How the daemon sends arbitrary follow-up messages to claude** after launch. The "two-contributor model" (§3 glossary, §4.6) describes handler-process and relay-subprocesses *writing to the daemon* — never the other direction.
- **What `agent_ready` means in the tmux/interactive context.** CHB-018 says the handler emits `agent_ready` BEFORE exec'ing claude. So `agent_ready` is "the daemon is done setting up env + settings.json + pre-exec messages and is about to launch claude". It is NOT "claude is alive and accepting input". The HC-056 30s timeout then fires on this synthesized agent_ready, which always emits eagerly — so what is being timed out? The implementation in `reviewloop.go` and `workloop.go` wires `waitAgentReady` against `tapCh` which observes the *bus event* of agent_ready, which the daemon itself emits pre-exec via `preExecMsgs`. So this is racy/broken in concept.

**Gap:** the spec's load-bearing assumption is that claude is launched and *self-directs* via reading transcripts. There is no model of "the daemon sends claude work."

### 1.2 `specs/handler-contract.md` (HC-001..057)

**Covers:** the cross-handler interface; LaunchSpec record; wire protocol on the daemon socket (NDJSON, framing, version negotiation); error taxonomy; pre-`agent_ready` ordering invariant (HC-INV-004); allowed claude CLI flags at MVH (HC-055); `agent_ready` timeout (HC-056); heartbeat-emission carve-out for claude (HC-057); `Session.Attach()` returning a live pty stream for claude (HC-054).

**Normative re session lifecycle:**
- HC-005: LaunchSpec delivery is JSON-on-stdin (or `--launch-spec <path>` for > 1 MiB). The `HandlerSpec` field of `handler.LaunchSpec` carries this. But **HC-005 is silent on the actual *prompt* / task content** — `LaunchSpec` is metadata (run_id, workspace_path, required_skills, freedom_profile_ref, ...) NOT the work prompt.
- HC-055: argv allow-list is exactly `--session-id` or `--resume`. No `--print`. No way to pass a prompt on argv. Operator `Config.HandlerArgs` is appended verbatim and validated against the CHB-007 deny-list.
- §6.1 `Session.SendInput(ctx, input) -> error` — declared as a Session method. Production implementation: real `exec.Cmd` writes to stdin pipe; **substrate session: no-op** (substrate.go:110).
- §6.4 "Daemon-to-handler control messages" catalog: `version_selected`, `cancel`, `shutdown`, `rotate_account`. All NDJSON framed on the same socket. But these are over the **handler-subprocess stdin pipe**, which under the tmux substrate does not exist.

**Gap:** HC presumes a handler subprocess with stdin/stdout to which the daemon writes NDJSON. Under PL-021b (tmux substrate, which is now required), the handler IS claude, and claude does not speak NDJSON on stdin/stdout. HC-054 acknowledges this for `Attach()` (returns a live pty stream) but offers no replacement for SendInput / control-messages.

### 1.3 `specs/process-lifecycle.md` (PL-021b et al.)

**Covers:** the direct-tmux substrate; pane creation via `tmux new-window`; tmux availability probe; session resolution (`$TMUX`-reuse or named session); window-name determinism; substrate seam (LaunchSpec.Substrate field); wait/kill discipline; window-name in `agent_started`; pane orphan recovery in PL-006.

**Normative for substrate:** subprocesses requiring interactive-pty MUST be spawned via `tmux new-window -e KEY=VALUE ... -- <binary> <argv>`. The daemon MUST NOT spawn via `exec.CommandContext` for these. **The daemon MUST NOT read pane stdout via `tmux pipe-pane`** (PL-021b §5). All bridge-protocol messages flow through the daemon's Unix socket via hook-relay.

**Gap:** PL-021b §5 explicitly forbids reading pane stdout. It says nothing about whether the daemon may *write* to the pane via `tmux send-keys`. The spec assumes the only daemon→claude channel needed is settings.json + env vars at launch — i.e. that whatever instructs claude is already on disk in the worktree when claude starts.

### 1.4 `specs/execution-model.md` review-loop semantics (EM-015d, EM-015e)

**Covers:** the hardcoded `implementer → reviewer → {APPROVE: close, REQUEST_CHANGES: implementer, BLOCK: close, cap-hit: close, no-progress: close}` cycle. Run.context keys: `iteration_count`, `last_verdict`, `claude_session_id`, `last_diff_hash`. Verdict file at `.harmonik/review.json` (and `review.iter-<N>.json` archive). `claude --resume <id>` carries the same claude session across implementer iterations (so claude's *own* transcript is the continuity vehicle).

**Gap:** the spec is silent on:
- How the *first* implementer iteration is told what to implement. (Bead-tied? Where does the bead description become a prompt?)
- How the reviewer is told what to review. (The diff is in the worktree, but is the reviewer expected to know to read it? Run `git diff`? Compare against what base?)
- How the implementer-resume launch is told the reviewer's feedback. claude's `--resume` reopens the *implementer's* transcript, which is opaque to the daemon and does not contain reviewer feedback. The `implementer_resumed` event carries `prior_verdict_summary` on the bus but the spec does not name a mechanism for the resumed implementer to *see* it.

### 1.5 `specs/architecture.md`

Frames the system into S01..S07 subsystems. The handler contract is the seam between S01 (deterministic daemon) and execution shape (S04 agent runner). Mechanism/cognition tagging discipline. Twin parity. Largely orthogonal to this audit, but the cognition seam matters: any "daemon constructs a prompt" path is mechanism (deterministic), any "claude reads the prompt and decides what to do" is cognition (in claude). The daemon-side prompt-construction layer is *not specified*.

## 2. Code surface map

### 2.1 `internal/daemon/claudelaunchspec.go` — buildClaudeLaunchSpec
Implemented per CHB-008/CHB-018. **MISSING:** no task delivery. argv ends at `--session-id <uuid>`. preExecMsgs are bus events the daemon emits to itself before launching claude — they don't reach claude.

### 2.2 `internal/handler/handler.go` — Handler.Launch
- Non-substrate path (production fallback): writes spec.HandlerSpec (a `handlercontract.LaunchSpec` record — metadata, not prompt) to subprocess stdin and closes stdin. The subprocess (twin) reads it; claude would not.
- Substrate path: skips HandlerSpec delivery entirely. **"the LaunchSpec is injected via env vars (CHB-006) or the hook-bridge socket instead"** (comment at line 268) — but the hook-bridge socket is one-way relay → daemon. No spec or code path delivers anything via "hook-bridge socket" from daemon → claude.

### 2.3 `internal/handler/substrate.go` — Substrate / SubstrateSession
- `SubstrateSpawn{WindowName, Cwd, Env, Argv}` — launch-time only. No post-launch operations.
- `substrateSessionAdapter.SendInput` returns nil silently. No tmux send-keys.
- `Stderr() io.Reader` returns nil. `Stdout() io.Reader` returns nil for tmux. `CloseStdin()` returns nil.

### 2.4 `internal/lifecycle/tmux/adapter.go` — Adapter
Methods: `ProbeTmux`, `ListSessions`, `ListWindows`, `NewWindowIn`, `KillWindow`, `WindowPanePID`, `KillSession`. **No `SendKeys`. No `LoadBuffer` / `PasteBuffer`.** Cannot write to a pane.

### 2.5 `internal/daemon/tmuxsubstrate.go`
Implements Substrate via the tmux Adapter. SpawnWindow → NewWindowIn. Kill = SIGTERM-then-SIGKILL the pane PID + `tmux kill-window`. Wait = poll WindowPanePID at 500ms. **No bridge for daemon→claude messages.**

### 2.6 `internal/daemon/reviewloop.go`
Calls `buildClaudeLaunchSpec` once per phase. Argv: same as single-mode for impl-initial / reviewer; `--resume` for impl-resume. Captures `claude_session_id` from `handler_capabilities` via stdout interceptor — relevant only on the non-substrate path because under tmux the stdout interceptor sees nothing. **MISSING:** no mechanism for delivering the bead description (initial task), no mechanism for delivering reviewer feedback (impl-resume), no mechanism for telling the reviewer what to review.

### 2.7 `internal/daemon/hookrelay_chb025.go`
Daemon-side socket acceptor for hook-relay envelopes. Per-session store keyed by (run_id, claude_session_id). Last-received-wins outcome dedup. **One-way (relay → daemon). No path to push to claude.**

### 2.8 `cmd/harmonik/main.go` — hook-relay subcommand
Reads stdin JSON from claude's hook invocation; writes one NDJSON envelope to the daemon socket; reads ACK. **One-way.**

### 2.9 `cmd/harmonik-twin-claude/`
- `main.go`: parses `--socket-path`, `--launch-spec`, `--script-path`, `--scenario`. **Reads a script file** then plays back NDJSON over a UDS or stdout.
- `scriptdriver.go` / `wire.go`: script playback engine.
- **The twin does not receive work from the daemon.** It is pre-scripted. So the impl/reviewer cycle in twin mode looks like work-is-being-done but no work-delivery code is exercised.

### 2.10 What works
- Settings.json materialization, including absolute binary path (hk-kqdpf.6).
- Hook-relay subcommand and daemon-side acceptor.
- tmux substrate spawn + kill.
- claude_session_id mint + persist + resume.
- preExecMsgs (handler_capabilities, session_log_location, skills_provisioned, agent_ready) on the bus.
- Orphan sweep (sessions + windows).

### 2.11 What does not work end-to-end
- Real claude given a real task.
- Real claude → real review.json reaching real disk via real claude executing real edits.
- Real impl-resume actually seeing the reviewer's prior verdict.
- Real reviewer knowing what to review.

## 3. Cycle gaps — impl→reviewer→impl

The review-loop spec defines event ordering and verdict-file location but does not specify the **input artifacts** each phase receives. Required input artifacts per phase (none of which the daemon currently writes):

| Phase | Needs to know | Currently delivered? |
|---|---|---|
| `implementer-initial` | the bead description / task / spec to implement | NO |
| `implementer-resume` | the prior reviewer's verdict notes + flags + diff context | NO (`--resume` reopens claude's own transcript but reviewer feedback is not in it) |
| `reviewer` | which diff to review (base SHA + head SHA), the bead context, the reviewer skill instructions | NO (the verdict file is the *output*, no input file exists) |

The CLAUDE.md skill registry mentions `/agent-reviewer` skill, but skills are claude-side capabilities — they don't carry per-invocation task content. A "what to review" handoff is still missing.

Bidirectional message passing requirement (user-stated): the daemon must be able to:
- send claude the initial task on session start;
- send claude follow-up messages mid-session (e.g., reviewer feedback for impl-resume);
- receive completion signals (have via Stop hook + verdict file);
- receive intermediate progress (have via Notification hook → agent_heartbeat — but only as `phase: reasoning|waiting_input` strings, no content);
- receive arbitrary outputs from claude (NOT IMPLEMENTED — claude writes to its transcript, not to a daemon-readable channel).

None of these except completion + low-fidelity heartbeats are implemented.

## 4. Risk register — what next smoke will surface unless addressed

1. **Trust prompt.** Real claude on a fresh worktree will prompt "Trust this directory?" interactively. With no human, the prompt hangs. HC-056 fires. Need either `claude --dangerously-skip-permissions`, or a `~/.claude/projects/<slug>/` pre-seeded trust marker, or `.claude/settings.local.json` with `permissionMode` (which HC-055 explicitly forbids).
2. **Auth.** Real claude needs an API key or OAuth session. Env vars must carry it, redacted per HC-028. Not currently materialized.
3. **Initial-task delivery.** Even with trust, claude opens an empty TUI prompt. Need a file claude reads at session-start (NOT CLAUDE.md per user constraint — user's CLAUDE.md exists in worktree and must not be overwritten; pick a different name like `.harmonik/task.md` or `.harmonik/initial-prompt.md`), and a `SessionStart` hook variant that paste-injects "read .harmonik/task.md and follow it" into the pane via `tmux send-keys`. Or a different mechanism entirely (e.g. claude's `--append-system-prompt`, which is on HC-055 deny-list by omission and would need amending).
4. **Reviewer task delivery.** Same as #3 but with a *different* task file or a phase-keyed task file (e.g. `.harmonik/reviewer-task.md` vs `.harmonik/implementer-task.md`).
5. **Impl-resume feedback delivery.** Either (a) write reviewer feedback to a file claude reads at resume start, or (b) `tmux send-keys` it into the pane after `--resume` has restored the prior transcript.
6. **`tmux send-keys` reliability.** Sending multi-kilobyte messages via `send-keys` is racy (tmux input-buffer limits, paste-buffer timing). The robust mechanism is `tmux load-buffer` + `paste-buffer`. Need to add to tmux.Adapter.
7. **Session log path derivation.** `DeriveCIaudeTranscriptPath` constructs a path under `~/.claude/projects/<slug>/<uuid>.jsonl`, but if claude doesn't actually create that file (because it never gets a task), the path emission is fictional. session_log_location emission is technically valid but operationally vacuous.
8. **agent_ready semantic confusion.** Daemon emits agent_ready pre-launch (per CHB-018). HC-056 then waits for it via tap. waitAgentReady observes the daemon's own emission, not claude's. The 30s timeout never sees a claude-originated signal. Either the bus tap is observing the wrong signal, or HC-056 is testing the wrong thing. Needs reframing: define "ready for work" as something claude actually emits (first SessionStart hook? First Notification?), and time *that*.
9. **Idempotency of task delivery.** If the daemon restarts mid-session and the worktree already has a `.harmonik/task.md`, do we overwrite? Re-paste into the pane? Need a contract for re-attach.
10. **Reviewer verdict-file write timing.** Currently `Stop` fires after each turn; the reviewer is expected to write `review.json` before its last Stop. If the reviewer never gets a task it never writes; the Stop fires but the file is absent; relay packages `error: missing_review_file` (CHB-014). This is the failure path we are observing in smoke RED.

## 5. Spec gaps vs implementation gaps

**Spec gaps (specs are silent — must be written):**
- A clause in CHB or a new spec section defining the daemon→claude task-delivery channel under the tmux substrate. Pick a mechanism: file-on-disk consumed by a settings.json `SessionStart` hook that paste-injects the read instruction, OR direct `tmux send-keys` of the task content as input. Either way, normative.
- A spec section defining the reviewer phase's input artifacts (task file or pasted instruction) and where the bead context lives.
- A spec section defining the impl-resume reviewer-feedback delivery mechanism.
- An amendment to PL-021b or a new clause permitting `tmux send-keys` / `tmux load-buffer + paste-buffer` for daemon → pane writes (currently PL-021b §5 forbids reading via `pipe-pane`; the symmetric writing case is unspecified).
- A trust-prompt resolution clause (worktree auto-trust mechanism that does not depend on the deny-listed flags).
- A reframing of `agent_ready` semantics for the interactive/substrate path, so HC-056 is meaningful.

**Implementation gaps (specs are clear, code is missing):**
- HC-055 / CHB-006: env vars are wired. (Implemented.)
- CHB-001..005: settings.json materialization. (Implemented.)
- HC-005: stdin LaunchSpec delivery exists for non-substrate path, no-op for substrate path — this matches the spec's silence. (Spec gap, not impl gap.)
- HC-056: `agent_ready` timeout implemented, but observing the wrong signal — implementation gap downstream of the spec gap on "what is agent_ready in interactive mode."

**Mixed (spec is hand-wavy AND code is stubbed):**
- `SubstrateSession.SendInput` no-op: spec defines `Session.SendInput` (HC-006.1) but does not say what it means under PL-021b. Both spec clarification and code implementation needed.
- Hook event coverage: spec defines 5 events; code wires them. But the daemon→claude direction has 0 events in spec and 0 code paths. Both needed.

## 6. Proposed scope for the new umbrella bead

Suggested umbrella codename will be auto-generated by `br`. Proposed child beads (8 concrete, sized for 1–2-hour implementers):

### B1 — Spec: define daemon→claude task-delivery file (P0)
Amend CHB (or new section `CHB-028 — Task-file delivery`) defining `${workspace_path}/.harmonik/agent-task.md` (NOT CLAUDE.md per user constraint) as the per-launch task artifact. Daemon writes it atomically before launching claude. Atomic write follows WM-027a discipline. File name reserved by harmonik; collisions are errors.

### B2 — Spec: define daemon→claude pane-paste mechanism (P0)
Amend PL-021b with a new clause permitting `tmux load-buffer -b <name> <file>` + `tmux paste-buffer -b <name> -t <target>` (or `send-keys -l`) for daemon→pane writes. Spec the buffer-name discipline (deterministic per session) and the cleanup obligation.

### B3 — Spec: define agent_ready semantics under the interactive substrate (P1)
Reframe HC-056 / HC-039 / HC-041 so `agent_ready` is observable from a real claude-originated signal under PL-021b. Candidate: first `SessionStart` hook landing on the relay. Update CHB-013 mapping accordingly. Resolve the current daemon-self-emits-agent_ready-pre-launch oddity.

### B4 — Spec: define impl-resume feedback delivery (P1)
Amend EM-015d to require the daemon, before launching an `implementer-resume` phase, to write `${workspace_path}/.harmonik/reviewer-feedback.iter-<N-1>.md` containing the prior verdict notes + flags + diff summary, AND to inject "read that file" into the resumed pane via the B2 paste mechanism.

### B5 — Spec: define reviewer input artifacts (P1)
Amend EM-015d to specify reviewer's input: `${workspace_path}/.harmonik/review-target.md` (bead context, base+head SHAs, prior verdicts if any). Reviewer is told via paste-inject to read it and produce `review.json`.

### B6 — Code: add `tmux.Adapter.LoadBuffer` and `PasteBuffer` (or `SendKeys`) (P0)
Extend `internal/lifecycle/tmux/adapter.go` interface with the methods needed by B2. Implement in `osadapter.go`. Add `internal/handler/substrate.go` method (e.g. `SubstrateSession.PasteText(ctx, text)`) and wire `tmuxSubstrateSession.PasteText` accordingly. Depends on B2.

### B7 — Code: write `.harmonik/agent-task.md` before claude launch (P0)
In `buildClaudeLaunchSpec` (or a new step prior to substrate spawn), construct the task content from the bead description + phase + skills + freedom-profile and write it atomically. For initial implementer: bead description. For reviewer: `review-target.md` (B5). For impl-resume: `reviewer-feedback.iter-<N-1>.md` (B4). Depends on B1, B4, B5.

### B8 — Code: paste-inject "read your task file and begin" after pane spawn (P0)
After `SpawnWindow` returns the pane handle, the daemon writes a deterministic kick-off message into the pane via the B6 mechanism. Kick-off message names the task file path. Depends on B6, B7.

### B9 — Code: worktree auto-trust resolution (P0)
Pick a mechanism that does NOT use HC-055-denied flags. Candidates: pre-seeding `~/.claude/projects/<slug>/.trusted`, writing into `~/.claude/settings.json` (user-level) under the trusted-projects list, or using `claude config set hasTrustDialogAccepted true --path <worktree>`. Spec the chosen path; implement.

### B10 — Code: fix `agent_ready` observation (P1)
Per B3, switch waitAgentReady's signal source from the daemon's self-emitted pre-launch event to a real claude-originated event (first hook arrival). May require adding a synthetic progress-message at hookrelay receipt time. Depends on B3.

### B11 — Beads hygiene (P2)
Close / supersede `hk-kqdpf.9` (task injection bead — superseded by B1+B7+B8) and `hk-kqdpf.10` (bead-stuck IN_PROGRESS bug — addressed by fixing the root-cause cascade in B7+B8+B10 plus a dedicated retry-path fix carried over).

## 7. Open questions for the umbrella to resolve

- Should the reviewer feedback be a single file or an append-log across iterations? (Argues for append-log so claude can see the trajectory.)
- Should the task file include the bead description verbatim, or a daemon-constructed prompt that wraps it? (Daemon-constructed wrapper is more flexible but is mechanism with embedded cognition-by-template — needs an architecture review.)
- For arbitrary mid-session message passing (a use case the user calls out): is "paste-inject into pane" the right primitive, or do we want a side-channel like a file claude polls? Pasting into the pane interrupts whatever claude is doing, which is fine for "here's the next instruction" but bad for "FYI here's some context." Probably both modes need a spec.
- Does the operator need to see what the daemon paste-injects? (Yes — visibility is core. The pane will show it, so this is automatic; but the structured-log audit trail must also capture it.)
