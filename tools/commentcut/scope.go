package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// defaultRoots is the scope. It is never "./..." — evaltasks/ holds graded Go
// fixtures whose comments are part of the grade, and testdata/ holds lint
// fixtures whose findings are the point of the file.
var defaultRoots = []string{"internal", "cmd", "tools", "test"}

// excludedDirs are refused wherever they appear in a path.
var excludedDirs = map[string]bool{
	"testdata":     true,
	"evaltasks":    true,
	".git":         true,
	"node_modules": true,
	"vendor":       true,
}

// findFiles walks roots and returns repo-relative paths of every Go file in
// scope, sorted. A path filter, when non-empty, keeps only files whose path
// contains one of its substrings; an exclude filter drops files whose path
// contains one of its substrings and wins over the keep filter.
//
// Containment is checked, not assumed. "-root .." used to resolve outside the
// repository and rewrite files in sibling checkouts, because the exclusion set
// tests directory NAMES and never asks whether the walk is still inside the
// tree it was pointed at.
func findFiles(repo string, roots, filter, exclude []string) ([]string, error) {
	if len(roots) == 0 {
		roots = defaultRoots
	}
	g, err := newGuard(repo)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, root := range roots {
		found, err := filesUnder(g, root)
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
	}
	out = keepMatching(out, filter)
	out = dropMatching(out, exclude)
	sort.Strings(out)
	return out, nil
}

// guard answers one question: is this absolute path inside the repository?
// It answers it twice — once lexically, so "-root ../.." is refused before any
// walk, and once through the symlinks, so a symlinked .go file cannot make the
// tool write outside the tree either.
type guard struct {
	repo string // as given, cleaned and absolute
	real string // with every symlink resolved
}

func newGuard(repo string) (*guard, error) {
	abs, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	g := &guard{repo: filepath.Clean(abs), real: filepath.Clean(abs)}
	if r, err := filepath.EvalSymlinks(g.repo); err == nil {
		g.real = r
	}
	return g, nil
}

// contains reports whether abs is the repository root or lives under it, both
// lexically and after symlink resolution.
func contains(base, abs string) bool {
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// check refuses a path that leaves the repository by either route.
func (g *guard) check(abs string) error {
	clean := filepath.Clean(abs)
	if !contains(g.repo, clean) {
		return fmt.Errorf("%s is outside the repository at %s", clean, g.repo)
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		// A path that does not resolve is handled by the caller's os.Stat.
		return nil //nolint:nilerr // absence is not an escape; the caller reports it
	}
	if !contains(g.real, filepath.Clean(resolved)) {
		return fmt.Errorf("%s resolves through a symlink to %s, outside the repository at %s",
			clean, resolved, g.real)
	}
	return nil
}

// filesUnder returns the in-scope Go files of one root, which may name a
// directory or a single file. Every candidate is checked for containment
// before it is returned.
func filesUnder(g *guard, root string) ([]string, error) {
	if filepath.IsAbs(root) {
		return nil, fmt.Errorf("scope root %s: must be relative to the repository root", root)
	}
	abs := filepath.Join(g.repo, root)
	if err := g.check(abs); err != nil {
		return nil, fmt.Errorf("scope root %s: %w", root, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("scope root %s: %w", root, err)
	}
	if !info.IsDir() {
		rel, err := filepath.Rel(g.repo, abs)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(rel, ".go") && !excluded(rel) {
			return []string{rel}, nil
		}
		return nil, nil
	}
	var out []string
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		return walkEntry(g, &out, path, d, err)
	})
	return out, err
}

// walkEntry decides what one entry of the scope walk contributes.
func walkEntry(g *guard, out *[]string, path string, d fs.DirEntry, err error) error {
	if err != nil {
		return err
	}
	if d.IsDir() {
		if skipDir(d.Name()) {
			return fs.SkipDir
		}
		return nil
	}
	rel, err := filepath.Rel(g.repo, path)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(path, ".go") || excluded(rel) {
		return nil
	}
	if err := g.check(path); err != nil {
		return fmt.Errorf("scope walk: %w", err)
	}
	*out = append(*out, rel)
	return nil
}

// dropMatching removes files whose path contains one of the substrings.
func dropMatching(files, exclude []string) []string {
	if len(exclude) == 0 {
		return files
	}
	kept := make([]string, 0, len(files))
	for _, f := range files {
		drop := false
		for _, sub := range exclude {
			if strings.Contains(f, sub) {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, f)
		}
	}
	return kept
}

func skipDir(name string) bool {
	return excludedDirs[name] || (strings.HasPrefix(name, ".") && name != ".")
}

func keepMatching(files, filter []string) []string {
	if len(filter) == 0 {
		return files
	}
	kept := files[:0]
	for _, f := range files {
		for _, sub := range filter {
			if strings.Contains(f, sub) {
				kept = append(kept, f)
				break
			}
		}
	}
	return kept
}

func excluded(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if excludedDirs[part] {
			return true
		}
	}
	return false
}

// packagesOf maps repo-relative file paths to the "./dir/..." patterns go test
// accepts, deduplicated.
func packagesOf(files []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		dir := "./" + filepath.ToSlash(filepath.Dir(f))
		if !seen[dir] {
			seen[dir] = true
			out = append(out, dir)
		}
	}
	sort.Strings(out)
	return out
}
