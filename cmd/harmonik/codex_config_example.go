package main

// codex_config_example.go — the SINGLE source-of-truth `codex:` config template
// folded into the config.yaml `harmonik init` generates (hk-yhvrh).
//
// # Why this exists
//
// The codex launch path reads ONE key out of .harmonik/config.yaml —
// codex.stale_wal_max_bytes — and that key is REQUIRED with NO compiled default
// (internal/daemon/codexwalguard.go, the same fail-loud / zero-hardcoded-thresholds
// mandate the governor and keeper follow). Before this file existed, `harmonik init`
// wrote a config.yaml with keeper: and harnesses.pi: blocks but NO codex: block at
// all, so the FIRST time a freshly-initialised project selected the codex harness
// the implement node died ~1s in at spec-build time with:
//
//	dot: agentic node "implement" failed: build launch spec for node "implement":
//	daemon: buildCodexRoutedLaunchSpec: harness.LaunchSpec: daemon: codex stale-WAL
//	guard: required key `codex.stale_wal_max_bytes` is not set; ... there is no
//	compiled default
//
// That is a structural precondition of routing any work to codex: a labels-only
// ramp into a fresh project hits this wall BEFORE the label can route anything, and
// it presents as an implement-node failure that is easy to misattribute to codex
// itself. Scaffolding the block at init time removes the wall without touching the
// guard's fail-loud behavior (adding a compiled default there would violate the
// no-hardcoded-defaults mandate).
//
// # One source of truth
//
// codexConfigExampleBlock is the ONLY place this YAML fragment is written, exactly
// as keeperConfigExampleBlock (keeper_config_example.go) and piConfigExampleYAML
// (resolve_pi_config.go) are for their blocks. init_cmd.go's writeConfigYAML
// composes it in rather than inlining a literal, so the two cannot drift.
//
// LOAD-BEARING round-trip invariant (asserted in
// init_codex_block_hkyhvrh_test.go): the config.yaml the REAL writeConfigYAML emits,
// fed to the REAL guard (reached via daemon.CodexHarness.LaunchSpec), must NOT
// produce *daemon.ErrMissingCodexStaleWALMaxBytes. If a new REQUIRED-with-no-default
// key is added under `codex:` in internal/daemon, you MUST add a line here.
//
// Bead ref: hk-yhvrh.

// codexConfigExampleBlock is the complete, commented top-level `codex:` block —
// every operator-required key with a suggested starting value. It is a standalone
// YAML fragment (the `codex:` mapping) so it can be embedded under schema_version: 1
// in .harmonik/config.yaml. Indentation is two-space, matching the rest of
// config.yaml.
//
// The suggested value is a STARTING POINT the operator owns — NOT a runtime default.
// Emitting it here is allowed for the same reason keeper's is: this is a template the
// operator copies and edits, not a compiled fallback.
const codexConfigExampleBlock = `codex:
  # codex.stale_wal_max_bytes — REQUIRED. There is NO compiled default: if this key
  # is absent, EVERY codex launch fails loud at spec-build time
  # ("required key ` + "`codex.stale_wal_max_bytes`" + ` is not set"), so init emits it uncommented.
  #
  # What it does NOT do: it is NOT a cleanup gate. The per-launch stale-WAL guard
  # cleans every present, unheld $CODEX_HOME/state_*.sqlite-wal REGARDLESS of size —
  # staleness is a function of being left behind by a killed run, not of size
  # (a 234 KB stale WAL fast-fails codex just as hard as a large one).
  #
  # What it DOES do: it is a SECONDARY LOGGING SIGNAL that classifies the cleanup
  # log line. A cleaned WAL larger than this many bytes logs as
  # codex_wal_guard_removed_large_stale (warn); anything smaller logs as
  # codex_wal_guard_removed_stale (info). Tuning it changes log severity, not
  # behavior. 1 MiB is the suggested starting point.
  stale_wal_max_bytes: 1048576
`

// codexConfigExampleYAML returns the complete codex: example block. It is the single
// source of truth for the block `harmonik init` writes into a generated
// .harmonik/config.yaml, so the template and the required-key set cannot drift.
func codexConfigExampleYAML() string {
	return codexConfigExampleBlock
}
