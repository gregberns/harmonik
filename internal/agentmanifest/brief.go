// Package agentmanifest builds and renders agent boot documents.
// Spec: .kerf/works/agent-manifest/SPEC.md §3–§4.
// Bead: hk-j784q (T3 — brief command + boot-document ORDER, emit-only).
package agentmanifest

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// operatorIntentLine is the fixed grafted line when parent_intent is the terminal "operator".
const operatorIntentLine = "I am the operator — the human who created and directs this fleet."

// handoffClaimHeader stamps the embedded handoff as a CLAIM from the prior session, not
// ground truth: `harmonik digest` is the live-state source that overrides it. See
// plans/2026-07-11-captain-startup-revamp/01-revamp-process.md Step 0.4(b).
const handoffClaimHeader = "**CLAIM, not ground truth — `harmonik digest` overrides.**"

// SkillEntry is a single skill reference in the boot document.
type SkillEntry struct {
	Name      string `json:"name"       yaml:"name"`
	ShortDesc string `json:"short_desc" yaml:"short_desc"`
	Pointer   string `json:"pointer"    yaml:"pointer"`
	// Presence mirrors the manifest context[].presence field: injected | retrieved | embodied.
	Presence string `json:"presence" yaml:"presence"`
}

// BootDoc is the structured boot document emitted by BuildBootDoc.
// Sections are ordered per SPEC §4: identity → wake → operating+skills → triggers → handoff.
// Handoff is empty string when no HANDOFF-<agent>.md file exists.
type BootDoc struct {
	AgentName      string       `json:"agent_name"      yaml:"agent_name"`
	TypeName       string       `json:"type_name"       yaml:"type_name"`
	Soul           string       `json:"soul"            yaml:"soul"`
	ParentIntent   string       `json:"parent_intent"   yaml:"parent_intent"`
	WakeReason     string       `json:"wake_reason"     yaml:"wake_reason"`
	Operating      string       `json:"operating"       yaml:"operating"`
	Skills         []SkillEntry `json:"skills"          yaml:"skills"`
	Docs           []SkillEntry `json:"docs"            yaml:"docs"`
	ActiveTriggers []Trigger    `json:"active_triggers" yaml:"active_triggers"`
	Handoff        string       `json:"handoff"         yaml:"handoff"`
}

// BuildBootDoc assembles the boot document for an agent.
//
// agentsDir is the absolute path to .harmonik/agents/.
// repoRoot is the project root (for path-bearing context refs and HANDOFF-<name>.md lookup).
// agentName is the instance name (may equal typeName when a bare type was requested).
// typeName is the already-resolved type name.
// wake is the wake reason string (fresh | keeper-restart | trigger:<id>); "" defaults to "fresh".
//
// No filesystem writes occur — this is emit-only (SPEC I2).
func BuildBootDoc(agentsDir, repoRoot, agentName, typeName, wake string) (*BootDoc, error) {
	tf, err := Load(agentsDir, typeName)
	if err != nil {
		return nil, err
	}

	parentIntent, err := resolveParentIntent(agentsDir, tf.Manifest.Identity.ParentIntent)
	if err != nil {
		return nil, err
	}

	var skills []SkillEntry
	var docs []SkillEntry
	for _, c := range tf.Manifest.Context {
		switch c.As {
		case "skill":
			skills = append(skills, buildSkillEntry(agentsDir, typeName, repoRoot, c))
		case "doc":
			docs = append(docs, buildDocEntry(agentsDir, typeName, repoRoot, c))
		}
	}

	var activeTriggers []Trigger
	for _, t := range tf.Manifest.Triggers {
		if t.Enabled {
			activeTriggers = append(activeTriggers, t)
		}
	}

	if wake == "" {
		wake = "fresh"
	}

	return &BootDoc{
		AgentName:      agentName,
		TypeName:       typeName,
		Soul:           tf.SoulContent,
		ParentIntent:   parentIntent,
		WakeReason:     wake,
		Operating:      tf.OperatingContent,
		Skills:         skills,
		Docs:           docs,
		ActiveTriggers: activeTriggers,
		Handoff:        readHandoff(repoRoot, agentName),
	}, nil
}

// resolveParentIntent reads the "I am" line from the parent type's soul.md.
// parentType may be "operator" (the terminal — no folder) or an existing type folder name.
func resolveParentIntent(agentsDir, parentType string) (string, error) {
	if parentType == "operator" {
		return operatorIntentLine, nil
	}
	soulPath := filepath.Join(agentsDir, parentType, soulFile)
	//nolint:gosec // G304: soulPath is constructed from validated inputs
	data, err := os.ReadFile(soulPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%w: parent type %q has no soul.md", ErrNotFound, parentType)
		}
		return "", fmt.Errorf("agentmanifest: read parent soul %q: %w", soulPath, err)
	}
	if line := extractIAmLine(string(data)); line != "" {
		return line, nil
	}
	return fmt.Sprintf("I am %s", parentType), nil
}

// extractIAmLine scans soulContent for the first line that starts with "I am"
// (with optional leading list marker or bold markdown). Returns "" if not found.
func extractIAmLine(soulContent string) string {
	scanner := bufio.NewScanner(strings.NewReader(soulContent))
	for scanner.Scan() {
		l := strings.TrimSpace(scanner.Text())
		l = strings.TrimPrefix(l, "- ")
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "I am ") || strings.HasPrefix(l, "**I am**") {
			return l
		}
	}
	return ""
}

// buildSkillEntry resolves a skill context entry to a SkillEntry with short-desc and pointer.
func buildSkillEntry(agentsDir, typeName, repoRoot string, c ContextEntry) SkillEntry {
	entry := SkillEntry{
		Name:     c.Ref,
		Presence: c.Presence,
	}

	var resolved string
	if strings.Contains(c.Ref, "/") {
		resolved = filepath.Join(repoRoot, c.Ref)
	} else {
		if p, err := ResolveRef(agentsDir, typeName, c.Ref); err == nil {
			resolved = p
		}
	}
	if resolved == "" {
		return entry
	}

	// Look for SKILL.md inside the resolved directory.
	skillMD := filepath.Join(resolved, "SKILL.md")
	if _, err := os.Stat(skillMD); err == nil {
		entry.Pointer = skillMD
		if c.Presence != "retrieved" {
			entry.ShortDesc = readSkillShortDesc(skillMD)
		}
	} else {
		entry.Pointer = resolved
	}
	return entry
}

// buildDocEntry resolves a doc context entry (as: doc) to a SkillEntry carrying the
// explicit resolved path and a description parsed from the ref's frontmatter, per
// plans/2026-07-11-captain-startup-revamp/02-cutover-and-open-questions.md §2.4.
func buildDocEntry(agentsDir, typeName, repoRoot string, c ContextEntry) SkillEntry {
	entry := SkillEntry{
		Name:     c.Ref,
		Presence: c.Presence,
	}

	var resolved string
	if strings.Contains(c.Ref, "/") {
		resolved = filepath.Join(repoRoot, c.Ref)
	} else if p, err := ResolveRef(agentsDir, typeName, c.Ref); err == nil {
		resolved = p
	}
	if resolved == "" {
		return entry
	}

	entry.Pointer = resolved
	entry.ShortDesc = readFrontmatterDescription(resolved)
	return entry
}

// readFrontmatterDescription returns the `description:` field of the YAML frontmatter
// block (delimited by leading "---" lines) at the top of the file at path.
// Returns "" when the file has no frontmatter or no description field.
func readFrontmatterDescription(path string) string {
	//nolint:gosec // G304: path is resolved by buildDocEntry against known roots
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return ""
	}
	rest := strings.SplitN(content, "\n", 2)[1]
	end := strings.Index(rest, "\n---")
	if end == -1 {
		return ""
	}
	var meta struct {
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(rest[:end]), &meta); err != nil {
		return ""
	}
	return strings.TrimSpace(meta.Description)
}

// readSkillShortDesc returns the first non-blank, non-heading line from skillMDPath.
func readSkillShortDesc(skillMDPath string) string {
	//nolint:gosec // G304: path comes from ResolveRef which validates against known dirs
	data, err := os.ReadFile(skillMDPath)
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		l := strings.TrimSpace(scanner.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		return l
	}
	return ""
}

// readHandoff reads HANDOFF-<agentName>.md from repoRoot. Returns "" if absent.
func readHandoff(repoRoot, agentName string) string {
	path := filepath.Join(repoRoot, fmt.Sprintf("HANDOFF-%s.md", agentName))
	//nolint:gosec // G304: agentName is validated by the caller
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// RenderMarkdown writes the boot document in markdown format to w.
// Sections are emitted in SPEC §4 order: identity → wake → operating+skills → triggers → handoff.
func RenderMarkdown(doc *BootDoc, w io.Writer) error {
	out := &errorWriter{w: w}
	// §1 Identity / SOUL — soul content byte-identical + grafted parent intent.
	out.println("## Identity")
	out.println()
	writeContent(out, doc.Soul)
	out.printf("\n**Parent intent:** %s\n", doc.ParentIntent)

	sectionDivider(out)

	// §2 Wake reason.
	out.println("## Wake reason")
	out.println()
	out.println(doc.WakeReason)

	sectionDivider(out)

	// §3 Operating instructions + skills.
	out.println("## Operating instructions")
	out.println()
	writeContent(out, doc.Operating)

	if len(doc.Skills) > 0 {
		out.println()
		out.println("### Skills")
		out.println()
		for _, s := range doc.Skills {
			renderSkillLine(out, s)
		}
	}

	if len(doc.Docs) > 0 {
		out.println()
		out.println("### Docs")
		out.println()
		for _, d := range doc.Docs {
			renderDocLine(out, d)
		}
	}

	sectionDivider(out)

	// §4 Active triggers.
	out.println("## Active triggers")
	out.println()
	if len(doc.ActiveTriggers) == 0 {
		out.println("_(no active triggers)_")
	} else {
		for _, t := range doc.ActiveTriggers {
			renderTriggerLine(out, t)
		}
	}

	sectionDivider(out)

	// §5 Handoff — LAST (episodic state only; no identity re-statement).
	out.println("## Handoff")
	out.println()
	out.println(handoffClaimHeader)
	out.println()
	if doc.Handoff == "" {
		out.println("_(no handoff on record)_")
	} else {
		writeContent(out, doc.Handoff)
	}
	return out.err
}

// RenderJSON writes the boot document as an indented JSON object to w.
func RenderJSON(doc *BootDoc, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// RenderYAML writes the boot document as a YAML document to w.
func RenderYAML(doc *BootDoc, w io.Writer) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	return enc.Encode(doc)
}

// RenderToon writes the boot document in toon (decorated terminal) format.
// Content is identical to markdown; sections use ASCII box borders.
func RenderToon(doc *BootDoc, w io.Writer) error {
	out := &errorWriter{w: w}
	bar := strings.Repeat("═", 60)
	boxHeader := func(title string) {
		out.printf("\n╔%s╗\n║ %-58s ║\n╚%s╝\n\n", bar, title, bar)
	}

	// §1 Identity.
	boxHeader("IDENTITY")
	writeContent(out, doc.Soul)
	out.printf("\nParent intent: %s\n", doc.ParentIntent)

	// §2 Wake reason.
	boxHeader("WAKE REASON")
	out.println(doc.WakeReason)

	// §3 Operating + skills.
	boxHeader("OPERATING INSTRUCTIONS")
	writeContent(out, doc.Operating)
	renderToonSkills(out, doc.Skills)
	renderToonDocs(out, doc.Docs)

	// §4 Triggers.
	boxHeader("ACTIVE TRIGGERS")
	renderToonTriggers(out, doc.ActiveTriggers)

	// §5 Handoff — LAST.
	boxHeader("HANDOFF")
	out.println(handoffClaimHeader)
	out.println()
	if doc.Handoff == "" {
		out.println("(no handoff on record)")
	} else {
		writeContent(out, doc.Handoff)
	}
	return out.err
}

func renderToonSkills(out *errorWriter, skills []SkillEntry) {
	if len(skills) == 0 {
		return
	}
	out.println("\nSkills:")
	for _, skill := range skills {
		if skill.Presence == "retrieved" || skill.ShortDesc == "" {
			out.printf("  • %s (pull on demand)", skill.Name)
		} else {
			out.printf("  • %s: %s", skill.Name, skill.ShortDesc)
		}
		if skill.Pointer != "" {
			out.printf(" — see %s", skill.Pointer)
		}
		out.println()
	}
}

func renderToonDocs(out *errorWriter, docs []SkillEntry) {
	if len(docs) == 0 {
		return
	}
	out.println("\nDocs:")
	for _, doc := range docs {
		if doc.ShortDesc != "" {
			out.printf("  • %s: %s", doc.Name, doc.ShortDesc)
		} else {
			out.printf("  • %s", doc.Name)
		}
		if doc.Pointer != "" {
			out.printf(" — see %s", doc.Pointer)
		}
		out.println()
	}
}

func renderToonTriggers(out *errorWriter, triggers []Trigger) {
	if len(triggers) == 0 {
		out.println("(no active triggers)")
		return
	}
	for _, trigger := range triggers {
		meta := trigger.Source
		if trigger.Every != "" {
			meta += ", every " + trigger.Every
		}
		if trigger.ActivityGuard != "" {
			meta += ", activity_guard " + trigger.ActivityGuard
		}
		if trigger.Message != "" {
			out.printf("  • %s [%s]: %s\n", trigger.ID, meta, trigger.Message)
		} else {
			out.printf("  • %s [%s]\n", trigger.ID, meta)
		}
	}
}

type errorWriter struct {
	w   io.Writer
	err error
}

func (w *errorWriter) print(args ...any) {
	if w.err != nil {
		return
	}
	_, w.err = fmt.Fprint(w.w, args...)
}

func (w *errorWriter) printf(format string, args ...any) {
	if w.err != nil {
		return
	}
	_, w.err = fmt.Fprintf(w.w, format, args...)
}

func (w *errorWriter) println(args ...any) {
	if w.err != nil {
		return
	}
	_, w.err = fmt.Fprintln(w.w, args...)
}

// sectionDivider writes the markdown section separator.
func sectionDivider(w *errorWriter) {
	w.println()
	w.println("---")
	w.println()
}

// writeContent writes content ensuring it ends with a newline.
func writeContent(w *errorWriter, content string) {
	w.print(content)
	if !strings.HasSuffix(content, "\n") {
		w.println()
	}
}

// renderSkillLine renders a single skill entry as a markdown list item.
func renderSkillLine(w *errorWriter, s SkillEntry) {
	if s.Presence == "retrieved" || s.ShortDesc == "" {
		if s.Pointer != "" {
			w.printf("- **%s** _(pull on demand)_ — see `%s`\n", s.Name, s.Pointer)
		} else {
			w.printf("- **%s** _(pull on demand)_\n", s.Name)
		}
		return
	}
	if s.Pointer != "" {
		w.printf("- **%s:** %s — see `%s`\n", s.Name, s.ShortDesc, s.Pointer)
	} else {
		w.printf("- **%s:** %s\n", s.Name, s.ShortDesc)
	}
}

// renderDocLine renders a single doc entry (as: doc) as a markdown list item, always
// showing the explicit resolved path and, when present, the frontmatter description.
func renderDocLine(w *errorWriter, d SkillEntry) {
	if d.Pointer == "" {
		w.printf("- **%s**\n", d.Name)
		return
	}
	if d.ShortDesc != "" {
		w.printf("- **%s:** %s — see `%s`\n", d.Name, d.ShortDesc, d.Pointer)
	} else {
		w.printf("- **%s** — see `%s`\n", d.Name, d.Pointer)
	}
}

// renderTriggerLine renders a single trigger as a markdown list item.
func renderTriggerLine(w *errorWriter, t Trigger) {
	meta := "source: " + t.Source
	if t.Every != "" {
		meta += ", every: " + t.Every
	}
	if t.ActivityGuard != "" {
		meta += ", activity_guard: " + t.ActivityGuard
	}
	if t.Message != "" {
		w.printf("- **%s** (%s): %s\n", t.ID, meta, t.Message)
	} else {
		w.printf("- **%s** (%s)\n", t.ID, meta)
	}
}
