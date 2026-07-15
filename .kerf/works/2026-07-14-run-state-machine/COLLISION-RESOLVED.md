# COLLISION RESOLVED — 2026-07-15 (planner)

Resolves `COLLISION-NOTE.md`. **Canonical lineage = RSM** (the combined spec), because it is
what the shipped code, reviews, and `specs/run-state-machine.md` (52 RSM refs) were built
against. M3 landed at `104a9ca7`, reviewed APPROVE.

- **Superseded / archived (do not promote):** Set A = `_complete-set-A-2026-07-14T1539Z/`
  (RX-001..020 + RX-INV-001..005, one `run-state-machine.md`); the RX per-component drafts under
  `05-spec-drafts/_archive-collision/`.
- **RX↔RSM ids are NOT 1:1** — verified against the normative spec (RX-INV-003 ≡ RSM-INV-001;
  RX-020 ≠ RSM-020). Surviving 04-design docs keep RX-* as historical (banner added); they were
  NOT blind-remapped.
- **Disposition:** closed out-of-jig — see `SESSION.md`. `kerf finalize` intentionally NOT run
  (it is a pre-implementation step and would regress the amended normative spec).
