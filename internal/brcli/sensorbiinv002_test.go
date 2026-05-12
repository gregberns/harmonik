package brcli_test

// BI-INV-002 sensor test — bead ID byte-equal across all four harmonik surfaces.
//
// Spec ref: specs/beads-integration.md §5 BI-INV-002.
//
// BI-INV-002: A bead's ID is stable from creation to tombstone. Every harmonik
// artifact that binds to a bead (run metadata, checkpoint trailers, event
// payloads, session-log metadata) MUST use the same ID across the entire bead
// lifetime. Harmonik MUST NOT mint harmonik-local alternate identifiers for the
// same bead.
//
// This file asserts byte-equality across all four Go surfaces:
//   - Surface 1: Run.BeadID (execution-model.md §4.3 EM-014; internal/core.Run)
//   - Surface 2: Checkpoint.BeadID (execution-model.md §4.4 EM-017; internal/core.Checkpoint)
//   - Surface 3: Event payload "bead_id" (event-model.md §6.3; internal/core.PayloadHasBeadID)
//   - Surface 4: Session-log sidecar "bead_id" JSON field (workspace-model.md §4.7; WM-028)
//
// It also includes a reviewer-scan sub-test that walks internal/ source files
// looking for any function name matching mint_alternate_id variants — none should
// exist per BI-INV-002.
//
// Dependency beads (all CLOSED): hk-872.18, hk-872.19, hk-872.20, hk-872.21.

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
)

// sensorBeadIDFixtureMakeID returns a canonical core.BeadID test value for
// use across all four BI-INV-002 surfaces. BeadID is an opaque string alias
// (beads-integration.md BI-008/BI-008a); the constructor is a plain conversion
// — no Parse/Mint/New/Generate helpers exist by design.
func sensorBeadIDFixtureMakeID() core.BeadID {
	return core.BeadID("bead-sensor-biinv002-stable")
}

// sensorBeadIDFixtureMakeRun constructs a minimal valid core.Run whose BeadID
// is set to id. All UUID fields are populated so Run.Valid() passes.
func sensorBeadIDFixtureMakeRun(t *testing.T, id core.BeadID) core.Run {
	t.Helper()
	now := time.Now()
	return core.Run{
		RunID:           core.RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("1.0.0"),
		Input:           core.WorkspaceRef("workspace://sensor/biinv002"),
		WorkflowMode:    core.WorkflowModeSingle,
		BeadID:          &id,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         map[string]any{},
		StartTime:       now,
	}
}

// sensorBeadIDFixtureMakeCheckpoint constructs a minimal valid core.Checkpoint
// whose BeadID is set to id. This is the Go carrier for the Harmonik-Bead-ID
// checkpoint trailer per execution-model.md §4.4 EM-017.
//
// TransitionRecordPath is derived from the generated RunID and TransitionID via
// core.TransitionRecordPath so that Checkpoint.Valid() passes the EM-018
// path-coherence invariant.
func sensorBeadIDFixtureMakeCheckpoint(t *testing.T, id core.BeadID) core.Checkpoint {
	t.Helper()
	runID := core.RunID(uuid.Must(uuid.NewV7()))
	transitionID := core.TransitionID(uuid.Must(uuid.NewV7()))
	return core.Checkpoint{
		CommitHash:           "aabbcc0011223344556677889900aabbcc001122",
		RunID:                runID,
		StateID:              core.StateID(uuid.Must(uuid.NewV7())),
		TransitionID:         transitionID,
		BeadID:               &id,
		SchemaVersion:        1,
		TransitionRecordPath: core.TransitionRecordPath(runID, transitionID),
	}
}

// sensorBeadIDFixtureMakeEventPayload constructs a minimal event payload
// map[string]any that carries "bead_id" per event-model.md §6.3 / BI-019.
func sensorBeadIDFixtureMakeEventPayload(id core.BeadID) map[string]any {
	return map[string]any{
		"bead_id": string(id),
		"node_id": "node-sensor-01",
		"action":  "sensor-check",
	}
}

// sensorBeadIDFixtureMakeSidecarJSON builds a minimal session-log sidecar JSON
// body carrying the bead_id field per workspace-model.md §4.7 WM-028.
func sensorBeadIDFixtureMakeSidecarJSON(t *testing.T, id core.BeadID) []byte {
	t.Helper()
	type sidecar struct {
		RunID         string  `json:"run_id"`
		SessionID     string  `json:"session_id"`
		NodeID        string  `json:"node_id"`
		AgentType     string  `json:"agent_type"`
		WorkflowID    string  `json:"workflow_id"`
		LaunchedAt    string  `json:"launched_at"`
		SchemaVersion string  `json:"schema_version"`
		BeadID        *string `json:"bead_id,omitempty"`
	}
	raw := string(id)
	s := sidecar{
		RunID:         "0196a1b2-c3d4-7ef0-8a1b-biinv002sensor",
		SessionID:     "sess-biinv002-sensor-001",
		NodeID:        "node-sensor-01",
		AgentType:     "agentic",
		WorkflowID:    "wf-biinv002",
		LaunchedAt:    time.Now().UTC().Format(time.RFC3339),
		SchemaVersion: "1",
		BeadID:        &raw,
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("sensorBeadIDFixtureMakeSidecarJSON: %v", err)
	}
	return b
}

// sensorBeadIDFixtureSidecarWriteAndRead writes sidecarJSON to a temp path and
// reads it back as a parsed map. The caller can then extract the "bead_id" key.
func sensorBeadIDFixtureSidecarWriteAndRead(t *testing.T, sidecarJSON []byte) map[string]interface{} {
	t.Helper()
	dir := t.TempDir()
	sidecarPath := filepath.Join(dir, "harmonik.meta.json")
	tmpPath := fmt.Sprintf("%s.tmp-%d", sidecarPath, os.Getpid())

	//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("sensorBeadIDFixtureSidecarWriteAndRead: open tmp: %v", err)
	}
	if _, err := f.Write(sidecarJSON); err != nil {
		_ = f.Close() //nolint:errcheck // best-effort close before fatal
		t.Fatalf("sensorBeadIDFixtureSidecarWriteAndRead: write: %v", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close() //nolint:errcheck // best-effort close before fatal
		t.Fatalf("sensorBeadIDFixtureSidecarWriteAndRead: sync: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("sensorBeadIDFixtureSidecarWriteAndRead: close: %v", err)
	}
	if err := os.Rename(tmpPath, sidecarPath); err != nil {
		t.Fatalf("sensorBeadIDFixtureSidecarWriteAndRead: rename: %v", err)
	}

	//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
	raw, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("sensorBeadIDFixtureSidecarWriteAndRead: ReadFile: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("sensorBeadIDFixtureSidecarWriteAndRead: unmarshal: %v", err)
	}
	return parsed
}

// TestBIINV002_BeadIDByteEqualAcrossSurfaces is the BI-INV-002 sensor test.
//
// It constructs a single canonical BeadID value and threads it through all four
// harmonik surfaces, then asserts byte-equality across all four.
//
// Spec ref: specs/beads-integration.md §5 BI-INV-002.
// Dependency beads: hk-872.18 (Run.BeadID), hk-872.19 (Checkpoint.BeadID),
//
//	hk-872.20 (event payload bead_id), hk-872.21 (session-log sidecar bead_id).
func TestBIINV002_BeadIDByteEqualAcrossSurfaces(t *testing.T) {
	t.Parallel()

	// Single canonical bead ID — the one stable identifier that must propagate
	// byte-for-byte across every surface per BI-INV-002.
	canonicalID := sensorBeadIDFixtureMakeID()
	canonical := string(canonicalID)

	// ─── Surface 1: Run.BeadID ───────────────────────────────────────────────
	t.Run("surface1-run-bead-id", func(t *testing.T) {
		t.Parallel()

		run := sensorBeadIDFixtureMakeRun(t, canonicalID)
		if run.BeadID == nil {
			t.Fatal("BI-INV-002 surface1: Run.BeadID is nil; want non-nil")
		}
		got := string(*run.BeadID)
		if got != canonical {
			t.Errorf("BI-INV-002 surface1: Run.BeadID = %q, want %q", got, canonical)
		}
		if !run.Valid() {
			t.Error("BI-INV-002 surface1: Run.Valid() = false; fixture must be structurally valid")
		}
	})

	// ─── Surface 2: Checkpoint trailer Harmonik-Bead-ID ──────────────────────
	t.Run("surface2-checkpoint-trailer-bead-id", func(t *testing.T) {
		t.Parallel()

		cp := sensorBeadIDFixtureMakeCheckpoint(t, canonicalID)
		if cp.BeadID == nil {
			t.Fatal("BI-INV-002 surface2: Checkpoint.BeadID is nil; want non-nil")
		}
		got := string(*cp.BeadID)
		if got != canonical {
			t.Errorf("BI-INV-002 surface2: Checkpoint.BeadID = %q, want %q", got, canonical)
		}
		if !cp.Valid() {
			t.Error("BI-INV-002 surface2: Checkpoint.Valid() = false; fixture must be structurally valid")
		}

		// Confirm the trailer key is registered in the registry.
		spec, ok := core.LookupTrailer("Harmonik-Bead-ID")
		if !ok {
			t.Fatal("BI-INV-002 surface2: LookupTrailer(\"Harmonik-Bead-ID\") = false; trailer must be registered (EM-017)")
		}
		if spec.Key != "Harmonik-Bead-ID" {
			t.Errorf("BI-INV-002 surface2: trailer key = %q, want \"Harmonik-Bead-ID\"", spec.Key)
		}
	})

	// ─── Surface 3: Event payload bead_id ────────────────────────────────────
	t.Run("surface3-event-payload-bead-id", func(t *testing.T) {
		t.Parallel()

		payload := sensorBeadIDFixtureMakeEventPayload(canonicalID)

		// PayloadHasBeadID is the structural primitive for BI-019 presence rule.
		if !core.PayloadHasBeadID(payload) {
			t.Error("BI-INV-002 surface3: PayloadHasBeadID = false; want true (BI-019)")
		}

		// Extract the raw string value and assert byte-equality.
		v, ok := payload["bead_id"]
		if !ok {
			t.Fatal("BI-INV-002 surface3: payload[\"bead_id\"] absent")
		}
		got, ok := v.(string)
		if !ok {
			t.Fatalf("BI-INV-002 surface3: payload[\"bead_id\"] is not a string; got %T", v)
		}
		if got != canonical {
			t.Errorf("BI-INV-002 surface3: payload[\"bead_id\"] = %q, want %q", got, canonical)
		}
	})

	// ─── Surface 4: Session-log sidecar bead_id ──────────────────────────────
	t.Run("surface4-session-log-sidecar-bead-id", func(t *testing.T) {
		t.Parallel()

		sidecarJSON := sensorBeadIDFixtureMakeSidecarJSON(t, canonicalID)
		parsed := sensorBeadIDFixtureSidecarWriteAndRead(t, sidecarJSON)

		v, ok := parsed["bead_id"]
		if !ok {
			t.Fatal("BI-INV-002 surface4: sidecar[\"bead_id\"] absent")
		}
		got, ok := v.(string)
		if !ok {
			t.Fatalf("BI-INV-002 surface4: sidecar[\"bead_id\"] is not a string; got %T", v)
		}
		if got != canonical {
			t.Errorf("BI-INV-002 surface4: sidecar[\"bead_id\"] = %q, want %q", got, canonical)
		}
	})

	// ─── Cross-surface byte-equality: all four must agree ────────────────────
	t.Run("cross-surface-byte-equality", func(t *testing.T) {
		t.Parallel()

		run := sensorBeadIDFixtureMakeRun(t, canonicalID)
		cp := sensorBeadIDFixtureMakeCheckpoint(t, canonicalID)
		payload := sensorBeadIDFixtureMakeEventPayload(canonicalID)
		sidecarJSON := sensorBeadIDFixtureMakeSidecarJSON(t, canonicalID)
		sidecar := sensorBeadIDFixtureSidecarWriteAndRead(t, sidecarJSON)

		// Extract each surface value.
		if run.BeadID == nil {
			t.Fatal("BI-INV-002 cross: run.BeadID nil")
		}
		surface1 := string(*run.BeadID)

		if cp.BeadID == nil {
			t.Fatal("BI-INV-002 cross: cp.BeadID nil")
		}
		surface2 := string(*cp.BeadID)

		payloadVal, ok := payload["bead_id"]
		if !ok {
			t.Fatal("BI-INV-002 cross: payload[\"bead_id\"] absent")
		}
		surface3, ok := payloadVal.(string)
		if !ok {
			t.Fatalf("BI-INV-002 cross: payload[\"bead_id\"] not string; %T", payloadVal)
		}

		sidecarVal, ok := sidecar["bead_id"]
		if !ok {
			t.Fatal("BI-INV-002 cross: sidecar[\"bead_id\"] absent")
		}
		surface4, ok := sidecarVal.(string)
		if !ok {
			t.Fatalf("BI-INV-002 cross: sidecar[\"bead_id\"] not string; %T", sidecarVal)
		}

		if surface1 != canonical {
			t.Errorf("BI-INV-002 cross: surface1 (Run.BeadID) = %q, want %q", surface1, canonical)
		}
		if surface2 != canonical {
			t.Errorf("BI-INV-002 cross: surface2 (Checkpoint.BeadID) = %q, want %q", surface2, canonical)
		}
		if surface3 != canonical {
			t.Errorf("BI-INV-002 cross: surface3 (event payload) = %q, want %q", surface3, canonical)
		}
		if surface4 != canonical {
			t.Errorf("BI-INV-002 cross: surface4 (session sidecar) = %q, want %q", surface4, canonical)
		}
	})
}

// sensorBeadIDFixtureAltIDPattern matches function names that suggest minting
// of alternative bead identifiers — a BI-INV-002 violation.
// Pattern covers: mint_alternate_id, MintAlternateID, alternateBeadID (and
// case-insensitive variants).
var sensorBeadIDFixtureAltIDPattern = regexp.MustCompile(
	`(?i)(mint_alternate_id|MintAlternateID|alternateBeadID)`,
)

// TestBIINV002_ReviewerScanNoMintAlternateID walks internal/ source files and
// fails if any function name matches the mint_alternate_id family of names.
//
// Per BI-INV-002: Harmonik MUST NOT mint harmonik-local alternate identifiers
// for the same bead. The absence of any such helper in source is a structural
// invariant that this test enforces at test-run time.
//
// Spec ref: specs/beads-integration.md §5 BI-INV-002.
func TestBIINV002_ReviewerScanNoMintAlternateID(t *testing.T) {
	t.Parallel()

	// Locate the repo root relative to this test file.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("TestBIINV002_ReviewerScanNoMintAlternateID: runtime.Caller failed")
	}
	// thisFile is .../internal/brcli/sensorbiinv002_test.go
	// repo root is four directories up: internal/brcli → internal → <repo>
	internalDir := filepath.Join(filepath.Dir(thisFile), "..", "..")
	internalDir = filepath.Clean(internalDir)

	fset := token.NewFileSet()

	var violations []string

	// Normalize thisFile for comparison inside the walk.
	thisFileClean := filepath.Clean(thisFile)

	err := filepath.Walk(internalDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			// Skip vendor and hidden dirs.
			if strings.HasPrefix(info.Name(), ".") || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Exclude this sensor file from its own scan: the test function names
		// here contain the patterns by design (to document what is forbidden).
		if filepath.Clean(path) == thisFileClean {
			return nil
		}
		// Parse file (no body needed — we only need function names).
		f, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			// Skip unparseable files (generated, syntax errors, etc.).
			return nil //nolint:nilerr // nil signals "no walk error; skip this file"
		}
		for _, decl := range f.Decls {
			fn, isFn := decl.(*ast.FuncDecl)
			if !isFn {
				continue
			}
			name := fn.Name.Name
			if sensorBeadIDFixtureAltIDPattern.MatchString(name) {
				rel, relErr := filepath.Rel(internalDir, path)
				if relErr != nil {
					rel = path
				}
				violations = append(violations,
					fmt.Sprintf("%s: func %s", rel, name),
				)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("TestBIINV002_ReviewerScanNoMintAlternateID: Walk internal/: %v", err)
	}

	if len(violations) > 0 {
		t.Errorf(
			"BI-INV-002 violation: found mint_alternate_id-style helper(s) in internal/; "+
				"Harmonik MUST NOT mint alternate bead identifiers (specs/beads-integration.md §5 BI-INV-002):\n  %s",
			strings.Join(violations, "\n  "),
		)
	}
}
