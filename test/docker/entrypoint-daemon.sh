#!/usr/bin/env bash
# entrypoint-daemon.sh — WS2.1 daemon-container boot.
#
# Brings up a self-contained harmonik project and starts the daemon so a unix
# socket appears at $HARMONIK_PROJECT/.harmonik/daemon.sock, then blocks to keep
# the container alive. WS2.3 compose may override CMD/args to point at a shared
# project volume + the remote-substrate worker registry; this default makes a
# bare `docker run` demonstrate a live daemon socket.
set -euo pipefail

PROJECT="${HARMONIK_PROJECT:-/work/project}"
SOCK="${PROJECT}/.harmonik/daemon.sock"

# Ssh client default for reaching the worker container passwordlessly (WS2.2).
mkdir -p /root/.ssh
chmod 700 /root/.ssh
if ! grep -q 'StrictHostKeyChecking' /root/.ssh/config 2>/dev/null; then
    printf 'Host *\n    StrictHostKeyChecking accept-new\n    UserKnownHostsFile /root/.ssh/known_hosts\n' > /root/.ssh/config
    chmod 600 /root/.ssh/config
fi

# LOCKED key mgmt (WS2.2): generate the client keypair at compose-up and publish
# the PUBLIC half to the shared volume so the worker can install it into its
# authorized_keys. The private key never leaves the daemon container / volume,
# and nothing is committed to git.
KEYS_DIR="${KEYS_DIR:-/keys}"
if [ -d "${KEYS_DIR}" ] || mkdir -p "${KEYS_DIR}" 2>/dev/null; then
    if [ ! -f "${KEYS_DIR}/id_ed25519" ]; then
        ssh-keygen -t ed25519 -N '' -f "${KEYS_DIR}/id_ed25519" -C harmonik-daemon >/dev/null
    fi
    install -m 600 "${KEYS_DIR}/id_ed25519"     /root/.ssh/id_ed25519
    install -m 644 "${KEYS_DIR}/id_ed25519.pub" /root/.ssh/id_ed25519.pub
fi

# A git repo is a precondition of `harmonik init`.
if [ ! -d "${PROJECT}/.git" ]; then
    mkdir -p "${PROJECT}"
    git config --global user.email daemon@harmonik.test
    git config --global user.name  harmonik-daemon
    git config --global init.defaultBranch main
    git -C "${PROJECT}" init -q
    git -C "${PROJECT}" commit -q --allow-empty -m "init"
fi

# Idempotent bootstrap: create .harmonik, beads DB, config; do NOT auto-supervise
# here (we start the supervisor explicitly below so we control the foreground).
if [ ! -d "${PROJECT}/.harmonik" ]; then
    harmonik init --project "${PROJECT}" --no-supervise
fi

# WS4-2 credfence (WS4-0 §5.2): strip any inherited API keys so the ONLY usable
# credential is the read-only-mounted /root/.claude subscription set. Belt-and-
# suspenders with compose omitting them from `environment:` — mirrors
# scripts/scratch-daemon.sh:244's `env -u`. The daemon (and every agent subprocess
# it forks) inherits this stripped environment via the tmux session below. NEVER
# ANTHROPIC_API_KEY (D2); missing /root/.claude ⇒ loud-PENDING, never key auth.
unset ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN

# Start the daemon inside a DETACHED tmux session. `harmonik daemon` requires
# $TMUX to be set; launching it as the session's command satisfies that without
# needing a TTY to attach to (`harmonik tmux-start` would try to attach, which
# fails in a non-interactive container). WS2.3 compose may layer `harmonik
# supervise start --watch-restart` on top of this for auto-revive.
tmux new-session -d -s harmonik-daemon -c "${PROJECT}" 'harmonik daemon 2>&1 | tee -a /tmp/harmonik-daemon.log'

for _ in $(seq 1 50); do
    [ -S "${SOCK}" ] && break
    sleep 0.2
done
if [ -S "${SOCK}" ]; then
    echo "entrypoint-daemon: daemon socket up at ${SOCK}"
else
    echo "entrypoint-daemon: WARNING socket ${SOCK} did not appear" >&2
fi

# Keep the container alive (compose/tests exec into it).
exec tail -f /dev/null
