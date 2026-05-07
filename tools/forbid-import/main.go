// Command forbid-import enforces the harmonik third-party import allowlist by
// walking the module's transitive dependency graph and checking every import
// path against a hard-coded allowlist and a deny list.
//
// # Allowlist rules (MVH — hardcoded)
//
//   - Standard library: paths whose first path segment contains no dot are
//     stdlib (e.g. "fmt", "encoding/json").  Always allowed.
//   - Self: anything under github.com/gregberns/harmonik/... is always allowed.
//   - go.mod direct deps: the module's own go.mod is read at runtime; every
//     module listed under "require" is allowed (including indirect).  This means
//     running "go mod tidy" before this tool is the canonical source of truth.
//   - Explicit deny list (deny takes precedence over allow): any import path
//     that is a transitive pull of an LLM SDK is rejected regardless of how it
//     arrived — reinforcing PL-INV-002 (daemon is deterministic; no LLM in
//     transitive closure).
//
// # Externalization path (future evolution)
//
// At MVH the allow/deny rules are embedded here.  When the list grows, move
// them to tools/forbid-import/allowlist.txt and deny-list.txt (one prefix per
// line, '#' comments) and load them at startup.  The tool's rule-file path
// would then be a protected file per quality-checks.md §Agent-enforceability
// item 5.
//
// # Invocation
//
//	go run ./tools/forbid-import [./...]
//
// The first positional argument is a package pattern passed to "go list".
// Defaults to "./..." when omitted.
//
// # Exit codes
//
//	0  — all imports are permitted
//	1  — one or more forbidden imports detected (details printed to stdout)
//	2  — tool usage/environment error
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// explicitDenyPrefixes are LLM SDK import path prefixes forbidden in the
// daemon's transitive closure (PL-INV-002).  Deny takes precedence over allow.
var explicitDenyPrefixes = []string{
	"github.com/anthropics/",
	"github.com/openai/",
	"github.com/sashabaranov/go-openai",
	"github.com/liushuangls/go-anthropic",
}

const selfPrefix = "github.com/gregberns/harmonik"

func main() {
	pattern := "./..."
	if len(os.Args) > 1 {
		pattern = os.Args[1]
	}

	allowedModules, err := readGoModRequires()
	if err != nil {
		fmt.Fprintf(os.Stderr, "forbid-import: reading go.mod: %v\n", err)
		os.Exit(2)
	}

	imports, err := listTransitiveImports(pattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "forbid-import: listing imports: %v\n", err)
		os.Exit(2)
	}

	forbidden := []string{}
	for _, imp := range imports {
		if isDenied(imp) {
			forbidden = append(forbidden, imp+" [DENIED: LLM SDK — PL-INV-002]")
			continue
		}
		if !isAllowed(imp, allowedModules) {
			forbidden = append(forbidden, imp+" [NOT IN ALLOWLIST]")
		}
	}

	if len(forbidden) > 0 {
		fmt.Println("forbid-import: forbidden transitive imports detected:")
		for _, f := range forbidden {
			fmt.Printf("  %s\n", f)
		}
		fmt.Println()
		fmt.Println("Add the module to go.mod via 'go get' if it is intentional,")
		fmt.Println("or remove the import.  LLM SDK imports are never permitted (PL-INV-002).")
		os.Exit(1)
	}

	fmt.Println("forbid-import: OK — all transitive imports are permitted")
}

// isDenied returns true if imp matches any deny-list prefix.
func isDenied(imp string) bool {
	for _, prefix := range explicitDenyPrefixes {
		if strings.HasPrefix(imp, prefix) {
			return true
		}
	}
	return false
}

// isAllowed returns true if imp is stdlib, self, or belongs to an allowed module.
func isAllowed(imp string, allowedModules []string) bool {
	// Stdlib: first path segment has no dot.
	first := imp
	if idx := strings.IndexByte(imp, '/'); idx >= 0 {
		first = imp[:idx]
	}
	if !strings.Contains(first, ".") {
		return true
	}

	// Self.
	if strings.HasPrefix(imp, selfPrefix) {
		return true
	}

	// go.mod requires.
	for _, mod := range allowedModules {
		if strings.HasPrefix(imp, mod) {
			return true
		}
	}

	return false
}

// goListPackage mirrors the subset of "go list -json" output we need.
type goListPackage struct {
	ImportPath  string
	Imports     []string
	TestImports []string
}

// listTransitiveImports calls "go list -deps -json <pattern>" and returns the
// deduplicated set of all import paths in the transitive dependency graph
// (excluding test imports; those are out of scope for the daemon ban).
func listTransitiveImports(pattern string) ([]string, error) {
	cmd := exec.Command("go", "list", "-deps", "-json", pattern)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}

	seen := map[string]bool{}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var pkg goListPackage
		if err := dec.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("json decode: %w", err)
		}
		seen[pkg.ImportPath] = true
		for _, imp := range pkg.Imports {
			seen[imp] = true
		}
	}

	result := make([]string, 0, len(seen))
	for imp := range seen {
		result = append(result, imp)
	}
	return result, nil
}

// readGoModRequires reads go.mod in the current working directory and returns
// the module paths listed under "require" directives.  This is intentionally
// simple (line-based) to avoid pulling in golang.org/x/mod at MVH.
func readGoModRequires() ([]string, error) {
	f, err := os.Open("go.mod")
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only file; close error immaterial

	var modules []string
	inRequire := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "require (" {
			inRequire = true
			continue
		}
		if inRequire && line == ")" {
			inRequire = false
			continue
		}
		// Single-line require: "require module/path vX.Y.Z"
		if strings.HasPrefix(line, "require ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				modules = append(modules, parts[1])
			}
			continue
		}
		// Inside require block: "  module/path vX.Y.Z [// indirect]"
		if inRequire && line != "" && !strings.HasPrefix(line, "//") {
			parts := strings.Fields(line)
			if len(parts) >= 1 {
				modules = append(modules, parts[0])
			}
		}
	}
	return modules, scanner.Err()
}
