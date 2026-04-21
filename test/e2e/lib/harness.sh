#!/usr/bin/env bash
# harness.sh — shared assertions, logging, and teardown helpers.
# Source from every test: `source "${E2E_LIB}/harness.sh"`.
#
# Exit code convention:
#   0   — PASS
#   77  — SKIP (GNU "skip" code, matches automake)
#   any other non-zero — FAIL

set -euo pipefail

# ANSI colors only when stderr is a TTY.
if [[ -t 2 ]]; then
    C_RED=$'\033[31m'
    C_GRN=$'\033[32m'
    C_YLW=$'\033[33m'
    C_DIM=$'\033[2m'
    C_RST=$'\033[0m'
else
    C_RED="" C_GRN="" C_YLW="" C_DIM="" C_RST=""
fi

log_info() { printf '%s[info]%s %s\n' "$C_DIM" "$C_RST" "$*" >&2; }
log_warn() { printf '%s[warn]%s %s\n' "$C_YLW" "$C_RST" "$*" >&2; }
log_err()  { printf '%s[err]%s  %s\n' "$C_RED" "$C_RST" "$*" >&2; }
log_ok()   { printf '%s[ok]%s   %s\n' "$C_GRN" "$C_RST" "$*" >&2; }

# Invoked by failed asserts and uncaught errors; prefix distinguishes it from
# a plain non-zero exit so the runner can render a better message.
die() {
    log_err "$*"
    exit 1
}

skip() {
    log_warn "SKIP: $*"
    exit 77
}

assert_eq() {
    local want="$1" got="$2" msg="${3:-}"
    if [[ "$want" != "$got" ]]; then
        die "assert_eq${msg:+ ($msg)}: want=${want} got=${got}"
    fi
}

assert_contains() {
    local haystack="$1" needle="$2" msg="${3:-}"
    if [[ "$haystack" != *"$needle"* ]]; then
        die "assert_contains${msg:+ ($msg)}: missing substring '${needle}'"
    fi
}

assert_not_contains() {
    local haystack="$1" needle="$2" msg="${3:-}"
    if [[ "$haystack" == *"$needle"* ]]; then
        die "assert_not_contains${msg:+ ($msg)}: unexpected substring '${needle}'"
    fi
}

assert_file_exists() {
    local path="$1" msg="${2:-}"
    if [[ ! -e "$path" ]]; then
        die "assert_file_exists${msg:+ ($msg)}: $path missing"
    fi
}

assert_exit_code() {
    local want="$1" got="$2" msg="${3:-}"
    if [[ "$want" != "$got" ]]; then
        die "assert_exit_code${msg:+ ($msg)}: want=${want} got=${got}"
    fi
}

# Register a cleanup hook — each call adds to a chain run on EXIT.
# Example: on_exit "rm -rf $tmpdir"
__E2E_CLEANUP_CMDS=()
on_exit() {
    __E2E_CLEANUP_CMDS+=("$*")
    # Install trap once; subsequent registrations just append.
    trap '__e2e_run_cleanup' EXIT
}
__e2e_run_cleanup() {
    local cmd
    # Run in reverse order so paired setup/teardown behaves like a stack.
    for (( i=${#__E2E_CLEANUP_CMDS[@]}-1 ; i>=0 ; i-- )); do
        cmd="${__E2E_CLEANUP_CMDS[i]}"
        # Cleanups must never abort the exit path; swallow their errors.
        eval "$cmd" 2>/dev/null || true
    done
}
