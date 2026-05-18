# Design pointer — counter-evidence

This file is a thin pointer. Phase-2 counter-evidence shaped which paths the
master design ruled out; the resolutions are recorded inline in the master.

- **Source research:** `../03-research/counter-evidence/findings.md`
  (counter-evidence against the kickoff constraints — most notably the
  `stream-json + --include-hook-events` competing architecture, and
  alternative single-binary vs subcommand framings).

- **Where it landed in the master design** (`claude-hook-bridge-design.md`):
  - §D1 (subcommand chosen over separate binary — explicit counter
    consideration)
  - §11 of the spec draft (Informative — alternative architecture
    (post-MVH)) preserves the stream-json path as a documented
    not-taken option
  - §D13 (matrix lock-in after ruling out the alternatives)
