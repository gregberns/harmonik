package agentmanifest

import (
	"strings"
	"testing"
)

// skillBlockLines renders a boot document whose ONLY populated list is Skills,
// then returns the rendered skill lines. Docs and triggers are left empty on
// purpose: no other section of either renderer emits a line with these
// prefixes, so what comes back is exactly the skills block.
func skillBlockLines(t *testing.T, doc *BootDoc, render func(*BootDoc, *strings.Builder) error, prefix string) []string {
	t.Helper()
	var buf strings.Builder
	if err := render(doc, &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	var got []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.HasPrefix(line, prefix) {
			got = append(got, line)
		}
	}
	return got
}

func renderMarkdownTo(doc *BootDoc, buf *strings.Builder) error { return RenderMarkdown(doc, buf) }
func renderToonTo(doc *BootDoc, buf *strings.Builder) error     { return RenderToon(doc, buf) }

// The skills block is the part of a boot document the agent acts on. Each line
// carries a name, an optional one-line description, a "pull on demand" mark for
// a retrieved skill, and an optional pointer to the file. Each combination is a
// separate branch in the renderers, and a line that loses the pointer or the
// pull-on-demand mark is an agent that cannot reach its own skill.
func TestRenderSkillLineShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		skill        SkillEntry
		wantMarkdown string
		wantToon     string
	}{
		{
			name: "description and pointer",
			skill: SkillEntry{
				Name:      "agent-comms",
				ShortDesc: "The inter-agent messaging surface.",
				Pointer:   ".claude/skills/agent-comms/SKILL.md",
				Presence:  "injected",
			},
			wantMarkdown: "- **agent-comms:** The inter-agent messaging surface. — see `.claude/skills/agent-comms/SKILL.md`",
			wantToon:     "  • agent-comms: The inter-agent messaging surface. — see .claude/skills/agent-comms/SKILL.md",
		},
		{
			name: "description and pointer, retrieved",
			skill: SkillEntry{
				Name:      "beads-cli",
				ShortDesc: "The task ledger.",
				Pointer:   ".claude/skills/beads-cli/SKILL.md",
				Presence:  "retrieved",
			},
			wantMarkdown: "- **beads-cli:** The task ledger. _(pull on demand)_ — see `.claude/skills/beads-cli/SKILL.md`",
			wantToon:     "  • beads-cli: The task ledger. (pull on demand) — see .claude/skills/beads-cli/SKILL.md",
		},
		{
			name: "description only, retrieved",
			skill: SkillEntry{
				Name:      "keeper",
				ShortDesc: "Watches how full the session is.",
				Presence:  "retrieved",
			},
			wantMarkdown: "- **keeper:** Watches how full the session is. _(pull on demand)_",
			wantToon:     "  • keeper: Watches how full the session is. (pull on demand)",
		},
		{
			name: "pointer only, retrieved",
			skill: SkillEntry{
				Name:     "watch",
				Pointer:  ".claude/skills/watch/SKILL.md",
				Presence: "retrieved",
			},
			wantMarkdown: "- **watch** _(pull on demand)_ — see `.claude/skills/watch/SKILL.md`",
			wantToon:     "  • watch (pull on demand) — see .claude/skills/watch/SKILL.md",
		},
		{
			name:         "name only",
			skill:        SkillEntry{Name: "harmonik-dispatch", Presence: "injected"},
			wantMarkdown: "- **harmonik-dispatch**",
			wantToon:     "  • harmonik-dispatch",
		},
		{
			name:         "name only, retrieved",
			skill:        SkillEntry{Name: "harmonik-lifecycle", Presence: "retrieved"},
			wantMarkdown: "- **harmonik-lifecycle** _(pull on demand)_",
			wantToon:     "  • harmonik-lifecycle (pull on demand)",
		},
		{
			name:         "embodied presence carries no pull-on-demand mark",
			skill:        SkillEntry{Name: "captain", ShortDesc: "Runs the fleet.", Presence: "embodied"},
			wantMarkdown: "- **captain:** Runs the fleet.",
			wantToon:     "  • captain: Runs the fleet.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := &BootDoc{Skills: []SkillEntry{tc.skill}}

			gotMD := skillBlockLines(t, doc, renderMarkdownTo, "- **")
			if len(gotMD) != 1 || gotMD[0] != tc.wantMarkdown {
				t.Errorf("markdown skill lines\n got: %q\nwant: [%q]", gotMD, tc.wantMarkdown)
			}

			gotToon := skillBlockLines(t, doc, renderToonTo, "  • ")
			if len(gotToon) != 1 || gotToon[0] != tc.wantToon {
				t.Errorf("toon skill lines\n got: %q\nwant: [%q]", gotToon, tc.wantToon)
			}
		})
	}
}

// Several skills render one line each, in the order the boot document lists
// them, under a heading that says what the block is.
func TestRenderSkillsBlockKeepsOrderUnderHeading(t *testing.T) {
	t.Parallel()

	doc := &BootDoc{Skills: []SkillEntry{
		{Name: "first", ShortDesc: "One.", Presence: "injected"},
		{Name: "second", Pointer: "b.md", Presence: "retrieved"},
		{Name: "third", Presence: "injected"},
	}}

	wantMD := []string{
		"- **first:** One.",
		"- **second** _(pull on demand)_ — see `b.md`",
		"- **third**",
	}
	gotMD := skillBlockLines(t, doc, renderMarkdownTo, "- **")
	if strings.Join(gotMD, "\n") != strings.Join(wantMD, "\n") {
		t.Errorf("markdown skill lines\n got: %q\nwant: %q", gotMD, wantMD)
	}

	wantToon := []string{
		"  • first: One.",
		"  • second (pull on demand) — see b.md",
		"  • third",
	}
	gotToon := skillBlockLines(t, doc, renderToonTo, "  • ")
	if strings.Join(gotToon, "\n") != strings.Join(wantToon, "\n") {
		t.Errorf("toon skill lines\n got: %q\nwant: %q", gotToon, wantToon)
	}

	var md strings.Builder
	if err := RenderMarkdown(doc, &md); err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if !strings.Contains(md.String(), "### Skills") {
		t.Errorf("markdown output has skill lines but no \"### Skills\" heading:\n%s", md.String())
	}

	var toon strings.Builder
	if err := RenderToon(doc, &toon); err != nil {
		t.Fatalf("RenderToon: %v", err)
	}
	if !strings.Contains(toon.String(), "\nSkills:\n") {
		t.Errorf("toon output has skill lines but no \"Skills:\" heading:\n%s", toon.String())
	}
}

// An agent with no skills gets no skills block at all — not an empty heading
// with nothing under it.
func TestRenderNoSkillsEmitsNoSkillsBlock(t *testing.T) {
	t.Parallel()

	for name, doc := range map[string]*BootDoc{
		"nil slice":   {Skills: nil},
		"empty slice": {Skills: []SkillEntry{}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var md strings.Builder
			if err := RenderMarkdown(doc, &md); err != nil {
				t.Fatalf("RenderMarkdown: %v", err)
			}
			if strings.Contains(md.String(), "### Skills") {
				t.Errorf("markdown emitted a skills heading for a doc with no skills:\n%s", md.String())
			}

			var toon strings.Builder
			if err := RenderToon(doc, &toon); err != nil {
				t.Fatalf("RenderToon: %v", err)
			}
			if strings.Contains(toon.String(), "Skills:") {
				t.Errorf("toon emitted a skills heading for a doc with no skills:\n%s", toon.String())
			}
		})
	}
}
