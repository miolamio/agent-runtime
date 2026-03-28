# Agent Runtime — Docker image for Claude Code agents
# Build: docker build -t agent-runtime:latest docker/
# Usage: airun "prompt" | airun shell

FROM buildpack-deps:bookworm-scm

ARG USERNAME=claude
ARG GIT_DELTA_VERSION=0.18.2
ARG TZ=UTC
# Pass --build-arg CLAUDE_BUST_CACHE=$(date +%s) to force Claude Code reinstall
ARG CLAUDE_BUST_CACHE=1

ENV TERM=xterm-256color \
    COLORTERM=truecolor \
    LANG=en_US.UTF-8 \
    CLAUDE_CONFIG_DIR=/home/${USERNAME}/.claude \
    PATH=/home/${USERNAME}/.local/bin:/usr/local/bin:$PATH

# ── System packages ──
RUN --mount=type=cache,target=/var/lib/apt,sharing=locked \
    --mount=type=cache,target=/var/cache/apt,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends \
    ripgrep fd-find jq gosu locales zsh fzf \
    && locale-gen en_US.UTF-8

# ── Non-root user ──
RUN groupadd --gid 1001 ${USERNAME} \
    && useradd --uid 1001 --gid ${USERNAME} -m -s /bin/zsh ${USERNAME} \
    && mkdir -p /home/${USERNAME}/.claude /home/${USERNAME}/.local/bin /workspace \
    && chown -R ${USERNAME}:${USERNAME} /home/${USERNAME} /workspace

# ── git-delta (nice diffs) ──
RUN ARCH=$(dpkg --print-architecture) \
    && DEB="git-delta_${GIT_DELTA_VERSION}_${ARCH}.deb" \
    && wget -q "https://github.com/dandavison/delta/releases/download/${GIT_DELTA_VERSION}/${DEB}" \
    && dpkg -i "${DEB}" && rm "${DEB}"

# ── Oh-My-Zsh (for interactive shell) ──
RUN sh -c "$(wget -O- https://github.com/deluan/zsh-in-docker/releases/download/v1.2.0/zsh-in-docker.sh)" -- \
    -t default -p git -p fzf

# ── Claude Code CLI (install as claude user so it lands in /home/claude/.local/bin) ──
# Use CLAUDE_BUST_CACHE build-arg to force reinstall: --build-arg CLAUDE_BUST_CACHE=$(date +%s)
USER ${USERNAME}
RUN echo "cache-bust: ${CLAUDE_BUST_CACHE}" && curl -fsSL "https://claude.ai/install.sh" | bash
USER root

# ── SSH known hosts (fetched at build time) ──
RUN ssh-keyscan -t ed25519,ecdsa github.com gitlab.com bitbucket.org \
    > /etc/ssh/ssh_known_hosts 2>/dev/null

# ── Default config ──
COPY --chown=${USERNAME}:${USERNAME} settings.json /home/${USERNAME}/.claude-init/settings.json
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

WORKDIR /workspace
ENTRYPOINT ["entrypoint.sh"]
CMD ["claude"]
