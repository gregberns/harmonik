# 04-Design — capture-tee (C4: apptap production wiring)

> Elaborates D6 within `00-decisions.md`. Facts: `03-research/capture-tee/findings.md`.

## 1. apptap additive API (the production consumer, honestly)

`Tap.Run` owns the spawn (`tap.go:82-96`) — wrong shape for a driver that originates writes.
Additive, in-package (no change to `Tap`):

```go
// CaptureWriter returns a writer that forwards to dst and tees a byte-identical
// copy to capture. A capture write error aborts the write (InCapture semantics,
// tap.go:55-57). Lossless/verbatim invariants (tap.go:9-24) apply.
func CaptureWriter(dst, capture io.Writer) io.Writer

// CaptureReader returns a reader that tees every byte read from src into capture.
// Read errors from src pass through; capture errors surface on Read.
func CaptureReader(src io.Reader, capture io.Writer) io.Reader
```

Implementation is thin (`io.MultiWriter` / `io.TeeReader` composition) but LIVES in apptap so
the invariants + tests + the "first production consumer" orphan (`ROADMAP.md:94`) are truly
closed at `internal/apptap`, not scattered stdlib calls. Tests mirror
`TestTapGateLosslessRoundtrip` (`tap_test.go:112`) for both helpers. OQ-RS-003 (package
placement) resolved: apptap STAYS at `internal/apptap` (the default), now with two consumers
classes (proxy Tap; splice helpers).

## 2. Wiring in the driver

Per driver-design §5: stdin writes flow through `CaptureWriter(stdinPipe, wireIn)`; stdout
through `CaptureReader(stdoutPipe, wireOut)`. Capture is ON by default for structured runs
(the record→replay seam's live recorder — the point of C4); config kill-switch
`capture: off` for emergencies only.

## 3. Persistence policy (live)

- Home: `.harmonik/run-context/<run_id>/wire-in.jsonl` + `wire-out.jsonl` (run-scoped;
  merge-stripped by `stripruncontext_hk4je.go` — capture never lands on main).
- Size cap: 64 MiB per file (config). On cap: capture writer flips to a counting sink,
  emits one `capture_truncated` structured-log line; DELIVERY IS NEVER BLOCKED by cap policy.
  (The InCapture abort-on-error semantics apply only to real io errors pre-cap — e.g. disk
  full aborts the tap loudly rather than silently recording a hole.)
- Retention: rides the existing run-context lifecycle (merge-strip + operator cleanup);
  no new rotation machinery (the `.harmonik/logs/` ON-035 rotation precedent is noted but
  NOT adopted — per-run files are naturally bounded by the cap + run lifetime).

## 4. Promotion to corpus (the redaction answer)

Two-tier rule (IN-011/IN-012):
- **Tier 1 (live, operator-local):** same trust domain as `.harmonik/logs/` (which already
  receives full pane payloads today); env-credential deny-list enforced at spawn
  (`claudehandler_chb006_024.go:346-363`); never committed (merge-strip + .gitignore).
- **Tier 2 (repo fixture):** promotion = a deliberate curation command
  (`scripts/promote-claude-corpus.py` or make target) that (a) parses every line through
  `claudewire` (frames only — unparseable lines are dropped and reported), (b) applies
  `core.RedactByFieldName` (`redaction.go:26-39`) over each frame's JSON, (c) value-scans for
  the spawn deny-list names + `$HOME`-prefixed paths and replaces with sentinels,
  (d) appends a ledger entry to `testdata/claude-agent/corpus/CAPTURE-LOG.md` (CREATED by
  M2 — the codex one referenced by `Makefile:123-134`/RS-016 does not exist on disk; M2's
  task also backfills the codex ledger stub to un-break that reference).
  Output: `testdata/claude-agent/corpus/raw-session-NN.jsonl` (codex naming convention).
- The IN spec states plainly: **no live raw-byte redaction exists or is claimed** (research
  gap, capture-tee findings §3); safety = trust-domain containment + promotion-time scrub.

## 5. Relation to "record keeper live" (ROADMAP orphan)
The P1 follow-up (record the keeper live) is NOT absorbed here — the keeper is out of M2's
scope (D7). What M2 contributes: the reusable `CaptureWriter`/`CaptureReader` helpers +
the promotion pipeline shape, which a future keeper recorder composes with. Stated in the IN
spec as a non-goal pointer.
