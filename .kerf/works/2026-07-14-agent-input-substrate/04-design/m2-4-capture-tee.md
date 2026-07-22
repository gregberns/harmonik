# M2-4 Design Pass — Live capture tee, apptap production wiring (C4)

**Task:** `plans/2026-07-13-code-revamp/TASKS.md:101` (M2-4). Splice
`internal/apptap.Tap` (`InCapture`/`OutCapture`, `tap.go:48/58/63`) onto the
structured driver's input/output stream — apptap's first production consumer
(closes ROADMAP orphan `plans/2026-07-13-code-revamp/ROADMAP.md:94` / `:69`).

**Spec anchors:** AIS-013 (pipes-only best-effort tee), AIS-014
(persistence/redaction/corpus-layout/retention), AIS-INV-002 (capture never
aborts the run), all in
`.kerf/works/2026-07-14-agent-input-substrate/05-spec-drafts/agent-input.md:201-213,265-270`;
RS-016 (`specs/replay-substrate.md:183`).

This pass resolves the four open items on `02-components.md:133-138`: **(1)
splice point, (2) persistence/rotation/budget, (3) redaction/secret-scan, (4)
shared recorder infra with the P1 record-keeper-live follow-up.**

---

## 0. The stream we tee is the STRUCTURED DRIVER's stdio — not Claude's tmux

The single most load-bearing scoping call. Two input paths exist (AIS-003), and
they have fundamentally different capturable streams:

- **Structured (Codex app-server) driver** — owns the child's stdin/stdout pipes
  directly (AIS-009, `agent-input.md:168-172`). This is a real NDJSON byte
  stream in both directions. **This is the C4 recorded corpus.** It is what
  M2-5's `Twin[E]` replays (`agent-input.md:44` "production-captured, not
  spike-captured").
- **tmux/Claude path** — there is no raw child stdio to tee; input is a tmux
  paste and output is a TUI pane. Per AIS-011 (`agent-input.md:185`) `capture-pane`
  is a **human observation window only, never a recorded corpus and never a
  signal**. The Claude path's ack is the hook-bridge event, not a captured
  stream. So the M2-4 production tee does **not** apply to the Claude path; its
  "observability" is the pane, which stays as-is.

Consequence: M2-4 wires the tee at the **Codex driver's stdio seam** (AIS-009's
"driver owns the child's stdin/stdout pipes directly; the capture tee tees
both"). It has a hard dependency on M2-2 (the Codex driver) existing to produce
that stream (see §6 residual).

---

## 1. Splice point — exactly where the tee sits

The Codex driver owns two pipes to its child (mirroring `apptap.Tap`'s wire
diagram, `tap.go:36-44`, and the `handler.Session` StdinPipe + `SendInput`/
`CloseStdin` shape AIS-009 cites at `agent-input.md:170`):

```
driver Effector[A].WriteInput ─► childStdin   (caller→child: input frames)
child stdout ─────────────────► driver EventSource[E] (child→caller: deltas/acks)
```

The tee splices at both, byte-identically:

- **Input direction (`InCapture`).** Where the driver's `Effector[A]` executes
  `WriteInput{seq, payload}` (§6.3 `agent-input.md:324`) onto `childStdin`, wrap
  the destination `io.MultiWriter(childStdin, inRecorderWriter)`. Same shape as
  `tap.go:113-116`.
- **Output direction (`OutCapture`).** Where the driver's `EventSource[E]` reads
  `childStdout` before codec-decode, wrap the source `io.TeeReader(childStdout,
  outRecorderWriter)` (or the `MultiWriter` on the copy dst, `tap.go:118-121`).
  The tee sits **before** the codec so the corpus is the raw wire (what
  `ReplayCodec[E]` re-decodes in M2-5), not post-decoded events.

**Do NOT use `Tap.Run` (`tap.go:82`).** `Run` does `exec.Command` + `cmd.Start`
+ `cmd.Wait` — it owns the process. The Codex driver already owns its own
process (AIS-009), so forcing `Tap.Run`'s exec+Wait ownership onto it is
non-conformant (AIS-013 `agent-input.md:203` "MUST NOT force `Tap.Run`'s
exec+Wait process ownership onto a driver that owns its own process"). **M2-4
adds a pipes-only splice constructor to `apptap`** (AIS-013's "a pipes-in
constructor added to `apptap`"), e.g.:

```go
// Splice tees an already-owned pair of pipes; it never spawns or Waits a process.
// in  → inDst  (+ inCap);  out → outDst (+ outCap). Returns when both copies drain.
func Splice(ctx context.Context, s SpliceIO) error
type SpliceIO struct {
    In  io.Reader; InDst  io.Writer; InCap  io.Writer
    Out io.Reader; OutDst io.Writer; OutCap io.Writer
}
```

This keeps `apptap` the format-agnostic tee primitive (RS-016
`specs/replay-substrate.md:185` — "owns no file format, opens no file"); the
`…Cap` writers are the consumer's persisters (§2).

**Best-effort reversal (AIS-INV-002).** Today the `MultiWriter` capture is
**fail-closed**: `tap.go:57` / `:62` document "Write errors on InCapture/
OutCapture are propagated and abort the tap." AIS-013 (`agent-input.md:203`)
mandates the explicit reversal — capture must **degrade-to-uncaptured**. M2-4
wraps each `…Cap` writer in a `bestEffortWriter` that, on first write error,
latches "capture off," logs once, and thereafter returns `n, nil` to the
`MultiWriter` so the **live stream never back-pressures or aborts** (AIS-INV-002
`agent-input.md:267`). The live `childStdin`/`childStdout` copies are never
wrapped — only the capture leg degrades.

---

## 2. Persistence / rotation / budget — CONSUMER-owned recorder

Persistence lives in the consumer, not the tee (AIS-014 `agent-input.md:210`).
The consumer is a new **recorder** component (§4) that supplies the two
`…Cap` writers.

### 2.1 Layout (AIS-014)

Corpus lands at `${workspace_path}/.harmonik/sessions/${session_id}/` — the
existing WM §4.7 session dir (`specs/workspace-model.md:73`, pre-created by the
workspace manager). **One file per direction:**

- `capture-in.ndjson` — caller→child input frames (structurally secret-free, §3).
- `capture-out.ndjson` — child→caller output (the secret-risk direction, §3).

Plus a **mechanical `CAPTURE-LOG` ledger, actually written per capture**
(AIS-014 `agent-input.md:210`). The executed model to copy is the keeper's
`EXTRACT-LOG` pattern (a ledger the code actually appends to per action); the
anti-pattern AIS-014 explicitly names is a `CAPTURE-LOG.md` that is *declared in
spec but never written by code* (also the RS-016 `CAPTURE-LOG.md` ledger at
`specs/replay-substrate.md:204`). Each capture-session appends one row: session
id, direction, byte count, start/stop, rotation/prune actions, and any
degrade-to-uncaptured event (§1) with reason. The ledger write is itself
best-effort (never aborts the run).

### 2.2 Rotation / retention — reuse the `brhistoryrotate` precedent

The corpus MUST NOT inherit the unrotated large-`events.jsonl` defect (AIS-014
`agent-input.md:210`; ROADMAP JSONL-rotation deferred-orphan, `REVIEW-FINDINGS.md:35`).
Reuse the **keep-N / age-prune** shape already proven in
`internal/daemon/brhistoryrotate.go:78-197`:

- **keep-N by mtime.** Retain the K most-recent session capture dirs under
  `.harmonik/sessions/`; archive-or-prune older (the `brhistoryrotate`
  `keepLatest`, `brhistoryrotate.go:48` `const …DefaultKeep = 20`, sort-desc-by-mtime
  `brhistoryrotate.go:143`). Corpus captures are replay fixtures, not audit
  records, so **hard-prune** (delete) is acceptable here rather than
  brhistoryrotate's archive-rename — but keep the same "non-fatal, log-and-continue"
  discipline (`brhistoryrotate.go:70-72`).
- **age-prune.** Also drop dirs older than an age bound, mirroring the
  `orphansweep` age-prune precedent (`internal/lifecycle/orphansweep*`).
- **When.** Run the prune as a **pre-flight at session-recorder open** (cheap,
  bounded, one dir scan), exactly as brhistoryrotate runs at daemon start
  (`brhistoryrotate.go:3` "startup pre-flight"). Not on a background timer.

### 2.3 Budget cap (the `make capture-fixtures` concern)

Per-session **byte budget** on each direction file (config, default e.g. 32 MiB).
On exceed: **stop capturing that direction, degrade-to-uncaptured** (§1
`bestEffortWriter` latch), append a `budget_exceeded` row to `CAPTURE-LOG`, and
let the live run continue untouched (AIS-INV-002). This is the runtime analog of
the `make capture-fixtures` budget cap flagged at `02-components.md:134`; the
fixture-build Make target is a C5/DoD concern, not M2-4 code.

---

## 3. Redaction / secret-scan — in the persisting writer, best-effort, fail-to-uncaptured

Redaction lives in the **persisting writer, not the tee** (AIS-014
`agent-input.md:210`). The tee stays a verbatim in-memory copy; the recorder's
writer scrubs before bytes hit disk.

### 3.1 Direction asymmetry (reuse HC-028 / HC-032)

- **INPUT (`capture-in`) is structurally secret-free** — secrets travel to the
  child via `HARMONIK_SECRET_*` env, never via the input payload
  (`specs/handler-contract.md:557` HC-028). So the input file needs only the
  cheap name/value scrub as defense-in-depth; it is not the risk surface.
- **OUTPUT (`capture-out`) is the secret risk** (AIS-014 `agent-input.md:210`).
  Apply the **value-pattern scrub** — the HC-032 per-handler value-shaped regexes
  (`specs/handler-contract.md:583`, e.g. `sk-ant-*`) plus the HC-031 common rule
  (`handler-contract.md:577`). Reuse the same registry the event bus uses:
  event-model EV-035 applies `(?i)(secret|token|password|api[_-]?key|auth)` +
  per-handler patterns (`event-model.md:874`; handler-contract §4.7). The corpus
  writer imports **that same pattern set**, not a private copy — one redaction
  source of truth.

Note the byte-stream nuance: HC-031 is a *field-name* rule and does not apply to
a raw NDJSON byte stream; the load-bearing scrub for the corpus is the
**value-shaped** HC-032 patterns matched against the streamed bytes.

### 3.2 Fail-to-uncaptured, NOT fail-closed (the one contrast with the bus)

The event bus redactor is **fail-closed** (ON-022 / `redaction_failed`,
`event-model.md:269,1691` — abort the emission rather than ship a secret). The
capture tee is the **opposite by mandate**: AIS-INV-002 forbids capture from
back-pressuring or aborting the live run (`agent-input.md:267`). Reconciled
correctly:

> On a scrub failure the recorder **drops the capture (stops writing that
> direction) and degrades to uncaptured** — it MUST NOT write the unredacted
> bytes through. That satisfies BOTH AIS-INV-002 (live run untouched) AND HC-034
> (no secret value to disk, `handler-contract.md:597`). "Best-effort" degrades
> to *no capture*, never to *raw capture*.

Log once + `CAPTURE-LOG` `redaction_failed` row.

---

## 4. Shared recorder infra with the P1 "record-keeper-live" follow-up

The P1 follow-up "record keeper live" is TASKS.md deferred-orphan **#8**
(`TASKS.md:161` "record-keeper-live apptap recorder | M2-4"), homed to M2-4 as
shared infra (`ROADMAP.md:94`). Both consumers need: pipes-only tee + redacting
+ rotating + budget-capped + ledger-writing persister. Factor that once.

**Proposal — a consumer-side `Recorder` in a new `internal/capture` package**
(kept OUT of the `apptap` stdlib-adjacent tee leaf per RS-016 / the RS-016
leaf-quarantine discipline `specs/replay-substrate.md:185`):

```go
package capture
// NewSessionRecorder opens (and prunes, §2.2) a session capture dir and returns
// the two best-effort, redacting, budget-capped Cap writers + the ledger.
func NewSessionRecorder(sessionDir string, opts Options) (*Recorder, error)
func (r *Recorder) InWriter()  io.Writer   // → apptap Splice InCap
func (r *Recorder) OutWriter() io.Writer   // → apptap Splice OutCap
func (r *Recorder) Close() error           // flush ledger, close files
type Options struct { KeepN int; MaxAge time.Duration; ByteBudget int64;
    RedactPatterns []*regexp.Regexp } // patterns sourced from handler-contract §4.7 registry
```

- **M2-4 (this task):** the Codex driver constructs a `Recorder` for its run's
  `session_id`, hands `InWriter()/OutWriter()` to `apptap.Splice` (§1).
- **P1 record-keeper-live:** the keeper constructs the *same* `Recorder` for its
  session stream. The keeper is off-daemon and session-id-addressed
  (`agent-input.md:56`), so it just needs the same `sessionDir` + writers — it
  does not need the driver seam. Shared code = the whole `internal/capture`
  package; the only difference is who feeds the tee.

This makes `apptap` the format-agnostic pipe tee (RS-016), `internal/capture`
the shared persistence/redaction/rotation consumer, and M2-4 + P1 two thin call
sites. No recorder logic is duplicated.

---

## 5. What M2-4 delivers (checklist)

1. `apptap.Splice` — pipes-only, process-non-owning tee constructor (§1); plus
   the `bestEffortWriter` degrade wrapper reversing today's fail-closed capture
   (`tap.go:57/62` → AIS-013/AIS-INV-002).
2. `internal/capture` package — `Recorder` (§4): session-dir layout (§2.1),
   keep-N/age-prune pre-flight (§2.2, brhistoryrotate shape), byte budget (§2.3),
   direction-asymmetric value-scrub sourced from handler-contract §4.7 (§3.1),
   fail-to-uncaptured (§3.2), mechanical `CAPTURE-LOG` ledger actually written.
3. Wire the Codex driver's stdio seam (AIS-009) through `apptap.Splice` with the
   `Recorder` writers (§1).
4. Tests: verbatim byte-identity of live stream with capture on AND after
   degrade; degrade-on-disk-error does not abort a run (AIS-INV-002); secret
   pattern scrubbed from `capture-out`; keep-N prune bounds the dir; budget cap
   latches; `CAPTURE-LOG` has a row per capture.

---

## 6. Residual / hand-offs

- **HARD dependency on M2-2 (Codex driver).** The tee needs the driver's owned
  stdio to splice onto (AIS-009). M2-2 is `pending-design`; M2-4 cannot *start*
  until the driver produces that stream — but the M2-4 design is now resolved and
  independent of M2-2's internal codec detail (it only needs the two pipes).
- **Non-blocking: recorder package placement.** `internal/capture` vs an `apptap`
  companion `recorder.go`. Recommend `internal/capture` (keeps the RS-016 tee
  leaf clean). Small planner/impl call; not a blocker (OQ-RS-003
  `specs/replay-substrate.md:441` is the parallel tee-placement question and is
  itself declared non-blocking).
- **Non-blocking: WM §4.7 amendment.** F5/T-WM (`06-integration.md:26,86`) — amend
  `specs/workspace-model.md` §4.7 to enumerate the capture-corpus files +
  `CAPTURE-LOG`. AIS relies on the existing session dir read-only; the enumeration
  is a Tasks item, not a spec hole.
- **Redaction pattern source coupling.** M2-4 imports the handler-contract §4.7 /
  EV-035 pattern registry rather than a private list — confirm the registry is
  importable from `internal/capture` without a depguard violation at impl time.
