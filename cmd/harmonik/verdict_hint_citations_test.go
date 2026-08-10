package main

// verdict_hint_citations_test.go — every `harmonik <verb>` the verdict-override
// commands print must name a verb that exists.
//
// Bead ref: hk-phantom-status-command-nlbtz.
//
// confirm-verdict and veto-verdict refused an unknown run correctly and then
// told the operator to run `harmonik status`. There is no `harmonik status`
// subcommand. The instruction is not merely untidy: any argv that begins with a
// flag bypasses the unknown-subcommand guard, so the flag-first spelling of a
// phantom verb — `harmonik --project DIR status`, which is the shape an
// operator already has on the line — starts a daemon and writes a full
// .harmonik/ tree into a directory that was never initialised
// (hk-cli-flag-first-starts-daemon-gjhiy). A stale reference to a verb that
// does not exist is therefore a live hazard.
//
// The check scans the SOURCE of the two command files, not the rendered help
// output, for two reasons. Some of these citations live in a runtime error that
// only prints after a socket round-trip against a live daemon, which a unit
// test cannot reach. And a source scan covers every message in the file,
// including one added tomorrow, so this test does not need updating when the
// text changes — it only fails when a NEW phantom verb appears.
//
// The verb vocabulary is derived from the dispatch chain in main.go rather than
// listed here. main.go is the single source of truth for which verbs exist
// (usage.go says so in unknownSubcommand's doc comment, and deliberately keeps
// no second list). A hand-written list in this file would drift out of step
// with the chain and start passing phantoms again.

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// verdictHintCitationRe matches a `harmonik <verb>` citation. The verb must
// start with a lowercase letter, so `harmonik --project DIR` (the documented
// way to start the daemon) and the `harmonik %s:` diagnostic prefixes are not
// mistaken for verb citations.
//
// The separator is \s+, not a literal space. The help text is hard-wrapped near
// column 72, so a citation that lands on a line break is the realistic way a
// phantom escapes this test. A literal space would miss it.
var verdictHintCitationRe = regexp.MustCompile(`harmonik\s+([a-z][a-z0-9-]*)`)

// mainDispatchVerbRe matches one arm of the subcommand chain in main.go, which
// is a flat sequence of `os.Args[1] == "<verb>"` guards.
var mainDispatchVerbRe = regexp.MustCompile(`os\.Args\[1\] == "([a-z][a-z0-9-]*)"`)

// realSubcommands reads the dispatch chain in main.go and returns every verb it
// handles.
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

	// Guard the regex itself. If the dispatch chain is ever rewritten into a
	// switch or a table, this pattern stops matching and every citation would
	// silently "pass" against an empty vocabulary. harmonik has well over
	// twenty subcommands, so a small result means the scan broke, not that the
	// CLI shrank.
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

	// Sanity-check the vocabulary against verbs these two files are certain to
	// cite. If these are absent the scan is reading the wrong thing.
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
