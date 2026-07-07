All component specs converge on one branch: `integration/keeper-test`, one worktree, no cross-component
conflicts (all changes are localized to `internal/keeper/` files that don't overlap across the two fixes).
Build order: T1 (B4 + B3) in parallel, then T2 (sid-rebind, hold, migration) in parallel, then T3
(regression-floor registration) once T1+T2 land. T4 (crew e2e) is a separate bead, blocked.
