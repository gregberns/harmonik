# C3 — Multi-tenant settings & global tooling — spec-draft

> Pass 5 (`spec-drafts`) of `fleet-portability`, component **C3**. This is the
> single C3 draft file. C3 touches **three** target specs; this file sections the
> new/changed normative clauses by target spec:
>
> - **PRIMARY — `specs/operator-nfr.md`** — the new multi-tenancy operability NFR
>   (the C3 spec home, D9).
> - **`specs/release-pipeline.md`** — amended §7.2 (last-good path + supervisor-of-record).
> - **`specs/process-lifecycle.md`** — a small canonical-supervisor note (C3 note).
>
> Per local kerf convention this draft contains ONLY the full normative text of the
> NEW or CHANGED clauses as they should appear in each target spec — not a re-paste
> of the whole file, not a diff. The changelog lists each target spec separately.
>
> **Minted ON id:** `ON-058` — verified free against the live `specs/operator-nfr.md`
> (highest existing is ON-057; grep of `ON-[0-9]+` confirms 058 is the next free
> number). Flag for integration: re-confirm 058 is still free at integration time in
> case a parallel spec work mints it first.

---

## Target: specs/operator-nfr.md

> Insert under `## 4. Normative requirements` (after the §4.11 group; the exact §4.x
> sub-section placement is an integration detail — a new `### 4.12 Multi-tenant
> global-surface isolation` sub-section is recommended, since no existing §4.x group
> governs the shared `~/.claude/` surface or `/tmp` globals). The requirement carries
> the `Tags:` / `Axes:` footer, matching the style of ON-005 / ON-008a / ON-011.

### ON-058 — Multi-tenant global-surface isolation

Harmonik's contributions to surfaces shared across all projects on one machine — the
global `~/.claude/settings.json` keeper hook stanzas, the `~/.claude/captain-tools/`
scripts, and `/tmp/hk-*` daemon-state files — MUST be project-namespaced so that N
harmonik fleets coexist on one machine without one project's bootstrap, enable, or
restart perturbing another project's live state. A merge into any shared surface MUST
be additive: it MUST NOT rewrite, relocate, or delete a peer project's harmonik
contribution, nor any non-harmonik contribution the operator placed there.

**(a) Keeper hook stanzas in `~/.claude/settings.json`.**

The `hooks.<Event>` surface (e.g. `hooks.Stop`, `hooks.PreCompact`) is a JSON array of
matcher-groups; the Claude Code harness fires every group whose matcher matches the
event. Harmonik MUST treat it as additive and MUST NOT assume merge-or-overwrite-by-type.

1. **Project-keyed dedup.** When `harmonik keeper enable` installs or normalizes a keeper
   Stop/PreCompact hook group, it MUST deduplicate existing groups on the PAIR
   `(script basename, HARMONIK_PROJECT=<projectDir>)`, NOT on script basename alone. A
   candidate group matches an existing group only when BOTH the keeper script basename
   AND the `HARMONIK_PROJECT=<projectDir>` value (for this project's resolved root) are
   present in the group's command. A basename match with a different `HARMONIK_PROJECT`
   value MUST NOT match; it MUST fall through to an additive append, producing a second
   sibling group in the array.
2. **Coexistence.** Two distinct projects MUST therefore produce two distinct sibling
   groups in the `hooks.<Event>` array. The harness fires all matching groups; each
   group writes only to its own `$HARMONIK_PROJECT/.harmonik/keeper/<agent>.{idle,ctx}`
   path. There MUST NOT be a single dispatcher hook keyed off cwd or a project registry.
3. **Non-perturbation.** An enable for project B MUST NOT rewrite project A's group's
   `HARMONIK_PROJECT` value, command, or env; and MUST NOT touch any non-keeper hook
   group the operator authored. The in-place normalize path MUST be guarded so it only
   ever rewrites the group matching THIS project's `(basename, HARMONIK_PROJECT)` pair.
4. **Doctor scope.** `harmonik keeper doctor` MUST validate the presence and correctness
   of THIS project's keeper group (matched on the same `(basename, HARMONIK_PROJECT)`
   pair); it MUST NOT report a green check merely because some other project's keeper
   group exists.

**(b) The `statusLine` scalar singleton.**

`statusLine` is a scalar object (`statusLine.command`); the harness permits exactly one.
Harmonik MUST write a SINGLE project-agnostic `statusLine.command` stanza shared by all
projects:

1. The keeper `statusLine` command MUST NOT carry a `HARMONIK_PROJECT=<dir>` prefix
   (this prefix is stripped from the statusLine command ONLY; it is retained on the
   Stop/PreCompact hook commands per (a)). The command is the bare keeper statusline
   script path.
2. Project routing for the statusLine path MUST be resolved at runtime from each Claude
   session's inherited `HARMONIK_PROJECT` environment variable: the statusline script
   MUST resolve `PROJECT` as `${HARMONIK_PROJECT:-${PWD}}` and write the context gauge
   to `$PROJECT/.harmonik/keeper/<agent>.ctx`. Because each fleet session inherits
   `HARMONIK_PROJECT` from its launch environment, a single shared stanza routes each
   session's `.ctx` write to the correct project. (This extends the established
   single-global-entry / identity-at-runtime pattern from agent-name to project.)
3. A cwd-walk dispatcher for statusLine is PROHIBITED as a conformance path: the
   statusLine JSON piped by Claude Code does not carry `cwd`/`workspace`, so cwd-based
   project resolution is impossible.
4. **Env-unset guard.** If `$HARMONIK_PROJECT` is unset at statusLine runtime (operator
   launched a bare `claude` outside fleet tooling), the script MUST fall back to `$PWD`.
   A fleet session's CWD is its project root, so the `.ctx` write still lands correctly.
5. Because all projects converge on the identical project-agnostic stanza, the merge
   after the first enable is a no-op; it MUST remain additive and idempotent.

**(c) The `~/.claude/captain-tools/` scripts.**

1. The captain-tools scripts (at minimum `captain-launch.sh` and `crewlog.sh`) MUST be
   version-controlled in `scripts/captain-tools/` and embedded in the harmonik binary.
2. `harmonik init` MUST provision the embedded captain-tools scripts to
   `~/.claude/captain-tools/` ONLY IF the target file is absent; it MUST NOT clobber an
   operator-modified copy already present.
3. The provisioned scripts MUST contain no literal absolute project path. They MUST
   resolve the project root at runtime as `${HK_PROJECT:-${HARMONIK_PROJECT:-$(git
   rev-parse --show-toplevel)}}`, and MUST derive any per-project session-name
   qualifier and per-project resource path from the runtime-resolved project root and
   the per-project hash of `harmonik project-hash` (per the project-hash subcommand
   contract below) — not from a compiled-in path. The Claude Code transcript-directory
   slug MUST be computed from the resolved project root (`s#/#-#g` over the path), not
   hardcoded.

**(d) Per-project daemon state under `~/.harmonik/` or hash-qualified `/tmp`.**

Every harmonik-owned daemon-state artifact that is today a machine-global `/tmp/hk-*`
file or an unqualified shared tmux session MUST either live under the project's own
`<projectDir>/.harmonik/` subtree, OR carry the PL-006a `<project_hash>` qualifier
(the first 12 hex characters of `SHA-256(realpath(project_root))`):

1. **Last-good binary.** The pre-1.0 last-good-binary state file MUST be
   `<projectDir>/.harmonik/state/last-good-binary` (NOT the machine-global
   `/tmp/hk-last-good-binary`). Absent file on first read MUST be treated as a fresh
   start; there is no migration from the old `/tmp` path.
2. **Daemon log and keeper-launcher session.** The daemon-log default and the
   keeper-launcher tmux session MUST be project-qualified by `<project_hash>`:
   `/tmp/hk-<project_hash>-daemon.log` and a `<project_hash>`-suffixed keeper-launcher
   session name. The keeper-launcher session MUST NOT carry the `harmonik-` prefix (so
   it stays outside the PL-006 orphan-sweep namespace); a bare-prefixed, hash-suffixed
   name preserves both sweep-immunity and per-project distinctness. Operator overrides
   (`$HK_LOG` / `$HK_SESS`) MUST still take precedence.
3. **Supervisor of record.** The in-binary `harmonik supervise` (per-project flywheel
   tmux session, per-project `.harmonik/cognition/`, per-project `supervisor.lock`;
   zero `/tmp` globals) is the canonical per-project supervisor. Any hand-authored
   `/tmp/hk-daemon-supervise.sh` recovery artifact is NOT part of the supported surface.

**(e) Project-hash derivation.** All shell-layer call sites that need the per-project
hash MUST obtain it from the read-only `harmonik project-hash` subcommand (the project-hash
contract below) rather than reimplementing SHA-256 in shell, and MUST guard the call so
that a stale binary lacking the subcommand degrades gracefully (the un-qualified name is
the fallback) rather than failing the launch.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

---

## Target: specs/release-pipeline.md

> Amend `### 7.2 Supervisor last-good guard`. The existing text (verified) reads:
>
> > The supervisor script (`hk-daemon-supervise.sh` or its successor) MUST implement
> > the last-good-binary protocol:
> > ...
> > 2. **Last-good tracking:** the supervisor persists the path to the last known-good
> >    binary in a state file (e.g. `/tmp/hk-last-good-binary` for pre-1.0;
> >    `~/.harmonik/state/last-good-binary` for post-1.0). ...
>
> Two changes: (i) name the in-binary supervisor as the supervisor-of-record in place
> of the `/tmp` script; (ii) move the pre-1.0 last-good path under the project's own
> `.harmonik/`. The new §7.2 normative text:

### 7.2 Supervisor last-good guard

The per-project supervisor — `harmonik supervise` (the in-binary supervisor-of-record;
the `/tmp/hk-daemon-supervise.sh` artifact is retired from the supported surface), or a
de-hardcoded out-of-band shell fallback (`scripts/hk-supervise.sh`) — MUST implement the
last-good-binary protocol:

1. **On binary adoption:** before adopting a newly-installed binary as the last-good
   binary, the supervisor MUST query the release ledger. If the binary's `commitHash`
   matches an entry with `Yanked: true`, the supervisor MUST refuse adoption and log
   `refused_yank: <semver> <reason>`.
2. **Last-good tracking:** the supervisor persists the path to the last known-good binary
   in the per-project state file `<projectDir>/.harmonik/state/last-good-binary` (this
   replaces the former machine-global `/tmp/hk-last-good-binary` pre-1.0 path; the
   per-project path is the standing location for all releases, realizing the prior post-1.0
   target). The last-good binary is updated only when a new binary is adopted successfully
   (daemon started and ran for ≥30s without crash). An absent state file on first read is a
   fresh start (no migration from the old `/tmp` path).
3. **On crash-restart:** if the current binary crashes within 30s of start, the supervisor
   falls back to the last-good binary. If the last-good binary is the same as the current
   binary (first install or unknown regression), the supervisor backs off exponentially and
   alerts the operator (via stderr) rather than spinning.
4. **Refuse-to-start for yanked binaries:** if the operator manually installs a binary
   whose commit hash appears in a yanked ledger entry, `harmonik` (the binary itself) MUST
   check the embedded ledger on startup and exit with code `9` (yanked-binary) and message
   `FATAL: this binary (v<semver>, <sha>) has been yanked: <reason>`. This self-check is
   belt-and-suspenders over the supervisor guard.

> Companion edit (same spec, the §7.2-introducing reference at release-pipeline.md:255):
> the phrase "The supervisor script (`hk-daemon-supervise.sh` or its successor)" is
> superseded by the §7.2 opening above, which names `harmonik supervise` as the
> supervisor-of-record. The de-hardcoded `scripts/hk-supervise.sh` fallback (no hardcoded
> PROJECT or BIN; resolves PROJECT from `$HK_PROJECT`/argument/`git rev-parse`, BIN from
> `command -v harmonik` with `$HOME/go/bin/harmonik` fallback, failing loudly if neither;
> session named with the project-qualified supervisor session per the cross-spec note
> below) is the supported out-of-band launcher.

---

## Target: specs/process-lifecycle.md (C3 note)

> A small note appended to the supervisor lifecycle section (near PL-019). No new PL
> requirement number is minted by C3; this is a clarifying cross-reference note. (If
> the spec-owner prefers a numbered note, integration may assign one.)

**C3 note — canonical per-project supervisor; legacy `/tmp` script retired.**

`harmonik supervise` (per §PL-019: per-project flywheel tmux session
`harmonik-<project_hash>-flywheel`, per-project `.harmonik/cognition/` subtree, per-project
`supervisor.lock`) is the canonical per-project supervisor and the supervisor-of-record for
the last-good-binary guard of [release-pipeline.md §7.2]. The hand-authored
`/tmp/hk-daemon-supervise.sh` artifact is legacy/out-of-band and is NOT part of the
supported surface; a de-hardcoded `scripts/hk-supervise.sh` is the supported out-of-band
shell fallback. Both the in-binary supervisor and the shell fallback derive their per-project
session name and resources from the PL-006a `project_hash`.

The project-qualified naming of the SUPERVISOR session (as distinct from the flywheel
session already named in PL-019) and of the keeper helper sessions is owned by the
launch-layer session-naming contract (extended by component C2). This C3 note records that
those sessions MUST be project-qualified via the PL-006a `project_hash`; the exact prefix and
format are the launch layer's published contract (see the cross-spec note below for the one
unresolved prefix seam). C3 does not duplicate the session-naming scheme here.

---

## harmonik project-hash subcommand contract

> New read-only CLI subcommand owned by C3, consumed by C2 and by C3's de-hardcoded
> shell scripts. Recommended spec home: the process-lifecycle CLI-surface section
> (`specs/process-lifecycle.md`, alongside the other `harmonik <verb>` entries and
> adjacent to the PL-006a `project_hash` definition it exposes), since the hash is a
> PL-006a concept. FLAG for integration: confirm PL CLI-surface vs operator-nfr CLI as
> the home; this draft recommends PL (the hash itself is PL-006a-owned).

**`harmonik project-hash [--project DIR]`** — a read-only subcommand that prints the
project hash to stdout. Normative contract:

1. It MUST print exactly the PL-006a `project_hash` — the first 12 hexadecimal characters
   of `SHA-256(realpath(project_root))` — followed by a single trailing newline, and exit 0.
   It MUST compute the identical hash the Go core uses for tmux-session scoping and
   provenance markers (no re-derivation; the shell layer MUST NOT reimplement SHA-256).
2. `--project DIR` selects the project root; when omitted, the project root defaults to the
   current working directory. The path MUST be resolved to its real (canonical) path before
   hashing, consistent with PL-006a.
3. It MUST be side-effect-free: it MUST NOT start, contact, or require a running daemon, and
   MUST NOT write any file. It MUST NOT require `$TMUX`.
4. On any error (e.g. unresolvable project root) it MUST exit non-zero with a diagnostic on
   stderr and print nothing to stdout, so a shell guard of the form
   `HASH="$(harmonik project-hash --project "$P" 2>/dev/null || true)"` degrades to the
   empty/fallback value rather than emitting a malformed hash.

Ownership: OWNED by C3 (C3 needs it first, for the de-hardcoded shell scripts). CONSUMED by
C2 (the launch-layer session-namers' shell-facing peers). Both consumers MUST use this single
subcommand as the authoritative hash source.

---

## Cross-spec / integration notes

**(i) The C2↔C3 supervisor-session-PREFIX seam — the ONE genuine seam integration MUST
resolve.** C3's design wants the project-qualified supervisor session to keep the `hk-`
prefix — `hk-<project_hash>-daemon-supervise` — so it stays OUTSIDE the PL-006
`harmonik-<project_hash>-` orphan-sweep namespace and needs no PL-006d live-owner sentinel
exemption. C2's design wants `harmonik-<project_hash>-daemon-supervise` — INSIDE the swept
namespace, requiring a PL-006d sentinel exemption. These are mutually exclusive; this draft
does NOT resolve them. The normative text above is deliberately PREFIX-AGNOSTIC: it refers to
"the project-qualified supervisor session" and "the keeper-launcher session … qualified by
`<project_hash>`" without hardcoding which prefix wins. Integration MUST pick one prefix for
the supervisor session and align: (a) the `SupervisorSessionName(projectDir)` const→func
change (on the C2/C3 boundary), (b) the `scripts/hk-supervise.sh` session name, and (c)
whether a PL-006d sentinel exemption is required. Recommendation (C3's): `hk-` prefix
(sweep-immune, no sentinel) — but this is the seam to settle, not a locked decision.

**(ii) `harmonik project-hash` is shared with C2.** The subcommand is owned by C3 but is the
single shell-facing hash source for both components. Integration MUST ensure C2's
session-namers and C3's scripts derive the hash from this one subcommand (against the same
underlying `tmuxStartHashDir` / `lifecycle.ComputeProjectHash` accessor), so there is exactly
one hashing scheme.

**(iii) captain-launch.sh session-id + name minting is a PUBLISHED Captain & Crew contract.**
Versioning `captain-launch.sh` (and qualifying the captain/crew/keeper session names it
mints) intersects the published Captain & Crew session-id/`--session-id` minting contract and
C2's session-name qualification. These edits MUST route through an INDEPENDENT reviewer
ALONGSIDE C2 (review gate). C3 owns the script FILE; C2 owns the qualified-name FORMAT.

**(iv) `harmonik init` (C1) is the provisioning site for the embedded captain-tools.** The
ON-058(c) "provisioned by `harmonik init` only if absent" obligation lands in C1's `init`
implementation; the embedded assets and the `scripts/captain-tools/` source live in C3. This
is the C1↔C3 provisioning seam — integration MUST confirm `init` (C1) calls the C3-provided
provisioning step (embed → `~/.claude/captain-tools/`, chmod 0755, never clobber).

**(v) Contradiction check against existing specs.** No contradiction found. ON-058 fills a
genuine gap: the existing operator-nfr §2.2 explicitly lists "no multi-tenancy in MVH … shared
… concerns acknowledged and deferred" — ON-058 is the POST-MVH realization of that deferred
concern (shared-surface hygiene for the global `~/.claude/` surface and `/tmp` globals), so it
is additive, not in tension. The amended release-pipeline §7.2 only tightens the already-named
post-1.0 target (`~/.harmonik/state/last-good-binary`) into a per-project path and renames the
supervisor-of-record from the retired `/tmp` script to `harmonik supervise`, consistent with
process-lifecycle PL-019 (which already establishes `harmonik supervise` + the per-project
`.harmonik/cognition/` subtree). The only open seam is the supervisor-session prefix (note (i)),
which is flagged, not decided.
