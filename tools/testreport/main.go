// Command testreport turns `go test -json` into a short report.
//
// It reads the JSON stream on stdin and prints the output of FAILING tests
// only, then one summary line per package, then what did NOT run. Output from a
// passing test is discarded. A full `go test -short ./...` run prints about
// 3,400 lines, of which about 126 say whether anything passed. The rest is
// daemon logging the test framework cannot suppress, because the code writes to
// os.Stderr directly instead of through the testing package.
//
// There is no parsing here beyond reading JSON fields. `go test -json` already
// tags every output line with its package and its test, so the report needs no
// pattern matching and cannot be confused by a log line that looks like a test
// result.
//
// The "NOT RUN" section is the reason this tool exists as much as the quiet is.
// A reader who sees only passes assumes the suite ran. This repo has been caught
// by that twice: a `-run` filter matched zero tests and exited green, and a
// package that failed to compile took a whole test tier with it while the run
// still reported success. Anything that did not execute is named on every run.
//
// Usage:
//
//	go test -json -short ./... | testreport
//
// It exits 1 when any package failed, so it can stand in for `go test` in a
// pipeline.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type event struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

type pkg struct {
	name    string
	failed  bool
	elapsed float64
	passed  int
	failing int
	skipped int
	// skippedTests names what did not run. A count alone lets a reader assume
	// the skips were the ones they expected.
	skippedTests []string
	// noTestFiles records a package the toolchain reported as having no tests
	// at all. It is not a failure, but it must never read as a pass.
	noTestFiles bool
	// output holds buffered lines keyed by test name. A test's buffer is
	// dropped when it passes and printed when it fails. The empty key holds
	// package-level output, which is where a build failure appears.
	output map[string][]string
	// failedTests preserves the order tests failed in, so the report reads in
	// the order the run produced.
	failedTests []string
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "testreport:", err)
		os.Exit(2)
	}
}

func run(in *os.File, out *os.File) error {
	packages := map[string]*pkg{}
	var order []string

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 || line[0] != '{' {
			fmt.Fprintln(out, string(line))
			continue
		}
		var ev event
		if err := json.Unmarshal(line, &ev); err != nil {
			fmt.Fprintf(out, "testreport: unreadable record: %s\n", string(line))
			continue
		}
		if ev.Package == "" {
			continue
		}
		p, ok := packages[ev.Package]
		if !ok {
			p = &pkg{name: ev.Package, output: map[string][]string{}}
			packages[ev.Package] = p
			order = append(order, ev.Package)
		}
		apply(p, ev)
	}
	if err := sc.Err(); err != nil {
		return err
	}

	return report(out, packages, order)
}

func apply(p *pkg, ev event) {
	switch ev.Action {
	case "output":
		if ev.Test == "" && strings.Contains(ev.Output, "[no test files]") {
			p.noTestFiles = true
		}
		p.output[ev.Test] = append(p.output[ev.Test], ev.Output)
	case "pass":
		if ev.Test == "" {
			p.elapsed = ev.Elapsed
			return
		}
		p.passed++
		delete(p.output, ev.Test)
	case "skip":
		if ev.Test == "" {
			p.elapsed = ev.Elapsed
			return
		}
		p.skipped++
		p.skippedTests = append(p.skippedTests, ev.Test)
		delete(p.output, ev.Test)
	case "fail":
		if ev.Test == "" {
			p.failed = true
			p.elapsed = ev.Elapsed
			return
		}
		p.failing++
		p.failedTests = append(p.failedTests, ev.Test)
	}
}

func report(out *os.File, packages map[string]*pkg, order []string) error {
	var failed []string
	for _, name := range order {
		p := packages[name]
		if !p.failed {
			continue
		}
		failed = append(failed, name)

		fmt.Fprintf(out, "\n=== FAIL %s\n", name)
		for _, line := range p.output[""] {
			fmt.Fprint(out, line)
		}
		for _, test := range p.failedTests {
			for _, line := range p.output[test] {
				fmt.Fprint(out, line)
			}
		}
	}

	fmt.Fprintln(out)
	names := make([]string, 0, len(packages))
	for name := range packages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p := packages[name]
		status := "ok  "
		if p.failed {
			status = "FAIL"
		}
		fmt.Fprintf(out, "%s %-60s %6.2fs  %d passed, %d failed, %d skipped\n",
			status, p.name, p.elapsed, p.passed, p.failing, p.skipped)
	}

	reportNotRun(out, packages, names)

	if len(failed) == 0 {
		fmt.Fprintf(out, "\nall %d packages passed\n", len(packages))
		return nil
	}
	fmt.Fprintf(out, "\n%d of %d packages FAILED: %v\n", len(failed), len(packages), failed)
	os.Exit(1)
	return nil
}

func reportNotRun(out *os.File, packages map[string]*pkg, names []string) {
	var silent, empty []string
	totalRan, totalSkipped := 0, 0
	for _, name := range names {
		p := packages[name]
		totalRan += p.passed + p.failing
		totalSkipped += p.skipped
		switch {
		case p.noTestFiles:
			empty = append(empty, p.name)
		case p.passed+p.failing+p.skipped == 0:
			silent = append(silent, p.name)
		}
	}

	fmt.Fprintf(out, "\nNOT RUN — read this before believing a green result\n")
	fmt.Fprintf(out, "  %d tests ran, %d skipped, across %d packages\n",
		totalRan, totalSkipped, len(packages))
	if len(empty) > 0 {
		fmt.Fprintf(out, "  %d packages have no test files: %s\n", len(empty), strings.Join(empty, " "))
	}
	if len(silent) > 0 {
		fmt.Fprintf(out, "  WARNING — %d packages ran ZERO tests but are not empty:\n", len(silent))
		for _, name := range silent {
			fmt.Fprintf(out, "    %s\n", name)
		}
	}
	if totalSkipped > 0 {
		fmt.Fprintf(out, "  skipped tests:\n")
		for _, name := range names {
			p := packages[name]
			if len(p.skippedTests) == 0 {
				continue
			}
			sort.Strings(p.skippedTests)
			fmt.Fprintf(out, "    %s: %s\n", p.name, strings.Join(p.skippedTests, " "))
		}
	}
}
