package main

import (
	"io"
	"os"

	"github.com/gregberns/harmonik/internal/scenario"
)

func runHarnessSubcommand(args []string) int {
	return runHarness(args, os.Stdout, os.Stderr)
}

func runHarness(args []string, stdout, stderr io.Writer) int {
	return scenario.RunHarness(args, stdout, stderr)
}
