package crewrun

// missionfrontmatter.go — the mission-handoff YAML front-matter readers that
// feed tier 2 of the crew-scoped harness resolver and the per-crew model pin.
//
// Moved verbatim out of internal/daemon/crewstart.go by P2 unit E2 (slice E2a).
//
// Spec ref: specs/crew-handoff-schema.md §3.
// Bead ref: hk-9j3z, hk-l63b9.

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// missionFrontMatter is the subset of the mission-handoff YAML front-matter the
// daemon reads at launch time. model: and harness: are the only fields modelled
// here; all other fields are the crew's concern (it re-derives them on
// /session-resume). yaml.v3 silently ignores the unmodelled keys (schema_version,
// crew_name, queue, …).
//
// Spec ref: specs/crew-handoff-schema.md §3 (model: optional, opus|sonnet|haiku).
// harness: is the crew-scoped harness resolver's mid-precedence tier (hk-l63b9).
type missionFrontMatter struct {
	Model   string `yaml:"model"`
	Harness string `yaml:"harness"`
}

// readMissionFrontMatter reads and parses a mission handoff's YAML front-matter
// block (the leading `---`-delimited block per crew-handoff-schema.md §3).
//
// Best-effort by design: an empty path, a missing/unreadable file, or a mission
// without a front-matter block all return the zero missionFrontMatter. A
// malformed front-matter block likewise degrades to the zero value rather than
// failing the crew-start op — front-matter fields are optimisations, not a
// correctness contract.
func readMissionFrontMatter(missionPath string) missionFrontMatter {
	if missionPath == "" {
		return missionFrontMatter{}
	}
	//nolint:gosec // G304: missionPath is an operator/captain-supplied handoff path
	data, err := os.ReadFile(missionPath)
	if err != nil {
		return missionFrontMatter{}
	}

	block := frontMatterBlock(string(data))
	if block == "" {
		return missionFrontMatter{}
	}

	var fm missionFrontMatter
	if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
		return missionFrontMatter{}
	}
	return fm
}

// ReadMissionModel reads the optional model: field from a mission handoff's YAML
// front-matter. The caller passes the result to BuildCrewLaunchSpec, which then
// injects no --model flag on "" and the crew inherits the compiled default model.
func ReadMissionModel(missionPath string) string {
	return readMissionFrontMatter(missionPath).Model
}

// ReadMissionHarness reads the optional harness: field from a mission handoff's
// YAML front-matter — the mid-precedence tier of the crew-scoped harness
// resolver (hk-l63b9): flag > mission harness: front-matter > per-crew config >
// default "claude".
func ReadMissionHarness(missionPath string) string {
	return readMissionFrontMatter(missionPath).Harness
}

// frontMatterBlock extracts the YAML body between the leading `---` fence and the
// closing `---` fence of a Markdown handoff. Returns "" when no front-matter
// block is present (the file does not open with a `---` line).
func frontMatterBlock(content string) string {
	const fence = "---"
	rest, ok := strings.CutPrefix(content, fence+"\n")
	if !ok {
		return ""
	}
	end := strings.Index(rest, "\n"+fence)
	if end < 0 {
		return ""
	}
	return rest[:end]
}
