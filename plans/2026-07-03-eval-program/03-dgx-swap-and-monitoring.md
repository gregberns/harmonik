# DGX — model-swap procedure + monitoring recon

Date: 2026-07-03 · Mode: READ-ONLY recon (no changes, no load, no model swap)
Target: `dgx.local` (192.168.1.86)

## STATUS: BLOCKED — could not log in

SSH is reachable but authentication failed. **No model-serving inspection or
monitoring-tool testing was possible** — everything below Goal 1/2/3 is a plan
of what to run *once login works*, not observed output.

### Exact failure

```
$ ssh dgx.local
gb@dgx.local: Permission denied (publickey,password).
```

- Box is up and on the LAN: `ping dgx.local` → 0% loss (~6 ms), and it is
  already in `~/.ssh/known_hosts` (rsa + ecdsa + ed25519 host keys), so this
  machine has been reached before from this laptop.
- Server: `SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.16` → an **Ubuntu** host
  (24.04-era OpenSSH). A stock amd64 `Ubuntu-*` build implies **x86-64, not an
  ARM DGX Spark / Grace-Hopper** (Spark ships DGX OS on ARM). Not confirmable
  without login.
- Auth methods offered by server: `publickey,password`.
- My key **`~/.ssh/id_ed25519`** (`SHA256:230qFcwjogNR5viBT5V4OJyqY5Y/PCKRbg4aiZfcChw`)
  IS offered to the server but **rejected** — it is not in the DGX's
  `~/.ssh/authorized_keys` for the login user.
- Default login user resolves to **`gb`** (no `~/.ssh/config` Host entry for
  dgx / 192.168.1.86 exists). Tried users `gb, nvidia, dgx, ubuntu, admin,
  operator, gregberns` with the ed25519 key — all `Permission denied`.
- Password auth exists on the server but this environment is non-interactive
  (`BatchMode`), so no password could be entered.

### What the operator must do to unblock (pick one)

1. **Authorize the key** (preferred). On the DGX, for the correct login user:
   `echo 'ssh-ed25519 AAAA…230qFcw… gb@laptop' >> ~/.ssh/authorized_keys`
   (public key is `/Users/gb/.ssh/id_ed25519.pub` on this laptop). Then tell me
   the **login username** if it isn't `gb`.
2. Or add an `~/.ssh/config` stanza on the laptop (`Host dgx` → `HostName
   192.168.1.86`, `User <real-user>`, `IdentityFile ~/.ssh/id_ed25519`) once the
   key is authorized.
3. Or run this recon yourself in a terminal where you can type the password.

---

## Goal 1 — Model-swap procedure (TO RUN once logged in)

Operator says: docker-compose under `~/models`, **ornith up at :8551**, a second
model (likely **qwen3-coder**) + a **traefik** proxy currently down.

```bash
ls -la ~/models
cat ~/models/docker-compose.y*ml            # service names, images, ports, profiles
docker compose -f ~/models/docker-compose.yml ps      # what's actually up
docker ps --format '{{.Names}}\t{{.Ports}}\t{{.Image}}'
curl -s localhost:8551/v1/models            # confirm ornith serving + model id
```

Expected swap shape (CONFIRM against the real compose file before trusting):

```bash
cd ~/models
docker compose stop  <ornith-service>       # bring current model DOWN
docker compose up -d <qwen3-service>        # bring other model UP
# if profiles are used instead of per-service up/down:
docker compose --profile qwen up -d ; docker compose --profile ornith down
docker compose logs -f <service>            # watch it become ready
```

Fill in real `docker compose` **service names**, images, host ports, and whether
swap is per-service `up/down` vs compose **profiles** vs traefik host-routing.

## Goal 2 — Monitoring tools that actually work (TO TEST, read-only)

Operator warns traditional tools may lie on this hardware. Run each; record
which returns **live util% / mem / power / temp**:

```bash
uname -m ; nvidia-smi -L                     # arch + GPU model(s)
nvidia-smi                                   # baseline table
nvidia-smi --query-gpu=utilization.gpu,memory.used,memory.total,power.draw,temperature.gpu --format=csv -l 1
nvidia-smi dmon -s pucvmet -d 1              # streaming util/pwr/clk/mem/temp
dcgmi discovery -l ; dcgmi dmon -e 203,252,155  # DCGM (often the real one on DGX)
tegrastats --interval 1000                   # ONLY if Jetson/Spark (ARM); no-op on x86
nvtop                                         # TUI; visual sanity check
```

**Poll-during-load candidate** (verbatim, adjust after testing which works):
```bash
nvidia-smi --query-gpu=timestamp,utilization.gpu,memory.used,power.draw,temperature.gpu --format=csv,noheader -l 1
```
Note anything broken/misleading (e.g. util% flat at 0 under known load, MIG
partitioning hiding utilization, dcgm-only accurate readings, tegrastats absent).

## Goal 3 — Box facts (TO CAPTURE)

`nvidia-smi -L`, `nvidia-smi --query-gpu=name,memory.total --format=csv`,
`uname -m`, `lscpu | grep -E 'Architecture|Model name|CPU\(s\)'`, `free -h`,
`cat /etc/nv_tegra_release 2>/dev/null` (present ⇒ Jetson/Spark),
`nvidia-smi --query-gpu=name --format=csv,noheader` → confirm Spark vs not.

Early signal: Ubuntu amd64 OpenSSH banner ⇒ **probably NOT a Spark** (which is
ARM/DGX-OS). Confirm with `uname -m` (expect `x86_64`).

## Beads to create (DGX eval track)

- **DGX-1** (P1, task): Restore SSH access to dgx.local for the eval agent
  (authorize `id_ed25519.pub`, confirm login user) — blocks all below.
- **DGX-2** (P2, task): Document the ornith↔qwen3-coder model-swap runbook from
  the real `~/models/docker-compose.yml` (service names + up/down/profile cmds +
  traefik role).
- **DGX-3** (P2, task): Verify which GPU-monitoring tool returns real util/mem/
  power on this hardware; record the verbatim poll command for load tests.
- **DGX-4** (P3, task): Capture box specs (GPU model, VRAM, CPU/arch, Spark? )
  into the eval-program docs.
