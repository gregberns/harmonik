# Harmonik Makefile
# Two gate targets and some helpers: `make fast` while you work, `make full`
# before anyone accepts the work. There is no third tier. Local and CI run the
# same two targets verbatim.

.DEFAULT_GOAL := fast

# Tool bin dir — keeps dev tools out of the global GOPATH.
#
# Resolved against the MAIN working tree rather than $(PWD). `.tools` holds
# built binaries and is gitignored, so a git worktree receives none of it, and
# every target that reached a tool there died with a bare ENOENT that never said
# "you are in a worktree and the tools live elsewhere". That covers fmt,
# fmt-check, lint and all three check tiers. Agents are routinely run in their
# own worktrees, so each one met this and improvised privately — and an agent
# that improvised by skipping the format step produced a commit that died at the
# fail-closed format gate much later, with no clue why.
#
# --git-common-dir returns the SHARED .git from inside a worktree as well as
# from the main checkout, so its parent is the main working tree either way.
# Falls back to $(PWD) outside a repository.
TOOLS_HOME := $(shell git rev-parse --path-format=absolute --git-common-dir 2>/dev/null | sed 's|/\.git/*$$||')
TOOLS_DIR := $(if $(TOOLS_HOME),$(TOOLS_HOME),$(PWD))/.tools
GOBIN_TOOLS := GOBIN=$(TOOLS_DIR)

# Module path (matches go.mod).
MODULE := github.com/gregberns/harmonik

# Twin-binary output directory (SH-009 in-tree default: <repo-root>/twins/).
TWINS_DIR := $(PWD)/twins

# Wall-clock budget for the agent-review pre-commit target (hk-pvcs.10).
# The pre-commit Tier 1 budget is <15s total; agent-review is an LLM call and
# gets its own hard cap. Override via: make agent-review AGENT_REVIEW_TIMEOUT=120
AGENT_REVIEW_TIMEOUT ?= 60

# `timeout` is GNU coreutils and absent from stock macOS; `gtimeout` is what
# `brew install coreutils` provides instead. Resolve whichever exists so
# agent-review doesn't hard-fail with "command not found" on a bare Mac
# (hk-x2spu: exposed once lefthook itself was made resolvable and pre-commit
# stopped silently no-op'ing). Empty means neither is installed — run
# unwrapped rather than block every commit on a missing dev dependency.
TIMEOUT_BIN := $(shell command -v timeout 2>/dev/null || command -v gtimeout 2>/dev/null)

# Commit hash stamped into twin binaries at build time (HC-043).
# Uses the shell form so the value is resolved at recipe execution time, not
# at Makefile parse time, which correctly reflects uncommitted state during
# incremental development.
COMMIT_HASH := $(shell git rev-parse HEAD)

# ---------------------------------------------------------------------------
# Core build / test
# ---------------------------------------------------------------------------

# build-harmonik: compile cmd/harmonik with the commit-hash ldflags stamp.
# The -X flag injects the current HEAD SHA into main.commitHash so that the
# daemon_started event payload carries a real git hash (hk-mz0x4).
# Output: /tmp/harmonik (matches the canonical smoke-test path).
.PHONY: build-harmonik
build-harmonik:  ## Build cmd/harmonik → /tmp/harmonik with commit-hash stamp (hk-mz0x4)
	go build -ldflags "-X main.commitHash=$(COMMIT_HASH)" -o /tmp/harmonik ./cmd/harmonik

# install-harmonik: install cmd/harmonik to $GOPATH/bin with the commit-hash ldflags
# stamp so daemon_started.binary_commit_hash is a real SHA (not "unknown").
# Use this instead of plain `go install ./cmd/harmonik` which omits the stamp.
# Bead ref: hk-mptxw (F8).
.PHONY: install-harmonik
install-harmonik:  ## Install cmd/harmonik with commit-hash stamp; use instead of plain go install (hk-mptxw)
	go install -ldflags "-X main.commitHash=$(COMMIT_HASH)" ./cmd/harmonik

.PHONY: build
build: build-harmonik  ## go build ./... + cmd/harmonik stamped binary (hk-mz0x4)
	go build ./...

.PHONY: test
test:  ## go test ./... (no race; quick smoke)
	go test ./...

# leakcheck: report processes this user abandoned — orphaned to init and of a
# shape that never legitimately outlives its parent (a bare shell, a Go test
# binary, or any non-system orphan burning CPU). Reports only, never kills.
#
# Run it after any test run that spawned helpers, and after any script that
# backgrounds a child. A test-support script once leaked 30 spin loops that ran
# 13 hours at ~551% CPU with nothing in the repo able to report it.
#
# Deliberately NOT wired into `test` or `check`. It observes the whole machine,
# not the build, so a failure would be unrelated to the code under test and
# would train people to ignore it. Keep it a thing you run, or a shell hook.
.PHONY: leakcheck
leakcheck:  ## Report abandoned processes (orphaned shells, test binaries, busy orphans)
	@./scripts/leakcheck.sh

# leakreap: kill the orphaned shells and test binaries leakcheck found. Only that
# one shape — a busy orphan of an unmodelled shape, or a live parent with a big
# child pool, is still reported and left alone for you to read.
.PHONY: leakreap
leakreap:  ## Kill orphaned shells and test binaries (leakcheck rule 1 only)
	@./scripts/leakcheck.sh --kill

# smoke-scratch: run harmonik smoke in a throw-away temp project so real-daemon
# validation never commits scratch files to the main trunk (logmine F17 / hk-nk9pu).
# Prereq: harmonik binary is built from source (this target builds it internally).
# Env overrides: HARMONIK_BIN, SMOKE_TIMEOUT, SKIP_BUILD, KEEP_DIR.
.PHONY: smoke-scratch
smoke-scratch:  ## Run harmonik smoke in a throw-away temp project (never touches main trunk; hk-nk9pu)
	scripts/smoke-scratch.sh

# test-e2e-real-claude: run the real-Claude single-mode E2E smoke test.
# Requires: claude, tmux, git, br, ntm on PATH; ANTHROPIC_API_KEY or
# CLAUDE_CODE_OAUTH_TOKEN set; harmonik buildable from source.
# Budget: 300s timeout (the agent interaction may take up to 180s).
.PHONY: test-e2e-real-claude
test-e2e-real-claude:  ## Run real-Claude E2E smoke (requires credentials + binaries on PATH)
	scripts/go-test-must-match.sh go test -tags e2e_real_claude -timeout 300s -v -run TestE2ERealClaudeSingleMode ./internal/daemon/...

# test-scenario: run the scenario tier with -race and the scenario build tag.
# Prereq: build-all compiles cmd/harmonik and the twins that daemon scenarios
# locate without a rebuild.
# Budget: 10 minutes, matching the scenario sub-run in `make full`.
#
# THE PACKAGE LIST IS DERIVED, by scripts/scenario-pkgs.sh, which asks `go list`
# what the scenario tag turns on rather than grepping for the tag line — read
# that script's header for why both greps that were tried are wrong. It used to
# be `./test/scenario/...
# ./internal/daemon/...` written by hand, under a comment claiming that "covers
# all packages that carry //go:build scenario files". That claim was false and
# had been for a long time. Scenario files had spread to four more packages, and
# 11 test functions there were compiled by NO target and run by NO target:
#   cmd/harmonik      4 (init, decisions list, decisions gate, keeper enable)
#   internal/runloop  4 (TestScenarioGateEfficacy_* — whether the scenario gate
#                        can tell a compile failure from a genuine red, which is
#                        the difference between a gate and a fail-open)
#   internal/sentinel 2 (adversary fresh-context and at-most-once-per-window)
#   internal/keeper   1 (orphaned-decision reap)
# `make vet-tagged` TYPECHECKS them, which is why they still compiled, but
# typechecking runs nothing, so they were free to rot — and one had.
# TestScenario_KeeperEnableOn058_HKF5Z was red: its fixture wrote three keeper
# scripts after runKeeperEnable started requiring four. The untagged sibling
# test was updated at the time. This one was not, because nothing ran it.
# The 11 tagged functions themselves cost under 8 seconds together. Refs hk-od9d4.
#
# WHAT THE WIDENING COSTS. `go test -tags=scenario ./cmd/harmonik` runs the WHOLE
# package, not only its tagged files, so four more packages means every ordinary
# test in them under -race too — about 950 more test functions. In isolation they
# cost 140 seconds, cmd/harmonik being 138 of it.
#
# In the tier they cost almost nothing, because `go test` runs packages
# concurrently and internal/daemon is 453 seconds on its own. Measured
# 2026-08-07, same box, same flags, back to back:
#
#   old list (test/scenario + internal/daemon)   464s, 18 failures
#   new list (six packages)                      471s, 17 failures
#
# So seven seconds. Note the failure counts: THIS TIER IS ALREADY RED, and was
# before this change — see hk-97gcz and hk-ynohn. The 13 tests that fail in both
# runs are the same 13. Widening added exactly one new red,
# TestWatcher_WarnCooldown_SuppressesImmediateRefire, which is a pre-existing
# keeper flake that fails standalone under plain `-short` on an untouched
# checkout, now hk-keeper-warn-cooldown-clock-bet-c5umc. The rest of the
# difference is this tier's own run-to-run churn.
#
# Deriving the list rather than reaching for `./...` still matters. `./...` would
# add every remaining package in the tree on the same terms, and `make full` has
# already run all of them under -short in the step before this one.
#
# See docs/methodology/TESTING.md §Scenario fixture determinism recipe for the
# worktree-factory / merge-mutex / phase-aware-twin / Skip* recipe used here.
.PHONY: test-scenario
test-scenario: build-all  ## Run scenario tier (-race, -tags=scenario, 10m budget; prereq: build-all)
	@scenario_pkgs=$$(scripts/scenario-pkgs.sh) || exit 1; \
	echo "test-scenario: packages carrying //go:build scenario files:"; \
	echo "$$scenario_pkgs" | sed 's/^/  /'; \
	scenario_log=$$(mktemp); \
	status=0; \
	go test -v -race -tags=scenario -timeout 10m $$scenario_pkgs >"$$scenario_log" 2>&1 || status=$$?; \
	cat "$$scenario_log"; \
	awk '/^--- SKIP:/ {n++} END {printf "scenario skips: %d\n", n+0}' "$$scenario_log"; \
	rm -f "$$scenario_log"; \
	exit $$status

# test-subprocess: WS2.4 non-docker subprocess daemon-boot smoke. Execs the real
# built harmonik binary as a separate process, waits for the daemon unix socket,
# submits one bead via the CLI, and asserts a terminal run outcome. Billing-free
# (dispatch routed to the generic twin via the codexdriver substrate; no real
# agent, no tmux, no network). Dedicated `subprocess` build tag isolates it from
# the default build/test (it is NOT part of -tags=scenario). Budget: 5 minutes.
# Cite: plans/2026-07-13-code-revamp/M6-PLAN.md §WS2.4.
.PHONY: test-subprocess
test-subprocess:  ## Run WS2.4 non-docker subprocess boot smoke (-tags=subprocess; needs go+br+git on PATH)
	scripts/go-test-must-match.sh go test -tags=subprocess -timeout 5m -count=1 ./cmd/harmonik -run TestSubprocessDaemonBootSmoke

# core-loop-lt: WS4-5 FORCED, single-entry LT-leg command. THE assessor's live-verify
# gate — drives the real task-processing loop on a scratch daemon across the core-loop
# matrix and returns non-zero unless EVERY cell is green (any red OR pending OR skip fails,
# the T9 zero-PENDING gate), so a partial matrix can never be mistaken for a pass. Emits a
# machine-readable per-cell grid (marker `MATRIX_JSON …` on the last stdout line) that the
# assessor folds into its LT verdict. Forced-LOCAL: real pi/codex (+claude, WS4-4) agents,
# never a CI required check (see docs/methodology/TESTING.md "Gate tiers & risk-tiering").
# Override the scratch dir with LT_SCRATCH=… . Cite: M6-PLAN §WS4-5.
#
# hk-xy9ym: this target must be SELF-SERVING for the CHECKED-OUT (pinned) code. It:
#   1. wipes + inits LT_SCRATCH from the LOCAL checkout ($(CURDIR)) at this checkout's
#      exact HEAD, passed to `init --rev`. hk-xy9ym originally got this by wiping the
#      directory first and cloning the local path, which worked only because of the wipe:
#      `init` had no revision argument at all. hk-scratch-daemon-audits-wrong-tree-zljvm
#      made the revision REQUIRED and made `init` force and verify the checkout, so the
#      guarantee now comes from naming the commit rather than from the `rm -rf` above it.
#   2. seeds the fixture beads into the fresh scratch DB + writes the cell->bead_id map, so
#      the scoped cell actually LAUNCHES (bare/un-seeded => every cell PENDING, no agent).
#   3. scopes to pi:local — the one leg that proves the full core-loop contract e2e with a
#      real agent (single-mode dispatch -> real pi/ornith change -> per-bead-branch landing).
#      codex (operator runs codex-minimal), claude (WS4-4, --enable-claude), remote (needs a
#      reachable tcp:// worker) and the pi-dot round-trip (stronger convergence model,
#      EXTRA_CELLS) stay behind their existing opt-in knobs, so a bare run never red/skip/
#      pends on an intentionally-absent leg. Widen with LT_HARNESSES/LT_SUBSTRATES.
LT_SCRATCH ?= /tmp/h/core-loop-lt
LT_SPECS   ?= scenarios/core-loop-proof/cells.json
# Per-cell seed map (cell<TAB>bead_id) that core-loop-seed.sh writes and core-loop-matrix.sh
# consumes via MATRIX_SEED_MAP. Lives under the (wiped-each-run) scratch so it is always
# regenerated against THIS run's freshly-created seed beads.
LT_SEED_MAP   ?= $(LT_SCRATCH)/.harmonik/matrix-seed-map.tsv
LT_HARNESSES  ?= pi
LT_SUBSTRATES ?= local
.PHONY: core-loop-lt
core-loop-lt: build-all  ## WS4-5 forced LT gate: inits scratch from LOCAL checkout + seeds pi:local, non-zero on any non-green cell + JSON grid (hk-xy9ym)
	@# hk-xy9ym: guard the `rm -rf` below — never wipe an empty/root/repo-root path.
	@test -n "$(LT_SCRATCH)" || { echo "core-loop-lt: LT_SCRATCH is empty — refusing"; exit 1; }
	@case "$(LT_SCRATCH)" in /|.|..) echo "core-loop-lt: unsafe LT_SCRATCH='$(LT_SCRATCH)' — refusing"; exit 1;; esac
	@# Resolve symlinks on BOTH sides and refuse if LT_SCRATCH is the repo root, $$HOME,
	@# /, or an ANCESTOR of the repo (wiping it would destroy the fleet checkout). Only
	@# checked when the dir already exists — a not-yet-created scratch is a safe no-op wipe.
	@if [ -d "$(LT_SCRATCH)" ]; then \
		lt="$$(cd "$(LT_SCRATCH)" && pwd -P)"; \
		repo="$$(cd "$(CURDIR)" && pwd -P)"; \
		home="$$(cd "$$HOME" 2>/dev/null && pwd -P || echo /nonexistent-home)"; \
		if [ "$$lt" = "$$repo" ] || [ "$$lt" = "$$home" ] || [ "$$lt" = "/" ]; then \
			echo "core-loop-lt: LT_SCRATCH resolves to '$$lt' (repo root, \$$HOME, or /) — refusing to wipe"; exit 1; fi; \
		case "$$repo/" in "$$lt"/*) echo "core-loop-lt: LT_SCRATCH '$$lt' is an ancestor of the repo — refusing to wipe"; exit 1;; esac; \
	fi
	@# Stop any stale scratch daemon (pidfile-scoped; fleet-safe) BEFORE wiping the dir so a
	@# leftover run from a prior invocation is never orphaned. Tolerate a not-yet-created scratch.
	bash scripts/scratch-daemon.sh down "$(LT_SCRATCH)" 2>/dev/null || true
	@# Guarantee a FRESH clone of the PINNED code: wipe, then init from the LOCAL
	@# checkout at THIS checkout's HEAD. --rev is what pins it; the wipe only saves
	@# init from having to be told --reuse.
	rm -rf "$(LT_SCRATCH)"
	bash scripts/scratch-daemon.sh init "$(LT_SCRATCH)" --source "$(CURDIR)" --rev "$$(git -C "$(CURDIR)" rev-parse HEAD)"
	@# Seed the fixture beads into the fresh scratch DB + emit the cell->bead_id map so the
	@# scoped cell launches (isolates origin + pre-creates the pi landing branch).
	bash scripts/core-loop-seed.sh "$(LT_SCRATCH)" "$(LT_SEED_MAP)"
	@# Run the scoped matrix: MATRIX_SEED_MAP wires per-cell seeds; --harnesses/--substrates
	@# scope to pi:local; --assert --gate --json keep the forced zero-PENDING gate + JSON grid.
	MATRIX_SEED_MAP="$(LT_SEED_MAP)" bash scripts/core-loop-matrix.sh "$(LT_SCRATCH)" --harnesses "$(LT_HARNESSES)" --substrates "$(LT_SUBSTRATES)" --assert --gate --json --specs "$(LT_SPECS)"

# test-docker-e2e: WS2.3 remote-substrate E2E across two containers. Builds the
# daemon (box A) + worker images, brings them up on a compose bridge network (the
# worker reachable as `worker`), waits until `ssh worker true` from the daemon
# succeeds (the worker installs the daemon's client key from the shared `keys`
# volume once both are up), then execs the compiled scenario test binary baked
# into the daemon image against HARMONIK_E2E_SSH_HOST=worker. origin.git + the
# worker clone live on the shared volume at the identical /shared path in both
# containers (CRUX 2). ALWAYS tears the stack down with `down -v` (fresh keys +
# repos each run), preserving the drive's exit code. Needs NO host ~/.ssh setup —
# keys are generated inside the containers at boot. Cite: M6-PLAN §WS2.3.
COMPOSE_E2E := test/docker/compose.yml
.PHONY: test-docker-e2e
test-docker-e2e:  ## WS2.3 remote-substrate E2E over ssh across daemon+worker containers (needs docker)
	docker compose -f $(COMPOSE_E2E) up -d --build
	@echo "test-docker-e2e: waiting for passwordless ssh daemon->worker …"
	@ok=0; \
	for i in $$(seq 1 30); do \
	  if docker compose -f $(COMPOSE_E2E) exec -T daemon \
	       ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=5 worker true >/dev/null 2>&1; then \
	    ok=1; echo "test-docker-e2e: ssh worker reachable (attempt $$i)"; break; \
	  fi; \
	  sleep 0.5; \
	done; \
	if [ "$$ok" != "1" ]; then \
	  echo "test-docker-e2e: FATAL ssh daemon->worker never came up" >&2; \
	  docker compose -f $(COMPOSE_E2E) logs --no-color >&2 || true; \
	  docker compose -f $(COMPOSE_E2E) down -v || true; \
	  exit 1; \
	fi; \
	out=$$(docker compose -f $(COMPOSE_E2E) exec -T daemon \
	  /usr/local/bin/remote-substrate.test \
	  -test.run '^TestScenario_RemoteSubstrate_Localhost_DOT_E2E$$' -test.v 2>&1); \
	rc=$$?; \
	echo "$$out"; \
	if [ "$$rc" = "0" ] && ! echo "$$out" | grep -q -- '--- PASS: TestScenario_RemoteSubstrate_Localhost_DOT_E2E'; then \
	  echo "test-docker-e2e: FATAL no PASS line for the e2e — -test.run matched nothing (a renamed or moved test exits 0 with 'no tests to run')" >&2; \
	  rc=1; \
	fi; \
	docker compose -f $(COMPOSE_E2E) down -v; \
	exit $$rc

# ---------------------------------------------------------------------------
# codex-app-server test taxonomy (T5, hk-oe86p)
# Four tiers: L0 unit / L1 contract / L2 integration / L3 live.
# ---------------------------------------------------------------------------

# test-codex-l012: run L0+L1+L2 codex-app-server taxonomy tests + the structured
# INPUT-driver harness (agent-input-substrate M2, T9): codexinput reactor,
# codexdriver, and the L0–L3 input harness + 4×strata×EventN fault matrix +
# bounded-liveness oracle. CODEX_LIVE=0 (default) — captured corpus, no live
# process. GATE: must be green before any codex-app-server deploy or protocol
# change. Includes the pre-deploy drift canaries + the SC6 gate trio (forbidigo /
# depguard / capture-pane) via `make codex-sc6-gates`, the N=10 determinism
# oracle, and the per-file coverage floor — this is the C6-deletion gate, so it
# blocks a deploy, not merely a CI branch check.
.PHONY: test-codex-l012
test-codex-l012:  ## Codex L0/L1/L2 + input-driver harness + fault matrix + N=10 + coverage + SC6 gates (CODEX_LIVE=0; hk-oe86p, T9)
	go test -count=1 ./internal/codextest/... ./internal/codexwire/... ./internal/codexdigitaltwin/... ./internal/codexreactor/... ./internal/codexinput/... ./internal/codexdriver/...
	scripts/codex-oracle-n10.sh 10
	scripts/codex-coverage-gate.sh
	$(MAKE) codex-capture-pane-gate

# codex-capture-pane-gate: the SC6 capture-pane grep ratchet — the structured
# input driver must never scrape a tmux pane (capture-pane is an exec-arg string,
# not an import, so a grep gate is the cheapest enforcement; the forbidigo +
# depguard halves of the SC6 trio live in .golangci.yml).
.PHONY: codex-capture-pane-gate
codex-capture-pane-gate:  ## SC6: forbid `capture-pane` in the structured input-driver packages (T9)
	scripts/codex-capture-pane-gate.sh

# transport-freeze-gate: the P2 E4 extraction ratchet — the reverse-tunnel
# concern left internal/daemon for internal/transport/tunnel, and depguard can
# only fence the import edge, not the creation of a new file. This grep gate
# fails if a reverse-tunnel-shaped file or one of the moved symbols reappears in
# internal/daemon. Wired into `make fast` via freeze-gates.
.PHONY: transport-freeze-gate
transport-freeze-gate:  ## P2 E4: forbid new reverse-tunnel files or moved symbols in internal/daemon
	scripts/transport-freeze-gate.sh

# queuewiring-freeze-gate: the P2 E3 extraction ratchet — the queue-ownership
# concern (QueueStore, the brcli->queue.BeadLedger bridge, the operator
# pause/resume consumer) left internal/daemon for internal/queuewiring, and
# depguard can only fence the import edge, not the creation of a new file. This
# grep gate fails if a queue-ownership-shaped file or one of the moved symbols
# reappears in internal/daemon. Wired into `make fast` via freeze-gates.
.PHONY: queuewiring-freeze-gate
queuewiring-freeze-gate:  ## P2 E3: forbid new queue-ownership files or moved symbols in internal/daemon
	scripts/queuewiring-freeze-gate.sh

# crewrun-freeze-gate: the P2 E2 extraction ratchet — the crew launch contract
# (the crew-start/crew-stop RPC payloads, the persistent-session launch-spec
# builder, the crew-scoped harness resolver, the mission front-matter readers,
# the idle-crew reaper) left internal/daemon for internal/crewrun, and depguard
# can only fence the import edge, not the creation of a new file. This grep gate
# fails if a crew-launch-shaped file or one of the moved symbols reappears in
# internal/daemon. Wired into `make fast` via freeze-gates.
.PHONY: crewrun-freeze-gate
crewrun-freeze-gate:  ## P2 E2: forbid new crew-launch files or moved symbols in internal/daemon
	scripts/crewrun-freeze-gate.sh

# harnesscodex-freeze-gate: the P2 E1a extraction ratchet — the codex harness
# implementation (the Harness impl, the launch-spec builder, the JSONL parser,
# the stale-WAL and billing guards, the Refs-trailer fallback, the no-work
# detector) left internal/daemon for internal/harness/codex, and depguard can
# only fence the import edge, not the creation of a new file. This grep gate
# fails if a codex-harness-shaped file or one of the moved symbols reappears in
# internal/daemon. Wired into `make fast` via freeze-gates.
.PHONY: harnesscodex-freeze-gate
harnesscodex-freeze-gate:  ## P2 E1a: forbid new codex-harness files or moved symbols in internal/daemon
	scripts/harnesscodex-freeze-gate.sh

# harnessclaude-freeze-gate: the P2 E1b extraction ratchet — the claude harness
# implementation (the Harness impl + the claude-hook-bridge launch-spec builder)
# left internal/daemon for internal/harness/claude, and depguard can only fence
# the import edge, not the creation of a new file. This grep gate fails if a
# claude-harness-shaped file or one of the moved symbols reappears in
# internal/daemon. claudeheartbeat.go (harness-blind heartbeat emitter) and
# claudeworktreesweep.go (CLI-leftover janitor) are named exceptions — see the
# script header. Wired into `make fast` via freeze-gates.
.PHONY: harnessclaude-freeze-gate
harnessclaude-freeze-gate:  ## P2 E1b: forbid new claude-harness files or moved symbols in internal/daemon
	scripts/harnessclaude-freeze-gate.sh

# harnesspi-freeze-gate: the P2 E1c extraction ratchet — the pi harness
# implementation (Harness impl, launch-spec builder, NDJSON parser, billing
# guard, Refs:-trailer fallback) left internal/daemon for internal/harness/pi,
# and depguard can only fence the import edge, not the creation of a new file.
# This grep gate fails if a pi-harness-shaped file or one of the moved symbols
# reappears in internal/daemon. pi_profile_resolve.go (claim-time daemon wiring
# over projectconfig) is a named exception — see the script header. The gate
# also asserts test-pi-live below still points at a package that HAS the test,
# because `go test -run` exits 0 on an empty match. Wired into `make fast` via
# freeze-gates.
.PHONY: harnesspi-freeze-gate
harnesspi-freeze-gate:  ## P2 E1c: forbid new pi-harness files or moved symbols in internal/daemon
	scripts/harnesspi-freeze-gate.sh

# runmerge-freeze-gate: the P2 E5 RT13 extraction ratchet — the run-branch merge
# path (the EM-052/EM-053 merge-to-main sequence, the pre-rebase worktree
# hygiene, the gofumpt/gci format gate, the run-context strip, the review-trailer
# amend) left internal/daemon for internal/runmerge, and depguard can only fence
# the import edge, not the creation of a new file. This grep gate fails if a
# merge-path-shaped file or one of the moved symbols reappears in internal/daemon,
# or if the daemon re-acquires a raw `git merge` / `git rebase` exec.
# beadsmergedriver.go (git merge-DRIVER registration), scenariotest/ and
# branching.go (the WM-019b task-branch landing path) are named exceptions — see
# the script header. Wired into `make fast` via freeze-gates.
.PHONY: runmerge-freeze-gate
runmerge-freeze-gate:  ## P2 E5 RT13: forbid new merge-path files or moved symbols in internal/daemon
	scripts/runmerge-freeze-gate.sh

# projectconfig-freeze-gate: the P2 LIFT crit 5 extraction ratchet — the
# .harmonik/config.yaml loader + config value types left internal/daemon for
# internal/projectconfig (a pure leaf). depguard fences the import edge; this
# gate forbids re-creating a config-loader file OR re-declaring/re-aliasing a
# moved symbol back inside internal/daemon (the operator's no-alias ruling).
.PHONY: projectconfig-freeze-gate
projectconfig-freeze-gate:  ## P2 LIFT crit 5: forbid new config-loader files, moved symbols, or aliases in internal/daemon
	scripts/projectconfig-freeze-gate.sh

# runlaunch-freeze-gate: the P2 E5 RT19b extraction ratchet — the run path's
# launch-time effects (the CHB-018 pre-exec relay, the HC-056 readiness
# deadlines and their sentinel, the spawn-cap / tmux-window / agent-ready
# anomaly events, the implementer phase-complete report, the force-teardown
# backstop) left internal/daemon for internal/runlaunch, internal/substrate and
# internal/harness/shared. Symbol-anchored, not name-anchored: internal/daemon
# legitimately keeps ~25 other emit* helpers that are E5 LIFT targets, so a
# '*events*.go' file scan would fire on correct code. Wired into `make fast` via
# freeze-gates.
.PHONY: runlaunch-freeze-gate
runlaunch-freeze-gate:  ## P2 E5 RT19b: forbid re-declaring the moved launch-effect symbols in internal/daemon
	scripts/runlaunch-freeze-gate.sh

# runloop-freeze-gate: the P2 LIFT extraction ratchet (chunk L0 onward) — the run
# machine's boundary contract (the PORT interfaces LedgerPort … RunRegistryPort
# and the BUNDLES RunPorts / RunEnv / SharedHandles) left internal/daemon for
# internal/runloop. depguard fences the import edge; this ratchet forbids
# re-declaring a moved TYPE back in internal/daemon. The daemon KEEPS the concrete
# adapters + constructors (daemon → runloop, the legal direction); the `= runloop.`
# forwarding arm allows the daemon-local aliases. Ratcheted: later chunks append
# their moved run-path filenames/symbols. Wired into `make fast` via freeze-gates.
.PHONY: runloop-freeze-gate
runloop-freeze-gate:  ## P2 LIFT L0: forbid re-declaring the moved run-path port/bundle types in internal/daemon
	scripts/runloop-freeze-gate.sh

# readywait-freeze-gate: the P2 E5 RT14 extraction ratchet — the open-coded
# agent_ready WAIT left internal/daemon. Every launch/ready/brief segment now
# runs on the runexec Dispatch machine via dispatchSegment, whose ClockPort-timed
# TimerAgentReady is the one ready bound (FakeClock-drivable, which
# waitAgentReady's raw time.After never was). depguard cannot express "do not
# re-hand-roll a wall-clock wait", so this grep ratchet closes that door: no
# re-declaration of the retired symbols, no raw wall-clock in the run-path files
# that are clean today or inside beadRunOne, and all four launch sites still bind
# through the seam. Slice RT19c (landed f839121ff) clock-ported the Working-phase
# watchdogs (pasteinject.go, dot_gate.go's pasteInjectQuitOnGateFile,
# waitsocketgrace.go, postreadyhang.go) onto the injected ClockPort and added all
# four to check (2), so they are now IN scope, not out. Wired into `make fast` via
# freeze-gates.
.PHONY: readywait-freeze-gate
readywait-freeze-gate:  ## P2 E5 RT14: forbid re-hand-rolling the agent_ready wait in internal/daemon
	scripts/readywait-freeze-gate.sh

# workersbootwire-freeze-gate: the P2 E4c extraction ratchet — the remote-worker
# registry BOOT WIRING (BuildRegistry / BuildRegistryWithRunner /
# BootHealthRunner, formerly buildWorkerRegistry & friends in workloop.go) left
# internal/daemon for internal/workers/bootwire.go, and depguard can only fence
# the import edge, not the creation of a new file. This grep gate fails if a
# worker-registry-construction file or one of the moved symbols reappears in
# internal/daemon, or if the daemon re-acquires a direct workers.NewRegistry
# construction. Wired into `make fast` via freeze-gates.
.PHONY: workersbootwire-freeze-gate
workersbootwire-freeze-gate:  ## P2 E4c: forbid new worker-registry boot-wiring files or moved symbols in internal/daemon
	scripts/workersbootwire-freeze-gate.sh

# runloop-emitter-gate: the P2 E5 RT16 ratchet — the DOT run path reaches its
# event bus through EmitterPort (internal/daemon/runports.go), not through the
# workLoopDeps bus field. RT16 converted 108 direct field reads on the six mover
# files to 8 port reads so RT18's re-signature is an 8-line change, not a
# 108-line one. depguard cannot express "reach this dependency through its
# port", so this grep gate rations the field per file, asserts EmitterPort is
# still an alias, and pins each mover to the seam. Wired into `make fast` via
# freeze-gates.
.PHONY: runloop-emitter-gate
runloop-emitter-gate:  ## P2 E5 RT16: forbid bypassing EmitterPort with a raw bus-field read in internal/daemon
	scripts/runloop-emitter-gate.sh

# workloop-scheduler-freeze-gate: the Seam A split ratchet — the dispatch
# SCHEDULER (runWorkLoop plus the 25 helpers only it reaches) left
# internal/daemon/workloop.go for internal/daemon/scheduler.go. Both files are
# one Go package, so the compiler cannot stop a later edit from pasting the
# scheduler back beside beadRunOne. This grep gate fails if a scheduler symbol is
# declared in workloop.go, if a scheduler symbol is declared anywhere other than
# scheduler.go, or if either side of the seam is missing — the last case is named
# so a deleted target fails loudly instead of passing on an empty grep. Wired
# into `make fast` via freeze-gates.
.PHONY: workloop-scheduler-freeze-gate
workloop-scheduler-freeze-gate:  ## Seam A: forbid moving the dispatch scheduler back into workloop.go
	scripts/workloop-scheduler-freeze-gate.sh


# vet-tagged: typecheck the files that `go vet ./...` cannot see (hk-i1m20).
# Static analyzers and the default build compile ONLY the untagged build, so a
# call site behind a `//go:build <tag>` line emits zero signal when it breaks —
# and an analyzer claim like unparam's "parameter X always receives value V" is
# scoped to the build it analyzed, not to the repo. A real arity-increase break
# in cmd/harmonik behind `//go:build scenario` survived two weeks undetected,
# with five live assertions dead the whole time.
#
# ONE invocation, not a loop: build tags are ADDITIVE, so a single combined vet
# compiles the union of all tagged files in ~4.6s cold / ~1.1s warm, where the
# same tags as separate invocations cost ~25s for identical coverage.
# `go vet` typechecks _test.go files but runs nothing, which is all this needs.
#
# Tag set = every tag with real files in the repo, minus `ignore` (deliberately
# uncompiled) and the GOOS constraints (darwin/linux/windows), which are chosen
# by the toolchain rather than by -tags. Add a tag here whenever one is
# introduced, or its files go back to being invisible.
TAGGED_BUILD_TAGS := scenario,integration,e2e_real_claude,subprocess,crash

.PHONY: vet-tagged
vet-tagged:  ## hk-i1m20: typecheck every build-tagged file (invisible to plain `go vet ./...`)
	go vet -tags=$(TAGGED_BUILD_TAGS) ./internal/... ./cmd/... ./test/...

# test-codex-live: run L3 live tests against a real codex app-server process.
# Requires: CODEX_LIVE=1, codex binary on PATH (or CODEX_BIN=<path> set),
# valid codex auth (~/.codex/auth.json). Budget: 90s per test, 2 scenarios.
# L3 happy-path = PRE-DEPLOY E2E GATE for the codex-app-server integration.
.PHONY: test-codex-live
test-codex-live:  ## Codex-app-server L3 live gate (CODEX_LIVE=1 required; token-capped; hk-oe86p)
	CODEX_LIVE=1 scripts/go-test-must-match.sh go test -timeout 180s -count=1 -run TestL3_ ./internal/codextest/...

# capture-fixtures: deliberate, budget-capped corpus capture.
# Requires: CODEX_LIVE=1, codex binary on PATH, valid codex auth.
# Output: testdata/codex-app-server/corpus/<session>.jsonl
# Ledger: testdata/codex-app-server/corpus/CAPTURE-LOG.md (manual update required).
# Run deliberately — NOT part of make test or CI. Token budget: one minimal turn.
.PHONY: capture-fixtures
capture-fixtures:  ## Capture new codex corpus (CODEX_LIVE=1 required; deliberate+token-capped; hk-oe86p)
	@echo "capture-fixtures: launching budget-capped codex session via L3 live harness"
	@echo "  Corpus output: testdata/codex-app-server/corpus/"
	@echo "  Update testdata/codex-app-server/corpus/CAPTURE-LOG.md after capture."
	CODEX_LIVE=1 scripts/go-test-must-match.sh go test -timeout 120s -count=1 -v -run TestL3_ ./internal/codextest/...

# capture-claude-fixtures: real-Claude twin-parity capture (WS3-Claude-A).
# Requires an AUTH'D, tmux-capable box: claude/tmux/git/br/ntm on PATH + a
# subscription OAuth session. On a box without those the test SKIPS cleanly.
# Output: testdata/twin-parity/claude/<scn>/{wire.ndjson,events.jsonl,meta.yaml}
# Credfence (codename:credfence, D2): run under `env -u ANTHROPIC_API_KEY
# -u ANTHROPIC_AUTH_TOKEN` so the run bills the subscription pool — NEVER an API
# key (mirrors scripts/scratch-daemon.sh:237-240). Distinct from capture-fixtures
# (codex corpus) — do not conflate.
.PHONY: capture-claude-fixtures
capture-claude-fixtures:  ## Capture real-Claude twin-parity fixtures (e2e_real_claude; auth+tmux required; credfenced)
	@echo "capture-claude-fixtures: real-Claude capture into testdata/twin-parity/claude/$${HARMONIK_CAPTURE_SCN:-happy-path}/"
	@echo "  SKIPS cleanly without claude/tmux/br/ntm binaries + auth."
	env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN \
	  HARMONIK_WIRE_CAPTURE_DIR=$(CURDIR)/testdata/twin-parity/claude \
	  HARMONIK_CAPTURE_COMMIT_SHA=$$(git rev-parse --short HEAD) \
	  scripts/go-test-must-match.sh go test -tags e2e_real_claude -timeout 300s -count=1 -v \
	    -run TestCaptureClaudeFixtures ./internal/daemon/...

# test-twin-parity-claude: the ROUTINE Claude twin-parity gate (WS3-Claude-D).
# Compares the canonical twin's --replay-path output (committed wire.ndjson)
# against the committed Claude reference capture using only F1's equivalence
# library: ordered kind-sequence + terminal outcome equivalent, hook/causal
# timing within tolerance, and a drifted twin caught with a first-divergence
# diff. Cheap, deterministic, zero-token — NO auth, tmux, or live model needed
# (distinct from capture-claude-fixtures, the separate PERIODIC live re-capture).
.PHONY: test-twin-parity-claude
test-twin-parity-claude:  ## Routine Claude twin-parity gate (twin-vs-reference-capture; zero-token, deterministic)
	scripts/go-test-must-match.sh go test -v -count=1 -run 'ClaudeParity|TwinParityCorpus' ./internal/twinparity/...

# test-pi-live: the REAL-BOX-GATED pi oracle (WS3-pi / pi-A). Drives a real
# `pi --mode json` single-turn, asserts the terminal NDJSON sequence
# (session → agent_end), and writes testdata/twin-parity/pi/<scn>/{ndjson,
# events.jsonl}. DEFAULT-SKIPPED without PI_LIVE=1; needs pi on PATH (or PI_BIN),
# PI_PROVIDER + PI_MODEL, and valid pi provider auth. Anti-false-green:
# HARMONIK_REQUIRE_PI_LIVE=1 turns a can't-run skip into a Fatalf.
.PHONY: test-pi-live
test-pi-live:  ## Real-pi oracle gate (PI_LIVE=1 required; pi provider auth; writes pi twin-parity fixtures)
	PI_LIVE=1 scripts/go-test-must-match.sh go test -timeout 180s -count=1 -run TestPiA_ ./internal/harness/pi/...

# test-twin-parity-pi: the ROUTINE pi twin-parity gate (WS3-pi / pi-C). Compares
# the pi twin's NDJSON (committed testdata/twin-parity/pi/happy-path-sample/ndjson
# — deterministic `harmonik-twin-pi --scenario happy-path` output) against the
# reference capture on the pi-native wire spine (session → agent_end) + the
# daemon-projected durable terminal triad, and proves a drifted twin is caught
# with a first-divergence diff. Cheap, deterministic, zero-token — NO auth or live
# pi needed (distinct from test-pi-live, the separate REAL-BOX re-capture).
.PHONY: test-twin-parity-pi
test-twin-parity-pi:  ## Routine pi twin-parity gate (twin-vs-reference-capture; zero-token, deterministic)
	scripts/go-test-must-match.sh go test -v -count=1 -run 'PiParity|TwinParityCorpus' ./internal/twinparity/...

# ---------------------------------------------------------------------------
# Keeper replay test taxonomy (T10; session-restart-substrate)
# Four tiers: L0 unit / L1 contract / L2 integration / L3 live, mirroring the
# codex pair (measurement-design §3; RS-017/018/019).
# ---------------------------------------------------------------------------

# test-keeper-l012: run L0+L1+L2 keeper taxonomy tests + the corpus drift
# canary. KEEPER_LIVE=0 (default) — corpus-driven, zero-token, no live pane.
# GATE: must be green before any keeper cycle change deploys.
.PHONY: test-keeper-l012
test-keeper-l012:  ## Keeper replay L0/L1/L2 gate (KEEPER_LIVE=0; corpus-driven)
	go test -count=1 ./internal/keepertest/... ./internal/keepertwin/... ./internal/keeper/...

# test-keeper-live: run the L3 one-cycle tmux smoke against a REAL tmux pane.
# Requires: KEEPER_LIVE=1, tmux on PATH. The pane runs a bare shell (no model,
# no daemon). No re-capture target exists (unlike codex capture-fixtures): the
# corpus source is the frozen baseline log — scripts/extract-keeper-corpus.py
# is a deterministic rebuild, not a token-capped capture.
.PHONY: test-keeper-live
test-keeper-live:  ## Keeper L3 live gate (KEEPER_LIVE=1 required; one-cycle tmux smoke)
	KEEPER_LIVE=1 scripts/go-test-must-match.sh go test -timeout 180s -count=1 -run TestL3_ ./internal/keepertest/...

# ---------------------------------------------------------------------------
# Twin-binary targets
# ---------------------------------------------------------------------------

# build-twin-generic: compile cmd/harmonik-twin-generic/ into twins/generic-twin.
# This is the generic test handler twin that emits harmonik-native NDJSON
# directly (testing the back half of the pipeline without simulating Claude's
# lifecycle). Renamed from harmonik-twin-claude per hk-w5vra.1.
# Output path satisfies SH-009 in-tree default (<repo-root>/twins/).
# Cite: specs/scenario-harness.md §4.3.SH-009;
#       specs/handler-contract.md §4.8.HC-036(c).
.PHONY: build-twin-generic
build-twin-generic:  ## Build cmd/harmonik-twin-generic → twins/generic-twin (SH-009 / HC-043)
	@mkdir -p $(TWINS_DIR)
	go build -ldflags "-X main.commitHash=$(COMMIT_HASH)" -o $(TWINS_DIR)/generic-twin ./cmd/harmonik-twin-generic

# build-twin-claude: compile the Claude lifecycle twin at the path used by
# daemon scenario fixtures.
.PHONY: build-twin-claude
build-twin-claude:  ## Build cmd/harmonik-twin-claude → ./harmonik-twin-claude
	go build -ldflags "-X main.commitHash=$(COMMIT_HASH)" -o ./harmonik-twin-claude ./cmd/harmonik-twin-claude

# build-twin-fail: compile the failure twin used by T2 daemon scenarios.
.PHONY: build-twin-fail
build-twin-fail:  ## Build test/twins/fail-immediately → ./twin-fail
	go build -ldflags "-X main.commitHash=$(COMMIT_HASH)" -o ./twin-fail ./test/twins/fail-immediately

# build-twin-hang: compile the hanging twin used by T2 daemon scenarios.
.PHONY: build-twin-hang
build-twin-hang:  ## Build test/twins/hang → ./twin-hang
	go build -ldflags "-X main.commitHash=$(COMMIT_HASH)" -o ./twin-hang ./test/twins/hang

# build-twin-pi: compile cmd/harmonik-twin-pi/ into twins/pi-twin. The pi test
# twin emits pi's `--mode json` NDJSON lifecycle (session → message_start/end →
# agent_end) deterministically so it can stand in for a real pi session in
# scenario tests and the twin-parity gate (WS3-pi). HC-043 commit stamp injected.
.PHONY: build-twin-pi
build-twin-pi:  ## Build cmd/harmonik-twin-pi → twins/pi-twin (SH-009 / HC-043)
	@mkdir -p $(TWINS_DIR)
	go build -ldflags "-X main.commitHash=$(COMMIT_HASH)" -o $(TWINS_DIR)/pi-twin ./cmd/harmonik-twin-pi

# twins: build the scenario twin binaries.
.PHONY: twins
twins: build-twin-generic build-twin-claude build-twin-pi build-twin-fail build-twin-hang  ## Build scenario twins

# build-all: build the module + all twin binaries.
# Suitable as a pre-scenario-test warmup target.
.PHONY: build-all
build-all: build twins  ## go build ./... + all twins (full build artifact set)

# ---------------------------------------------------------------------------
# Secret scan — refuses content that adds API keys, credential patterns, or
# .env files.
#
# THIS TARGET IS THE STANDALONE ONE, and it is not where the scan runs. It reads
# the INDEX, for use before a commit is made. The gates run AFTER the commit,
# when the index is empty, so they call the script directly with a committed
# scope: `--head-only` from gate-static and `--range` from full. Nothing depends
# on this target, and from 2026-07-23, when lefthook was deleted, to 2026-08-12
# nothing depended on the scan at all while four documents said it ran —
# scripts/gate-fails-closed-test.sh now asserts both call sites are in the
# expanded step list.
# ---------------------------------------------------------------------------
.PHONY: secret-scan
secret-scan:  ## Scan the staged index for API keys / credentials / .env files (gates use --head-only / --range)
	scripts/secret-scan.sh

# ---------------------------------------------------------------------------
# Format write + fail-closed format check
#
# fmt: write gofumpt + gci formatting in-place (used by pre-commit hook and
#      manually to fix a dirty tree).
# fmt-check: fail with a non-zero exit code if any file is unformatted (used
#            by `make fast`, `make full`, and CI). gofumpt -l and gci diff both exit 0
#            on format drift, so we wrap them with explicit output checks here.
# ---------------------------------------------------------------------------
# Order: gci first (import ordering), then gofumpt (blank-line rules). This
# ensures a single pass converges: gci may shift import blocks in ways that
# gofumpt wants to touch, but gofumpt never alters import order so gci stays
# satisfied after gofumpt runs.
.PHONY: fmt
fmt:  ## Auto-format all Go files with gofumpt + gci (writes in-place)
	scripts/go-format.sh write

.PHONY: fmt-check
fmt-check:  ## Fail-closed: exit 1 if gofumpt or gci would change any file (run 'make fmt' to fix)
	scripts/go-format.sh check

# ---------------------------------------------------------------------------
# THE GATE — two targets, and only two.
#
#   make fast   the inner loop. Run it while you work.
#   make full   the merge decision. Run it before you ask anyone to accept work.
#
# Every recipe line below is fail-closed. `make` stops at the first non-zero
# exit, and no line here carries a `-` prefix, a `|| true`, a `|| exit 0`, or a
# trailing `if` that can swallow a status. scripts/gate-fails-closed-test.sh
# holds that property: it re-derives the step list from `make -n` and it also
# drives a real `make full` with a stub `go` on PATH.
#
# WHY the delta-scoped gate is gone. It asked git which files changed since
# main and then tested only those packages. Two things were wrong with the
# answer. main is hundreds of commits behind this branch, so the file list was
# noise. And even from a fresh base the question cannot find a break in a
# package the change did not touch, which is the usual case here because
# internal/daemon imports eight other packages. Measured 2026-08-03 on this
# box: whole-repo `go test -short -count=1 ./...` costs about 13 seconds more
# than internal/daemon alone. The scoping bought 13 seconds and paid for it
# with the ability to see. scripts/scenario-gate.sh, which implemented it, is
# deleted: it had five ways to approve work that never passed (compile failure,
# timeout, signal kill, unrecognized exit code, and a retry that allowed when
# the second run passed) against one way to block.
#
# fast and full share every static step. They differ in exactly two ways: full
# tests EVERY package instead of the major set, and full adds the whole-tree
# lint allow list, the tagged scenario tier and the module hygiene checks. Keep
# it that way. A step that belongs to only one of them is how a
# third tier grows back.
# ---------------------------------------------------------------------------

# FAST_PKGS — the packages `make fast` runs unit tests for.
#
# internal/core, internal/daemon, internal/queue and internal/queuewiring are
# the named core set. The rest are on the same queue-and-bead path and a silent
# break in any of them is invisible in the four above:
#
#   internal/brcli      the bead-ledger adapter. Every bead state transition
#                       goes through it, so a break here loses work while the
#                       queue still reports progress.
#   internal/eventbus   the queue and the run loop are observed only through
#                       emitted events. A break here makes every other gate,
#                       watcher and status surface blind at once.
#   internal/runloop    owns the run state machine that carries a dispatched
#                       bead from launch to merge, and holds the merge-path
#                       scenario gate.
#   internal/workflow   parses and walks the workflow graph that decides what
#                       a bead does next. workflow.dot is data this package
#                       reads, so a graph edit is only as safe as this package.
#   cmd/harmonik        the only way an operator or an agent reaches the queue.
#                       It is also one of the two packages red today, so
#                       leaving it out would hide a known break from the inner
#                       loop.
#
# This set is a judgment, not a boundary the compiler enforces. `make full`
# tests every package and is the only thing a merge may rest on.
FAST_PKGS := \
	./internal/core \
	./internal/daemon \
	./internal/queue \
	./internal/queuewiring \
	./internal/brcli \
	./internal/eventbus \
	./internal/runloop \
	./internal/workflow \
	./cmd/harmonik

# ---------------------------------------------------------------------------
# CORE_PKGS — the core set, and the only thing that has to be green.
#
# Operator, 2026-08-06: "This whole tool set revolves around the ability to run
# beads through the queue. That's the core. There's a dozen other tools in here
# — those are not core." Tests outside this set do not have to run, and may be
# ignored or disabled.
#
# This is NOT a new judgment. CHARTER.md §3 decided the set on 2026-07-28 and
# calls it DECIDED, not proposed. It is written there as a pipeline:
#
#   config → event bus → queue → bead-ledger adapter → worktrees →
#   harness registry + one substrate → work loop → merge
#
# The list below is that pipeline resolved to packages, one group per stage, in
# the charter's own order. Nothing is added: §3 says anything absent is deferred
# by default rather than by argument.
#
# WHAT IS DELIBERATELY OUT, and it is most of the tree. §3 names comms, crew,
# captain, keeper, dashboard, live-state, subscribe and the sentinel — AND the
# socket listener, by operator decision the same day. So internal/keeper,
# internal/crew, internal/crewrun, internal/dashboard, internal/sentinel,
# internal/presence, internal/digest, internal/watch, internal/schedule and
# internal/supervise are all out, along with the second and third substrates
# (internal/harness/codex, internal/harness/pi and the codex* packages) — §3
# says "one substrate", and claude is it.
#
# WHY THIS TARGET EXISTS. The daemon suite goes red under load with a different
# test each time, and that has been investigated four or five times without
# resolution. Scoping is the answer that was actually available: a flake in a
# package the queue does not depend on stops being a blocker by definition
# rather than by another investigation.
#
# THIS DOES NOT REPLACE `make full`. `make full` is still the merge decision and
# still tests every package. `make core` answers a different and narrower
# question — is the thing this tool exists to do working — and that is the
# question an assessor sign-off rests on.
CORE_PKGS := \
	./internal/projectconfig \
	./internal/branching \
	./internal/daemon/bootconfig \
	./internal/eventbus \
	./internal/queue \
	./internal/queue/cli \
	./internal/queue/readiness \
	./internal/queuewiring \
	./internal/brcli \
	./internal/workspace \
	./internal/harness/shared \
	./internal/harness/claude \
	./internal/lifecycle \
	./internal/lifecycle/tmux \
	./internal/substrate \
	./internal/daemon \
	./internal/runloop \
	./internal/runexec \
	./internal/runlease \
	./internal/runlaunch \
	./internal/workflow \
	./internal/workflow/dot \
	./internal/runmerge \
	./internal/mergeq \
	./internal/core \
	./internal/handler \
	./internal/handlercontract \
	./internal/gitprobe \
	./cmd/harmonik

# Per-step wall-clock cap. A hung step must FAIL, never hang and never pass.
# Two caps, because they catch different hangs:
#   GATE_GO_TIMEOUT   goes to `go test -timeout`. A test that blocks forever
#                     panics the test binary and `go test` exits non-zero.
#   GATE_STEP_SECS    wraps the whole command in `timeout`, which catches a
#                     hang in the toolchain itself or in a child process the
#                     test leaked. `timeout` exits 124, which is non-zero, so
#                     make stops. Absent `timeout`/`gtimeout` the wrapper is
#                     empty and only the inner cap applies — a hang then hangs,
#                     which is loud, and it still never reports success.
GATE_GO_TIMEOUT ?= 25m
GATE_STEP_SECS  ?= 1800
GATE_CAP := $(if $(TIMEOUT_BIN),$(TIMEOUT_BIN) --kill-after=30s $(GATE_STEP_SECS)s,)

# When neither `timeout` nor `gtimeout` is installed, GATE_CAP expands to
# nothing and the outer cap silently disappears. Silently is the problem: a
# missing guard that says nothing is indistinguishable from a guard that is
# working. gate-static says so out loud once per run.

# script-tests — the self-tests for the shell the gate itself depends on.
# Most shell scripts in this repo decide something and about a quarter of them
# have a test. These are those tests as they apply to the gate path. They guard
# the parts that fail silently. (The count used to be written here as a number
# and it was wrong by nine before anyone noticed.)
#
# Most cost a few seconds. reachability-gate-test.sh costs about 13 seconds of
# wall clock on a warm cache and about 46 of CPU across cores, because the only
# honest way to prove that gate can fail is to write an unreachable function into
# a linked package and run the real whole-program analysis against it. It calls
# the gate ten times; six of those reach the analysis and four are fail-closed
# setup cases that stop before it. That is the price of the claim, and the claim
# is the one every other gate here got wrong at least once. Move it to `make
# full` if the inner loop starts to hurt, but do not weaken it in place.
#
# About 4 of those 13 seconds bought hermeticity, and they are not optional. The
# earlier version wrote its canary into the live internal/lifecycle, so a second
# run started while the first was mid-analysis deleted the first one's canary and
# both runs went red with messages naming deadcode's roots and the build cache —
# neither of which was at fault. It now mirrors the working tree into a throwaway
# copy per run and measures its own baseline there. Refs hk-ky66d.
#
# The throwaway tree also costs about 46 MB of Go build cache per run that no
# later run can reuse: the scratch path is new each time and nothing passes
# -trimpath, so every first-party package compiles at an address seen once. Go's
# own 5-day trim bounds it, and docs/disk-reclaim.md is the cure if it does not.
# A scratch path that is stable per checkout would recover the disk and most of
# the 4 seconds, but it needs a lock or a per-run suffix, and shared mutable
# state between two runs is the defect this change removed. Refs hk-11fdr.
#
# It is wrapped because it compiles Go, and a lane sharing one GOCACHE with
# another lane is the collision with-lane-gocache.sh exists for. Two other
# script-tests here compile Go unwrapped and predate that wrapper.
.PHONY: script-tests
script-tests:  ## Self-tests for the shell the gate depends on
	scripts/go-format-test.sh
	scripts/validate-commit-msg-test.sh
	scripts/agent-reviewer-run-test.sh
	scripts/agent-reviewer-prompt-parity-test.sh
	scripts/with-lane-gocache-test.sh
	scripts/go-test-must-match-test.sh
	scripts/loadgen-test.sh
	scripts/gate-fails-closed-test.sh
	scripts/scratch-daemon-rev-pin-test.sh
	scripts/scratch-daemon-toolchain-test.sh
	scripts/scratch-daemon-provenance-test.sh
	scripts/lint-allow-test.sh
	scripts/lint-allow-ratchet-test.sh
	scripts/scenario-pkgs-test.sh
	scripts/lint-changed-test.sh
	scripts/changed-func-coverage-test.sh
	scripts/queue-daemon-count-test.sh
	scripts/required-check-name-gate-test.sh
	scripts/commit-msg-gate-test.sh
	scripts/secret-scan-test.sh
	scripts/pipefail-grepq-gate-test.sh
	scripts/with-lane-gocache.sh scripts/reachability-gate-test.sh

# freeze-gates — the per-subsystem "do not move this back" greps. Cheap
# (sub-second each) and they only ever answer a structural question, so they
# belong in the inner loop.
.PHONY: freeze-gates
freeze-gates:  ## Subsystem freeze / ratchet greps (structural, sub-second each)
	scripts/transport-freeze-gate.sh
	scripts/queuewiring-freeze-gate.sh
	scripts/crewrun-freeze-gate.sh
	scripts/harnesscodex-freeze-gate.sh
	scripts/harnessclaude-freeze-gate.sh
	scripts/harnesspi-freeze-gate.sh
	scripts/runmerge-freeze-gate.sh
	scripts/projectconfig-freeze-gate.sh
	scripts/runlaunch-freeze-gate.sh
	scripts/runloop-freeze-gate.sh
	scripts/readywait-freeze-gate.sh
	scripts/workersbootwire-freeze-gate.sh
	scripts/runloop-emitter-gate.sh
	scripts/workloop-scheduler-freeze-gate.sh
	scripts/queue-status-writer-ratchet.sh
	scripts/lint-allow-ratchet.sh
	scripts/required-check-name-gate.sh
	scripts/pipefail-grepq-gate.sh

# gate-static — everything fast and full share that runs no test.
#
# Ordered cheapest-first so the inner loop reports the cheap break first.
# Every Go step runs under a GOCACHE private to THIS checkout. The lanes used
# to share one, and a concurrent process invalidating cache facts mid-run gives
# "could not import ... no such file or directory". The cache is keyed on the
# checkout root and PERSISTS, so a lane stays as warm as a shared cache would.
# Do NOT swap in with-isolated-gocache.sh: it deletes the cache on exit, so
# every line here would build cold.
.PHONY: gate-static
gate-static:  ## Shared static half of fast and full: script self-tests, format, build, vet, freeze greps, changed-line lint
	$(MAKE) script-tests
	$(MAKE) gate-static-product

# gate-static-product — the static half MINUS the script self-tests.
#
# WHY THIS SPLIT EXISTS. `script-tests` tests the GATE TOOLING — the shell
# scripts that implement the gates — not the product. It costs minutes and it
# runs before a single line of product code is checked. That is right for `fast`
# and `full`, which are developer and CI targets. It is wrong for the per-bead
# commit gate, whose only question is "can this run beads through the queue?".
# A bead's gate must not spend its first several minutes proving that
# lint-allow-test.sh still works.
#
# `make core` therefore calls THIS target, and `fast` / `full` keep the script
# self-tests through `gate-static` above.
#
# WHAT THE SPLIT COSTS, stated plainly, because an earlier version of this
# comment claimed it cost nothing. `make core` is the only gate the per-bead
# path runs on its own, and it now runs the gate scripts WITHOUT running the
# tests that prove those scripts fail closed. secret-scan.sh, commit-msg-gate.sh
# and lint-changed.sh all still execute here; what no longer executes on this
# path is the proof that they still refuse what they exist to refuse. That proof
# now lives only in `fast`, `full` and CI.
#
# Read that as a real reduction, not a technicality:
# scripts/gate-fails-closed-test.sh exists because secret-scan.sh had no caller
# for twenty days AND was admitting a key when a test was finally written for it.
# That test just moved off the continuously-run path onto the hand-run one.
#
# The trade is still worth making — a bead's gate must not spend its first
# minutes proving that lint-allow-test.sh works — but it is a trade, and the
# next person to read this should not have to rediscover which half was given
# up. Refs D3=v3 (see internal/daemon/standard-bead.dot).
.PHONY: gate-static-product
gate-static-product:  ## Static half without the script self-tests (what `make core` runs)
	@if [ -z "$(TIMEOUT_BIN)" ]; then \
		echo "NOTE: no timeout/gtimeout on PATH, so the per-step wall-clock cap is inert."; \
		echo "      go test -timeout=$(GATE_GO_TIMEOUT) still bounds a hung TEST, but a hang in"; \
		echo "      the toolchain itself will hang instead of failing. brew install coreutils."; \
	fi
	$(MAKE) fmt-check
	# The commit just made must carry a well-formed message and honest review
	# trailers. About a tenth of a second. This is the only place a bad message
	# CAN fail a build, and the only moment failing is fair: the commit is
	# yours, it is the tip, and amending it costs nothing.
	#
	# READ "CAN" AS THE WHOLE OF THE CLAIM. --head-only demotes itself to advice
	# and exits 0 whenever the history does not descend from the message
	# baseline named in that script, and that baseline is not an ancestor of
	# main. So on main a bad message fails NOWHERE: this call goes advisory and
	# the ledger call in `full` never fails by design. An earlier version of
	# this comment said "the ONLY place a bad message fails a build" flatly,
	# which reads as a guarantee that only holds on a branch that descends from
	# the baseline. Bead hk-commit-msg-gate-advisory-on-main-ap068 is the record.
	scripts/commit-msg-gate.sh --head-only
	# The same moment, for credentials. --head-only, NOT the default index scope.
	# The gates run after the commit is made, when the ordinary flow leaves
	# nothing staged, so the index scope here would read whatever a developer
	# happened to leave behind rather than the change under test. Not a
	# GUARANTEED no-op — stage a key and the index scope does block it — but it
	# answers a question nobody asked, and it is silent when it answers nothing.
	scripts/secret-scan.sh --head-only
	scripts/with-lane-gocache.sh go build ./...
	scripts/with-lane-gocache.sh go vet ./...
	scripts/with-lane-gocache.sh $(MAKE) vet-tagged
	$(MAKE) freeze-gates
	scripts/with-lane-gocache.sh scripts/reachability-gate.sh
	scripts/lint-changed.sh $(TOOLS_DIR)/golangci-lint run --allow-parallel-runners --new-from-rev=HEAD~1

# gate-test-compile — compiles every _test.go file in the repo and runs none of
# them. `go build ./...` does NOT compile test files, so a test that references
# an undefined symbol used to reach reviewers as a green commit. This runs no
# test, so a pre-existing red elsewhere cannot make it fail. Only a build break
# can, and a test that does not build is a build break.
.PHONY: gate-test-compile
gate-test-compile:  ## Compile every _test.go file, run none
	$(GATE_CAP) scripts/with-lane-gocache.sh go test -run='^$$' -count=1 ./...

# ---------------------------------------------------------------------------
# THE TEST STEP, and why it is spelled the long way in both targets.
#
# `go test` runs under -json and its stream goes to a file, which tools/testreport
# then renders. Three things forced that shape.
#
#   THE REPORT IS WORTH MOST WHEN THE RUN IS RED. A plain recipe would stop at
#   the first failing step and never render anything, which is the run a reader
#   most needs read to them. So the status is captured, the report is rendered,
#   and only then does the step exit.
#
#   CAPTURED, NOT DISCARDED. `go test ... | testreport` looks equivalent and is
#   not: a pipeline in /bin/sh exits with the status of the LAST command, so a
#   killed or crashed `go test` would be reported by whatever the renderer made
#   of a truncated stream. That is a fail-open, and it is the exact shape the
#   gate this replaced was deleted for. Both statuses are kept and the test
#   status wins, so a run that died still fails even if the report parsed fine.
#
#   A FILE, NOT A PIPE, because testreport buffers the whole stream before it
#   prints anyway — it cannot know what to suppress until a test has passed.
#   Neither spelling streams, and a file cannot lose the status.
#
# Kept inline in both targets rather than hidden in a script on purpose:
# scripts/gate-fails-closed-test.sh reads the expanded step list with `make -n`,
# and `make -n` does not descend into a shell script. A step behind a script is
# a step that structural check cannot see, and the test step is the one place
# that matters most.
#
# ONE DEFINITION, THREE USERS. `fast`, `full` and `gate-test-report-probe` all
# expand the canned recipe below. Writing it out three times would let the
# probe drift away from the thing it claims to test, and a probe that no longer
# matches the real step proves nothing about the real step.
#
# $(1) is a label for the messages. $(2) is the package list. $(3) is the
# short-mode flag, and it is a PARAMETER rather than a constant because the
# three callers do not want the same answer.
#
# THE FLAG USED TO BE HARD-CODED `-short`, IN ALL THREE. That made `make core`
# green without running the tests that answer the question `core` exists to ask.
# `-short` skips 45 tests in the core set — 35 in internal/daemon and 10 in
# internal/runloop — and the daemon 35 include TestScenario_HappyPath_N1 and
# TestSmokeLoop, the two end-to-end tests that put a bead in one end of the queue
# and assert it comes out closed at the other. A gate that asks "can this run
# beads through the queue?" and skips those reads as finished when it is not.
# Refs hk-od9d4.
#
# THE RENDERER IS BUILT, NOT `go run`. Two reasons, both learned here. `go run`
# would compile the tool against the default shared GOCACHE while every
# neighbouring step uses the per-checkout one, which is the cross-lane cache
# corruption scripts/with-lane-gocache.sh exists to prevent. And a renderer that
# fails to COMPILE must be told apart from a renderer that ran and found
# failures; building it separately means a broken tool exits 2 and can never be
# mistaken for a verdict.
# ---------------------------------------------------------------------------
define RUN_TESTS_AND_REPORT
@RAW=$$(mktemp); \
BINDIR=$$(mktemp -d); \
trap 'rm -f "$$RAW"; rm -rf "$$BINDIR"' EXIT; \
scripts/with-lane-gocache.sh go build -o "$$BINDIR/testreport" ./tools/testreport \
	|| { echo "$(1): could not build tools/testreport, so no run can be judged"; exit 2; }; \
TEST_STATUS=0; \
TMPDIR=/tmp $(GATE_CAP) scripts/with-lane-gocache.sh \
	go test -json $(3) -count=1 -timeout=$(GATE_GO_TIMEOUT) $(2) \
	> "$$RAW" || TEST_STATUS=$$?; \
REPORT_STATUS=0; \
"$$BINDIR/testreport" < "$$RAW" || REPORT_STATUS=$$?; \
if [ "$$TEST_STATUS" -ne 0 ]; then \
	echo "$(1): go test exited $$TEST_STATUS, so this run FAILED regardless of what the report shows"; \
	exit "$$TEST_STATUS"; \
fi; \
exit "$$REPORT_STATUS"
endef

# ---------------------------------------------------------------------------
# make fast — the inner loop.
#
# WHY THE WHOLE-TREE LINT JUDGE ENDS THIS TARGET (hk-dp69a). The changed-line
# step inside gate-static matches a finding's LINE against the lines the commit
# changed. Whole-function linters — gocognit, cyclop, funlen — report at a
# function's DECLARATION, which is usually outside the hunk that changed its
# body. Those findings are invisible AT the commit that caused them, and no
# choice of base revision fixes that. The whole-tree judge is the only step
# that can see them, and it used to run in `make full` alone, so a finding sat
# in the tree until the next person ran the merge decision. That happened five
# times, and twice the finding was a real defect rather than a style point.
#
# The two lint steps are complements and neither can go. The changed-line step
# is the only one that can see an EXTRA finding in a file-and-linter pair the
# allow list already carries, because the list holds no count.
#
# LAST, and NOT inside the shared static half. The assessor's gate is `make
# core`, which reaches that half through gate-static-product, and a lint red
# there would block every test before it ran.
#
# THE COST OF RUNNING IT LAST, and why it is still the right place. make stops
# at the first failing step, so a red TEST step hides this one. The finding is
# delayed, not lost: the work has to reach a green test run before it lands, and
# this step runs there. Moving it ahead of the tests would remove that delay and
# charge for it — every whole-tree finding, including one another lane wrote,
# would then block all test feedback. The delayed case ends inside one session.
# The case this target was changed to fix lasted a whole cycle.
#
# THE COST, measured on 2026-08-11 on a box with no other gate running: the
# three steps this target used to have took 333 s, and `make lint-allow` right
# after them took 5 s, 3 s and 4 s on three runs. The whole target then measured
# 338 s. So the step adds about 1 percent.
#
# It is that cheap because --new-from-rev does not scope the ANALYSIS. The
# changed-line step already analysed every package and only filtered what it
# printed, so the whole-tree run reads the linter's cache and re-prints. Do not
# quote these numbers without re-measuring: they came off one machine on one day,
# and a run taken while another lane holds the box measures that lane.
# ---------------------------------------------------------------------------
.PHONY: fast
fast:  ## THE inner loop: format, build, vet, compile every test, unit-test the major packages, lint changed lines, judge the whole tree
	$(MAKE) gate-static
	$(MAKE) gate-test-compile
	$(call RUN_TESTS_AND_REPORT,make fast,$(FAST_PKGS),-short)
	$(MAKE) lint-allow

# ---------------------------------------------------------------------------
# make core — is the thing this tool exists to do working?
#
# Runs the core set defined at CORE_PKGS above (CHARTER.md §3), and nothing
# else. Use it to answer "can this run beads through the queue" without a
# verdict from a dozen packages the queue does not depend on.
#
# It runs the same test step as `fast` and `full`, so it reports through
# tools/testreport and its NOT RUN section names every skipped test — read that
# section, because a disabled test is still an unproven claim.
#
# NOT a substitute for `make full`, which stays the merge decision and stays
# whole-tree. A green here and a red there means the core works and something
# outside it does not, which is a real and useful answer, not a contradiction.
#
# THIS TARGET RUNS WITHOUT `-short`, AND THAT IS THE POINT. `-short` skips 45
# tests in this set, and the skipped set is not incidental: it is the real-daemon
# end-to-end tier, including TestScenario_HappyPath_N1 (real daemon.Start, real
# twin subprocess, asserts the full run_started → agent_ready → run_completed
# event subsequence and the bead reaching closed) and TestSmokeLoop (one ready
# bead through a real br ledger, a real git worktree, a real merge, to closed).
# Those two ARE the headline claim. Running the gate with them off answered
# nothing and read as finished.
#
# THE COST. The test step roughly doubles: measured once on one box, `-short`
# took 144s and skipped 45 tests, and no `-short` took 246s and skipped none for
# shortness. Whole-target wall clock was about 400s, most of the rest being
# gate-static. Treat those as an order of magnitude, not a promise — they were
# taken on one machine on one day, and they will drift. Re-measure before you
# quote them. Refs hk-od9d4.
#
# WHY `twins` IS A PREREQUISITE. Seven of the newly-enabled tests look for
# ./twin-fail, ./twin-hang and ./harmonik-twin-claude at the checkout root and
# `t.Skip` when they are absent. Without this prerequisite they would trade one
# silent skip for another and the gate would still not run them. `twins` builds
# all five twin binaries into this checkout, so they run. If one still skips,
# tools/testreport names it in the NOT RUN section.
# ---------------------------------------------------------------------------
.PHONY: core
core: twins  ## The core set only (CHARTER §3): can this run beads through the queue?
	$(MAKE) gate-static-product
	$(call RUN_TESTS_AND_REPORT,make core,$(CORE_PKGS),)

# gate-test-report-probe — the smallest real use of the test step above.
#
# It exists so scripts/gate-fails-closed-test.sh can drive the capture-render-
# exit block behaviourally. The behavioural case on `full` cannot reach it: it
# dies at `go build` inside gate-static, hundreds of steps earlier. Without this
# probe the one invariant that matters here — the go-test status outranks the
# report status — is held up by a comment and nothing else, and deleting the
# precedence check leaves every test in the repo green.
#
# internal/sentinel is the package because it is small and fast. Under the
# self-test the `go` on PATH is a stub, so nothing real is compiled or run.
.PHONY: gate-test-report-probe
gate-test-report-probe:  ## Smallest real use of the test step (drives scripts/gate-fails-closed-test.sh)
	$(call RUN_TESTS_AND_REPORT,make gate-test-report-probe,./internal/sentinel,-short)

# ---------------------------------------------------------------------------
# make full — the merge decision.
#
# No scoping. No retry. No fail-open. `go test -short -count=1 ./...` is a
# strict superset of FAST_PKGS, which is why full does not call fast: calling
# it would test internal/daemon twice and cost about five extra minutes for no
# extra answer. Everything else fast runs, full runs, in the same order.
# ---------------------------------------------------------------------------
.PHONY: full
full:  ## THE merge decision: everything in fast over EVERY package, plus the lint allow list, scenario tier, module hygiene
	$(MAKE) gate-static
	# The ledger: every commit from the grandfather baseline forward, named and
	# counted. It REPORTS and never fails. Everything in that range is already
	# written and most of it arrived by merge, and amending a commit another
	# lane can see is what this project refuses outright — so there is no legal
	# repair for a bad message in there. A gate that refuses what cannot be
	# fixed gets deleted, not obeyed. gate-static above is where enforcement is
	# MEANT to live — and on main it does not, because --head-only goes advisory
	# off the message baseline and this ledger call never fails by design, so on
	# main neither mode blocks anything. An earlier version of this line said
	# "gate-static above is the enforcement" without that qualifier. Bead
	# hk-commit-msg-gate-advisory-on-main-ap068 is the record.
	scripts/commit-msg-gate.sh
	# The credential scan, and this one FAILS. NOT the same span as the message
	# ledger above: the two scripts name different baselines. The credential
	# baseline is the older of the two, so the message ledger's span nests
	# INSIDE this one (measured 2026-08-12) and this one is by far the wider.
	# Exact commit counts are not quoted here because they move with every
	# commit. This one fails where the ledger only reports, because a bad
	# message on a merged commit has no legal repair and a leaked key has one —
	# rotate it, and take the value out of the tree before the merge lands — so
	# refusing here is a demand that can be met. About 5 to 6 seconds over the
	# two hundred thousand-odd added lines this branch carries.
	scripts/secret-scan.sh --range
	$(MAKE) gate-test-compile
	$(call RUN_TESTS_AND_REPORT,make full,./...,-short)
	$(MAKE) lint-allow
	# test-subprocess is the ONLY test that boots the real binary as a process:
	# it waits for the socket, submits through the real CLI, and asserts a
	# terminal event. Everything else calls daemon.Start in-process, so a
	# regression in the boot path a real operator takes had nothing standing in
	# front of it. It runs in about 11 seconds against a scenario tier that costs
	# 8 minutes, and the tag keeps it out of the default build.
	$(MAKE) test-subprocess
	$(MAKE) test-scenario
	$(MAKE) module-hygiene

# ---------------------------------------------------------------------------
# full-guarded — the SAME merge decision, run only when the box can answer.
#
# Not a third tier. There are still two targets: this one runs `make full` and
# adds nothing to it. What it adds is a refusal. `make full` has three ways to
# report a failure that is not in the code and all three are silent — another
# heavy run sharing the box, a process holding the sidecar lock on the Claude
# config, or free disk under the floor. The script refuses to start on any of
# them, samples disk and the lock while the suite runs, and prints FULL_RC so
# the reader has an exit code that did not come out of a pipeline.
#
# The script calls `make full` itself, so the guard must NOT move into `full`.
# That recurses with no bottom.
# ---------------------------------------------------------------------------
.PHONY: full-guarded
full-guarded:  ## `make full` behind a pre-flight that refuses a busy box, a held config lock or low disk
	scripts/run-full.sh

# ---------------------------------------------------------------------------
# lint-allow — THE HOOK for the whole-tree lint verdict.
#
# `make full` lints the WHOLE tree, not only the changed lines, and fails when a
# finding appears in a file that is not on the allow list at
# tools/lintreport/allow.txt. A bare `golangci-lint run` reports more than a
# thousand findings, so it exits non-zero on every commit and is useless as a
# verdict — which is why the old `make check` was documented as "never gate on
# this", and why the whole-tree linter watched nothing at all.
#
# WHY A LIST AND NOT A COUNT. This target used to compare a single number
# against a committed ceiling. A count is a weak verdict, because it falls just
# as readily when somebody silences a finding as when somebody fixes one, and it
# says nothing about WHERE the debt is. It also cannot tell "fixed two in the
# daemon, added two in the queue" from "no change at all".
#
# The allow list names each tolerated file-and-linter pair on its own line. A
# finding whose pair is listed is grandfathered. A finding whose pair is NOT
# listed fails the build. Clean a file, delete its line, and that file can never
# regress. The list is keyed on file-and-linter rather than on line number
# because line numbers rot within days and would churn the list on every
# unrelated edit.
#
# Every run prints what it is tolerating, broken down by package and by linter,
# including a run that passes. "No new lint findings" must never be readable as
# "this tree is clean". The tree carries 1,187 findings today and 615 of them
# are in internal/daemon.
#
# WHERE THIS RUNS. Both `make fast` and `make full`, and `fast` does not call
# `full`, so each pays for one whole-tree run. It was the merge decision alone
# until hk-dp69a measured the cost of that: a finding a whole-function linter
# reports at a declaration outside the changed hunk is invisible to the
# changed-line step in gate-static, so it reached the tree and waited for the
# next person to run the merge decision. Five times. The rule this step applies
# already caught every one of them — only the timing was wrong.
#
# The changed-line step stays. It is the only one that can see an EXTRA finding
# in a pair the list already carries, because the list holds no count.
#
# scripts/lint-allow-test.sh holds this target's assertions. It runs inside
# script-tests, so it runs in both fast and full.
# ---------------------------------------------------------------------------
.PHONY: lint-allow
lint-allow:  ## Whole-tree lint judged against the allow list in tools/lintreport/allow.txt
	@if [ ! -x scripts/lint-allow.sh ]; then \
		echo "make full: the whole-tree lint script is missing."; \
		echo "  Expected: scripts/lint-allow.sh, executable, exit 0 when no finding"; \
		echo "  falls outside tools/lintreport/allow.txt."; \
		echo "  This step FAILS while it is missing. A merge decision with a missing"; \
		echo "  step has not produced a verdict, and a gate that shrugs at a missing"; \
		echo "  step is the fail-open behaviour this gate was built to remove."; \
		exit 1; \
	fi
	scripts/lint-allow.sh

# ---------------------------------------------------------------------------
# module-hygiene — the cheap whole-module checks the old `check` target held.
# ---------------------------------------------------------------------------
.PHONY: module-hygiene
module-hygiene:  ## go.mod/go.sum tidy check, forbidden-import check, govulncheck
	@# go mod tidy diff check — fail if tidy would change go.mod or go.sum.
	@# The originals go to a temp directory and come back either way, so the
	@# check never leaves a tidied tree behind. No step here may end in a
	@# construct that discards a status. scripts/gate-fails-closed-test.sh
	@# refuses those in any gate step, because a swallowed status is how a gate
	@# starts approving work that did not pass.
	@saved=$$(mktemp -d) && \
	cp go.mod go.sum "$$saved/" && \
	go mod tidy; \
	drift=0; \
	diff -q go.mod "$$saved/go.mod" >/dev/null 2>&1 || drift=1; \
	diff -q go.sum "$$saved/go.sum" >/dev/null 2>&1 || drift=1; \
	cp "$$saved/go.mod" "$$saved/go.sum" .; \
	rm -rf "$$saved"; \
	if [ "$$drift" -ne 0 ]; then \
		echo "ERROR: go mod tidy would change go.mod or go.sum; run 'go mod tidy' and commit the result"; \
		exit 1; \
	fi
	go run ./tools/forbid-import ./...
	$(TOOLS_DIR)/govulncheck ./...

# ---------------------------------------------------------------------------
# Lanes that are NOT the gate.
#
# Each one answers a question `make full` deliberately does not ask. None of
# them may block a merge, and none of them is a third tier.
# ---------------------------------------------------------------------------

# test-race-nightly — was check-race-full. Renamed because the `check-` prefix
# said "gate" and this never was one: .github/workflows/nightly-race.yml runs
# it on a schedule and its result is surfaced through ops-monitor, never as a
# merge verdict. It is the only -race coverage left now that `make full` runs
# the suite without -race, so it earns its keep as a lane.
.PHONY: test-race-nightly
test-race-nightly:  ## Nightly, non-gating: go test -race -count=1 ./... (full-parallel, no -short)
	TMPDIR=/tmp go test -race -count=1 ./...

# test-integration — the `integration`-tagged tier. Dropped OUT of the merge
# decision: it needs tmux and a live environment, `make test-scenario` already
# covers the real-daemon path it duplicates, and no automated lane ever ran it.
# Kept runnable so the coverage is not lost, and named so it is findable.
.PHONY: test-integration
test-integration:  ## The integration-tagged tier (needs tmux + a live environment; not part of `make full`)
	go test -race -tags=integration -count=1 ./...

# coverage-gates — the two coverage ratchets. Dropped OUT of the merge decision:
# each re-runs the suite a second time under -covermode, which roughly doubles
# the cost of a verdict, and a ratchet measures a trend rather than answering
# "did this work pass". The old `make check` also invoked coverage-gate.sh
# behind an `if [ -x ... ]` that PASSED when the script was missing. Here a
# missing script is a failure like any other.
.PHONY: coverage-gates
coverage-gates:  ## Coverage ratchets, internal/** and cmd/** (trend measure; not part of `make full`)
	scripts/coverage-gate.sh
	scripts/with-isolated-gocache.sh scripts/cmd-coverage-gate.sh

# coverage-changed — the question the ratchets above cannot ask.
#
# Both gates measure a PACKAGE, and a package number hides the thing worth
# seeing. internal/keeper reads 79.2% while the function written to fix a
# data-loss bug reads 0.0%. One new untested function moves the package figure
# by a fraction of a point, so no threshold fires and nobody looks.
#
# This asks the narrow question instead: of the functions in THIS diff, which
# are at 0.0%? It is a REPORT and it always exits 0. Deliberately not a gate
# and deliberately not in `make fast` — see the header of the script for the
# -coverpkg trade-off it makes and the false alarms that remain.
#
#   make coverage-changed              # against HEAD~1, same as the lint step
#   make coverage-changed BASE=<ref>   # against a merge-base, for a whole lane
BASE ?= HEAD~1
.PHONY: coverage-changed
coverage-changed:  ## Report (never gate): functions this diff touched that no test exercises
	scripts/changed-func-coverage.sh $(BASE)

# ---------------------------------------------------------------------------
# Keeper acceptance corpus — keeper conformance set (hk-urxa3)
# Named conformance set for the keeper test-validation system.  Runs the
# registered corpus slots without a real tmux session.
# test-keeper-conformance-full additionally runs the L-twin integration tier
# (requires tmux on PATH).
#
# The non-integration tier runs all 15 keeper slots and the cmd-level upgrade
# test.
#
# Corpus map — REGISTERED (15 keeper slots + 1 cmd-level):
#   floor: band-min / force-act / hard-ceiling SID-independent
#          live-watcher flock vs corpse
#   #1 restart-now does not abort no_tmux_target (B4 fix, L-fake-tmux, 2 slots)
#   #5 hold dies on restart, hard-ceiling overrides hold, WARN fires under hold
#   #6 binary-upgrade refuse-to-start + config --example restores (cmd/harmonik)
#
# Corpus item #2 (session_id survives /clear, rebinds same lane) is L-twin only.
# It never ran here.  Use test-keeper-conformance-full.
# ---------------------------------------------------------------------------
.PHONY: test-keeper-conformance
test-keeper-conformance:  ## Keeper acceptance corpus: 15 keeper slots + upgrade test, zero real tmux. (hk-urxa3)
	@echo "test-keeper-conformance: 15 slots registered, 0 have NO test."
	scripts/go-test-must-match.sh go test -race -count=1 -run 'TestKeeperConformance' ./internal/keeper/ ./cmd/harmonik/

.PHONY: test-keeper-conformance-full
test-keeper-conformance-full: test-keeper-conformance  ## The above + the L-twin tier: 3 real-tmux slots, corpus #1/#2/#4 (requires tmux on PATH)
	scripts/go-test-must-match.sh go test -race -tags=integration -count=1 -run 'TestKeeperConformanceCorpus_Integration' ./internal/keeper/

# ---------------------------------------------------------------------------
# Release validation gate (hk-o4j13)
# Invoked by the release CI workflow (hk-jdesv adds the .github/workflows step).
# Runs each phase in order; any nonzero exit propagates immediately.
#   1. lint               — golangci-lint full run
#   2. go test -short     — unit suite, -race, skip heavy real-daemon E2E. The
#                           E2E tier runs in phase 3 below, and in `make core`.
#   3. scenario suite     — full -tags=scenario run with twins (see test-scenario)
#   4. --version smoke    — verify the built binary starts and prints a version
# ---------------------------------------------------------------------------
.PHONY: release-validate
release-validate: build-all  ## Optional local sanity check (NOT on the release critical path — dogfooding+captain-certify is the gate)
	# LINT IS A MERGE-TIME GATE, NOT A RELEASE-TIME GATE. CI Tier 1/2 run golangci-lint --new-from-rev
	# on every commit to main, so code reaching a release tag is already linted. We do NOT re-run lint here:
	#   (1) full `golangci-lint run` fails on ~5666 pre-existing legacy issues (the release bar in the spec
	#       assumed a clean baseline that never existed — pipeline was DOA), and
	#   (2) `--new-from-rev=origin/main` cannot resolve its base ref in the tag-triggered release runner
	#       (checkout is a detached tag, origin/main is not fetched) so it falls back to linting everything.
	# The release gate validates BUILD + VET + TESTS + SCENARIO + SMOKE of already-merged, already-linted code.
	$(MAKE) fmt-check
	go vet ./...
	go test -short -race -count=1 ./...
	go test -race -tags=scenario -timeout 10m ./test/scenario/... ./internal/daemon/...
	@echo "release-validate: harmonik --version smoke"
	@/tmp/harmonik --version

# ---------------------------------------------------------------------------
# Lint shorthand
# ---------------------------------------------------------------------------
LINT_FULL_TIMEOUT ?= 15m

.PHONY: lint lint-full-count
lint:  ## golangci-lint run (shorthand)
	$(TOOLS_DIR)/golangci-lint run

# lint-full-count publishes a whole-tree finding count for a reader who wants
# one number. It is NOT the verdict and nothing gates on it: `make full` judges
# each finding against tools/lintreport/allow.txt through `make lint-allow`,
# because a count falls just as readily when somebody silences a finding as when
# somebody fixes one. Kept as a hand-run measure, not as a gate.
#
# TWO CHANGES WERE NEEDED before it could report a trustworthy number, both
# measured 2026-08-03 on this tree.
#
#   THE CACHE. This used to run under with-isolated-gocache.sh, which hands the
#   command a `mktemp -d` GOCACHE and deletes it on exit. Every run therefore
#   type-checked every dependency from scratch. Cold it did not finish: 8m15s
#   wall before golangci-lint hit its own cap and exited 4, so the target
#   published NO COUNT AT ALL. Under with-lane-gocache.sh, which keys the cache
#   on the checkout root and keeps it, the same run takes 10.7s. That is the
#   same argument the gate-static comment above makes, and it applies here for
#   the same reason: this is a recipe that runs on every `make full`, not once
#   at the end of a tier.
#
#   THE CAP. .golangci.yml sets run.timeout to 5m, which is right for the
#   changed-line runs but is under a cold whole-tree run. Overridden here, and
#   only here, to a bound that a cold checkout fits inside. NOT disabled: a
#   linter that hangs must fail rather than hang.
lint-full-count:  ## Publish the full-tree lint finding count (a measure, not the verdict; see lint-allow)
	@REPORT=$$(mktemp); \
	trap 'rm -f "$$REPORT"' EXIT; \
	LINT_STATUS=0; \
	scripts/with-lane-gocache.sh $(TOOLS_DIR)/golangci-lint run --allow-parallel-runners --issues-exit-code=0 --max-issues-per-linter=0 --max-same-issues=0 \
		--timeout=$(LINT_FULL_TIMEOUT) \
		--output.text.path=/dev/null --output.json.path="$$REPORT" >/dev/null || LINT_STATUS=$$?; \
	if [ "$$LINT_STATUS" -ne 0 ]; then \
		echo "lint-full-count: golangci-lint failed (exit $$LINT_STATUS)" >&2; \
		exit "$$LINT_STATUS"; \
	fi; \
	jq -er 'if any(.Issues[]; .FromLinter == "typecheck") then \
		error("full lint count unavailable: typecheck failed; fix compilation first") \
		else "full lint findings: \(.Issues | length)" end' "$$REPORT"

# ---------------------------------------------------------------------------
# Agent review — LOCAL ONLY
# Invokes the agent-reviewer skill against the diff vs. the last commit,
# then cross-checks the stored verdict via check-verdict.sh (hk-q6axs.4).
# Only an APPROVE verdict allows the commit to proceed.
# If the skill binary/wrapper is not present, exits 0 with an explanatory
# message so that Makefile pipelines are not blocked during early bootstrap.
# ---------------------------------------------------------------------------
.PHONY: agent-review
agent-review:  ## Run agent-reviewer + verdict cross-check; APPROVE required to commit (hk-q6axs.4)
	@SKILL=".claude/skills/agent-reviewer/run"; \
	if [ -x "$$SKILL" ]; then \
		if [ -n "$(TIMEOUT_BIN)" ]; then \
			$(TIMEOUT_BIN) $(AGENT_REVIEW_TIMEOUT) "$$SKILL" --diff HEAD~1; \
		else \
			echo "agent-review: no timeout/gtimeout binary found; running unwrapped (no hard cap)."; \
			"$$SKILL" --diff HEAD~1; \
		fi; \
		EXIT=$$?; \
		if [ $$EXIT -eq 124 ]; then \
			echo "agent-review: timed out after $(AGENT_REVIEW_TIMEOUT)s; retry manually or add Trivial: true for trivial commits."; \
			exit 1; \
		fi; \
		if [ $$EXIT -ne 0 ]; then exit $$EXIT; fi; \
		scripts/check-verdict.sh --diff HEAD~1; \
	else \
		echo "agent-reviewer skill not yet installed (filed under hk-jhob.1)."; \
		echo "Install it to enable structured pre-commit review; skipping for now."; \
		exit 0; \
	fi

.PHONY: review-verdict
review-verdict:  ## Cross-check diff-keyed verdict: APPROVE → pass; absent/REQUEST_CHANGES/BLOCK → fail (hk-q6axs.4)
	@scripts/check-verdict.sh --diff HEAD~1

# ---------------------------------------------------------------------------
# Tool installation
# Pins dev tools into ./.tools/ to avoid polluting the global GOPATH.
# Fresh-clone setup: make bootstrap  (installs tools)
#
# NOTE: git hooks are RETIRED. lefthook (and its self-re-arming `install`) was
# removed — validation runs from the two gate targets, not from a
# pre-commit/pre-push/commit-msg hook. gate-static calls
# scripts/commit-msg-gate.sh --head-only and scripts/secret-scan.sh --head-only
# over the commit just made. `make full` adds scripts/commit-msg-gate.sh with no
# argument (the message ledger, which reports and never fails) and
# scripts/secret-scan.sh --range (which fails on a finding).
# scripts/validate-commit-msg.sh stays callable on a message file, and
# scripts/secret-scan.sh stays callable with no argument, which reads the index.
# ---------------------------------------------------------------------------
.PHONY: tools
tools:  ## Install pinned dev tools into ./.tools/ (gofumpt, gci, golangci-lint, govulncheck, deadcode)
	@mkdir -p $(TOOLS_DIR)
	$(GOBIN_TOOLS) go install mvdan.cc/gofumpt@v0.7.0
	$(GOBIN_TOOLS) go install github.com/daixiang0/gci@v0.13.5
	$(GOBIN_TOOLS) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.3.0
	$(GOBIN_TOOLS) go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
	$(GOBIN_TOOLS) go install golang.org/x/tools/cmd/deadcode@v0.48.0

# bootstrap: one-stop fresh-clone setup — installs pinned tools.
.PHONY: bootstrap
bootstrap: tools  ## Fresh-clone setup: install pinned dev tools


# ---------------------------------------------------------------------------
# Queue dogfood readiness gate (T9)
# ---------------------------------------------------------------------------
# One command that produces the evidence an assessor reads, and the verdict on
# it. Before this existed the gate had no way in from a terminal: nothing
# imported internal/queue/readiness, so the record could only be produced from
# Go, and the reader it was built for is a separate session that cannot.
#
# It measures the host here rather than in Go on purpose. The validator is pure
# and takes plain values, so what a caller measures is what lands in the record.
# Measuring in the Makefile keeps that seam, and keeps the numbers in front of
# the person running it.
#
# Required inputs. The target refuses without them; it does not guess.
#   SCRATCH      the throwaway clone the pass may touch
#   EVIDENCE     the directory to retain the record and the verdict in
#   BEADS        the canary items, ';'-separated, each 'bead=why re-running it is safe'
#   CONCURRENCY  how many items run at the same time
#
# The item count comes from BEADS because BEADS is the list, and there is no
# second source for it to disagree with. Concurrency is a separate decision, so
# it is a separate input. The evidence record still refuses a stated count that
# does not match the items named — that check guards the direct command, where
# the two can genuinely disagree.
#
# A reason may contain spaces. It must not contain a single quote or a
# semicolon; both are the separators this target splits on.
#
# Optional: HARNESS, QUEUE_KIND, and the three host limits. Naming a limit here
# is how the operator's number reaches the record instead of a source edit.
# The project whose ledger and event logs the capture reads. It defaults to the
# working directory, which is right when the target runs from the fleet
# checkout. Point it at the checkout that holds .harmonik when it does not — a
# git worktree has no .harmonik of its own.
QDR_PROJECT     ?= $(CURDIR)
QDR_HARNESS     ?= claude
QDR_QUEUE_KIND  ?= stream
QDR_MAX_LOAD    ?= 1.0
QDR_MIN_DISK_GB ?= 10
QDR_MAX_DAEMONS ?= 1
.PHONY: queue-dogfood-readiness
queue-dogfood-readiness: build-harmonik  ## Capture and judge the evidence that a queue dogfood run is safe to start (T9)
	@test -n "$(SCRATCH)" || { echo "queue-dogfood-readiness: SCRATCH is required — the throwaway clone the pass may touch"; exit 2; }
	@test -n "$(EVIDENCE)" || { echo "queue-dogfood-readiness: EVIDENCE is required — where to retain the record and the verdict"; exit 2; }
	@test -n "$(BEADS)" || { echo "queue-dogfood-readiness: BEADS is required — ';'-separated 'bead=why re-running it is safe'"; exit 2; }
	@test -n "$(CONCURRENCY)" || { echo "queue-dogfood-readiness: CONCURRENCY is required — how many items run at the same time"; exit 2; }
	@# The daemon count lives in scripts/queue-daemon-count.sh, and that file
	@# carries the reasoning. The short form: the count feeds
	@# `queue readiness validate --daemons-alive`, and a count that is wrongly zero
	@# can never reach the --max-daemons ceiling, so a wrong zero turns the ceiling
	@# off. This target used to hold one unconditional line that piped a hand-written
	@# pgrep pattern into `wc -l`, and that line could not refuse at all. Every step
	@# of the derivation now refuses with exit 2 instead of guessing, and the line
	@# below carries that refusal into this target.
	@#
	@# It is a script and not a recipe line because a recipe line cannot be tested.
	@# scripts/queue-daemon-count-test.sh drives every refusal the script has and
	@# both directions of the happy path, and it runs inside script-tests.
	@set -eu; \
	scratch='$(SCRATCH)'; evidence='$(EVIDENCE)'; beads='$(BEADS)'; \
	mkdir -p "$$evidence"; \
	item_count=0; \
	old_ifs="$$IFS"; IFS=';'; \
	set --; \
	for pair in $$beads; do \
		IFS="$$old_ifs"; \
		pair="$$(printf '%s' "$$pair" | sed -e 's/^ *//' -e 's/ *$$//')"; \
		if [ -n "$$pair" ]; then \
			case "$$pair" in \
				*=*) set -- "$$@" --select "$$pair"; item_count=$$((item_count + 1)) ;; \
				*) echo "queue-dogfood-readiness: BEADS entry '$$pair' is not 'bead=reason'"; exit 2 ;; \
			esac; \
		fi; \
		IFS=';'; \
	done; \
	IFS="$$old_ifs"; \
	test "$$item_count" -gt 0 || { echo "queue-dogfood-readiness: BEADS named no item"; exit 2; }; \
	echo "measuring the host"; \
	case "$$(uname -s)" in \
		Darwin) load="$$(sysctl -n vm.loadavg | tr -d '{}' | awk '{print $$1}')" ;; \
		*) load="$$(awk '{print $$1}' /proc/loadavg)" ;; \
	esac; \
	cpus="$$(getconf _NPROCESSORS_ONLN)"; \
	free_gb="$$(df -k "$$scratch" 2>/dev/null | awk 'NR==2 {printf "%.1f", $$4/1024/1024}')"; \
	test -n "$$free_gb" || { echo "queue-dogfood-readiness: cannot measure free disk on '$$scratch'"; exit 2; }; \
	daemons="$$(scripts/queue-daemon-count.sh /tmp/harmonik "$(QDR_PROJECT)")" || exit 2; \
	echo "  load=$$load cpus=$$cpus free_disk_gb=$$free_gb daemons_alive=$$daemons"; \
	echo "capturing the readiness record"; \
	/tmp/harmonik queue readiness capture \
		--project "$(QDR_PROJECT)" \
		"$$@" \
		--item-count "$$item_count" \
		--concurrency "$(CONCURRENCY)" \
		--local=true \
		--out "$$evidence/readiness.json"; \
	echo "judging it"; \
	/tmp/harmonik queue readiness validate \
		--snapshot "$$evidence/readiness.json" \
		--out "$$evidence/validation.json" \
		--harness "$(QDR_HARNESS)" \
		--repo-target "$$scratch" \
		--scratch-repo "$$scratch" \
		--queue-kind "$(QDR_QUEUE_KIND)" \
		--item-count "$$item_count" \
		--concurrency "$(CONCURRENCY)" \
		--load-average "$$load" \
		--cpu-count "$$cpus" \
		--free-disk-gb "$$free_gb" \
		--daemons-alive "$$daemons" \
		--max-load-per-cpu "$(QDR_MAX_LOAD)" \
		--min-free-disk-gb "$(QDR_MIN_DISK_GB)" \
		--max-daemons "$(QDR_MAX_DAEMONS)"

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------
.PHONY: help
help:  ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'
