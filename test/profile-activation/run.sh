#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
image=${AIRUN_TEST_IMAGE:-agent-runtime:profiles-dev}
docker run --rm --network none --user 1001:1001 --workdir /tmp \
    --mount "type=bind,src=$root/docker,dst=/airun,readonly" \
    --mount "type=bind,src=$root/test/profile-activation,dst=/acceptance,readonly" \
    --entrypoint node "$image" /acceptance/acceptance.mjs
