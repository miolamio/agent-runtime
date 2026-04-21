#!/usr/bin/env bash
# `airun proxy init` creates ~/.airun/proxy.yaml and ~/.airun/students.json
# with 0600 permissions, seeds the yaml with a commented template, and makes
# students.json an empty JSON array.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"

HOME="$th" "$AIRUN_BIN" proxy init >/dev/null

cfg="$th/.airun/proxy.yaml"
users="$th/.airun/students.json"
assert_file_exists "$cfg"   "proxy.yaml created"
assert_file_exists "$users" "students.json created"

# Stat-mode format differs macOS/Linux — try BSD first.
mode=$(stat -f '%Lp' "$cfg" 2>/dev/null || stat -c '%a' "$cfg")
assert_eq "600" "$mode" "proxy.yaml permissions"
mode=$(stat -f '%Lp' "$users" 2>/dev/null || stat -c '%a' "$users")
assert_eq "600" "$mode" "students.json permissions"

yaml_content=$(cat "$cfg")
assert_contains "$yaml_content" "listen: \"127.0.0.1:8080\"" "default listen addr"
assert_contains "$yaml_content" "rpm: 0"                    "rpm default"
users_content=$(cat "$users")
assert_contains "$users_content" "[]" "students.json starts empty"
