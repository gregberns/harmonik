package crewrun

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type missionFrontMatter struct {
	Model   string `yaml:"model"`
	Harness string `yaml:"harness"`
}

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
