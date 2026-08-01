# Harmonik Makefile
# Three-tier check gauntlet (Tier 1 / Tier 2 / Tier 3) + helpers.
# Local/CI parity: every CI gate invokes these same targets verbatim.
# See docs/foundation/project-level/quality-checks.md §Three-tier identical gauntlet.

.DEFAULT_GOAL := check

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
	go test -tags e2e_real_claude -timeout 300s -v -run TestE2ERealClaudeSingleMode ./internal/daemon/...

# test-e2e-real-claude-reviewloop: run the real-Claude review-loop E2E smoke test.
# Requires: claude, tmux, git, br, ntm on PATH; ANTHROPIC_API_KEY or
# CLAUDE_CODE_OAUTH_TOKEN set; harmonik buildable from source.
# Budget: 300s timeout (the two-agent cycle may take up to 240s).
.PHONY: test-e2e-real-claude-reviewloop
test-e2e-real-claude-reviewloop:  ## Run real-Claude review-loop E2E smoke (requires credentials + binaries on PATH)
	go test -tags e2e_real_claude -timeout 300s -v -run TestE2ERealClaudeReviewLoopMode ./internal/daemon/...

# test-scenario: run the scenario tier with -race and the scenario build tag.
# Prereq: build-all compiles cmd/harmonik and the twins that daemon scenarios
# locate without a rebuild.
# Budget: 10 minutes, matching the scenario sub-run in check-full (Tier 3).
# Covers all packages that carry //go:build scenario files:
#   ./test/scenario/...  — top-level scenario package (test/scenario/harness_test.go)
#   ./internal/daemon/...— daemon-resident scenario tests (scenario_*.go files)
# See docs/methodology/TESTING.md §Scenario fixture determinism recipe for the
# worktree-factory / merge-mutex / phase-aware-twin / Skip* recipe used here.
.PHONY: test-scenario
test-scenario: build-all  ## Run scenario tier (-race, -tags=scenario, 10m budget; prereq: build-all)
	@scenario_log=$$(mktemp); \
	status=0; \
	go test -v -race -tags=scenario -timeout 10m ./test/scenario/... ./internal/daemon/... >"$$scenario_log" 2>&1 || status=$$?; \
	cat "$$scenario_log"; \
	printf 'scenario skips: '; \
	grep -c '^--- SKIP:' "$$scenario_log" || true; \
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
	go test -tags=subprocess -timeout 5m -count=1 ./cmd/harmonik -run TestSubprocessDaemonBootSmoke

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
#   1. wipes + inits LT_SCRATCH by cloning the LOCAL checkout ($(CURDIR)) — NOT origin/main
#      (scratch-daemon.sh init defaults to the origin URL and SKIPS a re-clone when .git
#      exists, so a stale origin/main clone would otherwise persist and be the daemon-under-
#      test). Cloning the local repo checks out this branch's HEAD = the pinned code.
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
	@# Guarantee a FRESH clone of the PINNED code: wipe, then init from the LOCAL checkout.
	rm -rf "$(LT_SCRATCH)"
	bash scripts/scratch-daemon.sh init "$(LT_SCRATCH)" "$(CURDIR)"
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
	docker compose -f $(COMPOSE_E2E) exec -T daemon \
	  /usr/local/bin/remote-substrate.test \
	  -test.run '^TestScenario_RemoteSubstrate_Localhost_E2E$$' -test.v; \
	rc=$$?; \
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
# internal/daemon. Wired into check-fast and check-short.
.PHONY: transport-freeze-gate
transport-freeze-gate:  ## P2 E4: forbid new reverse-tunnel files or moved symbols in internal/daemon
	scripts/transport-freeze-gate.sh

# queuewiring-freeze-gate: the P2 E3 extraction ratchet — the queue-ownership
# concern (QueueStore, the brcli->queue.BeadLedger bridge, the operator
# pause/resume consumer) left internal/daemon for internal/queuewiring, and
# depguard can only fence the import edge, not the creation of a new file. This
# grep gate fails if a queue-ownership-shaped file or one of the moved symbols
# reappears in internal/daemon. Wired into check-fast and check-short.
.PHONY: queuewiring-freeze-gate
queuewiring-freeze-gate:  ## P2 E3: forbid new queue-ownership files or moved symbols in internal/daemon
	scripts/queuewiring-freeze-gate.sh

# crewrun-freeze-gate: the P2 E2 extraction ratchet — the crew launch contract
# (the crew-start/crew-stop RPC payloads, the persistent-session launch-spec
# builder, the crew-scoped harness resolver, the mission front-matter readers,
# the idle-crew reaper) left internal/daemon for internal/crewrun, and depguard
# can only fence the import edge, not the creation of a new file. This grep gate
# fails if a crew-launch-shaped file or one of the moved symbols reappears in
# internal/daemon. Wired into check-fast and check-short.
.PHONY: crewrun-freeze-gate
crewrun-freeze-gate:  ## P2 E2: forbid new crew-launch files or moved symbols in internal/daemon
	scripts/crewrun-freeze-gate.sh

# harnesscodex-freeze-gate: the P2 E1a extraction ratchet — the codex harness
# implementation (the Harness impl, the launch-spec builder, the JSONL parser,
# the stale-WAL and billing guards, the Refs-trailer fallback, the no-work
# detector) left internal/daemon for internal/harness/codex, and depguard can
# only fence the import edge, not the creation of a new file. This grep gate
# fails if a codex-harness-shaped file or one of the moved symbols reappears in
# internal/daemon. Wired into check-fast and check-short.
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
# script header. Wired into check-fast and check-short.
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
# because `go test -run` exits 0 on an empty match. Wired into check-fast and
# check-short.
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
# the script header. Wired into check-fast and check-short.
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
# '*events*.go' file scan would fire on correct code. Wired into check-fast and
# check-short.
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
# their moved run-path filenames/symbols. Wired into check-fast and check-short.
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
# four to check (2), so they are now IN scope, not out. Wired into check-fast and
# check-short.
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
# construction. Wired into check-fast and check-short.
.PHONY: workersbootwire-freeze-gate
workersbootwire-freeze-gate:  ## P2 E4c: forbid new worker-registry boot-wiring files or moved symbols in internal/daemon
	scripts/workersbootwire-freeze-gate.sh

# runloop-emitter-gate: the P2 E5 RT16 ratchet — the DOT run path reaches its
# event bus through EmitterPort (internal/daemon/runports.go), not through the
# workLoopDeps bus field. RT16 converted 108 direct field reads on the six mover
# files to 8 port reads so RT18's re-signature is an 8-line change, not a
# 108-line one. depguard cannot express "reach this dependency through its
# port", so this grep gate rations the field per file, asserts EmitterPort is
# still an alias, and pins each mover to the seam. Wired into check-fast and
# check-short.
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
# into check-fast and check-short.
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
	CODEX_LIVE=1 go test -timeout 180s -count=1 -run TestL3_ ./internal/codextest/...

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
	CODEX_LIVE=1 go test -timeout 120s -count=1 -v -run TestL3_ ./internal/codextest/...

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
	  go test -tags e2e_real_claude -timeout 300s -count=1 -v \
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
	go test -count=1 -run 'ClaudeParity' ./internal/twinparity/...

# test-pi-live: the REAL-BOX-GATED pi oracle (WS3-pi / pi-A). Drives a real
# `pi --mode json` single-turn, asserts the terminal NDJSON sequence
# (session → agent_end), and writes testdata/twin-parity/pi/<scn>/{ndjson,
# events.jsonl}. DEFAULT-SKIPPED without PI_LIVE=1; needs pi on PATH (or PI_BIN),
# PI_PROVIDER + PI_MODEL, and valid pi provider auth. Anti-false-green:
# HARMONIK_REQUIRE_PI_LIVE=1 turns a can't-run skip into a Fatalf.
.PHONY: test-pi-live
test-pi-live:  ## Real-pi oracle gate (PI_LIVE=1 required; pi provider auth; writes pi twin-parity fixtures)
	PI_LIVE=1 go test -timeout 180s -count=1 -run TestPiA_ ./internal/harness/pi/...

# test-twin-parity-pi: the ROUTINE pi twin-parity gate (WS3-pi / pi-C). Compares
# the pi twin's NDJSON (committed testdata/twin-parity/pi/happy-path-sample/ndjson
# — deterministic `harmonik-twin-pi --scenario happy-path` output) against the
# reference capture on the pi-native wire spine (session → agent_end) + the
# daemon-projected durable terminal triad, and proves a drifted twin is caught
# with a first-divergence diff. Cheap, deterministic, zero-token — NO auth or live
# pi needed (distinct from test-pi-live, the separate REAL-BOX re-capture).
.PHONY: test-twin-parity-pi
test-twin-parity-pi:  ## Routine pi twin-parity gate (twin-vs-reference-capture; zero-token, deterministic)
	go test -count=1 -run 'PiParity' ./internal/twinparity/...

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
	KEEPER_LIVE=1 go test -timeout 180s -count=1 -run TestL3_ ./internal/keepertest/...

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
# Secret scan — blocks staging content that adds API keys, credential
# patterns, or .env files. Invoked by the agent-driven validation command
# (git hooks are retired); also callable standalone to audit a working tree.
# ---------------------------------------------------------------------------
.PHONY: secret-scan
secret-scan:  ## Scan staged diff for API keys / credentials / .env files
	scripts/secret-scan.sh

# ---------------------------------------------------------------------------
# Format write + fail-closed format check
#
# fmt: write gofumpt + gci formatting in-place (used by pre-commit hook and
#      manually to fix a dirty tree).
# fmt-check: fail with a non-zero exit code if any file is unformatted (used
#            by check-fast, check, and CI). gofumpt -l and gci diff both exit 0
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
# Tier 1 — check-fast (<15s target)
# Author-iteration speed.  Pre-commit hook runs this on staged files.
# ---------------------------------------------------------------------------
.PHONY: check-fast
check-fast:  ## Tier 1: fmt-check (fail-closed), go vet, go build, golangci-lint --new-from-rev, go test -short
	scripts/go-format-test.sh
	$(MAKE) fmt-check
	go vet ./...
	go build ./...
	$(MAKE) vet-tagged
	$(TOOLS_DIR)/golangci-lint run --allow-parallel-runners --new-from-rev=HEAD~1
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
	@# `|| exit 1` is load-bearing. Without it the recipe line's status comes from
	@# the trailing `if`, so a refusal from the script is discarded and the gate
	@# goes green while the diagnostic scrolls past on stderr.
	@CHANGED_PKGS=$$(scripts/changed-go-packages.sh) || exit 1; \
	if [ -n "$$CHANGED_PKGS" ]; then \
		echo "check-fast: testing" $$CHANGED_PKGS; \
		printf '%s\n' "$$CHANGED_PKGS" | tr '\n' '\0' | xargs -0 go test -short; \
	else \
		echo "check-fast: no Go file changed in HEAD, in the working tree, or untracked - skipping go test"; \
	fi

# ---------------------------------------------------------------------------
# Tier 2a — check-short (CI gate, ~2-3 min)
# Runs in CI on every push/PR via .github/workflows/ci.yml.
# Skips real-daemon E2E tests (skipRealDaemonE2EInShort) that require br,
# twin binaries, and a live daemon — none available on hosted runners.
# Those tests live in the separate Tier 3 scenario lane (hk-6hzci).
# TMPDIR=/tmp: ensures socket-path tests don't hit macOS TMPDIR length limits.
# ---------------------------------------------------------------------------
.PHONY: check-short
check-short:  ## CI Tier 2: fmt-check + golangci-lint (new-from-rev) + go test -short -race (skips real-daemon E2E; hk-jzepv)
	scripts/go-format-test.sh
	scripts/changed-go-packages-test.sh
	@# ~12s, so it lives here rather than in check-fast's <15s budget. It guards
	@# loadgen.sh's argument parsing, where an omitted option value once turned
	@# the parser itself into the runaway spin loop the script exists to prevent.
	scripts/loadgen-test.sh
	$(MAKE) fmt-check
	go vet ./...
	go build ./...
	$(MAKE) vet-tagged
	$(TOOLS_DIR)/golangci-lint run --allow-parallel-runners --new-from-rev=origin/main
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
	# PROVEN-GREEN recipe = all THREE knobs together (isolated proof: run
	# 28969662856, supervise green at 37.2s; daemon pkg green at ~930s):
	#   -p=1          serialize PACKAGES to kill cross-package -race saturation
	#   -parallel=1   serialize intra-package t.Parallel to kill shared-state
	#                 collisions (cmd/harmonik signature-less + brcli/lifecycle
	#                 0.00s fails)
	#   -timeout=20m  headroom for the daemon pkg running serially (~930s > the
	#                 default 10m, else it panics "test timed out after 10m0s")
	# Restore -parallel=2 only after the colliding pkgs are made hermetic
	# (see follow-up hk-d515w).
	TMPDIR=/tmp go test -short -race -count=1 -p=1 -parallel=1 -timeout=20m ./...

# ---------------------------------------------------------------------------
# check-report — QUIET unified reporter over the check gauntlet (hk-l4sen).
# Runs EVERY step of a tier but presents ~10 lines on green (one ✓ per step)
# and, on red, only the failing step's failing lines. Changes presentation
# only; the step list is derived at runtime from `make -n <target>` so it can
# never silently drop a step. TIER selects the underlying tier target:
#   fast  -> check-fast    short -> check-short (default)    full -> check
# ---------------------------------------------------------------------------
TIER ?= short
.PHONY: check-report
check-report:  ## Quiet reporter: ~10 lines on green, only failing step's lines on red (TIER=fast|short|full, default short)
	scripts/check-report.sh $(TIER)

# ---------------------------------------------------------------------------
# Tier 2b — check-race-full (non-gating nightly)
# Full-parallel -race run with no -short and no -parallel cap.  Used as the
# nightly CI gate (.github/workflows/nightly-race.yml) to surface data races
# suppressed by check-short's -parallel=1 saturation guard.  Never blocks
# merges; result surfaced via ops-monitor checks['nightly-race'] digest.
# (hk-plw4z)
# ---------------------------------------------------------------------------
.PHONY: check-race-full
check-race-full:  ## Non-gating nightly: go test -race -count=1 ./... (full-parallel, no -short, no -parallel cap; hk-plw4z)
	TMPDIR=/tmp go test -race -count=1 ./...

# ---------------------------------------------------------------------------
# Tier 2 — check (~3-5 min target)
# Default pre-push + work-in-progress verification.
# ---------------------------------------------------------------------------
.PHONY: check
check:  ## Tier 2: fmt-check (fail-closed), full golangci-lint, go test -race, go mod tidy check, coverage gate, govulncheck
	$(MAKE) fmt-check
	go vet ./...
	go build ./...
	$(TOOLS_DIR)/golangci-lint run
	go test -race -count=1 ./...
	@# go mod tidy diff check — fail if tidy would change go.mod or go.sum
	@cp go.mod go.mod.check
	@cp go.sum go.sum.check 2>/dev/null || true
	@go mod tidy
	@if ! diff -q go.mod go.mod.check >/dev/null 2>&1 || ! diff -q go.sum go.sum.check >/dev/null 2>&1; then \
		cp go.mod.check go.mod; \
		[ -f go.sum.check ] && cp go.sum.check go.sum || rm -f go.sum; \
		rm -f go.mod.check go.sum.check; \
		echo "ERROR: go mod tidy would change go.mod or go.sum; run 'go mod tidy' and commit the result"; exit 1; \
	fi
	@cp go.mod.check go.mod
	@[ -f go.sum.check ] && cp go.sum.check go.sum || rm -f go.sum
	@rm -f go.mod.check go.sum.check
	go run ./tools/forbid-import ./...
	@if [ -x scripts/coverage-gate.sh ]; then scripts/coverage-gate.sh; else echo "coverage-gate.sh not yet present (hk-pvcs.5); skipping"; fi
	@# cmd/** coverage ratchet. Lives in tier 2, not check-fast: it runs
	@# `go test -covermode=atomic ./cmd/...`, which blows the 15s fast budget.
	@# Runs under a private GOCACHE: measured on 2026-07-22, a shared-cache run
	@# fails with "could not import flag ... no such file or directory" whenever a
	@# concurrent process invalidates cache facts mid-run. The gate fails closed on
	@# that, so without isolation it reports a spurious hard failure.
	scripts/with-isolated-gocache.sh scripts/cmd-coverage-gate.sh
	$(TOOLS_DIR)/govulncheck ./...

# ---------------------------------------------------------------------------
# Tier 3 — check-full (~10-15 min target)
# Agent declared-done MUST pass this.
# ---------------------------------------------------------------------------
.PHONY: check-full
check-full:  ## Tier 3: everything in check + integration + scenario + crash test suites
	$(MAKE) check
	go test -race -tags=integration ./...
	$(MAKE) test-scenario
	go test -tags=crash ./test/crash/...

# ---------------------------------------------------------------------------
# Keeper acceptance corpus — keeper conformance set (hk-urxa3)
# Named conformance set for the keeper test-validation system.  Runs all
# 6 corpus scenarios and the supporting floor without a real tmux session.
# test-keeper-conformance-full additionally runs the L-twin integration tier
# (requires tmux on PATH).
#
# Corpus map:
#   floor: band-min / force-act / hard-ceiling SID-independent / pct-inert-warn-1m
#          live-watcher flock vs corpse / operator-attached warn-only
#   #1 restart-now does not abort no_tmux_target (B4 fix, L-fake-tmux + L-twin)
#   #2 session_id survives /clear, rebinds same lane (L-twin only)
#   #3 unconfirmed handoff not truncated, no second nonce (hk-vpnp)
#   #4 watch re-stall auto-heals once, no alert storm (B3 fix, L-fake-tmux + L-twin)
#   #5 hold dies on restart; hard-ceiling overrides hold; WARN fires under hold
#   #6 binary-upgrade refuse-to-start + config --example restores
# ---------------------------------------------------------------------------
.PHONY: test-keeper-conformance
test-keeper-conformance:  ## Keeper acceptance corpus: 6 scenarios + floor, zero real tmux (hk-urxa3)
	go test -race -count=1 -run 'TestKeeperConformance' ./internal/keeper/ ./cmd/harmonik/

.PHONY: test-keeper-conformance-full
test-keeper-conformance-full: test-keeper-conformance  ## Keeper acceptance corpus + L-twin integration tier (requires real tmux)
	go test -race -tags=integration -count=1 -run 'TestKeeperConformanceCorpus_Integration' ./internal/keeper/

# ---------------------------------------------------------------------------
# Release validation gate (hk-o4j13)
# Invoked by the release CI workflow (hk-jdesv adds the .github/workflows step).
# Runs each phase in order; any nonzero exit propagates immediately.
#   1. lint               — golangci-lint full run
#   2. go test -short     — unit suite, -race, skip heavy E2E (hk-p258q)
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
.PHONY: lint lint-full-count
lint:  ## golangci-lint run (shorthand)
	$(TOOLS_DIR)/golangci-lint run

lint-full-count:  ## Publish the full-tree grandfathered lint finding count (not a gate)
	@REPORT=$$(mktemp); \
	trap 'rm -f "$$REPORT"' EXIT; \
	LINT_STATUS=0; \
	scripts/with-isolated-gocache.sh $(TOOLS_DIR)/golangci-lint run --allow-parallel-runners --issues-exit-code=0 --max-issues-per-linter=0 --max-same-issues=0 \
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

.PHONY: check-verdict
check-verdict:  ## Cross-check diff-keyed verdict: APPROVE → pass; absent/REQUEST_CHANGES/BLOCK → fail (hk-q6axs.4)
	@scripts/check-verdict.sh --diff HEAD~1

# ---------------------------------------------------------------------------
# Tool installation
# Pins dev tools into ./.tools/ to avoid polluting the global GOPATH.
# Fresh-clone setup: make bootstrap  (installs tools)
#
# NOTE: git hooks are RETIRED. lefthook (and its self-re-arming `install`)
# was removed — validation now runs via the agent-driven validation command,
# not a pre-commit/pre-push/commit-msg hook. scripts/validate-commit-msg.sh
# and scripts/secret-scan.sh remain callable directly by that command.
# ---------------------------------------------------------------------------
.PHONY: tools
tools:  ## Install pinned dev tools into ./.tools/ (gofumpt, gci, golangci-lint, govulncheck)
	@mkdir -p $(TOOLS_DIR)
	$(GOBIN_TOOLS) go install mvdan.cc/gofumpt@v0.7.0
	$(GOBIN_TOOLS) go install github.com/daixiang0/gci@v0.13.5
	$(GOBIN_TOOLS) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.3.0
	$(GOBIN_TOOLS) go install golang.org/x/vuln/cmd/govulncheck@v1.1.4

# bootstrap: one-stop fresh-clone setup — installs pinned tools.
.PHONY: bootstrap
bootstrap: tools  ## Fresh-clone setup: install pinned dev tools

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------
.PHONY: help
help:  ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'
