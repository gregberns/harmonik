# Spec-draft pointer — counter-evidence

The counter-evidence thread didn't produce new normative requirements; it
forced the spec to document the not-taken alternative explicitly. This file
is a thin pointer.

- **Master draft:** `./claude-hook-bridge.md`
- **Section in the master draft where this thread landed:**
  - §11 — Informative — alternative architecture (post-MVH). Preserves the
    `stream-json + --include-hook-events` competing architecture as a
    documented option that may be reconsidered after MVH.
  - §12 — Open questions (OQ-CHB-001..003) carry the residual uncertainty
    surfaced by counter-evidence.

Requirements influenced by this thread (rule-outs, not adds):

- **CHB-010** — Subcommand surface (chosen over stream-json bridging).
- **CHB-INV-003** — Mechanism, no cognition (counter-evidence pushed toward
  a thinner, dumber bridge).
