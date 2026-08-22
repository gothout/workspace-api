#!/bin/bash
# Sobe o ralph loop em background (opencode, modelo Ox Alpha por padrão)
# Uso: ./start.sh [max_iterations]   (default 50)
#      MODEL=outro/modelo ./start.sh  para trocar o modelo
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PID_FILE="$SCRIPT_DIR/.ralph.pid"
LOG_DIR="$SCRIPT_DIR/logs"
LATEST_LOG="$LOG_DIR/latest.log"

if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
  echo "Ralph já está rodando (PID $(cat "$PID_FILE"))."
  echo "Use ./view.sh para acompanhar ou ./stop.sh para parar."
  exit 1
fi

mkdir -p "$LOG_DIR"
LOG_FILE="$LOG_DIR/ralph-$(date +%Y%m%d-%H%M%S).log"
ln -sf "$LOG_FILE" "$LATEST_LOG"

cd "$SCRIPT_DIR/../.."
setsid nohup bash "$SCRIPT_DIR/run-loop.sh" "$@" >> "$LOG_FILE" 2>&1 &
PID=$!
echo "$PID" > "$PID_FILE"

echo "Ralph iniciado em background (PID $PID)"
echo "Log: $LOG_FILE"
echo "Acompanhe com: $SCRIPT_DIR/view.sh"
