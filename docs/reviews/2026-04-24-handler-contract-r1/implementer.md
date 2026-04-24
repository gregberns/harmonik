# Round 1 Implementer Review — handler-contract.md v0.1

## Verdict summary

The spec is implementable in broad strokes: the `Handler` / `Session` / `Adapter` interfaces (§6.1) are concrete enough to stub; the six-sentinel taxonomy with `errors.Is` routing (§4.5, §8) lines up cleanly with Go idiom; the launch handshake pseudocode (§7.2) and the silent-hang table (§7.1) give me real state machines to code against. Twin parity (§4.8) is a strong architectural pin that makes the `claude-twin` binary straightforward to sketch.

Where I get stuck: the on-the-wire encoding of the progress stream is deferred to "event-model §3.1" for the envelope but the transport framing itself (length-prefix? newline-delimited JSON? CBOR?) is not declared here OR mentioned as deferred. The LaunchSpec-over-stdin vs. file-path selection rule is clear but the reverse direction — how the handler signals "LaunchSpec consumed" — is unspecified. Skill provisioning semantics (idempotency across re-launches, cleanup on failure, per-agent-type install shape) have mostly been pushed to "agent-configuration" which does not exist yet, so the Claude Code handler implementer has no normative guidance for *how* to install a skill into a live subprocess. Rate-limit clearance detection is described as "the adapter detects clearance" but there is no event or condition that the adapter observes to do so.

Roughly one-third of the six requirements I attempted are genuinely stuck on missing detail; the other two-thirds are implementable with minor clarifications.

## Sketches

### HC-001 / HC-002 — Handler and Session interface — IMPLEMENTABLE

```go
type Handler interface {
    Launch(ctx context.Context, spec LaunchSpec) (Session, error)
    AgentType() string
}

type Session interface {
    ID() SessionID
    SendInput(ctx context.Context, input []byte) error
    Attach(ctx context.Context) (io.Reader, error)
    Kill(ctx context.Context) error
    Wait(ctx context.Context) (execmodel.Outcome, error)
    LogLocation() string
}

type claudeCodeHandler struct {
    binPath        string
    expectedCommit string
    adapter        Adapter
    socketPath     string
    redactor       *redaction.Registry
}

func (h *claudeCodeHandler) Launch(ctx context.Context, spec LaunchSpec) (Session, error) {
    if err := verifyCommitHash(h.binPath, h.expectedCommit); err != nil {
        return nil, fmt.Errorf("claude-code: %w", errors.Join(err, ErrStructural))
    }
    // launch handshake per §7.2 ...
}
```

§6.1 schemas are enough to get both interfaces and the skeleton `Launch` method compiling. `AgentType()` returning a plain string is fine; §9.1 cross-refs architecture §1.6a for the URN shape (`harmonik.agent.claude-code`) but the Handler contract itself does not declare the string-format rule — I would cite architecture for it. Not a blocker.

### HC-004 + §4.9 + §4.11 — Session lifecycle spawn → ready → chunks → completed/failed — PARTIALLY

```go
func (h *claudeCodeHandler) Launch(ctx context.Context, spec LaunchSpec) (Session, error) {
    existing := h.sessionByKey(spec.RunID, spec.NodeID)
    if existing != nil {
        if existing.terminating { return nil, ErrTransient }
        return existing, nil
    }
    subproc, err := h.spawnSubprocess(ctx, spec)  // resolved abs path; env carries HARMONIK_SECRET_*
    if err != nil { return nil, fmt.Errorf("spawn: %w", ErrStructural) }
    watcher := h.spawnWatcher(subproc, spec)  // S01-owned per HC-011
    caps, err := watcher.Await("handler_capabilities", 5*time.Second)
    if err != nil { return nil, ErrProtocolMismatch }
    version := negotiate(h.supportedVersions(), caps.Versions)
    if version == 0 { return nil, ErrProtocolMismatch }
    watcher.Send("version_selected", version)
    sl, _  := watcher.Await("session_log_location", 10*time.Second)
    sp, _  := watcher.Await("skills_provisioned", spec.Timeout)
    rdy, _ := watcher.Await("agent_ready", spec.Timeout)
    return newSession(subproc, watcher, sl.LogPath), nil
}
```

Stuck points:

1. §4.2.HC-007 says the handler subprocess emits events on a "named pipe (or Unix socket, or file chosen at launch; transport is the handler's choice subject to per-handler spec)." So the daemon watcher must be prepared to `Dial()` across three transports per agent type. Per-handler spec doesn't exist for claude-code yet; the implementer invents. Could be trimmed — "Unix socket over `.harmonik/daemon.sock` per HC-044" is effectively already normative; the "or named pipe, or file" branch is slack the contract does not need.
2. §4.2.HC-008: "Subprocess exit status MUST be treated as a liveness signal only." What is the timeout between `outcome_emitted` being received and the subprocess actually exiting? If the subprocess hangs for 90s after emitting outcome, is that a structural failure or normal? The silent-hang machine is keyed off progress events, but post-outcome shutdown is a different regime; the spec is silent.
3. §4.9.HC-039 "emit a single `agent_ready` event on process startup": I need to know the event's payload shape to write the adapter's `DetectReady`. §6.4 defers payload schema to event-model §3.2 which I cannot read here. §4.9 says `session_id` and `capabilities[]` "at minimum" — is the adapter allowed to pattern-match on `capabilities[]` content, or is its presence sufficient?

### HC-046 / HC-047 / HC-049 — Skill injection resolve → provision → emit — STUCK

```go
func (h *claudeCodeHandler) provisionSkills(ctx context.Context, spec LaunchSpec) ([]ProvisionedSkill, error) {
    var out []ProvisionedSkill
    for _, name := range spec.RequiredSkills {
        var resolved string
        for _, base := range spec.SkillSearchPaths {
            candidate := filepath.Join(base, name)
            if _, err := os.Stat(candidate); err == nil {
                resolved = candidate
                break
            }
        }
        if resolved == "" {
            return nil, fmt.Errorf("%w: skill %q not resolved in %v",
                ErrSkillProvisioningFailed, name, spec.SkillSearchPaths)
        }
        // ??? how do I actually "provision" this into a Claude Code subprocess?
        out = append(out, ProvisionedSkill{Name: name, SourcePath: resolved})
    }
    return out, nil
}
```

Stuck points:

1. §4.11.HC-046 declares the obligation ("available in the agent-type-specific shape: file drops, CLI binaries on PATH, MCP registrations, reference-doc bundles") but defers per-handler installation shape to a future `agent-configuration` spec. The Claude Code handler implementer has no guidance on whether a "skill" is a directory copied into `~/.claude/skills/`, a binary symlinked into `$PATH`, or a `--mcp-config` flag. Given that Claude Code today reads `.claude/skills/` (per docs at `docs/foundation/components.md` §Section 10), the convention exists but is not normative here. Minimum: cite `[docs/foundation/components.md §10]` as the bootstrap surface until agent-configuration lands.
2. §4.11.HC-047 says skill resolution is "first match against `skill_search_paths[]` in order." What constitutes a "match"? Directory with the right name? File with the right name? An `index.yaml` manifest at `<path>/<name>/index.yaml`? The compile-time-deterministic rule is clear; the "what does a skill look like on disk" is missing.
3. §4.11.HC-049 says emit `skills_provisioned` "carrying the set of installed skills, the source path resolved for each skill, and the skill package version where available." The `skill package version` field source is unspecified — read from a manifest? From the directory name? The payload schema sits in event-model, which this review can't see, but the emission-side contract is under-specified.
4. Idempotency is not declared: if a skill is already present from a prior launch in the same worktree (handler restarts, re-launch), does provisioning re-copy, no-op, or fail? For Claude Code where skills live in `.claude/skills/` in the workspace, this matters.

### HC-026 + §7.1 — Silent-hang detector — IMPLEMENTABLE

```go
type hangDetector struct {
    t              time.Duration
    last           time.Time
    state          hangState  // active | warning | softTerminating | hardTerminating
    killCh         chan os.Signal
    emit           func(event)
}

func (d *hangDetector) OnTick(now time.Time) {
    elapsed := now.Sub(d.last)
    switch d.state {
    case active:
        if elapsed >= d.t {
            d.state = warning
            d.emit(agentWarningSilentHang{})
        }
    case warning:
        if elapsed >= 2*d.t {
            d.state = softTerminating
            d.emit(agentSoftTerminating{})
            d.killCh <- syscall.SIGTERM
        }
    case softTerminating:
        if elapsed >= 4*d.t {
            d.state = hardTerminating
            d.emit(agentHardTerminating{})
            d.killCh <- syscall.SIGKILL
        }
    }
}

func (d *hangDetector) OnEvent(e event) {
    d.last = time.Now()
    if d.state == warning { d.state = active; d.emit(agentResumedAfterWarning{}) }
}
```

The §7.1 table is dense enough to drive the switch statement above directly. Two small gaps:

1. The timer-tick cadence is unspecified. With `T = 120s`, a 1s tick is fine, but a 10-minute tick would silently extend the effective threshold. Should be declared (e.g., "watcher ticks at ≤ T/10").
2. Observation: the `soft-terminating → hard-terminating` transition fires on "now - last_progress_event_at >= M_hard," which is `4 * T`. The state machine in the warning row fires at `M_soft = 2 * T`. So from the last event, soft happens at 2T and hard at 4T (not soft + 2T). Confirming the absolute-from-last semantic in a one-line note would save an implementer from misreading "delta from soft-terminate entry."

### HC-025 — Rate-limit detection + emission — PARTIALLY

```go
func (a *claudeCodeAdapter) DetectRateLimit(e event) (bool, time.Duration) {
    if e.Type != "agent_output_chunk" { return false, 0 }
    if m := rateLimitRegex.FindSubmatch(e.Payload.Chunk); m != nil {
        retry, _ := time.ParseDuration(string(m[1]))
        return true, retry
    }
    return false, 0
}

// watcher side, wired in HC-011 loop:
if limited, retryAfter := adapter.DetectRateLimit(evt); limited {
    watcher.emit(agentRateLimited{RetryAfter: retryAfter})
    watcher.enterRateLimitedState()  // ???
}
```

Stuck points:

1. §4.6.HC-025 says on clearance emit `agent_rate_limit_cleared`. What signals clearance? The spec says "the session resumes producing output" — but output chunks can appear *while* rate-limited (error messages, retry countdowns). Does the adapter have a second callback `DetectRateLimitCleared(event)` (not listed in §4.3.HC-013), or is clearance inferred from the absence of rate-limit match for N events? The Adapter surface in §6.1 has no `DetectClearance` method, yet the watcher must decide when to emit `agent_rate_limit_cleared`. Material gap.
2. The watcher's behavior *during* rate-limit is undefined: are progress events still passed through to the event bus? Does the silent-hang timer reset? Informative implication: rate-limited sessions should pause the hang timer, but §7.1 does not carry an exception row.

### HC-035 / HC-036 / HC-040 — Twin parity (claude-twin binary) — IMPLEMENTABLE

```go
// cmd/harmonik-twin-claude/main.go
func main() {
    spec := readLaunchSpec()  // stdin or --launch-spec file, per HC-005
    conn, _ := net.Dial("unix", os.Getenv("HARMONIK_DAEMON_SOCKET"))
    enc := json.NewEncoder(conn)
    enc.Encode(event{Type: "handler_capabilities", Payload: caps{Versions: []int{1}}})
    enc.Encode(event{Type: "session_log_location", Payload: logLoc{Path: deriveLogPath(spec)}})
    enc.Encode(event{Type: "skills_provisioned", Payload: skills{Installed: spec.RequiredSkills}})
    enc.Encode(event{Type: "agent_ready", Payload: ready{SessionID: spec.RunID + ":" + spec.NodeID}})
    for _, step := range scriptedSteps(spec) {
        enc.Encode(event{Type: "agent_output_chunk", Payload: step})
    }
    enc.Encode(event{Type: "outcome_emitted", Payload: scriptedOutcome(spec)})
    os.Exit(0)
}
```

§4.8 is strong enough to make the twin a one-file program: same interfaces (nothing to implement — the twin is a subprocess, not a Go Handler type), same event types, same transport. The `handler_capabilities`-first ordering (§4.2.HC-009) and the `session_log_location → skills_provisioned → agent_ready` ordering (§7.2) combine to give me a linear script.

Light gap: §4.8.HC-036 allows "the binary name suffixes `-twin`" as one of the differences. For a real `claude-code` binary named literally `claude-code` and a twin named `claude-twin` (not `claude-code-twin`), is that conformant? §4.10.HC-045 says "twin binaries obey the same launch rules" — suggesting the suffix convention is strict — but the MVH naming in `core-scope.md §4` uses `claude-twin`. A one-line naming rule ("twin binary name is `<real>-twin` OR a declared alias in configuration") would close the ambiguity.

## Under-specified surfaces

The following are material gaps where an implementer is forced to invent a contract the spec does not declare:

1. **Progress-stream transport framing.** §4.2.HC-007 defers transport to per-handler spec ("named pipe, Unix socket, or file"). No per-handler spec exists. HC-044 pins a single socket `.harmonik/daemon.sock`; reconciling that with HC-007's "handler's choice" would let the Claude Code implementer build once. Recommend: pin Unix socket as MVH transport; move "handler's choice" to a post-MVH note.
2. **Event framing on the progress stream.** Envelope schema lives in event-model §3.1 but on-the-wire framing (newline-delimited JSON, length-prefixed, CBOR, protobuf) is not declared anywhere. Pick one.
3. **Skill shape on disk.** See HC-046 sketch above. The Claude Code handler cannot be written without knowing whether a skill is a directory, a file, or a manifest. Bootstrap-cite `docs/foundation/components.md §10` until `agent-configuration` lands.
4. **Rate-limit clearance detection.** Adapter surface §4.3.HC-013 has no callback for clearance detection. Either add `DetectRateLimitClear(event) -> bool` OR declare a watcher rule (e.g., "after N progress events with no rate-limit match, emit clearance").
5. **Post-outcome subprocess shutdown window.** §4.2.HC-008 says exit after `outcome_emitted` is "clean shutdown." No timeout is declared. If a subprocess hangs post-outcome, HC-026 silent-hang fires — but that seems like the wrong failure class. Declare a separate "post-outcome shutdown timeout" (e.g., 5s per HC-018) and a reclassification rule.
6. **Launch idempotency scope during re-launch.** §4.1.HC-004 pins idempotency to "within one daemon generation." Cross-generation re-launch after daemon restart is described as "a new launch." Who is responsible for garbage-collecting the previous generation's socket / subprocess / log files? Reconciliation owns it in the abstract, but the handler contract's pre-launch obligation is unwritten.
7. **Session log pre-creation contract.** §4.2.HC-010 says S04 emits `session_log_location` before `agent_ready`. §9.3 co-refs workspace-model §5.3a for the three-subsystem pipeline (S04 → S06 → S08). The handler must know: does S06 pre-create the log directory before `Launch` is called, or is the handler responsible for `mkdir -p`? If pre-created, what if the dir is missing? Reconciliation-dispatched or fail-launch?
8. **Channel buffer sizing for watcher → event bus.** §4.3.HC-015 says "event publication MUST NOT block the state lock" and §4.6.HC-027 says undeliverable events go to dead-letter. Buffer size policy (bounded? unbounded? drop-oldest?) determines whether dead-letter fires often or never. Not this spec's job to set the number, but a pointer to operator-nfr or event-model for the policy would help.

## Recommendations

1. Pin Unix socket as the MVH transport for the progress stream in HC-007; remove "named pipe or file" from the MVH surface (can be re-added post-MVH as a per-handler override).
2. Add one requirement under §4.2 declaring the on-the-wire framing of the progress stream (newline-delimited JSON is the natural fit given `log/slog` and §9.1).
3. Add `DetectRateLimitCleared(event) -> bool` to the Adapter surface in §4.3.HC-013 and §6.1. Without it, the watcher cannot emit `agent_rate_limit_cleared` deterministically.
4. Add a "post-outcome shutdown window" requirement (HC-054?) with a reclassification rule that shutdown-hang is not silent-hang.
5. Add a sentence to HC-046 or a new HC-047a: "Until the agent-configuration spec lands, handlers MUST resolve skill packages against the directory layout declared in `docs/foundation/components.md §10`." Bootstrap citation form is explicitly allowed by the template.
6. Tighten HC-007 transport: "The handler subprocess MUST connect back to the daemon on `.harmonik/daemon.sock` (per HC-044) and emit events as newline-delimited JSON envelopes per event-model §3.1."
7. Consider adding a tick-cadence sentence to §7.1: "The watcher SHOULD tick at ≤ T/10."
8. Clarify in HC-036 whether the `<real>-twin` naming suffix is normative or a convention.

## Affirmations

1. The six-sentinel-plus-two-sub-sentinel error taxonomy (§4.5, §8) is exactly right for Go: `errors.Is` routing works, the `ErrProtocolMismatch` and `ErrSkillProvisioningFailed` narrowing is implementable with `fmt.Errorf("...: %w", ErrStructural)` wrapping, and the mechanism-tagged classification rule (HC-023) means the classifier is a pure function I can unit-test exhaustively.
2. The watcher-per-session + adapter-as-callback-object split (§4.3) is concrete: it maps directly to Go `go func() { for evt := range stream { ... } }()` with the adapter as a plain struct. No goroutine leaks, no per-session mutexes needed beyond the one at the event-bus edge.
3. Twin-parity-as-invariant rather than test-discipline (§4.8, HC-INV-002) means the twin binary is a trivial side project rather than a maintenance tax. The "zero `if isTwin` branches in daemon code" lint is easy to write.
4. The §7.1 silent-hang state table is the cleanest spec-to-code surface in the document: I can write unit tests straight off the table rows.
5. LaunchSpec (§6.1 RECORD) is well-shaped: every required field has a clear source (execution-model for run/workflow, control-points for budget/freedom/skills, reconciliation for snapshot_token). No "TBD" fields.
6. §4.7 secrets redaction with compile-time schema check (HC-033) is a load-bearing-invariant-turned-into-a-startup-assert — good pattern. The registry-plus-regex mechanism is unambiguous.
7. §4.12 handler-as-modularity-boundary is doing real work: I can see how swapping the claude-code adapter for a cloud-execution adapter leaves daemon code untouched. The spec earns its "execution-shape seam" framing.
