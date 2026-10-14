#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/go"
go build -o shave ./cmd/shave
echo "built ./go/shave — move to your PATH (e.g. /usr/local/bin/shave)"