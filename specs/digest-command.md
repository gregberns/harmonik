# `harmonik digest` Command

```yaml
---
title: harmonik digest
spec-id: digest-command
requirement-prefix: DC
status: draft
spec-category: operator-surface
spec-shape: requirements-first
version: 0.1.0
spec-template-version: 1.1
owner: flywheel-author
last-updated: 2026-05-30
depends-on:
  - cognition-loop
  - process-lifecycle
  - event-model
  - queue-model
---
```

## 1. Purpose

`harmonik digest` is a pure-Go subcommand that computes and emits a
schema-versioned status sheet from durable file surfaces only — no LLM, no
daemon connection required in snapshot mode. It is the canonical producer of
the `CL-DIGEST` artifact consumed by the cognition loop (CL-030) and by
operators monitoring the system state.

## 2. Scope

### 2.1 In scope

- `harmonik digest` snapshot mode: reads queue.json, events.jsonl, notes.jsonl,
  br ready/list, and kerf next; emits JSON or human-readable text.
- `--json` flag: NDJSON output carrying `schema_version` per CL-033.
- `--since <event_id>`: ScanAfter watermark filter per CL-031.
- `--full` flag: disable CL-032 size caps.
- `--project DIR`: project directory selection.
- Exit code 7 on missing `.harmonik/` per PL-028d.

### 2.2 Out of scope

- `--watch` continuous polling mode (post-v0.1).
- `harmonik supervise` integration (post-v0.1).
- Pi TUI panel rendering (post-v0.1).

## 3. Glossary

- **digest** — the deterministically-computed status sheet read by the cognition
  loop each turn; produced by this command (CL-030).
- **watermark** — `last_processed_event_id` stored in `.harmonik/cognition/state.json`;
  passed to this command via `--since` to filter the events window.

## 4. Normative requirements

### DC-001 — Snapshot mode requires no daemon

`harmonik digest` snapshot mode (without `--watch`) MUST operate without a
running daemon and MUST NOT connect to the daemon socket. All inputs are read
from file surfaces. Exit 0 when the daemon is stopped is required.

Tags: mechanism

### DC-002 — Missing .harmonik/ exits 7

When the target project directory does not contain a `.harmonik/` subdirectory,
`harmonik digest` MUST exit with code 7. This is the sole Cat 0 failure for
this command (per PL-028d).

Tags: mechanism

### DC-003 — JSON output carries schema_version (CL-033)

With `--json`, `harmonik digest` MUST emit exactly one NDJSON line carrying a
`schema_version` integer field. The current schema version is 1. Readers MUST
accept N-1 (version 0 does not exist; this becomes relevant when version 2 ships).

Tags: mechanism

### DC-004 — Digest inputs are deterministic only (CL-031)

The builder MUST NOT consult any LLM. Inputs are:
- `queue.json` — queue envelope from `.harmonik/queue.json`
- `origin/main` git log — recent commits
- `events.jsonl` via `ScanAfter(watermark)` — recent typed events
- `br ready --format json` — unblocked open beads
- `br list --status in_progress --json` — in-progress beads
- `.harmonik/cognition/notes.jsonl` — open (unresolved) note entries
- `kerf next --format=json` — ranked bead feed (advisory)

Tags: mechanism

### DC-005 — Size caps per CL-032

Under ordinary conditions (≤10 active runs, ≤20 open notes), the default caps
are:
- Active runs in the queue: capped at 10; remainder reported as `active_runs_omitted`.
- Open notes: capped at 20; remainder reported as `open_notes_omitted`.
- Recent events: capped at 20 most recent; remainder reported as `recent_events_omitted`.

`--full` disables all caps. When truncated, the JSON output carries a
`truncated` object with the omission counts.

Tags: mechanism

### DC-006 — `--since` uses ScanAfter semantics

`--since <event_id>` MUST restrict the events window to events with an EventID
strictly greater than the supplied UUIDv7 (ScanAfter semantics per
event-model.md §4.1 EV-002). A missing `--since` includes all events.

Tags: mechanism

### DC-007 — Non-fatal collection errors are reported in the output

When an individual input source fails (e.g. br not on PATH, kerf returns non-zero,
notes.jsonl is absent), the builder MUST continue collecting the remaining sources
and report the error in the `errors[]` field of the output. Only a missing
`.harmonik/` is a hard failure (DC-002).

Tags: mechanism

## 5. Invariants

### DC-INV-001 — No LLM in the digest path

No call path from `harmonik digest` may invoke a language model, network request,
or daemon socket. Violation is a structural invariant breach.

### DC-INV-002 — schema_version is always present in JSON output

Every `--json` emission MUST carry `schema_version`. A missing field is a
serialization bug.

## 6. CLI reference

```
harmonik digest [--project DIR] [--json] [--since EVENT_ID] [--full]

FLAGS
  --project DIR     Project directory (default: current working directory)
  --json            Emit one schema-versioned NDJSON object to stdout
  --since EVENT_ID  Restrict events to those after this UUIDv7
  --full            Disable size caps

EXIT CODES
  0  — success
  1  — argument error
  7  — .harmonik/ directory not found
```

## 7. Open questions

- **OQ-DC-001.** Should `--watch` use `harmonik subscribe` or file-poll?
  Working answer: subscribe (parity with operator-nfr.md); post-v0.1.

## 8. Revision history

| Date       | Version | Author | Changes |
|------------|---------|--------|---------|
| 2026-05-30 | 0.1.0   | agent  | Initial draft (hk-1qrty). CL-030..033 + OQ-CL-002 resolved as subcommand. |
