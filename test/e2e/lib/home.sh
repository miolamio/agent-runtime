#!/usr/bin/env bash
# home.sh — helpers for fully-isolated test HOMEs. The point is to run airun
# against a throwaway ~/.airun/ tree without ever touching the user's real
# config or state volumes.
#
# Source after harness.sh. Needs docker.sh loaded if you use mk_test_home
# with profile volumes.
set -euo pipefail

# mk_test_home: mktemp a directory, seed ~/.airun/config.env with an
# unroutable Z.AI base URL (so claude inside a container fails fast without
# burning any real provider quota), and print the path on stdout.
#
# The caller is expected to:
#   th=$(mk_test_home)
#   on_exit "rm -rf '$th'"
#
# Extra files (profiles, agents, skills) go under "$th/.airun/...".
mk_test_home() {
    local th
    th=$(mktemp -d -t airun-e2e.XXXXXXXX)
    mkdir -p "$th/.airun/profiles" "$th/.airun/agents" "$th/.airun/skills"
    # Port 1 is the TCPMUX reserved port — kernel replies with RST
    # immediately, so any HTTP client fails within milliseconds. We want
    # claude inside the container to exit fast without making real network
    # calls.
    cat > "$th/.airun/config.env" <<'EOF'
ARUN_PROVIDER=zai
ARUN_WORKSPACE=/tmp
ARUN_MODE=bind
ZAI_API_KEY=e2e-placeholder-invalid-key
ZAI_BASE_URL=http://127.0.0.1:1
ZAI_MODEL=glm-5.1
ZAI_HAIKU_MODEL=GLM-4.5-Air
API_TIMEOUT_MS=5000
EOF
    chmod 0600 "$th/.airun/config.env"
    echo "$th"
}

# mk_test_profile <home> <profile_name> [extra_yaml]: write a profile yaml
# into $home/.airun/profiles/$name.yaml. extra_yaml (optional) is appended
# to the base — use this to add skills/plugins/settings/provider blocks.
mk_test_profile() {
    local home="$1" name="$2" extra="${3:-}"
    cat > "$home/.airun/profiles/${name}.yaml" <<EOF
name: ${name}
description: e2e test profile
EOF
    if [[ -n "$extra" ]]; then
        printf '%s\n' "$extra" >> "$home/.airun/profiles/${name}.yaml"
    fi
}

# unique_test_tag: emits a short token suitable for profile and container
# name suffixes — unique per shell invocation.
unique_test_tag() {
    printf 'e2e-%d-%d' "$$" "$RANDOM"
}

# install_docker_shim <home>: drop a fake `docker` binary into $home/bin that
# logs every invocation to $home/docker.log and returns plausible answers.
# Exports DOCKER_SHIM_LOG for the caller.
#
# The shim is the right tool for tests that just want to assert airun's
# argument building without spinning up a real container (real claude runs
# inside the container retry for minutes against an unroutable API — that
# path belongs in --with-network tests). Everything downstream of airun is
# stubbed; upstream (config/profile/runner logic) is real.
#
# Usage:
#   th=$(mk_test_home)
#   install_docker_shim "$th"
#   PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" ...
#   grep -F 'run --rm' "$DOCKER_SHIM_LOG"
install_docker_shim() {
    local home="$1"
    mkdir -p "$home/bin" "$home/captured_mounts"
    DOCKER_SHIM_LOG="$home/docker.log"
    DOCKER_SHIM_CAPTURE="$home/captured_mounts"
    export DOCKER_SHIM_LOG DOCKER_SHIM_CAPTURE
    : > "$DOCKER_SHIM_LOG"

    cat > "$home/bin/docker" <<'SHIM'
#!/usr/bin/env bash
# Fake docker used by airun e2e tests.  Logs every invocation (one command
# per line, null-separated args follow on the same line) and returns plausible
# responses so airun's control flow runs end-to-end.
set -u

log="${DOCKER_SHIM_LOG:-/dev/null}"
# Record as one line: argv joined by spaces, with a newline terminator.
# Tests grep this file; quoting is intentionally simple — we don't need
# round-trippable parsing.
printf '%s\n' "$*" >> "$log"

case "${1:-}" in
    info|version)
        # Prereq checks poke this; emit something non-empty.
        echo "Server Version: 99.0.0-e2eshim"
        exit 0
        ;;
    image)
        # `docker image inspect <tag>` — succeed if tag is agent-runtime:latest
        # so skip_if_no_image passes; fail on other tags so negative-path tests
        # still work.
        if [[ "${2:-}" == "inspect" && "${3:-}" == "agent-runtime:latest" ]]; then
            echo '[{"Id":"sha256:shim","RepoTags":["agent-runtime:latest"]}]'
            exit 0
        fi
        exit 1
        ;;
    volume)
        # `docker volume inspect <name>` succeeds after a simulated mount;
        # `docker volume rm` always succeeds.  We track "mounted" volumes
        # in a sibling file so the `inspect` check reflects the state airun
        # sees after its `docker run -v <name>:<path>` calls.
        vols="${DOCKER_SHIM_LOG%.log}.volumes"
        touch "$vols"
        case "${2:-}" in
            inspect)
                grep -Fxq "${3:-}" "$vols" && exit 0 || exit 1
                ;;
            rm)
                # Remove the named volume from the tracked set (use a tmpfile
                # to avoid in-place editing quirks on BSD awk).
                grep -Fxv "${3:-}" "$vols" > "${vols}.tmp" 2>/dev/null || true
                mv "${vols}.tmp" "$vols"
                echo "${3:-}"
                exit 0
                ;;
            *)
                exit 0
                ;;
        esac
        ;;
    run|create)
        # For each "-v SRC:DST[:ro]" flag:
        #   - Named volume (no leading '/'): remember it so subsequent
        #     `docker volume inspect` calls find it.
        #   - Host bind mount: if SRC is a file that exists RIGHT NOW, copy
        #     it to $DOCKER_SHIM_CAPTURE/<basename of DST>. airun generates
        #     short-lived temp files (plugin scripts, settings.json) that it
        #     deletes on return, so tests have no other way to inspect them.
        vols="${DOCKER_SHIM_LOG%.log}.volumes"
        touch "$vols"
        prev=""
        for arg in "$@"; do
            if [[ "$prev" == "-v" ]]; then
                if [[ "${arg:0:1}" != "/" ]]; then
                    vol="${arg%%:*}"
                    if ! grep -Fxq "$vol" "$vols" 2>/dev/null; then
                        echo "$vol" >> "$vols"
                    fi
                else
                    src="${arg%%:*}"
                    rest="${arg#*:}"
                    dst="${rest%%:*}"
                    if [[ -f "$src" && -n "${DOCKER_SHIM_CAPTURE:-}" ]]; then
                        cp "$src" "${DOCKER_SHIM_CAPTURE}/$(basename "$dst")" 2>/dev/null || true
                    fi
                fi
            fi
            prev="$arg"
        done
        # `docker create` prints the container id on stdout; `docker run` streams
        # the container output. We just emit something plausible and exit 0.
        if [[ "$1" == "create" ]]; then
            echo "shim-container-id"
        fi
        exit 0
        ;;
    start|cp|rm|ps|inspect)
        exit 0
        ;;
    *)
        exit 0
        ;;
esac
SHIM
    chmod +x "$home/bin/docker"
}

