package handlercontract_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// skillsprovisioned_hc049_test.go — sensor tests for HC-049 (emit
// skills_provisioned before agent_ready) per bead hk-8i31.58.
//
// Verifies:
//   (a) ProgressMsgTypeSkillsProvisioned constant value.
//   (b) SkillsProvisionedMsg wire struct shape and JSON field names.
//   (c) SkillProvisionedEntry wire struct (including optional version field).
//   (d) Spec-corpus ordering: handler-contract.md §4.11.HC-049 / §5 HC-INV-004
//       must state that skills_provisioned precedes agent_ready.
//
// Helper prefix: skillsProvisionedFixture (per implementer-protocol.md).

// skillsProvisionedFixtureModuleRoot returns the module root by walking
// upward from this test file until a go.mod is found.
func skillsProvisionedFixtureModuleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod in any parent directory")
		}
		dir = parent
	}
}

// skillsProvisionedFixtureValidMsg returns a well-formed SkillsProvisionedMsg
// for shape tests.
func skillsProvisionedFixtureValidMsg(t *testing.T) handlercontract.SkillsProvisionedMsg {
	t.Helper()
	version := "1.2.3"
	return handlercontract.SkillsProvisionedMsg{
		Type:      handlercontract.ProgressMsgTypeSkillsProvisioned,
		RunID:     "0196f100-0000-7000-8000-000000000001",
		SessionID: "sess-001",
		Skills: []handlercontract.SkillProvisionedEntry{
			{
				Name:       "claude-cli",
				SourcePath: "/skills/claude-cli",
				Version:    &version,
			},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-049: ProgressMsgTypeSkillsProvisioned constant
// ─────────────────────────────────────────────────────────────────────────────

// TestSkillsProvisioned_ConstantValue verifies that ProgressMsgTypeSkillsProvisioned
// equals "skills_provisioned" per §4.11.HC-049.
func TestSkillsProvisioned_ConstantValue(t *testing.T) {
	t.Parallel()

	if handlercontract.ProgressMsgTypeSkillsProvisioned != "skills_provisioned" {
		t.Errorf("ProgressMsgTypeSkillsProvisioned = %q; want \"skills_provisioned\"",
			handlercontract.ProgressMsgTypeSkillsProvisioned)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-049: SkillsProvisionedMsg wire struct
// ─────────────────────────────────────────────────────────────────────────────

// TestSkillsProvisioned_MsgJSONFieldNames verifies that SkillsProvisionedMsg
// marshals with the spec-mandated wire field names per event-model.md §8.3.8.
func TestSkillsProvisioned_MsgJSONFieldNames(t *testing.T) {
	t.Parallel()

	msg := skillsProvisionedFixtureValidMsg(t)
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	required := []string{"type", "run_id", "session_id", "skills"}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON field %q missing from SkillsProvisionedMsg", key)
		}
	}
}

// TestSkillsProvisioned_MsgTypeFieldMatchesConstant verifies that the Type
// field set to ProgressMsgTypeSkillsProvisioned survives JSON round-trip.
func TestSkillsProvisioned_MsgTypeFieldMatchesConstant(t *testing.T) {
	t.Parallel()

	msg := skillsProvisionedFixtureValidMsg(t)
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded handlercontract.SkillsProvisionedMsg
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.Type != handlercontract.ProgressMsgTypeSkillsProvisioned {
		t.Errorf("SkillsProvisionedMsg.Type round-trip: got %q, want %q",
			decoded.Type, handlercontract.ProgressMsgTypeSkillsProvisioned)
	}
}

// TestSkillsProvisioned_MsgSkillsIsArrayNotNull verifies that the skills field
// encodes as a JSON array (not null) when non-nil, even when empty.
func TestSkillsProvisioned_MsgSkillsIsArrayNotNull(t *testing.T) {
	t.Parallel()

	msg := handlercontract.SkillsProvisionedMsg{
		Type:      handlercontract.ProgressMsgTypeSkillsProvisioned,
		RunID:     "0196f100-0000-7000-8000-000000000001",
		SessionID: "sess-001",
		Skills:    []handlercontract.SkillProvisionedEntry{}, // empty but non-nil
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	skillsRaw, ok := raw["skills"]
	if !ok {
		t.Fatal("skills field missing from JSON")
	}
	if skillsRaw == nil {
		t.Error("skills marshalled as null; want JSON array (even when empty)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-049: SkillProvisionedEntry wire struct
// ─────────────────────────────────────────────────────────────────────────────

// TestSkillsProvisioned_EntryJSONFieldNames verifies that SkillProvisionedEntry
// marshals with the spec-mandated wire field names per event-model.md §8.3.8.
func TestSkillsProvisioned_EntryJSONFieldNames(t *testing.T) {
	t.Parallel()

	version := "1.0.0"
	entry := handlercontract.SkillProvisionedEntry{
		Name:       "my-skill",
		SourcePath: "/skills/my-skill",
		Version:    &version,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	required := []string{"name", "source_path", "version"}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON field %q missing from SkillProvisionedEntry", key)
		}
	}
}

// TestSkillsProvisioned_EntryVersionOmittedWhenNil verifies that the optional
// version field is omitted when nil (omitempty per event-model.md §8.3.8 "version?").
func TestSkillsProvisioned_EntryVersionOmittedWhenNil(t *testing.T) {
	t.Parallel()

	entry := handlercontract.SkillProvisionedEntry{
		Name:       "my-skill",
		SourcePath: "/skills/my-skill",
		Version:    nil, // absent
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	if _, ok := m["version"]; ok {
		t.Error("version field present in JSON when Version is nil; want omitted (omitempty)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-049: Ordering invariant — spec-corpus sensor
// ─────────────────────────────────────────────────────────────────────────────

// TestSkillsProvisioned_SpecCorpusOrderingClause verifies that the handler-
// contract spec (specs/handler-contract.md) contains the HC-049 ordering
// obligation: skills_provisioned MUST precede agent_ready.
//
// This is a spec-corpus sensor: it reads the normative spec file and checks
// for the ordering clause. The test fails if the spec is missing or the
// ordering clause is absent, signaling spec drift.
//
// Spec refs: handler-contract.md §4.11.HC-049 and §5 HC-INV-004.
func TestSkillsProvisioned_SpecCorpusOrderingClause(t *testing.T) {
	t.Parallel()

	root := skillsProvisionedFixtureModuleRoot(t)
	specPath := filepath.Join(root, "specs", "handler-contract.md")

	content, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading handler-contract.md: %v (spec file missing?)", err)
	}

	specText := string(content)

	// HC-049 must be present.
	if !strings.Contains(specText, "HC-049") {
		t.Error("handler-contract.md missing HC-049 clause; skills_provisioned ordering obligation may have been removed from spec")
	}

	// The ordering clause must state skills_provisioned precedes agent_ready.
	// We check for the canonical ordering string from §5 HC-INV-004:
	//   handler_capabilities → session_log_location → skills_provisioned → agent_ready
	orderingIndicator := "skills_provisioned"
	agentReadyIndicator := "agent_ready"
	skillsIdx := strings.Index(specText, orderingIndicator)
	readyIdx := strings.Index(specText, agentReadyIndicator)

	if skillsIdx < 0 {
		t.Error("handler-contract.md missing \"skills_provisioned\" token; HC-049 ordering obligation may have drifted")
	}
	if readyIdx < 0 {
		t.Error("handler-contract.md missing \"agent_ready\" token")
	}

	// Verify there's a section where skills_provisioned appears before agent_ready
	// in the ordering sequence (both appear in the same ordering clause in §5).
	// We look for the sequence in the HC-INV-004 invariant text.
	inv004 := "HC-INV-004"
	inv004Idx := strings.Index(specText, inv004)
	if inv004Idx < 0 {
		t.Error("handler-contract.md missing HC-INV-004 invariant; ordering invariant may have been removed")
	}

	// Search for the HC-INV-004 invariant definition section (not just first
	// mention). The definition is at the "#### HC-INV-004" heading; search all
	// occurrences and check each window for the skills_provisioned reference.
	const lookAheadWindow = 1024
	found := false
	searchFrom := 0
	for {
		idx := strings.Index(specText[searchFrom:], inv004)
		if idx < 0 {
			break
		}
		abs := searchFrom + idx
		section := specText[abs:]
		if len(section) > lookAheadWindow {
			section = section[:lookAheadWindow]
		}
		if strings.Contains(section, "skills_provisioned") {
			found = true
			break
		}
		searchFrom = abs + 1
	}
	if !found {
		t.Errorf("No HC-INV-004 section found that references skills_provisioned; " +
			"ordering invariant may not include skills_provisioned in the spec")
	}
}
