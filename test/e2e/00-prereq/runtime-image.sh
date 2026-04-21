#!/usr/bin/env bash
# agent-runtime image is built. Required for any container-run test.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/docker.sh"
source "${E2E_LIB}/skip.sh"

skip_if_no_docker
skip_if_no_image "agent-runtime:latest"

# Image exists: confirm its build-id file was baked in so state-volume
# mismatch detection still works.
out=$(docker run --rm --entrypoint cat agent-runtime:latest /etc/airun-build-id 2>&1 || true)
if ! [[ "$out" =~ ^[0-9]+$ ]]; then
    die "/etc/airun-build-id missing or malformed in image (got: '$out')"
fi
