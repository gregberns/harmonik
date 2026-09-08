// Command durabilityhelper is the out-of-process leg of the kernel state
// package's kill -9 durability probe (kernel/state's TestSyncAppendSurvivesKillNine).
// It takes its database path from an environment variable, appends one
// record with sync=true, announces success on stdout, and then blocks so
// the test can send it an uncooperative kill signal — a real process death,
// not a clean shutdown.
package main

import (
	"context"
	"fmt"
	"os"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
	"github.com/gregberns/harmonik/kernel/state"
)

const (
	dbPathEnv = "HARMONIK_KERNEL_STATE_DURABILITY_DB"

	// Namespace, journal, and the record itself are fixed: the test that
	// launches this binary reads the same journal back under the same
	// (namespace, journal) pair after the kill.
	namespace     = "durability-ns"
	journal       = "durability-journal"
	payload       = "durable-payload"
	announcedLine = "APPENDED"
)

func main() {
	dbPath := os.Getenv(dbPathEnv)
	if dbPath == "" {
		fmt.Fprintf(os.Stderr, "durabilityhelper: %s is unset\n", dbPathEnv)
		os.Exit(1)
	}

	st, err := state.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "durabilityhelper: open: %v\n", err)
		os.Exit(1)
	}

	if _, err := st.Append(context.Background(), namespace, &kernelv1.JournalAppendRequest{
		Journal: journal,
		Records: [][]byte{[]byte(payload)},
		Sync:    true,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "durabilityhelper: append: %v\n", err)
		os.Exit(1)
	}

	if _, err := os.Stdout.WriteString(announcedLine + "\n"); err != nil {
		fmt.Fprintf(os.Stderr, "durabilityhelper: announce: %v\n", err)
		os.Exit(1)
	}

	select {} // blocks until the test process sends a kill signal
}
