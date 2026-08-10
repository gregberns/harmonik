#!/usr/bin/env bash
#
# scratch-daemon-toolchain-test.sh — assertions for provision_toolchain in
# scripts/scratch-daemon.sh.
#
# THE DEFECT THIS HOLDS SHUT. A scratch clone is what the live gate
# (`make core-loop-lt`) dispatches real agents into. Every implementer that runs
# there ends at the commit gate, and the commit gate reaches `.tools/` for
# gofumpt, gci and golangci-lint. Nothing ever installed them. The codex cell
# therefore died `exit 127` on `.tools/gofumpt: No such file or directory` —
# three minutes into a run, with a bare ENOENT that names no cause and reads
# like a product failure. `git clean -qfdx -e .harmonik` in `init` deletes
# `.tools/` on every run including `--reuse`, so there was also no earlier point
# that could have provisioned it and survived.
#
# WHAT IS ASSERTED, AND WHY EACH CASE IS NEEDED:
#
#   WIRED       `init` must CALL the function. A provisioning step nothing
#               invokes is the same run failure with more code in front of it.
#   INSTALLS    a successful recipe leaves the function reporting success.
#   PARTIAL     a recipe that exits 0 while leaving a tool absent must be a hard
#               error. This is the case the whole verify step exists for: under
#               a warm cache `go install` can leave a partial .tools/ and still
#               exit 0, and a half-provisioned toolchain fails at exactly the
#               same place as no toolchain at all.
#   RECIPE-FAIL a failing recipe must stop init rather than start a daemon that
#               will fail every bead minutes later.
#   UNPARSEABLE a `tools:` recipe this function cannot read tool names out of
#               must be a hard error, not a silent zero-tool pass. Without this,
#               reformatting the Makefile would disarm the check and nothing
#               would say so.
#   NO-MAKEFILE a tree with no Makefile must be refused rather than skipped. A
#               skip here would be fail-open: it returns the run to the exact
#               ENOENT above, with one more line of output in front of it.
#   REAL-SHAPE  the name derivation must agree with THIS repo's real Makefile.
#               Every other case uses a fixture, so this is the only one that
#               fails when the real recipe and the parser drift apart.
#
# The fixtures are throwaway Makefiles, never the real one: `go install` needs
# the network and minutes, and neither is the subject here. REAL-SHAPE covers
# the real recipe by parsing it, which needs no network at all.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="$repo_root/scripts/scratch-daemon.sh"

assertions=0
failures=0

pass() {
	assertions=$((assertions + 1))
	echo "scratch-daemon-toolchain-test: ok: $1"
}

fail() {
	assertions=$((assertions + 1))
	failures=$((failures + 1))
	echo "scratch-daemon-toolchain-test: FAIL: $1"
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# run_provision <dir> — call provision_toolchain against <dir> in a child bash.
# `--help` is what makes sourcing safe: scratch-daemon.sh ends in `main "$@"`,
# and help is its one subcommand that defines every function and then returns.
# Stdout and stderr are joined because the diagnostics under test are on stderr.
run_provision() {
	bash -c '
		source "$1" --help >/dev/null 2>&1
		provision_toolchain "$2"
	' _ "$script" "$1" 2>&1
}

# fixture <name> <recipe-body> — a directory holding only a Makefile. TOOLS_DIR
# is spelled the way the real Makefile spells it, so the function's own notion
# of where tools land is exercised rather than assumed.
fixture() {
	local dir="$work/$1" body="$2"
	mkdir -p "$dir"
	{
		printf 'TOOLS_DIR := $(shell pwd)/.tools\n\n'
		printf '.PHONY: tools\ntools:\n'
		printf '%s\n' "$body"
	} >"$dir/Makefile"
	echo "$dir"
}

# ---------------------------------------------------------------------------
# WIRED — init calls it.
# ---------------------------------------------------------------------------
if awk '/^cmd_init\(\)/ { in_fn = 1 } in_fn && /provision_toolchain/ { found = 1 } in_fn && /^}/ { in_fn = 0 } END { exit !found }' "$script"; then
	pass "WIRED: cmd_init calls provision_toolchain"
else
	fail "WIRED: cmd_init never calls provision_toolchain — the scratch clone is dispatched without a toolchain"
fi

# ---------------------------------------------------------------------------
# INSTALLS — a recipe that leaves both binaries reports success.
#
# `@:` is the shell no-op, so the two `go install` lines are read by the name
# derivation and executed by nothing. That is the point: this case is about the
# function's control flow, not about Go's installer.
# ---------------------------------------------------------------------------
ok_recipe='	@mkdir -p $(TOOLS_DIR) && touch $(TOOLS_DIR)/alpha $(TOOLS_DIR)/beta && chmod +x $(TOOLS_DIR)/alpha $(TOOLS_DIR)/beta
	@: go install example.com/a/cmd/alpha@v1.0.0
	@: go install example.com/b/cmd/beta@v2.0.0'
dir="$(fixture installs "$ok_recipe")"
out="$(run_provision "$dir")"
status=$?
if [ "$status" -eq 0 ]; then
	pass "INSTALLS: exits 0 when every derived tool is present"
else
	fail "INSTALLS: exited $status on a complete toolchain — $out"
fi
if [ -x "$dir/.tools/alpha" ]; then
	pass "INSTALLS: the recipe wrote into the scratch clone's own .tools/"
else
	fail "INSTALLS: nothing landed in $dir/.tools/"
fi

# ---------------------------------------------------------------------------
# PARTIAL — exits 0, installs one of two.
# ---------------------------------------------------------------------------
partial_recipe='	@mkdir -p $(TOOLS_DIR) && touch $(TOOLS_DIR)/alpha && chmod +x $(TOOLS_DIR)/alpha
	@: go install example.com/a/cmd/alpha@v1.0.0
	@: go install example.com/b/cmd/beta@v2.0.0'
dir="$(fixture partial "$partial_recipe")"
out="$(run_provision "$dir")"
status=$?
if [ "$status" -ne 0 ]; then
	pass "PARTIAL: exits non-zero when a tool is absent (got $status)"
else
	fail "PARTIAL: exited 0 with beta absent — the run would die at the commit gate instead"
fi
if printf '%s' "$out" | grep -q 'beta'; then
	pass "PARTIAL: names the absent tool"
else
	fail "PARTIAL: failed without naming beta — $out"
fi

# ---------------------------------------------------------------------------
# RECIPE-FAIL — the recipe itself fails.
# ---------------------------------------------------------------------------
bad_recipe='	@exit 3
	@: go install example.com/a/cmd/alpha@v1.0.0'
dir="$(fixture recipefail "$bad_recipe")"
out="$(run_provision "$dir")"
status=$?
if [ "$status" -ne 0 ]; then
	pass "RECIPE-FAIL: exits non-zero when 'make tools' fails (got $status)"
else
	fail "RECIPE-FAIL: exited 0 after a failed recipe"
fi

# ---------------------------------------------------------------------------
# UNPARSEABLE — a recipe with no readable tool names.
# ---------------------------------------------------------------------------
opaque_recipe='	@mkdir -p $(TOOLS_DIR)
	@echo installing'
dir="$(fixture unparseable "$opaque_recipe")"
out="$(run_provision "$dir")"
status=$?
if [ "$status" -ne 0 ]; then
	pass "UNPARSEABLE: exits non-zero when no tool name can be read (got $status)"
else
	fail "UNPARSEABLE: exited 0 having verified nothing — the check is disarmed and silent"
fi

# ---------------------------------------------------------------------------
# NO-MAKEFILE — a tree that holds no Makefile at all.
# ---------------------------------------------------------------------------
mkdir -p "$work/nomakefile"
out="$(run_provision "$work/nomakefile")"
status=$?
if [ "$status" -ne 0 ]; then
	pass "NO-MAKEFILE: refuses a tree with no Makefile (got $status)"
else
	fail "NO-MAKEFILE: exited 0 on a tree it could not provision"
fi

# ---------------------------------------------------------------------------
# REAL-SHAPE — the derivation agrees with this repo's real Makefile.
#
# The expected set is spelled out here on purpose. Deriving it a second way
# would only prove the two derivations agree with each other.
# ---------------------------------------------------------------------------
derived="$(awk '
	/^tools:/     { in_recipe = 1; next }
	in_recipe && /^[^\t]/ { in_recipe = 0 }
	in_recipe && /go install/ {
		for (i = 1; i <= NF; i++) if ($i ~ /@/) {
			sub(/@.*$/, "", $i); n = split($i, parts, "/"); print parts[n]
		}
	}
' "$repo_root/Makefile" | sort | tr '\n' ' ')"
expected="deadcode gci gofumpt golangci-lint govulncheck "
if [ "$derived" = "$expected" ]; then
	pass "REAL-SHAPE: the real Makefile yields exactly: $derived"
else
	fail "REAL-SHAPE: expected '$expected' from the real Makefile, read '$derived'.
  Either the pinned tool set changed (update this case) or the 'tools:' recipe
  changed shape and provision_toolchain can no longer read it."
fi

# ---------------------------------------------------------------------------
echo "scratch-daemon-toolchain-test: $assertions assertions, $failures failed"
[ "$failures" -eq 0 ] || exit 1
