package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Formatter runs the repository's own formatters over the files the tool
// touched. gofumpt, not gofmt, is the gate (make fmt-check runs
// scripts/go-format.sh check), and gofumpt does things gofmt does not — it
// deletes leading and trailing blank lines inside a block, which deleting a
// lone comment creates. A gofmt-only pass leaves files that fail the gate.
type Formatter struct {
	Gofumpt string
	Gci     string
	Module  string
	Repo    string
}

// NewFormatter locates gofumpt and gci the way scripts/go-format.sh does: the
// GOFUMPT and GCI environment variables win, otherwise .tools/ under the main
// working tree.
func NewFormatter(repo, module string) (*Formatter, error) {
	f := &Formatter{Module: module, Repo: repo}
	var err error
	if f.Gofumpt, err = findTool("GOFUMPT", repo, "gofumpt"); err != nil {
		return nil, err
	}
	if f.Gci, err = findTool("GCI", repo, "gci"); err != nil {
		return nil, err
	}
	return f, nil
}

func findTool(env, repo, name string) (string, error) {
	if v := os.Getenv(env); v != "" {
		return v, nil
	}
	home := toolsHome(repo)
	p := filepath.Join(home, ".tools", name)
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("%s not found: set $%s, or run 'make tools' to populate %s/.tools",
		name, env, home)
}

// toolsHome returns the main working tree. .tools/ holds built binaries and is
// gitignored, so a git worktree receives none of it.
func toolsHome(repo string) string {
	out, err := exec.Command("git", "-C", repo, "rev-parse",
		"--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return repo
	}
	dir := strings.TrimSpace(string(out))
	return strings.TrimSuffix(strings.TrimSuffix(dir, "/"), "/.git")
}

// Run formats the given repo-relative files in place.
func (f *Formatter) Run(files []string) error {
	if len(files) == 0 {
		return nil
	}
	for _, batch := range chunk(files, 200) {
		args := append([]string{
			"write", "-s", "standard", "-s", "default",
			"-s", "prefix(" + f.Module + ")", "--",
		}, batch...)
		if err := f.exec(f.Gci, args); err != nil {
			return err
		}
		if err := f.exec(f.Gofumpt, append([]string{"-w", "--"}, batch...)); err != nil {
			return err
		}
	}
	return nil
}

func (f *Formatter) exec(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Dir = f.Repo
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", filepath.Base(bin), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func chunk(ss []string, n int) [][]string {
	var out [][]string
	for len(ss) > n {
		out = append(out, ss[:n])
		ss = ss[n:]
	}
	if len(ss) > 0 {
		out = append(out, ss)
	}
	return out
}
