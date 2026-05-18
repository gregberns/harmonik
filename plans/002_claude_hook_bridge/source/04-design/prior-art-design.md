# Design pointer — prior-art

This file is a thin pointer. The prior-art research thread informed
architectural choices in the master design but did not require its own
design artifact.

- **Source research:** `../03-research/prior-art/findings.md` (in-repo:
  `internal/handler/adapter_claudecode.go` ClaudeCodeAdapter MVP surface;
  external: Claude Code SDK / agent SDK patterns for hook-driven bridging).

- **Where it landed in the master design** (`claude-hook-bridge-design.md`):
  - §D1 (subcommand vs separate binary — chose `harmonik hook-relay`
    subcommand, informed by adapter pattern already in repo)
  - §D10 (twin parity — twin reuses the same relay subcommand surface)
  - §D13 (responsibility matrix — slots the new pieces alongside the
    existing handler/daemon split)
