#!/usr/bin/env bash
# docker.sh — Docker helpers for e2e tests. Source after harness.sh.
set -euo pipefail

# docker_available: 0 if the daemon responds to `docker info`, non-zero otherwise.
docker_available() {
    docker info >/dev/null 2>&1
}

# image_exists <tag>: 0 if the image tag is locally present.
image_exists() {
    local tag="$1"
    docker image inspect "$tag" >/dev/null 2>&1
}

# wait_port <host> <port> <timeout_s>: poll until the TCP port accepts
# connections or the timeout elapses. Returns non-zero on timeout.
wait_port() {
    local host="$1" port="$2" timeout="${3:-30}"
    local deadline=$(( $(date +%s) + timeout ))
    while (( $(date +%s) < deadline )); do
        if (exec 3<>"/dev/tcp/${host}/${port}") 2>/dev/null; then
            exec 3<&- 3>&-
            return 0
        fi
        sleep 0.2
    done
    return 1
}

# inspect_env <container> <var_name>: print the value of a docker-inspected env
# var for a running or exited container; prints nothing if missing.
inspect_env() {
    local name="$1" var="$2"
    docker inspect -f "{{range .Config.Env}}{{println .}}{{end}}" "$name" 2>/dev/null \
        | awk -F= -v key="$var" '$1==key { $1=""; sub(/^=/,""); print; exit }'
}

# cleanup_by_prefix <name-prefix>: docker rm -f every container whose name
# starts with the prefix. Useful in trap handlers to not leave stragglers.
cleanup_by_prefix() {
    local prefix="$1"
    local ids
    ids=$(docker ps -aq --filter "name=^${prefix}" 2>/dev/null || true)
    if [[ -n "$ids" ]]; then
        # shellcheck disable=SC2086
        docker rm -f $ids >/dev/null 2>&1 || true
    fi
}

# cleanup_volume <name>: remove a docker volume, swallowing "not found".
cleanup_volume() {
    local name="$1"
    docker volume rm "$name" >/dev/null 2>&1 || true
}
