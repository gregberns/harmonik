package main

// migrate_rc_prefix_cmd.go — `harmonik migrate-rc-prefix [--project DIR]`
// (hk-f4w7).
//
// Interactive one-shot migration for existing projects that pre-date the
// daemon.remote_control_prefix config field. When the field is absent or empty in
// .harmonik/config.yaml, the command:
//
//  1. Reads the project's beads issue_prefix via `br config get issue_prefix`
//     as a default suggestion (falls back to deriveBeadPrefix on br failure).
//  2. Prompts the user to confirm or override the suggestion.
//  3. Writes the chosen value in-place into .harmonik/config.yaml, preserving
//     all existing content and comments.
//
// Satisfies locked decision §8.3 of the rc-prefix plan: do NOT silently
// backfill; ask the user at migrate time.
//
// Bead ref: hk-f4w7.

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gregberns/harmonik/internal/daemon"
)

// runMigrateRCPrefixSubcommand dispatches `harmonik migrate-rc-prefix [flags]`.
func runMigrateRCPrefixSubcommand(args []string) int {
	return runMigrateRCPrefix(args, os.Stdin, os.Stdout, os.Stderr)
}

// runMigrateRCPrefix is the testable core of migrate-rc-prefix.
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
			fmt.Fprint(stdout, migrateRCPrefixUsage)
			return 0
		}
	}

	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "harmonik migrate-rc-prefix: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = wd
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik migrate-rc-prefix: cannot resolve project path %q: %v\n", projectDir, err)
		return 1
	}
	projectDir = absProject

	cfgPath := filepath.Join(projectDir, ".harmonik", "config.yaml")
	if _, err := os.Stat(cfgPath); err != nil {
		fmt.Fprintf(stderr, "harmonik migrate-rc-prefix: %s not found — run 'harmonik init' first\n", cfgPath)
		return 1
	}

	// Load current config to check whether the prefix is already set.
	cfg, err := daemon.LoadProjectConfig(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik migrate-rc-prefix: load config: %v\n", err)
		return 1
	}
	if cfg.Daemon.RemoteControlPrefix != "" {
		fmt.Fprintf(stdout, "harmonik migrate-rc-prefix: daemon.remote_control_prefix is already set to %q — nothing to do\n", cfg.Daemon.RemoteControlPrefix)
		return 0
	}

	// Suggestion: prefer beads issue_prefix, fall back to dir-derived slug.
	suggestion := readBeadsIssuePrefix(projectDir)
	if suggestion == "" {
		suggestion = deriveBeadPrefix(projectDir)
	}

	// Prompt the user.
	fmt.Fprintf(stdout, "harmonik migrate-rc-prefix: daemon.remote_control_prefix is not set.\n")
	fmt.Fprintf(stdout, "This prefix is prepended to Claude Code remote-control session labels\n")
	fmt.Fprintf(stdout, "(e.g. %q → %q-captain, %q-paul) so concurrent projects are\n", suggestion, suggestion, suggestion)
	fmt.Fprintf(stdout, "distinguishable in the global session picker. Empty = bare label (legacy).\n\n")
	fmt.Fprintf(stdout, "Enter prefix [%s]: ", suggestion)

	sc := bufio.NewScanner(stdin)
	sc.Scan()
	chosen := strings.TrimSpace(sc.Text())
	if chosen == "" {
		chosen = suggestion
	}

	if err := patchRCPrefixInConfig(cfgPath, chosen); err != nil {
		fmt.Fprintf(stderr, "harmonik migrate-rc-prefix: patch config: %v\n", err)
		return 1
	}

	if chosen == "" {
		fmt.Fprintf(stdout, "harmonik migrate-rc-prefix: set daemon.remote_control_prefix to \"\" (bare label — no prefix)\n")
	} else {
		fmt.Fprintf(stdout, "harmonik migrate-rc-prefix: set daemon.remote_control_prefix to %q\n", chosen)
	}
	fmt.Fprintf(stdout, "Run 'harmonik daemon restart' for the change to take effect.\n")
	return 0
}

// readBeadsIssuePrefix returns the project's beads issue_prefix by running
// `br config get issue_prefix` in projectDir. Returns "" on any error so the
// caller can fall back to deriveBeadPrefix.
func readBeadsIssuePrefix(projectDir string) string {
	brPath, err := exec.LookPath("br")
	if err != nil {
		return ""
	}
	//nolint:gosec // G204: brPath from LookPath; projectDir operator-controlled
	cmd := exec.Command(brPath, "config", "get", "issue_prefix")
	cmd.Dir = projectDir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(out.String())
}

// rcPrefixFieldRe matches an existing (non-commented) remote_control_prefix:
// field line in the YAML, capturing leading whitespace. It does NOT match
// comment lines like "  # remote_control_prefix: ..." because the '#' would
// appear after the leading whitespace before the field name.
var rcPrefixFieldRe = regexp.MustCompile(`(?m)^(\s*)remote_control_prefix:.*$`)

// patchRCPrefixInConfig rewrites daemon.remote_control_prefix in the YAML file
// at cfgPath. The rest of the file, including all comments, is preserved.
//
// Strategy:
//  1. If a remote_control_prefix: line already exists (even with empty value),
//     replace it in-place.
//  2. Otherwise, insert the field line-by-line after the first daemon-block
//     anchor found (workflow_mode, max_concurrent, or target_branch).
//  3. If the daemon: block exists but none of those anchors do, insert
//     immediately after the "daemon:" line itself.
//  4. If no daemon: block exists at all, append a minimal daemon: block.
func patchRCPrefixInConfig(cfgPath, prefix string) error {
	//nolint:gosec // G304: cfgPath constructed from operator-supplied projectDir
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", cfgPath, err)
	}
	content := string(data)

	if rcPrefixFieldRe.MatchString(content) {
		// Replace the existing line, preserving its indentation.
		content = rcPrefixFieldRe.ReplaceAllStringFunc(content, func(match string) string {
			subs := rcPrefixFieldRe.FindStringSubmatch(match)
			indent := subs[1]
			return indent + "remote_control_prefix: " + prefix
		})
	} else {
		content = insertRCPrefixLine(content, prefix)
	}

	//nolint:gosec // G306: config file; 0644 matches writeConfigYAML convention
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", cfgPath, err)
	}
	return nil
}

// insertRCPrefixLine inserts "  remote_control_prefix: <prefix>" into content
// using a line-by-line scan. Insertion anchors (tried in order):
//  1. After workflow_mode:, max_concurrent:, or target_branch: (first found, in
//     that order — these are the most common daemon-block fields).
//  2. After the "daemon:" line itself (no known sub-fields found).
//  3. Append a new "daemon:" block at the end of the file.
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

	// Locate the "daemon:" block header.
	daemonIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "daemon:" {
			daemonIdx = i
			break
		}
	}

	// Pass 3 (no daemon: block at all): append a minimal one.
	if daemonIdx == -1 {
		if strings.HasSuffix(content, "\n") {
			return content + "daemon:\n  remote_control_prefix: " + prefix + "\n"
		}
		return content + "\ndaemon:\n  remote_control_prefix: " + prefix + "\n"
	}

	// Determine the extent of the daemon block: it ends at the first following
	// non-blank line that is NOT indented (i.e. a new top-level key).
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

	// Pass 1: insert after the first matching anchor field WITHIN the daemon
	// block, so a like-named field in an unrelated top-level block is not
	// mistaken for a daemon sub-field.
	anchors := []string{"workflow_mode:", "max_concurrent:", "target_branch:"}
	for _, anchor := range anchors {
		for i := daemonIdx + 1; i < blockEnd; i++ {
			if strings.HasPrefix(strings.TrimSpace(lines[i]), anchor) {
				return insertAfter(i)
			}
		}
	}

	// Pass 2: known daemon block but no anchor sub-field — insert right after
	// the "daemon:" line itself.
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
