// Package main is the "hanging twin" binary used by the exploratory test
// harness (EXPLORATORY_TESTING_PLAN.md §4 P3).
//
// Behaviour: starts cleanly, writes nothing to stdout, and blocks until it is
// killed. SIGKILL terminates it immediately, which is what the T2/T3
// hanging-subprocess scenarios require.
package main

import "time"

func main() {
	for {
		time.Sleep(time.Hour)
	}
}
