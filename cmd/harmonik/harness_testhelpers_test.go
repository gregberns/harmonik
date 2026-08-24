package main

import (
	"io"

	"github.com/gregberns/harmonik/internal/scenario"
)

func harnessDiscoverScenarios(
	projectRoot string,
	scenarioPaths []string,
	cadenceFilter scenario.CadenceFilter,
	verbose bool,
	stderr io.Writer,
) ([]scenario.Entry, []error) {
	return scenario.DiscoverScenarios(projectRoot, scenarioPaths, cadenceFilter, verbose, stderr)
}

func harnessMatrixCellCount(matrix map[string][]string) int {
	return scenario.MatrixCellCount(matrix)
}
