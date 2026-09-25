#!/bin/bash
set -e

echo "=== Linux System Information ==="
echo "OS / Kernel: $(uname -srmo)"
if [ -f /etc/os-release ]; then
  . /etc/os-release
  echo "Distro:      $PRETTY_NAME"
fi
echo "Uptime:     $(uptime -p 2>/dev/null || uptime)"
echo "Architecture: $(uname -m)"
echo ""
echo "=== CPU & Memory ==="
echo "CPU Cores:   $(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo 'Unknown')"
free -h 2>/dev/null || cat /proc/meminfo | head -n 4
echo ""
echo "=== Disk Usage ==="
df -h /
