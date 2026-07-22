# 04-design / process-lifecycle-design — PL-021b/PL-021d amendments (M2 agent-input-substrate)

> Pass 4 (Change Design), spec area `specs/process-lifecycle.md` (§4.7 "ntm adapter scope",
> PL-021b :728 / PL-021d :770). Components C3 (observation-only tmux) + C6 (deletion boundary).
> Subordinate to `04-design/00-decisions.md` — conforms to **D5** (process ownership /
> inspectability reinterpretation), **D6** (keeper carve-out), **D10** (new `specs/agent-input.md`,
> prefix `AIS`). Does NOT re-decide any of these. Line numbers verified against the tree on
> `phase1-session-restart-substrate`, 2026-07-14.

---

## Current state (what the spec says NOW)

**PL-021b — "Direct-tmux substrate (MVH alternative to ntm adapter)"** (`process-lifecycle.md:728`,
§4.7). Eight numbered obligations on `internal/lifecycle/tmux`: (1) pane creation via `tmux
new-window`; (2) `tmux -V` ≥ 3.0 availability check; (3) `$TMUX` session resolution; (4) replay-stable
window naming per WM-002a; **(5) "No pane-output consumption"** — the daemon MUST NOT read pane
stdout via `pipe-pane`; the pty "exists exclusively for operator ergonomics (interactive attach)";
(6) the substrate seam (`LaunchSpec` optional substrate handle, composition-root injected); (7)
Wait/kill via `tmux list-panes` poll + `tmux kill-window`; (8) `tmux_window_name` observability on
`agent_started`. PL-021b §5 is the load-bearing "read boundary" — it forbids the READ direction only.

**PL-021d — "Daemon→pane write mechanism (tmux load-buffer + paste-buffer)"** (`process-lifecycle.md:770`,
§4.7). The symmetric WRITE clause. Load-bearing sentences: it exists because pane writing "is
unspecified in PL-021b and needed for initial-task delivery and inter-phase message injection"; the
daemon "MUST use the `tmux load-buffer` + `tmux paste-buffer` sequence rather than `tmux send-keys`
with a bare string" (5-step: temp file per WM-026 → load-buffer → paste-buffer → delete-buffer →
remove temp); `send-keys -l` "permitted as a fallback when payload length is below 512 bytes and no
newlines"; the bare `send-keys` form is "FORBIDDEN". Plus buffer-name discipline
(`harmonik-<session-id>-<purpose>`), a delete-buffer cleanup obligation, the "why this is not
pipe-pane" inspectability argument, and a `daemon_pane_write` INFO audit record.

**This clause is exactly what C3 retires from the daemon RUN path.** Its verbs (`load-buffer`,
`paste-buffer`, `send-keys`) are what C6 deletes — but only the daemon-run-scoped subset (see D6).

---

## Target state (specific sentence-level changes)

### PL-021b — add the observation-only boundary clause (C3 / SC3)

Amend §5 and the item-5 gloss ("exists exclusively for operator ergonomics") to state the **new
tmux role: observation-only**. Add a subclause (PL-021b §5a or a short paragraph after item 8):

> **Observation-only after AIS.** Once the structured agent-input substrate (`specs/agent-input.md`
> §AIS) owns the daemon-run input path, the direct-tmux substrate carries **no input** for daemon
> runs. The read verb `tmux capture-pane` MAY survive as a human-facing observation seam
> (`internal/lifecycle/tmux/osadapter.go`); the READ boundary of §5 (no `pipe-pane` structured
> consumption) is unchanged. Inspectability of a headless `stream-json` agent is satisfied per the
> AIS capture-tee (tail the recorded wire) + an optional observation pane, NOT by tmux owning the
> child process (**D5**).

No change to items 1–4, 6–8 (spawn/naming/kill are shared with crew spawn and remote — they survive).

### PL-021d — DEMOTE, do not delete (C3 / D6)

PL-021d is **demoted**, not removed. Concretely:

1. **Retitle + status line.** Change the heading to
   `PL-021d — Daemon→pane write mechanism (DEMOTED — superseded by AIS for daemon runs; retained for keeper + interactive-session nudge)`
   and insert a **demotion clause** as the first paragraph:

   > **DEMOTED (M2 agent-input-substrate).** The `load-buffer` + `paste-buffer` write mechanism is
   > **removed from the daemon RUN input path** — daemon-run task delivery and inter-phase injection
   > are now owned by `specs/agent-input.md` §AIS (the structured input port + real ack). This clause
   > is **PRESERVED** for exactly two non-run consumers: (a) the **session-keeper** paste path
   > ([session-keeper.md §SK-002], `internal/keeper/injector.go`) per the **D6 carve-out**; and (b)
   > the **interactive-session nudge / boot-seed** paths (`cmd/harmonik/{captain,crew,comms}.go`) that
   > inject into panes they did not spawn via `handler.Substrate`. For those two consumers the
   > `load-buffer` + `paste-buffer` discipline, buffer-name format, delete-buffer cleanup, and
   > `daemon_pane_write` audit **remain NORMATIVE**. For daemon runs, they are superseded — see AIS.

2. **Change the "needed for" sentence.** The opening rationale ("needed for initial-task delivery and
   inter-phase message injection") is edited to say those two daemon-run uses are **now AIS-owned**;
   the paste path's live purpose narrows to keeper handoff-cycle nudges + interactive boot-seed.

3. **Cross-reference AIS.** Add to §9 Cross-references and inline: "Superseded for daemon runs by
   `specs/agent-input.md` §AIS (AIS-INV-001 bounded-liveness ack)."

4. **Preserve every verb sentence verbatim** — the `load-buffer`/`paste-buffer`/`send-keys -l` MUST
   language stays, because keeper and the CLI nudge still require it. The demotion is a **scope
   narrowing of the consumer set**, not a deletion of the write discipline.

### C6 deletion boundary (SC4)

A short normative note (in PL-021d's demotion clause or a PL-021b observation subclause) fixing the
**deletion boundary** C6 executes after the bake window:

> **Deletion boundary (post-bake, AIS-INV-001 acceptance-gated).** After the AIS bake window passes
> (C5 fault-harness N-green + output-or-stale oracle), the daemon-run paste-inject stack
> (`internal/daemon/pasteinject.go`, the input portions of `tmuxsubstrate.go`, and the six
> type-asserted side-interfaces) is deleted. The tmux write verbs `load-buffer`/`paste-buffer`/
> `send-keys` in `internal/lifecycle/tmux` are **NOT deleted** while the D6 keeper carve-out and the
> CLI nudge paths still consume them. `capture-pane` (read) is retained for observation. tmux window
> **spawn** verbs (`new-window`, `kill-window`, `list-panes`) are retained (shared with crew spawn +
> M4 remote).

---

## Rationale

- **D5 (process ownership).** A real bidirectional protocol needs the driver to own the child's
  stdin+stdout directly (the codex app-server pattern). tmux owning the pty is incompatible with the
  daemon parsing the child's stdout for protocol events. PL-021b's item-5 "pty exists exclusively for
  operator ergonomics" survives verbatim; what changes is that ergonomics = observation, not hosting.
  The locked "tmux inspectability required" decision (`project.yaml:26`) is **reinterpreted, not
  reopened**: inspectability = capture-tee-backed observation, because a headless `stream-json` agent
  shows raw NDJSON in a pane, not a watchable TUI. **PLANNER-RECONCILE (D5, carried inline): operator
  must confirm a capture-tee-backed observation window satisfies "tmux inspectability required"
  before design lock; the FIFO-stdin+side-pipe-stdout hosting fallback is heavier and the codex
  precedent argues against it.**

- **D6 (keeper carve-out) resolves the A11 deletion hazard.** SK-002 (freshly normative 2026-07-13,
  one day before this work) REQUIRES `PanePort.Inject` to follow PL-021d. Keeper's injector shells
  out to tmux directly (own buffers, own `exec.Command`), is off-daemon, and is depguard-barred from
  importing daemon — so it structurally cannot obtain a `handler.Substrate` session handle. Deleting
  PL-021d verbs from `lifecycle/tmux` would break keeper's restart cycle. The carve-out **narrows C6
  scope to the daemon-run path** and amends SK-002's cross-reference (see `session-keeper-design.md`)
  rather than contradicting it. **PLANNER-RECONCILE (D6, carried inline): TASKS.md M2-3/M2-6 state
  the intent as "keeper MIGRATED to the C2 driver" before C6 deletes; this design chooses
  carve-out + narrowed C6 scope (safe, unblocks M2) and DEFERS migrate-vs-carve-out to the planner.
  If migration is insisted, it becomes a sizeable session-id-keyed input-contract task and SK-002 must
  be re-drafted in the same motion.**

- **PL-021d's non-daemon-run consumers survive the demotion** (seam-contract risks #2, #5):
  - `cmd/harmonik/{captain,crew,comms}.go` boot-seed + wake-nudge inject into interactive panes
    without touching `handler.Substrate` — explicit carve-out, not deleted.
  - **EM-015d-RIA / EM-015d-RFD** (`execution-model.md:375-403`, "PL-021b-PASTE" label) reference the
    paste steps for the daemon-run resume path — these are **rewired to AIS** in the execution-model
    design, so the PL demotion is consistent with that motion.
  - **CHB-028** (`claude-hook-bridge.md:469`) names `agent-task.md` as "the normative daemon→claude
    task-delivery channel." Retiring paste obsoletes the **delivery instruction** (the paste verb),
    NOT the `agent-task.md` **artifact contract** — CHB-028's file contract survives with a new
    delivery clause (AIS delivers the payload; the artifact still exists). The PL demotion must NOT be
    read as deleting the agent-task.md contract.

---

## Requirements traceability

| Success criterion / decision | PL change |
|---|---|
| **SC3 — tmux observation-only** | PL-021b new observation-only subclause; `capture-pane` read verb survives, write verbs carry no daemon-run input; item-5 READ boundary unchanged. |
| **SC4 — deletion after bake** | C6 deletion-boundary note: daemon-run paste stack deleted post-bake gated on AIS-INV-001 acceptance (C5); tmux write verbs NOT deleted while keeper + CLI nudge consume them; spawn verbs retained. |
| **D5 — process ownership / inspectability** | PL-021b subclause reinterprets inspectability as capture-tee-backed; driver owns stdio (specced in AIS, cross-ref'd here). |
| **D6 — keeper carve-out** | PL-021d demotion clause preserves the write discipline for keeper (SK-002) + CLI nudge; narrows C6 scope; cross-refs SK-002 amendment. |
| **D10 — new AIS spec** | PL-021d "superseded by `specs/agent-input.md` §AIS" cross-reference added in the same commit that reserves the `AIS` prefix. |

## Open items handed to Tasks pass

- The exact AIS §-anchor PL-021d cross-references (fixed once `agent-input.md` numbering lands).
- Machine depguard: land a REAL `handler → internal/lifecycle/tmux` deny with the seam (current
  boundary is doc-comment-only, seam-contract Q1/risk #3) — expressible once C3/C6 land.
- `SubstrateSpawn.StdinDevNull` asymmetry (risk #6): the AIS driver changes who owns stdin per
  harness; the flag's fate is an AIS/handler-contract call, noted here so PL stays consistent.
- Remote (SSH) seam (risk #7): every side-interface has a `…Via(runner)` twin; the AIS input port
  must stay remote-capable so M4 does not regress — carried into the AIS + handler-contract designs.
