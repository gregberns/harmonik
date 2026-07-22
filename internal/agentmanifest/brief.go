// Package agentmanifest: brief.go — boot-document builder + renderers (harmonik agent brief).
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
// Handoff is the empty string BOTH when no HANDOFF-<agent>.md exists and when one
// exists but is zero-byte; HandoffPresent disambiguates the two. Refs: hk-4tjyj.
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
	// HandoffPresent reports whether HANDOFF-<agent>.md EXISTS on disk, regardless
	// of its size. Handoff=="" && HandoffPresent is an EMPTY handoff file — the
	// prior session's state was lost, not never written. That distinction was
	// invisible before hk-4tjyj: both rendered "(no handoff on record)", which made
	// a keeper-destroyed handoff indistinguishable from a first boot.
	HandoffPresent bool `json:"handoff_present" yaml:"handoff_present"`
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

	handoffContent, handoffPresent := readHandoff(repoRoot, agentName)

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
		Handoff:        handoffContent,
		HandoffPresent: handoffPresent,
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

// readHandoff reads HANDOFF-<agentName>.md from repoRoot. It returns the content
// and whether the file EXISTS. ("", false) = absent; ("", true) = present but
// zero-byte — a lost handoff, which the renderers must call out loudly rather
// than silently conflate with "never written". Refs: hk-4tjyj.
func readHandoff(repoRoot, agentName string) (string, bool) {
	path := filepath.Join(repoRoot, fmt.Sprintf("HANDOFF-%s.md", agentName))
	//nolint:gosec // G304: agentName is validated by the caller
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// emptyHandoffWarning is the loud, distinct rendering for a handoff file that
// EXISTS but is empty. Refs: hk-4tjyj.
func emptyHandoffWarning(agentName string) string {
	return fmt.Sprintf(
		"**WARNING — HANDOFF-%s.md EXISTS but is EMPTY.** The previous session's handoff was "+
			"lost (not \"never written\"). Do NOT assume there was nothing to carry over: "+
			"re-ground from `harmonik digest`, the bead ledger, and recent git history before acting.",
		agentName)
}

// RenderMarkdown writes the boot document in markdown format to w.
// Sections are emitted in SPEC §4 order: identity → wake → operating+skills → triggers → handoff.
func RenderMarkdown(doc *BootDoc, w io.Writer) {
	// §1 Identity / SOUL — soul content byte-identical + grafted parent intent.
	fmt.Fprintln(w, "## Identity")
	fmt.Fprintln(w)
	writeContent(w, doc.Soul)
	fmt.Fprintf(w, "\n**Parent intent:** %s\n", doc.ParentIntent)

	sectionDivider(w)

	// §2 Wake reason.
	fmt.Fprintln(w, "## Wake reason")
	fmt.Fprintln(w)
	fmt.Fprintln(w, doc.WakeReason)

	sectionDivider(w)

	// §3 Operating instructions + skills.
	fmt.Fprintln(w, "## Operating instructions")
	fmt.Fprintln(w)
	writeContent(w, doc.Operating)

	if len(doc.Skills) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "### Skills")
		fmt.Fprintln(w)
		for _, s := range doc.Skills {
			renderSkillLine(w, s)
		}
	}

	if len(doc.Docs) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "### Docs")
		fmt.Fprintln(w)
		for _, d := range doc.Docs {
			renderDocLine(w, d)
		}
	}

	sectionDivider(w)

	// §4 Active triggers.
	fmt.Fprintln(w, "## Active triggers")
	fmt.Fprintln(w)
	if len(doc.ActiveTriggers) == 0 {
		fmt.Fprintln(w, "_(no active triggers)_")
	} else {
		for _, t := range doc.ActiveTriggers {
			renderTriggerLine(w, t)
		}
	}

	sectionDivider(w)

	// §5 Handoff — LAST (episodic state only; no identity re-statement).
	fmt.Fprintln(w, "## Handoff")
	fmt.Fprintln(w)
	fmt.Fprintln(w, handoffClaimHeader)
	fmt.Fprintln(w)
	switch {
	case doc.Handoff != "":
		writeContent(w, doc.Handoff)
	case doc.HandoffPresent:
		fmt.Fprintln(w, emptyHandoffWarning(doc.AgentName))
	default:
		fmt.Fprintln(w, "_(no handoff on record)_")
	}
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
func RenderToon(doc *BootDoc, w io.Writer) {
	bar := strings.Repeat("═", 60)
	boxHeader := func(title string) {
		fmt.Fprintf(w, "\n╔%s╗\n║ %-58s ║\n╚%s╝\n\n", bar, title, bar)
	}

	// §1 Identity.
	boxHeader("IDENTITY")
	writeContent(w, doc.Soul)
	fmt.Fprintf(w, "\nParent intent: %s\n", doc.ParentIntent)

	// §2 Wake reason.
	boxHeader("WAKE REASON")
	fmt.Fprintln(w, doc.WakeReason)

	// §3 Operating + skills.
	boxHeader("OPERATING INSTRUCTIONS")
	writeContent(w, doc.Operating)
	if len(doc.Skills) > 0 {
		fmt.Fprintln(w, "\nSkills:")
		for _, s := range doc.Skills {
			if s.Presence == "retrieved" || s.ShortDesc == "" {
				fmt.Fprintf(w, "  • %s (pull on demand)", s.Name)
			} else {
				fmt.Fprintf(w, "  • %s: %s", s.Name, s.ShortDesc)
			}
			if s.Pointer != "" {
				fmt.Fprintf(w, " — see %s", s.Pointer)
			}
			fmt.Fprintln(w)
		}
	}
	if len(doc.Docs) > 0 {
		fmt.Fprintln(w, "\nDocs:")
		for _, d := range doc.Docs {
			if d.ShortDesc != "" {
				fmt.Fprintf(w, "  • %s: %s", d.Name, d.ShortDesc)
			} else {
				fmt.Fprintf(w, "  • %s", d.Name)
			}
			if d.Pointer != "" {
				fmt.Fprintf(w, " — see %s", d.Pointer)
			}
			fmt.Fprintln(w)
		}
	}

	// §4 Triggers.
	boxHeader("ACTIVE TRIGGERS")
	if len(doc.ActiveTriggers) == 0 {
		fmt.Fprintln(w, "(no active triggers)")
	} else {
		for _, t := range doc.ActiveTriggers {
			meta := t.Source
			if t.Every != "" {
				meta += ", every " + t.Every
			}
			if t.ActivityGuard != "" {
				meta += ", activity_guard " + t.ActivityGuard
			}
			if t.Message != "" {
				fmt.Fprintf(w, "  • %s [%s]: %s\n", t.ID, meta, t.Message)
			} else {
				fmt.Fprintf(w, "  • %s [%s]\n", t.ID, meta)
			}
		}
	}

	// §5 Handoff — LAST.
	boxHeader("HANDOFF")
	fmt.Fprintln(w, handoffClaimHeader)
	fmt.Fprintln(w)
	switch {
	case doc.Handoff != "":
		writeContent(w, doc.Handoff)
	case doc.HandoffPresent:
		fmt.Fprintln(w, emptyHandoffWarning(doc.AgentName))
	default:
		fmt.Fprintln(w, "(no handoff on record)")
	}
}

// sectionDivider writes the markdown section separator.
func sectionDivider(w io.Writer) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)
}

// writeContent writes content ensuring it ends with a newline.
func writeContent(w io.Writer, content string) {
	fmt.Fprint(w, content)
	if !strings.HasSuffix(content, "\n") {
		fmt.Fprintln(w)
	}
}

// renderSkillLine renders a single skill entry as a markdown list item.
func renderSkillLine(w io.Writer, s SkillEntry) {
	if s.Presence == "retrieved" || s.ShortDesc == "" {
		if s.Pointer != "" {
			fmt.Fprintf(w, "- **%s** _(pull on demand)_ — see `%s`\n", s.Name, s.Pointer)
		} else {
			fmt.Fprintf(w, "- **%s** _(pull on demand)_\n", s.Name)
		}
		return
	}
	if s.Pointer != "" {
		fmt.Fprintf(w, "- **%s:** %s — see `%s`\n", s.Name, s.ShortDesc, s.Pointer)
	} else {
		fmt.Fprintf(w, "- **%s:** %s\n", s.Name, s.ShortDesc)
	}
}

// renderDocLine renders a single doc entry (as: doc) as a markdown list item, always
// showing the explicit resolved path and, when present, the frontmatter description.
func renderDocLine(w io.Writer, d SkillEntry) {
	if d.Pointer == "" {
		fmt.Fprintf(w, "- **%s**\n", d.Name)
		return
	}
	if d.ShortDesc != "" {
		fmt.Fprintf(w, "- **%s:** %s — see `%s`\n", d.Name, d.ShortDesc, d.Pointer)
	} else {
		fmt.Fprintf(w, "- **%s** — see `%s`\n", d.Name, d.Pointer)
	}
}

// renderTriggerLine renders a single trigger as a markdown list item.
func renderTriggerLine(w io.Writer, t Trigger) {
	meta := "source: " + t.Source
	if t.Every != "" {
		meta += ", every: " + t.Every
	}
	if t.ActivityGuard != "" {
		meta += ", activity_guard: " + t.ActivityGuard
	}
	if t.Message != "" {
		fmt.Fprintf(w, "- **%s** (%s): %s\n", t.ID, meta, t.Message)
	} else {
		fmt.Fprintf(w, "- **%s** (%s)\n", t.ID, meta)
	}
}
