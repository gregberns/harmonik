# 02 — Components (Affected Spec Areas)

**Work:** process-group-provenance
**Pass:** 2 (Decompose)
**Beads:** hk-n93gq, hk-o7x4w, hk-g1qby, hk-c6dt2, hk-5z4ww

Every line citation below was re-verified against the working tree at the time of writing
(`/Users/gb/github/harmonik`, read-only). Claims inherited from `00-inherited-child-pgid/`
were re-checked against source, not carried forward on trust.

---

## 0. How this pass is organised

The problem space asks for two categories to stay separate, and they do:

- **Drift (D-items).** The spec already describes the system falsely today. These are fixed
  regardless of which design wins in pass 3/4. Landing the amendment without fixing them
  would bury them.
- **Amendment (A-items).** A genuine behavioral change: the system does X today, the spec
  says X, and we want Y.

A third category turned up and gets its own bucket, because folding it into either would be
wrong:

- **Gap (G-items).** Implemented, shipped behavior with **no requirement anywhere**. Not
  drift (nothing false is written) and not an amendment (there is nothing to amend).

New findings this pass surfaced — beyond the three the problem space already named — are
called out inline as **NEW FINDING** and re-listed in §7.

---

## 1. Affected spec files

Four, not the two the problem space named. `specs/beads-integration.md` is added here (§1.3);
`specs/handler-contract.md` is confirmed but its scope is smaller than the inherited draft
assumed (§1.2).

| File | Role in this work | Requirements touched |
|---|---|---|
| `specs/process-lifecycle.md` | Primary. Owns the marker, the sweep, the pidfile, the invariant, the OQ. | PL-002b, PL-005, PL-006, PL-006a, PL-007, PL-021b, PL-021c, PL-INV-002, PL-INV-005, OQ-PL-008, OQ-PL-011 |
| `specs/handler-contract.md` | Secondary, and **conditional** on OQ-PL-008 (§5). | HC-044, HC-018; HC-044a read-only cross-check |
| `specs/beads-integration.md` | **Newly identified.** BI-014a mandates exactly the matching PL-007 forbids. | BI-014a |
| `specs/process-lifecycle.md` §4.2a (new) | Proposed new requirement family — see §3. | PL-006e/f/g (new IDs) |

---

## 1.1 `specs/process-lifecycle.md`

### Drift — the spec is descriptively false today

---

#### **D1 — PL-006 `:343`: `br` children carry no marker at all**

Current text (`specs/process-lifecycle.md:343`), verbatim fragment:

> "`br` subprocesses bear the same PL-006a provenance marker (env var + PGID) and re-parent
> to init on daemon crash exactly as handler subprocesses do; the SIGTERM-then-SIGKILL
> discipline is identical for both."

Verified false on both halves. `internal/brcli/adapter.go:146` is
`cmd := exec.CommandContext(ctx, a.brPath, args...)`; the only field set afterwards is
`cmd.Dir` (`adapter.go:152`). A grep for `cmd.Env` and `SysProcAttr` across `internal/brcli`
returns **zero non-test hits** — so the child inherits the daemon's own environment, and the
daemon never `os.Setenv`s the marker into itself (zero `os.Setenv` hits in `internal/daemon`
and `cmd/harmonik`). No env marker, no deliberate PGID.

**What must be true after the change:** PL-006 must state the marker `br` children actually
carry, and that statement must be enforceable at the one spawn site that creates them. The
requirement is *not* "describe reality" — reality is unsafe (see D-related A5). It is: the
spec must name a marker, and the BI adapter spawn must be normatively obliged to set it, so
the `br` sweep can be project-scoped instead of name-scoped.

**Bead:** hk-c6dt2. **Category:** drift (the false sentence) + amendment (A5, the obligation).

---

#### **D2 — PL-INV-005 `:1135`: the sensor is false at six spawn sites, not four**

Current text (`:1135`):

> "Sensor: every spawn site MUST set the provenance marker of PL-006a (environment variable
> + PGID). A subprocess without the marker is not a harmonik-owned subprocess by definition
> and MUST NOT be reaped by PL-006."

The problem space says "four places." Verified: it is six, and they fail in **three
different ways**, which matters because a single blanket exemption clause will not cover them.

| Spawn site | env marker | daemon PGID | Why |
|---|---|---|---|
| `internal/handler/handler.go:309` | yes (`:308` `cmd.Env = spec.Env`, marker injected at `internal/daemon/workloop.go:1103`) | yes | the only fully-conformant site |
| `internal/handler/handler.go:273` → `launchViaSubstrate` (`:426`) | yes — `SubstrateSpawn.Env` (`:437`) reaches `tmux new-window -e K=V` | **no** — no `SysProcAttr` is set anywhere on this path; the group is whatever the tmux server assigns | C4 of the inherited work |
| `internal/brcli/adapter.go:146` | **no** | **no** | D1 |
| `internal/daemon/scheduletick.go:255` | yes (`:251`) | **no** — bare `SysProcAttr{Setpgid:true}` ⇒ own group, **deliberately**: "Detach into its own process group so it survives a daemon restart" | intentional exemption, undocumented in spec |
| `internal/daemon/dot_cascade.go:2269` | yes (`:2259` `cmd.Env = append(os.Environ(), env...)`) | **no** — own group, deliberately (hk-me8ru group-kill) | intentional exemption, undocumented |
| `internal/daemon/dot_cascade.go:2108` (local `go build`/`go vet` gate) | **no** — `cmd.Env` is never set on this branch | **no** — own group (hk-me8ru) | **NEW FINDING**: this site sets neither half |

Plus three launch-layer sites that spawn harmonik processes with no marker discipline at all:
`internal/supervise/supervisor.go:355` (`Setpgid`), `internal/supervise/supervisor_watchdog.go:199`
(`Setsid`), `internal/supervise/daemon_watchdog.go:397` (`Setsid`). Whether PL-INV-005 is
meant to reach these is itself undecided — see OD-6.

**What must be true after the change:** PL-INV-005's sensor must be a predicate that is
*true*. That requires (a) restating the marker as whatever pass 4 lands, and (b) an explicit,
enumerated **exemption register** — a normative list of spawn sites that legitimately do not
carry the daemon-group half, with the reason and the consequence (namely: an exempt process
is invisible to PL-006 and MUST NOT be reaped by it). Goal 3 ("every spawn site either sets
the marker or is explicitly and normatively exempted") is not satisfiable by prose alone; it
needs a list a reviewer can diff against `grep SysProcAttr`.

**Beads:** hk-n93gq (the substrate row), hk-c6dt2 (the br row), hk-5z4ww (indirectly — an
unmarked process cannot be safely reaped). **Category:** drift.

---

#### **D3 — PL-006a `:376`: darwin PGID matching that no code implements**

Current text (`:376`):

> "The orphan sweep (PL-006) MUST match on the environment variable on Linux and on the PGID
> on darwin (where `/proc/<pid>/environ` is not available); darwin-specific fallback
> mechanics are tracked as OQ-PL-008."

Verified false. The sweep's candidate enumeration is `ps -eo pid,ppid`
(`internal/lifecycle/orphansweep.go:227`) — **it does not even request `pgid`**. The only
matcher is `MatchesProvenanceMarker` (`internal/lifecycle/provenance.go:177-186`), which
inspects env strings only. On darwin, `ReadProcessEnviron` (`provenance.go:195`) always
returns `ErrNotExist`, so `orphansweep.go:258-260` `continue`s every candidate. There is no
PGID branch to be inert — there is no PGID branch.

**What must be true after the change:** `:376` must describe a matching rule that (a) has a
readable input on darwin, (b) is bounded so it cannot collide (see A8 / §4), and (c) states
the behavior when the input is unreadable. Per the work's fail-closed constraint, unreadable
⇒ **do not reap**, and that must be written, not implied. Note that the current text is not
just undone but *unsafe if implemented literally after the kill-path change* — see §5.

**Bead:** hk-o7x4w. **Category:** drift.

---

#### **D4 — PL-006a `:372`: the `setsid` MUST has zero production callers**

Current text (`:372`):

> "The daemon MUST call `syscall.Setsid()` immediately on startup (PL-005 step 0) before
> spawning any subprocess, producing a session whose PGID equals the daemon's PID at that
> moment. This PGID MUST be recorded in the pidfile per PL-002b (line 2). On every handler
> subprocess spawn, the daemon MUST set Go's `SysProcAttr{Setpgid: true, Pgid:
> <recorded_pgid>}` and MUST retry once on `EACCES` (the child has already called `execve`)."

Three separate falsehoods in one paragraph:

1. `SetsidDaemon` (`internal/lifecycle/provenance.go:79`) has **zero** production callers
   (grep over `internal/` + `cmd/` excluding tests). Any daemon that leads its own session
   does so only because `internal/supervise/daemon_watchdog.go:397` spawned it with
   `SysProcAttr{Setsid: true}` — an accident of launch path, exactly as hk-g1qby states.
2. The pidfile line-2 PGID *is* written (`internal/lifecycle/pidfile.go:121`) but it is
   `syscall.Getpgrp()` of an un-setsid'd daemon, so its value is whatever group the launcher
   left it in.
3. "On every handler subprocess spawn" is true only for the direct-exec branch
   (`handler.go:309`); the substrate branch returns at `handler.go:273` before reaching it.

Note also that `SpawnSysProcAttr` (`provenance.go:59`), the helper whose doc comment quotes
this very sentence, has **zero** production callers — the live path uses
`SpawnChildSysProcAttr` (`spawndaemonchild_darwin.go:20` / `_linux.go:20`).

**What must be true after the change:** the setsid MUST is either (a) retired with an explicit
"RETIRED, and here is what replaced it" note, or (b) kept and made true. The inherited draft
deletes it silently (correction C3) and that must not ship. Whichever way it goes, the
paragraph must stop asserting a per-spawn rule that only one of two branches obeys.

**Bead:** hk-g1qby. **Category:** drift, with a decision attached (OD-3).

---

#### **D5 — NEW FINDING — PL-INV-005 `:1133` parentage clause contradicts PL-021b `:732`**

Current text (`:1133`):

> "Every live handler subprocess spawned during normal operation MUST have the daemon (by
> PID) as its initial parent; post-crash re-parenting to init (PID 1) is the only legal
> parentage deviation..."

Against PL-021b §1 (`:732`):

> "the daemon MUST create the subprocess via `tmux new-window -d -t <session>: -n
> <window-name> -c <cwd> -e KEY=VALUE [...] -- <binary> <argv...>`. The daemon MUST NOT spawn
> such subprocesses via `exec.CommandContext` directly."

A `tmux new-window` child's parent is the **tmux server**, not the daemon. Two normative MUSTs
in the same spec are in direct contradiction, and PL-021b is the one the code obeys
(`internal/handler/handler.go:273`, `:426-437`). `HC-044:806` ("MUST spawn every handler
subprocess as a direct child process") carries the identical contradiction.

This is load-bearing for this work, not a stylistic nit: **PL-INV-005 is cited at `:1133` as
the justification for the sweep's detection rule**, and its premise ("was spawned by a daemon
of this project") is exactly what the provenance mechanism has to re-establish for a process
the daemon did not fork.

**Beads:** hk-n93gq, hk-o7x4w. **Category:** drift.

---

#### **D6 — NEW FINDING — the `PPID==1` filter makes the handler sweep structurally blind to substrate-hosted orphans on *both* platforms**

PL-006 `:343` scopes the subprocess sweep to "processes that have been re-parented to init
(parent pid 1)", and `orphansweep.go:244-246` implements it (`if err != nil || ppid != 1
{ continue }`). A tmux-hosted agent whose daemon died is **not** re-parented to init — the
tmux server is still its parent and still alive. So the handler-process sweep can never see
the substrate path's processes, on Linux or darwin.

Coverage of substrate-hosted orphans rests entirely on PL-021c (`:752-761`), which kills by
**window-name prefix** and explicitly refuses to SIGKILL survivors (`:761`: "The daemon MUST
NOT send SIGKILL to survivors at MVH"), leaving them "adopted by the OS init process and not
tracked further by this daemon instance" — i.e. re-parented to init *after* the sweep that
would have caught them has already run.

Consequence for scoping: fixing the darwin marker (hk-o7x4w) does **not** by itself make the
sweep cover the tmux path. The work must say which path each reaper owns, or hk-o7x4w will be
closed with the leak still open.

**Bead:** hk-o7x4w (scope definition). **Category:** drift (PL-006's enumeration clause
claims a completeness it does not have).

---

#### **D7 — NEW FINDING — the relay-grandchild exclusion fails OPEN on darwin, and the hk-o7x4w fix activates it**

`internal/lifecycle/orphansweep.go:270-274`:

```go
args, cmdErr := ReadProcessCmdlineArgs(pid)
if cmdErr == nil && IsRelayGrandchild(args) {
    continue
}
matched = append(matched, pid)
```

`ReadProcessCmdlineArgs` is `/proc`-only and says so at `provenance.go:151-153`: "On platforms
where `/proc` is absent (darwin) this returns (nil, error)". So on darwin `cmdErr != nil`
always, the `IsRelayGrandchild` guard is never consulted, and the process is **added to
`matched`** — i.e. SIGTERM'd then SIGKILL'd.

Today this is harmless only by accident: the environ read at `:257` `continue`s first, so the
loop never reaches `:270` on darwin. **The moment a darwin-readable provenance marker lands,
PL-017a(b)'s exclusion silently degrades to "kill everything that matched," including live
`harmonik hook-relay` grandchildren.** That directly violates this work's own fail-closed
constraint, and it is a regression *introduced by* the fix for hk-o7x4w.

**What must be true after the change:** every exclusion the sweep applies must have a
darwin-readable input, or the sweep must fail closed when the exclusion input is unreadable.
Concretely, PL-017a(b) needs a portable argv source (`ps -o args=` — already used by
`agentwatcherreap.go:60`) or an explicit "unreadable ⇒ skip the candidate" rule.

**Bead:** hk-o7x4w (it is the bead whose fix arms the hazard). **Category:** drift + latent
defect.

---

#### **D8 — NEW FINDING — OQ-PL-008 is double-booked**

`:358` says "case-fold ambiguity remains tracked under OQ-PL-008" — a project-hash
canonicalisation question. The OQ body at `:1358-1363` is *exclusively* about the darwin
provenance marker and never mentions case-folding. Resolving OQ-PL-008 for provenance (which
this work must do) would silently close a second, unrelated question.

**What must be true after the change:** split the case-fold question to its own OQ before
resolving OQ-PL-008, or the resolution note must explicitly carve it out.
**Category:** drift (a dangling cross-reference). **Bead:** none directly; hygiene attached to
hk-o7x4w's OQ resolution.

---

### Amendment — genuine behavioral change

---

#### **A1 — PL-006a `:368-370`: redefine the provenance marker**

Current text (`:370`):

> "The provenance marker MUST be implemented by BOTH of the following to permit
> disambiguation across OS and tool differences: (i) setting the environment variable
> `HARMONIK_PROJECT_HASH=<project_hash>` on every spawned subprocess (readable via
> `/proc/<pid>/environ` on Linux); (ii) setting the subprocess's process group (PGID) to a
> deterministic per-project value as concretized below."

**What must be true after the change** (stated as requirements on the spec, not as prose):

1. The spec must name **one** mechanism, and must state, per platform, what a third party
   reads to evaluate it. "BOTH of the following" is currently a promise of redundancy that
   delivers zero coverage on darwin.
2. It must state whether the marker binds **the process** or **the process group**, and
   therefore whether it covers **grandchildren** (the open question in the problem space).
   The env marker is inherited by descendants; a per-pid registry record is not. That
   difference is the whole design and must be explicit.
3. It must bind identity to `(pid, start_time)` — never `pid` alone, never `pgid` alone.
   Measured pid wrap on this box is ~45 minutes; a bare recorded pid or pgid identifies
   nothing after downtime. See §4 for the start-time source.
4. It must state the **unreadable** case: fail closed, do not reap. Today nothing says this;
   `orphansweep.go:258-260` happens to `continue`, but D7 shows the neighbouring exclusion
   fails the other way.
5. It must be evaluated in-text against the four hazards the problem space names: pid/pgid
   recycling, a live sibling of the same identity, a peer project on the same box, darwin.

**Beads:** all five. **Category:** amendment.

---

#### **A2 — PL-006a `:372`: the daemon `setsid` obligation**

See D4 for the drift. The amendment question is what the requirement *becomes*. The
problem space is right that hk-g1qby's stated rationale ("PGID is the only usable darwin
signal") does not survive A1. The surviving justification is the one the problem space calls
"valuable and unwritten": a setsid'd daemon gives `br` children a project-scoped group they
presently lack — a candidate fix for hk-c6dt2 that costs one syscall and no new file format.

**What must be true after the change:** `:372` must state which of these it is —
(a) retired (with a pointer to what replaced it), (b) retained as a marker mechanism,
(c) retained for a *different* stated reason (group namespace for unmarked children), or
(d) demoted to spec-conformance-only. And the pidfile line-2 PGID (`:372` sentence 2) must
say what it is *for*, since PL-INV-002's sensor (`:1103`) is currently its only live consumer.

**Bead:** hk-g1qby. **Category:** amendment. **Gated by:** OQ-PL-008 (§5).

---

#### **A3 — PL-006a `:370(ii)` + a new HC clause: PGID as kill handle, not provenance**

The inherited work's kill-path fix is correct and has landed three times in-repo already
(`dot_cascade.go:2108-2113`, `:2269-2277`, `scheduletick.go:255` — all `Setpgid:true` with
`Pgid:0` plus, in the cascade cases, `syscall.Kill(-cmd.Process.Pid, SIGKILL)`; bead
hk-me8ru). That precedent is the strongest argument for it and the inherited design cites it
nowhere.

**What must be true after the change:** the spec must separate the two roles a PGID can play
and say which one applies to handler children — a **kill handle** (own group, so
`kill(-pgid)` reaches descendants) or a **provenance value** (project-constant, so a sweep can
match it). It cannot be both; §5 shows why choosing "handle" forecloses "value."

**Bead:** hk-n93gq. **Category:** amendment. **Gated by:** OQ-PL-008.

---

#### **A4 — PL-021b `:740` §7: substrate kill discipline must reach grandchildren too**

Current text (`:740`):

> "The substrate `Kill` operation MUST issue `tmux kill-window`; SIGKILL escalation is
> delegated to tmux itself."

Goal 2 says the kill path must reach grandchildren "on **both** spawn branches (direct-exec
*and* substrate/tmux-hosted)." A fix confined to `handler.go:309` + `session.go:421` covers
only the first. `tmux kill-window` sends SIGHUP to the pane's foreground process group;
whether that reaches a grandchild the agent forked into a different group is not stated
anywhere and is not tested.

**What must be true after the change:** PL-021b §7 must state the grandchild guarantee for
the substrate path explicitly — either "kill-window is sufficient because X", or a named
escalation, or an honest declaration that substrate-hosted grandchildren are covered only by
PL-021c's window sweep (which per D6 does not SIGKILL). Silence here is what produced C4.

**Bead:** hk-n93gq. **Category:** amendment.

---

#### **A5 — PL-006 `:343` + BI-014a: the `br` sweep must become project-scoped**

Current implementation is `comm == "br" && PPID == 1` (`internal/lifecycle/orphansweepbr.go:64-79`) —
matching on the process *basename*, weaker even than a binary path, with no project scope. It
reaps other projects' and other developers' `br` processes. That is hk-c6dt2, and it is a
live production hazard, not a paper one.

**What must be true after the change:** (a) the BI adapter spawn site must be normatively
obliged to set whatever A1 lands (env marker at minimum; group membership if A2 goes that
way); (b) the sweep must be obliged to match that marker and MUST NOT match on `comm` or
binary path; (c) the fail-closed rule applies — an unmarked `br` process is not ours and is
not touched, even at the cost of leaving a genuine orphan behind (SQLite WAL contention is
recoverable; killing a developer's `br` is not).

**Bead:** hk-c6dt2. **Category:** amendment (plus D1's drift correction).

---

#### **A6 — PL-007 `:475`: strengthen the matching prohibition from "binary path alone" to "argv is never provenance"**

Current text (`:475`):

> "The sweep MUST NOT match on binary path alone and MUST NOT kill a process lacking a valid
> project-scoped marker."

Being precise: this forbids **binary path alone**. It does **not** literally forbid matching
on a *richer* command line — which is exactly the loophole
`ReapPriorAgentFollowWatchers` occupies. Its own package doc admits the design at
`internal/lifecycle/agentwatcherreap.go:19-20`: "identification here is by COMMAND LINE +
AGENT IDENTITY only, never by liveness/parentage." The problem space says `:475` forbids it;
strictly, it does not, and that gap is why the code exists.

The prior-refusals evidence (a live `harmonik comms recv --agent mike --follow` is argv-
identical to a leaked one, and every crew is *required* to keep one armed) shows the rule
should be stronger: **argv is not a provenance signal at all**, at any richness, because
harmonik's own operating contract manufactures argv-identical live twins.

**What must be true after the change:** `:475` must state that identification MUST rest on a
marker the process could only have received from this project's daemon, and that argv /
`comm` / binary path are permissible only as **narrowing filters applied after** a marker
match, never as the match itself. This one sentence is what makes hk-c6dt2's and hk-5z4ww's
current implementations non-conformant, which is the point.

**Beads:** hk-c6dt2, hk-5z4ww. **Category:** amendment.

---

#### **A7 — PL-005 `:315` / `:317` step ordering: the prior daemon's pidfile is destroyed before the sweep can read it**

**NEW FINDING, and it constrains the PL-002b design in §4.**

PL-005 step 1 (`:315`) is "Acquire the pidfile lock (§PL-002)". PL-002b step 3 (`:179`)
mandates `ftruncate(fd, 0)` immediately after lock acquisition, and
`internal/lifecycle/pidfile.go:110` does exactly that. PL-005 step 3 (`:317`) — "Execute the
orphan sweep per §PL-006" — runs **two steps later**.

So by the time the sweep runs, the prior daemon's recorded PGID (line 2) and any recorded
start time (proposed line 4) have already been zeroed by the new daemon. Any design in which
the sweep matches against *the prior generation's* recorded values is unimplementable as the
startup sequence currently stands.

**What must be true after the change:** if the pidfile is to carry provenance data the sweep
consumes, PL-005 must require the prior pidfile's content to be **read into memory at step 1
before `ftruncate`**, and PL-002b must state that obligation on the writer side. Alternatively
the provenance record must live in a file the new daemon does not truncate — which is an
argument for the separate registry of §3.

**Beads:** hk-o7x4w, hk-c6dt2, hk-g1qby (any of them, if their fix reads a prior-generation
recorded value). **Category:** amendment (a step-ordering obligation that does not exist today).

---

#### **A8 — PL-002b `:180`: pidfile schema gains a start-time field**

Full analysis in **§4**. Summary of what must be true after the change: the pidfile records a
value that lets a later reader prove the recorded PID/PGID still refers to *the same process
era*, using a clock the reader can independently observe for an arbitrary pid, and readers
tolerate its absence by failing closed.

**Beads:** hk-o7x4w, hk-g1qby, hk-c6dt2. **Category:** amendment.

---

#### **A9 — OQ-PL-011 `:1381`: its premise dies, its hazard does not**

Current text (`:1381`):

> "PL-006a relies on the recorded PGID for orphan-sweep coverage on darwin; handlers that
> internally call `setsid` (e.g., nohup-style wrappers) escape the marker and the orphan
> sweep cannot reap their descendants."

If A1 removes PGID from the provenance role, the *provenance* framing of OQ-PL-011 is void.
The **kill-handle** hazard survives and is arguably worse under A3: a descendant that calls
`setsid` escapes the group and therefore escapes `kill(-pgid)`.

The inherited draft (`00-inherited-child-pgid/05-spec-drafts/process-lifecycle.md:34`)
re-frames OQ-PL-011 from provenance to kill-handle **without saying it did**. Same defect
class as C3.

**What must be true after the change:** OQ-PL-011 must be re-stated against whichever role
survives, with the re-framing declared, not slipped in.

**Bead:** hk-n93gq. **Category:** amendment.

---

### Gap — implemented, unspecified

#### **G1 — `ReapPriorAgentFollowWatchers` has no requirement anywhere**

Verified: grep for "follow watcher" / "hk-6629b" / any watcher-reap obligation across
`specs/` returns nothing normative — `specs/park-resume-protocol.md` mentions `comms recv
--follow` as a session's *own* monitor, never a reaper. `ReapPriorAgentFollowWatchers`
(`internal/lifecycle/agentwatcherreap.go`) is referenced only from test files. It is complete,
tested, unwired, and unspecified.

**What must be true after the change:** pass 3/4 must choose one of three, and write it down:
(a) a new requirement giving `--follow` watchers a per-session provenance record so the
reaper can identify a *prior* session's watcher (not merely a same-argv one); (b) an explicit
decision that this is out of scope and the code is deleted; (c) an explicit decision that the
capability is deferred, with the code left dead and a dated marker saying so. Leaving it in
the current state — dead code that would violate A6 if wired — is not an option this work can
pass over silently, because hk-5z4ww is one of its five beads.

Note the reaper's stated acceptance criterion ("reap prior same-agent watchers **regardless of
liveness**", `agentwatcherreap.go:14-16`) is fundamentally incompatible with a
liveness-or-parentage-based marker. Whatever marker A1 lands must be able to distinguish
**session generation**, not just project ownership — otherwise (a) is unreachable and the
honest answer is (b) or (c).

**Bead:** hk-5z4ww. **Category:** gap.

---

## 1.2 `specs/handler-contract.md`

#### **HC-044 `:806` — conditional; smaller than the inherited draft assumes**

Current text (`:806`), the relevant span:

> "The daemon MUST spawn every handler subprocess as a direct child process (per
> [process-lifecycle.md §4.5]). … On Linux, handler subprocesses SHOULD install
> `PR_SET_PDEATHSIG(SIGTERM)` at spawn time; macOS has no equivalent and subprocess survival
> across daemon death is a platform reality addressed by §4.10.HC-044a."

**NEW FINDING:** HC-044 says **nothing about process groups**. Not "the child joins the daemon
group," not "the child leads its own group" — the concept is absent. Yet
`internal/handler/session.go:408-410` attributes the joined-group design to it:

> "The handler is spawned with `SysProcAttr{Setpgid: true, Pgid: <daemon_pgid>}`
> (`lifecycle.SpawnChildSysProcAttr` per HC-044 / PL-006a)"

That citation is wrong. Only `PL-006a:372` imposes the group rule; HC-044 imposes none. So the
inherited draft's HC-044 rewrite is an **addition** of a new obligation, not a correction of a
false one — and it should be presented that way in pass 5. Also note `:806`'s "direct child
process" carries the same substrate contradiction as D5.

**Branch map for HC-044 — see §5 for the gating argument:**

| OQ-PL-008 resolves as | Does HC-044 change? | What it must say |
|---|---|---|
| **B1** — durable per-process provenance record (registry / marker file) | **Yes** | PGID is freed from the provenance role, so HC-044 gains the own-group spawn obligation + the group-directed `session.Kill` obligation, and states that the child's PGID is a kill handle with no provenance meaning. |
| **B2** — daemon-group PGID bounded by a recorded start-time conjunct | **No** — and it MUST NOT | Children must stay in the daemon's group for the matcher to have anything to match. HC-044 gains at most a cross-reference. hk-n93gq then has **no fix shape** under this branch (see §5). |
| **B3** — darwin post-crash sweep declared permanently out of scope | **Yes, identically to B1** | The kill path is live-daemon-only and orthogonal to the sweep, so the own-group + group-kill obligations land unchanged; hk-o7x4w is re-scoped or closed won't-fix. |

So HC-044's change is identical under B1 and B3 and **null under B2**. OQ-PL-008 is a real
gate on this file, not a courtesy check.

**Beads:** hk-n93gq (primary), hk-o7x4w (via the branch).

---

#### **HC-018 `:483` — the cleanup bound now applies to a tree**

Current text (`:483`):

> "A `ctx` cancellation MUST cause in-flight Go-side operations to return with
> `context.Canceled` (wrapped as `ErrCanceled` per §4.5) within 500ms, and MUST cause
> subprocess cleanup to complete within 5s."

PL-006 `:343` anchors the sweep's SIGTERM→SIGKILL interval to this bound. Under B1/B3, "the
subprocess" becomes "the subprocess **and its process group**."

**What must be true after the change:** HC-018 must state whether the 5s bound is per-process
or per-group, and that the escalation clock does **not** restart per descendant. A tree of
depth *n* with a per-process 5s bound is a 5*n*-second cleanup, which silently breaks the
bound the sweep depends on. This is a small edit with a real failure mode behind it.

**Bead:** hk-n93gq. **Category:** amendment.

---

#### **HC-044a `:812` — read-only cross-check, no change expected**

HC-044a's orphan detection is a per-run pidfile at `.harmonik/worktrees/<run_id>/.lock` with a
`kill(pid, 0)` liveness probe plus an "argv check" for recycled PIDs. It is a **fourth**
independent provenance scheme (alongside the env marker, the tmux name prefix, and PL-006d's
sentinel files), and it has the same pid-recycling weakness A8 addresses.

**What must be true after the change:** pass 3 should decide whether HC-044a's per-run pidfile
is subsumed by the §3 registry or stays separate. If the registry lands, having HC-044a keep
its own `.lock` + argv check is exactly the accretion this work exists to stop. Flagging it,
not proposing it — no bead currently covers HC-044a.

---

## 1.3 `specs/beads-integration.md` — **newly identified, not in the problem space**

#### **BI-014a `:352` — mandates precisely what PL-007 `:475` forbids**

Current text (`:352`):

> "On daemon startup, the orphan sweep of [process-lifecycle.md §4.2 PL-006] MUST enumerate
> processes **whose binary path matches the pinned `br` location** and whose parent PID is 1
> (re-parented to init)."

PL-007 `:475`: "The sweep MUST NOT match on binary path alone and MUST NOT kill a process
lacking a valid project-scoped marker."

These are a direct spec-vs-spec contradiction, both in force. The implementation obeys
BI-014a and is weaker still (`comm == "br"`, a basename, not a path —
`orphansweepbr.go:64-79`). The problem space's "spec areas affected" list omits this file
entirely, so a fix confined to `process-lifecycle.md` would leave the normative source of
hk-c6dt2's behavior standing.

**What must be true after the change:** BI-014a must be amended in lockstep with A5 —
enumeration by project-scoped marker, with binary-path/`comm` demoted to a post-match
narrowing filter at most, and an explicit fail-closed rule for unmarked `br` processes. The
BI adapter's spawn obligation (A5(a)) belongs here too, since `internal/brcli` is BI's
subsystem, not PL's.

**Bead:** hk-c6dt2. **Category:** drift (the contradiction) + amendment (the new obligation).

---

## 2. Judgment: is PL-006a the right home?

**No. It has become a grab-bag and should be split — but *within* `process-lifecycle.md`, not
into a new file.**

PL-006a currently does four unrelated jobs in one requirement:

1. define `project_hash` (a primitive — consumed by PL-031 `:383`, PL-006d `:449`, PL-021c
   `:758`, the tmux naming builder, and the `harmonik project-hash` CLI);
2. own the tmux **session-name namespace** (`:360-366`, a naming convention);
3. define the **process provenance marker** (`:368-370`);
4. impose a **daemon startup obligation** (`setsid`, `:372`) and a **per-spawn attribute rule**
   (`:372`), plus a **matching rule for the sweep** (`:376`).

Job 1 is a primitive with many consumers and belongs where it is. Job 2 is a naming
convention with its own sweep (PL-006/PL-006d) and is fine where it is. Jobs 3 and 4 are a
different thing entirely — they are a *mechanism with a schema, a lifecycle, and a platform
matrix* — and they are what the five beads are about. Cramming a fifth consumer into `:370`
is how `:376` came to describe a darwin path nobody implemented for two revisions without
anyone noticing.

**Recommendation:** carve a new §4.2a "Process provenance" into `process-lifecycle.md` with a
new requirement family:

- **PL-006e — Provenance record.** What the marker *is*, its schema, where it lives, its
  write/delete discipline (WM-026 atomic per the existing precedent at `:181`), its
  GC/staleness rule, and its per-platform readability.
- **PL-006f — Matcher discipline.** How a reaper evaluates the record: the
  `(identity, start_time)` conjunct, the fail-closed rule, and the prohibition on argv/`comm`
  as a match (A6's teeth live here).
- **PL-006g — Spawn-site register.** The enumerated list of spawn sites, each marked
  *conformant* or *exempt-with-reason*, that makes PL-INV-005's sensor a checkable predicate
  (D2).

PL-006a then keeps jobs 1 and 2 and cross-references §4.2a for job 3/4.

**Why not a new spec file.** Every consumer of the mechanism already lives in
`process-lifecycle.md` §4.2 — PL-006, PL-006d, PL-007, PL-021c, PL-INV-005. A new file forces
a cross-spec hop for the sweep's most-read requirement and buys nothing. It also would not
solve the accretion problem: PL-006a is overloaded because nothing forced a split, and a §4.2a
with three named requirements forces exactly that.

**The condition under which a new file becomes right:** if pass 3 lands a registry with an
RPC/CLI surface (e.g. `harmonik provenance list`), a cross-subsystem write contract (BI
writing `br` records, supervise writing launch-layer records), or a versioned on-disk schema
with N-1 compatibility obligations — then it has become a subsystem, and
`specs/process-provenance.md` is warranted. Judge that at the end of pass 4, not now.

---

## 3. New spec files needed

**None at this time.** One new *section* with three new requirement IDs, as above:

| Proposed | Location | Scope |
|---|---|---|
| §4.2a PL-006e | `specs/process-lifecycle.md`, after PL-006d | The provenance record: schema, location, write/delete discipline, GC, platform readability. |
| §4.2a PL-006f | same | Matcher discipline: identity+start-time conjunct, fail-closed, argv prohibition. |
| §4.2a PL-006g | same | Spawn-site register: conformant vs exempt, with reasons; makes PL-INV-005 checkable. |

Deferred-file decision recorded above.

---

## 4. PL-002b: what the start-time conjunct actually implies

The problem-space direction (operator/captain, 2026-07-22) is that any PGID-based match MUST
be bounded by a start-time conjunct via `ps lstart` or a pidfile-recorded start time, **not**
mtime. Here is what that concretely means against the current requirement.

### Current text — `specs/process-lifecycle.md:180`, step 4 of PL-002b

> "Write the pidfile's three lines, each terminated by `\n`: line 1 = the daemon's PID (ASCII
> decimal integer); line 2 = the daemon's PGID (ASCII decimal integer); line 3 = the daemon's
> `daemon_instance_id` (UUIDv7 per PL-005 step 0; lowercase canonical hyphenated form, 36
> ASCII characters). Short writes MUST loop. Readers MUST tolerate one-line pidfiles for
> backward compatibility with v0.2.x format and two-line pidfiles for backward compatibility
> with v0.4.0 format…"

Implemented at `internal/lifecycle/pidfile.go:121`
(`fmt.Sprintf("%d\n%d\n%s\n", pid, pgid, instanceID)`); read positionally by `ReadPidfile`
(`pidfile.go:177` doc, body to ~`:231`), which trims blanks and indexes `lines[0..2]`.

### The concrete schema change

**Append line 4 = the daemon's process start time, as reported by the kernel, in the same
encoding a reader will obtain for an arbitrary pid.**

Source selection matters more than the field:

- **Do not** record `time.Now()` at PL-005 step 0. That is the daemon's *observation* of when
  it started, not the kernel's record of the process. A sweep comparing it against
  `ps -o lstart=` for a candidate pid needs a fuzz window, and a fuzz window is exactly the
  collision surface the conjunct exists to close.
- **Do** record the value read back from the kernel for the daemon's own pid at startup, via
  the same command the sweep will use (`ps -o lstart= -p <pid>`, normalised to epoch
  seconds). Both sides then read one source and the comparison is exact.
- **Not mtime** — already ruled out by the operator direction, and correctly: file mtime is a
  property of the file, not the process, and survives a `touch`.

**Granularity is sufficient.** `lstart` resolves to one second. Measured pid churn on this box
is ~37 pids/sec with a ~45-minute wrap, so the same pid cannot recur within the same second.
A one-second-granular start time is therefore a *complete* discriminator for pid reuse here,
not an approximation. That should be stated in the spec with the measurement, so a future
reader on a faster box knows the assumption to re-check.

### Backward compatibility — three separate answers

1. **File format: compatible.** Appending line 4 leaves lines 1-3 byte-identical. Existing
   positional readers ignore trailing lines — including the out-of-package reader at
   `:685` (the supervisor config snapshot, which reads "line 3 per PL-002b"), and
   `ReadPidfile` itself, which indexes `lines[0..2]`. No old reader breaks.
2. **Reader tolerance: needs a new normative sentence, and it must fail closed.** The existing
   clause tolerates 1- and 2-line files by defaulting; the natural extension is "missing line
   4 ⇒ start time unknown." But the default must be stated as a *refusal*: an unknown start
   time MUST mean the recorded PID/PGID cannot be used for provenance matching — never "match
   on pid alone as a fallback." Without that sentence, every pidfile written before this
   change becomes a permanent unbounded-match license.
3. **Go API: source-level break, not a format break.** `ReadPidfile` returns
   `(pid int, pgid int, instanceID string, err error)`. A fourth value breaks every caller.
   Recommend a `PidfileRecord` struct — but that is an implementation call and the spec MUST
   NOT mandate the Go signature.

### Two things the pidfile change does *not* buy — state these plainly

- **It authenticates the daemon, not a child.** The pidfile records the *daemon's* pid/pgid.
  If children lead their own groups (A3/B1), the daemon's recorded PGID is not any child's
  PGID and line 4 cannot bound a *child* match. Per-child provenance needs a per-child durable
  record — the §3 registry. The pidfile change is therefore **necessary for branch B2 and for
  hk-g1qby's daemon-group-namespace argument, and nearly useless for B1**. Choosing the
  pidfile route without noticing this is the most likely way this work ships something that
  does not fix hk-o7x4w.
- **It is unreadable at sweep time as the startup sequence stands.** See A7: PL-005 step 1
  truncates the prior generation's pidfile two steps before the step-3 sweep runs. The schema
  change is inert without the accompanying step-ordering obligation.

### Sensor interaction

PL-INV-002's sensor at `:1103` enumerates the pidfile predicate ("parsed PID equals
`getpid()` AND parsed PGID equals `getpgrp()` AND parsed `daemon_instance_id` equals…").
**Recommendation:** line 4 does **not** join that predicate. Adding a conjunct there would
break the v0.4.x-pidfile tolerance the sensor explicitly grants, for no liveness benefit. Line
4 serves provenance matching only, and the spec should say so at the point of definition.

---

## 5. Dependency map, and the OQ-PL-008 ordering claim

### The ordering claim, verified

The problem space asserts: *OQ-PL-008 is not a surfaced non-gate — it is the decision that
determines whether hk-o7x4w has any fix shape left at all; resolving it must come before the
kill-path change or it is silently foreclosed.*

**Verified against the spec text, and I agree — with one refinement to the reasoning.**

The mechanical argument holds. OQ-PL-008's default at `:1363` is "PGID is the primary marker
on darwin," and PL-006a `:376` states it as a MUST. If HC-044 gains "every handler child leads
its own group" (A3), then on darwin the sweep's only PGID-based rule would be "match processes
whose PGID equals their own PID" — which is true of **every group leader on the box**,
including the operator's shells and every peer project. That is not merely vacuous; implemented
literally it is a machine-wide kill. So landing A3 first would leave a MUST in the spec that is
unsafe to implement, and would foreclose branch B2 without a decision record.

**The refinement.** The problem space frames the stake as "hk-o7x4w has no fix shape left." I
would state it more precisely: hk-o7x4w retains a fix shape under B1 (registry) and loses one
under B3 (declared out of scope) — those are unaffected by the ordering. What A3 forecloses is
specifically **B2**, the daemon-group-PGID-plus-start-time option. B2 is a genuinely live
option — it is cheap, it needs no new file format, and it is the only one of the three that
also fixes hk-c6dt2 for free (a setsid'd daemon's group scopes `br` children that carry no
marker of their own). So the gate is real and the ordering claim stands, but the thing at risk
is a *specific viable option*, not "any fix at all."

And B2 has a cost that must be on the table when it is decided: **under B2, hk-n93gq has no
fix.** Children must stay in the daemon's group for the matcher to work, so `session.Kill`
cannot become group-directed, so grandchildren are not reached. B2 buys the darwin sweep by
paying with the grandchild kill path. That trade is the central decision of this work, and it
is invisible in the inherited design, which presents the kill fix as a "surfaced, not a gate"
choice (`04-design/child-pgid-design.md:97-102`).

### Ordering

```
                    ┌──────────────────────────────────────────┐
                    │  OQ-PL-008  (darwin provenance mechanism) │  ← GATE
                    │  B1 registry / B2 daemon-PGID+time / B3 none │
                    └───────────────┬──────────────────────────┘
                                    │
        ┌───────────────────────────┼────────────────────────────┐
        │                           │                            │
        ▼                           ▼                            ▼
   A1 PL-006a marker          A2 PL-006a setsid            A8 PL-002b line 4
   (+ new §4.2a PL-006e/f)    (hk-g1qby: retire vs         (needed for B2;
   D3 fixed here              re-argue on br-group)         near-inert for B1)
        │                           │                            │
        │                           └──────────┬─────────────────┘
        │                                      ▼
        │                            A7 PL-005 step ordering
        │                            (read prior pidfile before ftruncate)
        │
        ├──────────────► A3 PGID-as-kill-handle ──► HC-044 (B1/B3 only) ──► HC-018 bound
        │                                              │
        │                                              └──► A4 PL-021b §7 substrate parity
        │                                              └──► A9 OQ-PL-011 re-framing
        │
        ├──────────────► A6 PL-007 argv prohibition ──┬──► A5 + BI-014a (hk-c6dt2)
        │                                              └──► G1 watcher decision (hk-5z4ww)
        │
        └──────────────► D2/PL-006g spawn-site register ──► PL-INV-005 sensor rewrite
```

**Independent of the gate — start these now, they are pure drift fixes:**
D1 (the false `br` sentence), D5 (the parentage contradiction), D6 (the PPID==1 blindness),
D7 (the fail-open relay exclusion), D8 (the double-booked OQ). None of these depend on which
branch wins, and three of them (D5, D6, D7) are things a reader of the current spec would
reasonably believe and be wrong about.

**Hard ordering constraints:**

1. **OQ-PL-008 before A3, and therefore before any HC-044 edit.** Verified above.
2. **A1 before A6.** The prohibition ("match only on the marker") is unwritable until the
   marker is named.
3. **A8 before A7 is *wrong* — A7 gates A8.** The schema change is inert until the startup
   sequence guarantees the prior generation's content is read before truncation. Decide A7
   first, or A8 ships a field nobody can read.
4. **A6 before A5 and before G1.** Both hk-c6dt2 and hk-5z4ww are argv-matchers; the rule that
   condemns them has to exist before their replacements are specified.
5. **D2/PL-006g last among the PL edits.** The exemption register can only be written once A1
   and A2 have fixed what "the marker" means.

---

## 6. Bead → change coverage

| Bead | Drift fixed | Amendments | Gap | Files |
|---|---|---|---|---|
| **hk-n93gq** (run children join daemon group) | D2 (substrate row), D5 | A3, A4, A9, HC-044 (B1/B3), HC-018 | — | PL, HC |
| **hk-o7x4w** (darwin sweep is a no-op) | **D3**, D6, D7, D8 | A1, A8, A7 | — | PL |
| **hk-g1qby** (`Setsid` never called) | **D4** | A2, A8, A7 | — | PL |
| **hk-c6dt2** (`br` sweep unscoped) | **D1**, BI-014a contradiction | A5, A6 | — | PL, **BI** |
| **hk-5z4ww** (watcher reaper unwired) | D2 (indirect) | A6 | **G1** | PL |

Bolded drift items are the three the problem space named plus D4; the rest are new this pass.

---

## 7. New findings — places the spec is descriptively false that the problem space did not name

Listed explicitly, per instruction, rather than folded in:

1. **D5** — `PL-INV-005:1133` ("daemon MUST be the initial parent") and `HC-044:806` ("direct
   child process") are both contradicted by `PL-021b:732`, which *mandates* `tmux new-window`
   — making the tmux server the parent. Two in-force MUSTs contradict each other; the code
   obeys PL-021b.
2. **D6** — The `PPID==1` candidate filter (`PL-006:343`, `orphansweep.go:244-246`) makes the
   handler sweep structurally unable to see substrate-hosted orphans on **either** platform,
   because a tmux-hosted process's parent is the live tmux server. PL-006's enumeration clause
   claims a completeness it does not have.
3. **D7** — The PL-017a(b) relay-grandchild exclusion **fails open** on darwin
   (`orphansweep.go:270-274` + `provenance.go:151-153`), and is masked today only because the
   environ read `continue`s first. Fixing hk-o7x4w arms it. Violates this work's own
   fail-closed constraint.
4. **D8** — `OQ-PL-008` is double-booked: `:358` files project-hash case-fold ambiguity under
   it, while the OQ body at `:1358-1363` is exclusively about darwin provenance. Resolving one
   silently closes the other.
5. **A7** — `PL-005:315` (acquire pidfile lock) truncates the prior generation's pidfile
   (`PL-002b:179`, `pidfile.go:110`) two steps before the `PL-005:317` orphan sweep runs. Any
   design that reads prior-generation pidfile values at sweep time is unimplementable without
   a step-ordering amendment. This constrains the PL-002b change and was not on the table.
6. **HC-044 group-silence** — `HC-044:806` imposes **no** process-group requirement of any
   kind, yet `session.go:408-410` cites "HC-044 / PL-006a" as the authority for the
   joined-group spawn. The code mis-cites; the inherited draft's HC-044 rewrite is an addition,
   not a correction.
7. **BI-014a `:352`** — mandates binary-path matching, which `PL-007:475` forbids. Direct
   spec-vs-spec contradiction; `specs/beads-integration.md` was missing from the problem
   space's affected-areas list.
8. **D2 refinement** — PL-INV-005's sensor is false at **six** spawn sites in three distinct
   failure modes (env-only / neither / launch-layer), not four uniformly. `dot_cascade.go:2108`
   (local `go build`/`vet`) sets neither half, which the problem space's table did not
   separate out.
9. **HC-044a `:812`** — a **fourth** independent provenance scheme (per-run `.lock` pidfile +
   argv recycling check) with the same pid-recycling weakness. Not covered by any of the five
   beads; flagged so the registry decision does not leave it orphaned.

---

## 8. Open decisions for pass 3/4

**OD-1 (the gate) — OQ-PL-008: what is the darwin provenance mechanism?**
- (a) **B1 — durable per-process record.** A registry the daemon writes at spawn and deletes at
  reap: `(pid, start_time, project_hash, role, run_id)`, atomic per WM-026, GC'd on staleness.
  *For:* darwin-readable, forgery-resistant, project-scoped, survives restart, frees the PGID
  to be a kill handle, has a strong in-repo precedent in PL-006d's sentinel/registry
  owner-proofs (`:449-458`). *Against:* new on-disk schema; write cost per spawn; does **not**
  cover grandchildren (records are per-pid, not inherited); needs a GC story.
- (b) **B2 — daemon-group PGID + pidfile start-time conjunct.** *For:* no new file format;
  fixes hk-c6dt2 for free via a project-scoped group; small. *Against:* forecloses hk-n93gq
  entirely; requires A7's step-ordering change; only works if the daemon actually `setsid`s
  (so it makes hk-g1qby load-bearing).
- (c) **B3 — declare the darwin post-crash handler sweep permanently out of scope.** *For:*
  honest; the live-daemon kill path is the primary leak source and is fixed by A3 regardless.
  *Against:* hk-o7x4w closes won't-fix; D6 shows the tmux path is uncovered either way, so
  this may be closer to the status quo than it looks.

**OD-2 — Does the marker cover grandchildren, or only direct children?**
The orphan leak is a grandchild problem (problem space, "Open questions"). The env marker is
inherited; a per-pid registry record is not. If B1 wins, the spec must state whether
grandchild coverage is provided by env-inheritance on Linux only (asymmetric), by a
group-membership conjunct, or not at all. **This is the question most likely to be skipped and
most likely to make the fix vacuous.**

**OD-3 — hk-g1qby: retire, re-argue, or downgrade?**
- (a) retire `:372`'s setsid MUST with an explicit retirement note;
- (b) re-argue it on the *new* basis (project-scoped group as a namespace for children that
  carry no marker of their own — the `br` case, hk-c6dt2);
- (c) downgrade to spec-conformance-only (implement it because the spec says so, claim no
  behavioral benefit).
Option (b) is the only one that buys something, and it only pays off under B2.

**OD-4 — hk-5z4ww: specify, delete, or defer?** See G1. Note the constraint: the reaper's
acceptance criterion is "reap prior same-agent watchers *regardless of liveness*," so any
marker that only proves project ownership is insufficient — it must prove **session
generation**. If the chosen marker cannot do that, (a) is unreachable.

**OD-5 — Substrate path: does it get its own provenance rule?** Under D5/D6 the tmux-hosted
path has a different parent, a different group, a different reaper, and a different kill verb.
Options: (a) one marker rule with a substrate carve-out; (b) two explicitly-named regimes
(daemon-forked vs substrate-hosted) with separate reapers; (c) declare substrate-hosted
processes exempt from PL-006 and covered solely by PL-021c. Silence here is what produced C4.

**OD-6 — Does PL-INV-005 reach launch-layer spawns?** `supervise/supervisor.go:355`,
`supervisor_watchdog.go:199`, `daemon_watchdog.go:397` spawn harmonik processes with no
marker. Either they enter the PL-006g exemption register with reasons, or PL-INV-005 is
explicitly scoped to *handler* subprocesses only (which its title, "Agent subprocess
parentage," arguably already implies) — but then the `br` sweep and the watcher reaper are
outside the invariant that supposedly governs them.

**OD-7 — Is HC-044a's per-run `.lock` subsumed by the registry?** If B1 wins, four provenance
schemes become five unless HC-044a is folded in. No bead covers this; decide explicitly rather
than by omission.

**OD-8 — Does line 4 of the pidfile join PL-INV-002's sensor?** Recommendation in §4: no.
Needs a one-line decision so the sensor text does not drift again.

---

## 9. Goal → area traceability

| Goal (from 01-problem-space §Goal) | Satisfied by |
|---|---|
| 1. One named provenance mechanism, darwin+Linux, restart-surviving, recycling-safe | OD-1 → A1 + new §4.2a PL-006e/PL-006f; A8 + A7 (start-time binding); D3 fixed |
| 2. Kill path reaches grandchildren on both platforms **and both spawn branches** | A3 + HC-044 (B1/B3) + HC-018 bound; **A4** for the substrate branch (the half C4 missed) |
| 3. Every spawn site marked or explicitly exempted; PL-INV-005 becomes true | D2 → new PL-006g spawn-site register; D5 (parentage clause); OD-6 |
| 4. Every reaper matches the marker, never binary path or command line | A6 (PL-007 `:475`) → A5 + BI-014a (hk-c6dt2); G1/OD-4 (hk-5z4ww); D7 (the exclusion must not fail open) |
| 5. Group leadership is a property of the daemon, not its launcher | A2 / OD-3 (hk-g1qby) |

**Non-goals confirmed untouched:** `waitWithSocketGrace` zero-grace-ctx; keeper restart timing
(hk-pvrfx, hk-4tjyj); `Pdeathsig` beyond preserving the darwin/linux spawn-variant split
(`spawndaemonchild_darwin.go` / `_linux.go` stay split — the darwin file exists precisely
because `Pdeathsig` is absent there).
