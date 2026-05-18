# DRAFT — Amendments to `specs/workspace-model.md`

Additive. Suggested clause number at integration: **WM-002a** (immediate sibling of WM-002 — the existing path-determinism rule it mirrors). Place under §4.1 "Worktree primitive" directly after WM-002.

---

## WM-002a — Tmux window-name convention

Every agent process that the daemon launches under the tmux substrate (per [process-lifecycle.md §4.4 PL-021b]) MUST occupy a tmux window whose name is derived by the pure function:

    window_name(bead_id, phase, iteration_count) =
        bead_id                                  when phase = "single"
        bead_id + "/i" + dec(iteration_count)    when phase = "implementer"
        bead_id + "/r" + dec(iteration_count)    when phase = "reviewer"

where `bead_id` is the run's bound bead identifier (§WM-001 optional `bead_id` field, present whenever the run was claimed via BI-016), `phase ∈ {single, implementer, reviewer}` is the workflow-graph node category at dispatch time, `iteration_count` is the 1-based review-loop turn (always 1 for `phase = single`), and `dec` is base-10 with no leading zeros.

The tmux session that contains these windows is named per [process-lifecycle.md §4.2 PL-006a] (`harmonik-<project_hash>`) and is referenced as `provenance.TmuxSessionName` at the substrate boundary — window names are scoped within that session and MUST NOT be assumed unique across sessions.

When the daemon runs in the PL-021b `$TMUX`-reuse mode (operator's session, `owns_session=false`), the window name MUST be prefixed with `hk-<hash6>-` where `<hash6>` is the first 6 hex chars of the project hash, yielding e.g. `hk-a1b2c3-<bead_id>` or `hk-a1b2c3-<bead_id>/i2`. The prefix preserves the sweep-sentinel invariant required by PL-021c.

**Replay determinism.** Given identical `(bead_id, phase, iteration_count, project_hash, owns_session)` inputs the function MUST produce a byte-identical window name across daemon restarts, host migrations, and replayed scenario runs. The substrate adapter MUST NOT inject wall-clock components, PIDs, run_ids, or random suffixes into the window name. Collision with an already-existing window of the same name inside the project's tmux session is a fail-fast `WindowNameCollision` error (mapped to PL-006 orphan-sweep coverage — a colliding window from a prior daemon instance is by construction an orphan and is reaped before any new spawn).

**Truncation rule.** tmux imposes no hard window-name length cap, but operator readability and the 80-column status line do. If `len(window_name) > 64` bytes after construction, the adapter MUST truncate `bead_id` (NOT the suffix) by retaining the leading 56 bytes and appending an 8-byte lowercase-hex prefix of `SHA-256(bead_id)`, yielding a name of the form `<bead_id[:56]>~<hash[:8]><suffix>`. The `~` separator is unreserved in tmux window names and unambiguous with `/` from the iteration suffix.

**Cross-references.**
- [process-lifecycle.md §4.4 PL-021b] — pane creation primitive that consumes this name.
- [process-lifecycle.md §4.2 PL-006a] — project-hash session scoping.
- [workspace-model.md §4.1 WM-002] — sibling worktree-path determinism rule whose discipline this clause mirrors.
- [beads-integration.md §4.6 BI-017] — `bead_id` provenance.

**Axes tags:** *mechanism*, *additive*; llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent.
