# linux-agent

Self-hosted Model Context Protocol (MCP) agent daemon for Linux environments, written in Go.

`linux-agent` connects to an MCP bridge (such as [mcp-bridge-go](https://github.com/kelvinzer0/mcp-bridge-go) deployed at `https://public-mcp-bridge.warunglakku.com`) or operates locally over standard I/O (`stdio`). It equips AI agents with a secure Linux Code Interpreter, the full suite of OpenCode native tooling (LSP, patch, diff, shell, file operations), and full `.agents/skills` compatibility.

---

## Architecture

```
┌────────────────────────────────┐         WebSocket            ┌──────────────────────┐         Streamable HTTP/SSE        ┌──────────────┐
│          linux-agent           │ ───────────────────────────> │  mcp-bridge-go       │ ─────────────────────────────────> │  AI Client   │
│  • Code Interpreter (Python/   │   registerTools              │  (Public Bridge)     │   /mcp?room=<room_id>            │  (Claude,    │
│    Bash/Node/Go)               │                              │                      │                                  │   Cursor,    │
│  • OpenCode Tools (LSP, patch, │ <─────────────────────────── │                      │ <───────────────────────────────── │  Antigravity,│
│    diff, grep, glob, view, ...)│   callTool                   │                      │   JSON-RPC (tools/call)          │   OpenCode)  │
│  • .agents/skills runner       │ ───────────────────────────> │                      │                                  │              │
└────────────────────────────────┘   toolResult                 └──────────────────────┘                                  └──────────────┘
```

---

## Features & MCP Tools

### 1. Code Interpreter & Shell Execution
- **`execute_code`**: Runs Python, Bash, Sh, Node.js, and Go scripts directly in the Linux environment with isolated timeouts, capturing `stdout`, `stderr`, execution time, and exit status.
- **`bash`**: Executes arbitrary shell commands with directory preservation and timeout controls.

### 2. OpenCode Native Tooling
Ported directly from the official [OpenCode](https://github.com/opencode-ai/opencode) architecture:
- **`view`**: Displays file contents with 1-based line numbering and offset/limit pagination, or lists directory items.
- **`write`**: Atomically creates or overwrites files, ensuring parent directories are created automatically.
- **`edit`**: Performs exact, unique string replacement within files.
- **`ls`**: Formatted directory listing displaying file sizes and modification timestamps.
- **`glob`**: Wildcard pattern search across directory trees (e.g. `**/*.go`, `src/**/*.ts`).
- **`grep`**: High-speed regex and literal text searching across project files.
- **`patch`**: Applies OpenCode's atomic multi-file patch format (`*** Begin Patch`, `*** Update File: ...`, `@@ ...`, `*** Add File: ...`, `*** Delete File: ...`, `*** End Patch`).
- **`diff`**: Computes unified diffs between files or arbitrary text buffers.

### 3. LSP (Language Server Protocol)
- **`lsp_diagnostics`**: Analyzes source files (`.go`, `.py`, `.ts`, `.js`, `.sh`, etc.) for syntax errors, compilation issues, and linter warnings with line numbers and severity levels.
- **`lsp_hover`**: Fetches symbol definitions, type signatures, and documentation at a specific line and column.

### 4. `.agents/skills` Compatibility
- **`list_skills`**: Discovers all skills defined in `.agents/skills/`, `.agent/skills/`, and global `~/.gemini/config/skills/`, parsing YAML frontmatter.
- **`read_skill`**: Reads full instructions, triggers, and documentation from `SKILL.md`.
- **`run_skill_script`**: Directly executes automation scripts located in a skill's `scripts/` directory.

---

## Quick Installation via `curl`

Install and activate `linux-agent` daemon as a standard Linux system service in one command:

```bash
curl -fsSL https://raw.githubusercontent.com/kelvinzer0/linux-agent/main/install.sh | sudo bash
```

Or specify a permanent bridge room at install time:
```bash
curl -fsSL https://raw.githubusercontent.com/kelvinzer0/linux-agent/main/install.sh | sudo bash -s -- --room my-linux-box
```

The installer will:
1. Detect architecture (`amd64`, `arm64`, `arm`) and install `/usr/local/bin/linux-agent`.
2. Generate configuration at `/etc/linux-agent/linux-agent.env`.
3. Register and start standard Linux services via `systemd` (or `init.d` fallback).
4. Configure automatic daily background update checks.

---

## Service Management

Manage the daemon using standard Linux service commands:

```bash
systemctl start linux-agent
systemctl stop linux-agent
systemctl restart linux-agent
systemctl status linux-agent
```

On systems without systemd (SysVinit, OpenWrt, Alpine):
```bash
service linux-agent start
service linux-agent stop
service linux-agent restart
service linux-agent status
```

### Automatic Updates

`linux-agent` has native self-updating built-in:
- **Automatic**: Daily checks via systemd timer (`linux-agent-update.timer`) and background daemon loop (`AUTO_UPDATE=true` in `/etc/linux-agent/linux-agent.env`).
- **Manual**: Run anytime to upgrade to the latest GitHub release:
  ```bash
  linux-agent -update
  ```

---

## Building from Source (Manual)

## Usage

### 1. Connecting via MCP Bridge (Recommended)

Connect to a persistent room on your bridge:
```bash
./bin/linux-agent -bridge https://public-mcp-bridge.warunglakku.com -room <your_room_id>
```

Or let `linux-agent` automatically request and allocate a new room from the bridge:
```bash
./bin/linux-agent -bridge https://public-mcp-bridge.warunglakku.com
```

You can also configure via environment variables:
```bash
export MCP_BRIDGE_URL="https://public-mcp-bridge.warunglakku.com"
export MCP_ROOM="my-linux-box"
./bin/linux-agent
```

### 2. Running in Direct Stdio Mode

To use `linux-agent` locally with MCP clients (Cursor, Claude Desktop, Antigravity):
```bash
./bin/linux-agent -stdio
```

Example MCP client configuration:
```json
{
  "mcpServers": {
    "linux-agent": {
      "command": "/path/to/linux-agent",
      "args": ["-stdio"]
    }
  }
}
```

---

## Testing

Run the test suite:
```bash
make test
```

---

## License

MIT
