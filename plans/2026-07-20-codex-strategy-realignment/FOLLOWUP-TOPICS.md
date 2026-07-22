# Topics queue — spawned by the 2026-07-20 Codex/remote realignment

Not distant "someday" items — these are real threads the realignment surfaced, with
enough context to be picked up cold. Grouped by when they get taken up.

---

## A. Sandbox & Security  — PARALLEL workstream (discuss right after this realignment; implement together)

Per **D3**: native harness sandboxes are OFF; security is re-homed to a single uniform
mechanism. This is NOT deferred — the operator wants it discussed as soon as this
realignment closes, and built alongside.

**A0 — Governing principle (operator, load-bearing).** Build security so *"it just works
AND is secure."* Security that gets in the way gets turned off, so the secure path must
also be the easy path. Three goals: (1) runnable, (2) not painful to run, (3) secure to
run — and (3) must not undermine (2). This is the acceptance bar for whatever we design.

**A1 — The uniform "sandbox all beads" model.** One sandbox boundary that every bead runs
inside, regardless of harness, with an **ad-hoc per-bead disable** (a bead flag) for cases
where sandboxing is too painful (e.g. testing against a local Docker daemon). Design the
on/off surface and the default.

**A2 — Mechanism: container vs harmonik-applied OS-sandbox wrapper — OPEN, DEFERRED.**
The main agent suggested a container (because seatbelt-vs-bubblewrap is the exact per-OS
nuance we're fleeing, and a container abstracts the host). Operator has **mixed feelings**
— explicitly deferred to its own later discussion. Do NOT assume container; keep it open.

> **EVIDENCE ADDED 2026-07-22 (lima, `hk-bzydx`/`hk-quoka`, commit `66230eea`). This does
> NOT decide A2 — the call remains the operator's. It removes the main unknown.**
> The wrapper was measured against the question that was actually blocking: *does enabling
> it for the other harnesses force us to widen read scope and give up the exfil posture?*
> **It does not.** Both `claude` and `codex` were run under a profile mirroring the REAL
> production config — backend `srt`, `allow_local_binding: true`, `warm_read` set to
> exactly the three pi entries, **no `$HOME` read grant** — plus only the two fixes above.
> `claude -p` printed its answer with only telemetry denied; `codex exec` printed its
> answer, 4,951 tokens, **exit 0, zero srt denials**.
> So: the wrapper is sufficient for **all three harnesses**, and enabling one is a
> **one-line addition to `sandbox.harnesses`** with pi's exfil posture preserved exactly as
> bisected. Whatever a container would buy beyond that has to be argued on other grounds
> (blast radius, host abstraction, §B) — **not** on the wrapper being unable to carry
> claude and codex, which was the open question and is now answered.
> *Not claimed:* codex's **background** calls still fail under the sandbox. That is a TLS
> limitation, **not** the domain list — with the list correct there are zero denials and
> they still fail. Adding domains cannot help and nobody should try.

**A3 — Pi macOS sandbox: added but likely never correctly implemented.** (Was F1.)
*Intent:* the operator's ORIGINAL reason for OS-sandboxing was specifically **Pi** — the
harness that runs LOW-QUALITY / experimental models that could damage the machine. Narrow
and sensible: confine Pi. Sandboxing was never originally about Codex; that generalization
happened later and separately (see D1).
*Code today:* **CORRECTED 2026-07-22 (lima, `hk-s13ee`) — the two premises below were
stale, and they are why this epic read as unbuilt greenfield when most of the mechanism
is already shipped and running.**

> **~~"Pi is spec'd UNSANDBOXED (`specs/pi-harness.md` PI-015)."~~ — MISREADING, and it
> inverts the meaning.** PI-015 forbids the **harness** passing a `--sandbox` **flag** —
> i.e. no **native** Pi sandbox. That is exactly what **D3** mandates. The harmonik-level
> `srt` wrapper is **external** to the harness and orthogonal to PI-015. **There is no
> spec-vs-code contradiction to reconcile.**
>
> **~~"A `pi-sandbox` srt/bwrap experiment was dropped."~~ — it LANDED and is LIVE.**
> The kerf work `pi-sandbox` is at the tasks pass, 7/7 beads. `GenerateSandboxProfile`
> (`internal/daemon/sandboxprofile.go`) and the engagement gate (`sandboxgate.go`) are in
> production and called from **both** launch paths; `srt` is installed at
> `/opt/homebrew/bin/srt`; `.harmonik/config.yaml` carries `backend: srt` with
> `harnesses: [pi]`. **Pi runs sandboxed in production today.** The `hk-u69my` loopback
> bug was fixed, not a reason the work stopped.

*Operator recollection:* "We added support for the macOS sandbox explicitly for Pi. It
probably was never fully implemented / implemented correctly." — *the recollection is
right that a **native** macOS path was never finished; what exists instead, and works, is
the external wrapper.*

*To do:* ~~find whether a macOS-native (`sandbox-exec`/seatbelt) Pi path was ever built
(wired/partial/abandoned); reconcile "confine Pi" intent vs current unsandboxed spec;~~
**Both halves are ANSWERED: nothing needs reconciling, and the thing to look at is not a
native path but the wrapper that already exists.** The remaining distance to "uniform
sandbox across all harnesses" may be **one config list plus whatever breaks** (see
`hk-155gs`) rather than a new subsystem.

**WHAT IS GENUINELY STILL MISSING** — stated so this correction does not overclaim:
1. **Coverage.** `sandbox.harnesses` still lists only `pi`, so claude and codex runs are
   not sandboxed. As of `66230eea` they are sandbox-**able** (see A2) — turning either on
   is a deliberate one-line config change, not a build.
2. **The per-bead disable (A1) does not exist in any form.** No bead-level sandbox field
   or flag anywhere in `internal/` or `cmd/`; the only knob is the project-wide list.
   Tracked as `hk-mp37h`.
3. **A2, container vs wrapper, remains genuinely open and operator-owned** — though the
   read-scope unknown that was blocking it is now measured (see A2).

*Superseded to-do, kept for provenance — the original A3 closed with:* "…fold into the A1
uniform mechanism if that's the answer, rather than a Pi-specific build." **That direction
is right and is what happened:** the wrapper is harness-agnostic and applies at one gate,
so folding pi into A1 is not pending work — it is the shape already in production.

---

## B. Containerized beads — BIG planning item (own effort; related to remote / Unit 3)

**The vision (operator, 2026-07-20):** we run in git worktrees today; the operator wants
to run **containerized beads** — the repo is spun up inside a container, and a bead is
**fully processed inside that container**. This must work **both locally and remotely**.

**Two purposes, explicit:** (1) **security** (a real blast-radius boundary around each
bead) and (2) **distributing load** (push beads onto other machines / many containers).

**Why it's big:** it reshapes the execution + remote model — how the repo gets into the
container, how results come back, how it composes with the DOT flow, and how "local
container" and "remote container" become the same shape. Needs substantial planning.
Connects directly to Unit 3 (the remote model) — likely the RIGHT long-term answer to
what the current ship-whole-repo-over-ssh model does badly.

**Status:** on the list; do NOT dig in yet. Take up after the realignment + sandbox design.

---

## C. Misc surfaced

**C1 — Orphaned harmonik processes.** With the system NOT operating, ~10-20 harmonik
processes were still running (days-old `comms join` keepalive loops for
admiral/yankee/assessor/captain + two `prod-readiness-watch` scripts, leftovers from prior
sessions). Killed this session. The count is suspicious — something may be leaking watcher
processes that outlive their session. Investigate what launches them and why they don't
clean up on exit. (Also noted in `HANDOFF-admiral.md`.)
