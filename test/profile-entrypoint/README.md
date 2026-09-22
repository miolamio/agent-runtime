Run `bash test/profile-entrypoint/run.sh` after building the current Docker sources
as `agent-runtime:profiles-dev`, or set `AIRUN_TEST_IMAGE` to an isolated test tag.

This test invokes the production image entrypoint as root with fresh root-owned
cache/state volumes. It verifies the ownership transition, non-root execution,
the selected reviewer's actual model system prompt, saved native transcripts,
and image permission defaults after fresh no-profile startup.

Claude, the adapter, baseline plugins and entrypoint are real. Only acquisition
of one cached catalog agent and the local model response are fixtures. Network
access is disabled; two test containers share a loopback-only network namespace.
No real credentials or model services are used. All containers and volumes have
unique names and are removed on completion.
