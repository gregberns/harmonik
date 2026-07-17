# Two-container Docker E2E harness (M6 WS2)

This directory holds the Docker harness that exercises harmonik's **remote-substrate**
lifecycle over a **real SSH transport** between two separate containers — a daemon
("box A") and a worker. It certifies the same `git worktree add` / `git fetch
ssh://worker…` remote path that `internal/daemon/scenario_remote_substrate_localhost_test.go`
drives on localhost, but across a genuine container-to-container network boundary,
so it does **not** depend on a working localhost-SSH loopback on the host.

## What's here

| File | Role |
|---|---|
| `compose.yml` | Two-service topology (`daemon` + `worker`), the two named volumes, and the drive env. |
| `Dockerfile.daemon` | Multi-stage, hermetic daemon image. Compiles `harmonik` + twins from source via the repo Makefile, bakes the compiled scenario test binary, pins `br` by checksum. |
| `Dockerfile.worker` | sshd + git + tmux + generic twin. No baked-in SSH key. |
| `entrypoint-daemon.sh` | Generates the client keypair, boots `harmonik daemon` in a detached tmux session, publishes the pubkey to the shared `keys` volume. |
| `entrypoint-worker.sh` | Installs the daemon's pubkey (background watcher), runs `sshd -D -e` in the foreground. |

## Build + run

The single entry point is the Makefile target (repo root, ~line 124):

```bash
make test-docker-e2e
```

It requires only a working Docker; it needs **no host `~/.ssh` setup** — the SSH keys
are generated inside the containers at boot. The target orchestrates:

1. `docker compose -f test/docker/compose.yml up -d --build` — build both images and
   start both services (`worker` first; `daemon` `depends_on` `worker: service_healthy`).
2. **Readiness gate** — polls (up to 30 attempts, 0.5s apart) until
   `docker compose exec -T daemon ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=5 worker true`
   succeeds. If passwordless SSH never comes up it dumps `compose logs`, tears the
   stack down, and exits non-zero (**FATAL** — it does not silently pass).
3. **Drive** — execs the compiled scenario binary baked into the daemon image:
   `/usr/local/bin/remote-substrate.test -test.run '^TestScenario_RemoteSubstrate_Localhost_E2E$' -test.v`.
4. **Teardown** — always `docker compose … down -v` (preserving the drive's exit code),
   which wipes both named volumes so each run starts with fresh keys and fresh repos.

## Topology

Two services on compose's default bridge network (built-in DNS resolves the worker by
its service name `worker`):

- **`worker`** — runs `sshd`. The daemon reaches it over SSH as `Host: worker`, runs
  `git worktree add` in a writable clone at `/work/worker`, and the daemon fetches
  `run/<id>` straight back over `ssh://worker/shared/worker …` — no worker→GitHub
  round-trip (hk-7bwx). Its healthcheck probes **only** that sshd is *listening* on
  `:22` (a bare `bash </dev/tcp/localhost/22`), **not** that auth works — gating on a
  successful `ssh worker true` would deadlock, since the worker can only authorize the
  daemon's key after the daemon has booted and published it, and the daemon
  `depends_on` the worker being healthy first.
- **`daemon`** — boots a self-contained harmonik project (`harmonik init` +
  `harmonik daemon` in a detached tmux session, socket at
  `${HARMONIK_PROJECT}/.harmonik/daemon.sock`) and execs the drive against the worker.

### The drive is a compiled test binary, not the full twin lifecycle

The daemon image bakes `/usr/local/bin/remote-substrate.test`
(`go test -c -tags scenario … ./internal/daemon/`). The Go test's stub
worktree-factory makes the commit that cleanly closes the bead; the generic twin does
not, which is why the harness drives the compiled scenario test rather than a full
daemon+twin lifecycle.

## SSH key handoff via the `keys` volume

No SSH secret is baked into either image (LOCKED WS2.2 decision). The keypair is
generated at compose-up and handed off through the shared `keys` volume, mounted at
`/keys` in **both** containers:

1. `entrypoint-daemon.sh` generates an ed25519 client keypair into `/keys` (only if
   absent), installs the private half into `/root/.ssh/id_ed25519`, and leaves the
   **public** half at `/keys/id_ed25519.pub`. The private key never leaves the daemon
   container / volume, and nothing is committed to git.
2. `entrypoint-worker.sh` runs an **idempotent background watcher** that polls
   `/keys/id_ed25519.pub` and appends it to `/root/.ssh/authorized_keys` the moment it
   appears (a no-op once installed). A one-shot poll would race and lose, because the
   daemon only publishes its key *after* it boots — which, per `depends_on`, is *after*
   the worker is already up.
3. The daemon's SSH client config uses `StrictHostKeyChecking accept-new`, so the first
   connection trusts the worker's freshly generated host key.

Because `down -v` wipes the `keys` volume every run, keys regenerate cleanly with no
stale-key silent-auth-failure hazard.

## Shared repo — identical path requirement (`HARMONIK_E2E_SHARED_ROOT=/shared`)

The second named volume, `shared`, is mounted at `/shared` in **both** containers and
holds `origin.git` plus the worker clone. `compose.yml` sets
`HARMONIK_E2E_SHARED_ROOT: /shared` on the daemon. **CRUX 2:** box A creates both repos
under `/shared`, and the worker must see them at the **identical absolute path**, because
the worker's `git fetch origin <baseSHA>` and box A's `git fetch ssh://worker/shared/worker …`
both have to resolve to the same on-disk repos. A different mount path on either side
would break the fetch. The daemon also runs with `HARMONIK_REQUIRE_REMOTE_E2E=1` so a
broken SSH path fails **loud** instead of skipping green.

## How the assessor invokes it

Assessors / CI invoke the harness through the Makefile target `make test-docker-e2e` —
never by calling `docker compose` steps by hand. The target owns the full
up → readiness-gate → drive → down sequence and propagates the drive's exit code, so a
green `make test-docker-e2e` is the single signal that the two-container remote-substrate
path passed. Per the WS4-0 decision, this same hermetic daemon image is also the
**default** run environment for the revived `core-loop-proof` (a subprocess daemon booted
inside this harness), with `scripts/scratch-daemon.sh` as the host-filesystem fallback.

## Auth mount point for WS4 real-agent cells (stub)

The current harness drives **twins only** and needs no model credentials. WS4's
real-agent cells will need auth, and the credential contract is fixed by the WS4-0
decision (`plans/2026-07-13-code-revamp/WS4-0-run-env-credential-decision.md`, D2):

- **Credentials arrive via a mounted `~/.claude` directory** — the host credential dir
  bind-mounted **read-only** into the daemon container at **`/root/.claude`** (the daemon
  runs as root; `~` = `/root`). This bills the subscription pool (credfence). The mount
  point is currently a documented convention only (see the stub comment at
  `Dockerfile.daemon:88-90`); nothing mounts anything yet.
- **NEVER `ANTHROPIC_API_KEY`.** An env-var API key is forbidden in every run
  environment (WS4-0 D2). Do **not** add `ANTHROPIC_API_KEY` (or `ANTHROPIC_AUTH_TOKEN`)
  to `compose.yml` `environment:`, a `.env` file, or a build arg. WS4-2 will both mount
  `${HOME}/.claude:/root/.claude:ro` and defensively `unset` those vars before the daemon
  starts (mirroring `scratch-daemon.sh`'s `env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN`),
  so the only usable credential is the mounted `/root/.claude`. A cell with no
  `/root/.claude` present is a **loud-PENDING**, never a key-auth fallback.

> Note: the `Dockerfile.daemon` stub comment still mentions `ANTHROPIC_API_KEY` alongside
> `~/.claude`; the WS4-0 decision supersedes that wording — the credential is the mounted
> `~/.claude` dir only, never an API key.

## Related

- `plans/2026-07-13-code-revamp/WS4-0-run-env-credential-decision.md` — the run-environment
  + credential decision this harness is the default for.
- `docs/methodology/TESTING.md` — the Docker cross-container E2E tier in the testing gate map.
- `internal/daemon/scenario_remote_substrate_localhost_test.go` — the localhost twin of
  the scenario this harness runs across two containers.
</content>
</invoke>
