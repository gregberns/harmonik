# A1 — Distributed fleet: what the operator actually proposed

**Source:** `plans/2026-06-30-distributed-fleet/` (8 files, operator session 2026-06-30, synthesized 2026-07-03). This note captures the operator's intent faithfully — it is not a critique. File:line citations are all within that plan dir unless a path is given.

---

## Bottom line

Over a 2026-06-30 session the operator asked for one thing along three axes: **run crew (and tasks) across more than one machine, each in its own fully isolated environment, driven by more than one model harness** (`PLANNING-SUMMARY.md:13-18`). Concretely: a captain on box A can stand up a crew on box B (or in a container, or on Codex/Pi instead of Claude); every node knows who else is online and healthy in a "zookeeper-like" roster; the comms bus reaches across node boundaries; and the operator can `harmonik attach` straight into any remote crew's tmux (`PLANNING-SUMMARY.md:21-24`). The operator framed the substrate as **peer-to-peer** ("a way to know who's online... like zookeeper", `01-p2p-comms/README.md:16-18`) — but the planning docs repeatedly reconcile that framing back toward the *already-decided* hub-and-spoke / central-scheduler model, treating "keep scheduling central" as a locked decision that peer-to-peer must not reopen (`01-p2p-comms/README.md:34,70-97`). **That reconciliation is the moment where the operator's push for a genuinely more powerful, more peer-ish underlying system got narrowed**: the strongest version of their idea (nodes that know and update *each other*, a comms log truly *distributed across* nodes) was routed into "recommended first cut = central-authoritative, replicated to nodes; true peer is P3/maybe-never" (`01-p2p-comms/README.md:113-120,208-209`). Much of the design was also framed as ~60% "just green-light the already-deferred Phase 3 work," which further shrinks the ambition to an increment on existing remote-substrate code (`PLANNING-SUMMARY.md:82-86`).

---

## The vision (quoted, operator-attributed)

Each subfolder opens with a **"The vision (operator, distilled)"** block. These are the operator's own framing:

**Multi-node / P2P substrate** (`01-p2p-comms/README.md:14-18`):
> "Connect harmonik via a peer-to-peer comms network. A lead machine SSHs into another, pulls a harmonik build, starts harmonik with details about the origin machine. The machines exchange node names, then know about each other and have some system to keep updated. On top of that there's a transport layer — the comms logs get distributed across the nodes. Crew running on a box hook into the harmonik daemon for comms. It would also be nice to SSH into those boxes easily and get access to the crew running in the tmux sessions."

> "Seems like this P2P would be kinda like zookeeper — a way to know who's online, if they're healthy, etc. Then the actual transport of whatever would be handled separately." (`01-p2p-comms/README.md:20-21`)

**Isolated crew — the connective "why"** (`00-the-case-for-isolated-crew.md:11-13`):
> "**Run a crew / an agent in its own sandbox, with its own set of work to do — a full environment, where the agent has a queue to dispatch into.**"

with "Isolated" defined as **own filesystem, own branch/worktree — not just a subfolder inside the main repo. The agent must not be able to accidentally write to the primary repo folder / main** (`00-the-case-for-isolated-crew.md:14-16`). The concrete pain that motivated it: **"Codex started working in the main repo when it needed to start in its own worktree/env. Isolation is the fix."** (`00-the-case-for-isolated-crew.md:39-42`).

**Container / sandbox + build-cache** (`02-container-sandbox/README.md:12-19`):
> "Need a container layer, so both tasks run through harmonik and crew can be run in isolated containers. Start with the harmonik *tasks* going into containers, then later crew... We'd probably not want to force a particular container/sandbox framework — allow it to be flexible somehow."

> "if we run in isolated containers, languages like Go, Rust, Haskell, OCaml all download source and compile (heavy build)... We need the isolated sandboxes to work as fast (or nearly as fast) as on the main machine."

**Crew on Pi (model diversity)** (`03-pi-crew/README.md:11-12`):
> "I want to figure out how to run the crew on Pi. This will give us more options on what models to run the crew with." (Pi = earendil-works/pi headless coding agent, NOT Raspberry Pi — `03-pi-crew/README.md:14-16`.)

**Auto-comms-on-startup** (`04-auto-comms-startup/README.md:10-12`):
> "With Pi, can we force the agent to listen to comms on startup? Right now the crew need to set up the comms connection — it would be nice to not have that. We should also see if this could be done with Claude Code too — maybe there's a way."

**Our-own-harness meta-question** (`05-hermes-harness-study/README.md:73-74`):
> "Now that we're branching into other harnesses, it might be interesting to investigate either our own harness (which harmonik could then run in a sandbox like Daytona) — or see if there are programmatic ones that make sense to support."

---

## Concrete components proposed

**Substrate / multi-node (idea 1)** — the operator names, and the sketch formalizes:
- **Dynamic join / bootstrap protocol** replacing the hand-edited static `workers.yaml`: lead SSHs into box B, ships/pulls a harmonik build, starts it with the origin's coordinates, the two exchange names and register each other (`01-p2p-comms/README.md:49-53,122-134`). Proposed CLI: `harmonik node join --hub tcp://<boxA-tailnet>:<port> --name <auto-or-given>` (`01-p2p-comms/README.md:129`).
- **A node record** as a live object: `{ name, addr (tailnet), os, arch, toolchains[], max_slots, state, last_heartbeat }` (`01-p2p-comms/README.md:105`).
- **Membership/presence/health layer** — the "zookeeper-like" roster: who's in the fleet, up, healthy, what they can run; heartbeat/lease split; graded liveness **alive → suspect → dead** (`01-p2p-comms/README.md:31-33,107-110`).
- **A presence roster every node can read** — replicated to nodes, not hoarded centrally; `harmonik fleet` shows every node, its health, its crew (`01-p2p-comms/README.md:54-57,174`).
- **Comms-bus distribution across node boundaries** — the genuinely new transport: a crew on box B can `comms send --to captain` on box A and `comms recv` events originated on A (`01-p2p-comms/README.md:58-60,141-166`).
- **SSH-into-crew ergonomics** — `harmonik attach <node>` and `harmonik attach <node>/<crew>` to drop straight into a remote crew's tmux window; `harmonik fleet` as a roster table (`01-p2p-comms/README.md:61-62,168-176`).
- Transport substrate reused: reverse-SSH tunnel (locked `hk-ege6`), Tailscale + `accept`-mode ACL (`01-p2p-comms/README.md:184-185`).

**Isolation / sandbox (idea 2)**:
- A **pluggable, framework-agnostic isolation layer** — a config-driven `Runner`-style seam over Docker / OrbStack / Lima / Apple `container` / Firejail / nsjail / plain-namespace; generalize the L3 `DockerExecRunner` (`02-container-sandbox/README.md:64-67`).
- **Warm-build-cache** strategies for cold sandboxes: mounted/shared cache volumes, warm base images, content-addressed remote cache (sccache / `GOCACHEPROG`), CoW snapshots (APFS/zfs/OrbStack clones), warm-affinity routing — likely **per-language** (`02-container-sandbox/README.md:68-83`).
- Backend candidates flagged by the operator: **Daytona** (noted as heavier micro-VM/hosted model), **Singularity/Apptainer** (rootless, SIF, process-oriented — closest to the operator's steer), **Modal** (serverless, bursty) (`02-container-sandbox/README.md:86-110`). Operator steer: **process isolation, not full micro-VM / hosted sandbox-as-a-service** (`02-container-sandbox/README.md:90-91`, `PLANNING-SUMMARY.md:152-154`).
- Security/blast-radius framing: containers isolate for **security/blast-radius and reproducibility**, a separate axis from the node boundary (`02-container-sandbox/README.md:29-33`); must satisfy spec invariants ON-024 (execution stays inside `workspace_path`) + ON-025 (network egress whitelist) (`02-container-sandbox/README.md:39-44`).

**Harness pluggability (ideas 3 + 5)**:
- **Pi as a crew runner** (not just per-bead) — a control-flow shim emulating Claude's crew-launch flags: keep-alive stream, pane-wake, session-resume/context-clear (`03-pi-crew/README.md:44-56`).
- **Per-crew swappable harness** — Claude / Codex / Pi / Hermes-over-ACP behind one seam (`00-the-case-for-isolated-crew.md:35-42`).
- **"Harness = pluggable seam (bead + context in, structured result out, transport-agnostic)"** as a first-class design item; interactive-tmux vs. programmatic-stdio/ACP is an implementation detail behind it (`05-hermes-harness-study/README.md:94-102`).
- **Hermes concepts to potentially adopt independent of adopting Hermes**: workspace-*kinds* per bead (`scratch`/`worktree`/`dir:`), **typed block reasons** (dependency/needs_input/capability/transient), **profile-as-forked-identity** for credential isolation (`05-hermes-harness-study/README.md:40-58`, `PLANNING-SUMMARY.md:161-165`).

**Boot enabler (idea 4)**:
- Make comms-subscribe a **forced boot property of the agent process** (launcher/harness-enforced sidecar `recv --follow`), not a prompt-driven step the model can skip or lose on `/clear`; Pi surface (greenfield) + Claude Code surface (SessionStart hook) (`04-auto-comms-startup/README.md:14-16,42-51`). Candidate: promote the hand-rolled "background watcher that re-invokes the agent on a new message" into a first-class `harmonik comms watch` primitive (`04-auto-comms-startup/README.md:37-40,58-60`).

---

## What got dismissed / descoped + why (quoted)

This is the crux the operator felt: their push for a genuinely powerful, peer-ish underlying system was repeatedly reconciled back toward the existing central model, and much of it reframed as "already designed, just green-light it."

**1. Peer-to-peer scheduling — treated as reopening a locked decision, pushed to "maybe-never."** The operator's "peer-to-peer" / "distributed across the nodes" framing is met with a locked prior decision quoted against it (`01-p2p-comms/README.md:76-79`):
> "**Locked (prior-art doc §2):** *'Don't go pure-pull — it dissolves the central queue.'* Harmonik's identity is **daemon-owns-the-queue**... The prior design deliberately **rejected a gossip mesh** at this scale ('take the *state machine*, not the *protocol*') and chose hub-and-spoke with graded liveness."

The resolution splits presence (peer-ish OK) from scheduling (stays central) and recommends against true peer scheduling (`01-p2p-comms/README.md:94-97`):
> "If the operator genuinely wants peer scheduling too (multiple nodes pulling from a shared queue with no central brain), that's a much bigger reopen of a locked decision and should be its own explicit decision — flag it, don't assume it."

**2. Gossip-mesh membership (true P2P roster) — "explicitly rejected... as over-engineering."** (`01-p2p-comms/README.md:117-120`):
> "*Option A2 — gossip mesh (SWIM/Serf).* True peer-to-peer membership. The prior-art doc **explicitly rejected this at current scale** as over-engineering. Revisit only if node count grows past ~dozens or the hub-as-single-point becomes the real bottleneck. **Pick A1 now; A2 is a later evolution, not v1.**"

**3. Peer bus replication (comms log truly distributed across nodes) — deferred to hub-relay star topology.** The operator explicitly said "the comms logs get distributed across the nodes"; the sketch recommends C1 hub-relay instead, which it concedes is **"a star topology on the comms plane — *not* peer-to-peer in the strict sense"** (`01-p2p-comms/README.md:149-153`). True peer replication (C2) is deferred (`01-p2p-comms/README.md:156-166`):
> "More machinery; defer unless hub-as-relay becomes the bottleneck... **Pick C1 now.** ...C2 is the 'real P2P' evolution if/when hub-relay hurts."

Both true-P2P options land in **"P3 (later / maybe-never)... Explicitly out of v1."** (`01-p2p-comms/README.md:208-209`).

**4. The whole substrate reframed as ~60% "finish already-designed work," shrinking the ambition.** (`PLANNING-SUMMARY.md:82-86`, echoed `01-p2p-comms/README.md:191-193`):
> "**~60% of idea 1 is 'finish already-designed work':** the resident node-agent + graded liveness + capability fingerprinting was *already designed* as deferred Phase 3 of the remote-node plan... Idea 1 is largely the operator green-lighting that phase, plus ~40% genuinely new..."

**5. Containers were "considered and left out" of the minimal model.** (`02-container-sandbox/README.md:45-47`):
> "`specs/workspace-model.md:198` deliberately excludes provisioning layers from MVH: 'No provisioning layer (adze, devbox, container build) participates in MVH worktree creation; the worktree is a plain subfolder.'"

**6. Container idea 2 itself is only a "scoping stub," deferred behind idea 1** (`02-container-sandbox/README.md:3-4,122`): "**Status: stub — flesh out after idea 1 is decided.**" Ideas 3 and 4 are likewise stubs deferred behind idea 1 (`03-pi-crew/README.md:68`, `04-auto-comms-startup/README.md:64`).

**7. Everything multi-node gated behind an unfinished live e2e.** (`01-p2p-comms/README.md:211-213`, `PLANNING-SUMMARY.md:171-173`):
> "**Gate:** P0–P2 all assume the **live remote e2e is finally green** on the real box (still owed — `hk-nepva`, gb-mbp `enabled:false`). Don't build the multi-node roster on a single-node path that's never run end-to-end live."

**The underestimated-need moment:** the operator's instinct that a *powerful underlying peer system* is required shows up as the "one real tension" — and the planning consistently resolves it toward the least-new-machinery, central-authority option on every axis (D1→A, D2→C1, roster→A1). The strongest form of the operator's vision (nodes that mutually know and update each other; a comms log genuinely replicated peer-to-peer that survives hub outage) is real and named, but classified as "real P2P... defer unless it hurts" (`01-p2p-comms/README.md:159-166,219-227`). If the hub is in fact a load-bearing bottleneck for the operator's actual use (many machines, resilience to the lead box dying), the recommended path structurally under-delivers on that — which is likely the dismissal the operator felt.

---

## Unresolved questions the plan raised

The plan surfaced **five explicit operator decisions on idea 1 alone** and folded them into a broader D1–D5 consensus set — none resolved (no beads/kerf work created, `PLANNING-SUMMARY.md:7`):

- **D1 — Peer-to-peer vs. hub-and-spoke** (the load-bearing one): synthesis (peer-ish membership+comms, central scheduling) vs. true peer scheduling that reopens daemon-owns-the-queue (`PLANNING-SUMMARY.md:115-122`, `01-p2p-comms/README.md:219-221`).
- **D2 — Comms-bus distribution**: hub-relay (C1) vs. peer replication (C2) (`PLANNING-SUMMARY.md:124-130`).
- **D3 — Warm-build-cache strategy per language** — the hard part; "a matrix decision, not one pick" (`PLANNING-SUMMARY.md:132-139`).
- **D4 — Sandbox backend / harness heaviness + the "our own harness vs. support existing" fork** — is tmux-interactive load-bearing for *all* harnesses (Gas-Town inspectability locked preference) or an artifact made optional per-harness? (`PLANNING-SUMMARY.md:141-154`, `05-hermes-harness-study/README.md:107-115`).
- **D5 — Join trust + node naming**: tailnet ACL sufficient vs. explicit join token; operator-given vs. auto-derived names; adopt Harbor/port/harbormaster naming or keep plain (`PLANNING-SUMMARY.md:156-159`, `01-p2p-comms/README.md:222-227`).

Plus stub-level open questions: minimal task-in-container path reusing L3 `DockerExecRunner`; how sandbox composes with nodes and Pi crew; whether ON-024/ON-025 already give the container contract or the spec needs extension; per-language cache invalidation ownership post-TOCTOU (`02-container-sandbox/README.md:113-120`); the minimal control-flag surface a Pi crew shim must emulate, and whether keeper's `/clear`→resume even applies to Pi (`03-pi-crew/README.md:60-66`); whether comms-subscribe can be a launcher-owned sidecar for both Pi and Claude Code, and whether a SessionStart hook survives the keeper `/clear`→resume `--session-id` flip (`04-auto-comms-startup/README.md:54-62`); whether the harness seam already exists implicitly or needs a contract doc, and whether an ACP spike is worth it (`05-hermes-harness-study/README.md:107-115`).

---

## Reusable NOW vs. stale

**Reusable now — durable requirements/constraints from the operator's intent:**
- **The core objective** stated verbatim: an agent in its own full environment (own fs + own branch/worktree + own queue/delegate capability) that *cannot* write to main, and stays **tmux-attachable** even when sandboxed/remote (`00-the-case-for-isolated-crew.md:11-16,43-45`, `PLANNING-SUMMARY.md:13-18`). This is the north-star requirement for any platform-architecture effort.
- **The trigger heuristic** for when isolation is warranted: work that is "more than a single queue item" — planning (research + agent↔operator rounds) or heavy epics needing subagent fan-out (`00-the-case-for-isolated-crew.md:20-31`).
- **The three-concerns decomposition** (A membership/presence, B scheduling, C transport) as a framing tool — regardless of which topology wins, separating these is sound (`01-p2p-comms/README.md:27-39`).
- **The pluggable-harness seam** ("bead + context in, structured result out, transport-agnostic") as a first-class design item — arguably the single highest-leverage reusable idea; it unlocks nodes/sandbox/model-diversity at once (`05-hermes-harness-study/README.md:94-102`).
- **Concrete Hermes prior-art patterns** worth lifting: workspace-kinds per bead, typed block reasons (addresses the real stranded-`in_progress` pain), profile-as-forked-identity for credential isolation (`05-hermes-harness-study/README.md:40-58`).
- **Operator steers that are still policy**: process isolation not micro-VM/hosted (`02-container-sandbox/README.md:90-91`); don't force a single container framework (`02-container-sandbox/README.md:16-18`); provider+model+credentials are operator config, fail-loud, never hardcoded (`03-pi-crew/README.md:35-36`); inspectability/tmux-attach is a locked design preference (`00-the-case-for-isolated-crew.md:43-45`).
- **The warm-build-cache problem** is a genuine, still-undesigned gap (documented only as incidents — the go cache-reaper TOCTOU `hk-y3frr`, warm-worktree affinity) that any real containerization must answer (`02-container-sandbox/README.md:55-58,68-83`).
- **Forced/declarative comms-subscribe** as a harness primitive rather than a prompt step — still a real, small, high-leverage enabler (`04-auto-comms-startup/README.md:33-40`).
- **The five open decisions D1–D5** remain the actual decision agenda — none were resolved.

**Potentially stale / needs re-verification (state claims from 2026-06/07):**
- Specific bead/gate state: live remote e2e "still owed" (`hk-nepva`, gb-mbp `enabled:false`), the single-worker `workers.NewRegistry` cap, `AgentTypePi` "declared but unimplemented" with codex as the template — all point-in-time and must be re-checked against current code before relying on them (`PLANNING-SUMMARY.md:60-74`, `01-p2p-comms/README.md:187`, `03-pi-crew/README.md:22-24`).
- The "~60% is already-designed Phase 3" claim depends on the prior-art doc's deferred design still being the intended path; a fresh platform effort may legitimately revisit hub-and-spoke rather than inherit it as locked.
- References to sibling plans as "the first buildable slice" (`plans/2026-07-02-pi-sandbox/`, HIGH) and the Pi/OpenRouter brief — their status has likely moved; treat as pointers, not current truth.
- Backend product specifics (Daytona/Modal/Singularity capabilities and positioning) are 2026-06 snapshots and warrant re-evaluation.
- The recommendations themselves (adopt synthesis/A1/C1) are *the planners'* recommendations, not operator decisions — a fresh effort should treat them as one option, especially since they're exactly what narrowed the operator's peer-to-peer ambition.
