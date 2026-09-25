---
name: linux-sysops
description: Linux system operations, hardware diagnostics, and environment inspection skill
triggers:
  - sysinfo
  - hardware
  - linux-ops
---

# Linux SysOps Skill

This skill provides system operations, hardware inspection, and resource auditing tools for linux-agent.

## Available Helper Scripts

- `scripts/sysinfo.sh`: Displays OS distribution, kernel, uptime, CPU, memory, and disk usage.

## Usage with linux-agent

You can execute the helper script using the `run_skill_script` tool:
```json
{
  "skill": "linux-sysops",
  "script": "sysinfo.sh"
}
```
