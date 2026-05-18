# queue-model — Patterns research

Research-only inventory of existing harmonik patterns relevant to drafting `specs/queue-model.md` (in-daemon queue, waves + streams; v0.1 surface: submit / append / dry-run / status).

## Questions

1. Data-model patterns for structured records (event payloads, intent log, checkpoint trailers).
2. Persistence patterns for daemon-owned state files in `.harmonik/`.
3. Schema-versioning and N-1 compat (ON-018).
4. UUIDv7 / identity field conventions (server-issued vs client-supplied).
5. Validation precedents (rule shape, error codes).

## Findings

### 1. Data-model patterns

The corpus uses pseudo-Pascal `RECORD <Name>:` blocks with column-aligned `field : Type -- comment` lines, paired with `ENUM <Name>:` blocks. `schema_version : Integer` sits at envelope level; comments cite the owning requirement-ID and N-1 target. Optional fields render `T | None`; required are bare types.

- Envelope archetype `RECORD Event`: `specs/event-model.md:649-661`.
- Intent log archetype `RECORD IntentLogEntry` + `ENUM TerminalOp`: `specs/beads-integration.md:625-641`.
- Transition record (text spec): `specs/execution-model.md:104`; persisted sibling file at `.harmonik/transitions/<run_id>/<transition_id>.json` carrying `schema_version` integer that MUST equal the commit's `Harmonik-Schema-Version` trailer: `specs/execution-model.md:365`.
- Per-type payload registries pair concrete YAML next to the RECORD: `specs/event-model.md:730-734`; example `schema_version: <Integer> # MUST equal 1` at `:946`.
- Workspace records `RECORD Workspace` + `RECORD SessionMetadataSidecar`: `specs/workspace-model.md:862, 903`.
- Enum-value declaration is bare identifier list (`claim / close / reopen` at `specs/beads-integration.md:637-641`); inline enum constraints in payload tables use `∈ {APPROVE, REQUEST_CHANGES, BLOCK}` form (`specs/event-model.md:109`).

### 2. Persistence patterns for daemon-owned state files

Process-lifecycle PL §4.4 owns the file-surface inventory: `specs/process-lifecycle.md:208` enumerates `.harmonik/daemon.pid`, `daemon.sock`, `daemon.instance-id`, `daemon.upgrading`, `daemon.state`, events log/spill/HWM, intent-log dir, reconciliation-locks dir, per-run lease lock.

Two main write disciplines:

- **Temp + rename + parent-dir fsync (WM-026 atomic discipline)** — canonical for marker/sidecar/metadata files. Defined at `specs/workspace-model.md:557-562`: (i) write JSON to sibling `<name>.tmp-<pid>`; (ii) `fsync(temp_fd)`; (iii) `rename(2)` (POSIX-atomic); (iv) `fsync(parent_directory_fd)` (REQUIRED; APFS / ext4-data=ordered loss otherwise). Reuses:
  - `daemon.instance-id` minted at PL-005 step 0: `specs/process-lifecycle.md:231`.
  - `daemon.upgrading` (PL-027(iv) before `execve`): `specs/process-lifecycle.md:662`.
  - `daemon.state` (ON-030a, written synchronously on every pause-class transition; read at PL-005 step 8a): `specs/operator-nfr.md:426-432`; read at `specs/process-lifecycle.md:241-244`.
  - Intent-log entries `.harmonik/beads-intents/<key>.json` (BI-030 — temp file with random suffix to avoid concurrent-recovery collision, fsync(temp), rename, fsync(parent_dir) on both create AND delete): `specs/beads-integration.md:463-465`.
  - Workspace sidecars / reviewer archive: `specs/workspace-model.md:582, 621, 661`.
- **JSONL append-only + fsync per durability class (EV-016)** — `specs/event-model.md:433-435`. Suited for events log; not the right pattern for a mutable singleton state file.
- **Pidfile truncate-rewrite-keep-fd** — `specs/process-lifecycle.md:171-177`. Inode must survive (flock association); not applicable to queue.json.
- **HWM piggyback fsync** — `event_id_hwm` updated within the same fsync domain as JSONL boundary write: `specs/event-model.md:307-309`. Pattern: avoid extra fsync by aligning state update to existing durability boundary.

Read-on-startup: PL-005 step 8a reads `daemon.state` + `daemon.upgrading` between reconciliation dispatch (step 8) and ready transition (step 9); corrupt markers treated as absent with structured-log warning. The `daemon.upgrading` marker is `unlink` + parent-dir-fsync on clean transition to `ready`. Source: `specs/process-lifecycle.md:241-244`.

### 3. Schema-versioning and N-1 compat

- **ON-018 normative**: `specs/operator-nfr.md:319-321`. Every versioned on-disk or wire artifact MUST maintain N-1 readability; readers pinned to N-1 MUST parse N's output with additive fields treated as unknown-but-non-fatal. Breaking changes require a migration release (ON-019, `:325-327`) that MUST NOT install mid-run and refuses install unless daemon is `paused`.
- ON-018 enumerates N-1-covered artifacts at `:319` (envelope, payloads, checkpoint trailers + sibling files, queue overlay, policy schema); extqueue plan adds `.harmonik/queue.json` to that enumeration (02-components §6 line 99).
- Concrete `schema_version: Integer ... N-1 readable per [operator-nfr.md §4.5]` phrasing is verbatim across:
  - `specs/event-model.md:540-542` (envelope-level + per-type independent increment).
  - `specs/beads-integration.md:633, 657` (IntentLogEntry).
  - `specs/handler-contract.md:790, 843` (LaunchSpec).
  - `specs/workspace-model.md:862, 903, 932`.
  - `specs/execution-model.md:404, 747, 879, 932`.
  - `specs/control-points.md:358` (policy YAML).
- **Breaking-change classification table** (precedent for queue-rule tightening): `specs/event-model.md:1010` ("Tighten validation (e.g., required length bound) — Yes — Migration release").
- Cross-artifact compat-matrix sensor: ON-INV-001 at `specs/operator-nfr.md:631-635`.

### 4. UUIDv7 / identity field conventions

UUIDv7 is the standard for every minted ID; daemon is sole generation locus.

- **EV-002 — `event_id` MUST be UUIDv7**: `specs/event-model.md:289-291`.
- **EV-002a — monotonic within process**: `specs/event-model.md:295-297`.
- **EV-002c — cross-restart monotonicity via HWM file**: `specs/event-model.md:307-309`.
- Server-issued IDs:
  - `event_id` — daemon emitter.
  - `transition_id` — daemon process, NOT agent subprocesses: `specs/execution-model.md:374`.
  - `daemon_instance_id` — fresh UUIDv7 per process at PL-005 step 0; reuse across exec-replace FORBIDDEN: `specs/process-lifecycle.md:231`.
  - `session_id` — UUIDv7 minted by handler per HC §4.1: `specs/event-model.md:71`.
- `run_id` propagation as `Harmonik-Run-ID` commit trailer + event field: `specs/execution-model.md:213-215` — direct precedent for `queue_id` / `queue_group_index` propagation onto `run_*` event payloads (anticipated by extqueue 02-components §5 final paragraph).
- **No client-supplied ID precedent.** Every daemon-internal ID is daemon-side; `claude_session_id` is the only "external-source" identity and it's an opaque pass-through, not a daemon-issued field (`specs/event-model.md:71`). Implication: `queue_id` should be returned from `queue.submit`, not supplied by the orchestrator.

### 5. Validation precedents

Two shapes in active use:

- **Inline-prose "MUST exclude / MUST reject" with rationale + cross-ref.** BI-013a at `specs/beads-integration.md:262-266`: rule states the exclusion (needs-attention label), names the setter, locates the application point ("at adapter read time"), and notes the inverse-operation effect. No error code; this is filtering, not rejection.
- **Structured-validation contract with typed error + downstream-event prohibition.** The `.harmonik/review.json` validation block at `specs/event-model.md:122` is the closest model: enumerate each field-level rule (`schema_version` MUST equal 1; `verdict` MUST be in `{APPROVE, REQUEST_CHANGES, BLOCK}`; `flags` MUST be String array; `notes` MUST be String) AND state failure routing (emit `review_loop_cycle_complete{completion_reason=error}`, route to needs-attention close; MUST NOT emit downstream event with malformed payload).
- **Read-recovery bullet-list** at `specs/event-model.md:722-728`: torn-tail / mid-file corruption / empty-log / concurrent-tail / post-fsync-tail. Each bullet: condition, emission (`store_divergence_detected{divergence_kind=...}`), reader-action (halt vs proceed). Closest shape to extqueue's submit/append/dry-run validation matrix.
- **Typed error enum**: `BrError` declared at `specs/beads-integration.md:620`; each value mapped to a reconciliation category in the table at `:677-686`. Useful precedent for a `QueueSubmitError` enum.
- **Exit-code taxonomy** lives in ON §8 (`specs/operator-nfr.md:779`); new categories MAY be added within N-1 window as long as existing code-to-category mappings remain stable (`:808`). Queue CLI inherits this taxonomy per extqueue 02-components §4 line 68.

## Patterns to adopt

- **`RECORD Queue / Group / Item` pseudo-Pascal** with `schema_version : Integer` envelope field; ENUM blocks for `GroupKind ∈ {wave, stream}` and `GroupStatus ∈ {pending, active, complete-success, complete-with-failures, paused}`. Optional fields `| None`.
- **WM-026 temp+rename+fsync(temp)+fsync(parent_dir) for `.harmonik/queue.json` writes.** Cite WM-026 directly rather than re-spec. Reads on startup land at PL-005 step 8a alongside `daemon.state` / `daemon.upgrading` — there is existing precedent for adding new markers to that step (PL v0.4.1 added two there).
- **`schema_version : Integer ... N-1 readable per [operator-nfr.md §4.5]`** — verbatim corpus-wide phrasing; ON-018 enumeration extension is explicitly anticipated.
- **Daemon-issued `queue_id` (UUIDv7) returned from `queue.submit`** — matches `daemon_instance_id` and `transition_id` daemon-locality discipline; no client-supplied IDs.
- **Validation contract as bullet list with typed error names** — each rule (existence, status-not-closed, no-cross-group-duplicate, no-foreign-in_progress, parallelism-narrowed-notice) states the check, the failure return, the emitted event where applicable. Mirrors the read-recovery bullet-list shape.

## Risks / conflicts noticed

- **ON-015 reframing is load-bearing.** Current ON-015 text at `specs/operator-nfr.md:300` explicitly says "operator's pending-tasks list and harmonik's dispatchable queue are the same store: Beads". extqueue inverts the second half. queue-model.md MUST coordinate the amendment via the operator-nfr edit, otherwise it contradicts ON-015 as-written.
- **`enqueue` operator-command name reuse.** ON-013a (`specs/operator-nfr.md:273`), ON-041, and PL-003a enumerate `enqueue` as an existing operator command. extqueue 02-components §4 line 67 flags retire-vs-alias for the design pass — naming reservation conflict is real and queue-model must not assume `enqueue` is free.
- **PL-005 step 8a is the read-on-startup landing spot, not step 0.** ON-030a and ON-020a both say "PL-005 step 0" but the actual gate moved to step 8a per PL v0.4.1 (`specs/process-lifecycle.md:1039` row item 6, with the actual step at `:241-244`). Cite step 8a directly.
- **Schema-versioning corpus has ~71 independent per-type contracts** (`specs/event-model.md:998`). queue.json adds one more N-1 contract; breaking-change classification at `:1010` names "tighten validation" as migration-release class — worth flagging.
- **No corpus precedent for "queue.json as a small mutable singleton file."** Existing temp+rename targets are either per-entity (intent log per key, sidecar per session) or singleton markers with trivial payloads (`daemon.state`, `daemon.upgrading`, `daemon.instance-id`). queue.json rewritten on every mutation is novel in surface; WM-026 discipline applies cleanly but write-frequency (every append, every group-advance) exceeds any existing temp+rename target. The HWM piggyback pattern at `specs/event-model.md:307` is the only precedent for amortizing fsync cost; worth a note on whether per-mutation fsync is acceptable or whether write-coalescing is post-MVH future-work.
