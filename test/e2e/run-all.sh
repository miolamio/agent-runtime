#!/usr/bin/env bash
# run-all.sh — discover and execute every *.sh test under test/e2e/<NN>-*/,
# render a TAP summary to stdout, and persist per-test logs to .logs/.
#
# Each test is a self-contained bash script (exits 0/77/other).
# Groups are ordered by the leading numeric prefix of the directory name.
#
# Usage:
#   test/e2e/run-all.sh                     # run everything not network-gated
#   test/e2e/run-all.sh --group 00-prereq   # only that group
#   test/e2e/run-all.sh --only 10-cli/help  # single test file substring match
#   test/e2e/run-all.sh --with-network      # also run #REQUIRES_NETWORK tests
#   test/e2e/run-all.sh --include-non-glm   # also run non-GLM provider tests
#   test/e2e/run-all.sh --fast              # bail out on first failure

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
LOG_DIR="${ROOT}/.logs"
export E2E_ROOT="$ROOT"
export E2E_LIB="${ROOT}/lib"

# Parse flags.
GROUP=""
ONLY=""
WITH_NETWORK=0
INCLUDE_NON_GLM=0
FAST=0
while (( $# > 0 )); do
    case "$1" in
        --group) GROUP="$2"; shift 2 ;;
        --only) ONLY="$2"; shift 2 ;;
        --with-network) WITH_NETWORK=1; shift ;;
        --include-non-glm) INCLUDE_NON_GLM=1; shift ;;
        --fast) FAST=1; shift ;;
        -h|--help)
            sed -n '2,/^set -euo/p' "${BASH_SOURCE[0]}" | sed -n '/^# /p' | sed 's/^# //'
            exit 0
            ;;
        *) echo "unknown flag: $1" >&2; exit 2 ;;
    esac
done

export E2E_WITH_NETWORK="$WITH_NETWORK"
export E2E_INCLUDE_NON_GLM="$INCLUDE_NON_GLM"

mkdir -p "$LOG_DIR"
: > "${LOG_DIR}/summary.tap"

# Collect tests. Group dirs are NN-name/ under $ROOT.
mapfile -t GROUP_DIRS < <(find "$ROOT" -mindepth 1 -maxdepth 1 -type d \
    -name '[0-9][0-9]-*' | sort)

declare -a TESTS=()
for gdir in "${GROUP_DIRS[@]}"; do
    gname=$(basename "$gdir")
    if [[ -n "$GROUP" && "$GROUP" != "$gname" ]]; then
        continue
    fi
    while IFS= read -r -d '' t; do
        rel="${gname}/$(basename "$t")"
        if [[ -n "$ONLY" && "$rel" != *"$ONLY"* ]]; then
            continue
        fi
        TESTS+=("$t")
    done < <(find "$gdir" -maxdepth 1 -type f -name '*.sh' -print0 | sort -z)
done

TOTAL=${#TESTS[@]}
if (( TOTAL == 0 )); then
    echo "1..0 # no tests matched"
    exit 0
fi

echo "1..${TOTAL}"

PASS=0 FAIL=0 SKIP=0 IDX=0
for t in "${TESTS[@]}"; do
    IDX=$(( IDX + 1 ))
    gname=$(basename "$(dirname "$t")")
    tname=$(basename "$t" .sh)
    mkdir -p "${LOG_DIR}/${gname}"
    log="${LOG_DIR}/${gname}/${tname}.log"

    # Run in a subshell so `set -e` inside the test can't kill the orchestrator,
    # and capture exit code.
    set +e
    bash "$t" > "$log" 2>&1
    ec=$?
    set -e

    case "$ec" in
        0)
            printf 'ok %d - %s/%s\n' "$IDX" "$gname" "$tname" | tee -a "${LOG_DIR}/summary.tap"
            PASS=$(( PASS + 1 ))
            ;;
        77)
            reason=$(grep -m1 'SKIP:' "$log" | sed 's/.*SKIP: //' || true)
            printf 'ok %d - %s/%s # SKIP %s\n' "$IDX" "$gname" "$tname" "${reason:-no reason given}" \
                | tee -a "${LOG_DIR}/summary.tap"
            SKIP=$(( SKIP + 1 ))
            ;;
        *)
            printf 'not ok %d - %s/%s (exit %d, log: %s)\n' \
                "$IDX" "$gname" "$tname" "$ec" "${log#${ROOT}/}" \
                | tee -a "${LOG_DIR}/summary.tap"
            FAIL=$(( FAIL + 1 ))
            # Surface the tail of the log so CI output is useful on failure.
            printf '  ---\n'
            sed 's/^/  /' "$log" | tail -n 20
            printf '  ---\n'
            if (( FAST )); then
                echo "# --fast: bailing after first failure" | tee -a "${LOG_DIR}/summary.tap"
                break
            fi
            ;;
    esac
done

printf '# pass=%d fail=%d skip=%d total=%d\n' "$PASS" "$FAIL" "$SKIP" "$TOTAL" \
    | tee -a "${LOG_DIR}/summary.tap"

(( FAIL == 0 ))
