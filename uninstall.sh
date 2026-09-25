#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "❌ Error: This script must be run as root or via sudo." >&2
  exit 1
fi

BIN_NAME="linux-agent"
INSTALL_BIN="/usr/local/bin/${BIN_NAME}"
CONFIG_DIR="/etc/${BIN_NAME}"
SYSTEMD_UNIT="/etc/systemd/system/${BIN_NAME}.service"
SYSTEMD_UPDATE_UNIT="/etc/systemd/system/${BIN_NAME}-update.service"
SYSTEMD_UPDATE_TIMER="/etc/systemd/system/${BIN_NAME}-update.timer"
INITD_SCRIPT="/etc/init.d/${BIN_NAME}"

echo "Stopping and disabling services..."
if command -v systemctl >/dev/null 2>&1; then
  systemctl stop "${BIN_NAME}.service" 2>/dev/null || true
  systemctl disable "${BIN_NAME}.service" 2>/dev/null || true
  systemctl stop "${BIN_NAME}-update.timer" 2>/dev/null || true
  systemctl disable "${BIN_NAME}-update.timer" 2>/dev/null || true
  rm -f "${SYSTEMD_UNIT}" "${SYSTEMD_UPDATE_UNIT}" "${SYSTEMD_UPDATE_TIMER}"
  systemctl daemon-reload
fi

if [ -f "${INITD_SCRIPT}" ]; then
  "${INITD_SCRIPT}" stop 2>/dev/null || true
  if command -v update-rc.d >/dev/null 2>&1; then
    update-rc.d -f "${BIN_NAME}" remove 2>/dev/null || true
  fi
  rm -f "${INITD_SCRIPT}"
fi

echo "Removing binary..."
rm -f "${INSTALL_BIN}"

read -r -p "Delete configuration directory ${CONFIG_DIR}? [y/N]: " CONFIRM
if [[ "${CONFIRM}" =~ ^[Yy]$ ]]; then
  rm -rf "${CONFIG_DIR}"
  echo "Removed configuration directory."
fi

echo "✅ linux-agent has been completely uninstalled."
