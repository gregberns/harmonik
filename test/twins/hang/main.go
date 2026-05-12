// Package main is the "hanging twin" binary used by the exploratory test
// harness (EXPLORATORY_TESTING_PLAN.md §4 P3).
//
// Behaviour: starts cleanly, writes nothing to stdout, and blocks forever via
// select{}. No signal handlers are installed, so SIGTERM is ignored by Go's
// default signal mask and SIGKILL terminates the process immediately — which is
// exactly what T2/T3 hanging-subprocess scenarios require.
package main

func main() {
	select {}
}
