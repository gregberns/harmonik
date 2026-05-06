# AR cluster bootstrap verification

**Date:** 2026-05-05. Verifier: focused AR pass against `bootstrap-subset-opening.md` §3 line 50 — "Include sensor beads only (`zs0.41`, `.50`)."

## Finding: opening pass is wrong on two counts

**Count 1 — `hk-zs0.41` is not a sensor.** It is `ar-042` "Invariants MUST name their sensor" — the cross-cutting *meta-rule* about sensor declaration in every spec's invariant blocks. The actual AR-INV sensor beads are `hk-zs0.50`–`.53` (`ar-inv-001`, `-003`, `-007`, `-008`).

**Count 2 — sensors-only is incomplete given Q1=twin-in / Q4=harness-in.** `hk-zs0.54` (Define `agent_type` identifier regex, `kind:primitive-shape`) is depended on by `hk-8i31.74` (`LaunchSpec`) and `hk-8i31.71` (`Handler` interface) — both load-bearing for Cluster C twin spawn. It must be INCLUDE.

## Recommended AR INCLUDE list

- `hk-zs0.50` — sensor AR-INV-001 (mechanism/cognition split at process boundary). Required by Q1: twin spawn is the canonical out-of-process delegation.
- `hk-zs0.51` — sensor AR-INV-003 (search + verifier + traces required-triple). Cheap corpus-presence test; gates conformance claim.
- `hk-zs0.52` — sensor AR-INV-007 (centralized-controller). The thesis. Bootstrap daemon is the first conformance instance.
- `hk-zs0.53` — sensor AR-INV-008 (three-artifact separation). Cheap corpus-lint; encoded in code structure.
- `hk-zs0.54` — `agent_type` regex schema. Hard prerequisite of HC `LaunchSpec`/`Handler`.

Optional add: `hk-zs0.42` (`ar-042` meta-rule itself) is satisfied structurally by including the four sensors; can stay deferred.

## Surprises

- The opening pass conflates the §5 invariant-sensor beads (`.50`–`.53`) with the §4 meta-rule that names them (`.41`). Easy slip — both carry "sensor" in the prose, but only `.50`–`.53` carry `kind:invariant`.
- AR has only **one schema bead** (`hk-zs0.54`) and it is bootstrap-load-bearing. Worth flagging in the synthesis pass that "AR is declaration-heavy with zero §6 type schemas" is not quite true.
- 49 §4 first-class declarations are correctly excluded; their conformance is structural, not implemented per-bead.

**Inline AR INCLUDE: `hk-zs0.50, .51, .52, .53, .54`.**
