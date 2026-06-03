# Harmonik

> ⚠️ **SAFETY — read before pointing the daemon at any repo.** The daemon currently **merges and pushes to `origin/main` on every successful bead** — the merge path (`internal/daemon/workloop.go` `mergeRunBranchToMain`) hardcodes `main` and has **no fail-closed guard**. Targeting an integration branch / protecting `main` is **not yet implemented** (tracked under `codename:productization`, gate bead `hk-6r6xv`). **Do NOT run this against a work repo or any branch that must not be auto-committed until that lands.** Personal/throwaway repos where auto-pushing `main` is acceptable are fine.

## Milestones

- **2026-05-14 — Phase 1 OPERATIONAL GREEN**: harmonik runs claude end-to-end on a bead with zero human input (smoke v13).
