# Change Design Review — Declared Substrate Capability Contract

## Round 1

BLOCK. The first design named the source inventory but did not decide each
capability's requirement, absent result, consumer port, and focused proof. It
also left the unreachable `runSessionSpawner` outcome open.

## Round 2

The revised daemon contract design adds a decision row for every capability.
It keeps the base port narrow, preserves the concrete Step 12 serialization
rule, and selects removal of the zero-probe, unreachable run-session interface.

## Round 3

APPROVE. The decision table now records every capability's owner, consumer
port, selected-mode requirement, absent result, and focused proof.
`sessionCreator` is limited to independent crew sessions. The selected removal
of run sessions names the API, state, branch, tests, and documentation that
must leave. The input buffer rule keeps concurrent shared-session runs apart
by their captured pane targets. The Step 12 serialization rule and the narrow
handler boundary remain intact.

## Alpha Review

The design package is ready for Alpha review. Do not advance to spec drafting
until Alpha accepts the capability decisions.
