package structuredlog

import "time"

// SchemaVersion is the current log_schema_version value per ON-035.
// Consumers use this to select the correct parser. Breaking shape changes MUST
// bump this value under the N-1 compat window per ON-INV-001.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035.
const SchemaVersion = "1.0"

// Level is a structured-log severity level.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035 — "level ∈ {debug, info, warn, error}".
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Record is the minimum ON-035 NDJSON structured-log record shape.
//
// Required fields (always present): Ts, LogSchemaVersion, Level, Subsystem,
// SourceSubsystem, Msg, Fields.
//
// Optional fields (omitted from JSON when zero): RunID, NodeID, EventID.
//
// The JSON keys match the snake_case names declared in ON-035; the Go field
// names use PascalCase per Go conventions.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035.
type Record struct {
	// Ts is the log emission timestamp, RFC 3339 with millisecond precision.
	Ts time.Time `json:"ts"`

	// LogSchemaVersion identifies the wire format version. Current: "1.0".
	LogSchemaVersion string `json:"log_schema_version"`

	// Level is the severity level: debug, info, warn, or error.
	Level Level `json:"level"`

	// Subsystem identifies the owning subsystem (e.g. "daemon", "queue").
	Subsystem string `json:"subsystem"`

	// SourceSubsystem is the subsystem that emitted this log record, per
	// event-model.md §4.9 EV-034a. For most records this equals Subsystem;
	// it differs when one subsystem emits on behalf of another.
	SourceSubsystem string `json:"source_subsystem"`

	// RunID is the run correlation ID. Omitted when empty.
	RunID string `json:"run_id,omitempty"`

	// NodeID is the node correlation ID. Omitted when empty.
	NodeID string `json:"node_id,omitempty"`

	// EventID is the UUIDv7 of the tracked event this log record corresponds
	// to. MUST be present when the log record is the subsystem's own emission
	// of an event tracked in JSONL. Omitted otherwise.
	//
	// Spec ref: event-model.md §4.1 (UUIDv7 correlation).
	EventID string `json:"event_id,omitempty"`

	// Msg is a short human-readable message.
	Msg string `json:"msg"`

	// Fields is a map of typed values carrying additional structured context.
	// Secrets-redaction MUST have been applied before Fields is populated.
	Fields map[string]any `json:"fields"`
}

// marshalTS formats t as RFC 3339 with millisecond precision.
func marshalTS(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// recordJSON is the JSON-encodable form of Record, with Ts rendered as a
// string so the millisecond format is preserved across json.Marshal.
type recordJSON struct {
	Ts               string         `json:"ts"`
	LogSchemaVersion string         `json:"log_schema_version"`
	Level            Level          `json:"level"`
	Subsystem        string         `json:"subsystem"`
	SourceSubsystem  string         `json:"source_subsystem"`
	RunID            string         `json:"run_id,omitempty"`
	NodeID           string         `json:"node_id,omitempty"`
	EventID          string         `json:"event_id,omitempty"`
	Msg              string         `json:"msg"`
	Fields           map[string]any `json:"fields"`
}

func (r Record) toJSON() recordJSON {
	fields := r.Fields
	if fields == nil {
		fields = map[string]any{}
	}
	return recordJSON{
		Ts:               marshalTS(r.Ts),
		LogSchemaVersion: r.LogSchemaVersion,
		Level:            r.Level,
		Subsystem:        r.Subsystem,
		SourceSubsystem:  r.SourceSubsystem,
		RunID:            r.RunID,
		NodeID:           r.NodeID,
		EventID:          r.EventID,
		Msg:              r.Msg,
		Fields:           fields,
	}
}
