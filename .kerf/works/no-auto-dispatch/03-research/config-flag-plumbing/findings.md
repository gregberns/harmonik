# Research — C2 Config / flag / status plumbing

> **Provenance.** All anchors bead-sourced from `hk-04q2j.3` (operator/captain code survey). Not
> independently re-verified by the planning agent — treat line numbers as approximate and re-locate
> at implementation time.

## The NoAutoPull surface (grep target: `NoAutoPull` / `noAutoPull` / `auto-pull`)

- `cmd/harmonik/main.go`: `autoPullFlag` decl ~996-998; `Config.NoAutoPull` assignment ~1353.
- `cmd/harmonik/usage.go`: flag help lines ~53-54, 59.
- `internal/lifecycle/supervise/shim.go`: ~261, 269 — the shim forwards `--no-auto-pull` to the
  spawned daemon; drop the arg.
- daemon `Config.NoAutoPull` (`daemon.go` ~379-395) — the config field feeding `workloop`'s
  `deps.noAutoPull` (see C1).
- `bootstate.go:374` — boot-state carries/echoes the flag.
- `internal/core/daemonevents_hqwn59.go`: status-payload `NoAutoPull` field ~1156, 1184-1186 —
  surfaced on the daemon status event.
- `scenario/orchdrive.go` ~216-219 + `daemon/scenariotest/concurrent_merge.go:246` — test-harness
  setters.
- `brcli/ready.go:182` — a comment referencing the fallback.

## Finding — this is a clean field-removal, gated on C1

Nothing here dispatches; it is pure plumbing that configured C1's now-deleted gate. Once C1 removes
the field, every reference above is dead and deletes cleanly. Sequenced AFTER C1 (bead `blocks`
edge).

## Open decision D2 (from the bead note, verbatim)

"Operator's call whether to keep `--auto-pull`/`--no-auto-pull` as accepted-but-ignored no-ops for
back-compat." If kept: the CLI still parses the flags but they set nothing (a warning is optional).
If deleted: any script/supervisor passing them errors. The supervise/shim.go forwarder (~261/269)
is the concrete back-compat consumer to check — if the shim stops passing the flag, an older daemon
binary is unaffected either way.
