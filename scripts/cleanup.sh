#!/usr/bin/env bash
set -eu
# Enable pipefail only when running under Bash (not all shells support it)
if [ -n "${BASH_VERSION:-}" ]; then
  set -o pipefail
fi

docker-compose down -v

echo ""
echo "Cleanup completed successfully!"
# Pause so the window doesn't close immediately when run via double-click
read -p "Press Enter to exit" || true


