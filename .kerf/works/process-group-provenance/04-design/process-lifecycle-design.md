# 04 — Change Design: `specs/process-lifecycle.md`

**Work:** process-group-provenance · **Pass:** 4 (change design) · **Crew:** kilo · **Date:** 2026-07-22
**Mode:** autonomous (no user present at write time; decisions surfaced to captain over comms)
**Inputs:** `02-components.md`; `03-research/{registry-b1,pgid-b2,substrate-path}/findings.md`;
`specs/process-lifecycle.md` @ `ae470edc`; live measurement on the operator's darwin box
(macOS 26.3.2, arm64), recorded in §0.2 and reproducible from the commands quoted there.

---

## 0. The decision this pass had to make, and what actually decided it

### 0.1 OD-1 is resolved: **B4-strict** — portable env marker, argv-strip enforced

Pass 2 framed OD-1 as a three-way choice (B1 durable registry / B2 daemon-group PGID +
start-time / B3 declare out of scope). Pass-3 research added **B4** (give `ReadProcessEnviron`
a darwin implementation via `ps -E`) and showed the premise under the original three — *darwin
cannot read another process's environment* — is **false**.

This pass resolves OD-1 to **B4 plus a mandatory argv-strip rule** (§0.2, finding 3), and
rejects B1, B2 and B3 on the record:

| Option | Disposition | Decisive reason |
|---|---|---|
| **B1** durable per-pid registry | **Rejected** | Records are per-pid and not inherited, so B1 cannot see grandchildren — and grandchildren are the entire observed leak (OD-2). Research: "it fixes the wrong half of the problem." Its one advantage (darwin-native) evaporates once B4 makes darwin readable. |
| **B2** daemon-group PGID + start-time conjunct | **Rejected** | The conjunct authenticates an identity by interrogating the process that holds it; at sweep time the recorded leader is **dead by construction**, so there is nothing to interrogate. Repairing it requires a durable liveness record, at which point it is B1 with extra steps. |
| **B3** declare darwin out of scope | **Rejected** | Only defensible while darwin was believed unreadable. It is readable (measured). |
| **B4-strict** env marker + argv-strip | **ADOPTED** | Portable; inherited, so it reaches grandchildren — the one thing B1 structurally cannot do; no new on-disk schema; no pid/pgid recycling exposure. Its two real weaknesses (forgery, inheritance≠ownership) are closed in this design by PL-006f's strip rule and generation conjunct. |

**But the framing of OD-1 was itself wrong, and that is this pass's main finding.** See §0.2.

### 0.2 Three measurements taken this pass, each of which changes a requirement

These were taken directly, not inherited from the research reports. Each is reproducible.

---

**Finding 1 — THE LONG-LIVED PROCESS POPULATION CARRIES NO MARKER. The daemon handler path
writes it; the launch layer and `br` do not. (Decisive.)**

> **Corrected 2026-07-22 after design review.** The original text of this finding said the
> marker "is carried by nothing in production" and that B1–B4 answered a question about an
> "empty set." That was a **steady-state snapshot generalised into a code fact, and it is
> wrong as stated** — see the code citation below. The decision it drove (B4-strict, with the
> weight on write coverage) is unaffected and stands; the claim needed narrowing, not the
> resolution. Recorded rather than silently rewritten, because a measurement that overreached
> is exactly the failure this work exists to name.

```
$ ps -AEwww -o command= | grep "PATH=" | grep -o "HARMONIK_[A-Z_]*=" | sort | uniq -c
  50 HARMONIK_AGENT=
  44 HARMONIK_PROJECT=
```

`HARMONIK_PROJECT_HASH` — the provenance marker that PL-006a `:370` defines, PL-006 `:343`
sweeps on, PL-007 `:475` requires, and PL-INV-005 `:1135` asserts as a sensor — appeared
**zero times** in that sample. Re-measured 2026-07-22 11:50Z with the daemon **running**
(pid 88350): still zero, across 14 matching processes.

**What the code actually does, and why both statements are true.**
`internal/daemon/workloop.go:1102-1104` (the hk-nvrvp fix, live) prepends
`lifecycle.ProvenanceEnvVar(projectHash)` to the environment of **every handler subprocess**,
and it reaches both the direct-exec and the tmux-hosted path. So the marker *is* written — onto
**handler subprocesses, which are transient**. They exist for the duration of a run and then
exit. Both snapshots above were taken with no handler run in flight, which is why they show
zero and why they are consistent with the code rather than contradicting it.

**The correct statement of the gap, which is narrower and sharper than "nothing writes it":**
the marker's coverage is **inverted with respect to lifetime**. It is present exactly on the
short-lived population and absent exactly on the long-lived one — launch-layer agents,
watchers, `br` — and the long-lived population is the one the sweeps enumerate and the one that
leaks. A reaper that runs at an arbitrary moment is overwhelmingly likely to be looking at
processes that carry nothing.

**Consequence, unchanged in force.** OQ-PL-008 asks *how a reaper reads the marker on darwin*.
Reading was never the binding constraint: on the population that matters, **nothing writes it**.
A work that lands only the read mechanism closes hk-o7x4w with the leak fully open — the precise
failure mode D6 warns about, one level deeper than pass 2 or pass 3 located it. Marker-on-handlers
**supports** the B4-strict resolution (a portable env marker is already the mechanism in use on
the one path that has one); it does not resurrect B1, B2 or B3.

This promotes **OD-6 (does PL-INV-005 reach launch-layer spawns?) from a loose end to the
load-bearing requirement of the work**, and it is why PL-006g (§4) is specified here as
normative and enumerated rather than as documentation.

It also confirms research risk #1 with direct evidence: the leaked population descends from
`harmonik start crew|captain`, which sets `HARMONIK_AGENT` and `HARMONIK_PROJECT` but **not**
the hash. The launch layer is the gap.

---

**Finding 2 — darwin env readability is BINARY-DEPENDENT, not universal.**

The research states darwin exposes "the full exec-time environment of every same-UID process."
Measured: it does not.

```
$ env HARMONIK_PROJECT_HASH=deadbeefcafe sleep 120 &      # /bin/sleep — Apple platform binary
$ ps -Ewww -p <pid> -o command=
 8933 sleep 120                                            # <- NO environment shown
```

while our own binaries do expose it (26 live `harmonik` processes show `PATH=` and their full
env under `ps -E`, as do `claude`, `tmux`, `zsh`). The dividing line is the platform-binary /
hardened-runtime status of the executable, not the UID.

**Consequence.** This is *survivable but must be written*: harmonik's own processes — the only
ones a harmonik reaper may legitimately kill — are readable. But the spec MUST NOT state
"darwin env is readable" as a flat fact. It must state readability as **conditional**, with an
explicit fail-closed rule for the opaque case. Without that sentence, a future implementer
reads an empty env for a hardened binary and has no normative instruction; the D7 evidence
says the neighbouring code fails the *unsafe* way when left to itself.

---

**Finding 3 — a FORGERY VECTOR in the obvious B4 implementation, measured and closed.**

`ps -E` appends the environment onto the **command** column. So a process that merely places
the marker text in its own **argv** is, under the naive match, indistinguishable from one that
truly carries it in its environment:

```
$ sh -c 'exec -a "sleep HARMONIK_PROJECT_HASH=deadbeefcafe" /bin/sleep 90' &
$ ps -ww  -p <pid> -o command=      # argv only
sleep HARMONIK_PROJECT_HASH=deadbeefcafe 90
$ ps -Eww -p <pid> -o command=      # argv + env  -> IDENTICAL
sleep HARMONIK_PROJECT_HASH=deadbeefcafe 90

naive match on the -E line   : 1     <- forgery SUCCEEDS
```

The naive match is what a direct reading of the research's B4 proposal produces. It hands any
local process a **machine-wide kill primitive**: put a project's marker in your argv and the
project's next sweep terminates you — or, aimed the other way, name a victim and get harmonik
to kill it.

**The close is cheap and measured.** Read the process twice — with and without `-E` — and
strip the non-`-E` result as a literal prefix of the `-E` result. What remains is the true
environment:

```
env-only := ps -Eww -p <pid> -o command=   MINUS-PREFIX   ps -ww -p <pid> -o command=

strict match on env-only     : 0     <- forgery DEFEATED
```

Verified simultaneously against the live peer-project daemon (pid 4018), which still matches
correctly under the strict rule. This is normative in PL-006f (§3) — not an implementation
note, because getting it wrong is a security defect and the naive form is the tempting one.

---

**Finding 4 (correction to pass 2) — G1's premise is false: the watcher reaper IS wired.**

`02-components.md` G1 states `ReapPriorAgentFollowWatchers` is "complete, tested, **unwired**,
and unspecified," and OD-4 offers "delete the dead code" as a cost-free option. Verified false:

```
cmd/harmonik/watcherreap.go:34   lifecycle.ReapPriorAgentFollowWatchers(...)
cmd/harmonik/captain.go:486      captainReapPriorWatchers(name)      # captain launch path
                                 crewReapPriorWatchers(...)          # crew start path
```

It is **live on every `harmonik start captain` and `harmonik start crew`**, matching by command
line, which is exactly what A6 is written to forbid. So OD-4 option (b) is not a deletion of
dead code but a removal of shipped behavior, and the A6 prohibition has a **live violation on
the fleet's own launch path**, not a paper one. This is the same defect class the crew's
in-flight `watcher-reap-scope` change addresses at the implementation level; the spec
obligation is designed in §3 (PL-006f) and §5 (OD-4).

---

## 1. Current state — what the spec says today

| Ref | Current text (abridged) | Status |
|---|---|---|
| PL-002b `:180` | Pidfile is three lines: pid, pgid, `daemon_instance_id`. Readers tolerate 1- and 2-line files. | True to code (`pidfile.go:121`). |
| PL-005 `:315`,`:317` | Step 1 acquire pidfile lock (which `ftruncate`s per PL-002b `:179`); step 3 run the orphan sweep. | True, and **destructive**: the prior generation's recorded values are zeroed two steps before the sweep could read them (A7). |
| PL-006 `:343` | Sweep enumerates processes re-parented to init (`PPID==1`); `br` children "bear the same PL-006a provenance marker (env var + PGID)". | **False** on both halves (D1); and `PPID==1` makes the sweep structurally blind to tmux-hosted orphans (D6). |
| PL-006a `:368-370` | Marker is **both** `HARMONIK_PROJECT_HASH` env (read via `/proc`) **and** a deterministic per-project PGID. | Env half is never written (Finding 1); PGID half is never set for provenance. A promise of redundancy delivering zero coverage. |
| PL-006a `:372` | Daemon MUST `syscall.Setsid()` at startup; PGID recorded in pidfile; every handler spawn MUST set `SysProcAttr{Setpgid, Pgid: <recorded>}`. | `SetsidDaemon` has **zero** production callers (D4); the rule holds on only one of two spawn branches. |
| PL-006a `:376` | Sweep MUST match env on Linux and **PGID on darwin**; darwin mechanics tracked as OQ-PL-008. | No PGID branch exists — the sweep never even requests `pgid` (D3). Unsafe if implemented literally after A3. |
| PL-007 `:475` | Sweep MUST NOT match on **binary path alone**; MUST NOT kill a process lacking a valid project-scoped marker. | Forbids too little: a *richer* command-line match is permitted, which is the loophole the live watcher reaper occupies (A6, Finding 4). |
| PL-017a(b) `:270-274` impl | Relay-grandchild exclusion via `ReadProcessCmdlineArgs`. | **Fails open** on darwin (`/proc`-only); masked today only because the environ read `continue`s first. Landing a darwin read **arms** it (D7). |
| PL-021b `:732`,`:740` | Daemon MUST spawn via `tmux new-window`; substrate `Kill` MUST issue `tmux kill-window`. | True to code, and contradicts PL-INV-005 `:1133` / HC-044 `:806` (D5). No grandchild guarantee stated (A4). |
| PL-021c `:752-761` | Window-name-prefix sweep; MUST NOT SIGKILL survivors at MVH. | Never fires as deployed (research §7.4); survivors re-parent to init *after* the sweep that would catch them (D6). |
| PL-INV-005 `:1133-1135` | Daemon MUST be the initial parent; every spawn site MUST set the marker. | False at **six** spawn sites in three distinct modes (D2), plus three launch-layer sites (OD-6) — and Finding 1 shows the sensor is false essentially everywhere. |
| OQ-PL-008 `:1358-1363` | Open: darwin provenance mechanism; default "PGID is the primary marker on darwin." | Premise false; also **double-booked** with a project-hash case-fold question filed at `:358` (D8). |
| OQ-PL-011 `:1381` | Handlers that internally `setsid` escape the marker and the sweep. | Provenance framing dies with A1; the kill-reach hazard survives and worsens (A9). |

---

## 2. Target state — structural change

`process-lifecycle.md` gains **one new section, §4.2a "Process provenance," placed after
PL-006d**, carrying three new requirement IDs. PL-006a is reduced to the two jobs it does well.
No new spec *file* — confirming pass 2 §2's recommendation, and now judged at the end of pass 4
as that section instructed: B4 introduces **no** RPC surface, **no** cross-subsystem write
contract beyond an env var, and **no** versioned on-disk schema, so the "it has become a
subsystem" condition is **not** met. Revisit only if a registry is ever reintroduced.

```
§4.2  ... PL-006, PL-006a, PL-006d ...
§4.2a Process provenance          <- NEW
      PL-006e  Provenance marker: schema, write obligation, platform readability
      PL-006f  Matcher discipline: strip rule, conjuncts, fail-closed, argv prohibition
      PL-006g  Spawn-site register: conformant | exempt-with-reason (makes PL-INV-005 checkable)
```

**PL-006a retains** job 1 (`project_hash` primitive — many consumers) and job 2 (tmux
session-name namespace). Jobs 3 and 4 (the marker; the setsid + per-spawn attribute rules) move
to §4.2a, with `:368-376` replaced by a cross-reference.

---

## 3. Target state — the three new requirements

### PL-006e — Provenance marker (replaces PL-006a `:368-370`)

**Target text intent:**

1. **One mechanism, named.** The provenance marker is the environment variable
   `HARMONIK_PROJECT_HASH=<project_hash>`, set at spawn time. The "BOTH of the following"
   construction is **removed**: PGID ceases to be a provenance value entirely (its surviving
   role is a kill handle, PL-006/A3 and HC-044). This resolves OQ-PL-008 to **B4-strict**.
2. **It binds the process *and its descendants*.** The marker is inherited across `fork`/`exec`
   unless a descendant deliberately clears it. The spec MUST state this explicitly, because it
   is the entire reason B1 was rejected (OD-2) — and MUST state its converse in PL-006f:
   inheritance is evidence of descent, **not** proof of ownership.
3. **Write obligation — the load-bearing clause (Finding 1).** *Every* process harmonik spawns
   MUST receive the marker, including launch-layer spawns (`harmonik start captain|crew`,
   supervisor, watchdogs) and `br` children. A spawn site that does not set it MUST appear in
   the PL-006g register with a reason. This is the clause whose absence makes the current spec
   describe a mechanism that exists nowhere.
4. **Platform readability is conditional, and unreadable means unowned (Finding 2).**
   - Linux: `/proc/<pid>/environ`.
   - darwin: `ps -E` per PL-006f's strip rule.
   - The spec MUST record that darwin exposes the environment **only for non-platform,
     non-hardened binaries**, that harmonik's own binaries qualify, and that an unreadable
     environment MUST be treated as *no marker* ⇒ **not ours** ⇒ **do not reap**. Fail closed,
     stated, not implied.
5. **Value shape.** The marker value MUST be whitespace-free (a hex `project_hash` qualifies).
   Rationale is mechanical, not aesthetic: on darwin the env is recovered from a
   whitespace-delimited column, so a value containing whitespace cannot be parsed unambiguously.
   This is why the marker hashes the project path rather than carrying it — note the live
   counter-example `HARMONIK_PROJECT=/Users/gb/github/harmonik`, which happens to be
   whitespace-free only by luck of directory naming.
6. **Generation nonce (new field, required by OD-4).** Spawns additionally carry
   `HARMONIK_SESSION_GEN=<nonce>`, a per-launch value. The marker answers *which project owns
   this*; the nonce answers *which generation of that project's session spawned it*. Without it,
   "reap **prior** watchers" (G1's acceptance criterion) is undecidable except by argv — which
   A6 forbids and which is exactly the live violation of Finding 4.

**Explicitly evaluated against the four hazards** (A1 requirement 5), in-text in the spec:

| Hazard | PL-006e + PL-006f outcome |
|---|---|
| pid / pgid recycling | **Immune** — no pid or pgid is used as identity anywhere in the matcher. |
| live sibling of the same identity | **Not distinguished by the marker alone** — resolved by the `HARMONIK_SESSION_GEN` conjunct, else the candidate is spared. |
| peer project on the same box | **Distinguished** — the marker is project-scoped. Live confirmation: a peer daemon for `/private/tmp/hk-baseeb2b-lt` runs on this box right now (pid 4018) and is correctly non-matching. |
| darwin | **Readable, conditionally** — Finding 2; opaque ⇒ fail closed. |

### PL-006f — Matcher discipline

**Target text intent:**

1. **The strip rule is NORMATIVE (Finding 3).** On darwin a reaper MUST derive the candidate's
   environment as *the `-E` process listing minus the non-`-E` listing taken as a literal
   prefix*, and MUST evaluate the marker **only** against that remainder. Matching against the
   raw `-E` line is **forbidden**: measured, it accepts a marker forged in argv and thereby
   exposes a machine-wide kill primitive. The spec states the failure, not just the rule, so a
   later "simplification" cannot quietly undo it.
2. **Argv is never provenance (A6, strengthened).** Identification MUST rest on a marker the
   process could only have received from this project's daemon. `argv`, `comm`, and binary path
   are permissible **only as narrowing filters applied after a marker match**, never as the
   match. Rationale to be stated in-text: harmonik's own operating contract *manufactures*
   argv-identical live twins — every crew is required to keep a `comms recv --follow` armed, so
   a live watcher is argv-identical to a leaked one. This clause is what makes the `br` sweep
   (BI-014a) and the launch-path watcher reaper (Finding 4) non-conformant, which is its point.
3. **Fail closed, uniformly.** Any input the matcher needs — marker, generation nonce, or an
   *exclusion* input — that is unreadable MUST cause the candidate to be **skipped**, never
   killed. This closes D7 by construction: PL-017a(b)'s relay-grandchild exclusion currently
   fails **open** on darwin, and landing a darwin read arms it. The rule is stated once here and
   applies to every exclusion, so no future exclusion re-introduces the hazard.
4. **No `(pid)` or `(pgid)` identity, ever.** Neither alone nor together. Where a *generation*
   must be distinguished, use the nonce, not a recorded pid, and not a start time (research
   B2: at sweep time the recorded leader is dead by construction, so a start-time conjunct has
   nothing to compare against).
5. **Ordering.** Marker match → generation conjunct (when the reaper's criterion is
   generational) → narrowing filters → exclusions → kill. Each step may only *reduce* the set.

### PL-006g — Spawn-site register

**Target text intent:** a normative table, one row per spawn site, each **conformant** or
**exempt-with-reason**, such that PL-INV-005's sensor becomes a predicate a reviewer can diff
against `grep -rn SysProcAttr internal/ cmd/`. Initial contents, from D2 plus OD-6 plus
Finding 1:

| Spawn site | Disposition | Note |
|---|---|---|
| `handler.go:309` direct-exec | conformant | The only currently-conformant site. |
| `handler.go:273→426` substrate | conformant (marker) / **exempt** (group) | Marker reaches the child via `tmux new-window -e`; group is assigned by the tmux server. Exempt from the group half; see PL-021b regime split (§5, OD-5). |
| `internal/brcli/adapter.go:146` | **must become conformant** | Sets neither half today (D1). A5 obliges the marker here; BI-014a is amended in lockstep. |
| `scheduletick.go:255` | exempt (group), conformant (marker) | Own group **deliberately** — survives daemon restart. Reason must be recorded, not inferred. |
| `dot_cascade.go:2269` | exempt (group), conformant (marker) | Own group deliberately (hk-me8ru group-kill). |
| `dot_cascade.go:2108` local `go build`/`vet` | **must become conformant (marker)**; exempt (group) | Sets **neither** half today — the site pass 2 newly separated out. |
| `supervise/supervisor.go:355`, `supervisor_watchdog.go:199`, `daemon_watchdog.go:397` | **must become conformant (marker)** | **OD-6 resolved: YES, PL-INV-005 reaches these.** Finding 1 makes this the highest-value row in the table: these launch-layer spawns are why the live marker population is empty. |
| `harmonik start captain` / `start crew` agent + watcher spawns | **must become conformant (marker + nonce)** | Source of the 50 `HARMONIK_AGENT`-bearing, marker-less processes measured in Finding 1, and the population the OD-4 reaper targets. |

**Consequence to state plainly in-text:** an **exempt** process is invisible to PL-006 and
MUST NOT be reaped by it. Exemption is a declaration of *non-coverage*, not a waiver that
permits a looser match.

---

## 4. Target state — amendments to existing requirements

| Ref | Change | Resolves |
|---|---|---|
| **PL-006a `:368-376`** | Replace with a cross-reference to §4.2a. PL-006a keeps `project_hash` and the tmux session-name namespace only. | A1, D3 |
| **PL-006a `:372` (setsid)** | **OD-3 → (a) RETIRE, explicitly.** Add a dated retirement note: the MUST is withdrawn because its stated rationale ("PGID is the only usable darwin signal") is void under PL-006e, and its surviving rationale (a project-scoped group namespace for children carrying no marker of their own) is void because PL-006e obliges **every** child to carry a marker — including `br`, the case that motivated it. Retirement must be **written**, not silent: the inherited draft deletes it invisibly (correction C3) and that must not ship. Note also that `SpawnSysProcAttr` (`provenance.go:59`), whose doc quotes this sentence, likewise has zero production callers. | D4, A2, OD-3 |
| **PL-006 `:343`** | Delete the false `br` sentence (D1). Restate the enumeration honestly: the `PPID==1` filter means this sweep covers **direct-exec orphans only** and is structurally blind to substrate-hosted processes on both platforms, whose parent is the live tmux server (D6). Name PL-021c as the substrate regime's reaper and state what it does **not** do. | D1, D6 |
| **PL-006 / A3** | Handler children lead **their own** process group; `session.Kill` is group-directed so it reaches descendants. PGID is a **kill handle with no provenance meaning** — the two roles are separated in text, and the spec says why they cannot be combined (a "match processes whose PGID equals their own PID" rule matches every group leader on the box: a machine-wide kill). Cite the three in-repo precedents (`dot_cascade.go:2108`,`:2269`, `scheduletick.go:255`, bead hk-me8ru), which the inherited design cites nowhere. | A3 |
| **PL-007 `:475`** | Strengthen per PL-006f(2): from "MUST NOT match on binary path **alone**" to "argv/`comm`/binary path are never provenance; they may only narrow **after** a marker match." | A6 |
| **PL-017a(b)** | Exclusion inputs must be portable (`ps -o args=`, already used by `agentwatcherreap.go:60`) **or** the candidate is skipped. Covered by PL-006f(3); cross-reference it here so the hazard cannot be re-opened locally. | D7 |
| **PL-021b `:740` §7** | State the grandchild guarantee for the substrate path explicitly. Per research §4, `tmux kill-window` does **not** reach a descendant that `setsid`s away. Target: an **honest declaration** — substrate-hosted grandchildren that leave the pane's session are covered by **nothing**, plus a named escalation for those that remain. Silence here is what produced C4; see §6. | A4 |
| **PL-021c** | Either wire the window-name sentinel (`handler.go:428` / `tmuxsubstrate.go:895`) or retire it. Research: it has **never fired and cannot fire as deployed** — a normative promise attached to dead code. Recommend **retire**, and let PL-021b's session-level sweep (44 real kills) carry the regime. | D6, research risk 4 |
| **PL-INV-005 `:1133`** | Delete the "daemon MUST be the initial parent" clause; it is contradicted by PL-021b `:732`, which *mandates* `tmux new-window`, and the code obeys PL-021b (D5). Replace with parentage-per-regime (§5). | D5 |
| **PL-INV-005 `:1135`** | Restate the sensor against PL-006e and make it checkable via the PL-006g register. | D2, OD-6 |
| **PL-002b `:180`** | **A8 is WITHDRAWN — no line 4.** See §4.1. | A8, OD-8 |
| **PL-005 `:315`/`:317`** | **A7 is WITHDRAWN as a spec change** — see §4.1; the ordering hazard is documented as a *rationale note* only. | A7 |
| **OQ-PL-008 `:1358`** | **RESOLVED** to B4-strict. Before resolving, **split out the project-hash case-fold question** filed at `:358` into its own OQ — resolving provenance must not silently close an unrelated question (D8). | OQ-PL-008, D8 |
| **OQ-PL-011 `:1381`** | Re-state against the surviving role, **declaring the re-framing**: no longer a provenance escape (PGID is not provenance), now a **kill-reach** hazard — and worse under A3, since a descendant that `setsid`s escapes `kill(-pgid)`. Measured relevance: this is not hypothetical, it is the production leak (§6). | A9 |

### 4.1 Two pass-2 amendments are withdrawn, on evidence

**A8 (pidfile line 4 = start time) — withdrawn.** Pass 2 carried it as a near-certainty and
§4 designed its schema. Both pass-3 reports and this pass's Finding 1 remove its purpose:

- Its function was to bound a **pid/pgid** match against recycling. Under B4-strict no pid or
  pgid is ever an identity, so there is nothing to bound.
- Research B2 §1 shows the conjunct cannot work in its intended shape anyway: at sweep time the
  recorded leader is dead by construction, so no counterpart exists to compare against.
- Pass 2 §4 itself notes it is "necessary for B2 and **nearly useless for B1**" — and B4 is
  further from the pidfile than B1 is.
- The measured recycling margin is also 5× smaller than the work assumed (~195 pids/s, ~511 s
  wrap, not ~37/s, ~45 min) — which weakens, not strengthens, any second-granular conjunct.

Adding a field with no consumer is precisely the accretion this work exists to stop. **OD-8 is
therefore moot and resolved by construction:** line 4 does not join PL-INV-002's sensor because
line 4 does not exist. The pidfile schema is unchanged.

**A7 (PL-005 step-ordering: read the prior pidfile before `ftruncate`) — withdrawn as a
change.** Pass 2 §5 makes A7 gate A8. With A8 withdrawn, no design reads prior-generation
pidfile values at sweep time, so the ordering hazard has no victim. It is retained as a
**rationale note** under PL-005 — "any future design that reads prior-generation pidfile
content at sweep time must first move that read ahead of step 1's truncation" — so the trap is
documented for whoever next reaches for it, without changing a startup sequence that no current
requirement needs changed.

---

## 5. The remaining open decisions, resolved

| OD | Resolution | Basis |
|---|---|---|
| **OD-1** | **B4-strict**: portable env marker + normative argv-strip. B1/B2/B3 rejected. Re-framed: the gate is marker **write coverage** (PL-006e(3), PL-006g), not the read mechanism. | §0.1, §0.2 Finding 1 |
| **OD-2** | Grandchild coverage comes from **env inheritance, on both platforms** — and from nothing else. This is what kills B1 on the merits. Stated with its limit: inheritance stops at any descendant that clears the variable, and it does **not** confer ownership (PL-006f(2)). | research §4.3; §0.1 |
| **OD-3** | **(a) Retire** `:372`'s setsid MUST, explicitly and dated. Option (b) "re-argue on the `br`-group basis" is void: PL-006e obliges `br` to carry the marker, so it needs no group namespace. | §4; D4 |
| **OD-4** | **(a) is now reachable — specify it.** G1's "unwired" premise is false (Finding 4): the reaper is live on both launch paths and matches by command line. Target: the reaper MUST gate on the PL-006e marker **and** the `HARMONIK_SESSION_GEN` nonce, with argv permitted only as a post-match narrowing filter; unreadable input ⇒ skip. This gives "reap **prior**-generation watchers regardless of liveness" a decidable basis for the first time — the nonce, not liveness, not parentage, not argv. Deletion (b) is off the table: it would remove shipped behavior, not dead code. | Finding 4; PL-006f |
| **OD-5** | **(b) Two explicitly-named regimes**, each with its own reaper and kill verb — plus, mandatorily, a named statement of what **neither** covers (§6). | D5, D6, research §10 |
| **OD-6** | **YES — PL-INV-005 reaches launch-layer spawns.** Promoted from loose end to the work's load-bearing requirement: Finding 1 shows these sites are exactly why the marker population is empty. Enumerated in the PL-006g register. | §0.2 Finding 1 |
| **OD-7** | **Retire HC-044a's per-run `.lock`.** Research: it was **never implemented**, so retiring costs nothing and the count of provenance schemes goes *down*. Detail in `handler-contract-design.md`. | research B1 §exec-summary |
| **OD-8** | **Moot** — no line 4 exists to join the sensor (§4.1). The `:358` case-fold question is still split out of OQ-PL-008 before it is resolved (D8). | §4.1 |

---

## 6. The thing this design refuses to leave unsaid

**Goal 2 — "the kill path reaches grandchildren on both spawn branches" — is achievable for the
orphaned case and NOT achievable for the still-parented one, and the spec must say exactly
which is which.**

> **Corrected 2026-07-22 after design review.** This section previously declared the whole
> `setsid` grandchild population unreachable — "no marker, group, session, or registry
> mechanism on the table reaches it." **That was too pessimistic, and being too pessimistic
> here is not the safe direction.** It tells an implementer the real leak is unfixable and
> invites closing hk-o7x4w as a known gap at the moment this design actually fixes it. The
> correction is recorded rather than quietly swapped in, because the overstatement was in the
> row this work was proudest of.

Measured in research §4: Claude Code `setsid()`s every subprocess it spawns, so such a process
leaves the pane's session and group entirely. That much holds. What does not hold is treating
the resulting population as one thing. **It splits by whether the root of its parent chain is
still alive**, and the two halves have opposite outcomes:

**(a) Orphaned to init — REACHED by this design.** When the spawning agent (and, for the tmux
path, its session) is gone, the `setsid`'d descendant is re-parented to init, so `PPID==1`.
`lifecycle.OSHandlerProcessLister.ListOrphanHandlerPIDs` (`orphansweep.go:194-264`) selects on
`PPID==1` **plus** a provenance-marker match and is **origin-agnostic** — it never asks *how*
the process became parentless, so a `setsid` origin is not excluded. Two things currently stop
it, and **both are in scope here**: the marker is absent from this population (PL-006e(3) write
coverage) and the darwin read path falls through to `continue` because `/proc` does not exist
(B4's `ps -E` read, Finding 2). With those two landed, this design reaps it.

*Live instance, measured 2026-07-22 11:41Z:* pid 8610, a `harmonik comms recv --follow` for
agent `mike`, `PPID==1`, started 22:47 Jul 21 — thirteen hours old, its spawning session long
gone, still holding a daemon subscriber slot. Independently observed twice this cycle (crew
boot sweep and the design reviewer). It carries no marker today, which is precisely why it is
still alive; it is not structurally out of reach.

**(b) Still parented, root alive — NOT covered.** While the spawning agent or tmux server is
alive, the `setsid`'d descendant has a real parent, so `PPID != 1` and the sweep does not see
it; `kill(-pgid)` does not reach it (it left the group); `tmux kill-window` does not reach it
(it left the session). This is the transient case — the window between the descendant
detaching and its root dying — and nothing on the table covers it.

| Regime | Spawned by | Reaper | Kill verb | Grandchild reach |
|---|---|---|---|---|
| **daemon-forked** (direct-exec) | daemon, `handler.go:309` | PL-006 sweep (`PPID==1` + PL-006e marker) | `kill(-pgid)` per A3 | Reaches descendants **that remain in the group**. |
| **substrate-hosted** (tmux) | tmux server, `handler.go:426` | PL-021b session sweep | `tmux kill-window` / `kill-session` | Reaches descendants **that remain in the pane's session**. |
| **orphaned `setsid` descendant** (either origin) | any descendant that called `setsid`, root now dead | PL-006 sweep — **origin-agnostic**, `orphansweep.go:194-264` | `kill(pid)` | **COVERED**, conditional on PL-006e(3) marker write coverage **and** the B4 darwin `ps -E` read. Both in scope. |
| **still-parented `setsid` descendant** (root alive) | any descendant that called `setsid`, root still alive | **none** | **none** | **NOT COVERED.** Named, dated, with the reason. |

Writing that last row is still the most important editorial act of this work, and it is now
also *true at the width it claims*. Pass 2 named the risk (C4: "silence here is what produced
C4"); research §10 named it again ("say so, or ship C4 again"). An honest non-coverage row
converts a silent leak into a known, bounded gap with a name. **And the correction cuts the
other way too:** a non-coverage row drawn wider than the evidence supports is its own failure —
it hides a fix behind a disclaimer, and would have let hk-o7x4w be closed as unfixable when
this design closes it.

**Follow-on, out of scope here, recorded so it is not lost:** the remaining uncovered half —
the still-parented `setsid` descendant — is reachable only by a periodic marker-scan reaper
that does not filter on parentage at all (PL-006e makes this possible on both platforms, since
the marker is inherited even across `setsid`), or by process-level containment
(sandbox/cgroup/jail). Both are new subsystems and neither belongs in this work. Note the
shape of it: the existing sweep is already marker-based and origin-agnostic; the only thing
standing between it and full coverage is its `PPID==1` pre-filter. Dropping that filter is a
small edit with a large blast radius — it turns a sweep over a handful of orphans into a sweep
over every marked process on the box — which is why it needs its own work with its own guards,
not a line appended here. PL-006e is the precondition for either route, and that is the forward
value of this change.

---

## 7. Requirements traceability

Every pass-2 requirement → a target state; no target state without a requirement.

| Pass-2 item | Addressed by | Where |
|---|---|---|
| D1 `br` marker sentence false | Delete sentence; A5 obligation in PL-006e(3) + PL-006g | §4, `beads-integration-design.md` |
| D2 sensor false at six sites | PL-006g register; PL-INV-005 `:1135` restated | §3, §4 |
| D3 darwin PGID matching unimplemented | PL-006a `:376` replaced by PL-006e/f | §3, §4 |
| D4 setsid MUST has no callers | OD-3 → explicit retirement | §4, §5 |
| D5 parentage contradiction | PL-INV-005 `:1133` clause deleted; parentage per regime | §4, §6 |
| D6 `PPID==1` blindness | PL-006 `:343` enumeration restated; regime table | §4, §6 |
| D7 relay exclusion fails open | PL-006f(3) universal fail-closed rule | §3 |
| D8 OQ double-booked | Split `:358` case-fold OQ before resolving OQ-PL-008 | §4 |
| A1 redefine marker | PL-006e (all six clauses + hazard table) | §3 |
| A2 setsid obligation | OD-3 → retire | §4, §5 |
| A3 PGID as kill handle | PL-006/A3 row; roles separated with rationale | §4 |
| A4 substrate grandchild reach | PL-021b §7 honest declaration | §4, §6 |
| A5 `br` sweep project-scoped | PL-006e(3) write obligation + PL-006f match rule | §3, `beads-integration-design.md` |
| A6 argv never provenance | PL-006f(2); PL-007 `:475` strengthened | §3, §4 |
| A7 step ordering | **Withdrawn**; retained as rationale note | §4.1 |
| A8 pidfile line 4 | **Withdrawn** with evidence | §4.1 |
| A9 OQ-PL-011 re-framing | Re-stated as kill-reach, re-framing declared | §4 |
| G1 watcher reaper unspecified | OD-4 → specify; premise corrected (it is wired) | §0.2 Finding 4, §5 |
| OD-1…OD-8 | All resolved | §5 |
| Goal 1 one named mechanism | PL-006e | §3 |
| Goal 2 grandchild kill reach | Partially achievable — **non-coverage declared** | §6 |
| Goal 3 every spawn site marked or exempt | PL-006g register | §3 |
| Goal 4 reapers match the marker | PL-006f(2), PL-007, BI-014a, OD-4 | §3, §4 |
| Goal 5 group leadership is the daemon's | **Dissolved** — OD-3 retires it; recorded, not silently dropped | §4, §5 |

**Cross-area consistency:** no target state here contradicts `handler-contract-design.md` or
`beads-integration-design.md`; the shared dependency (OQ-PL-008 → B4-strict → HC-044 changes
under the B1/B3 branch shape) is resolved consistently in all three — see
`handler-contract-design.md` §1.

---

## 8. Risks this design carries into pass 5

1. **Marker write coverage is a wide diff.** PL-006e(3) obliges ~9 spawn sites, several outside
   `internal/daemon`. Sequence it: launch layer first (largest measured gap), `br` second, the
   two `dot_cascade` sites last.
2. **Widening the marker widens the blast radius.** Research risk 2, and it is real: a marker on
   more processes means more candidates match. This is *safe only because* PL-006f's conjuncts
   and fail-closed rule land **in the same change**. Marker-write and matcher-discipline MUST
   ship together; splitting them ships the hazard without the guard.
3. **The strip rule costs a second `ps` per candidate.** Negligible at sweep cardinality; state
   the cost in-spec so it is not "optimised away" back into the forgeable single call.
4. **`HARMONIK_SESSION_GEN` is a new variable**, needing a mint point and a definition of
   "generation." **The per-`harmonik start <role>` mint proposed here is WRONG and must not be
   carried into pass 5** — a keeper restart is a `/clear` in the same pane driven by the same
   live agent process and never re-invokes `harmonik start`, so every generation it produces
   would share one nonce, and keeper restart is the event that creates the duplicate watchers.
   Measured, with the environments of three same-pane generations compared side by side, in
   `FINDING-generation-nonce-mint-point.md` (this directory, 2026-07-22). That file also shows
   the keeper's `tmux setenv` hook cannot serve as the mint point (it reaches new panes, not the
   children of an already-running process) and proposes the replacement: the harmonik CLI stamps
   the generation onto a long-lived watcher **at arm time** by reading the harmonik-owned
   `.managed` file the keeper already updates every cycle. Pass 5 must define it precisely or
   OD-4 regresses to argv.
5. **`ps -E` truncation.** ARG_MAX-bounded output can truncate a long environment; truncation
   fails closed (no match ⇒ no reap), which is correct, but it means a legitimately-owned
   process can be missed. Acceptable, and must be stated rather than discovered.
