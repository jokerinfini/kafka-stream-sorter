#!/usr/bin/env bash
set -eu
# Enable pipefail only when running under Bash (not all shells support it)
if [ -n "${BASH_VERSION:-}" ]; then
  set -o pipefail
fi

docker-compose down -v


