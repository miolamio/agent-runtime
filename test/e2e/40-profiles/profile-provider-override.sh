#!/usr/bin/env bash
# A profile with `provider: kimi` must override the zai default from
# config.env when the user doesn't pass --provider on the CLI.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"
source "${E2E_LIB}/home.sh"

tag=$(unique_test_tag)
th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

# Add a Kimi key so ContainerEnvWithModel emits KIMI_* env vars.
cat >> "$th/.airun/config.env" <<EOF
KIMI_API_KEY=e2e-kimi-key
KIMI_BASE_URL=http://127.0.0.1:1
KIMI_MODEL=kimi-k2.5
EOF

mk_test_profile "$th" "$tag" "provider: kimi"

out=$(PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" -p "$tag" "ping" 2>&1 || true)

# The airun banner reports the provider it actually resolved.
assert_contains "$out" "provider=kimi" "airun banner reflects profile provider override"

# The env-file airun builds contains the provider-specific keys; we can't read
# it directly (short-lived) but the docker log should at least show that the
# Kimi model env var flows through. The env-file approach hides key names
# inside the file, so fall back to the banner + absence of zai in the args.
log=$(cat "$DOCKER_SHIM_LOG")
assert_not_contains "$log" "provider=zai" "zai should not be advertised"
