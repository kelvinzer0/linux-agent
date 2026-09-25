#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "❌ Error: This installer must be run as root or via sudo." >&2
  exit 1
fi

export PATH="/usr/local/go/bin:/usr/local/bin:${HOME:-/root}/go/bin:${PATH:-/usr/bin:/bin}"

BIN_NAME="linux-agent"
INSTALL_BIN="/usr/local/bin/${BIN_NAME}"
CONFIG_DIR="/etc/${BIN_NAME}"
CONFIG_FILE="${CONFIG_DIR}/${BIN_NAME}.env"
SYSTEMD_UNIT="/etc/systemd/system/${BIN_NAME}.service"
SYSTEMD_UPDATE_UNIT="/etc/systemd/system/${BIN_NAME}-update.service"
SYSTEMD_UPDATE_TIMER="/etc/systemd/system/${BIN_NAME}-update.timer"
INITD_SCRIPT="/etc/init.d/${BIN_NAME}"
REPO="kelvinzer0/linux-agent"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo "")"

# Parse CLI flags (e.g. ./install.sh --room my-room --bridge https://...)
CUSTOM_ROOM=""
CUSTOM_BRIDGE=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --room)
      CUSTOM_ROOM="$2"
      shift 2
      ;;
    --bridge)
      CUSTOM_BRIDGE="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

echo "=================================================="
echo "         🚀 Installing linux-agent daemon         "
echo "=================================================="

# 1. Architecture Detection
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64|amd64)
    GOARCH="amd64"
    ;;
  aarch64|arm64)
    GOARCH="arm64"
    ;;
  armv7*|armhf)
    GOARCH="arm"
    ;;
  *)
    echo "⚠️ Unsupported or unknown architecture: ${ARCH}. Will attempt compile."
    GOARCH="${ARCH}"
    ;;
esac

# 2. Acquire Binary
INSTALLED=false

# Check local build first
if [ -n "${SCRIPT_DIR}" ] && [ -f "${SCRIPT_DIR}/bin/${BIN_NAME}" ]; then
  echo "📦 Installing from local build: ${SCRIPT_DIR}/bin/${BIN_NAME}"
  cp "${SCRIPT_DIR}/bin/${BIN_NAME}" "${INSTALL_BIN}"
  INSTALLED=true
elif [ -n "${SCRIPT_DIR}" ] && [ -f "${SCRIPT_DIR}/${BIN_NAME}" ]; then
  echo "📦 Installing from local binary: ${SCRIPT_DIR}/${BIN_NAME}"
  cp "${SCRIPT_DIR}/${BIN_NAME}" "${INSTALL_BIN}"
  INSTALLED=true
fi

# If not local, try downloading release from GitHub
if [ "${INSTALLED}" = "false" ]; then
  DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/linux-agent-linux-${GOARCH}"
  echo "⬇️ Downloading prebuilt binary (${GOARCH}) from GitHub..."
  
  TMP_DL="${INSTALL_BIN}.tmp.$$"
  if curl -fSL --retry 3 --connect-timeout 10 -o "${TMP_DL}" "${DOWNLOAD_URL}" 2>/dev/null && [ -s "${TMP_DL}" ]; then
    mv -f "${TMP_DL}" "${INSTALL_BIN}"
    echo "✅ Download completed."
    INSTALLED=true
  else
    echo "ℹ️ Direct download failed. Querying GitHub API for latest asset URL..."
    API_URL="https://api.github.com/repos/${REPO}/releases/latest"
    ASSET_URL="$(curl -fsSL "${API_URL}" 2>/dev/null | grep -i "browser_download_url.*linux-${GOARCH}" | head -n 1 | cut -d '"' -f 4 || true)"
    if [ -n "${ASSET_URL}" ]; then
      if curl -fSL --retry 3 --connect-timeout 10 -o "${TMP_DL}" "${ASSET_URL}" && [ -s "${TMP_DL}" ]; then
        mv -f "${TMP_DL}" "${INSTALL_BIN}"
        echo "✅ Download completed via API asset URL."
        INSTALLED=true
      fi
    fi
  fi
  rm -f "${TMP_DL}" 2>/dev/null || true
fi

# Fallback: Compile from source if Go is installed
if [ "${INSTALLED}" = "false" ]; then
  if command -v go >/dev/null 2>&1; then
    echo "🔨 Go compiler detected. Building from source..."
    TMP_BUILD_DIR="$(mktemp -d)"
    trap 'rm -rf "${TMP_BUILD_DIR}"' EXIT

    if [ -n "${SCRIPT_DIR}" ] && [ -f "${SCRIPT_DIR}/go.mod" ]; then
      SRC_DIR="${SCRIPT_DIR}"
    else
      echo "📥 Cloning latest source from GitHub..."
      git clone --depth 1 "https://github.com/${REPO}.git" "${TMP_BUILD_DIR}"
      SRC_DIR="${TMP_BUILD_DIR}"
    fi

    (cd "${SRC_DIR}" && go build -trimpath -ldflags="-s -w" -o "${INSTALL_BIN}" ./cmd/linux-agent)
    INSTALLED=true
  else
    echo "❌ Error: Could not acquire binary and Go compiler is not installed." >&2
    echo "Please install Go (1.22+) or download a release binary manually." >&2
    exit 1
  fi
fi

chmod 755 "${INSTALL_BIN}"
echo "✅ Installed binary to ${INSTALL_BIN}"

# 3. Setup Configuration
mkdir -p "${CONFIG_DIR}"
if [ ! -f "${CONFIG_FILE}" ]; then
  BRIDGE_VAL="${CUSTOM_BRIDGE:-https://public-mcp-bridge.warunglakku.com}"
  cat << EOF > "${CONFIG_FILE}"
# MCP Bridge URL
MCP_BRIDGE_URL=${BRIDGE_VAL}

# Permanent room ID (leave empty to let bridge allocate a new room automatically)
MCP_ROOM=${CUSTOM_ROOM}

# Automatic background updates (daily check via GitHub Releases)
AUTO_UPDATE=true
EOF
  chmod 600 "${CONFIG_FILE}"
  echo "✅ Created configuration file: ${CONFIG_FILE}"
else
  if [ -n "${CUSTOM_ROOM}" ]; then
    sed -i "s/^MCP_ROOM=.*/MCP_ROOM=${CUSTOM_ROOM}/" "${CONFIG_FILE}"
  fi
  if [ -n "${CUSTOM_BRIDGE}" ]; then
    sed -i "s|^MCP_BRIDGE_URL=.*|MCP_BRIDGE_URL=${CUSTOM_BRIDGE}|" "${CONFIG_FILE}"
  fi
  echo "ℹ️ Configuration file already exists at ${CONFIG_FILE} (preserved)."
fi

# 4. Create SysVinit / Service fallback (/etc/init.d/linux-agent)
cat << 'EOF' > "${INITD_SCRIPT}"
#!/bin/sh
### BEGIN INIT INFO
# Provides:          linux-agent
# Required-Start:    $network $remote_fs $local_fs
# Required-Stop:     $network $remote_fs $local_fs
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: linux-agent MCP daemon
### END INIT INFO

PIDFILE="/var/run/linux-agent.pid"
CONFIG="/etc/linux-agent/linux-agent.env"
DAEMON="/usr/local/bin/linux-agent"

[ -f "$CONFIG" ] && . "$CONFIG"

case "$1" in
  start)
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
      echo "linux-agent is already running."
      exit 0
    fi
    echo "Starting linux-agent..."
    start-stop-daemon --start --background --make-pidfile --pidfile "$PIDFILE" \
      --exec "$DAEMON" -- -bridge "$MCP_BRIDGE_URL" -room "$MCP_ROOM"
    ;;
  stop)
    echo "Stopping linux-agent..."
    start-stop-daemon --stop --pidfile "$PIDFILE" --retry 5 2>/dev/null || true
    rm -f "$PIDFILE"
    ;;
  restart)
    $0 stop
    sleep 1
    $0 start
    ;;
  status)
    "$DAEMON" -status
    ;;
  update)
    "$DAEMON" -update
    ;;
  *)
    echo "Usage: $0 {start|stop|restart|status|update}"
    exit 1
    ;;
esac
exit 0
EOF
chmod +x "${INITD_SCRIPT}"

# 5. Setup Service via Systemd or Init
is_systemd() {
  if [ -d /run/systemd/system ] || [ "$(ps -p 1 -o comm= 2>/dev/null)" = "systemd" ]; then
    return 0
  fi
  return 1
}

if is_systemd && command -v systemctl >/dev/null 2>&1; then
  echo "⚙️ Configuring systemd service..."
  
  cat << 'EOF' > "${SYSTEMD_UNIT}"
[Unit]
Description=linux-agent - MCP Linux Daemon (Code Interpreter & OpenCode Tools)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
EnvironmentFile=-/etc/linux-agent/linux-agent.env
ExecStart=/usr/local/bin/linux-agent
Restart=always
RestartSec=5s
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

  cat << 'EOF' > "${SYSTEMD_UPDATE_UNIT}"
[Unit]
Description=Daily update check for linux-agent
After=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/linux-agent -update
EOF

  cat << 'EOF' > "${SYSTEMD_UPDATE_TIMER}"
[Unit]
Description=Daily auto-update timer for linux-agent

[Timer]
OnCalendar=daily
RandomizedDelaySec=1h
Persistent=true

[Install]
WantedBy=timers.target
EOF

  systemctl daemon-reload
  systemctl enable "${BIN_NAME}.service"
  systemctl restart "${BIN_NAME}.service"
  systemctl enable "${BIN_NAME}-update.timer" 2>/dev/null || true
  systemctl start "${BIN_NAME}-update.timer" 2>/dev/null || true
  echo "✅ Systemd service & auto-update timer active."
else
  echo "ℹ️ Systemd not detected or inactive. Configuring via init.d..."
  if command -v update-rc.d >/dev/null 2>&1; then
    update-rc.d "${BIN_NAME}" defaults 2>/dev/null || true
  fi
  "${INITD_SCRIPT}" restart || true
fi

echo ""
echo "=================================================="
echo "   🎉 linux-agent installation completed!        "
echo "=================================================="
echo "Service commands:"
echo "  systemctl start linux-agent"
echo "  systemctl stop linux-agent"
echo "  systemctl restart linux-agent"
echo "  systemctl status linux-agent"
echo ""
echo "Check MCP URL anytime:"
echo "  linux-agent status           # View live MCP URL & connection status"
echo "  linux-agent -update          # Manual self-update"
echo ""
echo "Config file: ${CONFIG_FILE}"
echo "=================================================="

# Check and print immediate status
sleep 1
"${INSTALL_BIN}" status 2>/dev/null || true
