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

.PHONY: build
build: build-harmonik  ## go build ./... + cmd/harmonik stamped binary (hk-mz0x4)
	go build ./...

.PHONY: test
test:  ## go test ./... (no race; quick smoke)
	go test ./...

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
# Tier 1 — check-fast (<15s target)
# Author-iteration speed.  Pre-commit hook runs this on staged files.
# ---------------------------------------------------------------------------
.PHONY: check-fast
check-fast:  ## Tier 1: gofumpt+gci diff, go vet, go build, golangci-lint --new-from-rev, go test -short
	$(TOOLS_DIR)/gofumpt -l -d .
	$(TOOLS_DIR)/gci diff -s standard -s default -s 'prefix($(MODULE))' .
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
# Tier 2 — check (~3-5 min target)
# Default pre-push + work-in-progress verification.
# ---------------------------------------------------------------------------
.PHONY: check
check:  ## Tier 2: full golangci-lint, go test -race, go mod tidy check, coverage gate, govulncheck
	$(TOOLS_DIR)/gofumpt -l -d .
	$(TOOLS_DIR)/gci diff -s standard -s default -s 'prefix($(MODULE))' .
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
	go test -race -tags=scenario ./test/scenario/...
	go test -tags=crash ./test/crash/...

# ---------------------------------------------------------------------------
# Lint shorthand
# ---------------------------------------------------------------------------
.PHONY: lint
lint:  ## golangci-lint run (shorthand)
	$(TOOLS_DIR)/golangci-lint run

# ---------------------------------------------------------------------------
# Agent review — LOCAL ONLY
# Invokes the agent-reviewer skill against the diff vs. the last commit.
# The skill is filed under hk-jhob.1 and is not yet installed.
# If the skill binary/wrapper is not present, exits 0 with an explanatory
# message so that Makefile pipelines are not blocked during early bootstrap.
# ---------------------------------------------------------------------------
.PHONY: agent-review
agent-review:  ## Run agent-reviewer skill against diff vs last commit (local only; stubs gracefully if skill absent)
	@SKILL=".claude/skills/agent-reviewer/run"; \
	if [ -x "$$SKILL" ]; then \
		timeout $(AGENT_REVIEW_TIMEOUT) "$$SKILL" --diff HEAD~1; \
		EXIT=$$?; \
		if [ $$EXIT -eq 124 ]; then \
			echo "agent-review: timed out after $(AGENT_REVIEW_TIMEOUT)s; retry manually or add Trivial: true for trivial commits."; \
			exit 1; \
		fi; \
		exit $$EXIT; \
	else \
		echo "agent-reviewer skill not yet installed (filed under hk-jhob.1)."; \
		echo "Install it to enable structured pre-commit review; skipping for now."; \
		exit 0; \
	fi

# ---------------------------------------------------------------------------
# Tool installation
# Pins dev tools into ./.tools/ to avoid polluting the global GOPATH.
# Run once after a fresh clone: make tools
# ---------------------------------------------------------------------------
.PHONY: tools
tools:  ## Install pinned dev tools into ./.tools/ (gofumpt, gci, golangci-lint, govulncheck, lefthook)
	@mkdir -p $(TOOLS_DIR)
	$(GOBIN_TOOLS) go install mvdan.cc/gofumpt@v0.7.0
	$(GOBIN_TOOLS) go install github.com/daixiang0/gci@v0.13.5
	$(GOBIN_TOOLS) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.3.0
	$(GOBIN_TOOLS) go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
	$(GOBIN_TOOLS) go install github.com/evilmartians/lefthook@v1.11.13

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------
.PHONY: help
help:  ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'
