#!/usr/bin/env bash
set -euo pipefail

# Keep direct queue status writes in the transition owner. This is standalone.

script_dir="$(cd "$(dirname "$0")" && pwd)"
exec go run "$script_dir/queue-status-writer-ratchet.go"
