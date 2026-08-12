package usage

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// transcriptSlug returns the name that Claude Code gives the transcript
// directory of one project path. Claude Code replaces every "/" and "." of the
// absolute path with "-", so /private/tmp/h/bravo-xt becomes
// -private-tmp-h-bravo-xt.
func transcriptSlug(path string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(path)
}

// projectSlugs returns the transcript-directory names of one project root: the
// name for the path as given, and the name for the path with every symlink
// resolved. Claude Code records the resolved path. On macOS /tmp is a symlink
// to /private/tmp, so a project under /tmp gets a different name from the one
// the operator typed.
func projectSlugs(projectDir string) []string {
	slugs := []string{transcriptSlug(projectDir)}
	resolved, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		return slugs
	}
	if s := transcriptSlug(resolved); s != slugs[0] {
		slugs = append(slugs, s)
	}
	return slugs
}

// worktreeSlugPrefixes returns the transcript-directory prefixes of the
// worktrees that the project tooling makes inside one project. A worktree sits
// under <project>/.harmonik/worktrees or <project>/.claude/worktrees. The "."
// of those names becomes a second "-", so the prefix carries a double dash and
// a different project almost never matches it.
func worktreeSlugPrefixes(roots []string) []string {
	prefixes := make([]string, 0, 2*len(roots))
	for _, root := range roots {
		prefixes = append(prefixes,
			root+transcriptSlug("/.harmonik/worktrees")+"-",
			root+transcriptSlug("/.claude/worktrees")+"-",
		)
	}
	return prefixes
}

// dirClass says whether one transcript directory holds the named project's
// sessions.
type dirClass int

const (
	// dirOther holds no session of the named project.
	dirOther dirClass = iota
	// dirOwn holds sessions of the named project.
	dirOwn
	// dirUncertain may hold sessions of the named project, or of a different
	// project with a similar path. See transcriptScope.Uncertain.
	dirUncertain
)

// classifyTranscriptDir places one transcript-directory name against the slugs
// of one project.
func classifyTranscriptDir(name string, roots, worktreePrefixes []string) dirClass {
	for _, root := range roots {
		if name == root {
			return dirOwn
		}
	}
	for _, prefix := range worktreePrefixes {
		if strings.HasPrefix(name, prefix) {
			return dirOwn
		}
	}
	for _, root := range roots {
		if strings.HasPrefix(name, root+"-") {
			return dirUncertain
		}
	}
	return dirOther
}

// transcriptScope names the transcript directories of one project.
type transcriptScope struct {
	// Own holds the directories whose sessions ran in the named project: the
	// project root and the worktrees inside it.
	Own []string
	// Uncertain holds the directories whose name starts with a project slug but
	// names no known place inside the project. The slug maps "/", "." and "-"
	// all to "-", so the name cannot be read back to one path: such a session
	// can come from a deleted worktree of this project or from a different
	// project whose path starts with the same characters. The report shows the
	// spend of these sessions on its own line. It never adds them to the total.
	Uncertain []string
}

// scopeTranscriptDirs splits the entries of claudeProjectsDir by their relation
// to projectDir. It returns empty lists when claudeProjectsDir does not exist.
func scopeTranscriptDirs(claudeProjectsDir, projectDir string) (transcriptScope, error) {
	var scope transcriptScope
	if projectDir == "" || claudeProjectsDir == "" {
		return scope, nil
	}
	entries, err := os.ReadDir(claudeProjectsDir)
	if os.IsNotExist(err) {
		return scope, nil
	}
	if err != nil {
		return scope, err
	}

	roots := projectSlugs(projectDir)
	worktreePrefixes := worktreeSlugPrefixes(roots)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		full := filepath.Join(claudeProjectsDir, e.Name())
		switch classifyTranscriptDir(e.Name(), roots, worktreePrefixes) {
		case dirOwn:
			scope.Own = append(scope.Own, full)
		case dirUncertain:
			scope.Uncertain = append(scope.Uncertain, full)
		case dirOther:
		}
	}
	sort.Strings(scope.Own)
	sort.Strings(scope.Uncertain)
	return scope, nil
}
