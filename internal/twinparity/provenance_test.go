// provenance_test.go — whether a twin-parity corpus is a real capture, and
// whether anything notices when it is not.
//
// # The gap this closes
//
// Every corpus under testdata/twin-parity/ carries a meta.yaml that declares
// `hand_authored: true` for a placeholder and `hand_authored: false` for a real
// capture. The real-capture writer sets it (internal/daemon
// e2e_real_claude_capture_test.go captureMetaYAML). Before this file, NOTHING
// read it back. A hand-authored sample and a live capture produced the same
// green parity gate, so the gate's output carried no information about which one
// it had just compared.
//
// That matters more than it sounds. The plan for proving the core loop's failure
// routes is to drive them with twins, on the grounds that a twin can emit BLOCK
// on demand where a real agent cannot. That plan rests entirely on the twins
// being faithful, and twin fidelity is what a parity gate is for. A parity gate
// that is green against a file somebody typed proves the equivalence ENGINE
// works. It proves nothing about any twin.
//
// # What this file asserts, and why it does not simply fail
//
// The corpora are hand-authored today and cannot be replaced from here: a real
// capture needs an authenticated, tmux-capable box (make capture-claude-fixtures
// for Claude, make test-pi-live with PI_LIVE=1 for pi). Failing the build until
// then would only mean everyone runs with the gate disabled.
//
// So the placeholders are named in knownPlaceholderCorpora below, and the test
// holds the list to the truth in both directions:
//
//   - A corpus that declares itself hand-authored and is NOT on the list fails.
//     A new placeholder — a codex corpus, say — cannot arrive quietly and be
//     read as proof.
//   - A corpus on the list that has become a REAL capture also fails, with the
//     instruction to take it off. A real capture that stays labelled a
//     placeholder wastes the strength somebody paid a live box to produce.
//
// The list is therefore the project's standing statement of what twin fidelity
// is currently worth: while a name is on it, no result driven by that twin is
// evidence about the real harness, only about the twin.
package twinparity

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var knownPlaceholderCorpora = map[string]string{
	"claude/happy-path-sample": "replace by running `make capture-claude-fixtures` on an authenticated, tmux-capable box",
	"pi/happy-path-sample":     "replace by running `make test-pi-live` with PI_LIVE=1 on a box with pi provider auth",
}

type corpusMeta struct {
	agent        string
	handAuthored bool
	// handAuthoredDeclared separates "declared false" from "never said". An
	// absent key must not read as a real capture.
	handAuthoredDeclared bool
	captureDate          string
}

func parseCorpusMeta(path string) (corpusMeta, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: path is built from a walk of the committed testdata tree
	if err != nil {
		return corpusMeta{}, err
	}
	var m corpusMeta
	for _, line := range strings.Split(string(raw), "\n") {
		if line != strings.TrimLeft(line, " \t") {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found || strings.HasPrefix(strings.TrimSpace(key), "#") {
			continue
		}
		if hash := strings.Index(value, "#"); hash >= 0 {
			value = value[:hash]
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "agent":
			m.agent = value
		case "capture_date":
			m.captureDate = value
		case "hand_authored":
			parsed, parseErr := strconv.ParseBool(value)
			if parseErr != nil {
				return corpusMeta{}, parseErr
			}
			m.handAuthored = parsed
			m.handAuthoredDeclared = true
		}
	}
	return m, nil
}

func discoverCorpora(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "testdata", "twin-parity")
	var found []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || d.Name() != "meta.yaml" {
			return nil
		}
		rel, relErr := filepath.Rel(root, filepath.Dir(path))
		if relErr != nil {
			return relErr
		}
		found = append(found, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(found)
	return found
}

// TestTwinParityCorpusProvenanceIsDeclared is the positive half: every
// corpus states where it came from, and the statement is readable. A corpus with
// no meta.yaml is not discovered at all, so the count is asserted too — an empty
// walk would satisfy every per-corpus check for free.
func TestTwinParityCorpusProvenanceIsDeclared(t *testing.T) {
	corpora := discoverCorpora(t)
	if len(corpora) == 0 {
		t.Fatal("no twin-parity corpora found under testdata/twin-parity; this test proved nothing")
	}

	for _, name := range corpora {
		t.Run(name, func(t *testing.T) {
			meta, err := parseCorpusMeta(filepath.Join("..", "..", "testdata", "twin-parity", name, "meta.yaml"))
			if err != nil {
				t.Fatalf("parse meta.yaml: %v", err)
			}
			if !meta.handAuthoredDeclared {
				t.Error("meta.yaml does not declare hand_authored, so nothing can tell a real capture from a sample; " +
					"add `hand_authored: true` for a sample or `false` for a live capture")
			}
			if meta.agent == "" {
				t.Error("meta.yaml does not declare agent")
			}
		})
	}
}

// TestTwinParityCorpusPlaceholdersAreTheOnesWeSaidTheyWere is the half that bites. It
// holds knownPlaceholderCorpora to the corpora actually on disk, both ways.
//
// Mutation that must turn this red: flip hand_authored in either committed
// meta.yaml, or add or remove a line in knownPlaceholderCorpora.
func TestTwinParityCorpusPlaceholdersAreTheOnesWeSaidTheyWere(t *testing.T) {
	corpora := discoverCorpora(t)
	if len(corpora) == 0 {
		t.Fatal("no twin-parity corpora found under testdata/twin-parity; this test proved nothing")
	}

	onDisk := map[string]bool{} // corpus → is a placeholder
	for _, name := range corpora {
		meta, err := parseCorpusMeta(filepath.Join("..", "..", "testdata", "twin-parity", name, "meta.yaml"))
		if err != nil {
			t.Fatalf("%s: parse meta.yaml: %v", name, err)
		}
		onDisk[name] = meta.handAuthored || !meta.handAuthoredDeclared

		datePlaceheld := meta.captureDate == "" || strings.Contains(meta.captureDate, "PLACEHOLDER")
		if onDisk[name] && !datePlaceheld {
			t.Errorf("%s declares hand_authored but stamps capture_date %q; one of the two is wrong",
				name, meta.captureDate)
		}
		if !onDisk[name] && datePlaceheld {
			t.Errorf("%s declares itself a real capture but its capture_date is still %q; "+
				"a capture nobody can date cannot be audited against the commit it was taken at",
				name, meta.captureDate)
		}
	}

	for name, isPlaceholder := range onDisk {
		_, listed := knownPlaceholderCorpora[name]
		switch {
		case isPlaceholder && !listed:
			t.Errorf("twin-parity corpus %s is hand-authored but is not named in knownPlaceholderCorpora. "+
				"A parity gate green against a hand-authored corpus proves the equivalence engine works and proves "+
				"NOTHING about the twin, so every result driven by that twin is unproven. Add it to the list with "+
				"what it would take to capture it for real, or capture it.", name)
		case !isPlaceholder && listed:
			t.Errorf("twin-parity corpus %s is now a REAL capture but is still named in knownPlaceholderCorpora. "+
				"Remove it: the gate is stronger than the list says, and leaving it listed wastes the capture.", name)
		}
	}

	for name, howToReplace := range knownPlaceholderCorpora {
		if _, ok := onDisk[name]; !ok {
			t.Errorf("knownPlaceholderCorpora names %s, which is not on disk. Remove the stale entry. (%s)",
				name, howToReplace)
		}
	}
}

// TestTwinParityCorpusAnnouncesWhenItProvesNothing makes the state visible where
// somebody reading a gate run will see it, rather than only in a comment inside
// a YAML file.
//
// It cannot fail on the placeholders themselves — that is what the list above is
// for — but it says the sentence out loud on every parity run, and it goes quiet
// by itself on the day the last real capture lands.
//
// t.Log is only shown on a verbose run, so the two Makefile gate targets that
// reach this test pass -v. That is deliberate and load-bearing: a warning nobody
// sees is the same as the comment in meta.yaml that nobody read, which is the
// thing this file exists to fix. If you drop -v from those targets, this test
// goes silent and still passes.
func TestTwinParityCorpusAnnouncesWhenItProvesNothing(t *testing.T) {
	corpora := discoverCorpora(t)
	var placeholders []string
	for _, name := range corpora {
		if _, listed := knownPlaceholderCorpora[name]; listed {
			placeholders = append(placeholders, name)
		}
	}
	if len(placeholders) == 0 {
		t.Log("every twin-parity corpus is a real capture; twin fidelity is proven for all of them")
		return
	}
	sort.Strings(placeholders)

	var b strings.Builder
	fmt.Fprintf(&b, "TWIN FIDELITY UNPROVEN — %d of %d twin-parity corpora are hand-authored, not captured:\n",
		len(placeholders), len(corpora))
	for _, name := range placeholders {
		fmt.Fprintf(&b, "    %s — %s\n", name, knownPlaceholderCorpora[name])
	}
	b.WriteString("  A green parity gate against these proves the equivalence engine works. It does NOT prove\n")
	b.WriteString("  any twin matches its agent, so a twin-driven result is not evidence about the real harness.")
	t.Log(b.String())
}
