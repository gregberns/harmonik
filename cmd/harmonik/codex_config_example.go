package main

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

func codexConfigExampleYAML() string {
	return codexConfigExampleBlock
}
