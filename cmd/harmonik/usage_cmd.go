package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gregberns/harmonik/internal/usage"
)

const usageCmdHelp = `harmonik usage — token cost analysis: join transcripts × events by run_id

USAGE
  harmonik usage [flags]

FLAGS
  --since DURATION|ISO   Window start: duration (24h, 7d) or ISO UTC timestamp
                         Default: 24h ago
  --until ISO            Window end: ISO UTC timestamp (default: now)
  --format FORMAT        Output format: summary (default) or json
  --project DIR          Project directory (default: current working directory)

DESCRIPTION
  Reads ~/.claude/projects/.../session.jsonl transcripts and .harmonik/events/events.jsonl,
  joins them on run/<run_id> (git branch), and produces cost rollups by bead, model,
  and session. Works without a running daemon.

  Outputs:
    summary   one-screen human-readable report (default)
    json      machine-readable JSON, pipeable to jq

EXAMPLES
  harmonik usage --since 24h
  harmonik usage --since 2026-06-21T00:00:00Z --until 2026-06-22T00:00:00Z
  harmonik usage --since 7d --format json | jq '.top_beads[]'
  harmonik usage --format json | jq '.by_tier'

EXIT CODES
  0   Success
  1   Argument or I/O error
`

// runUsageSubcommand implements `harmonik usage [flags]`.
func runUsageSubcommand(args []string) int {
	var sinceStr, untilStr, format, projectDir string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--help", "-h":
			fmt.Print(usageCmdHelp)
			return 0
		case "--since":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "harmonik usage: --since requires a value")
				return 1
			}
			i++
			sinceStr = args[i]
		case "--until":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "harmonik usage: --until requires a value")
				return 1
			}
			i++
			untilStr = args[i]
		case "--format":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "harmonik usage: --format requires a value")
				return 1
			}
			i++
			format = args[i]
		case "--project":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "harmonik usage: --project requires a value")
				return 1
			}
			i++
			projectDir = args[i]
		default:
			// Allow --since=VALUE and --format=VALUE forms.
			consumed := false
			for _, prefix := range []string{"--since=", "--until=", "--format=", "--project="} {
				if len(args[i]) > len(prefix) && args[i][:len(prefix)] == prefix {
					val := args[i][len(prefix):]
					switch prefix {
					case "--since=":
						sinceStr = val
					case "--until=":
						untilStr = val
					case "--format=":
						format = val
					case "--project=":
						projectDir = val
					}
					consumed = true
					break
				}
			}
			if !consumed {
				fmt.Fprintf(os.Stderr, "harmonik usage: unknown flag %q\n", args[i])
				return 1
			}
		}
	}

	if format == "" {
		format = "summary"
	}
	if format != "summary" && format != "json" {
		fmt.Fprintf(os.Stderr, "harmonik usage: --format must be 'summary' or 'json', got %q\n", format)
		return 1
	}

	if projectDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik usage: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = cwd
		if root := findProjectRoot(projectDir); root != "" {
			projectDir = root
		}
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik usage: cannot resolve project path: %v\n", err)
		return 1
	}
	projectDir = abs

	cfg := usage.DefaultConfig(projectDir)

	if sinceStr != "" {
		parsed, parseErr := usage.ParseSince(sinceStr)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik usage: %v\n", parseErr)
			return 1
		}
		cfg.Since = parsed
	}

	if untilStr != "" {
		cfg.Until = usage.NormTS(untilStr)
		if cfg.Until == "" {
			fmt.Fprintf(os.Stderr, "harmonik usage: cannot parse --until %q: expected ISO timestamp\n", untilStr)
			return 1
		}
	} else {
		cfg.Until = usage.NormTS(time.Now().UTC().Format(time.RFC3339))
	}

	result, err := usage.RunAnalysis(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik usage: analysis failed: %v\n", err)
		return 1
	}

	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(result); encErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik usage: JSON encode error: %v\n", encErr)
			return 1
		}
	default:
		usage.PrintSummary(result, os.Stdout)
	}

	return 0
}
