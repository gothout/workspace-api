#!/bin/bash
# Wrapper: sobe o Ralph loop com opencode
# Uso: ./run-loop.sh [max_iterations]   (default 50)
set -e
export PATH="$HOME/.local/bin:/usr/local/go/bin:$HOME/go/bin:$PATH"
cd "$(dirname "$0")/../.."
MAX="${1:-50}"
exec scripts/ralph/ralph.sh --tool opencode "$MAX"
