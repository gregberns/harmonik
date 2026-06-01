// Package structuredlog implements the ON-035 structured-log wire format for
// harmonik subsystems.
//
// Every subsystem MUST emit structured logs; unstructured log lines are
// forbidden at spec-declared emission points. This package provides:
//
//   - [Record] — the minimum NDJSON record shape declared by ON-035.
//   - [Handler] — a [log/slog.Handler] that writes ON-035-compliant NDJSON
//     records and rotates log files at 100 MiB or 24 hours.
//   - [NewHandler] — constructor; accepts a [Config] with subsystem identity,
//     project directory, and a secrets-redaction function.
//
// # Wire format
//
// Each log record is one JSON object followed by a newline ('\n'). Required
// fields: ts, log_schema_version, level, subsystem, source_subsystem, msg,
// fields. Optional fields (omitted when zero): run_id, node_id, event_id.
//
// # Rotation
//
// Log files are written to <project_dir>/.harmonik/logs/<subsystem>-active.jsonl.
// When the file exceeds 100 MiB or 24 hours have elapsed since creation, it
// is renamed to .harmonik/logs/<subsystem>-<rotated_at_rfc3339>.jsonl and a
// fresh file is opened.
//
// # Secrets redaction
//
// Redaction is producer-side per ON-022. The [Config.Redact] hook is called on
// every field map before emission. Consumers MUST NOT re-redact.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035; §4.7 ON-022.
//
// Bead ref: hk-sx9r.51.
package structuredlog
