#!/usr/bin/env bash
# A profile listing `skills: [foo, bar]` mounts each skill directory under
# /home/claude/.claude/skills/<name> read-only. Non-existent skill names are
# silently skipped (SkillPaths filters via os.Stat).
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"
source "${E2E_LIB}/home.sh"

tag=$(unique_test_tag)
th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

# Prepare two skills; only the present one should be mounted.
mkdir -p "$th/.airun/skills/foo" "$th/.airun/skills/foo-body"
echo "content" > "$th/.airun/skills/foo/SKILL.md"

mk_test_profile "$th" "$tag" "skills:
  - foo
  - ghost-skill-does-not-exist"

PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" -p "$tag" "ping" >/dev/null 2>&1 || true

log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" "${th}/.airun/skills/foo:/home/claude/.claude/skills/foo:ro" \
    "existing skill is bind-mounted read-only"
assert_not_contains "$log" "ghost-skill-does-not-exist" \
    "missing skill is skipped without error"
