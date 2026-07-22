# The agent substrate — start here

**Date:** 2026-07-16

## What this folder is

Greg has three machines (a laptop, a DGX box, a Mac mini), all his, all on one Tailscale network, all running coding agents. Nothing an agent learns on one machine reaches the others, and an agent that wants to talk to another box has to rediscover how to get there every time. This folder answers one question: **what should we build so the fleet has a shared memory and a shared address book, and so agents on different machines can send each other messages — without any single machine being in charge?**

The answer is a design, an ordered build plan, and the evidence behind both. The recommendation is decisive: build a small daemon (**`fleetd`**, one per box) whose only jobs are moving bytes between named mailboxes, writing bytes to local disk on request, and keeping an honest list of which boxes are reachable — with everything that understands *agents, logs, or SSH* living in swappable plugins on top.

---

## Start here — three docs, in order

1. **`ARCHITECTURE.md`** — the design. What to build and why, in plain language, with every claim labelled measured / verified / deduced. Read §1 (the pitch) and §6 (the transport verdict) first if you read nothing else.
2. **`ROADMAP.md`** — the build order. Eight milestones, each ending in a demo you can watch. Starts with a half-day test that can kill the whole plan before any real code is written.
3. **`BRIEF.md`** — the problem statement, in Greg's own words. It is the authority; the other two answer to it. Read it if a decision in the design ever looks arbitrary — the reason is almost always a quote here.

Everything else in `design/` and `investigate/` is supporting evidence. You do not need it to make the decision; it is there so you can check the work.

---

## The headline answer (what to build, and what to reuse vs write)

- **One daemon per box, `fleetd`, split into a tiny kernel and swappable plugins.** The kernel is 19 remote-procedure calls (a way to call a function that lives in another program) and knows only about *bytes, boxes, and disk* — never about an agent, a log, or an SSH key. Everything that understands those things is a plugin: a separate ordinary program the daemon launches and can swap while everything keeps running. **This split is Greg's own idea (BRIEF §3) and it is the whole bet.**
- **Transport: reuse `nats-server` as an embedded Go library, with its durable-storage feature (JetStream) turned OFF.** Each daemon runs its own in-process message broker; the three brokers mesh directly; there is no machine in the middle. This is measured to survive two of three boxes dying, with the survivor still serving everything. **This overrules Greg's stated lean away from NATS** — argued out loud in §6, not slid past him. (See the ZeroMQ answer below.)
- **Roster + address book: write it ourselves (~400 lines), do NOT reuse a library.** The obvious choice (`memberlist`) has a headline feature that, verified in its own source, switches itself off on a 3-box fleet. A static list of boxes from a config file plus a 1-second health probe is what Greg described and what fits.
- **Live reload: reuse `hashicorp/go-plugin`.** Plugins are separate processes; reloading one means killing the child and starting the new binary — measured at 4–9 milliseconds, with the daemon never stopping.
- **Storage: reuse SQLite** (the pure-Go build, so it cross-compiles to the DGX with no C compiler). Box-local, never replicated: if you want data on another box, you publish it on a channel. **Durability is a per-message choice the plugin makes, never a kernel guarantee** — which is exactly what BRIEF §4 asked for.

Two things Greg should be told rather than left to notice: **he listed five channel types and gets four** (the fifth, "fanout," already ships under two other names — see ARCHITECTURE §4.1), and **we are recommending against his NATS lean.** Both are flagged in the design, not buried.

---

## The ZeroMQ answer (he asked twice; here it is)

**Greg's instinct was half right, and the honest answer tells him which half.** He was right that the *shape* he wanted from ZeroMQ — a messaging pattern baked into a library, with no central server — is the correct shape, and the final design keeps it. He was wrong that ZeroMQ the *library* is the way to get it, and he was wrong that plain NATS can't give him the "no box in charge" property he actually cares about. The ZeroMQ library loses on three fleet-specific facts, not on taste: (1) its mature Go binding wraps C and **cannot cross-compile to the DGX**, which has no C or Go compiler; (2) by its own official Guide, a publisher **cannot tell whether anyone is listening** — which is precisely the "notify the sender the receiver isn't there" feature BRIEF §5 asks for, refused at the architecture level; and (3) ZeroMQ's *own* answer to durable messaging routes through a central **broker** — the exact hub that sank the previous round. So: **ZeroMQ, no — and its own documentation explains why.** The thing Greg feared in NATS is real but misaimed: it lives in JetStream (the disk-persistence add-on, which runs a majority-vote algorithm that stalls when two boxes are down), and we turn JetStream off, leaving a plain in-process broker on every box with nothing in the middle. That is the not-centralized system he wanted, measured by killing two of three boxes and watching the third keep working.

---

## Live reload — what he actually gets

He gets what he asked for: **swap a plugin's code in about 4–9 milliseconds while the daemon and every other plugin keep running**, plus crash isolation for free (a plugin that panics cannot take the daemon down). He does **not** get Erlang's BEAM: a plugin cannot carry its in-memory state across a reload (state has to live in the kernel's storage instead), and the kernel itself still needs a restart to change — which is why keeping the kernel small is a hard rule, not a preference.

---

## The walking skeleton — what to build first

Build the smallest thing that proves the kernel/plugin split works across two machines: `fleetd` with just 4 of the 19 kernel calls, the NATS mesh, a SQLite journal, the probe loop, and one trivial `echo` plugin (~1,200 lines total). Demo it with four commands — one of which is **designed to fail**, publishing to a sleeping box and losing the message forever, so Greg *sees* at-most-once delivery working as intended before anything is built on top of it. But do that only after **Milestone 0**: a real overnight laptop-sleep test that three separate designs flagged as their top risk and none actually ran — if the mesh doesn't re-form on wake, the transport decision reopens before a line of production code is written.

---

## File index

Read top-to-bottom order is the three "Start here" docs; the rest is evidence, grouped by when you'd reach for it.

**Front door and authority**
- `00-README.md` — this file.
- `BRIEF.md` — the authoritative problem statement, in Greg's words. Read early; it settles every "why."
- `ARCHITECTURE.md` — the design. The main deliverable. §13 records what the critique changed.
- `ROADMAP.md` — the build order, eight milestones, each with a watchable demo. §7 records critique changes.

**The contract (read when you start building)**
- `design/25-reconciled-proto/fleet/kernel/v1/kernel.proto` — **the authoritative kernel interface, the 19 RPCs.** Verified this session: lints clean, builds, boundary-clean.
- `design/25-reconciled-proto/fleet/kernel/v1/plugin.proto` — the plugin manifest and lifecycle interface.
- `design/25-reconciled-proto/buf.yaml` — protobuf lint/build config for the above.
- `design/20-kernel-boundary-test.sh` — the test that mechanically proves the kernel names no domain concept. Run it against the proto; it prints `VOCABULARY CLEAN`.
- `design/20-kernel-proto/` — the *earlier* proto draft that the reconciled one supersedes. Historical; don't build from it.

**Subsystem designs (read the one you're implementing)**
- `design/20-kernel.md` — the kernel: channels, storage, the boundary, the 19 RPCs.
- `design/21-transport.md` — the NATS-vs-alternatives measurements: the 2-of-3-kill test, the 61s→5s ACK-window fix, the JetStream/Raft grep.
- `design/22-plugin-system.md` — live reload: go-plugin, the drain gate, the WASM/Yaegi comparison, the plugin library.
- `design/23-roster-addressbook.md` — the roster: why hand-rolled beats memberlist, the liveness states, the address book.
- `design/23-roster-probe-prototype.go.txt` — the probe-loop prototype the roster design measured.
- `design/24-validation-plugins.md` — the plugins (comms, registry, logtail, ssh) designed against the kernel API, which is how the API was stress-tested; also the ACK "four-rung ladder."
- `design/29-critique.md` — the adversarial review of docs 20–24. Read it to see the design attacked; its findings are reconciled in ARCHITECTURE §13.

**Investigations (read for the raw evidence behind a claim)**
- `investigate/10-zeromq.md` (+ `10-zeromq-experiments/`) — the ZeroMQ / mangos experiments and the "a broker is where the messages live" reframing.
- `investigate/11-transport-options.md` — the transport option survey. **Caveat: this is the source of the fabricated "broker on every node = Greg's idea" attribution; treat its framing of Greg's preferences with care (see ARCHITECTURE §13).**
- `investigate/12-plugins-live-reload.md` — the live-reload option space (go-plugin, stdlib plugin, Yaegi, WASM) with measurements.
- `investigate/13-roster-and-addressbook.md` — the roster/address-book survey; measured memberlist and recommended it (the roster design later overturned that).
- `investigate/14-prior-art.md` — how Istio, Nomad, Vault, Telegraf solved the same problems.
- `investigate/15-salvage.md` — what was recoverable from the previous research round and from harmonik (the existing comms code).

---

## The honest take (the critic's strongest surviving points)

- **A quote was put in Greg's mouth, and it mattered.** Two subsystem docs justified overruling his NATS preference by claiming "a broker on every node" was his own idea, in quotation marks. It is not in the brief and not anywhere — it was invented in an investigation and quoted back as fact. That is precisely the failure mode BRIEF §9 warned about, at the highest-stakes decision. It leaked into ARCHITECTURE §6 and has been struck; the technical case for NATS never needed it and stands without it. This is the single most important correction in the review.
- **The kernel is on the edge of "light."** It is 19 RPCs, and 11 of them are storage-shaped. The design defends each one with a named consumer, but Greg's own thesis is "the core is actually really light," and a heavy kernel is a failed kernel. The ~3,000-line tripwire is a real kill criterion, not decoration.
- **Nobody has actually closed the laptop lid.** The whole "the laptop is just another peer" premise rests on a macOS sleep behaving the way three simulations *guessed* it would. This is why Milestone 0 exists and why it gates everything.
- **LOOKUP is the one piece with no library to lean on** (~150–250 lines), and everything routes through it (name resolution, host keys, agent registry). It has a subtle correctness edge on disk-wipe that is now down to ~10 lines (reusing the roster's `boot_id`) but is still unbuilt.
- **The good news, stated plainly because the critic verified it:** zero hallucinated technical facts across the whole corpus — memberlist, NATS, go-plugin, harmonik, Tailscale claims all checked, several to the exact line. No hub was re-imported. Nothing Greg asked for was quietly cut. The problem was duplication and one bad attribution, not fabrication.

---

## Open questions for Greg (real forks only, each with a recommendation)

1. **We are overruling your lean away from NATS. Do you accept it?** The system embeds NATS as a library with its risky feature (JetStream) turned off, giving you the "no box in charge" property you wanted, measured. **Recommendation: accept it.** The blast radius if you say no is tiny — the transport hides behind a 6-method interface in one file, so rejecting it costs one day and changes no API. But it's your explicit preference we're overriding, so it's your call to confirm.
2. **You listed five channel types; you're getting four.** The fifth, "fanout," is ambiguous and both of its meanings already ship (broadcast = PUBSUB, work-queue = POINT_TO_POINT). **Recommendation: four is right; confirm it.** 30 seconds of your time, blocks nothing.
3. **The plugin library — build it, or is `scp` fine?** Your "[Idea]" of syncing plugins across boxes automatically is real but it's the one component that could break all three boxes at once. **Recommendation: don't build it until copying two binaries by hand is actually annoying** — with four plugins and one operator, it may never be. It's sequenced last (M8) with an explicit "only if the chore is real" gate.
4. **Tailscale SSH (a five-minute win) — flip it on?** Running `tailscale set --ssh` on the DGX and granting it in the Tailscale access rules (its "ACL," the allow-list Tailscale already maintains) deletes most of the SSH-key-distribution work the `ssh` plugin would otherwise do. **Recommendation: do it, and keep plain SSH as a fallback** (nobody has verified Tailscale SSH survives a control-plane outage).

---

## Provenance

- **Investigated fresh this round** (exact filenames in the File Index above): the ZeroMQ/mangos experiments (`investigate/10-zeromq.md`), transport options and NATS measurements (`investigate/11-transport-options.md`, `design/21-transport.md`), live-reload options (`investigate/12-plugins-live-reload.md`, `design/22-plugin-system.md`), roster/address-book (`investigate/13-roster-and-addressbook.md`, `design/23-roster-addressbook.md`), prior art (`investigate/14-prior-art.md`), and the plugin designs that stress-tested the kernel API (`design/24-validation-plugins.md`). The kernel design is `design/20-kernel.md`; its authoritative proto is `design/25-reconciled-proto/`.
- **Salvaged from the previous round** (`~/research/2026-07-15-agent-comms-substrate/`, kept quarantined as a framing-poison risk): only its *mechanism* findings — measurements and code reading — were reused, via `investigate/15-salvage.md`. Every *conclusion* from that round that rested on its bad brief (build nothing, NATS hub on the DGX, cut the plugins, cut request/reply, reject the roster) is void and was re-derived against this brief.
- **Measured vs deduced:** ARCHITECTURE.md labels every load-bearing claim `[MEASURED]`, `[VERIFIED]`, `[CLAIMED]`, or `[DEDUCED]` at the point of use, and §11 lists what is still deduced and unbuilt (the sleep test, LOOKUP replication, packet-loss behaviour, macOS binary quarantine). This README re-verified independently, this session: the reconciled proto lints clean, builds clean, has exactly 19 RPCs, and passes the boundary test (`VOCABULARY CLEAN`); and the word "broker" does not appear in `BRIEF.md`, confirming the fabricated-quote finding.

---

## Sources

**Files read in full this session:**
- `/Users/gb/research/2026-07-15-agent-substrate-v2/BRIEF.md`
- `/Users/gb/research/2026-07-15-agent-substrate-v2/ARCHITECTURE.md`
- `/Users/gb/research/2026-07-15-agent-substrate-v2/ROADMAP.md`
- `/Users/gb/research/2026-07-15-agent-substrate-v2/design/29-critique.md`
- `/Users/gb/research/2026-07-15-agent-substrate-v2/design/25-reconciled-proto/fleet/kernel/v1/kernel.proto`
- `/Users/gb/research/2026-07-15-agent-substrate-v2/design/25-reconciled-proto/fleet/kernel/v1/plugin.proto`
- `/Users/gb/research/2026-07-15-agent-substrate-v2/design/20-kernel-boundary-test.sh`

**Files read in part:** `design/20-kernel.md` (§6, §11), `design/21-transport.md` (§91 context), `investigate/11-transport-options.md` (broker-quote provenance, lines 45/378/448).

**Commands run this session (verification):**
- `grep -in "broker" BRIEF.md` → no matches (confirms the fabricated-quote finding).
- `buf lint` / `buf build` on `design/25-reconciled-proto` → both rc=0.
- `grep -c "  rpc " kernel.proto` → 19.
- `bash design/20-kernel-boundary-test.sh` against the reconciled proto → `VOCABULARY CLEAN`.
- `ls` / `find` across the folder → the file index above.

**Amendments made this session:** `ARCHITECTURE.md` (§6.1 fabricated quote struck; §9 row 1 roster-library overstatement corrected; §11 Q2 boot_id reuse; new §13 Amendments) and `ROADMAP.md` (M3 boot_id reuse; new §7 Amendments).
