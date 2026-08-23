package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gregberns/harmonik/internal/projectconfig"
)

func runMigrateRCPrefixSubcommand(args []string) int {
	return runMigrateRCPrefix(args, os.Stdin, os.Stdout, os.Stderr)
}

func runMigrateRCPrefix(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	projectDir := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectDir = strings.TrimPrefix(args[i], "--project=")
		case args[i] == "--help" || args[i] == "-h":
			if _, err := fmt.Fprint(stdout, migrateRCPrefixUsage); err != nil {
				return 1
			}
			return 0
		}
	}

	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik migrate-rc-prefix: cannot determine working directory: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		projectDir = wd
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik migrate-rc-prefix: cannot resolve project path %q: %v\n", projectDir, err); writeErr != nil {
			return 1
		}
		return 1
	}
	projectDir = absProject

	cfgPath := filepath.Join(projectDir, ".harmonik", "config.yaml")
	if _, err := os.Stat(cfgPath); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik migrate-rc-prefix: %s not found — run 'harmonik init' first\n", cfgPath); writeErr != nil {
			return 1
		}
		return 1
	}

	cfg, err := projectconfig.LoadProjectConfig(projectDir)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik migrate-rc-prefix: load config: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	if cfg.Daemon.RemoteControlPrefix != "" {
		if _, writeErr := fmt.Fprintf(stdout, "harmonik migrate-rc-prefix: daemon.remote_control_prefix is already set to %q — nothing to do\n", cfg.Daemon.RemoteControlPrefix); writeErr != nil {
			return 1
		}
		return 0
	}

	suggestion := readBeadsIssuePrefix(projectDir)
	if suggestion == "" {
		suggestion = deriveBeadPrefix(projectDir)
	}

	if _, err := fmt.Fprintf(stdout, "harmonik migrate-rc-prefix: daemon.remote_control_prefix is not set.\n"); err != nil {
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "This prefix is prepended to Claude Code remote-control session labels\n"); err != nil {
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "(e.g. %q → %q-captain, %q-paul) so concurrent projects are\n", suggestion, suggestion, suggestion); err != nil {
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "distinguishable in the global session picker. Empty = bare label (legacy).\n\n"); err != nil {
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "Enter prefix [%s]: ", suggestion); err != nil {
		return 1
	}

	sc := bufio.NewScanner(stdin)
	sc.Scan()
	chosen := strings.TrimSpace(sc.Text())
	if chosen == "" {
		chosen = suggestion
	}

	if err := patchRCPrefixInConfig(cfgPath, chosen); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik migrate-rc-prefix: patch config: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	if chosen == "" {
		if _, err := fmt.Fprintf(stdout, "harmonik migrate-rc-prefix: set daemon.remote_control_prefix to \"\" (bare label — no prefix)\n"); err != nil {
			return 1
		}
	} else {
		if _, err := fmt.Fprintf(stdout, "harmonik migrate-rc-prefix: set daemon.remote_control_prefix to %q\n", chosen); err != nil {
			return 1
		}
	}
	if _, err := fmt.Fprintf(stdout, "Run 'harmonik daemon restart' for the change to take effect.\n"); err != nil {
		return 1
	}
	return 0
}

func readBeadsIssuePrefix(projectDir string) string {
	brPath, err := exec.LookPath("br")
	if err != nil {
		return ""
	}
	//nolint:gosec // G204: brPath from LookPath; projectDir operator-controlled
	cmd := exec.CommandContext(context.Background(), brPath, "config", "get", "issue_prefix")
	cmd.Dir = projectDir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(out.String())
}

var rcPrefixFieldRe = regexp.MustCompile(`(?m)^(\s*)remote_control_prefix:.*$`)

func patchRCPrefixInConfig(cfgPath, prefix string) error {
	//nolint:gosec // G304: cfgPath constructed from operator-supplied projectDir
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", cfgPath, err)
	}
	content := string(data)

	if rcPrefixFieldRe.MatchString(content) {
		content = rcPrefixFieldRe.ReplaceAllStringFunc(content, func(match string) string {
			subs := rcPrefixFieldRe.FindStringSubmatch(match)
			indent := subs[1]
			return indent + "remote_control_prefix: " + prefix
		})
	} else {
		content = insertRCPrefixLine(content, prefix)
	}

	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", cfgPath, err)
	}
	return nil
}

func insertRCPrefixLine(content, prefix string) string {
	lines := strings.Split(content, "\n")

	newField := "  remote_control_prefix: " + prefix

	insertAfter := func(i int) string {
		result := make([]string, 0, len(lines)+1)
		result = append(result, lines[:i+1]...)
		result = append(result, newField)
		result = append(result, lines[i+1:]...)
		return strings.Join(result, "\n")
	}

	daemonIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "daemon:" {
			daemonIdx = i
			break
		}
	}

	if daemonIdx == -1 {
		if strings.HasSuffix(content, "\n") {
			return content + "daemon:\n  remote_control_prefix: " + prefix + "\n"
		}
		return content + "\ndaemon:\n  remote_control_prefix: " + prefix + "\n"
	}

	blockEnd := len(lines)
	for i := daemonIdx + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if lines[i][0] != ' ' && lines[i][0] != '\t' {
			blockEnd = i
			break
		}
	}

	anchors := []string{"workflow_mode:", "max_concurrent:", "target_branch:"}
	for _, anchor := range anchors {
		for i := daemonIdx + 1; i < blockEnd; i++ {
			if strings.HasPrefix(strings.TrimSpace(lines[i]), anchor) {
				return insertAfter(i)
			}
		}
	}

	return insertAfter(daemonIdx)
}

const migrateRCPrefixUsage = `harmonik migrate-rc-prefix — set daemon.remote_control_prefix for an existing project

USAGE
  harmonik migrate-rc-prefix [--project DIR]

FLAGS
  --project DIR  Project directory (default: current working directory)

WHAT IT DOES
  Checks whether daemon.remote_control_prefix is configured in .harmonik/config.yaml.
  If already set, exits 0 immediately (nothing to migrate).
  If absent or empty, reads the project's beads issue_prefix as a default suggestion,
  prompts you to confirm or enter a different slug, then writes the chosen value
  in-place into config.yaml — the rest of the file is preserved unchanged.

WHY
  daemon.remote_control_prefix is prepended to every --remote-control session label
  harmonik emits (e.g. "hk" → "hk-captain", "hk-paul"), making concurrent projects
  distinguishable in the global Claude Code session picker. Projects initialised
  before this field was introduced have no prefix and see bare labels. This command
  adds the field without re-running harmonik init (which overwrites the whole config).

NOTES
  - Side-effect-free except for patching .harmonik/config.yaml.
  - Does not start or contact a running daemon.
  - Run 'harmonik daemon restart' after migrating for the change to take effect.

EXAMPLES
  harmonik migrate-rc-prefix
  harmonik migrate-rc-prefix --project /path/to/project

SPEC
  plans/2026-06-20-remote-control-session-prefix/00-PLAN.md §8.3 (hk-f4w7)
`
