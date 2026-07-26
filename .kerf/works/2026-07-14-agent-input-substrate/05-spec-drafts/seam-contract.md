# 05 spec-draft index — component `seam-contract` → target spec files

> The `seam-contract` research/design component (C1 input+ack contract, C3 observation-only tmux
> boundary, C6 deletion boundary) produces NO spec file of its own — kerf spec-drafts map 1:1 to
> TARGET SPEC files, not to research components. This index maps this component to the target-spec
> drafts that carry its normative text. See those files for the full updated spec text.

- **`agent-input.md`** (NEW, prefix AIS) — AIS-001 InputPort + retirements, AIS-002 machine depguard,
  AIS-003 Ack, AIS-004/005 dual-signal + front-stop, AIS-011/012 observation-only tmux + deletion
  boundary, AIS-INV-001 bounded liveness.
- **`handler-contract.md`** — HC-069 InputPort verb, HC-070 Ack + emitted event, HC-071 depguard
  inversion, HC-INV-007 carve-out, HC-INV-008 bounded input liveness.
- **`process-lifecycle.md`** — PL-021b observation-only subclause, PL-021d demotion, C6 deletion note.
- **`session-keeper.md`** — SK-002 keeper carve-out, SK-021 deferred migration.
