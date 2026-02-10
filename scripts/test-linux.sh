#!/usr/bin/env bash
# Run Linux isolation tests in Docker.
# Requires: Docker with cgroups v2 support.
set -euo pipefail

REPO_ROOT="$(git -C "$(dirname "$0")/.." rev-parse --show-toplevel)"

docker build -t opcode-test-linux -f "$REPO_ROOT/Dockerfile.test" "$REPO_ROOT"
docker run --rm \
    --privileged \
    --cgroupns=host \
    opcode-test-linux
