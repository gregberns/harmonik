# harmonik CLI

Agent-driven bead execution daemon and workflow tooling.

## Subcommands

### `harmonik run`

Execute one or more beads and exit on completion.

```
harmonik run <bead-id>
harmonik run --beads hk-abc,hk-def --max-concurrent 2
```

### `harmonik graph validate`

Validate a workflow DOT file against EM-038 pre-run checks.  
No daemon required. Reads the file directly.

```
harmonik graph validate <path>
harmonik graph validate --json <path>
```

**Exit codes:**
- `0` — file is valid (no diagnostics)
- `1` — one or more validation diagnostics found
- `2` — usage error (missing path, bad flags)

**Flags:**
- `--json` — emit diagnostics as a JSON array instead of plain text

**Example (plain text):**
```
$ harmonik graph validate workflow.dot
workflow.dot: 2 diagnostic(s)
  [em038_missing_start_node_id] workflow must declare start_node_id
  [em038_missing_terminal_node_ids] workflow must declare a non-empty terminal_node_ids list
```

**Example (JSON):**
```
$ harmonik graph validate --json workflow.dot
[
  {
    "code": "em038_missing_start_node_id",
    "detail": "workflow must declare start_node_id"
  }
]
```

**Note:** Reference resolution (handler_ref, policy_ref, etc.) is skipped in standalone
validation because no registry is available. Sub-workflow refs will also fail to resolve.
Structural and attribute checks (node types, idempotency_class, axis values, reachability,
cycle bounds) are fully enforced.

### `harmonik handler`

Inspect or resume a paused handler. No daemon required.

### `harmonik queue`

Submit or inspect the bead queue. The daemon must be running.

### `harmonik reconcile`

Close `in_progress` beads whose implementation has already merged to the target branch.

### `harmonik beads-merge`

Git merge-driver for `.beads/issues.jsonl` (union-by-bead-ID, registered via `.gitattributes`).

### `harmonik tmux-start`

Bootstrap a tmux session and start the daemon inside it.

### `harmonik hook-relay`

Forward a Claude hook event to the daemon (internal use by Claude Code hook configs).
