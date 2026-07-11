package agentmanifest

// brief_test.go — unit tests for BuildBootDoc, renderers, and helpers.
// Bead: hk-j784q (T3 — brief command + boot-document ORDER).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeTypeFolder scaffolds a minimal valid type folder in dir for testing.
// soulLines and opLines override the defaults when non-empty.
func makeTypeFolder(t *testing.T, agentsDir, typeName, parentIntent, soulLines, opLines string) {
	t.Helper()
	dir := filepath.Join(agentsDir, typeName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}

	manifest := "type: " + typeName + "\n" +
		"cardinality: { min: 0, max: n }\n" +
		"harness: claude\n" +
		"identity:\n" +
		"  soul: soul.md\n" +
		"  parent_intent: " + parentIntent + "\n" +
		"context: []\n" +
		"triggers: []\n" +
		"handoff:\n" +
		"  channel: private\n" +
		"keeper:\n" +
		"  thresholds: default\n" +
		"lifecycle:\n" +
		"  self_restart: true\n" +
		"markers:\n" +
		"  never_emits: []\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if soulLines == "" {
		soulLines = "I am " + typeName + " — the default test soul.\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "soul.md"), []byte(soulLines), 0o644); err != nil {
		t.Fatalf("write soul: %v", err)
	}

	if opLines == "" {
		opLines = "## Loop\n1. Do work.\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "operating.md"), []byte(opLines), 0o644); err != nil {
		t.Fatalf("write operating: %v", err)
	}
}

// TestExtractIAmLine checks that extractIAmLine finds the "I am" sentence.
func TestExtractIAmLine(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain",
			input: "I am captain — I run the show.\n",
			want:  "I am captain — I run the show.",
		},
		{
			name:  "bold markdown",
			input: "# Captain\n\n**I am** captain — I oversee the fleet.\n",
			want:  "**I am** captain — I oversee the fleet.",
		},
		{
			name:  "list marker",
			input: "# Soul\n- I am crew — I work beads.\n",
			want:  "I am crew — I work beads.",
		},
		{
			name:  "no match",
			input: "# Title\nSome other content.\n",
			want:  "",
		},
		{
			name:  "empty",
			input: "",
			want:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractIAmLine(tc.input)
			if got != tc.want {
				t.Errorf("extractIAmLine = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBuildBootDoc_SoulByteIdentical verifies that BootDoc.Soul == raw soul.md content.
func TestBuildBootDoc_SoulByteIdentical(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	soulContent := "I am crew — I work beads on an epic until the queue drains.\n\n## I do\n- Claim and complete beads.\n"
	// Parent type (captain) so crew's parent_intent resolves.
	makeTypeFolder(t, agentsDir, "captain", "operator", "I am captain — I run the fleet.\n", "")
	makeTypeFolder(t, agentsDir, "crew", "captain", soulContent, "## Loop\n1. Pick bead.\n")

	doc, err := BuildBootDoc(agentsDir, tmpDir, "crew", "crew", "fresh")
	if err != nil {
		t.Fatalf("BuildBootDoc: %v", err)
	}
	if doc.Soul != soulContent {
		t.Errorf("Soul not byte-identical:\ngot:  %q\nwant: %q", doc.Soul, soulContent)
	}
}

// TestBuildBootDoc_ParentIntentOperator verifies operator terminal grafting.
func TestBuildBootDoc_ParentIntentOperator(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	makeTypeFolder(t, agentsDir, "admiral", "operator", "", "")

	doc, err := BuildBootDoc(agentsDir, tmpDir, "admiral", "admiral", "fresh")
	if err != nil {
		t.Fatalf("BuildBootDoc: %v", err)
	}
	if doc.ParentIntent != operatorIntentLine {
		t.Errorf("ParentIntent = %q, want %q", doc.ParentIntent, operatorIntentLine)
	}
}

// TestBuildBootDoc_ParentIntentGrafted verifies the parent's "I am" line is grafted.
func TestBuildBootDoc_ParentIntentGrafted(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	makeTypeFolder(t, agentsDir, "captain", "operator", "I am captain — I oversee the fleet.\n", "")
	makeTypeFolder(t, agentsDir, "crew", "captain", "", "")

	doc, err := BuildBootDoc(agentsDir, tmpDir, "crew", "crew", "fresh")
	if err != nil {
		t.Fatalf("BuildBootDoc: %v", err)
	}
	if !strings.HasPrefix(doc.ParentIntent, "I am captain") {
		t.Errorf("ParentIntent = %q, want prefix \"I am captain\"", doc.ParentIntent)
	}
}

// TestBuildBootDoc_ActiveTriggersOnly verifies only enabled triggers are included.
func TestBuildBootDoc_ActiveTriggersOnly(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(agentsDir, "crew")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `type: crew
cardinality: { min: 0, max: n }
harness: claude
identity:
  soul: soul.md
  parent_intent: operator
context: []
triggers:
  - { id: queue,   source: queue,  enabled: true  }
  - { id: reports, source: cron,   enabled: false }
handoff:
  channel: private
keeper:
  thresholds: default
lifecycle:
  self_restart: true
markers:
  never_emits: []
`
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "soul.md"), []byte("I am crew.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "operating.md"), []byte("Loop.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	doc, err := BuildBootDoc(agentsDir, tmpDir, "crew", "crew", "fresh")
	if err != nil {
		t.Fatalf("BuildBootDoc: %v", err)
	}
	if len(doc.ActiveTriggers) != 1 {
		t.Errorf("ActiveTriggers count = %d, want 1", len(doc.ActiveTriggers))
	}
	if doc.ActiveTriggers[0].ID != "queue" {
		t.Errorf("ActiveTriggers[0].ID = %q, want %q", doc.ActiveTriggers[0].ID, "queue")
	}
}

// TestBuildBootDoc_WakeDefault verifies "" wake defaults to "fresh".
func TestBuildBootDoc_WakeDefault(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeTypeFolder(t, agentsDir, "crew", "operator", "", "")

	doc, err := BuildBootDoc(agentsDir, tmpDir, "crew", "crew", "")
	if err != nil {
		t.Fatalf("BuildBootDoc: %v", err)
	}
	if doc.WakeReason != "fresh" {
		t.Errorf("WakeReason = %q, want %q", doc.WakeReason, "fresh")
	}
}

// TestBuildBootDoc_HandoffRead verifies HANDOFF-<agent>.md is read when present.
func TestBuildBootDoc_HandoffRead(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeTypeFolder(t, agentsDir, "crew", "operator", "", "")

	handoffContent := "# HANDOFF-crew\n\nOpen: bead hk-abc.\n"
	handoffPath := filepath.Join(tmpDir, "HANDOFF-crew.md")
	if err := os.WriteFile(handoffPath, []byte(handoffContent), 0o644); err != nil {
		t.Fatal(err)
	}

	doc, err := BuildBootDoc(agentsDir, tmpDir, "crew", "crew", "fresh")
	if err != nil {
		t.Fatalf("BuildBootDoc: %v", err)
	}
	if doc.Handoff != handoffContent {
		t.Errorf("Handoff = %q, want %q", doc.Handoff, handoffContent)
	}
}

// TestBuildBootDoc_HandoffAbsent verifies empty Handoff when no file exists.
func TestBuildBootDoc_HandoffAbsent(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeTypeFolder(t, agentsDir, "crew", "operator", "", "")

	doc, err := BuildBootDoc(agentsDir, tmpDir, "leto", "crew", "fresh")
	if err != nil {
		t.Fatalf("BuildBootDoc: %v", err)
	}
	if doc.Handoff != "" {
		t.Errorf("Handoff = %q, want empty (no HANDOFF-leto.md)", doc.Handoff)
	}
}

// TestBuildBootDoc_NoFilesystemWrites verifies zero writes during BuildBootDoc.
func TestBuildBootDoc_NoFilesystemWrites(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeTypeFolder(t, agentsDir, "crew", "operator", "", "")

	beforeEntries, _ := os.ReadDir(tmpDir)

	_, err := BuildBootDoc(agentsDir, tmpDir, "crew", "crew", "fresh")
	if err != nil {
		t.Fatalf("BuildBootDoc: %v", err)
	}

	afterEntries, _ := os.ReadDir(tmpDir)
	if len(afterEntries) != len(beforeEntries) {
		t.Errorf("BuildBootDoc wrote files: before=%d after=%d", len(beforeEntries), len(afterEntries))
	}
}

// TestRenderMarkdown_SectionOrder verifies §4 ordering: identity before wake before operating
// before triggers before handoff.
func TestRenderMarkdown_SectionOrder(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeTypeFolder(t, agentsDir, "crew", "operator", "I am crew — test soul.\n", "## Loop\n1. Work.\n")

	doc, err := BuildBootDoc(agentsDir, tmpDir, "crew", "crew", "keeper-restart")
	if err != nil {
		t.Fatalf("BuildBootDoc: %v", err)
	}

	var buf strings.Builder
	RenderMarkdown(doc, &buf)
	out := buf.String()

	sections := []string{
		"## Identity",
		"## Wake reason",
		"## Operating instructions",
		"## Active triggers",
		"## Handoff",
	}
	pos := -1
	for _, sec := range sections {
		idx := strings.Index(out, sec)
		if idx == -1 {
			t.Errorf("section %q not found in output", sec)
			continue
		}
		if idx <= pos {
			t.Errorf("section %q appears before previous section (pos %d <= %d)", sec, idx, pos)
		}
		pos = idx
	}
}

// TestRenderMarkdown_SoulBeforeHandoff verifies the provenance rule: identity first, handoff last.
func TestRenderMarkdown_SoulBeforeHandoff(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	soulContent := "I am crew — unique-soul-marker.\n"
	makeTypeFolder(t, agentsDir, "crew", "operator", soulContent, "")

	handoffContent := "# HANDOFF-crew\nunique-handoff-marker\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "HANDOFF-crew.md"), []byte(handoffContent), 0o644); err != nil {
		t.Fatal(err)
	}

	doc, err := BuildBootDoc(agentsDir, tmpDir, "crew", "crew", "fresh")
	if err != nil {
		t.Fatalf("BuildBootDoc: %v", err)
	}
	var buf strings.Builder
	RenderMarkdown(doc, &buf)
	out := buf.String()

	soulIdx := strings.Index(out, "unique-soul-marker")
	handoffIdx := strings.Index(out, "unique-handoff-marker")
	if soulIdx == -1 || handoffIdx == -1 {
		t.Fatalf("markers not found: soulIdx=%d handoffIdx=%d\nout:\n%s", soulIdx, handoffIdx, out)
	}
	if soulIdx >= handoffIdx {
		t.Errorf("soul content appears after handoff (soulIdx=%d >= handoffIdx=%d)", soulIdx, handoffIdx)
	}
}

// TestRenderMarkdown_HandoffClaimHeader verifies the Handoff section is stamped as a CLAIM
// that `harmonik digest` overrides, both when a handoff is present and when it is absent.
func TestRenderMarkdown_HandoffClaimHeader(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeTypeFolder(t, agentsDir, "crew", "operator", "", "")

	handoffContent := "# HANDOFF-crew\nsome prior-session claim\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "HANDOFF-crew.md"), []byte(handoffContent), 0o644); err != nil {
		t.Fatal(err)
	}

	doc, err := BuildBootDoc(agentsDir, tmpDir, "crew", "crew", "fresh")
	if err != nil {
		t.Fatalf("BuildBootDoc: %v", err)
	}

	const wantClaim = "CLAIM, not ground truth"
	const wantOverride = "harmonik digest"

	var mdBuf strings.Builder
	RenderMarkdown(doc, &mdBuf)
	md := mdBuf.String()
	handoffSection := md[strings.Index(md, "## Handoff"):]
	if !strings.Contains(handoffSection, wantClaim) || !strings.Contains(handoffSection, wantOverride) {
		t.Errorf("RenderMarkdown Handoff section missing CLAIM header, got:\n%s", handoffSection)
	}
	if idx := strings.Index(handoffSection, wantClaim); idx > strings.Index(handoffSection, "some prior-session claim") {
		t.Errorf("CLAIM header must precede handoff content")
	}

	var toonBuf strings.Builder
	RenderToon(doc, &toonBuf)
	toon := toonBuf.String()
	toonHandoffSection := toon[strings.Index(toon, "HANDOFF"):]
	if !strings.Contains(toonHandoffSection, wantClaim) || !strings.Contains(toonHandoffSection, wantOverride) {
		t.Errorf("RenderToon Handoff section missing CLAIM header, got:\n%s", toonHandoffSection)
	}
}

// TestBuildBootDoc_CronTriggerActivityGuard verifies that a cron trigger with activity_guard
// is included in ActiveTriggers and that RenderMarkdown includes the activity_guard value.
func TestBuildBootDoc_CronTriggerActivityGuard(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(agentsDir, "admiral")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `type: admiral
cardinality: { min: 0, max: 1 }
harness: claude
identity:
  soul: soul.md
  parent_intent: operator
context: []
triggers:
  - id: alignment-audit
    source: cron
    every: 1h
    enabled: true
    deliver: comms
    message: "Run the hourly alignment audit."
    activity_guard: 24h
handoff:
  channel: private
keeper:
  thresholds: default
lifecycle:
  self_restart: true
markers:
  never_emits: []
`
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "soul.md"), []byte("I am admiral.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "operating.md"), []byte("## Loop\n1. Direct.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	doc, err := BuildBootDoc(agentsDir, tmpDir, "admiral", "admiral", "fresh")
	if err != nil {
		t.Fatalf("BuildBootDoc: %v", err)
	}
	if len(doc.ActiveTriggers) != 1 {
		t.Fatalf("ActiveTriggers count = %d, want 1", len(doc.ActiveTriggers))
	}
	tr := doc.ActiveTriggers[0]
	if tr.Source != "cron" {
		t.Errorf("trigger source = %q, want %q", tr.Source, "cron")
	}
	if tr.ActivityGuard != "24h" {
		t.Errorf("trigger activity_guard = %q, want %q", tr.ActivityGuard, "24h")
	}

	var buf strings.Builder
	RenderMarkdown(doc, &buf)
	out := buf.String()
	if !strings.Contains(out, "activity_guard: 24h") {
		t.Errorf("rendered output missing activity_guard: 24h\noutput:\n%s", out)
	}
	if !strings.Contains(out, "alignment-audit") {
		t.Errorf("rendered output missing trigger id 'alignment-audit'\noutput:\n%s", out)
	}
}

// TestRenderMarkdown_EnabledFalseTriggersAbsent verifies that disabled triggers
// are not included in §4 of the rendered boot doc.
func TestRenderMarkdown_EnabledFalseTriggersAbsent(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(agentsDir, "crew")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `type: crew
cardinality: { min: 0, max: n }
harness: claude
identity:
  soul: soul.md
  parent_intent: operator
context: []
triggers:
  - { id: queue, source: queue, enabled: true }
  - id: disabled-report
    source: cron
    every: 6h
    enabled: false
    deliver: comms
    message: "This should not appear."
handoff:
  channel: private
keeper:
  thresholds: default
lifecycle:
  self_restart: true
markers:
  never_emits: []
`
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "soul.md"), []byte("I am crew.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "operating.md"), []byte("## Loop\n1. Work.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	doc, err := BuildBootDoc(agentsDir, tmpDir, "crew", "crew", "fresh")
	if err != nil {
		t.Fatalf("BuildBootDoc: %v", err)
	}
	if len(doc.ActiveTriggers) != 1 {
		t.Fatalf("ActiveTriggers count = %d, want 1 (enabled only)", len(doc.ActiveTriggers))
	}

	var buf strings.Builder
	RenderMarkdown(doc, &buf)
	out := buf.String()
	if strings.Contains(out, "disabled-report") {
		t.Errorf("disabled trigger 'disabled-report' appears in rendered output")
	}
	if strings.Contains(out, "This should not appear") {
		t.Errorf("disabled trigger message appears in rendered output")
	}
}

// TestRenderJSON_Roundtrip verifies json output can be decoded back to a BootDoc.
func TestRenderJSON_Roundtrip(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeTypeFolder(t, agentsDir, "crew", "operator", "I am crew.\n", "Loop.\n")

	doc, err := BuildBootDoc(agentsDir, tmpDir, "crew", "crew", "fresh")
	if err != nil {
		t.Fatalf("BuildBootDoc: %v", err)
	}
	var buf strings.Builder
	if err := RenderJSON(doc, &buf); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var decoded BootDoc
	if err := json.Unmarshal([]byte(buf.String()), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v\noutput:\n%s", err, buf.String())
	}
	if decoded.Soul != doc.Soul {
		t.Errorf("roundtrip Soul mismatch: got %q, want %q", decoded.Soul, doc.Soul)
	}
	if decoded.WakeReason != doc.WakeReason {
		t.Errorf("roundtrip WakeReason mismatch: got %q, want %q", decoded.WakeReason, doc.WakeReason)
	}
}
