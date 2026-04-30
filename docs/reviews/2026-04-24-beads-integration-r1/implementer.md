# Round 1 Implementer Review — beads-integration.md v0.2.0

## Verdict summary

The spec is **implementable in the large** and broadly exemplary on the structural contract: `br`-only access, terminal-transition-only write surface, the intent-log / audit-log idempotency pattern, and the ownership line between Beads and harmonik are all clearly enough named that a Go module tree falls out more or less directly. An implementer can stand up `internal/beads/adapter.go` and a `br` subprocess driver from this text.

Where the spec falls short is at the **CLI surface itself**. The normative content specifies "route through `br`" and "write only at claim/close/reopen," but never enumerates the actual `br` subcommand inventory, the output-format contract (does the adapter parse `br ready --json`? line-oriented text? a specific JSON envelope?), the exit-code taxonomy, or stderr discipline. The idempotency contract (§4.10.BI-029 through BI-032) is one of the sharper idempotency stories I have seen in this corpus — but it relies on "Beads's audit log" having a queryable idempotency-key-indexed entry, and the spec does not state the `br` command that produces it, nor whether Beads's audit log even accepts caller-supplied idempotency keys. This is the single biggest blocker: the whole restart-recovery contract of §4.10 depends on an external API surface the spec does not pin down.

Other gaps are smaller: the `IntentLogEntry` directory-atomicity rules (create-before-call, delete-on-success) need the same loose-object-style "name the concrete sequence" treatment that execution-model got in R2 for commit atomicity; the Beads-CLI skill's authoritative package path is punted to OQ-BI-002, which is acceptable for MVH but leaves BI-027/BI-028 implementable "by name, not by concrete resolution"; and the store-authority rules in §4.7 are correct but defer the whole reconciliation flow to `reconciliation.md §9.2a` / `9.3` without a short local summary of what "Cat 3 auto-resolver" does to Beads — the implementer of the adapter needs that summary.

Roughly **2 of 8 requirements I walked are fully implementable**, **5 are partially implementable** (need the `br` CLI inventory pinned or an ambiguity resolved), and **1 is blocked** on OQ-BI-002 (the skill-package path). The blocking item is narrow and does not stop the adapter from shipping; it stops the agent-facing skill from resolving against disk.

## Requirements I attempted to implement

### BI-002 — All Beads interactions route through the `br` CLI — IMPLEMENTABLE

```go
// internal/beads/cli.go
type CLI struct {
    binary string            // resolved at startup, path to `br`
    cwd    string            // project root (Beads SQLite lives under here)
    logger *slog.Logger
}

func (c *CLI) exec(ctx context.Context, args ...string) (stdout, stderr []byte, err error) {
    cmd := exec.CommandContext(ctx, c.binary, args...)
    cmd.Dir = c.cwd
    // subprocess only; never link libbeads
    return runAndCapture(cmd)
}
```

The structural rule ("subprocess only; never link libbeads") is concretely expressed and mechanically enforceable via the dependency-manifest test §10.2 names. One grumble: §4.2.BI-002 line 76 says "neither the daemon nor any agent MAY access Beads's SQLite file directly or link against Beads's Rust library" but does not also forbid *reading* the SQLite file read-only via `sqlite3` CLI out-of-band. Arguably a scope question (covered by "every interaction … MUST go through the `br` CLI"), but a reviewer-agent could reasonably split the hair. Cheap fix: add "direct SQLite reads outside `br`" to the prohibition for symmetry.

### BI-004 — Daemon invokes `br` directly; agents via skill — PARTIALLY

```go
// daemon path
claimResult, err := cli.exec(ctx, "claim", beadID, "--idempotency-key", key)

// agent path — the handler injects the Beads-CLI skill into the LaunchSpec
// per handler-contract.md §4.11, and the agent invokes `br` through the skill.
// But WHAT does the skill do that the daemon's direct exec does not?
```

The normative split is clear (daemon direct; agent-via-skill). What is *not* clear is the observable difference. Does the skill wrap `br` to prohibit status-change invocations (the "agents MUST NOT issue terminal-transition `br` writes" rule from §4.9.BI-027)? If so, how — a wrapper binary that filters argv, a permission layer, a documentation-only contract enforced by reviewer-agent? The spec leaves this to the Beads-CLI skill package implementation. That is the right delegation per §4.11, but BI-004 should cite HC-047 or CP-050's provisioning surface more pointedly so the implementer knows where to look for the wrapper-or-documentation decision.

Stuck point: the "MUST NOT bypass the skill" rule is structurally enforceable only if the handler's LaunchSpec does not expose `br` on PATH except via the skill's resolved installation directory. §4.2.BI-002 says agents MUST go through the skill; §4.2.BI-004 restates that — but neither names the mechanism. Implementer must invent (probably: skill-injected `br` lives in a skill-only PATH prefix, and the agent's `PATH` does not otherwise include the system `br`).

### BI-010 — Harmonik writes to Beads only at terminal workflow transitions — IMPLEMENTABLE

```go
type TerminalOp int

const (
    OpClaim TerminalOp = iota
    OpClose
    OpReopen
)

func (a *Adapter) Terminal(ctx context.Context, op TerminalOp, runID, transitionID uuid.UUID, beadID string) error {
    key := fmt.Sprintf("%s:%s:%s", runID, transitionID, opString(op))
    if err := a.writeIntent(key, op, beadID); err != nil {
        return err
    }
    if err := a.cli.execBrWrite(ctx, op, beadID, key); err != nil {
        return fmt.Errorf("br %s failed: %w", opString(op), err)
    }
    return a.deleteIntent(key)
}
```

The enumeration is unambiguous (claim / close / reopen), the triggering events are declared (dispatch, merge-success, failure-or-reopen-verdict), and the cross-refs into [workspace-model §5.8] and [reconciliation §9.5] are clean. The one sharpness missing: does a reconciliation Cat 3c auto-resolver invoke `br close` directly through the adapter, or does it go through a special path? BI-010 line 134 says close is "emitted when a run's workflow reaches a success terminal state AND the merge to the target branch has completed" — a Cat 3c auto-reconcile might trigger a close without either condition holding, because the divergence is "merge already landed, but Beads was not updated." The spec handles this implicitly in BI-022 ("after the investigator's verdict lands OR after a Cat 3c auto-resolver fires") but does not say "Cat 3c's action-mapping writes a close via §4.4," instead citing to `reconciliation.md §9.2a`. Local clarification would save an implementer 20 minutes of cross-spec reading.

### BI-012 — Terminal writes route through the adapter — IMPLEMENTABLE

Trivially implementable as a compiler/linter rule: only one module imports `os/exec` with a `br` argv. Matches test obligation §10.2 line 456. Good.

### BI-013 / BI-014 / BI-015 / BI-016 — Read surface — PARTIALLY

```go
// What I cannot write from the spec alone:
func (a *Adapter) ReadyWork(ctx context.Context) ([]BeadRecord, error) {
    // Which exact command? `br ready`? `br ready --format=json`? `br list --status=open --ready-only`?
    // What is the output shape? A JSON array? NDJSON? Pipe-separated text?
    ...
}
```

Stuck points:

1. **§4.5 names four read queries** (ready-work, dependency-graph, bead-detail, reconciliation-queries) but does not name the `br` subcommand that serves each one. BI-013 parenthetically says "`br ready` (or its equivalent command)" — the "or its equivalent" permits version-drift absorption by the adapter (§4.8), but leaves the MVH target unpinned. The adapter's first release needs one specific CLI contract to target.
2. **Output format is unspecified.** `br` supports JSON output on at least some subcommands based on external evidence (30+ commands per §A.3), but the spec never says "the adapter MUST use `--format=json` output from `br`" or declares the parser's input grammar. Contract tests (§10.2 line 451) are named "against a live `br` binary at the pinned version" but an implementer cannot write the adapter unit tests without a documented output shape.
3. **`br audit-log` is implied but not named.** §4.10.BI-031 says "query Beads's audit log for an entry matching the idempotency key." Which `br` command? What field does it return? What indexes/filters are available? This is where the spec most clearly owes an inventory.

Recommendation: either extend §4.2 with a BI-00N requirement "The adapter MUST consume the `br` commands enumerated in Appendix A.N," or add an inline enumeration table under §4.5 (`br ready`, `br show <id>`, `br edges <id>`, `br audit-log --idempotency-key=<k>`) and let §4.8's adapter absorb any future argv tweaks. The table does not have to be long; 5–8 rows would unblock the adapter.

### BI-017 / BI-018 / BI-019 / BI-020 — Bead-ID propagation — IMPLEMENTABLE

```go
type Run struct {
    // ...
    BeadID *string // unset for non-bead-bound runs
}

// checkpoint trailer emission already handled by execution-model §6.2;
// this spec's role is declaring WHEN the trailer is set:
if run.BeadID != nil {
    trailers["Harmonik-Bead-ID"] = *run.BeadID
}

// event payloads:
evt.Payload["bead_id"] = run.BeadID // nil → omitted
```

The propagation is declaration-shaped: §4.6 binds `bead_id` onto four surfaces (run metadata, checkpoint trailer, event payload, session-log metadata), cross-references the owning specs for each surface, and restates the "iff the run is bead-bound" invariant. This matches the template's scope-discipline rule well. No gaps.

The one thing I'd call out: BI-017 says "the field unset" and event-model says `bead_id?` (optional in NDJSON events); session-log metadata in workspace-model §5.3 uses sidecar file schema. The three surfaces express "absent" differently (null / missing key / absent from sidecar). §6.1 `BeadRecord` says `bead_id : String` (not nullable), which is consistent because `BeadRecord` is the Beads-side record, not the harmonik-side binding. But a reviewer could reasonably ask: should there be an invariant clause like "every harmonik surface binds absent-bead as its idiomatic null — JSON `null` in events, missing key in trailers (handled by EM-018), missing record in session-log sidecars"? Cheap add; not blocking.

### BI-021 / BI-022 / BI-023 — Store-authority rules — PARTIALLY

```go
// BI-021 — Beads wins on content/status within its domain
if cached.Title != fresh.Title {
    cache.Update(fresh) // never push back to Beads
}

// BI-022 — git wins on completion
if bead.Status == "closed" && !gitHasMergeCommit(beadID) {
    reconciliation.Flag(Cat3Evidence{
        BeadID: beadID,
        Evidence: "closed-without-merge-commit",
    })
}
```

The ruleset is correct and maps cleanly to the 3-store model. But two gaps:

1. **"No silent auto-reconcile into git's direction" (line 220) is a hazard rule.** The adapter implementation needs to know WHICH writes are blocked. The text says "Beads status is corrected … only after the investigator's verdict lands or after a Cat 3c auto-resolver fires." Concretely: the adapter exposes `Close(runID, transitionID, beadID)` — can that be invoked from *any* caller, or only from a reconciliation-verdict-executor? BI-022's axis line says `idempotency=non-idempotent`, which is alarming for an otherwise-idempotent adapter. Recommend adding a method-level constraint: "the adapter's write surface is caller-role-checked; the MVH implementation accepts callers (a) the orchestrator's dispatch loop, (b) the reconciliation verdict executor."
2. **BI-023 JSONL-as-observational is clear.** The only twist is reconciliation detectors consume JSONL for divergence evidence (line 227 — `[reconciliation.md §9.3a]`). The rule "JSONL MUST NOT drive a write back to Beads except through the §4.4 write surface triggered by an investigator verdict or a Cat 3 auto-resolver" is good, but invites a structural check: how does the implementer prove it? Probably: the adapter's `Close/Reopen` call path takes a `Source` enum argument (`OrchestratorTerminal` / `ReconciliationVerdict`) that is logged. Cheap add.

### BI-029 / BI-030 / BI-031 — Idempotency contract — IMPLEMENTABLE, with one concrete gap

```go
// BI-029 — key
key := fmt.Sprintf("%s:%s:%s", runID, transitionID, opString(op))

// BI-030 — intent-log fsync before br call
f, err := os.Create(filepath.Join(intentDir, key + ".json"))
if err != nil { return err }
if err := json.NewEncoder(f).Encode(entry); err != nil { return err }
if err := f.Sync(); err != nil { return err }
if err := f.Close(); err != nil { return err }

// fsync the containing directory to make the rename visible post-crash
if dir, err := os.Open(intentDir); err == nil {
    defer dir.Close()
    dir.Sync()
}

// BI-031 — restart recovery
existing := findUnresolvedIntents(intentDir)
for _, intent := range existing {
    auditEntry := cli.queryAudit(intent.IdempotencyKey)  // ??? what command?
    if auditEntry != nil {
        os.Remove(intent.path)
        continue
    }
    cli.execBrWrite(intent.Op, intent.BeadID, intent.IdempotencyKey)
    os.Remove(intent.path)
}
```

The pattern is correct and ergonomic. `BI-030`'s axis line (`idempotency=recoverable-non-idempotent`) is honest. The file-naming (§6.2 `.harmonik/beads-intents/<idempotency_key>.json`) is concrete, and OQ-BI-003 honestly flags the colon-encoding question.

**The concrete gap: BI-031 requires querying Beads's audit log with the idempotency key. Two unanswered sub-questions:**

1. Does the `br` CLI expose an audit-log query filtered by idempotency key? (If not, the recovery logic has to scan the whole audit log for the key, which does not scale.)
2. Does Beads record the idempotency key itself, or does it record some derivative? If the key is a caller-supplied claim/close arg, Beads must persist it verbatim in its audit log for BI-031 to work. The spec leans on "Beads's own idempotency combined with the adapter's key ensures at-most-once effect" (line 283) without saying whether Beads's `claim`/`close`/`reopen` subcommands accept an `--idempotency-key` flag.

Recommend: add a BI-00N requirement of the shape "The adapter MUST supply the idempotency key to the `br` subcommand via a flag whose name and semantics are pinned to Beads version `X.Y.Z` per §4.8.BI-024. The Beads audit log MUST persist the key such that `br audit-log --filter-idempotency-key=<k>` (or equivalent) returns at most one entry per key." If Beads does not currently expose this, OQ-BI-00N is the honest home ("Beads audit-log query shape is not yet enumerated; if absent at the pinned version, the adapter implements a scan").

An alternative if Beads has no key-indexed audit-log query: the intent log itself could carry the pre-invocation timestamp, and the adapter could check Beads's audit-log for a status-change entry within the intent's time window that matches the target bead. This is less clean and has false-positive risk under concurrent writes. The spec should pick.

### BI-032 — Intent log is Cat 3a evidence source — IMPLEMENTABLE (dependency)

Declaration-shaped; the contract is purely "the intent-log shape + durability are owned here, the detector consumes them." Gaps live in reconciliation.md, not here.

## §4.2 — `br` CLI contract

The `br` CLI access surface is declared at the structural level (subprocess only, no `br serve`, daemon-direct vs agent-via-skill) but is **not concretely enumerated at the command level**.

Missing from the spec:

1. **Inventory of subcommands the adapter uses.** §4.5 names four read surfaces (ready-work, graph, detail, reconciliation) but not the `br` commands. §4.4 names three write surfaces (claim, close, reopen) and relies on the reader to infer `br claim`, `br close`, `br reopen`. Implementer convention says YES, but a reviewer-agent will flag "spec permits `br transition --to=closed` as equivalent; which is MVH?"
2. **Error-handling envelope.** When `br` fails (command not found, wrong version, SQLite lock, bead not found, permission denied), what does the adapter see? Exit codes? stderr grammar? A JSON error envelope? The spec is silent. Contrast with handler-contract.md §4.5 which specifies the full NDJSON error shape for handler subprocess failures.
3. **stdin/stdout/stderr discipline.** Does the adapter write anything to `br`'s stdin? Is stdout always parseable output, or does it mix progress with data? Is stderr always diagnostic? Pinning this matters because the adapter's parser is pure: "parse stdout as JSON; stderr is logged verbatim; exit-code 0 is success, nonzero maps to classes in §8." The spec has no §8 (optional) — error taxonomy — for `br`-level failures.
4. **Exit-code taxonomy.** `br`'s exit codes, whatever they are, map onto harmonik's retry policy. Transient (SQLite busy → retry with backoff) vs permanent (bead not found → fail terminally) vs structural (wrong `br` version → halt via Cat 0). The spec says "`br --version` fails within T=5s" is a Cat 0 signal (cited via [reconciliation.md]) but does not enumerate which exit codes the adapter interprets at runtime.

Recommendation: add §4.2a (new requirements BI-004a, BI-004b, BI-004c) with a short CLI-contract block:

- BI-004a: `br` command inventory for MVH — `br ready`, `br show <id>`, `br edges <id>`, `br claim <id> --idempotency-key=<k>`, `br close <id> --idempotency-key=<k>`, `br reopen <id> --idempotency-key=<k>`, `br audit-log [--idempotency-key=<k>] [--bead=<id>]`, `br --version`. (Exact grammar TBD at pinned Beads version per §4.8.)
- BI-004b: Output discipline — all harmonik-consumed `br` subcommands MUST be invoked with a JSON output flag; the adapter MUST parse stdout as a JSON document (object or array) per subcommand; stderr is diagnostic-only.
- BI-004c: Exit-code taxonomy — 0=success, nonzero=failure; the adapter MUST map known failure codes to classes `{Transient, Permanent, Structural}` at the pinned Beads version. Unknown codes default to `Structural`.

Alternative: OQ-BI-004 "`br` CLI contract — output format and exit-code taxonomy pinned at first-release adapter binding." This is a legitimate punt for MVH if Beads-Rust's output is stable enough to discover empirically during bootstrap.

## §4.3 — Beads-managed data — ownership line

BI-005 through BI-009 name what Beads owns:

- Bead content: title, description, type.
- Typed dependency edges: parent-child, blocks, conditional-blocks, waits-for.
- Coarse status: 5-value enum.
- Stable IDs.
- Atomic-claim semantics.

The ownership line is **mechanically auditable**. A grep over the harmonik codebase for any write to `title`, `description`, or `type` on a `BeadRecord`-shaped type that does not go through the `br`-CLI adapter module is a conformance test (§10.2 line 451). Good.

One ambiguity: **§6.1 `BeadRecord` includes an `audit_trail_ref : String` field** described as "opaque handle for `br` audit-log retrieval." The adapter's consumers can plumb this through, but the retrieval mechanism is unspecified (which `br` command dereferences it?). This is symptomatic of the §4.2 CLI-inventory gap; fixing §4.2 fixes this.

Another: the spec does not say whether Beads owns **bead creation** semantics. Who calls `br create`? Harmonik definitely *reads* beads and *transitions* beads, but the ingestion agent (per the user's memory on task-ingestion, which translates kerf/external sources into beads) is the creator. The spec should have a line saying "bead creation is out of scope; the ingestion agent (TBD) MUST use `br create` via the Beads-CLI skill path per §4.9." As-is, BI-010 enumerates three transitions but not "create" — an implementer could misread it as "claim is the first write," omitting the prior creation.

Recommended: add a §2.2 out-of-scope line "Bead creation is owned by the ingestion agent (not declared here); harmonik consumes existing beads" OR add BI-009a declaring create as a fourth agent-driven surface gated by the skill.

## §4.4 — Harmonik write surface — "terminal transitions only"

The three terminal transitions are:

| From → To | Op | Trigger |
|---|---|---|
| `open → in_progress` | claim | daemon dispatches a run against a ready bead |
| `in_progress → closed` | close | workflow terminal success AND merge completed |
| `closed → open` | reopen | failure classification or `reopen-bead` verdict |

The write API shape falls out directly:

```go
type Adapter interface {
    Claim(ctx context.Context, runID, transitionID uuid.UUID, beadID string) error
    Close(ctx context.Context, runID, transitionID uuid.UUID, beadID string) error
    Reopen(ctx context.Context, runID, transitionID uuid.UUID, beadID string) error
}
```

Everything else is a read or a non-write. Very clean.

**Two concerns:**

1. **`tombstone` and `deferred` are in the enum (§4.3.BI-007) but are not in the terminal-transition set.** The spec is silent on who writes these transitions. Presumably the ingestion agent (tombstone) and some operator-facing surface (deferred — per `operator-nfr.md §7.4`?). The spec should name these explicitly as out-of-scope OR declare that harmonik does not emit `tombstone`/`deferred` writes from the orchestrator path.
2. **BI-010's "success terminal state AND merge completed"** — the AND is load-bearing. If workflow succeeds but merge fails (conflict, pre-receive hook rejects), the close MUST NOT fire. This matches execution-model's "tip of task branch = last-durable-state" but the spec would benefit from a line explicitly saying "if the merge step fails, the run's terminal state is NOT success; a subsequent reconciliation may reopen."

## §4.6 — Bead-ID propagation

The propagation is **opaque correlation, not structured**. `BeadID` is declared as `String` (§6.1 — actually `execution-model.md §6.1` defines `BeadID = String` and calls it "opaque stable bead identifier"). Downstream:

- Run.bead_id : String | None
- Harmonik-Bead-ID trailer value : String (conditional presence)
- Event payload.bead_id : String | null
- Session-log metadata bead_id : String | absent

This is the right call: harmonik treats the bead ID as a black box and does not parse structure out of it. One place to tighten: §6.1 `BeadRecord.bead_id : String` — state explicitly that harmonik MUST NOT assume a format (e.g., integer, UUID, prefixed string); any downstream parsing is a contract violation. This matches the "Beads is authoritative" ownership rule but should be named.

The cross-cutting propagation is correctly scoped: this spec owns WHEN, the four target specs own WHAT SHAPE. BI-017 → `execution-model.md §4.3`; BI-018 → `execution-model.md §6.2`; BI-019 → `event-model.md §3.2`; BI-020 → `workspace-model.md §5.3`. All cited, all consistent with what I see in the target specs.

## §4.7 — Store-authority rules

Three-store mapping:

| Store | Authoritative for | Yielding to |
|---|---|---|
| Git | Completion (merge-commit presence) | — |
| Beads | Bead content, coarse status (within its domain) | Git on completion disagreement (BI-022) |
| JSONL | Nothing authoritative; observational only | Both (BI-023) |

The rule set maps cleanly to the 3-store model. Invariant BI-INV-003 ("Git wins on completion disagreement") is the locked decision per the user's memory (state-source-of-truth: "git = completion authority (with reconciliation duty)").

**What I would add:**

1. **Where does "beads-vs-git agree, jsonl-disagrees" resolve?** Implicitly BI-023 says JSONL loses. Explicitly — does this produce a Cat 2 (torn JSONL) flag in reconciliation, or a Cat 3b (audit-lag), or silent drop? The spec cites `[reconciliation.md §9.3a]` but does not summarize.
2. **The inverse of BI-022 (Beads `open` but merge commit exists) is handled at execution-model.md line 569 ("a transition event in JSONL references a checkpoint commit that does not exist in git"). That is the mirror case — git commit absent despite JSONL claim — and is correctly routed to Cat 3. But "Beads `open` AND merge commit exists" is the more interesting case: the close write was never issued, or was rolled back. Does this classify as Cat 3a (torn Beads write) or Cat 3c (lag)? Spec punts to reconciliation.md §9.2a. Again, a local one-liner would help.

## §4.8 — Version-pin + adapter layer

Two normative moves:

1. **Beads version pinned per harmonik release** (BI-024).
2. **Single adapter module owns all `br` translation** (BI-025).

Responsibilities of the adapter:

- Translate typed queries into `br` argv.
- Parse `br` output into typed results.
- Absorb breaking changes in a single code change (not scattered).
- Manage the intent log (§4.10).
- Map exit codes to transient/permanent/structural (MISSING from spec — see §4.2 review above).

This is the shape that localizes breakage. The spec is consistent with the "harmonik absorbs breakage rather than forking Beads" rule (BI-026). Implementable.

**Nitpick:** BI-024 line 234 says "A harmonik release MUST name the Beads version it tested against." Where? A field in `specs/_registry.yaml`? A CI manifest? Recommend adding: "the pinned version MUST appear in the release's `go.mod` indirect dep (via a version-marker comment) and in a conformance test's fixture." Not load-bearing but concrete.

## §4.9 — Beads-CLI skill

BI-027 / BI-028 ride on the handler-contract skill-injection surface (§4.11). I checked the handler-contract spec (§4.11 LaunchSpec carries `required_skills[]` and `skill_search_paths[]`; launch fails on unresolvable skills per HC-048). Mechanically the path is declared:

1. Workflow node declares `required_skills: ["beads-cli"]` per `execution-model.md §4.2.EM-008`.
2. Handler resolves via `skill_search_paths[]` at launch (`handler-contract.md §4.11.HC-047`).
3. Agent subprocess has `br` available in its PATH (as provisioned by the skill).

**What is MISSING at the spec level:**

1. **Skill-package authoritative path** — acknowledged as OQ-BI-002 ("the concrete skill-package file location lands at bootstrap time"). The adapter can name the skill by name (`beads-cli`), but an agent cannot actually invoke `br` until the skill's resolved installation path exists. For MVH this is a bootstrap item; for implementation it is a blocker until resolved.
2. **Skill inventory declaration** — §4.9.BI-027 says the skill MUST document "the `br` command surface, output formats, idiomatic `jq` pipelines, and the harmonik write discipline." This is the documentation contract. But is the skill package's *content* normative (a binary, a shell script, a docs-only bundle)? Handler-contract §4.11 treats skills as capability bundles (file-drop / CLI / MCP registration / docs). The spec's §A.3 rationale explicitly says "the CLI composes naturally with shell plus `jq`" — implying the skill is docs + PATH. Recommend adding: "for MVH, the Beads-CLI skill is a file-drop package containing (a) the pinned `br` binary (or a wrapper that calls the system-installed `br`), (b) a reference doc enumerating the command surface, and (c) a `README` for agents describing the write-discipline restriction."
3. **"Agents MUST NOT issue terminal-transition `br` writes"** — BI-027 line 256 is a documentation contract, not a mechanical one. Mechanically-enforceable version: either the skill provisions `br` via a wrapper that refuses `claim|close|reopen` args, OR the agent's role permission-schema (per `control-points.md §4.6`) rejects those argvs, OR the skill ships a subset `br-reader` binary. Spec should either pick an enforcement model or declare it as reviewer-agent-enforced (honor-system, with reconciliation detector flagging violations).

## §4.10 — `br`-adapter idempotency

The intent-log / audit-log / re-issue dance is the **crispest part of the spec**. The four requirements:

- BI-029: deterministic key `<run_id>:<transition_id>:<op>`.
- BI-030: pre-write fsynced intent file, deleted on success.
- BI-031: restart recovery reads Beads audit log for key; found → delete intent; not found → re-issue.
- BI-032: intent log is Cat 3a's evidence source.

Concrete sketch:

```go
func (a *Adapter) recoverPendingIntents(ctx context.Context) error {
    entries, _ := os.ReadDir(a.intentDir)
    for _, e := range entries {
        intent := a.loadIntent(e.Name())
        landed, err := a.cli.auditCheckKey(ctx, intent.IdempotencyKey)
        if err != nil {
            return fmt.Errorf("audit-log query failed: %w", err)
        }
        if landed {
            os.Remove(filepath.Join(a.intentDir, e.Name()))
            continue
        }
        if err := a.cli.execBrWrite(ctx, intent.Op, intent.BeadID, intent.IdempotencyKey); err != nil {
            // leave intent on disk for next retry
            return err
        }
        os.Remove(filepath.Join(a.intentDir, e.Name()))
    }
    return nil
}
```

**What works:**

- Fsync discipline inherited from event-model §3.4 — cited, consistent.
- Schema version on `IntentLogEntry` (§6.1) with N-1 readability contract (§6.3). Good.
- Directory-as-log pattern is simple and debuggable (post-crash you `ls .harmonik/beads-intents/` and see exactly the ambiguous writes).

**What is missing / weak:**

1. **Audit-log query shape (see §4.10 gap in BI-031 review above).** The whole pattern collapses if `br audit-log --filter-idempotency-key=<k>` doesn't exist or isn't cheap. THIS IS THE SPEC'S BIGGEST GAP.
2. **Directory fsync is not named.** BI-030 says "fsync the file." POSIX semantics require also fsync-ing the parent directory after file creation so the directory entry is durable. Handler-contract's session-log rule (HC-010) typically does this; the event-model's fsync contract (§3.4) should too. Implementer must remember to do this; spec should state it.
3. **Concurrent intents for the same key.** Two callers invoking the same `(run_id, transition_id, op)` (e.g., adapter re-called during a retry storm): the second `os.Create` truncates the first. No data loss because the content is deterministic, but a reviewer would ask whether `O_EXCL` is required. Not load-bearing but worth a sentence.
4. **Intent-file retention on failure vs success.** The spec says "after the `br` call returns success, the adapter MUST delete the intent file" (BI-030). What if the `br` call returns an error? The spec does not say, but the pattern requires the intent file to REMAIN — that's the whole point. Add a normative: "the adapter MUST NOT delete the intent file on any non-success return from `br`; the intent file persists until the next adapter invocation confirms (via audit-log check) that the write has either landed or been re-issued successfully." This is implicit but should be stated.
5. **Intent-log cleanup for long-dead keys.** If a run is canceled mid-flight and never completes, the intent file lingers forever. No hygiene rule. Recommend: BI-0NN "the adapter MAY garbage-collect intent files older than T_intent (default: 24h) after verifying no corresponding audit-log entry exists and the associated run is in a terminal state." Cheap add, prevents inode leak.

## §6 — Schemas

Record shapes are concrete and implementable:

- `BeadRecord` — fields mapped cleanly onto Beads's surface. (Only grumble: `audit_trail_ref` is described as "opaque handle for `br` audit-log retrieval" but never used by any requirement — dead field? If yes, drop. If no, cite the usage.)
- `CoarseStatus` — 5-value enum. Concrete.
- `DependencyEdge` / `EdgeKind` — 4 edge kinds. Concrete.
- `IntentLogEntry` — concrete, schema-versioned, fsynced.
- `TerminalOp` — 3-value enum. Concrete.

The on-disk layout (§6.2) is concrete enough. OQ-BI-003 acknowledges the colon-in-filename portability issue honestly.

§6.4 co-owned event payloads enumerates five events with `bead_id` presence: `run_started`, `run_completed`, `run_failed`, `checkpoint_written`, `store_divergence_detected`. I cross-checked against event-model.md:

- `run_started` — event-model line 78: yes, `bead_id?` in payload. Match.
- `run_completed` — event-model line 80 (§8.1.3): payload includes `bead_id?`. Match. (Line not shown in my grep but inferable from the context.)
- `run_failed` — event-model §8.1.4: match.
- `checkpoint_written` — event-model line 84: `bead_id?`. Match.
- `store_divergence_detected` — event-model line 152: `bead_id?`. Match.

No drift. Good.

**Missing from §6.4:** `session_log_location` event (event-model line 114 — `bead_id?`). Also `divergence_inconclusive` (event-model line 154 — `bead_id?`). Recommend adding these to §6.4 for completeness. §4.6.BI-020 calls out session-log metadata but the event (distinct from the sidecar) should also be listed as co-owned here.

## §7 (Protocols) — absent

No §7. The adapter idempotency dance is arguably a protocol (pre-write intent log → br call → post-write delete → restart recovery). Not strictly required by the template (§7 is optional), but **adding a §7.1 pseudocode block for the terminal-transition-write protocol would help implementers a lot.** Shape:

```
FUNCTION terminal_transition_write(run_id, transition_id, op, bead_id):
    key = derive_key(run_id, transition_id, op)
    intent_path = intent_dir / (key + ".json")

    IF intent_path exists:
        audit_entry = br_audit_log(key)
        IF audit_entry != nil:
            delete intent_path
            return success  // idempotent replay
        // fall through to re-issue

    write_intent(intent_path, {key, op, bead_id, timestamp})
    fsync(intent_path); fsync(intent_dir)

    result = br_<op>(bead_id, --idempotency-key=key)

    IF result.error != nil:
        return result.error  // intent file remains; restart recovery handles

    delete intent_path
    return success
```

This is what the prose at lines 274–283 says but would read more clearly as pseudocode. Execution-model and reconciliation both went this direction in R1/R2 — BI should too.

## §8 (Error taxonomy) — absent

No §8. For a spec whose central operation is an external subprocess invocation that can fail in several distinct ways, this is a notable absence.

The spec relies entirely on reconciliation.md §9.2/§9.3 to classify `br`-level failures. That is fine for the orchestration level but does not help the adapter's internal retry/backoff logic. Consider adding §8 covering:

- `ErrBrUnavailable` — `br` binary not on PATH, or `br --version` fails. Detection: exec error or nonzero exit on `--version`. Route: Cat 0 per reconciliation.md §9.2. Emitted event: `store_access_failed` (if event-model has it; if not, reuse `store_divergence_detected` with inconclusive).
- `ErrBrVersionMismatch` — `br --version` returns an unpinned version. Detection: string compare. Route: halt with structural error.
- `ErrBrSqliteBusy` — exit code N (TBD per Beads). Detection: exit-code match. Route: transient, exponential backoff, max R retries.
- `ErrBrBeadNotFound` — target bead ID unknown to Beads. Detection: exit code + stderr grep. Route: permanent; likely a bug in the caller; escalate.
- `ErrBrAuditQueryFailed` — audit-log query failed. Detection: nonzero exit on `br audit-log`. Route: intent log stays; retry next startup.

Not strictly required (§8 is optional). But the test obligations at §10.2 lines 458 ("crash-injection tests kill the adapter … restart and verify idempotent completion via the audit-log check") assume the taxonomy exists.

## §9 (Cross-references)

§9.1 depends-on list includes `execution-model` and `event-model`. Let me check:

- §4.6.BI-018 cites `execution-model.md §6.2` — `execution-model.md §6.2` (trailer table) is a dep. Correct.
- §4.7.BI-022 cites `execution-model.md §4.4` — correct (git checkpoint trail).
- §4.3.BI-005 / §4.6.BI-017 cite `execution-model.md §4.3` and §6.1 — correct.
- §6.4 events cite `event-model.md §3.2` — correct.
- §4.10.BI-030 cites `event-model.md §3.4` (fsync durability) — correct.

§9.3 co-references include handler-contract, control-points, workspace-model, reconciliation, process-lifecycle, operator-nfr. All are read-only consumptions; none should be in depends-on.

**However:** §4.9.BI-028 says "every agent operating in a harmonik run MUST have the Beads-CLI skill available in its launch context unless a role-specific permission set explicitly excludes it." This is a normative requirement that depends on handler-contract's LaunchSpec.required_skills shape. That feels like a depends-on, not a co-reference. Likewise §4.4.BI-010 depends on workspace-model's "merge to the target branch has completed" surface (§5.8) — normatively, not read-only.

Recommend: promote `handler-contract` and `workspace-model` from §9.3 to §9.1. They are in the dependency edge, even if this spec does not consume their internal types.

## §10 (Conformance)

Test-surface obligations (§10.2) are prose, per the bootstrap rule. The obligations are concrete per requirement group. Crash-injection test for §4.10 is named. Good.

**Missing test obligations:**

- `br` command surface matching the pinned Beads version — only partially covered by "contract tests against a live `br` binary." Recommend adding: "a fixture manifest of expected `br` subcommands and their output grammar, pinned to Beads version, checked into the harmonik repo and verified by CI."
- Role-permission filtering for agents (§4.9.BI-027's "agents MUST NOT issue terminal-transition writes") — there is no conformance test named for this. If the enforcement is honor-system, the conformance test would be a reconciliation-detector-based audit; if it is mechanism-enforced, it is a subprocess-argv filter test.

## Invariants

Four invariants. All correctly scoped:

- BI-INV-001: no intra-run writes to Beads. Cross-checked against BI-011; consistent.
- BI-INV-002: bead-ID stable. Cross-checked against §4.6; consistent.
- BI-INV-003: git wins on completion. Cross-checked against BI-022 and locked decision.
- BI-INV-004: all status writes idempotency-keyed and intent-logged. Cross-checked against BI-029/BI-030.

Invariant-vs-requirement discipline per template §5 selection test: each invariant spans ≥2 subsystems. BI-INV-001 spans harmonik code paths across subsystems. BI-INV-002 spans 4+ surfaces. BI-INV-003 spans git+Beads+reconciliation. BI-INV-004 spans adapter+Cat 3a detector. All pass the selection test.

## Default-baseline Axes application

Scanned all 32 BI-NNN requirements for `Axes:` line presence vs template §4.N+1 rules:

- BI-002, BI-004, BI-009, BI-012 — access/atomicity/write-routing; Axes present. Correct.
- BI-010 — terminal write; Axes present. Correct.
- BI-013, BI-014, BI-015, BI-016 — reads (external I/O); Axes present. Correct.
- BI-021, BI-022 — store-authority; Axes present. BI-022's `idempotency=non-idempotent` is notable (reconciliation-driven corrective writes); correct but worth a reviewer's eye.
- BI-025, BI-029, BI-031 — adapter layer / idempotency; Axes present. Correct.
- BI-030 — `idempotency=recoverable-non-idempotent` — correct classification per template §4.N+1 axis tokens.
- BI-INV-004 — `idempotency=recoverable-non-idempotent`. Correct.

**Missing axes (minor):**

- BI-011 — "no intra-run writes to Beads" — a declaration rule, correctly baseline per template exemption.
- BI-005 through BI-008 — declarations, correctly baseline.
- BI-017 through BI-020 — declarations, correctly baseline.
- BI-023 — JSONL observational — declaration, correctly baseline.
- BI-024, BI-026 — version-pin / no-fork policy — declarations, correctly baseline.
- BI-027 — skill is agent-facing access path — declaration, correctly baseline.
- BI-028 — every agent has skill by default — declaration, correctly baseline.
- BI-032 — intent log as evidence source — declaration, correctly baseline.

I see no strict misses; the declaration-only exemption is applied correctly throughout.

## Under-specified requirements (summary)

Ranked by severity:

1. **§4.2 `br` CLI inventory.** No command surface, no output format, no exit-code taxonomy. BLOCKER for concrete adapter ship; not a blocker for the structural spec. Recommend adding BI-004a/b/c OR adding OQ-BI-004.
2. **§4.10 audit-log query shape.** BI-031 depends on `br audit-log --filter-idempotency-key=<k>` (or equivalent) existing and being cheap. Not confirmed. BLOCKER for restart-recovery semantics.
3. **§4.9 skill-package path (OQ-BI-002).** Agents cannot resolve `br` without it. Non-blocker for adapter; blocker for agent-facing skill resolution.
4. **§4.4 behavior on workflow success + merge failure.** Spec implies the AND blocks the close; a normative sentence would confirm.
5. **§4.9 skill enforcement model.** Is "agents MUST NOT issue terminal writes" mechanism-enforced (wrapper, permission filter) or honor-system? Spec silent.
6. **§6.1 `audit_trail_ref` field usage.** Declared but unused in any requirement.
7. **§4.7 local summary of Cat 3a/3c auto-resolver actions.** Spec punts entirely to reconciliation.md; a one-paragraph summary would save cross-spec jumps.
8. **§4.10 directory-fsync discipline.** Post-file-create directory fsync not named; POSIX-durability concern.
9. **§4.10 intent-file cleanup policy for abandoned runs.** No TTL / GC rule; inode leak over long runtime.
10. **§6.4 co-owned events missing `session_log_location` and `divergence_inconclusive`.** Minor completeness gap.

## Type-vs-reference friction

- `BeadID` — declared in execution-model.md §6.1 as `TYPE BeadID = String`. This spec's §6.1 `BeadRecord.bead_id : String` does not use the typed alias. Minor inconsistency; recommend aligning to `BeadID`.
- `RunID`, `TransitionID` — used in §6.1 `IntentLogEntry` as `UUID`. Execution-model uses `RunID` / `TransitionID` typed aliases. Same inconsistency; align or add "where `UUID` is the concrete backing type for `RunID`/`TransitionID`."
- `Timestamp` — §6.1 `IntentLogEntry.requested_at : Timestamp`. Described in-body as "monotonic; RFC 3339 wall clock." A Timestamp cannot be both monotonic and wall-clock; these are mutually exclusive properties on a single field. Recommend: split into `requested_at_wall : Timestamp` and `requested_at_mono : Integer (nanoseconds)`, or pick one.
- `CoarseStatus`, `EdgeKind`, `TerminalOp` — defined inline here. Canonical-location rule says "a term belongs to the first spec that introduces it." These terms are first-used here, so definition here is correct.

## Protocol / state machine gaps

No §7, so no state machine. Two implicit state machines would benefit from formalization:

1. **Intent-file lifecycle.** States: {not-present, present-pre-call, present-call-in-flight, present-call-returned-error, not-present-success}. Transitions are the adapter's operations. The normative MUST on "delete after success" covers the happy path; the recovery path covers restart-after-crash. Explicit state diagram would help (see §7 recommendation above).

2. **Bead lifecycle from harmonik's perspective.** The 5-value CoarseStatus is a state space; harmonik writes 3 of the transitions (claim/close/reopen); 2 transitions (tombstone/deferred) are out of scope. A one-table state diagram in §7.2 would clarify which transitions are harmonik-driven vs Beads-internal vs operator-agent-driven.

## Affirmations

1. **The "terminal-transition writes only" discipline (§4.4) is a crisp load-bearing decision.** It names exactly three transitions, declares intra-run writes forbidden (BI-011 + BI-INV-001), and ties the discipline to a concrete cost (Beads's `blocked_issues_cache` thrash per §A.3). This is the kind of "forbid broadly, permit narrowly" design that makes conformance testing tractable.

2. **The intent-log + audit-log idempotency pattern (§4.10) is the sharpest idempotency story in the foundation corpus.** It combines (a) deterministic keying, (b) pre-call durable intent, (c) post-call durable audit, (d) restart-recovery reconciliation via both sources, and (e) Cat 3a detector integration. The only gap is the audit-log query shape (handed off to Beads); the harmonik-side contract is complete.

3. **The ownership line in §4.3 is mechanically auditable.** Title, description, type, edges, status — all Beads; harmonik's cache is observational and reconciles one direction only. A grep-level conformance test falls out directly.

4. **The `br`-only access rule combined with the adapter-as-single-module rule (§4.8) localizes breakage perfectly.** Beads's pre-1.0 version churn has exactly one landing zone in harmonik. This matches the "harmonik absorbs breakage rather than forking Beads" invariant.

5. **§6.4 co-owned event payloads discipline is on-model.** Each event says WHEN `bead_id` appears, cross-references event-model for WHAT SHAPE. Matches the template's co-owned pattern exactly.

6. **§9 cross-reference hygiene (modulo the handler-contract / workspace-model promotion recommendation).** Depends-on is minimal; co-references are honest about read-only consumption; bootstrap citation of `problem-space.md` is permitted and correctly flagged.

## Recommendations (ranked)

1. **Add a `br` CLI inventory (§4.2a or Appendix A.N).** Either 5–8 concrete subcommands + output-format flag + exit-code classes, OR an open question pinning the decision to first-release bootstrap. Otherwise the adapter cannot be implemented concretely.

2. **Resolve the audit-log query contract (§4.10 / BI-031).** Either add a BI-NNN requirement naming `br audit-log --filter-idempotency-key=<k>` as a required Beads CLI surface, or add an OQ if Beads does not expose this yet.

3. **Add §7 protocol pseudocode for the terminal-transition write dance.** Makes the intent/audit/delete sequence concrete at implementation level without touching the normative body.

4. **Add §8 failure taxonomy for `br`-level errors.** `ErrBrUnavailable`, `ErrBrSqliteBusy`, `ErrBrBeadNotFound`, `ErrBrAuditQueryFailed`, `ErrBrVersionMismatch`. Each mapped to Cat 0/3a/3b per reconciliation. Not strictly required but unblocks adapter-level retry logic.

5. **Add a §4.9.BI-0NN requirement for the skill enforcement model.** Pick one of: (a) skill ships a `br` wrapper that filters argv, (b) role permission-schema enforces at launch time, (c) honor-system + reconciliation detector flags violations. Either-or is fine; spec must pick.

6. **Promote `handler-contract` and `workspace-model` from §9.3 to §9.1.** BI-028 and BI-010 have hard normative dependencies on those surfaces.

7. **Fix typed-ID usage in §6.1.** `bead_id : String` → `bead_id : BeadID`; `run_id : UUID` → `run_id : RunID`; etc. Consistency with execution-model's typed aliases.

8. **Split `IntentLogEntry.requested_at`** into wall-clock + monotonic, or pick one. The current phrasing conflates incompatible properties.

9. **Add an intent-file cleanup hygiene rule.** TTL-based GC bounded by audit-log check + run terminal state.

10. **State explicitly that `tombstone` and `deferred` transitions are out of harmonik's write surface** (§2.2 or BI-0NN). Prevents implementer confusion about which of the 5 enum values harmonik writes.

11. **Add `session_log_location` and `divergence_inconclusive` to §6.4's co-owned event list.** Minor but completes the table.

12. **Clarify `audit_trail_ref` field in §6.1 `BeadRecord`.** Either declare the consumer (which `br` command dereferences it) or drop the field.

## Implementability score

**7.5 / 10** — implementable in large, with two concrete blockers and one acceptable bootstrap punt.

- Architectural contract: 9/10 (`br`-only, terminal-only writes, adapter-as-module, 3-store authority — all crisp).
- Idempotency contract: 8/10 (pattern is excellent; audit-log query shape is the one structural gap).
- CLI surface: 4/10 (no command inventory, no output format, no exit codes — BLOCKER for concrete adapter ship).
- Schema hygiene: 8/10 (records concrete; typed-alias consistency and `audit_trail_ref` cleanup needed).
- Cross-reference accuracy: 8/10 (small promotions needed from §9.3 to §9.1).
- Test obligations: 7/10 (per-group prose is good; skill-enforcement and `br`-fixture-manifest tests missing).
- Open-question honesty: 9/10 (OQ-BI-001/002/003 correctly flag the three deferred items).

A 1–2 revision cycle tightening the §4.2 CLI surface + the §4.10 audit-log query contract + a §7 protocol pseudocode block would move the score to 9.0+ and clear the path for an adapter prototype.
