# Subsystem Organization

> How harmonik's Go source tree is shaped, where each subsystem lives, and how cross-subsystem imports are mechanically constrained. Scope: single-binary daemon MVH per `process-lifecycle.md §8.6` and `architecture.md §1.4a` ("a subsystem is a Go package inside the daemon process").

## Decisions

1. **Single Go module, single binary** — module path `github.com/gregberns/harmonik`, one `go.mod`, one primary binary. Matches foundation §1.4a; no multi-repo/multi-module cost on a solo-dev codebase.
2. **Subsystems live under `internal/`** — every S0x package is under `internal/` so nothing outside the module can import it. Public surface is CLI, not library.
3. **Shared types in `internal/core`** — `run_id`, `state_id`, `transition`, `outcome`, `event` types (execution-model §2.1, event-model §3.2) live in one leaf package with **no imports** from any subsystem.
4. **Dependency layering enforced by `depguard` v2** (single tool for both lint rules and component-graph enforcement). Component graph lives in `.golangci.yml` under `linters-settings.depguard`; no separate architecture-tool binary. (Rationale in §Dependency layering.)
5. **Handler implementations are subsystems too** — `internal/handler/claudecode`, `internal/handler/pi`, `internal/handler/twin` each declare a subsystem envelope per §1.4a.
6. **External adapters are packages, not subsystems** — `internal/adapter/br` (Beads CLI), `internal/adapter/ntm` — thin shells per foundation boundary rules; do not declare envelopes.
7. **`pkg/` is deliberately unused at MVH** — no public library surface; prevents accidental API-stability obligations. Revisit only if an external Go consumer materializes.
8. **Go 1.25 toolchain pinned via `go.mod`** — `toolchain go1.25.x`. Enables `log/slog`, `testing/synctest`, `os.Root`, `go.mod tool` directives. See `quality-checks.md` for the ⚑ on this assumption.
9. **No `go.work` file.** Single module; no multi-module workspace. Keeps tooling expectations simple and avoids editor/LSP variance.
10. **Dev tool dependencies managed via Go 1.24+ `go.mod tool` directive.** No `tools.go` pattern. `gofumpt`, `gci`, `golangci-lint`, etc., are declared as `tool` requirements in `go.mod` and resolved with `go tool <name>`.

## Go module layout

```
harmonik/
  go.mod                          # module github.com/gregberns/harmonik; toolchain go1.25.x; tool directives for dev tools
  .golangci.yml                   # linters + depguard v2 component-graph rules (layering enforcement)
  cmd/
    harmonik/                     # daemon + subcommands (daemon, attach, runner)
      main.go
    harmonik-twin-generic/        # generic NDJSON back-half test handler (renamed per hk-w5vra.1)
    harmonik-twin-claude/         # Claude-lifecycle twin (hk-w5vra.2, not yet built)
    harmonik-twin-pi/
  internal/
    core/                         # shared types — NO imports from subsystems
      ids.go                      # run_id, state_id, transition_id, bead_id
      event.go                    # event envelope + typed taxonomy
      outcome.go                  # outcome, transition, checkpoint types
      tags.go                     # four-axis + mechanism/cognition tag types
    orchestrator/                 # S01
    policy/                       # S02
    eventbus/                     # S03
    agentrunner/                  # S04
    hook/                         # S05
    workspace/                    # S06
    scenario/                     # S07 (post-MVH, stub OK at bootstrap)
    memory/                       # S08
    improvement/                  # S09 (post-MVH)
    handler/                      # handler-contract §4 implementations
      contract/                   # the Handler interface + LaunchSpec types
      claudecode/
      pi/
      twin/
    adapter/                      # external-process shells
      br/                         # Beads CLI adapter (beads-integration §10.8)
      ntm/                        # ntm subprocess adapter (process-lifecycle §8.7)
    daemon/                       # composition root — wires all subsystems
      daemon.go
  specs/                          # normative specs (from kerf finalize)
  docs/                           # knowledge base
```

## Package-per-subsystem mapping

| ID | Subsystem | Package path |
|---|---|---|
| S01 | Orchestrator Core | `internal/orchestrator` |
| S02 | Policy Engine | `internal/policy` |
| S03 | Event Bus | `internal/eventbus` |
| S04 | Agent Runner | `internal/agentrunner` |
| S05 | Hook System | `internal/hook` |
| S06 | Workspace Manager | `internal/workspace` |
| S07 | Scenario Harness | `internal/scenario` |
| S08 | Memory Layer | `internal/memory` |
| S09 | Improvement Loop | `internal/improvement` |
| — | Handler contract (shared) | `internal/handler/contract` |
| — | Handlers (per agent type) | `internal/handler/{claudecode,pi,twin}` |
| — | Composition root | `internal/daemon` |

A subsystem MAY split into sub-packages (`internal/orchestrator/runner`, `internal/orchestrator/router`); `depguard` rules target the subsystem root and its subtree (`internal/orchestrator/...`).

## Shared types — where and why

`internal/core` holds every type that crosses a subsystem boundary: identity types (`RunID`, `StateID`, `TransitionID`, `BeadID`), the event envelope + typed taxonomy (event-model §3.2), `Outcome`/`Transition`/`Checkpoint` (execution-model §2.1), and the four-axis/ZFC tag types (architecture §1.1, §1.2). **Invariant:** `internal/core` imports nothing from `internal/*` subsystems — only stdlib and a narrow allowlist (`github.com/google/uuid` etc.), enforced by an empty `mayDependOn` for `core`. This prevents the common "shared types drift into subsystem-specific types" failure. Non-shared types stay in their owning subsystem per the envelope discipline (architecture §1.4): a type leaves its home package only if it appears in a cross-subsystem event payload or shared-state contract.

## Dependency layering enforcement

**Tool chosen: `depguard` v2** (bundled with `golangci-lint`). One tool for both lint rules and the component-graph enforcement — no separate architecture-linter binary, no second config file, no second failure-message shape.

**Why over the prior `go-arch-lint` pick.** `depguard` v2 supports component-style rules natively: each rule is scoped to a set of source files (the component) and declares allow/deny lists against other import paths (the edges). Two things changed the calculus vs the earlier recommendation:

1. `depguard` v2's file-scoped rules express the same "component graph" idea `go-arch-lint` modeled, without the second binary.
2. Dropping `go-arch-lint` removes a **single-maintainer-tool risk** (one upstream maintainer, niche adoption). `depguard` ships inside `golangci-lint` — the healthiest meta-linter in the Go ecosystem.

`internal/` alone remains insufficient (it stops external imports but permits any internal-to-internal edge); the component graph below is what prevents forbidden cross-subsystem imports.

**Config shape** (excerpt from `.golangci.yml`; full file in `quality-checks.md`):

```yaml
# .golangci.yml → linters-settings.depguard
linters-settings:
  depguard:
    rules:
      # Leaf: internal/core must not import any sibling subsystem.
      core:
        files: ["**/internal/core/**"]
        deny:
          - { pkg: "github.com/gregberns/harmonik/internal/", desc: "core is a leaf; no subsystem imports" }

      # eventbus / policy / handler-contract: may import core only.
      eventbus:
        files: ["**/internal/eventbus/**"]
        allow: ["$gostd", "github.com/gregberns/harmonik/internal/core"]
      policy:
        files: ["**/internal/policy/**"]
        allow: ["$gostd", "github.com/gregberns/harmonik/internal/core"]
      handler-contract:
        files: ["**/internal/handler/contract/**"]
        allow: ["$gostd", "github.com/gregberns/harmonik/internal/core"]

      # Adapters: core only.
      adapter-br:
        files: ["**/internal/adapter/br/**"]
        allow: ["$gostd", "github.com/gregberns/harmonik/internal/core"]
      adapter-ntm:
        files: ["**/internal/adapter/ntm/**"]
        allow: ["$gostd", "github.com/gregberns/harmonik/internal/core"]

      # Workspace: core + eventbus + adapter-br + uuid.
      # uuid added: workspace parses RunID/WorkflowID strings from JSON directly;
      # uuid is already in core's allow-list (leaf utility, no harmonik-internal deps).
      workspace:
        files: ["**/internal/workspace/**"]
        allow:
          - "$gostd"
          - "github.com/gregberns/harmonik/internal/core"
          - "github.com/gregberns/harmonik/internal/eventbus"
          - "github.com/gregberns/harmonik/internal/adapter/br"
          - "github.com/google/uuid"

      # Agent runner: core + eventbus + handler contract + adapter-ntm.
      agentrunner:
        files: ["**/internal/agentrunner/**"]
        allow:
          - "$gostd"
          - "github.com/gregberns/harmonik/internal/core"
          - "github.com/gregberns/harmonik/internal/eventbus"
          - "github.com/gregberns/harmonik/internal/handler/contract"
          - "github.com/gregberns/harmonik/internal/adapter/ntm"

      # Orchestrator: everything listed above except daemon/cmd.
      orchestrator:
        files: ["**/internal/orchestrator/**"]
        allow:
          - "$gostd"
          - "github.com/gregberns/harmonik/internal/core"
          - "github.com/gregberns/harmonik/internal/eventbus"
          - "github.com/gregberns/harmonik/internal/policy"
          - "github.com/gregberns/harmonik/internal/handler/contract"
          - "github.com/gregberns/harmonik/internal/workspace"
          - "github.com/gregberns/harmonik/internal/hook"
          - "github.com/gregberns/harmonik/internal/adapter/br"

      # Hook / memory / scenario / improvement / handlers — one rule each, same shape.
      # See full matrix in §Package-per-subsystem mapping.

      # Composition root: may import every subsystem.
      daemon:
        files: ["**/internal/daemon/**"]
        allow:
          - "$gostd"
          - "github.com/gregberns/harmonik/internal/..."

      # cmd: daemon + core only.
      cmd:
        files: ["**/cmd/**"]
        allow:
          - "$gostd"
          - "github.com/gregberns/harmonik/internal/core"
          - "github.com/gregberns/harmonik/internal/daemon"
```

**Allowed-edge matrix** (the source of truth that the rules above encode — identical to the prior `go-arch-lint` graph):

| From ↓ / May import → | core | eventbus | policy | handler-contract | adapter-br | adapter-ntm | workspace | hook | agentrunner | orchestrator | memory | scenario | improvement | handlers | daemon |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| core         | — | | | | | | | | | | | | | | |
| eventbus     | ✓ | — | | | | | | | | | | | | | |
| policy       | ✓ | | — | | | | | | | | | | | | |
| handler-ctr  | ✓ | | | — | | | | | | | | | | | |
| adapter-br   | ✓ | | | | — | | | | | | | | | | |
| adapter-ntm  | ✓ | | | | | — | | | | | | | | | |
| workspace    | ✓ | ✓ | | | ✓ | | — | | | | | | | | |
| hook         | ✓ | ✓ | | | | | | — | | | | | | | |
| agentrunner  | ✓ | ✓ | | ✓ | | ✓ | | | — | | | | | | |
| orchestrator | ✓ | ✓ | ✓ | ✓ | ✓ | | ✓ | ✓ | | — | | | | | |
| memory       | ✓ | ✓ | | | | | | | | | — | | | | |
| scenario     | ✓ | ✓ | | | | | | | ✓ | ✓ | | — | | | |
| improvement  | ✓ | ✓ | | | | | | | | | ✓ | | — | | |
| handlers     | ✓ | | | ✓ | | | | | | | | | | — | |
| daemon       | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — |
| cmd          | ✓ | | | | | | | | | | | | | | ✓ |

**Example violation.** Agent adds `import ".../internal/workspace"` to `internal/eventbus/bus.go`. `golangci-lint run` fails via the `eventbus` depguard rule with a message naming the offending file, the denied import, and the `desc` from config. **CI wiring:** one `make lint` target running `go vet` + `golangci-lint run` (which now includes the component-graph check); required status check. The component graph mirrors the intra-foundation dependency graph in `components.md` — spec and code structure stay aligned.

## ⚑ Assumptions worth user's eye

1. **⚑ Module path** — `github.com/gregberns/harmonik` assumed from the repo URL and git user. Confirm before first `go mod init`.
2. **⚑ `pkg/` empty at MVH** — the standard Go-community debate. Closing it off prevents an agent inventing a "public API" nobody asked for. Revisit if cross-project reuse appears.
3. **⚑ `internal/core` as single shared package** — an alternative is finer-grained shared packages (`internal/ids`, `internal/events`, `internal/outcome`). One-package is simpler; subdividing is a refactor, not a design change. Flagging because the choice is load-bearing.
4. **⚑ `internal/daemon` as composition root** — all wiring (DI, startup, socket listener, shutdown) lives here so subsystems stay mutually unaware. The daemon package is the only one allowed to import most subsystems. Standard Go-app pattern but worth naming.
5. **⚑ Handlers live under `internal/handler/*`, not under `internal/agentrunner/*`** — handlers implement a contract the runner consumes, but they are their own subsystem-envelope-declaring packages per §1.4a. Co-locating with the runner would invert the dependency.

## Deferred / follow-up

- Sub-package structure inside each subsystem (e.g., `orchestrator/runner`, `orchestrator/router`) — owned by each subsystem's own spec work.
- Test-package conventions (`_test` packages, integration-test placement) — captured in `docs/methodology/TESTING.md`; verify alignment when tests land.
- Vendor policy, module proxy, GOPROXY settings — bootstrap-time concern.
- Versioning strategy for the `core` type set across N-1 compatibility (operator-nfr §7.5) — belongs in its own spec work once event-schema evolution is exercised.
- Scenario harness (`internal/scenario`) may need relaxed rules to drive the full stack; reconsider its `mayDependOn` set when the harness spec is written.
