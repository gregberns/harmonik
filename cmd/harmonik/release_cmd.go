package main

// release_cmd.go — `harmonik release` subcommand.
//
// Implements the release-ledger CLI surface from specs/release-pipeline.md §4:
//
//	harmonik release ledger              — list all ledger entries
//	harmonik release certify <semver>    — certify a pre-release (flip Prerelease:false, stamp CertifiedAt)
//	harmonik release yank <semver>       — mark a certified release as yanked
//
// The ledger is a JSON file at <project>/.harmonik/release-ledger.json.
// No daemon is required; all verbs operate directly on the file.
//
// Exit codes:
//
//	0  success
//	1  argument / flag error
//	2  ledger invariant violation (already certified, already yanked, etc.)
//	3  file I/O error
//
// Spec ref: specs/release-pipeline.md §4, §6, §7.1.
// Bead ref: hk-n7ofb.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/release"
)

const releaseTopUsage = `harmonik release — release ledger management

USAGE
  harmonik release <verb> [flags]

VERBS
  ledger               List all release ledger entries
  certify <semver>     Certify a pre-release (flip prerelease:false, stamp certified_at)
  yank    <semver>     Yank a certified release (requires --reason)

FLAGS (all verbs)
  --project DIR        Project directory (default: current working directory)

FLAGS (yank only)
  --reason TEXT        Human-readable reason for the yank (required)

EXIT CODES
  0  Success
  1  Argument / flag error
  2  Ledger invariant violation (already certified, already yanked, not found, etc.)
  3  File I/O error

EXAMPLES
  harmonik release ledger
  harmonik release certify v0.2.0
  harmonik release yank v0.2.0 --reason "critical regression in merge logic"
  harmonik release ledger --project /path/to/project
`

// runReleaseSubcommand dispatches `harmonik release <verb> [args]`.
// subArgs is os.Args[2:].
func runReleaseSubcommand(subArgs []string) int {
	if len(subArgs) == 0 || subArgs[0] == "--help" || subArgs[0] == "-h" {
		fmt.Print(releaseTopUsage) //nolint:forbidigo // help output to stdout
		return 0
	}

	verb := subArgs[0]
	rest := subArgs[1:]

	switch verb {
	case "ledger":
		return runReleaseLedger(rest)
	case "certify":
		return runReleaseCertify(rest)
	case "yank":
		return runReleaseYank(rest)
	default:
		fmt.Fprintf(os.Stderr, "harmonik release: unrecognised verb %q; verbs are: ledger, certify, yank\n", verb)
		return 1
	}
}

// parseReleaseFlags parses the shared --project flag out of args.
// Returns projectDir and the remaining positional args after flag extraction.
func parseReleaseFlags(args []string) (projectDir string, positional []string, extraFlags map[string]string, err error) {
	extraFlags = make(map[string]string)
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectDir = strings.TrimPrefix(args[i], "--project=")
		case args[i] == "--reason" && i+1 < len(args):
			i++
			extraFlags["reason"] = args[i]
		case strings.HasPrefix(args[i], "--reason="):
			extraFlags["reason"] = strings.TrimPrefix(args[i], "--reason=")
		case args[i] == "--help" || args[i] == "-h":
			extraFlags["help"] = "1"
		case strings.HasPrefix(args[i], "-"):
			return "", nil, nil, fmt.Errorf("unknown flag %q", args[i])
		default:
			positional = append(positional, args[i])
		}
	}

	if projectDir == "" {
		wd, wdErr := os.Getwd()
		if wdErr != nil {
			return "", nil, nil, fmt.Errorf("cannot determine working directory: %w", wdErr)
		}
		projectDir = wd
	}
	absDir, absErr := filepath.Abs(projectDir)
	if absErr != nil {
		return "", nil, nil, fmt.Errorf("cannot resolve project path %q: %w", projectDir, absErr)
	}
	return absDir, positional, extraFlags, nil
}

// runReleaseLedger implements `harmonik release ledger [--project DIR]`.
func runReleaseLedger(args []string) int {
	projectDir, _, flags, err := parseReleaseFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik release ledger: %v\n", err)
		return 1
	}
	if flags["help"] != "" {
		fmt.Print(`harmonik release ledger — list all release ledger entries

USAGE
  harmonik release ledger [--project DIR]

FLAGS
  --project DIR  Project directory (default: current working directory)
`)
		return 0
	}

	path := release.LedgerPath(projectDir)
	entries, loadErr := release.LoadLedgerFile(path)
	if loadErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik release ledger: %v\n", loadErr)
		return 3
	}

	if len(entries) == 0 {
		fmt.Println("release ledger: no entries")
		return 0
	}

	for _, e := range entries {
		state := "pre-release"
		if e.Yanked {
			state = "yanked"
		} else if e.CertifiedAt != "" {
			state = "stable"
		}
		fmt.Printf("%-12s  %-10s  certified_at=%-25s  commit=%s\n",
			e.Semver, state, orDash(e.CertifiedAt), short(e.CommitHash))
		if e.Yanked && e.YankedReason != "" {
			fmt.Printf("             yanked_reason: %s\n", e.YankedReason)
		}
	}
	return 0
}

// runReleaseCertify implements `harmonik release certify <semver> [--project DIR]`.
func runReleaseCertify(args []string) int {
	projectDir, positional, flags, err := parseReleaseFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik release certify: %v\n", err)
		return 1
	}
	if flags["help"] != "" {
		fmt.Print(`harmonik release certify — certify a pre-release

USAGE
  harmonik release certify <semver> [--project DIR]

ARGUMENTS
  semver  The semver string to certify, e.g. v0.2.0

FLAGS
  --project DIR  Project directory (default: current working directory)
`)
		return 0
	}
	if len(positional) != 1 {
		fmt.Fprintln(os.Stderr, "harmonik release certify: requires exactly one argument: <semver>")
		return 1
	}
	semver := positional[0]

	path := release.LedgerPath(projectDir)
	entries, loadErr := release.LoadLedgerFile(path)
	if loadErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik release certify: %v\n", loadErr)
		return 3
	}

	certifiedAt := time.Now().UTC().Format(time.RFC3339)
	updated, certErr := release.Certify(entries, semver, certifiedAt)
	if certErr != nil {
		if errors.Is(certErr, release.ErrAlreadyCertified) {
			fmt.Fprintf(os.Stderr, "harmonik release certify: %s is already certified (no-op)\n", semver)
			return 2
		}
		fmt.Fprintf(os.Stderr, "harmonik release certify: %v\n", certErr)
		return 2
	}

	if saveErr := release.SaveLedgerFile(path, updated); saveErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik release certify: %v\n", saveErr)
		return 3
	}

	fmt.Printf("harmonik release certify: %s certified at %s\n", semver, certifiedAt)
	return 0
}

// runReleaseYank implements `harmonik release yank <semver> --reason <reason> [--project DIR]`.
func runReleaseYank(args []string) int {
	projectDir, positional, flags, err := parseReleaseFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik release yank: %v\n", err)
		return 1
	}
	if flags["help"] != "" {
		fmt.Print(`harmonik release yank — yank a certified release

USAGE
  harmonik release yank <semver> --reason <reason> [--project DIR]

ARGUMENTS
  semver  The semver string to yank, e.g. v0.2.0

FLAGS
  --reason TEXT  Human-readable reason for the yank (required)
  --project DIR  Project directory (default: current working directory)
`)
		return 0
	}
	if len(positional) != 1 {
		fmt.Fprintln(os.Stderr, "harmonik release yank: requires exactly one argument: <semver>")
		return 1
	}
	semver := positional[0]
	reason := flags["reason"]
	if reason == "" {
		fmt.Fprintln(os.Stderr, "harmonik release yank: --reason is required")
		return 1
	}

	path := release.LedgerPath(projectDir)
	entries, loadErr := release.LoadLedgerFile(path)
	if loadErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik release yank: %v\n", loadErr)
		return 3
	}

	updated, yankErr := release.Yank(entries, semver, reason)
	if yankErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik release yank: %v\n", yankErr)
		return 2
	}

	if saveErr := release.SaveLedgerFile(path, updated); saveErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik release yank: %v\n", saveErr)
		return 3
	}

	fmt.Printf("harmonik release yank: %s yanked — %s\n", semver, reason)
	return 0
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
