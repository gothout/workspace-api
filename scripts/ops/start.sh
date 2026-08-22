#!/usr/bin/env bash
set -euo pipefail

APP="workspace-api"
PIDFILE="/var/run/${APP}.pid"
LOGFILE="/var/log/${APP}/${APP}.log"
BIN="/usr/local/bin/${APP}"
CONFIG="/etc/${APP}/configs.json"

if [[ -n "${1:-}" ]]; then
  BIN="$1"
fi

if [[ ! -x "$BIN" ]]; then
  echo "[ERRO] Binário não encontrado ou não executável: $BIN" >&2
  echo "       Rode primeiro: go build -o $BIN ./cmd/server" >&2
  exit 1
fi

if command -v systemctl &>/dev/null && systemctl list-unit-files "${APP}.service" &>/dev/null; then
  echo "[INFO] Iniciando via systemd..."
  sudo systemctl start "${APP}.service"
  echo "[OK]   Serviço iniciado. Logs: scripts/ops/logs.sh"
  exit 0
fi

# Fallback: nohup
mkdir -p "$(dirname "$LOGFILE")"
if [[ -f "$PIDFILE" ]] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
  echo "[AVISO] Já existe um processo rodando (PID $(cat "$PIDFILE"))." >&2
  echo "        Para reiniciar: ./scripts/ops/stop.sh && ./scripts/ops/start.sh" >&2
  exit 1
fi

echo "[INFO] Iniciando $BIN (fallback nohup)..."
nohup "$BIN" serve --config "$CONFIG" >>"$LOGFILE" 2>&1 &
echo $! >"$PIDFILE"
echo "[OK]   PID $(cat "$PIDFILE") salvo em $PIDFILE"
echo "[INFO] Logs em tempo real: ./scripts/ops/logs.sh"
