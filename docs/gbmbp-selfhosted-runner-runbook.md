# gb-mbp self-hosted GitHub Actions runner — registration runbook

**What it is:** the operator steps to register **gb-mbp** (a large macOS box) as a
self-hosted GitHub Actions runner so the heavy `make check-short` `-race` gate can
run there instead of only on the 2-vCPU `ubuntu-latest` GitHub-hosted runner.

**Why:** the `-race` gate is proving un-green-able on `ubuntu-latest`
(`-parallel=2` flakes on saturation; `-parallel=1` timed out at 10min). gb-mbp has
the core headroom to run it reliably. This is the durable structural substrate
(hk-l7i5a, codename `gbmbp-selfhosted-runner`) — it is **parallel to and
non-gating for** the v0.5.0 A5 release. A5 still ships on the ubuntu runner.

**The workflow side is already merged** (`.github/workflows/ci.yml`, job
`check-macos`). It is **inert until an operator does BOTH of the steps below** —
registers the runner AND flips the enable flag. Until then it is skipped and
cannot affect any PR.

> Authoritative source for the CI job: `.github/workflows/ci.yml` (job
> `check-macos`). If this doc and the workflow disagree, the workflow wins.

---

## Safety model (why the merged workflow can't wedge anything)

The `check-macos` job is guarded two ways:

1. **`if: vars.GBMBP_RUNNER_ENABLED == 'true'`** — a repository variable that is
   **unset by default**. When unset (or anything but `true`), GitHub evaluates the
   `if:` to false and **skips the job before scheduling it**. A skipped job never
   asks for a `[self-hosted, macos]` runner, so there is nothing to queue and
   nothing to hang. This is the property that makes merging the workflow safe with
   no runner registered.
2. **`continue-on-error: true`** — belt-and-suspenders. Even once the runner is
   live and the flag is on, a failure in this leg is non-gating (it is also not a
   required status check in branch protection).

Net: with the flag OFF (default), CI behaves **exactly** as before this change —
the `ubuntu-latest` `check` leg (which covers the Linux `sd_notify` path) and the
`hooks` leg run identically; `check-macos` shows as skipped.

---

## Prerequisites

- Physical/SSH access to the **gb-mbp** macOS box.
- GitHub **admin** on this repo (needed to mint a runner registration token and to
  set the repo variable).
- `gh` CLI authenticated as that admin (`gh auth status`), or web-UI access to
  **Settings → Actions → Runners**.

---

## Step 1 — Register the runner on gb-mbp

On the gb-mbp box:

```bash
# 1. Create a work dir for the runner.
mkdir -p ~/actions-runner && cd ~/actions-runner

# 2. Download the latest macOS (arm64) runner package.
#    Check https://github.com/actions/runner/releases for the current version.
#    Example (substitute the current VERSION):
VERSION=2.319.1
curl -o actions-runner-osx-arm64.tar.gz -L \
  https://github.com/actions/runner/releases/download/v${VERSION}/actions-runner-osx-arm64-${VERSION}.tar.gz
tar xzf actions-runner-osx-arm64.tar.gz

# 3. Get a registration token (valid ~1h). Either:
#    a) gh CLI:
gh api -X POST repos/gregberns/harmonik/actions/runners/registration-token --jq .token
#    b) or the web UI: Settings → Actions → Runners → New self-hosted runner
#       → copy the token from the ./config.sh line it shows.

# 4. Configure. Labels MUST include self-hosted (implicit) and macos.
./config.sh \
  --url https://github.com/gregberns/harmonik \
  --token <REGISTRATION_TOKEN> \
  --labels self-hosted,macos \
  --name gb-mbp \
  --work _work \
  --unattended
```

> Confirm the labels: the CI job targets `runs-on: [self-hosted, macos]`. The
> `self-hosted` label is applied automatically to every self-hosted runner; you
> must add `macos` explicitly (the `--labels macos` above; `self-hosted` in the
> list is harmless/redundant).

Toolchain note: `make check-short` needs `go` (via `go.mod`), plus `make tools`
installs `gofumpt`/`gci`/`golangci-lint` into `.tools`. Ensure Xcode command-line
tools and a working `make`/`git` are present on gb-mbp. Go is fetched by
`actions/setup-go@v5` per-run, so no system Go pin is required.

## Step 2 — Run the runner as a service (survives reboot)

```bash
cd ~/actions-runner
./svc.sh install        # installs a launchd service under the current user
./svc.sh start
./svc.sh status         # confirm "started"
```

Verify it appears **Idle** under **Settings → Actions → Runners** (or
`gh api repos/gregberns/harmonik/actions/runners --jq '.runners[] | {name,status,labels:[.labels[].name]}'`).
You should see `gb-mbp` with status `online` and labels including `self-hosted`
and `macos`.

## Step 3 — Flip the enable flag ON

Only after Step 2 shows the runner online:

```bash
gh variable set GBMBP_RUNNER_ENABLED --repo gregberns/harmonik --body true
```

(Or web UI: **Settings → Secrets and variables → Actions → Variables → New
repository variable**, name `GBMBP_RUNNER_ENABLED`, value `true`.)

## Step 4 — Verify the leg runs

Push a trivial commit (or re-run CI on an open PR) and confirm:

```bash
gh run list --workflow=ci.yml --limit 1
# then, on that run:
gh run view <run-id>
```

You should now see the **check (Tier 2, macOS self-hosted)** job execute on gb-mbp
(no longer skipped). Expect the **srt** tests — macOS-only, skipped on the Linux
leg — to now run here.

---

## Rollback / deregister

**Fast disable (keep runner registered):** flip the flag off. The job goes back to
skipped immediately; no PR impact.

```bash
gh variable delete GBMBP_RUNNER_ENABLED --repo gregberns/harmonik
# or: gh variable set GBMBP_RUNNER_ENABLED --repo gregberns/harmonik --body false
```

**Full deregister (remove the runner from gb-mbp):**

```bash
cd ~/actions-runner
./svc.sh stop && ./svc.sh uninstall
# mint a remove-token, then unconfigure:
TOKEN=$(gh api -X POST repos/gregberns/harmonik/actions/runners/remove-token --jq .token)
./config.sh remove --token "$TOKEN"
```

Because the CI job is guarded by the flag AND `continue-on-error`, **either**
disable path leaves all other PRs unaffected. Always flip the flag OFF *before*
deregistering the runner if the flag is currently on, so no run dispatches to a
runner that is going away.
