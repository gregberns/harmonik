# Workflow Convergence Evidence — 2026-08-01

## Independent readings

Three independent source and plan readings reached the same conclusion.

The code topology reading found one large shared pre-dispatch section, a
roughly 725-line single tail, and a roughly 218-line DOT branch that delegates
to the general DOT driver. Launch and much post-exit work are already shared.
The single tail still duplicates launch context, head and no-commit checks,
error classification, and terminal-spine setup. A no-review `implement → close`
DOT graph can express the single behavior. It must retain explicit bypass audit
and selection semantics while the tail is retired.

The intent reading found the same direction in the program plan. DOT is the
normal default. Review-loop is retired. `workflow:single` is an explicit,
audited compatibility choice. Step 7 names the destination: make single-shot a
graph and delete the single-mode tail. The plan observed 866 DOT starts and two
single starts in its measured window.

The event reading found that neither prior event option is valid. `run_started`
is emitted before DOT load. The DOT helper creates a random UUID only to satisfy
an in-memory record. The graph retains its logical identity as a string while
the core record expects a UUID. Neither is a truthful durable identity.

## Decision

Resolve and validate an immutable workflow descriptor before `run_started`.
Use it for the core run record, durable event, replay, reconciliation, and DOT
execution. A descriptor identity must come from the selected graph, not a
random helper UUID.

Keep the reviewed `standard-bead.dot` graph as the normal default. Map an
explicit no-review selection to a named no-review DOT graph. After the known
tail behavior is moved or retired, delete the imperative single tail and its
dispatch branch.

## Sequencing risks

The tail cannot be deleted as a text cleanup. Its known behavior includes
post-exit classification, escape and no-commit guards, session and orphan
handling, audit semantics, and merge budget. The plan records 18 tail-only
capabilities: 13 need porting, three need an explicit retirement decision, and
the rest are shared. The descriptor first makes event ownership correct without
preserving the duplicate executor.
