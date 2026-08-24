package agentmanifest

import (
	"os"
	"path/filepath"
	"testing"
)

// A skill whose SKILL.md opens with YAML frontmatter must contribute a real
// description to the brief, not the "---" opening fence. Before this, every
// shipped skill (all of which carry frontmatter) rendered as "name: ---".
func TestReadSkillShortDescSkipsFrontmatterFence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "frontmatter description wins",
			body: "---\nname: agent-comms\ndescription: The inter-agent messaging surface.\n---\n\n# Agent comms\n\nProse.\n",
			want: "The inter-agent messaging surface.",
		},
		{
			name: "folded description collapses to its first line",
			body: "---\nname: x\ndescription: >\n  First line here.\n  Second line here.\n---\n\nProse.\n",
			want: "First line here. Second line here.",
		},
		{
			name: "no frontmatter falls back to first prose line",
			body: "# Boot\n\nRun `harmonik agent brief` first.\n",
			want: "Run `harmonik agent brief` first.",
		},
		{
			name: "frontmatter without description skips to prose, past the banner comment",
			body: "---\nname: x\n---\n\n<!-- SOURCE OF TRUTH: somewhere\n     more banner -->\n\n# Title\n\nReal prose line.\n",
			want: "Real prose line.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "SKILL.md")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}
			got := readSkillShortDesc(path)
			if got != tc.want {
				t.Errorf("readSkillShortDesc()\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}
