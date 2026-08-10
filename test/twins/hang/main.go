// Package main is the "hanging twin" binary used by the exploratory test
// harness (EXPLORATORY_TESTING_PLAN.md §4 P3).
//
// Behaviour: starts cleanly, writes nothing to stdout, and blocks until it is
// killed. SIGKILL terminates it immediately, which is what the T2/T3
// hanging-subprocess scenarios require.
package main

import "time"

func main() {
	// Do NOT write this as `select {}`. That blocks the main goroutine, but it
	// also leaves every goroutine asleep with no timer pending, so the runtime's
	// deadlock detector fires and the process dies at once:
	//
	//	fatal error: all goroutines are asleep - deadlock!
	//	goroutine 1 [select (no cases)]:
	//	main.main()
	//
	// That is what this binary did until 2026-08-10, so the twin every T2
	// hanging-subprocess test launched had already exited by itself, in
	// milliseconds, with a panic and a non-zero status. The tests still passed —
	// a bead is reopened on a panic exit as readily as on a SIGKILL — so nothing
	// reported that the condition under test was never produced (hk-3xwsj).
	//
	// A pending timer keeps the detector quiet, and sleeping in a loop needs no
	// signal handler, so the process keeps Go's default signal dispositions.
	for {
		time.Sleep(time.Hour)
	}
}
