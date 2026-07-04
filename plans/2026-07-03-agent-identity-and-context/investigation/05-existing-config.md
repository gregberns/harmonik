# 05 — Existing per-agent configuration surface

Concrete inventory of config primitives that already exist for per-agent/per-role behavior, as of 2026-07-03.

## 1. Crew registry records — `.harmonik/crew/*.json`

**Go struct**: `internal/crew/registry.go:38-46` — `crew.Record`, schema-versioned (`schema_version == 1`).

```go
type Record struct {
    SchemaVersion int       `json:"schema_version"`
    Name          string    `json:"name"`       // stable identity == $HARMONIK_AGENT, comms id, filename
    SessionID     string    `json:"session_id"` // Claude session id (mutated by UpdateSessionID)
    Queue         string    `json:"queue"`      // named queue, == .harmonik/queues/<queue>.json
    Epic          string    `json:"epic"`       // parent epic bead id
    Handle        string    `json:"handle"`     // tmux/substrate handle; pane target = Handle+".0"
    StartedAt     time.Time `json:"started_at"`
}
```

On-disk live records have `epic` **empty** (attribution is on the bead via `--assignee`, not here).

**API** (`registry.go`): `Write` (atomic temp+rename+fsync), `Load`, `List`, `UpdateSessionID`,
`Remove`. Name validation `[a-z0-9-]` 1–64, no `/`/`..`.
**Writers**: `crewstart.go:224-230` sets only Name/SessionID/Queue/StartedAt (Epic/Handle blank).
**Consumers**: orphansweep (session exemption), stategather (`hasCaptainRecord`), quiesce (pane target from `Record.Handle`).

## 2. Mission-handoff files — `.harmonik/crew/missions/<name>.md`

**Format**: Markdown + YAML frontmatter. Contract = `specs/crew-handoff-schema.md` (draft, v1.0.0).
Path `.harmonik/crew/missions/<crew_name>.md`, gitignored.

**Frontmatter — 6 required + 1 optional**:

| Field | Req | Meaning |
|---|---|---|
| `schema_version` | yes | == 1 |
| `crew_name` | yes | == registry Name / `$HARMONIK_AGENT` / comms id |
| `queue` | yes | == registry Queue |
| `epic_id` | yes | assigned epic bead id |
| `goal` | yes | single-line plain-English mission |
| `captain_name` | yes | comms identity crew reports to |
| `model` | **optional** | opus\|sonnet\|haiku; daemon injects `--model` |

Templates: `_TEMPLATE-planner.md` (opus, oversight, empty epic, "never dispatch") vs
`_TEMPLATE-runner.md` (sonnet, epic set, drains queue). Real missions add free-form body:
`## On boot`, `## Goal`, `## Operating loop`, `## Hard bounds`, `## Keeper restart`, `## translations`.

**Convention vs enforced**: mostly convention — crews parse it themselves via `/session-resume`.
The daemon reads **only `model:`** at launch (`crewstart.go:537-556` `readMissionModel`, best-effort).
All other fields are the crew's concern; mission path is paste-seeded (`pasteCrewMission :437`), not validated.

## 3. Agent/role registry concept in Go

- **`crew.Record`** is the closest thing to an agent registry — per-crew identity + queue,
  file-backed. **No "role" enum field.**
- **`AgentType`** (`internal/core/agenttype.go:14-31`) is the harness *conformance class*, NOT a
  crew role: `claude-code`, `pi`, `claude-twin`, `pi-twin`, `codex`. Used in lifecycle events +
  harness adapters (`adapter_{claudecode,codex,pi}.go`).
- **Roles** (captain / crew-runner / planner / watch) are expressed only as *skills* + mission-
  template variants. There is **no Go role-registry / role enum**. Role is data (mission frontmatter
  + which skill loads), not typed code.

## 4. YAML config keys for per-agent/per-role behavior — `.harmonik/config.yaml`

- `daemon.{workflow_mode, remote_control_prefix, max_concurrent}`.
- `watch:` — `status_target`/`opsmonitor_target` (route to `watch` vs `captain` — the wake-economy
  per-role routing knob), thresholds. Daemon refuses boot if absent.
- `keeper:` — per-session context-fill thresholds/timings/budgets (every watched session, not per-named-agent).
- `sentinel:`, `codex.stale_wal_max_bytes`, `sandbox.{backend, harnesses}`.
- **`harnesses.pi.{provider,model,api_key_env,api_key_file}`** — the one truly *per-agent-type*
  config block (resolved by `resolve_pi_config.go`, zero baked defaults, fail-loud). **This is the
  existing precedent for injecting per-agent-type launch config from central YAML.**
- `.harmonik/workers.yaml` = remote-substrate worker registry — per-*machine*, not per-role.

## Summary — what exists to build on

1. **File-backed per-crew record** (`crew.Record`, atomic CRUD) — but Epic/Handle underused, no role field.
2. **A specified mission-handoff schema** (6+1 fields) — but only `model:` enforced in Go; rest is convention.
3. **`AgentType`** typed harness class — only typed agent taxonomy; orthogonal to crew role.
4. **`config.yaml harnesses.pi.*`** = precedent for central-YAML per-agent-type injection; `watch.*_target` = precedent for per-role routing.
5. **No** existing "inject the right data on launch for all agents" surface beyond `--model`,
   harness creds, and the paste-seeded mission file. **That gap is exactly what the operator wants to fill.**

Key files: `internal/crew/registry.go`, `internal/daemon/crewstart.go:224,537-556`,
`internal/core/agenttype.go`, `specs/crew-handoff-schema.md`, `.harmonik/config.yaml`,
`.harmonik/crew/missions/_TEMPLATE-{planner,runner}.md`, `cmd/harmonik/resolve_pi_config.go`.
