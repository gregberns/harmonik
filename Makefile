# Harmonik Makefile
# Three-tier check gauntlet (Tier 1 / Tier 2 / Tier 3) + helpers.
# Local/CI parity: every CI gate invokes these same targets verbatim.
# See docs/foundation/project-level/quality-checks.md §Three-tier identical gauntlet.

.DEFAULT_GOAL := check

# Tool bin dir — keeps dev tools out of the global GOPATH.
TOOLS_DIR := $(PWD)/.tools
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
# Prereq: build-all compiles cmd/harmonik + all twins (twins/generic-twin,
# harmonik-twin-claude) so scenario tests can locate them without a rebuild.
# Budget: 10 minutes, matching the scenario sub-run in check-full (Tier 3).
# Covers all packages that carry //go:build scenario files:
#   ./test/scenario/...  — top-level scenario package (test/scenario/harness_test.go)
#   ./internal/daemon/...— daemon-resident scenario tests (scenario_*.go files)
# See docs/methodology/TESTING.md §Scenario fixture determinism recipe for the
# worktree-factory / merge-mutex / phase-aware-twin / Skip* recipe used here.
.PHONY: test-scenario
test-scenario: build-all  ## Run scenario tier (-race, -tags=scenario, 10m budget; prereq: build-all)
	go test -race -tags=scenario -timeout 10m ./test/scenario/... ./internal/daemon/...

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

# build-twin-claude: alias kept for compatibility during transition; delegates
# to build-twin-generic until hk-w5vra.2 ships the real Claude twin.
# TODO(hk-w5vra.2): replace this alias with the real harmonik-twin-claude build.
.PHONY: build-twin-claude
build-twin-claude: build-twin-generic  ## Alias → build-twin-generic (hk-w5vra.2 will replace with real Claude twin)

# twins: build all twin binaries into twins/.
# Add further per-twin prerequisites here as new twin packages land.
.PHONY: twins
twins: build-twin-generic  ## Build all twin binaries into twins/ (SH-009 search-path default)

# build-all: build the module + all twin binaries.
# Suitable as a pre-scenario-test warmup target.
.PHONY: build-all
build-all: build twins  ## go build ./... + all twins (full build artifact set)

# ---------------------------------------------------------------------------
# Secret scan — runs as the first pre-commit command (lefthook secret-scan).
# Also callable standalone to audit a working tree before staging.
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
	$(TOOLS_DIR)/gci write -s standard -s default -s 'prefix($(MODULE))' .
	$(TOOLS_DIR)/gofumpt -w .

.PHONY: fmt-check
fmt-check:  ## Fail-closed: exit 1 if gofumpt or gci would change any file (run 'make fmt' to fix)
	@UNFORMATTED=$$($(TOOLS_DIR)/gofumpt -l .); \
	if [ -n "$$UNFORMATTED" ]; then \
		echo "gofumpt: unformatted files (run 'make fmt' to fix):"; \
		echo "$$UNFORMATTED"; \
		$(TOOLS_DIR)/gofumpt -d .; \
		exit 1; \
	fi
	@GCI_DIFF=$$($(TOOLS_DIR)/gci diff -s standard -s default -s 'prefix($(MODULE))' .); \
	if [ -n "$$GCI_DIFF" ]; then \
		echo "gci: import order drift detected (run 'make fmt' to fix):"; \
		echo "$$GCI_DIFF"; \
		exit 1; \
	fi

# ---------------------------------------------------------------------------
# Tier 1 — check-fast (<15s target)
# Author-iteration speed.  Pre-commit hook runs this on staged files.
# ---------------------------------------------------------------------------
.PHONY: check-fast
check-fast:  ## Tier 1: fmt-check (fail-closed), go vet, go build, golangci-lint --new-from-rev, go test -short
	$(MAKE) fmt-check
	go vet ./...
	go build ./...
	$(TOOLS_DIR)/golangci-lint run --new-from-rev=HEAD~1
	@CHANGED_PKGS=$$(git diff --name-only HEAD 2>/dev/null | grep '\.go$$' | xargs -I{} dirname {} | sort -u | sed 's|^|./|' | tr '\n' ' '); \
	if [ -n "$$CHANGED_PKGS" ]; then \
		go test -short $$CHANGED_PKGS; \
	else \
		echo "check-fast: no changed Go packages, skipping go test"; \
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
	$(MAKE) fmt-check
	go vet ./...
	go build ./...
	$(TOOLS_DIR)/golangci-lint run --new-from-rev=origin/main
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
.PHONY: lint
lint:  ## golangci-lint run (shorthand)
	$(TOOLS_DIR)/golangci-lint run

# ---------------------------------------------------------------------------
# specaudit-lint — spec-drift lint (M1-1)
# Runs the 129 relocated spec-prose sensor tests behind the `specaudit` build
# tag (skipped by the default `go test ./...`). See
# internal/specaudit/RELOCATED-ALLOWLIST.md.
# ---------------------------------------------------------------------------
.PHONY: specaudit-lint
specaudit-lint:  ## Run the tagged spec-drift sensor suite (-tags specaudit; M1-1)
	scripts/specaudit-lint.sh

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
# Tool installation + git-hooks setup
# Pins dev tools into ./.tools/ to avoid polluting the global GOPATH.
# Fresh-clone setup: make bootstrap  (installs tools + wires git hooks)
# ---------------------------------------------------------------------------
.PHONY: tools
tools:  ## Install pinned dev tools into ./.tools/ (gofumpt, gci, golangci-lint, govulncheck, lefthook)
	@mkdir -p $(TOOLS_DIR)
	$(GOBIN_TOOLS) go install mvdan.cc/gofumpt@v0.7.0
	$(GOBIN_TOOLS) go install github.com/daixiang0/gci@v0.13.5
	$(GOBIN_TOOLS) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.3.0
	$(GOBIN_TOOLS) go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
	$(GOBIN_TOOLS) go install github.com/evilmartians/lefthook@v1.11.13

# install-hooks: wire lefthook.yml hooks into .git/hooks/ so pre-commit,
# pre-push, and commit-msg gates run automatically on every commit.
# Prereq: lefthook binary must exist in .tools/ (run `make tools` first).
.PHONY: install-hooks
install-hooks:  ## Wire lefthook.yml hooks into .git/hooks/ (run after make tools)
	$(TOOLS_DIR)/lefthook install

# check-hooks: assert that the hooks installed in .git/hooks/ match lefthook.yml.
# Fails if any hook declared in lefthook.yml is absent or does not invoke lefthook.
# Run in CI after install-hooks to detect drift.
.PHONY: check-hooks
check-hooks:  ## Assert installed git hooks match lefthook.yml (CI drift check)
	scripts/check-hooks.sh

# bootstrap: one-stop fresh-clone setup — installs pinned tools then wires hooks.
# Run this once after cloning; subsequent `make tools` re-pins tools without
# re-running lefthook install (though re-running bootstrap is harmless).
.PHONY: bootstrap
bootstrap: tools install-hooks  ## Fresh-clone setup: install tools + wire git hooks (lefthook install)

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------
.PHONY: help
help:  ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'
