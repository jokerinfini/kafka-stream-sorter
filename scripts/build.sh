#!/usr/bin/env bash
set -euo pipefail

# Ensure we run from repo root regardless of invocation path
cd "$(dirname "$0")/.."

# Build the Docker image for the pipeline app at the repository root.
# The tag "core-infra-project:latest" will be used by docker-compose.
docker build -t core-infra-project:latest .


