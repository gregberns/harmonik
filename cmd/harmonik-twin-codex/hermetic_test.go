package main

import (
	"testing"

	"github.com/gregberns/harmonik/internal/testhelpers/hermetic"
)

// TestMain makes this package's tests give the same answer on any machine.
//
// This package is outside CORE_PKGS but inside `make full`, which is the merge
// decision. A run of the whole tree with an empty HOME and commit.gpgsign=true
// in the global gitconfig named it: its fixtures call `git commit` and nothing
// turned signing off.
//
// See internal/testhelpers/hermetic for the full list of seams and the reason
// each one is on it.
func TestMain(m *testing.M) { hermetic.Main(m) }
