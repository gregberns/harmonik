# Cluster spec-draft index — testing (R-C)

Codename: `2026-07-18-keeper-restart-delivery` · pass 5 (spec-draft)

Index of the normative requirements cluster R-C adds (K6 test coverage). One-line summaries
below; the full normative text lives in the spec-named amendment file pointed to below.

## scenario-harness.md → `05-spec-drafts/scenario-harness-amendment.md` (v0.2.3 → v0.2.4)

### K6 — Keeper session-twin integration tier is outside the SH YAML contract (new §4.14)

- **SH-035** — The keeper's pane/timing/handoff/operator-typing coverage (operator-typing
  collision, late-handoff after the 300s watch, operator-present misread, FORCE-ACT-still-cuts)
  rides the keeper's own session-twin integration tier (`cmd/harmonik-twin-session` + real tmux
  + real injector + real `HANDOFF-<agent>.md`), OUTSIDE the SH YAML contract; the
  `harmonik-twin-claude` wire twin MUST NOT be extended with a tmux-pane/operator-typing surface
  to cover them. Mirrors the spec's existing real-tmux carve-out.
- **SH-036** — The harness MAY add AT MOST ONE wire-observable comms-unreachable-fallback
  scenario, and ONLY IF that fallback emits an assertable bus event the wire twin can observe
  (seed absent → terminal-fallback, never a silent no-op per SK-INV-006; positive control seeds
  present → comms). It MUST NOT be promoted into the §10.1 three-scenario conformance floor; the
  floor is unchanged (any addition remains a foundation amendment).

## Notes

- The two-twins correction is the load-bearing point: four of five failures are invisible to the
  pane-less wire twin, so they ride the keeper's session-twin integration tier instead.
- No prior IDs renumbered or retired; the §10.1 floor is untouched; spec status remains
  `reviewed`.
- Full normative prose, tags, co-references, and revision-history entry: see
  `05-spec-drafts/scenario-harness-amendment.md`.
