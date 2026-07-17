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

# Install the daemon's client public key from the shared volume. Wait briefly for
# the daemon side to publish it (both containers come up concurrently).
mkdir -p /root/.ssh
chmod 700 /root/.ssh
PUB="${KEYS_DIR}/id_ed25519.pub"
for _ in $(seq 1 50); do
    [ -f "${PUB}" ] && break
    sleep 0.2
done
if [ -f "${PUB}" ]; then
    # Idempotent: don't accumulate dup lines across restarts on a persistent volume.
    touch /root/.ssh/authorized_keys
    grep -qxF "$(cat "${PUB}")" /root/.ssh/authorized_keys || cat "${PUB}" >> /root/.ssh/authorized_keys
    chmod 600 /root/.ssh/authorized_keys
    echo "entrypoint-worker: installed client key from ${PUB}"
else
    echo "entrypoint-worker: WARNING no client key at ${PUB} — passwordless ssh will fail" >&2
fi

# Ensure the writable clone path exists (bind-mount or fresh).
mkdir -p /work/worker

echo "entrypoint-worker: starting sshd (foreground)"
exec /usr/sbin/sshd -D -e
