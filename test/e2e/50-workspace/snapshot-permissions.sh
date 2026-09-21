#!/usr/bin/env bash
# Real Docker, no model/network: run the updated entrypoint against copied files.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/docker.sh"
docker_available || skip 'Docker unavailable'
image_exists agent-runtime:latest || skip 'agent-runtime:latest unavailable'
work=$(mktemp -d -t airun-permissions.XXXXXXXX)
on_exit "rm -rf '$work'"
mkdir "$work/subdir"
echo original > "$work/subdir/existing.txt"
chmod 755 "$work" "$work/subdir"
chmod 644 "$work/subdir/existing.txt"
metadata_before=$(python3 - "$work/subdir" "$work/subdir/existing.txt" <<'PYMETA'
import os,sys
print([(os.stat(p).st_uid,os.stat(p).st_gid,os.stat(p).st_mode) for p in sys.argv[1:]])
PYMETA
)
entrypoint=$(cd "${E2E_ROOT}/../../docker" && pwd)/entrypoint.sh
name="airun-permission-test-$$-$RANDOM"
on_exit "docker rm -f '$name'"
# A stub prevents marketplace registration and version detection from calling Claude.
printf '#!/bin/sh\necho 99.0.0\n' > "$work/claude-stub"
chmod 755 "$work/claude-stub"
docker create --name "$name" --network none -e AIRUN_WORKSPACE_MODE=snapshot \
  -e PATH=/tmp/test-bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  --mount "type=bind,source=$entrypoint,target=/tmp/test-entrypoint.sh,readonly" \
  --entrypoint /bin/bash agent-runtime:latest /tmp/test-entrypoint.sh \
  sh -c 'test "$(id -u)" = 1001 && echo changed >> /workspace/subdir/existing.txt && echo created > /workspace/subdir/new.txt' >/dev/null
docker cp "$work/." "$name:/workspace"
docker cp "$work/claude-stub" "$name:/tmp/claude-stub"
# Pre-start copy a directory containing the command stub.
mkdir "$work/test-bin"
cp "$work/claude-stub" "$work/test-bin/claude"
docker cp "$work/test-bin" "$name:/tmp/test-bin"
docker start -a "$name"
assert_eq 0 "$(docker inspect --format '{{.State.ExitCode}}' "$name")" 'snapshot must be writable by UID 1001'
assert_eq original "$(cat "$work/subdir/existing.txt")" 'host input unchanged'
[[ ! -e "$work/subdir/new.txt" ]] || die 'snapshot wrote into host'
# Bind mount must retain host ownership and permissions (read-only mount also
# makes an accidental chown fail, so this catches regressions in the entrypoint).
bind_name="${name}-bind"
on_exit "docker rm -f '$bind_name'"
docker run --name "$bind_name" --network none -e AIRUN_WORKSPACE_MODE=bind \
  -e PATH=/tmp/test-bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  --mount "type=bind,source=$entrypoint,target=/tmp/test-entrypoint.sh,readonly" \
  --mount "type=bind,source=$work/test-bin,target=/tmp/test-bin,readonly" \
  --mount "type=bind,source=$work,target=/workspace,readonly" \
  --entrypoint /bin/bash agent-runtime:latest /tmp/test-entrypoint.sh \
  sh -c 'test "$(cat /workspace/subdir/existing.txt)" = original'

metadata_after=$(python3 - "$work/subdir" "$work/subdir/existing.txt" <<'PYMETA'
import os,sys
print([(os.stat(p).st_uid,os.stat(p).st_gid,os.stat(p).st_mode) for p in sys.argv[1:]])
PYMETA
)
assert_eq "$metadata_before" "$metadata_after" 'host owners and permissions unchanged'
