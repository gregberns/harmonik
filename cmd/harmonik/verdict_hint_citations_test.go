package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var verdictHintCitationRe = regexp.MustCompile(`harmonik\s+([a-z][a-z0-9-]*)`)

var mainDispatchVerbRe = regexp.MustCompile(`os\.Args\[1\] == "([a-z][a-z0-9-]*)"`)

func realSubcommands(t *testing.T) map[string]bool {
	t.Helper()

	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	verbs := map[string]bool{}
	for _, m := range mainDispatchVerbRe.FindAllStringSubmatch(string(src), -1) {
		verbs[m[1]] = true
	}

	if len(verbs) < 20 {
		t.Fatalf("dispatch scan of main.go found only %d subcommands (%v); the "+
			"chain shape changed and mainDispatchVerbRe needs updating — it must "+
			"not be allowed to pass vacuously", len(verbs), sortedKeys(verbs))
	}

	return verbs
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestVerdictCommandsCiteOnlyRealSubcommands fails when confirm-verdict or
// veto-verdict tells the operator to run a verb that does not exist.
func TestVerdictCommandsCiteOnlyRealSubcommands(t *testing.T) {
	verbs := realSubcommands(t)

	for _, must := range []string{"confirm-verdict", "veto-verdict"} {
		if !verbs[must] {
			t.Fatalf("dispatch scan of main.go did not find %q; the scan is not "+
				"reading the real subcommand chain", must)
		}
	}
	if verbs["status"] {
		t.Fatalf("main.go now dispatches a %q subcommand; this test's premise is "+
			"stale and the messages may cite it again", "status")
	}

	for _, file := range []string{"confirm_verdict.go", "veto_verdict.go"} {
		// #nosec G304 -- both paths are string literals in the slice above, not input.
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}

		seen := map[string]bool{}
		for _, m := range verdictHintCitationRe.FindAllStringSubmatch(string(src), -1) {
			verb := m[1]
			if seen[verb] {
				continue
			}
			seen[verb] = true

			if !verbs[verb] {
				t.Errorf("%s tells the operator to run 'harmonik %s', which is not a "+
					"subcommand. Following it with a --project flag already on the "+
					"line starts a daemon in a directory that was never initialised. "+
					"Cite a verb from the dispatch chain in main.go, or say plainly "+
					"that no command does the job. Real verbs: %s",
					file, verb, strings.Join(sortedKeys(verbs), " "))
			}
		}
	}
}
