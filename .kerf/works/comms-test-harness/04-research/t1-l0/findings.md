# Research — T1 (L0 pure-projection)

No open research questions: the design doc (§2 "L0") already confirms all four seams exist and are used by
shipped tests (`commscursor_race_hkfvo9e_test.go` proves the L0 pattern works today). Risk: none identified
— pure functions, deterministic fixtures, no daemon dependency. Approach: table-driven Go tests per bead,
following the existing sentinel test's style for consistency.
