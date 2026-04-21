#!/usr/bin/env bash
# env.sh — source test environment (airun config.env) into the current shell.
#
# Resolution order:
#   1. $AIRUN_TEST_ENV (explicit override, usually pointed at a throwaway)
#   2. ~/.airun/config.env  (the user's real prod config — honour GLM-5.1-only)
#
# Source with: `source "${E2E_LIB}/env.sh"`.
# Exports every KEY=VAL from the resolved file into the current environment,
# then exposes these helpers:
#
#   AIRUN_BIN         path to the airun binary (env or ./bin/airun)
#   AIRUN_CONFIG_ENV  the file that was loaded (empty if nothing found)
#   AIRUN_REPO_ROOT   absolute path of the repo checkout

set -euo pipefail

AIRUN_REPO_ROOT=${AIRUN_REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)}
export AIRUN_REPO_ROOT

AIRUN_BIN=${AIRUN_BIN:-"${AIRUN_REPO_ROOT}/bin/airun"}
export AIRUN_BIN

__airun_env_candidate=${AIRUN_TEST_ENV:-"${HOME}/.airun/config.env"}
if [[ -r "${__airun_env_candidate}" ]]; then
    AIRUN_CONFIG_ENV="${__airun_env_candidate}"
    # Load KEY=VAL lines; skip comments and blanks. `set -a` makes every
    # assignment export automatically.
    set -a
    # shellcheck disable=SC1090
    source "${AIRUN_CONFIG_ENV}"
    set +a
else
    AIRUN_CONFIG_ENV=""
fi
export AIRUN_CONFIG_ENV
unset __airun_env_candidate
