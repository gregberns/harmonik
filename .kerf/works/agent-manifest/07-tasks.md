# 07 — Tasks

Ordered decomposition of the `agent-manifest` SPEC into an epic + independently-mergeable child
tasks. Grouped by the build/land order in `06-integration.md`. Component refs (C-A…C-I) point at
`03-components.md`; section refs point at `SPEC.md`.

**Epic:** agent-manifest — configurable agent types (manifest + brief command + instruction sets).

The load-bearing interface is the boot-document ORDER (SPEC §4) and the provenance rule I1 (SOUL
always from `soul.md`, handoff last + episodic-only). Emit-only, no side effects (I2).

---

## Group 1 — Type registry + loader (C-A)

### T1 — Type-folder layout + `manifest.yaml` schema + Go loader
- **Goal:** New `internal/agentmanifest/` package parses `.harmonik/agents/<type>/manifest.yaml` +
  static files (`soul.md`, `operating.md`) into a typed struct; loader resolves a type folder by name.
- **Touches:** new `internal/agentmanifest/` (loader + struct); new tree `.harmonik/agents/`;
  schema per SPEC §2 (type, cardinality, harness, identity, context[], triggers[], handoff, keeper,
  lifecycle, markers). Parallels `internal/crew/registry.go:38`.
- **Accept:** a `crew/manifest.yaml` + `soul.md` + `operating.md` parse into the struct; missing/
  malformed fields are detected (surfaced to C-C); loader resolves a type by name; `_skills/` refs
  in `context[]` resolve. Leading-underscore prefix reserved (no type named `_skills`).
- **Deps:** none.

### T2 — `crew.Record.type` field + instance→type resolution
- **Goal:** Extend `crew.Record` with a `type` field (default/legacy `crew`); resolver maps an
  instance name (e.g. `leto`) → its type folder (`crew/`); a bare type name resolves to itself.
- **Touches:** `internal/crew/registry.go:38` (add `type` field + back-compat read); the
  agentmanifest name resolver.
- **Accept:** `leto` → `crew`; a record with no `type` reads as `crew`; a bare type name resolves to
  itself; existing registry files load without migration errors.
- **Deps:** T1.

---

## Group 2 — Boot command + document order (C-B + C-F)

### T3 — `harmonik agent brief` command + boot-document ORDER (emit-only)
- **Goal:** New `harmonik agent` verb group; `brief` resolves agent→type→manifest and emits the
  ordered Markdown boot document (SPEC §4 order: 1 SOUL, 2 wake-reason, 3 operating+skills, 4
  triggers, 5 handoff). NO side effects (I2).
- **Touches:** new `cmd/harmonik/agent*.go` (sibling to `start.go`/`crew.go`); reads T1 loader;
  name resolution: `--agent` optional → `$HARMONIK_AGENT` fallback → conflict is a HARD ERROR unless
  `--override`; `--format markdown|json|yaml|toon` (default markdown). Skills render as short-desc +
  pointer, never inline bodies.
- **Accept:** `agent brief --agent leto` prints full ordered doc; `$HARMONIK_AGENT=leto` no-flag =
  identical; conflicting `--agent`≠`$HARMONIK_AGENT` errors unless `--override`; `--format json`
  same content structured; SOUL byte-identical to `soul.md`; handoff last + no identity re-statement;
  zero filesystem mutation.
- **Deps:** T1, T2.

---

## Group 3 — Proving ground: crew instruction set (C-G crew)

### T4 — Author the `crew` type folder (proving ground)
- **Goal:** Author `.harmonik/agents/crew/` (`manifest.yaml`, `soul.md` ≤25L, `operating.md` ≤45L)
  from the drafts + scattered sources; prove `brief` emits a working crew boot doc.
- **Touches:** `.harmonik/agents/crew/*` (distill from `crew-launch/SKILL.md`,
  `missions/_TEMPLATE-runner.md`, `leto.md`, `orchestrator-rules`); source: drafts/agents/crew.
- **Accept:** folder passes C-C check; `agent brief --agent crew` yields a boot doc a crew can boot
  from; authoring limits honored (soul ≤25L, operating ≤45L).
- **Deps:** T3.

---

## Group 4 — Schema-check verb (C-C)

### T5 — `harmonik agent check` schema-check verb
- **Goal:** First-class verb validating a type folder: manifest fields present + typed, referenced
  files exist, `context[].ref` resolve (incl. `_skills/`), boot-doc order buildable.
- **Touches:** shares `internal/agentmanifest/` validation; new `cmd/harmonik/agent` subcommand;
  optional thin `--validate`/`--dry-run` alias on `brief`.
- **Accept:** well-formed folder → exit 0 + "ok"; missing `soul.md` ref / unknown `context.ref` →
  non-zero exit naming the specific defect.
- **Deps:** T1.

---

## Group 5 — Scoped skills model (C-D)

### T6 — Per-agent scoped skills + shared `_skills/` dir + disable global autoload
- **Goal:** Harmonik-supplied skills injected ONLY into agents whose manifest requests them; shared
  skills live once in `.harmonik/agents/_skills/`, per-type skills in the type folder; global skill
  autoload disabled.
- **Touches:** `.harmonik/agents/_skills/`; `manifest.context[].ref` resolution (T1);
  `.claude/settings.json` disable autoload (embedded-asset re-sync gotcha — `cp` to embedded copy).
- **Accept:** an agent requesting skills X,Y gets exactly X,Y (short-desc+pointer) and nothing else;
  a shared skill referenced by two types is stored once; a fresh agent pane shows no un-requested
  skills.
- **Deps:** T1.

---

## Group 6 — The one boot skill (C-E)

### T7 — The ONE boot skill
- **Goal:** A single thin boot skill whose only job is: call `harmonik agent brief` with the agent
  name and read the emitted document. Replaces the multi-skill boot cascade.
- **Touches:** new skill under the skills layout; referenced as boot entry for every type; mirrors
  the crew seed (`crewstart.go:447`) but points at the command.
- **Accept:** running the one boot skill with just an agent name fully orients the agent (identity +
  wake-reason + operating + triggers + handoff), no other skill invoked to boot.
- **Deps:** T3.

---

## Group 7 — Wake-reason + keeper provenance re-pin (C-F / keeper wiring)

### T8 — `--wake` flag + keeper restart re-pins identity from `soul.md`
- **Goal:** `brief --wake <fresh|keeper-restart|trigger:<id>>` (default `fresh`) populates boot-doc
  §2; keeper restart re-runs `agent brief` so SOUL re-pins from `soul.md`, NEVER from the appended
  handoff block (I1).
- **Touches:** T3 emit logic (`--wake`); keeper `cycle.go:456/1016` (the KEEPER-IDENTITY-into-handoff
  append is superseded — restart triggers `agent brief`).
- **Accept:** `--wake keeper-restart` renders the correct §2 reason; keeper `/clear` funnels through
  `agent brief` → SOUL from `soul.md`; no path re-seeds identity from a prior session's handoff.
- **Deps:** T3, T7.

---

## Group 8 — Remaining instruction sets + captain wiring (C-G admiral/watch, C-H)

### T9 — Author `admiral`, `watch`, `captain` type folders
- **Goal:** Author the three remaining type folders per AUTHORING-GUIDE from the drafts + scattered
  sources; distill, don't transcribe.
- **Touches:** `.harmonik/agents/{admiral,watch,captain}/*` (sources: `captain/SKILL.md`+`STARTUP.md`,
  `missions/admiral.md`+`admiral-playbook.md`, `watch/SKILL.md`+`missions/watch.md`,
  `orchestrator-rules`); source: drafts/agents.
- **Accept:** three folders exist; each `soul.md` ≤25L, `operating.md` ≤45L; each passes C-C check;
  the "admiral directs · captain drives · crew executes" split encoded in each "I do NOT".
- **Deps:** T3, T5.

### T10 — Wire `harmonik start captain` to the captain type folder
- **Goal:** `harmonik start captain` sources its boot context from `.harmonik/agents/captain/` via
  the same `brief` emit, not ad-hoc `ensureBootAssets` file discovery.
- **Touches:** `cmd/harmonik/captain.go runCaptainLaunchWithOps:315` (add the symmetric seed paste
  mirroring `crewstart.go:502`, pointing captain at `agent brief`); `start.go:81` captain case
  unchanged. Daemon-checkout-reverts-tracked-files gotcha applies.
- **Accept:** `harmonik start captain` boots a captain from the emitted document; identity re-pins
  from `soul.md` on keeper restart (I1), not from the appended KEEPER-IDENTITY handoff block.
- **Deps:** T8, T9.

---

## Group 9 — Triggers (C-I)

### T11 — Declare triggers schema + scheduled comms-delivered command + activity-guard
- **Goal:** Model the `triggers[]` table: crew's single `queue` trigger + admiral's `cron` report
  trigger delivering a comms message; declare the activity-guard (fire only while fleet has been
  operating in a config'd window); render enabled triggers in boot-doc §4.
- **Touches:** `manifest.yaml triggers` on crew/admiral; boot-doc §4 emission (T3); reuses
  `harmonik comms send --wake`. Daemon-side delivery of NEW cron sources is a buildable follow-on
  (out of this task's scope — declaration + emission lands here).
- **Accept:** crew manifest declares one `queue` trigger; admiral declares a `cron` trigger with a
  message; both render in boot-doc §4; `enabled: false` removes a trigger from the emitted document;
  activity-guard window is a config value (not hardcoded).
- **Deps:** T3, T9.

### T12 — Watch marker-check on the event stream + friendly reminder
- **Goal:** Watch checks each type's declared `markers.never_emits` against the EVENT STREAM (not
  transcripts); on a violation emit a friendly reminder. Declarative-only wiring per SPEC §8.
- **Touches:** `manifest.markers` (T1); watch event-stream consumption (agent-comms / event bus).
- **Accept:** a declared `never_emits` marker checked against typed events; a violation produces a
  friendly reminder; no transcript grepping (event-stream authoritative).
- **Deps:** T1, T11.

---

## Dependency graph (edges)
```
T1 → T2 → T3 → T4
T1 → T5
T3 → T7 → T8
T1 (→ via T3,T5) T9 ; T3,T5 → T9
T8,T9 → T10
T3,T9 → T11
T1,T11 → T12
```
Explicit edges added via `br dep add <task> <dependson>`:
- T2 dep T1
- T3 dep T2
- T4 dep T3
- T5 dep T1
- T7 dep T3
- T8 dep T7
- T9 dep T3 ; T9 dep T5
- T10 dep T8 ; T10 dep T9
- T11 dep T3 ; T11 dep T9
- T12 dep T11
