#!/bin/bash
# Acompanha o ralph loop: status + tail do log
# Uso: ./view.sh          status resumido + tail -f do log (Ctrl+C sai sem parar o loop)
#      ./view.sh status   só o resumo, sem tail
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PID_FILE="$SCRIPT_DIR/.ralph.pid"
PRD_FILE="$SCRIPT_DIR/prd.json"
LATEST_LOG="$SCRIPT_DIR/logs/latest.log"

echo "=== Status do Ralph ==="
if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
  echo "Estado:   RODANDO (PID $(cat "$PID_FILE"))"
else
  echo "Estado:   PARADO"
fi

if [ -f "$PRD_FILE" ]; then
  TOTAL=$(jq '.userStories | length' "$PRD_FILE")
  DONE=$(jq '[.userStories[] | select(.passes)] | length' "$PRD_FILE")
  NEXT=$(jq -r '[.userStories[] | select(.passes | not)] | sort_by(.priority) | .[0] | "\(.id) - \(.title)" // empty' "$PRD_FILE")
  echo "Stories:  $DONE/$TOTAL concluídas"
  [ -n "$NEXT" ] && echo "Próxima:  $NEXT"
fi

if [ -f "$LATEST_LOG" ]; then
  LAST_ITER=$(grep -oE "Ralph Iteration [0-9]+ of [0-9]+" "$LATEST_LOG" | tail -1)
  [ -n "$LAST_ITER" ] && echo "Iteração: $LAST_ITER"
  echo "Log:      $(readlink -f "$LATEST_LOG")"
fi

if [ "${1:-}" = "status" ]; then
  exit 0
fi

if [ ! -f "$LATEST_LOG" ]; then
  echo "Nenhum log ainda. Rode ./start.sh primeiro."
  exit 1
fi

echo ""
echo "=== Tail do log (Ctrl+C sai sem parar o loop) ==="
tail -n 40 -f "$LATEST_LOG"
