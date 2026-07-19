#!/usr/bin/env bash
# entrypoint-worker.sh — WS2.2 worker-container boot.
#
# LOCKED key mgmt: no secret is baked into the image. The daemon container
# generates a client keypair into the shared volume ($KEYS_DIR/id_ed25519.pub);
# this worker installs that public key into root's authorized_keys, generates its
# own sshd host keys, then runs sshd in the foreground.
set -euo pipefail

KEYS_DIR="${KEYS_DIR:-/keys}"

# Host keys — generated per-boot (not committed).
ssh-keygen -A

# Install the daemon's client public key from the shared volume. The daemon only
# GENERATES its key AFTER it boots, and under WS2.3 compose the daemon boots only
# once this worker is healthy (sshd listening) — so the key is NOT guaranteed to
# be present at worker-boot time. A one-shot poll here would race and lose. Run an
# idempotent background installer that watches the shared volume and appends the
# key to authorized_keys as soon as it appears (and stays a no-op once installed),
# so `ssh worker true` starts succeeding the moment the daemon has published.
mkdir -p /root/.ssh
chmod 700 /root/.ssh
touch /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys
PUB="${KEYS_DIR}/id_ed25519.pub"
install_key_watcher() {
    while true; do
        if [ -f "${PUB}" ]; then
            # Idempotent: don't accumulate dup lines across restarts / re-checks.
            if ! grep -qxF "$(cat "${PUB}")" /root/.ssh/authorized_keys 2>/dev/null; then
                cat "${PUB}" >> /root/.ssh/authorized_keys
                echo "entrypoint-worker: installed client key from ${PUB}"
            fi
        fi
        sleep 1
    done
}
install_key_watcher &

# Ensure the writable clone path exists (bind-mount or fresh).
mkdir -p /work/worker

echo "entrypoint-worker: starting sshd (foreground)"
exec /usr/sbin/sshd -D -e
