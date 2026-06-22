package main

// sentinel_cmd.go — `harmonik sentinel` CLI subcommand block (flywheel V4, hk-9mr2).
//
// Exposes the sentinel's governor-trip and exception-write surface to the
// adversary crew session and to operators.
//
// Verbs:
//
//	emit-trip   Write ONE decision_required exception for a governor trip.
//	            Called by the sentinel-adversary crew after reviewing the
//	            captain's comms/commits and confirming the trip is legitimate.
//
// Exit codes:
//
//	0   Success (exception written or already pending — idempotent).
//	1   Argument / file-system error.
//	2   Unrecognised verb.
//
// The command is intentionally LLM-free — it only writes a file. The judgment
// (whether to call emit-trip) belongs to the adversary crew session.
//
// Spec ref: flywheel-motion.md §2.1 (bindingness is deterministic), §2.3.
// Bead ref: hk-9mr2. Epic: hk-0oca (codename:flywheel).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/sentinel"
)

// runSentinelSubcommand routes `harmonik sentinel <verb> [args]`.
// subArgs is os.Args[2:].
func runSentinelSubcommand(subArgs []string) int {
	verb := ""
	if len(subArgs) > 0 {
		verb = subArgs[0]
	}
	rest := []string{}
	if len(subArgs) > 1 {
		rest = subArgs[1:]
	}

	switch verb {
	case "--help", "-h", "":
		sentinelUsage()
		return 0
	case "emit-trip":
		return runSentinelEmitTrip(rest)
	case "clear-trip":
		return runSentinelClearTrip(rest)
	case "record-halt":
		return runSentinelRecordHalt(rest)
	default:
		fmt.Fprintf(os.Stderr, "harmonik sentinel: unknown verb %q\n", verb)
		sentinelUsage()
		return 2
	}
}

// runSentinelEmitTrip implements `harmonik sentinel emit-trip`.
//
// Writes ONE decision_required exception for a sentinel governor trip.
// Idempotent: if a pending sentinel exception already exists, returns the
// existing ack_token without writing again.
//
// Usage:
//
//	harmonik sentinel emit-trip [--project DIR] [--bead ID,...] [--undeployed-tail]
//
// Flags:
//
//	--project DIR           Project directory (default: cwd).
//	--bead ID[,ID,...]      Comma-separated list of ready bead IDs to name in the
//	                        exception reason. Multiple --bead flags are additive.
//	--undeployed-tail       Include "undeployed tail exists" in the exception reason.
//
// Prints the ack_token on stdout. No output when the exception was already pending.
func runSentinelEmitTrip(args []string) int {
	var (
		projectFlag       string
		readyBeadIDs      []string
		hasUndeployedTail bool
	)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--project" && i+1 < len(args):
			i++
			projectFlag = args[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case arg == "--bead" && i+1 < len(args):
			i++
			for _, id := range strings.Split(args[i], ",") {
				if id = strings.TrimSpace(id); id != "" {
					readyBeadIDs = append(readyBeadIDs, id)
				}
			}
		case strings.HasPrefix(arg, "--bead="):
			for _, id := range strings.Split(strings.TrimPrefix(arg, "--bead="), ",") {
				if id = strings.TrimSpace(id); id != "" {
					readyBeadIDs = append(readyBeadIDs, id)
				}
			}
		case arg == "--undeployed-tail":
			hasUndeployedTail = true
		case arg == "--help" || arg == "-h":
			sentinelEmitTripUsage()
			return 0
		default:
			fmt.Fprintf(os.Stderr, "harmonik sentinel emit-trip: unknown argument %q\n", arg)
			sentinelEmitTripUsage()
			return 1
		}
	}

	// Resolve project directory.
	if projectFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik sentinel emit-trip: cannot determine cwd: %v\n", err)
			return 1
		}
		projectFlag = wd
	}
	abs, err := filepath.Abs(projectFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik sentinel emit-trip: resolve project path: %v\n", err)
		return 1
	}

	tok, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:        abs,
		ReadyBeadIDs:      readyBeadIDs,
		HasUndeployedTail: hasUndeployedTail,
		Now:               time.Now().UTC(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik sentinel emit-trip: %v\n", err)
		return 1
	}
	if tok != "" {
		fmt.Println(tok)
	}
	return 0
}

// runSentinelClearTrip implements `harmonik sentinel clear-trip`.
//
// Operator escape hatch: scans .harmonik/decision_acks/ for the current pending
// sentinel exception and marks it acknowledged with ack_method="operator".
// Prints the cleared ack_token on stdout. Exits 0 with no output when no
// pending trip exists (idempotent).
//
// Usage:
//
//	harmonik sentinel clear-trip [--project DIR]
//
// Flags:
//
//	--project DIR   Project directory (default: cwd).
//
// Spec ref: flywheel-motion.md §2.2 (legitimate-halt clear path), bead hk-kgwv.
func runSentinelClearTrip(args []string) int {
	var projectFlag string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--project" && i+1 < len(args):
			i++
			projectFlag = args[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case arg == "--help" || arg == "-h":
			sentinelClearTripUsage()
			return 0
		default:
			fmt.Fprintf(os.Stderr, "harmonik sentinel clear-trip: unknown argument %q\n", arg)
			sentinelClearTripUsage()
			return 1
		}
	}

	if projectFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik sentinel clear-trip: cannot determine cwd: %v\n", err)
			return 1
		}
		projectFlag = wd
	}
	abs, err := filepath.Abs(projectFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik sentinel clear-trip: resolve project path: %v\n", err)
		return 1
	}

	tok, err := sentinel.ClearPendingTrip(context.Background(), abs, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik sentinel clear-trip: %v\n", err)
		return 1
	}
	if tok != "" {
		fmt.Println(tok)
	}
	return 0
}

// runSentinelRecordHalt implements `harmonik sentinel record-halt`.
//
// Captain escape hatch (§2.2 clause 2): records a legitimate-halt reason for
// the current pending sentinel exception and clears it, subject to re-adjudication
// on the next governor pass. Requires a non-empty --reason flag — an empty reason
// is indistinguishable from a bare self-ack, which the spec forbids.
//
// Usage:
//
//	harmonik sentinel record-halt --reason TEXT [--project DIR] [--token ACK_TOKEN]
//
// Flags:
//
//	--reason TEXT         Human-readable halt reason (required).
//	                      E.g. "ENOSPC: disk full on /dev/sda1" or
//	                           "infra: gb-mbp SSH unreachable".
//	--project DIR         Project directory (default: cwd).
//	--token ACK_TOKEN     Explicit ack_token to clear (default: scan for pending).
//
// Prints the cleared ack_token on stdout. Exits 0 with no output when no pending
// trip exists. The governor will re-evaluate on the next tick and emit a fresh
// trip if movement is still low (next-pass re-adjudication).
func runSentinelRecordHalt(args []string) int {
	var (
		projectFlag string
		reasonFlag  string
		tokenFlag   string
	)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--project" && i+1 < len(args):
			i++
			projectFlag = args[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case arg == "--reason" && i+1 < len(args):
			i++
			reasonFlag = args[i]
		case strings.HasPrefix(arg, "--reason="):
			reasonFlag = strings.TrimPrefix(arg, "--reason=")
		case arg == "--token" && i+1 < len(args):
			i++
			tokenFlag = args[i]
		case strings.HasPrefix(arg, "--token="):
			tokenFlag = strings.TrimPrefix(arg, "--token=")
		case arg == "--help" || arg == "-h":
			sentinelRecordHaltUsage()
			return 0
		default:
			fmt.Fprintf(os.Stderr, "harmonik sentinel record-halt: unknown argument %q\n", arg)
			sentinelRecordHaltUsage()
			return 1
		}
	}

	if strings.TrimSpace(reasonFlag) == "" {
		fmt.Fprintln(os.Stderr, "harmonik sentinel record-halt: --reason is required and must be non-empty")
		sentinelRecordHaltUsage()
		return 1
	}

	if projectFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik sentinel record-halt: cannot determine cwd: %v\n", err)
			return 1
		}
		projectFlag = wd
	}
	abs, err := filepath.Abs(projectFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik sentinel record-halt: resolve project path: %v\n", err)
		return 1
	}

	tok, err := sentinel.RecordLegitimateHalt(context.Background(), sentinel.LegitimateHaltInput{
		ProjectDir: abs,
		AckToken:   tokenFlag,
		HaltReason: reasonFlag,
		Now:        time.Now().UTC(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik sentinel record-halt: %v\n", err)
		return 1
	}
	if tok != "" {
		fmt.Println(tok)
	}
	return 0
}

func sentinelUsage() {
	fmt.Print(`harmonik sentinel — flywheel sentinel surface (hk-9mr2)

Usage:
  harmonik sentinel <verb> [flags]

Verbs:
  emit-trip    Write a decision_required governor-trip exception.
               Called by the sentinel-adversary crew after confirming the trip.
  clear-trip   Operator escape hatch: clear a stuck pending sentinel exception.
               Records ack_method=operator (flywheel-motion.md §2.2).
  record-halt  Captain escape hatch: record a legitimate-halt reason and clear
               the pending exception, subject to re-adjudication next pass.
               Requires --reason; records ack_method=legitimate_halt (§2.2 clause 2).

Run 'harmonik sentinel <verb> --help' for verb-specific flags.
`)
}

func sentinelEmitTripUsage() {
	fmt.Print(`harmonik sentinel emit-trip — write a sentinel governor-trip exception

Usage:
  harmonik sentinel emit-trip [--project DIR] [--bead ID,...] [--undeployed-tail]

Flags:
  --project DIR           Project directory (default: cwd)
  --bead ID[,ID,...]      Ready bead IDs to name in the exception reason
  --undeployed-tail       Note that merged-but-undeployed work exists

Idempotent: if a pending sentinel exception already exists, prints the existing
ack_token without writing again. Exit 0 in both cases.

Spec ref: flywheel-motion.md §2.1, §2.3 (flywheel V4).
`)
}

func sentinelClearTripUsage() {
	fmt.Print(`harmonik sentinel clear-trip — operator escape hatch to clear a stuck sentinel trip

Usage:
  harmonik sentinel clear-trip [--project DIR]

Flags:
  --project DIR   Project directory (default: cwd)

Scans .harmonik/decision_acks/ for a pending sentinel exception and marks it
acknowledged with ack_method=operator. Prints the cleared ack_token on stdout.
Exits 0 with no output when no pending trip exists (idempotent).

Spec ref: flywheel-motion.md §2.2 (legitimate-halt clear path, bead hk-kgwv).
`)
}

func sentinelRecordHaltUsage() {
	fmt.Print(`harmonik sentinel record-halt — captain escape hatch to record a legitimate halt

Usage:
  harmonik sentinel record-halt --reason TEXT [--project DIR] [--token ACK_TOKEN]

Flags:
  --reason TEXT       Human-readable halt reason (required, must be non-empty).
                      Examples: "ENOSPC: disk full on /dev/sda1"
                                "infra: gb-mbp SSH unreachable since 14:30 UTC"
  --project DIR       Project directory (default: cwd)
  --token ACK_TOKEN   Explicit ack_token to clear (default: scan for pending exception)

Records a legitimate-halt reason for the current pending sentinel exception and
marks it acknowledged with ack_method=legitimate_halt. The recorded reason is
visible to the adversary on re-adjudication. This is NOT a permanent suppression:
the governor evaluates on the next tick and emits a fresh trip if movement is
still low — the next adversary pass re-adjudicates with the reason in context.

An empty --reason is rejected: it would be indistinguishable from a bare self-ack,
which spec §2.2 forbids ("A legitimate-halt is a recorded reason subject to
re-adjudication, not a self-issued dismissal").

Prints the cleared ack_token on stdout. Exits 0 with no output when no pending
trip exists.

Spec ref: flywheel-motion.md §2.2 clause 2 (AC4, bead hk-jvul).
`)
}
