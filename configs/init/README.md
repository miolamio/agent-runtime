# Container Init Manifest

Place `init.yaml` at `~/automatica-init/init.yaml` and mount it into the container.

The init script (`scripts/init-container.sh`) runs as a clawker post-init script and processes this manifest to:
- Install npm packages (`npm_packages`)
- Clone skill repos into `/home/user/.claude/skills/` (`skill_repos`)
- Install Python packages (`pip_packages`)
