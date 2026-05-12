# Exploratory Testing T5 — EVENT/JSONL Integrity and Redaction

**Tester:** T5 (agent `a497afa6493a1ec1e`)
**Date:** 2026-05-12
**Branch:** `worktree-agent-a497afa6493a1ec1e`
**Spec reference:** `specs/event-model.md` EV-001, EV-014a, EV-016, EV-020, EV-035
**Probe test file:** `internal/t5probe/probe_test.go`

---

## Scenario coverage

| Scenario | Method | Result |
|---|---|---|
| S1: JSONL valid JSON per line | `TestT5_JSONLValidJSON` | PASS |
| S2: Envelope fields in JSONL (EV-001) | `TestT5_EnvelopeFieldsInJSONL` | **FAIL — see F-001** |
| S3: Event order in JSONL | `TestT5_EventOrderInJSONL` | PASS |
| S4: SIGKILL / crash durability (O_APPEND + fsync chain) | `TestT5_SIGKILLDurability` | PASS (with caveat — see F-002) |
| S5: HC-031 field-name redaction | `TestT5_RedactionHC031ByFieldName` | PASS |
| S6: HC-032 value-pattern redaction | `TestT5_RedactionHC032ValuePattern` | PASS |
| S7: Dispatch order — sync blocks, async off-path (EV-014a) | `TestT5_DispatchOrder_SyncBlocksAsyncDoesNot` | PASS |

---

## Findings

### F-001 — EV-001 VIOLATION (HIGH): JSONL records contain only payload bytes, not the full envelope

**Bead:** `hk-0pyuk`
**Labels:** `exploratory-finding,tester-T5`
**Severity:** HIGH — JSONL is not spec-conformant; any consumer or tooling that reads the log and expects EV-001 envelope fields will fail.

**Root cause:** `internal/eventbus/busimpl.go` lines 182-190. The `Emit` method:
1. Unmarshals the caller-supplied payload bytes into a `map[string]any`.
2. Applies redaction via `registry.RedactionMiddleware`.
3. Re-marshals the redacted payload map.
4. Writes the re-marshalled payload bytes to the JSONL file.

The `core.Event` envelope struct is constructed at lines 201-204 for in-memory consumer dispatch, but is **never serialized to disk**. As a result every JSONL line is a bare payload JSON object, missing all six required envelope fields:

- `event_id` (UUIDv7, EV-002)
- `schema_version` (integer)
- `type` (event type string)
- `timestamp_wall` (RFC 3339)
- `source_subsystem` (Go package identifier, EV-004)
- `payload` (the type-specific body, nested under the envelope)

**Spec requirement (EV-001, event-model.md §4.1):** Every event emitted to the bus or appended to JSONL MUST carry all common envelope fields.

**Confirmed by:** `TestT5_EnvelopeFieldsInJSONL` — FAIL on all three lines for all six fields.

**Fix direction:** `busimpl.Emit` must construct a `core.Event` (or equivalent envelope map) and serialize the complete envelope + nested payload to JSONL. The envelope fields that `busimpl` currently lacks the information to populate (notably `event_id`, `schema_version`, `timestamp_wall`, `source_subsystem`) must be injected either at `Emit` call time (caller supplies them) or generated inside `Emit` before the JSONL write. This is a prerequisite for the JSONL to be usable for reconciliation, replay, or observability per spec §4.5.

---

### F-002 — OBSERVATION: True SIGKILL durability not testable from external test binary

**Bead:** none filed (not reproduced as a defect)
**Severity:** OBSERVATION only

A subprocess that writes events and is killed with SIGKILL requires use of `internal/` packages from an external binary, which Go's module system forbids. The probe instead validated the O_APPEND + fsync durability chain in-process:

- 5 F-class (`daemon_started`) events were written with `sync=true`.
- The file was re-opened without closing (fd-leak crash simulation).
- A 6th event was written.
- Verification: all 6 lines present, all valid JSON, file ends with `\n`, re-open does not truncate.

**Conclusion:** The `JSONLWriter.Append` + fsync path (EV-016 / EV-020) is structurally sound. The O_APPEND flag prevents truncation on re-open. The single-write-per-line buffer minimises the torn-write window under POSIX O_APPEND semantics. A proper out-of-tree SIGKILL test binary is a follow-up if needed.

---

## Passing findings (for record)

**HC-031 field-name redaction (S5, PASS):** Fields named `secret_token`, `api_key`, `password` are replaced with `<redacted>` before JSONL write and consumer dispatch. Safe fields pass through unchanged.

**HC-032 value-pattern redaction (S6, PASS):** A registered `sk-[A-Za-z0-9]+` pattern causes any field whose string value matches to be replaced with `<redacted>` regardless of field name. Safe fields unaffected.

**EV-014a dispatch order (S7, PASS):** `Emit` returns in under 10ms when the async consumer has a 100ms sleep. Sync consumer runs before Emit returns; async consumer completes after. `Drain` collects all in-flight goroutines.

**Event ordering (S3, PASS):** Emission order `daemon_started` -> `run_started` -> `run_completed` is preserved in JSONL line order.

**Valid JSON (S1, PASS):** All JSONL lines are parseable as valid JSON objects (modulo F-001 -- the object is the payload only, not the envelope).

---

## Bead IDs filed

| Bead ID | Description | Labels |
|---|---|---|
| `hk-0pyuk` | EV-001 violation: busimpl.Emit writes payload-only to JSONL | exploratory-finding, tester-T5 |

---

## Files examined

- `specs/event-model.md` §4.1 EV-001, §4.4 EV-016, §6.2 EV-020, §4.2 EV-014a, §4.4 EV-035
- `internal/eventbus/busimpl.go` -- `Emit` method (lines 164-240)
- `internal/eventbus/jsonlwriter.go` -- `Append` method
- `internal/core/event.go` -- `Event` envelope struct
- `internal/handlercontract/redaction.go` -- HC-031 `RedactByFieldName`
- `internal/handlercontract/redactionregistry.go` -- HC-032 `RedactionMiddleware`
- `internal/daemon/daemon.go` -- `Start` composition root

## Time used

Approximately 25 minutes within the 30-minute limit.
