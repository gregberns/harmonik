package main

// init_scaffold_links_hk5qey_test.go — regression pin for the scaffold
// dangling-reference defect (hk-5qey). The original bug: the embedded
// AGENT_INDEX.md scaffold linked [TASKS.md](TASKS.md), a file provisionScaffolds
// never writes, so every freshly inited project shipped a dead link.
//
// The pin is a general invariant rather than a TASKS.md special case: every
// repo-relative markdown link AND every backticked *.md / *.yaml / *.yml path
// mentioned by any file under assets/scaffolds/ MUST exist in a project after
// provisioning. Provisioning here mirrors ensureBootAssets (captain.go) — the
// scaffold-writing path that does NOT call ensureClaudeMDSymlink — so a
// scaffold that names CLAUDE.md, TASKS.md, or any other non-provisioned file
// fails, on the weaker of the two write paths.
//
// No PATH dependency: it calls the provisioning functions directly rather than
// runInit, so it never skips.
//
// Bead ref: hk-5qey.

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// mdLinkRe matches inline markdown links: [text](target).
var mdLinkRe = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)

// backtickPathRe matches a backticked token that looks like a repo-relative
// path to a markdown or YAML file, e.g. `HANDOFF.md` or
// `.harmonik/context/project.yaml`.
var backtickPathRe = regexp.MustCompile("`([A-Za-z0-9_./-]+\\.(?:md|ya?ml))`")

func TestScaffoldsHaveNoDanglingReferences_HK5qey(t *testing.T) {
	repo := t.TempDir()

	// Mirror ensureBootAssets minus provisionSkills (skills write no scaffold
	// targets): scaffolds, context tiers, AGENTS.md router. Deliberately NOT
	// ensureClaudeMDSymlink — ensureBootAssets does not call it, so CLAUDE.md
	// must not be a legal scaffold reference.
	if code := provisionScaffolds(repo, false, io.Discard, io.Discard); code != 0 {
		t.Fatalf("provisionScaffolds exit %d (want 0)", code)
	}
	if code := provisionContextTiers(repo, false, io.Discard, io.Discard); code != 0 {
		t.Fatalf("provisionContextTiers exit %d (want 0)", code)
	}
	if code := renderAgentsMD(repo, "main", false, io.Discard, io.Discard); code != 0 {
		t.Fatalf("renderAgentsMD exit %d (want 0)", code)
	}

	entries, err := fs.ReadDir(initSkillAssets, "assets/scaffolds")
	if err != nil {
		t.Fatalf("read embedded assets/scaffolds: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("hk-5qey: no embedded scaffolds found — the pin would be vacuous")
	}

	checked := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		body, rerr := initSkillAssets.ReadFile("assets/scaffolds/" + name)
		if rerr != nil {
			t.Fatalf("read embedded scaffold %s: %v", name, rerr)
		}
		text := string(body)

		refs := map[string]bool{}
		for _, m := range mdLinkRe.FindAllStringSubmatch(text, -1) {
			if target := normalizeScaffoldRef(m[1]); target != "" {
				refs[target] = true
			}
		}
		for _, m := range backtickPathRe.FindAllStringSubmatch(text, -1) {
			if target := normalizeScaffoldRef(m[1]); target != "" {
				refs[target] = true
			}
		}

		for target := range refs {
			checked++
			if _, serr := os.Lstat(filepath.Join(repo, filepath.FromSlash(target))); serr != nil {
				t.Errorf("hk-5qey: scaffold %s references %q, which provisioning never writes: %v",
					name, target, serr)
			}
		}
	}

	// Guard against a silently vacuous pin: AGENT_INDEX.md is expected to link
	// at least STATUS.md and AGENTS.md.
	if checked < 2 {
		t.Errorf("hk-5qey: only %d scaffold reference(s) checked — expected the "+
			"AGENT_INDEX.md links; the regression pin is not exercising anything", checked)
	}
}

// normalizeScaffoldRef reduces a markdown link target or backticked path to a
// repo-relative path to check, or "" when it is not a repo-relative file
// reference (absolute URL, anchor-only, mail link, absolute path).
func normalizeScaffoldRef(raw string) string {
	ref := strings.TrimSpace(raw)
	if ref == "" || strings.HasPrefix(ref, "#") || strings.HasPrefix(ref, "/") {
		return ""
	}
	if strings.Contains(ref, "://") || strings.HasPrefix(ref, "mailto:") {
		return ""
	}
	if i := strings.IndexAny(ref, "#?"); i >= 0 {
		ref = ref[:i]
	}
	return strings.TrimSpace(ref)
}
