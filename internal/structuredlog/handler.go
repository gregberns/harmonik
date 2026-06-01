package structuredlog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// rotateMaxBytes is the file-size threshold triggering rotation (100 MiB).
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035.
const rotateMaxBytes = 100 << 20

// rotateMaxAge is the time-based rotation threshold (24 hours).
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035.
const rotateMaxAge = 24 * time.Hour

// Config carries the parameters for [NewHandler].
type Config struct {
	// Subsystem is the owning subsystem name (e.g. "daemon", "queue").
	// Required.
	Subsystem string

	// SourceSubsystem is the subsystem that emits the log records. Defaults
	// to Subsystem when empty.
	SourceSubsystem string

	// ProjectDir is the harmonik project root. Log files are written to
	// <ProjectDir>/.harmonik/logs/. Required.
	ProjectDir string

	// Redact is the producer-side secrets-redaction function. It receives the
	// fields map before emission and must return a sanitised copy. May be nil
	// (no redaction applied, suitable for subsystems with no secret-typed
	// fields). Consumers MUST NOT re-redact per ON-022.
	//
	// Spec ref: specs/operator-nfr.md §4.7 ON-022.
	Redact func(fields map[string]any) map[string]any

	// MinLevel is the minimum slog level that is emitted. Records below this
	// level are dropped. Defaults to slog.LevelDebug (emit all).
	MinLevel slog.Level

	// NowFn is the time source, injectable for testing. Defaults to time.Now.
	NowFn func() time.Time
}

func (c *Config) sourceSubsystem() string {
	if c.SourceSubsystem != "" {
		return c.SourceSubsystem
	}
	return c.Subsystem
}

func (c *Config) now() time.Time {
	if c.NowFn != nil {
		return c.NowFn()
	}
	return time.Now()
}

// Handler is a [log/slog.Handler] that writes ON-035-compliant NDJSON records.
//
// Thread-safe. Rotate is called on each Handle call when the rotation
// thresholds are exceeded.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035.
type Handler struct {
	cfg    Config
	attrs  []slog.Attr
	groups []string

	mu          sync.Mutex
	file        *os.File
	openedAt    time.Time
	writtenSize int64
}

// NewHandler constructs a Handler that writes structured logs to
// <cfg.ProjectDir>/.harmonik/logs/<cfg.Subsystem>-active.jsonl, rotating the
// file when it exceeds 100 MiB or 24 hours.
//
// Returns an error if the logs directory cannot be created or the initial log
// file cannot be opened.
func NewHandler(cfg Config) (*Handler, error) {
	if cfg.Subsystem == "" {
		return nil, fmt.Errorf("structuredlog.NewHandler: Subsystem is required")
	}
	if cfg.ProjectDir == "" {
		return nil, fmt.Errorf("structuredlog.NewHandler: ProjectDir is required")
	}

	h := &Handler{cfg: cfg}
	if err := h.openFile(); err != nil {
		return nil, err
	}
	return h, nil
}

// logDir returns the path to the logs directory for this project.
func (h *Handler) logDir() string {
	return filepath.Join(h.cfg.ProjectDir, ".harmonik", "logs")
}

// activePath returns the path of the currently-active log file.
func (h *Handler) activePath() string {
	return filepath.Join(h.logDir(), h.cfg.Subsystem+"-active.jsonl")
}

// openFile creates the logs directory (if needed) and opens the active log
// file for append. Must be called with h.mu held (or before the Handler is
// shared).
func (h *Handler) openFile() error {
	dir := h.logDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("structuredlog: create log dir %s: %w", dir, err)
	}

	active := h.activePath()
	//nolint:gosec // G304: path is composed from operator-provided ProjectDir.
	f, err := os.OpenFile(active, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("structuredlog: open log file %s: %w", active, err)
	}

	// Measure what's already in the file so the size threshold is accurate
	// even when we're appending to an existing active file on startup.
	info, statErr := f.Stat()
	if statErr == nil {
		h.writtenSize = info.Size()
	}

	h.file = f
	h.openedAt = h.cfg.now()
	return nil
}

// rotateIfNeeded checks the size and age thresholds and rotates if either is
// exceeded. Must be called with h.mu held.
func (h *Handler) rotateIfNeeded() error {
	now := h.cfg.now()
	sizeExceeded := h.writtenSize >= rotateMaxBytes
	ageExceeded := now.Sub(h.openedAt) >= rotateMaxAge

	if !sizeExceeded && !ageExceeded {
		return nil
	}

	// Close the active file.
	if err := h.file.Close(); err != nil {
		return fmt.Errorf("structuredlog: close active log for rotation: %w", err)
	}
	h.file = nil

	// Rename active → <subsystem>-<rotated_at>.jsonl
	// Use the time the file was opened (not now) so the name reflects the
	// log window, matching the spec's "<subsystem>-<rotated_at>" convention.
	stamp := h.openedAt.UTC().Format("2006-01-02T15-04-05Z")
	rotatedName := h.cfg.Subsystem + "-" + stamp + ".jsonl"
	rotatedPath := filepath.Join(h.logDir(), rotatedName)
	if err := os.Rename(h.activePath(), rotatedPath); err != nil {
		return fmt.Errorf("structuredlog: rename %s → %s: %w", h.activePath(), rotatedPath, err)
	}

	h.writtenSize = 0
	return h.openFile()
}

// Enabled reports whether the handler is enabled for the given level.
func (h *Handler) Enabled(_ context.Context, lvl slog.Level) bool {
	return lvl >= h.cfg.MinLevel
}

// WithAttrs returns a new Handler with the given attributes added to each
// subsequent record's fields.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := h.clone()
	h2.attrs = append(h2.attrs, attrs...)
	return h2
}

// WithGroup returns a new Handler that nests subsequent attributes under the
// given group name. Groups are flattened into the fields map with a "." prefix.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	h2 := h.clone()
	h2.groups = append(h2.groups, name)
	return h2
}

// Handle writes a single ON-035 NDJSON record to the active log file.
//
// Rotation is attempted before the write when thresholds are exceeded.
// Secrets redaction is applied to the fields map via cfg.Redact before any
// bytes reach the file.
func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	if !h.Enabled(context.Background(), r.Level) {
		return nil
	}

	fields := h.buildFields(r)
	if h.cfg.Redact != nil {
		fields = h.cfg.Redact(fields)
	}

	rec := Record{
		Ts:               r.Time,
		LogSchemaVersion: SchemaVersion,
		Level:            slogLevelToLevel(r.Level),
		Subsystem:        h.cfg.Subsystem,
		SourceSubsystem:  h.cfg.sourceSubsystem(),
		Msg:              r.Message,
		Fields:           fields,
	}

	// Extract correlation IDs from attrs if present.
	if rid, ok := fields["run_id"]; ok {
		if s, ok := rid.(string); ok {
			rec.RunID = s
			delete(fields, "run_id")
		}
	}
	if nid, ok := fields["node_id"]; ok {
		if s, ok := nid.(string); ok {
			rec.NodeID = s
			delete(fields, "node_id")
		}
	}
	if eid, ok := fields["event_id"]; ok {
		if s, ok := eid.(string); ok {
			rec.EventID = s
			delete(fields, "event_id")
		}
	}

	line, err := json.Marshal(rec.toJSON())
	if err != nil {
		return fmt.Errorf("structuredlog: marshal record: %w", err)
	}
	line = append(line, '\n')

	h.mu.Lock()
	defer h.mu.Unlock()

	if rotErr := h.rotateIfNeeded(); rotErr != nil {
		return rotErr
	}

	n, err := h.file.Write(line)
	h.writtenSize += int64(n)
	return err
}

// Close flushes and closes the underlying log file. After Close, Handle
// returns an error.
func (h *Handler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.file == nil {
		return nil
	}
	err := h.file.Close()
	h.file = nil
	return err
}

// clone returns a shallow copy of h with its own attrs/groups slices.
func (h *Handler) clone() *Handler {
	h2 := *h
	h2.attrs = append([]slog.Attr(nil), h.attrs...)
	h2.groups = append([]string(nil), h.groups...)
	return &h2
}

// buildFields collects slog attrs from the record and any pre-attached attrs
// into a flat map, applying group prefixes.
func (h *Handler) buildFields(r slog.Record) map[string]any {
	fields := make(map[string]any)

	prefix := ""
	if len(h.groups) > 0 {
		for _, g := range h.groups {
			prefix += g + "."
		}
	}

	for _, a := range h.attrs {
		setAttr(fields, prefix, a)
	}

	r.Attrs(func(a slog.Attr) bool {
		setAttr(fields, prefix, a)
		return true
	})

	return fields
}

// setAttr writes a slog.Attr into the fields map, respecting groups.
func setAttr(fields map[string]any, prefix string, a slog.Attr) {
	key := prefix + a.Key
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindGroup:
		for _, sub := range v.Group() {
			setAttr(fields, key+".", sub)
		}
	default:
		fields[key] = v.Any()
	}
}

// slogLevelToLevel maps a slog.Level to the ON-035 level string.
func slogLevelToLevel(l slog.Level) Level {
	switch {
	case l >= slog.LevelError:
		return LevelError
	case l >= slog.LevelWarn:
		return LevelWarn
	case l >= slog.LevelInfo:
		return LevelInfo
	default:
		return LevelDebug
	}
}
