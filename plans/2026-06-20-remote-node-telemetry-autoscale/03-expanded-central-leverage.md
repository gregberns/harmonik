# Expanded remote→central reporting + central-leverage surface

**Date:** 2026-06-20
**Codename area:** remote-substrate / node-telemetry
**Scope of THIS doc:** everything the remote could push back to central *beyond* CPU/RAM autoscale telemetry (that is a sibling agent's doc, 02). This doc enumerates the capability, liveness, observability, queue-feedback, and artifact-return surfaces, ranks them, and draws the over-reach line.

---

## 0. Where we are today (grounding)

The remote is a near-passive execution substrate. Central pushes a bead → SSH → spawn claude in a tmux window → wait for a pushed branch → merge. The ONLY remote→central reporting that exists:

- **4 boot probes** (`internal/workers/health.go`): `tmux -V`, `claude --version`, `git rev-parse HEAD` at `repo_path`, `ANTHROPIC_API_KEY` absent. Pass/fail flips `SetEnabled`.
- **3 typed failure events** the *central side* synthesizes from SSH exit codes / socket polls — NOT things the remote volunteers: `worker_unhealthy` (boot probe failed), `worker_offline` (ssh rc 255 mid-dispatch/mid-run), `worker_tunnel_failed` (reverse tunnel socket never came live).
- **Registry is V1, single-worker** (`registry.go`): `SelectWorker` is binary — enabled + free slot → reserve, else nil (local fallback). No affinity, no scoring, no second worker (`ErrTooManyWorkers`).

Key structural fact: **central infers the remote's state from the OUTSIDE** (exit codes, socket polls, "did a branch get pushed"). The remote knows far more about itself than central can infer, and it never says so. That is the whole opportunity. We already pay for a live harmonik presence on the box (hook relay + spawn) — a self-describing agent is nearly free incremental cost on top of a process that is already resident.

The persistent gotchas in the live `workers.yaml` history are exactly the things a self-reporting remote would have caught: orphaned claude after a premature teardown, ghost worktree dirs, `no_commit_during_implementer` (claude exits 0 in ~9s having done nothing), agent-never-launched-but-permission-probe-claude-ran. Every one of those was diagnosed by an operator SSHing in and looking. A reporting channel turns "operator finds it" into "central is told."

---

## 1. Design spine — one channel, many payloads

Don't build five bespoke reporting mechanisms. Build **one**: a periodic + event-driven `worker_report` the remote-resident harmonik pushes to central over the comms bus (the same `harmonik comms` event channel central already owns), keyed by `worker_name`. It carries a versioned, additive payload. Central keeps a **live `WorkerState` snapshot per worker** in the registry (today the registry holds only `inFlight`); every family below is a field-group on that snapshot.

This matters because the operator dislikes unrequested abstraction: families 2–6 are not five subsystems, they are **fields on one struct + one cadence**. The cost is a struct, a goroutine on the remote, and a handler on central. Everything else is "what we put in the struct."

Two cadences:
- **Heartbeat** (e.g. every 15–30s while resident): cheap, idempotent snapshot — load, slots, capability digest hash, last-successful-run timestamp.
- **Edge events** (on transition): agent_launched, agent_stuck, commit_made/no_commit, worktree_leaked, disk_pressure_crossed. These are the high-value, low-volume signals.

The capability digest is sent as a **hash in the heartbeat**; central asks for the full manifest only when the hash changes (cache invalidation). Keeps the steady-state heartbeat tiny.

---

## 2. Family A — Capability / affinity advertisement

**What the remote reports** (full manifest on first contact + when digest-hash changes):
- OS + arch (`darwin/arm64`), kernel, available core count, total RAM.
- Toolchain inventory + versions: `go`, `node`, `python`, `gh`, `tmux`, `claude` — the actual `--version` strings, not just present/absent.
- claude-auth identity: which subscription account is logged in (so central never sends work to a box that's logged into the wrong account or an API key — today this is a fail-closed boot probe; make it a positive *identity* report so central can route per-account-billing).
- **Repo-clone freshness:** which repos are cloned at which paths, and `git rev-parse HEAD` + `git fetch` recency for each. (Today's single probe is one repo, one HEAD.)
- **Worktree warmth:** which branches/base-SHAs already have a materialized worktree (or a warm object cache). This is the affinity gold.
- Disk free, git object-store size.

**How central uses it:** the registry's `SelectWorker` stops being binary and becomes **capability-aware placement**. A bead carries (or central derives) requirements: needs-node? needs-the-X-repo? prefers-base-SHA-Y? Central scores each candidate worker against the live capability snapshot and routes to the box that *can actually do it* and *has the warmest start* (clone present, base SHA already fetched, worktree cache hot). Misroutes like the live `workers.yaml` incident — "enabling sent the operator's flywheel beads to gb-mbp where they ALL failed" — become impossible because central knows the box's capability profile and the bead's needs before dispatch.

**Concrete payoff:**
1. **Kills the binary-routing misfire class** that has repeatedly forced the operator to disable the worker. This alone justifies the channel.
2. **Warm-start latency.** Routing a bead to a box that already has the base SHA fetched and a worktree cached skips the slowest part of remote spawn (fetch-base + worktree-create — the exact step that has been silently failing). Affinity routing is also a *correctness* win, not just speed: the steps most likely to fail are the ones a warm box skips.
3. **Per-account cost attribution** falls out for free once auth-identity is a reported field.

---

## 3. Family B — Liveness & self-diagnosis (HIGHEST near-term value)

This is the family that directly retires the operator's manual-SSH-and-look loop. Today central infers liveness from the *outside* (pgrep over SSH, ssh exit codes). The resident agent knows the truth from the *inside*.

**What the remote reports** (edge events + heartbeat):
- **agent-up / agent-stuck:** the resident harmonik watches the spawned claude window. It can distinguish "claude is running and producing output" from "claude window exists but has been silent for N minutes" from "claude exited." This is the fix for the **`no_commit_during_implementer` / claude-exits-0-in-9s** class: instead of central guessing from a missing branch 30 minutes later, the remote reports `agent_exited_early { exit_code, wall_seconds, made_commit:false, transcript_tail }` within seconds.
- **orphaned-claude detection:** the resident agent enumerates claude processes and cross-checks them against runs central believes are active. An orphan (claude whose run central already tore down — the exact "orphaned claude" gotcha in the live notes) is reported as `orphan_detected { pid, started_at, owning_run:unknown }`.
- **worktree-leak detection:** enumerate `git worktree list` + the worktree dir, cross-check against active runs; report `worktree_leaked { path, branch, age, owning_run:unknown }`. (The "ghost worktree dirs" gotcha.)
- **disk-pressure:** report a crossing of a free-space floor *before* a run fails to write, with the biggest consumers (often the leaked worktrees + git objects). The live notes include an ENOSPC-from-a-runaway-log incident — the remote could have named the 18G logfile.
- **last-successful-run timestamp + a rolling success/fail tally per box.**
- **premature-teardown self-detection:** the remote saw its claude window get killed at ~5s (the live "treats detached tmux-new-window return as agent-exit" bug). The remote can report "my window was torn down but claude was still alive" — a *self-witness* of a central-side bug central cannot see.

**How central uses it:**
- **Smarter failure attribution.** Today a failed run is a black box: central knows "no branch pushed," not *why*. With self-diagnosis, central tags the failure: agent-never-launched vs. agent-launched-but-stuck vs. agent-exited-early-no-commit vs. infra (disk/clone). That tag drives the *recovery*: re-dispatch elsewhere vs. retry-same-box vs. quarantine-box. This is the difference between the operator running a 10-agent fan-out diagnosis (the `major-issue-fanout` skill exists *because* attribution is currently blind) and central just *knowing*.
- **Self-healing.** Reported orphans/leaks → central issues a targeted reap (the `supervise reap` verb already exists for the local namespace — extend it remote-ward). The remote could even reap its own orphans on instruction and confirm. The operator stops being the garbage collector.
- **Quarantine, not just disable.** A box with a rising fail-tally or recurring leaks gets auto-quarantined (drained, not deleted) with the *reason* attached, instead of the operator hand-editing `enabled: false` with a 6-line postmortem comment (which is literally what `workers.yaml` looks like today).

**Concrete payoff:** the single biggest operator-time sink in the live history — "operator SSHes in, finds the orphan/leak/early-exit, hand-writes a disable comment" — is replaced by central being told and acting. This is the clearest near-term win in the whole doc.

---

## 4. Family C — Run telemetry / observability

**What the remote reports** (streamed per active run):
- **progress / transcript tail:** a periodic tail of claude's session log for the running bead, pushed to central. Central gets a live "what is the remote agent doing right now" without the operator SSHing in to read a multi-MB transcript (the `crewlog.sh`-via-haiku-subagent pattern in memory exists precisely to avoid reading transcripts — a pushed tail is strictly better).
- **commit-made / no-commit signal** at run end (overlaps Family B's early-exit, but here as the *normal* terminal signal: "claude finished, made N commits touching M files" vs "finished, no commit").
- **token / cost attribution per box per run:** parse claude's own usage output; attribute spend to (worker, bead, account). Central aggregates fleet-wide cost per box and per bead. (Memory note: ~96% of spend is cache-read, burn is 24/7 uptime — per-box attribution would *prove* which boxes are worth keeping warm.)
- **timing breakdown:** fetch-base ms, worktree-create ms, agent wall-time, gate (build/test) ms. Central learns where remote runs spend time and which boxes are slow at which phase.

**How central uses it:** fleet-wide observability dashboard fed by events, not by SSH. Cost accounting that can answer "is gb-mbp earning its warm cache?" Phase-timing feeds the affinity scorer in Family A (a box that's slow at worktree-create but fast at the gate gets different work).

**Concrete payoff:** observability the operator currently gets only by hand. Cost attribution is genuinely new capability — there is no per-box spend view today. Useful but **second-order to Family B** — you want to know a box is *broken* before you want a pretty timing chart.

---

## 5. Family D — Queue feedback / load-aware placement / bidding

**What the remote reports** (heartbeat): current load (slots used / max, real CPU/RAM from sibling doc 02), and an **ETA-to-free-slot** estimate (how long until a running bead likely frees a slot, from rolling run-duration stats).

**How central uses it:** `SelectWorker` becomes **load-aware** instead of "first enabled box with a nominal free slot." With ≥2 workers (which requires lifting the V1 single-worker cap — see §8), central picks the *least-loaded capable* box. The "bid" framing — box advertises "I can take this bead, ETA 0s, warm cache, cost-class subscription" and central picks the best bid — is a clean generalization but **only earns its keep at N≥3 workers**. At N=1 it's pure over-engineering.

**Concrete payoff:** real load-balancing once the fleet is plural. At N=1 (today) this is **latent value** — design the snapshot fields now so they're populated, but don't build a bidding auction for one box. The honest ranking: load/ETA fields are *promising* (need the second worker first); a bidding *protocol* is *over-reach* until N≥3.

---

## 6. Family E — Artifact / result return (beyond git push)

**What the remote reports / returns:**
- **failure-repro bundle:** on a failed gate (build/test red) or an early-exit, the remote packages the run's transcript tail + the failing test output + a `git diff` of the uncommitted worktree + relevant logs, and returns it to central as an artifact attached to the bead. Today a failed remote run leaves *nothing* on central — the work is on a box the operator has to SSH into. The "ghost worktree" gotcha is partly that the *evidence* of what went wrong dies with the worktree teardown.
- **profiling / gate output:** test timings, build logs — attach on demand.
- **partial-work salvage:** if claude made commits but the gate failed, the remote can push the branch *anyway* under a `salvage/` prefix so central can inspect, rather than discarding (the local `harmonik promote <sha>` salvage pattern, extended remote-ward).

**How central uses it:** a failed remote run leaves a *forensic trail attached to the bead* instead of an orphaned worktree on a remote box. Re-dispatch decisions get evidence. The operator stops SSHing in to autopsy.

**Concrete payoff:** closes the "remote failures are evidence-free" gap. Strong, but **gated on Family B** — you need the early-exit/failure *signal* before bundling is meaningful. Bundle-return is the natural Phase-2 partner to liveness self-diagnosis.

---

## 7. The peer-node question — where's the line?

The operator's framing: which of these turn the remote from a *worker* into a true *peer node* that could run its OWN local sub-queue / local daemon?

**The line I'd draw:** the families above all keep **central as the single scheduler**. The remote becomes *self-describing and self-diagnosing* but central still decides what runs where. That is the right amount of peer-ness for now: a **rich worker**, not an autonomous node.

A true peer (local sub-queue / local daemon that pulls and schedules its own work) is a **different architecture** with real costs:
- two schedulers = split-brain bead ownership (which node owns the terminal transition? the whole `beads-integration` contract says the daemon owns it — *which* daemon?).
- reconciliation across nodes (already hard with one).
- the comms bus becomes a multi-master coordination problem, not a star.

**Verdict: peer-node is over-reach for this initiative** and the operator's stated dislike of unrequested abstraction points the same way. BUT — there is a narrow, valuable middle: a remote that can **buffer a small prefetched bead queue** (central pushes N beads, remote works them back-to-back without a per-bead SSH round-trip) is *not* a second scheduler — central still picked the N beads and still owns transitions; the remote just has a local in-tray. That's a throughput optimization (amortize SSH/spawn setup), not a second brain. I'd file it as **promising, not over-reach**, explicitly distinct from "local daemon."

---

## 8. The single-worker cap is the real gate

`workers.go` enforces V1 = at most one worker (`ErrTooManyWorkers`). Every multi-box idea here (capability *routing* implies choice; load-aware *placement* implies alternatives; bidding implies competitors) is inert until that cap lifts. **Lifting V1→V2 (N workers, registry holds a `[]WorkerState` with live snapshots) is the keystone enabler** — it's a prerequisite, not a feature, and it's modest (the registry already locks and tracks per-worker `inFlight`; generalize from one to a map). Do this early; it unblocks an entire column of the ranking.

---

## 9. Ranking

### CLEAR WINS — do soon (high value at N=1, modest cost, retire known operator pain)

1. **The one `worker_report` channel + per-worker `WorkerState` snapshot** (§1). The substrate everything sits on. Cheap on a process that's already resident.
2. **Family B liveness self-diagnosis: early-exit, orphan, worktree-leak, disk-pressure edge events** (§3). Directly retires the operator's SSH-and-look loop and the manual `enabled:false`-postmortem ritual. Works at N=1. Highest leverage in the doc.
3. **Failure attribution tags** derived from B (agent-never-launched vs stuck vs early-exit vs infra) (§3). Turns black-box failures into routed recovery; pre-empts the need for the 10-agent fan-out diagnosis.
4. **Capability/auth/clone-freshness manifest** (§2) — *report it now* even before routing uses it; it makes the misroute class diagnosable and feeds everything. The reporting is a clear win; the *scoring* waits for §8.
5. **Lift the V1 single-worker cap → N-worker registry with live snapshots** (§8). Keystone enabler; modest; unblocks the routing/placement column.

### PROMISING — needs the live-remote-agent and/or the 2nd worker first

6. **Capability-aware + warm-worktree affinity routing** (§2) — needs the manifest (5 above) AND N≥2 to *choose*. The warm-start/correctness payoff is large; build right after the cap lifts.
7. **Run telemetry: transcript-tail stream + per-box cost attribution** (§4). Real new observability; depends on the resident agent streaming. Cost attribution is genuinely novel.
8. **Failure-repro bundle return** (§6). Closes the evidence-free-failure gap; natural Phase-2 partner to B. Needs B's signal first.
9. **Load/ETA-aware placement** (§5) — fields populated now, *used* once N≥2.
10. **Self-healing reap on central instruction** (§3) — remote reaps its own reported orphans/leaks and confirms. Needs B + a control-channel command path (central→remote action, not just remote→central report).
11. **Prefetched local in-tray** (buffer N beads, no second scheduler) (§7) — throughput optimization; promising, explicitly NOT a peer daemon.

### OVER-REACH — note and defer (flag as unrequested abstraction)

12. **True peer node / local sub-queue daemon** (§7) — second scheduler, split-brain bead ownership, multi-master comms. Different architecture; defer hard.
13. **Bidding/auction protocol** (§5) — only earns keep at N≥3; until then a scoring function over snapshots is sufficient and simpler. Don't build an auction for ≤2 boxes.
14. **On-demand profiling subsystem** (§6) — speculative; attach logs on failure is enough; a profiling *subsystem* is gold-plating.

---

## 10. The architecture-changing insight

**Today central infers the remote's state from the outside — exit codes, socket polls, "did a branch appear" — and is blind to everything the box knows about itself. The single highest-leverage move is to invert that: make the already-resident remote agent *self-describe and self-diagnose*, turning the remote from a channel central watches into a peer that reports.** Once that inversion exists, capability routing, failure attribution, observability, and self-healing are all just *fields on one reported snapshot* — not five subsystems. The reason the operator keeps SSHing in to find orphaned claudes and ghost worktrees, and the reason a 10-agent fan-out is needed to attribute a remote failure, is the *same* root cause: the box knows, and never says. Fix the saying-channel once and the whole class of "operator is the remote's eyes and garbage collector" dissolves. Critically, this stops short of a second scheduler — central stays the single brain; the remote just stops being mute.
