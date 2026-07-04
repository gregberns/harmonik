# 03 — Components

Buildable units. Each: what it is · what it touches (with investigation file:line refs) · acceptance.
Build order follows DIRECTION §"Suggested build order": C-A → C-B/C-F → C-G(crew) → C-C/C-D → then
admiral + captain wiring (C-G/C-H) → triggers (C-I).

## C-A — Type-folder + manifest schema
**What.** The type registry: `.harmonik/agents/<type>/` holding `manifest.yaml` + static content
files (`soul.md`, `operating.md`, per-type extras). Git-tracked, immutable identity master. Schema
per SPEC.md §manifest.
**Touches.** New tree `.harmonik/agents/`. Parallels the existing config precedents:
`internal/crew/registry.go:38` (`crew.Record`, the closest existing per-agent record — but no role
field), `.harmonik/config.yaml harnesses.pi.*` (existing central per-agent-type YAML injection
precedent, investigation 05 §4). A Go parser/loader for the manifest (new package, e.g.
`internal/agentmanifest/`).
**Acceptance.** A `crew/manifest.yaml` + `soul.md` + `operating.md` parse into a typed struct;
missing/malformed fields are detected (feeds C-C). Loader resolves a type folder by name.

## C-B — The boot/injection command
**What.** `harmonik agent brief` (name picked — see SPEC §command-name). Resolves an agent → its
type → reads the manifest → **emits the ordered Markdown boot document** (SPEC §boot-document order).
No side effects (I2).
**Name resolution (load-bearing, MANIFEST §7).** `--agent` OPTIONAL; if omitted use `$HARMONIK_AGENT`
(harmonik sets it in every agent process — investigation 02 §4, `crewlaunchspec.go:124`,
`captain.go` tmux `-e HARMONIK_AGENT`). If BOTH supplied and they **conflict → hard error**.
`--override` bypasses the conflict error (captain/operator testing another agent's assembled metadata).
`--format markdown|json|yaml|toon` (default markdown). NO writes, symlinks, or seeds.
**Touches.** New subcommand under a new `harmonik agent` verb group in `cmd/harmonik/` (sibling to
`start.go`, `crew.go`). Reads the C-A loader. Uses `$HARMONIK_AGENT` the same way `comms.go` derives
default `--from`/`--name` (investigation 02 §4). Does NOT touch the tmux/launch path — it only emits.
**Acceptance.** `harmonik agent brief --agent leto` prints the full ordered document; with
`$HARMONIK_AGENT=leto` and no flag, identical output; conflicting `--agent`≠`$HARMONIK_AGENT` errors
unless `--override`; `--format json` prints the same content structured. Zero filesystem mutation.

## C-C — Schema-check command
**What.** A **first-class verb** (not just a flag): `harmonik agent check --agent <type>` validates a
type folder is well-formed (manifest fields present, referenced files exist, skill refs resolve,
boot-doc order buildable). MANIFEST §7 requires this be its own command; a `--validate`/`--dry-run`
flag on `brief` MAY also exist as a thin alias.
**Touches.** Same `internal/agentmanifest/` validation used by C-A's loader; new subcommand in
`cmd/harmonik/agent`.
**Acceptance.** A well-formed folder → exit 0 + "ok"; a folder with a missing `soul.md` ref or unknown
`context.ref` → non-zero exit naming the specific defect.

## C-D — Harmonik-supplied, per-agent-scoped skills + shared folder
**What.** Skills laid out exactly like the current skill spec, but **attached to harmonik** and
injected ONLY into the agents whose manifest requests them (NOT global auto-load). A **shared skills
folder** holds skills used by >1 type; manifests POINT there (no duplication); per-agent skills can
live in the type folder. Globally-auto-pulled skills are disabled via `.claude/settings.json` so
nothing leaks in. In the boot document each skill = **short-desc + pointer**, pulled on demand.
**Touches.** New shared skills dir (e.g. `.harmonik/agents/_skills/` — see DECISIONS-NEEDED),
`manifest.yaml context[].ref` resolution (C-A). `.claude/settings.json` edit to disable global
autoload — note the embedded-asset re-sync gotcha (edits to `.claude/skills/*` need `cp` to the
embedded init copy; MEMORY: embedded-asset-resync). Relates to finishing the AGENTS.md router
migration (investigation 03: move UBS/Beads tail out into role-scoped skills).
**Acceptance.** An agent whose manifest requests skills X,Y gets exactly X,Y as short-desc+pointer in
its boot document and nothing else; a shared skill referenced by two types is stored once; global
autoload is off (a fresh agent pane shows no un-requested skills).

## C-E — The ONE boot skill
**What.** A single thin boot skill (operator's "NOT a pile of skills" constraint, MANIFEST §3) whose
only job is: call `harmonik agent brief` with the agent name and read the emitted document. Replaces
the multi-skill boot cascade; the *code* does the assembly.
**Touches.** New skill under the skills layout; referenced as the boot entry for every type. Mirrors
today's crew seed which says "read <handoff> and run /session-resume" (`crewstart.go:447`) — but now
points at the command.
**Acceptance.** Running the one boot skill with just an agent name yields the agent fully oriented
(identity + wake-reason + operating instructions + triggers + handoff), no other skill invoked to boot.

## C-F — The boot-document order
**What.** The fixed emission ORDER (MANIFEST §7 — "the real work is getting the order right"):
1. Identity/SOUL (who I am · what I do NOT do · escalate-to · + parent intent) — FIRST, anchors all.
2. This wake's reason (fresh / keeper-restart / scheduled trigger).
3. Operating instructions (`operating.md` + requested skills as short-desc + pointer).
4. Active triggers (queue / cron / comms).
5. Handoff (episodic state) — LAST, read in the context of identity, never the reverse.
**Touches.** The C-B emit logic; the wake-reason input (how the command learns the wake kind — flag or
env; see DECISIONS-NEEDED). Provenance rule (I1): SOUL always from `soul.md`, handoff last + episodic-only.
**Acceptance.** Emitted document sections appear in exactly this order; SOUL content is byte-identical
to `soul.md`; handoff text contains no identity re-statement.

## C-G — Instruction sets for crew / captain / admiral / watch
**What.** Author each type's folder content per AUTHORING-GUIDE (soul.md ≤25L, operating.md ≤45L,
manifest.yaml). Distill from existing scattered sources; do NOT transcribe. crew first (proving
ground), then admiral, watch, captain.
**Touches (source material to distill, investigation 01/04):**
- crew: `.claude/skills/crew-launch/SKILL.md`, `.harmonik/crew/missions/_TEMPLATE-runner.md`, `leto.md`.
- captain: `.claude/skills/captain/SKILL.md` + `STARTUP.md` (extract essence only).
- admiral: `.harmonik/crew/missions/admiral.md`, `admiral-playbook.md` (do NOT mirror its rambling style).
- watch: `.claude/skills/watch/SKILL.md`, `.harmonik/crew/missions/watch.md`.
- shared: `.claude/skills/orchestrator-rules/SKILL.md`.
The **"admiral directs · captain drives · crew executes"** split (orchestrator-rules §Identity,
investigation 01) is the anti-drift boundary each soul.md's "I do NOT" encodes.
**Acceptance.** Four type folders exist; each `soul.md` ≤25 lines, `operating.md` ≤45 lines; a reader
who knows harmonik grasps each file in <60s; each passes C-C's check.

## C-H — Wire captain's `harmonik start captain` to its config
**What.** Captain stays a first-class verb, but its *content* comes from the `captain` type folder:
`harmonik start captain` (or its launch path) sources the captain manifest via the same C-B emit so a
captain boots from the registry, not from ad-hoc `ensureBootAssets` file discovery.
**Touches.** `cmd/harmonik/captain.go` `runCaptainLaunchWithOps` (:315) — today provides NO seed paste
(investigation 02 §1); add the symmetric injection point (after tmux launch, mirroring the crew paste
`crewstart.go:502`) that points the captain at `agent brief`. `start.go:81` captain case unchanged as
the verb. Note the daemon-checkout-reverts-tracked-files gotcha for any tracked mission edits (MEMORY).
**Acceptance.** `harmonik start captain` results in a captain whose boot context is the emitted
document from `.harmonik/agents/captain/`, not hand-found files; identity re-pins from `soul.md` on
keeper restart (I1) rather than from the appended KEEPER-IDENTITY-in-handoff (`cycle.go:456/1016`).

## C-I — Triggers incl. scheduled comms-delivered commands
**What.** The manifest `triggers` table: declared, toggleable (`enabled: true/false`) subscriptions —
`queue` / `cron` / `interval` / `event` / `comms` / `manual`. A `cron` trigger **delivers a comms
message carrying a specific instruction to call a command** (MANIFEST §4, "it is now time to…"). The
daemon owns the clock + the loop and hands the agent a wake-reason. This work SCOPES + declares the
schema and models crew's single `queue` trigger + admiral's scheduled report; the daemon-side loop
wiring for new sources is the buildable follow-on.
**Touches.** `manifest.yaml triggers`. Daemon comms bus (agent-comms skill; existing wake path). The
scheduled-delivery mechanism reuses `harmonik comms send --wake`. Activity-guard ("only while the
fleet has been operating") is a trigger option to pin (DECISIONS-NEEDED). Report *target* is the
deferred publish seam — for now the message just tells the agent to post to comms / write its handoff.
**Acceptance.** A crew manifest declares one `queue` trigger; an admiral manifest declares a `cron`
trigger with a message; both render in the boot document §4 "Active triggers"; toggling `enabled:
false` removes a trigger from the emitted document. (Daemon delivery of new cron triggers may land as a
follow-on bead; the declaration + emission lands here.)
