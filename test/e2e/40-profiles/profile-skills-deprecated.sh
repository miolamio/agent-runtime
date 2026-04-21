#!/usr/bin/env bash
# v0.7.0 dropped the ~/.airun/skills bind-mount mechanism entirely. A legacy
# profile that still lists `skills: [...]` must:
#   1) emit a deprecation warning on stderr,
#   2) NOT bind-mount anything under /home/claude/.claude/skills/,
#   3) otherwise run normally (the field is ignored, not an error).
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

tag=$(unique_test_tag)
th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

# Seed a skill directory that the OLD mount would have picked up. If the
# runner still honours the skills field we'll see this path in docker args.
mkdir -p "$th/.airun/skills/legacy-skill"
echo "content" > "$th/.airun/skills/legacy-skill/SKILL.md"

mk_test_profile "$th" "$tag" "skills:
  - legacy-skill"

out=$(PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" -p "$tag" "ping" 2>&1 || true)

assert_contains "$out" "deprecated 'skills' field" \
    "deprecation warning surfaces on stderr"

log=$(cat "$DOCKER_SHIM_LOG")
assert_not_contains "$log" "/home/claude/.claude/skills/legacy-skill" \
    "skills directory is NOT bind-mounted anymore"
assert_not_contains "$log" "${th}/.airun/skills/legacy-skill" \
    "legacy skill host path does not reach docker args"
