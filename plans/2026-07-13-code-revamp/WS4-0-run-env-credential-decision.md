# WS4-0 — Run-environment + credential decision (RESOLVED)

**Status:** RESOLVED by operator 2026-07-16 (recorded in `M6-PLAN.md` §WS4, lines 245–252).
This note is the authoritative write-up that **WS4-2** consumes. It does not re-decide;
it records the decision, grounds the credential mechanism in the as-built files, and
spells out the one remaining piece of work WS4-2 needs.

**This is a design gate.** It blocks WS4-2 ("Reseat the matrix runner onto WS2's
subprocess env"). Nothing here is Go code.

---

## The decision, in one paragraph

The revived `core-loop-proof` runs against a **subprocess daemon**, never in-process and
never on-box. The **default** run environment is **Docker** — the subprocess daemon booted
inside WS2's docker harness (`test/docker/Dockerfile.daemon` + `entrypoint-daemon.sh`),
because it is the most reproducible / CI-portable. The **fallback** is a **subprocess
daemon in a scratch worktree** via `scripts/scratch-daemon.sh`, which works today and is
the fast path for a quick local run. Credentials in both environments reuse **credfence**:
the run authenticates against the subscription pool via the mounted `~/.claude` credential
directory and **NEVER** an `ANTHROPIC_API_KEY` (D2).

---

## 1. Default environment — **Docker**

- The daemon runs as a **subprocess inside the WS2 docker harness**. The image is the
  hermetic, multi-stage `test/docker/Dockerfile.daemon`; `entrypoint-daemon.sh` boots the
  daemon in a detached tmux session and blocks (`test/docker/entrypoint-daemon.sh:56`,
  socket at `${HARMONIK_PROJECT}/.harmonik/daemon.sock`).
- Rationale: most reproducible. The image compiles `harmonik` + twins from source via the
  repo Makefile (`Dockerfile.daemon:31`), pins `br` by checksum, and mounts no host build
  artifacts — so a CI run and a local run exercise byte-identical binaries.

## 2. Fallback environment — **subprocess in a scratch worktree**

- `scripts/scratch-daemon.sh` boots a **standalone** `harmonik --project <scratch>` daemon
  inside a tmux session (`scratch-daemon.sh:239-245`). No `harmonik supervise` → no
  auto-revive, so `down`/`pkill` stays down for a clean rebuild.
- Rationale: works today with zero image build; the fast path for a quick local iteration.
  Same subprocess-daemon shape as Docker, just on the host filesystem instead of a container.

## 3. REJECTED alternatives

- **On-box daemon** (the shared live daemon) — rejected: a proof run must be isolated and
  disposable, not coupled to production state.
- **In-process daemon** — rejected: does not exercise the real socket / subprocess boot
  path the proof is meant to certify.

---

## 4. Credentials — reuse **credfence**, NEVER `ANTHROPIC_API_KEY` (D2)

**NEVER `ANTHROPIC_API_KEY`.** Authentication is via the **mounted `~/.claude` credential
directory**, which bills the subscription pool. An env-var API key is forbidden in every
run environment (D2). This is not a preference — it is the credfence invariant.

The mechanism is already half-built:

- **Scratch fallback:** `scripts/scratch-daemon.sh:240` launches the daemon under
  `env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN …` — the API keys are actively
  **unset** before the daemon (and therefore every agent it spawns) starts, so the run
  cannot fall back to key auth and instead uses the host's `~/.claude` subscription
  credentials (codename:credfence, matching `smoke-scratch`).
- **Docker default:** `test/docker/Dockerfile.daemon:78-81` already declares the WS4
  credential mount point as a documented stub — a real `~/.claude` credential set is
  expected at **`/root/.claude`** inside the container (the daemon runs as root; WORKDIR
  `/work`, `/root/.ssh`, so `~` = `/root`). It is a convention comment only today; nothing
  mounts anything yet.

---

## 5. Remaining work — what WS4-2 must do next

The decision is settled; **one concrete piece of plumbing remains**, and it lives in the
Docker default:

> **Make the docker image see the mounted `~/.claude` auth dir.**

Concretely, WS4-2 (working with the WS2.3/WS2.5 compose surface) must:

1. **Mount the host credential dir read-only into the daemon container** at the path the
   entrypoint already assumes. In the WS2 compose file for the daemon service:

   ```yaml
   services:
     daemon:
       volumes:
         - ${HOME}/.claude:/root/.claude:ro   # subscription credentials (credfence)
   ```

   Container path `/root/.claude` is fixed by `Dockerfile.daemon:78-81` (daemon runs as
   root). Read-only (`:ro`) — the proof run must not mutate host credentials.

2. **Apply the credfence unset inside the container**, mirroring `scratch-daemon.sh:240`.
   The daemon (and every agent subprocess it forks) must start with `ANTHROPIC_API_KEY`
   and `ANTHROPIC_AUTH_TOKEN` **unset**, so the only usable credential is the mounted
   `/root/.claude`. Either:
   - do not pass those vars into the container env at all (compose `environment:` omits
     them), **and** have `entrypoint-daemon.sh` defensively `unset ANTHROPIC_API_KEY
     ANTHROPIC_AUTH_TOKEN` before `tmux new-session … harmonik daemon`
     (`entrypoint-daemon.sh:56`) — belt-and-suspenders, matching the scratch fallback's
     explicit `env -u`.

3. **Do NOT** add any `ANTHROPIC_API_KEY` to the compose env, a `.env` file, or a build
   arg. If a cell needs auth and `/root/.claude` is absent, the cell is a **loud-PENDING**,
   never a key-auth fallback (this preserves the WS4-2 accept criterion that a fixtureless
   cell is loud-PENDING, never green).

Once (1) and (2) land, the Docker default has working subscription auth and WS4-2's matrix
runner can boot a subprocess daemon in the harness with credfence intact.

---

## Grounding references

- `scripts/scratch-daemon.sh:234-245` — standalone subprocess daemon boot; credfence
  `env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN` at line 240.
- `test/docker/Dockerfile.daemon:78-81` — WS4 credential mount-point stub; `/root/.claude`
  named as the container credential path.
- `test/docker/entrypoint-daemon.sh:11-12,56` — daemon boots in detached tmux; socket at
  `${HARMONIK_PROJECT}/.harmonik/daemon.sock`; runs as root (`/root/.ssh`).
- `plans/2026-07-13-code-revamp/M6-PLAN.md:245-252` — the operator-resolved decision this
  note writes up; `:263-268` — WS4-2, the consumer.
