#!/usr/bin/env bash
set -euo pipefail

# projectconfig-freeze-gate.sh — P2 LIFT crit 5 extraction ratchet.
#
# The .harmonik/config.yaml loader + config value types left internal/daemon for
# internal/projectconfig (a pure leaf: parseProjectConfig is the pure boundary,
# LoadProjectConfig the one edge). depguard fences the IMPORT edge
# (internal/projectconfig must not import internal/daemon), but it cannot forbid
# CREATING a config file OR re-declaring a moved symbol inside the daemon. This
# grep ratchet closes those doors: it fails the build if a projectconfig-shaped
# non-test file reappears in internal/daemon, or if one of the moved symbols —
# including the unexported parseProjectConfig / rawProjectConfig — is re-declared
# there.
#
# The `type X =` arm ALSO catches and BANS a re-aliasing dodge such as
#   type ProjectConfig = projectconfig.ProjectConfig
# in internal/daemon. That is the operator's explicit no-alias ruling: an alias
# would make this freeze gate unenforceable (the daemon could keep naming
# `daemon.ProjectConfig` forever), so it is forbidden outright — reference
# `projectconfig.ProjectConfig` directly instead.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# (1) No new projectconfig-source file in the daemon package (recursive — a
#     sub-package is the obvious evasion of a -maxdepth 1 scan). Test files are
#     exempt: daemon behavior tests (e.g. projectconfig_hkbfvk7_test.go, which
#     tests ResolveModelPreference) legitimately keep a projectconfig-named test
#     file while USING projectconfig.* as a fixture.
while IFS= read -r f; do
    echo "projectconfig-freeze-gate: FORBIDDEN config-loader file in internal/daemon: $f" >&2
    HITS=$((HITS + 1))
done < <(find internal/daemon -type f -name '*projectconfig*.go' ! -name '*_test.go')

# (2) No re-declaration of the symbols that moved to internal/projectconfig. The
#     alternation also catches a re-declaration inside a grouped `var ( … )` /
#     `const ( … )` / `type ( … )` block, which a bare ^(func|type|var|const)
#     anchor misses, and — via the `=` arm — a `type X = projectconfig.X` alias.
for sym in ProjectConfig LoadProjectConfig parseProjectConfig rawProjectConfig \
           KeeperConfig KeeperConfigPresence SandboxConfig SandboxNetworkConfig \
           SandboxCacheConfig PiHarnessConfig PiFallbackConfig PiProfileConfig \
           SuperviseConfig SuperviseDaemonWatchdogConfig HarnessesConfig \
           WatchConfig WatchdogConfig OpsmonitorConfig StallSentinelConfig \
           CrewConfig DaemonConfig DaemonRestartBackoffConfig \
           ErrMalformedConfigYAML ErrUnknownConfigKey ErrUnsupportedConfigVersion \
           ErrWorkflowModeFloorViolation; do
    # NOTE: `projectconfig` is deliberately ABSENT from the `grep -v` exclusion
    # list below. The exclusion drops legitimate local re-assignments that call a
    # sibling leaf (`x = runmerge.Foo()`); if `projectconfig` were listed, a
    # `type ProjectConfig = projectconfig.ProjectConfig` alias line would be
    # excluded and the no-alias ruling would go unenforced. Keeping it out means
    # the `=` arm trips on exactly that alias. No legitimate daemon line matches
    # (every real reference is spelled `projectconfig.X`, never bare-SYM-at-BOL).
    MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -E "^[[:space:]]*(type[[:space:]]+|func[[:space:]]+|var[[:space:]]+|const[[:space:]]+)?${sym}\b[[:space:]]*(=|struct|interface|func|\()" internal/daemon 2>/dev/null | grep -vE '=[[:space:]]*(shared|claude|codex|pi|crewrun|queuewiring|tunnel|codesync|gitprobe|runmerge)\.' || true)"
    if [ -n "$MATCHES" ]; then
        echo "projectconfig-freeze-gate: FORBIDDEN re-declaration (or alias) of ${sym} in internal/daemon:" >&2
        printf '%s\n' "$MATCHES" >&2
        HITS=$((HITS + 1))
    fi
done

if [ "$HITS" -ne 0 ]; then
    echo "projectconfig-freeze-gate: FAIL — the config loader was extracted in P2 LIFT crit 5; build on internal/projectconfig, do not reopen internal/daemon" >&2
    exit 1
fi
echo "projectconfig-freeze-gate: OK — the config loader stays out of internal/daemon"
