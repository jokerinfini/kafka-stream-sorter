#!/usr/bin/env bash
set -eu
# Enable pipefail only when supported (some shells on Windows lack it)
if [ -n "${BASH_VERSION:-}" ]; then
  set -o pipefail
fi

# Ensure we run from repo root regardless of invocation path
cd "$(dirname "$0")/.."

# Build the Docker image for the pipeline app at the repository root.
# The tag "core-infra-project:latest" will be used by docker-compose.
docker build -t core-infra-project:latest .

echo ""
echo "Build completed successfully!"
# Pause so the window doesn't close immediately when run via double-click
read -p "Press Enter to exit" || true


