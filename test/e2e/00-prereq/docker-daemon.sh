#!/usr/bin/env bash
# docker daemon is reachable. Many downstream tests need it; we skip (not fail)
# on laptops where the user hasn't started Docker/OrbStack yet.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/docker.sh"

if ! command -v docker >/dev/null 2>&1; then
    skip "docker CLI is not installed"
fi
if ! docker_available; then
    skip "docker daemon is not responding (start OrbStack / Docker Desktop)"
fi

# If we get here the daemon responded at least once — record its server version
# so test logs have useful context.
docker version --format '{{.Server.Version}}' || true
