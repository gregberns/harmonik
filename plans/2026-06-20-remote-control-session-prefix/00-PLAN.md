# Plan — Per-project prefix for Claude Code Remote-Control session names

**Date:** 2026-06-20
**Status:** DRAFT — investigation done; semantics confirmed; one storage decision open.
**Trigger:** Harmonik launches every crew/captain/watchdog as
`claude --remote-control "<agent-name>"`. The name (`captain`, `paul`, `chani`, …) is the
**display label in Claude Code's Remote-Control registry** — the list shown in the iOS app /
claude.ai session picker, which is **global per host/account, not per-project**. Two
projects running at once both register `captain`, `paul`, … → indistinguishable in the
picker. We want a per-project prefix, supplied from harmonik config.

> NOTE: This is **not** about tmux session names (those already carry a 12-char project
> hash and don't truly collide). This is about the Claude Code Remote-Control session
> *label* passed via `--remote-control <name>`.

---

## 1. Confirmed Claude Code semantics (decisive)

Claude Code exposes two relevant flags:

```
--remote-control [name]                         start RC session, optionally named
--remote-control-session-name-prefix <prefix>   prefix for AUTO-generated names (default: hostname)
```

**The prefix flag only applies to auto-generated names. When an explicit name is passed,
the prefix is ignored.** (Verified against current Claude Code docs/behavior.) Harmonik
*always* passes an explicit name, so `--remote-control-session-name-prefix` would be inert
for us as-is.

The RC session name is a **human-readable display label**, not a unique key — two live
sessions *can* share a name, but the picker can't tell them apart. The intended
multi-project pattern is therefore to **fold the prefix into the explicit name**:
`--remote-control "<prefix>-<name>"` → `hk-captain`, `hk-paul`.

**Design consequence:** prefix the `--remote-control` argument; do **not** rely on the
`--remote-control-session-name-prefix` flag.

---

## 2. Where harmonik passes the name today

| Launcher | Path | Line | Current arg |
|----------|------|------|-------------|
| Crew (daemon — main path) | `internal/daemon/crewlaunchspec.go` | 79 / 81 | `--remote-control <rc.name>` |
| Captain (CLI launcher) | `cmd/harmonik/captain.go` | 71 | `--remote-control <name>` |
| Captain (bash, embedded + repo) | `cmd/harmonik/captain-tools/captain-launch.sh` (+ `scripts/…`) | 81, 114 | `--remote-control $CAP_NAME` |
| Watchdog / keeper | `scripts/ctx-watchdog-launch.sh` | 56, 71 | `--remote-control $AGENT` |

**Key isolation property:** the RC name is purely a Claude-side display label. Harmonik's
own identity lives in `HARMONIK_AGENT=<name>` (set separately at `crewlaunchspec.go:92`),
the tmux session, and the `--session-id`. So we can change the RC *label* to
`<prefix>-<name>` while keeping `HARMONIK_AGENT=<name>` bare — internal identity (comms,
crew registry, keeper rebind) is untouched. **This must be verified, not assumed** (see
§6 risk): confirm nothing reads the RC label back as an identity key.

---

## 3. Goal

A per-project `remote_control_prefix` string, stored in harmonik config, that is prepended
to every `--remote-control` name harmonik emits:

- one **project slug** (locked §8) — short, meaningful, **2-char default** derived like the
  beads prefix (`deriveBeadPrefix`, `cmd/harmonik/init_cmd.go:241`), operator-overridable to
  longer; the RC prefix defaults to this slug;
- empty/absent ⇒ today's behavior (bare name) — fully backward compatible;
- reaches the daemon crew-launch path **and** the captain/watchdog bash launchers.

Result in the picker: `hk-captain`, `hk-paul`, `mp-captain`, `mp-chani` — disambiguated
across concurrent projects, with the agent identity still legible.

---

## 4. Storage — recommendation: `.harmonik/config.yaml`

- **`.harmonik/config.yaml` (daemon block) — RECOMMENDED.** Loaded at daemon startup
  (`main.go:861` → `daemon.LoadProjectConfig`, `internal/daemon/projectconfig.go:596`) and
  cached on `daemon.Config`; it's the only project config already on the crew-launch path.
  Add e.g. `daemon.remote_control_prefix: hk` (or a top-level `project: { slug: hk }` if we
  want one identity token shared with §8).
- `.harmonik/context/project.yaml` (tier-3, captain-only) — not loaded by the daemon, so it
  can't reach `crewlaunchspec.go` without a second loader. Rejected.

**Default derivation / backfill:** on `harmonik init`, write the prefix alongside the
existing `br init --prefix <xx>` step so beads-prefix and RC-prefix match by default. For
existing projects with no field, fall back to (a) beads `issue_prefix` if cheaply
readable, else (b) `deriveBeadPrefix(dir)`, else (c) empty → bare name (legacy).

---

## 5. Plumbing

1. **Config field.** Add `remote_control_prefix` to `rawDaemonConfig` / `DaemonConfig`
   (`internal/daemon/projectconfig.go`), parse in the daemon block parser, default-derive.
2. **Crew launch (the main path).** Add an `rcPrefix` field to `crewLaunchCtx`
   (`crewlaunchspec.go:19`); build the name arg as `joinPrefix(rcPrefix, rc.name)` (a tiny
   helper: `prefix + "-" + name`, or bare `name` when prefix empty). Keep
   `HARMONIK_AGENT=<name>` **bare**. Populate `rcPrefix` from the cached daemon config where
   `crewLaunchCtx` is built (crew start path — confirm config handle is in scope there).
3. **Captain CLI launcher.** Thread the prefix into `cmd/harmonik/captain.go:71` (read from
   config or a new `--rc-prefix` flag with config default).
4. **Bash launchers.** `captain-launch.sh` and `ctx-watchdog-launch.sh` need the prefix.
   Cleanest: a small read-side CLI — e.g. `harmonik config get remote_control_prefix
   --project <dir>` (or fold into an existing config-print command) — that the scripts call
   the same way `captain-launch.sh:51` already shells out to `harmonik project-hash`. Then
   `--remote-control "${RC_PREFIX:+$RC_PREFIX-}$CAP_NAME"`.
   - **Embed re-sync:** editing `captain-launch.sh` requires `cp` to
     `cmd/harmonik/captain-tools/captain-launch.sh` or `TestCaptainLaunchShEmbedInSync`
     goes RED (memory: embedded-asset re-sync gotcha). Run full `go test ./cmd/harmonik/`.
5. **Single helper.** Put the join logic in ONE place (a `lifecycle`/`daemon` helper) and
   have Go callers share it, so the format (`<prefix>-<name>`, delimiter, empty handling)
   doesn't drift between captain and crew.

---

## 6. Risks & tests

- **RC label vs. identity coupling (must verify first).** Confirm nothing reads the
  `--remote-control` label back as a key — comms uses `HARMONIK_AGENT`, crew registry uses
  the bare name + session-id, keeper rebinds via `--session-id`/`--resume`. Grep for any
  consumer of the RC title. If something does key off it, prefix there too. **This gates the
  design.**
- **Backward compat.** Empty prefix ⇒ exact current arg (`--remote-control <name>`). Unit
  test the empty path emits no `-` and no change.
- **Resume path.** The `--resume` re-launch (`crewlaunchspec.go:79`, and
  `captain-launch.sh:114`) must use the **same** prefixed label as the original launch, or
  the picker shows a renamed session. Test both branches produce identical labels.
- **Embed-sync** (see §5.4).
- **Live check.** Run two projects concurrently, confirm the iOS/claude.ai picker shows
  `hk-*` vs `mp-*` and that crew wake / keeper rebind still work.

---

## 7. Suggested work breakdown

Small, contained — likely a single kerf work (codename e.g. `rc-prefix`) or a handful of
beads under one epic:

1. Verify RC-label identity isolation (§6 gate) — read-only spike.
2. Config field + default derivation + `init` write (aligned with beads `--prefix`).
3. Crew-launch threading + shared join helper + unit tests (empty + resume branches).
4. Captain CLI + bash launchers (+ watchdog) + config-get read-side + embed re-sync.
5. Live two-project validation.

---

## 8. Decisions (locked 2026-06-20 by operator)

1. **One slug.** A single project slug sets the identity; the RC prefix defaults to it.
   Suggest a **2-character** default (derived like the beads prefix), but the operator may
   set it longer if they want — no hard length cap, just a short default.
2. **Delimiter: dash.** `hk-paul`.
3. **Existing projects:** the implementation agent should **ask the user** at migrate time
   rather than silently backfilling. Default suggestion = the project's current beads
   `issue_prefix`. (So: don't auto-rewrite; prompt, defaulting to the beads value.)
