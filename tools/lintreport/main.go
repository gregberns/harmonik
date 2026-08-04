// Command lintreport turns golangci-lint JSON into a short report, judged
// against an explicit list of findings the tree is allowed to still have.
//
// The tree carries a large backlog. Gating on zero findings would block every
// change for months, and gating on a COUNT hides what is being tolerated: a
// count goes down when someone silences a finding just as readily as when
// someone fixes one. So the allow list names each tolerated pair explicitly,
// one line per file and linter:
//
//	internal/daemon/workloop.go	errcheck
//
// A finding whose pair is on the list is tolerated and reported as a remainder.
// A finding whose pair is NOT on the list fails the build. Clean a file, delete
// its line, and the file can never regress. That is the whole mechanism.
//
// The unit is file-and-linter on purpose. Line numbers rot within days, so an
// allow list keyed by line would churn on every unrelated edit. A count per file
// would drift for the same reason. File-and-linter is stable under editing and
// still small enough to remove one piece at a time.
//
// Every run prints what is being ignored, including a clean run, so that
// "no new lint findings" can never be misread as "this tree is clean".
//
// Usage:
//
//	golangci-lint run --output.json.path=lint.json ...
//	lintreport -allow tools/lintreport/allow.txt lint.json
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// report is the subset of golangci-lint's JSON output this tool reads.
type report struct {
	Issues []struct {
		FromLinter string `json:"FromLinter"`
		Text       string `json:"Text"`
		Pos        struct {
			Filename string `json:"Filename"`
			Line     int    `json:"Line"`
		} `json:"Pos"`
	} `json:"Issues"`
}

// key identifies one tolerated pair. It is what the allow list holds.
type key struct {
	file   string
	linter string
}

func main() {
	allowPath := flag.String("allow", "", "path to the allow list")
	write := flag.Bool("write", false, "rewrite the allow list from the current findings (use once, to seed it)")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: lintreport -allow <file> [-write] <golangci-lint.json>")
		os.Exit(2)
	}
	if *allowPath == "" {
		fmt.Fprintln(os.Stderr, "lintreport: -allow is required")
		os.Exit(2)
	}

	findings, err := readFindings(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "lintreport:", err)
		os.Exit(2)
	}

	if *write {
		if err := writeAllow(*allowPath, findings); err != nil {
			fmt.Fprintln(os.Stderr, "lintreport:", err)
			os.Exit(2)
		}
		fmt.Printf("wrote %d allowed pairs to %s\n", len(findings), *allowPath)
		return
	}

	allow, err := readAllow(*allowPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lintreport:", err)
		os.Exit(2)
	}
	os.Exit(judge(os.Stdout, findings, allow))
}

// readFindings groups every issue by its file-and-linter pair.
func readFindings(path string) (map[key][]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r report
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	out := map[key][]string{}
	for _, iss := range r.Issues {
		k := key{file: filepath.ToSlash(iss.Pos.Filename), linter: iss.FromLinter}
		out[k] = append(out[k], fmt.Sprintf("%s:%d: %s", k.file, iss.Pos.Line, iss.Text))
	}
	return out, nil
}

// readAllow loads the allow list. Blank lines and lines starting with # are
// comments, so the list can carry a note about why a pair is still there.
func readAllow(path string) (map[key]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	allow := map[key]bool{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			return nil, fmt.Errorf("%s line %d: want '<file> <linter>', got %q", path, n, line)
		}
		allow[key{file: parts[0], linter: parts[1]}] = true
	}
	return allow, sc.Err()
}

func writeAllow(path string, findings map[key][]string) error {
	keys := sortedKeys(findings)
	var b strings.Builder
	b.WriteString("# Lint findings this tree is allowed to still have.\n")
	b.WriteString("# One line per file and linter. Clean a file, then delete its line.\n")
	b.WriteString("# A finding NOT on this list fails the build.\n")
	b.WriteString("# Seeded from the tree as it stood when the gate was introduced.\n\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s\t%s\n", k.file, k.linter)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// judge prints every finding that is not allowed, then what the list tolerates.
// It returns the process exit code.
func judge(out *os.File, findings map[key][]string, allow map[key]bool) int {
	var newPairs []key
	tolerated := 0
	byLinter := map[string]int{}
	byPackage := map[string]int{}

	for _, k := range sortedKeys(findings) {
		if allow[k] {
			tolerated += len(findings[k])
			byLinter[k.linter] += len(findings[k])
			byPackage[filepath.Dir(k.file)] += len(findings[k])
			continue
		}
		newPairs = append(newPairs, k)
	}

	for _, k := range newPairs {
		fmt.Fprintf(out, "\n=== NOT ALLOWED  %s  [%s]\n", k.file, k.linter)
		for _, msg := range findings[k] {
			fmt.Fprintf(out, "  %s\n", msg)
		}
	}

	// A pair on the list with no findings left means someone cleaned a file and
	// did not delete its line. That is not a failure, but leaving it makes the
	// file silently re-regressable, so it is named.
	var stale []key
	for k := range allow {
		if _, still := findings[k]; !still {
			stale = append(stale, k)
		}
	}
	if len(stale) > 0 {
		sort.Slice(stale, func(i, j int) bool { return stale[i].file < stale[j].file })
		fmt.Fprintf(out, "\n%d allow-list entries are now clean — delete these lines:\n", len(stale))
		for _, k := range stale {
			fmt.Fprintf(out, "  %s\t%s\n", k.file, k.linter)
		}
	}

	// WHAT IS BEING IGNORED. Printed on every run, including a clean one.
	fmt.Fprintf(out, "\nIGNORED — findings the allow list tolerates today\n")
	fmt.Fprintf(out, "  %d findings across %d file/linter pairs\n", tolerated, len(allow)-len(stale))
	fmt.Fprintf(out, "  by package:\n")
	for _, p := range sortedCounts(byPackage) {
		fmt.Fprintf(out, "    %5d  %s\n", byPackage[p], p)
	}
	fmt.Fprintf(out, "  by linter:\n")
	for _, l := range sortedCounts(byLinter) {
		fmt.Fprintf(out, "    %5d  %s\n", byLinter[l], l)
	}
	fmt.Fprintf(out, "  NOTE: .golangci.yml also excludes whole paths and rules before a finding\n")
	fmt.Fprintf(out, "        ever reaches this tool. This list cannot show those. Read the\n")
	fmt.Fprintf(out, "        exclusions section there to see the second layer.\n")

	if len(newPairs) == 0 {
		fmt.Fprintln(out, "\nno new lint findings")
		return 0
	}
	fmt.Fprintf(out, "\nFAIL: %d file/linter pairs are not on the allow list\n", len(newPairs))
	return 1
}

func sortedKeys(m map[key][]string) []key {
	out := make([]key, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].file != out[j].file {
			return out[i].file < out[j].file
		}
		return out[i].linter < out[j].linter
	})
	return out
}

func sortedCounts(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if m[out[i]] != m[out[j]] {
			return m[out[i]] > m[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}
