#!/usr/bin/env bash
set -euo pipefail

APP="workspace-api"
PIDFILE="/var/run/${APP}.pid"

if command -v systemctl &>/dev/null && systemctl list-unit-files "${APP}.service" &>/dev/null; then
  echo "[INFO] Parando via systemd..."
  sudo systemctl stop "${APP}.service"
  echo "[OK]   Serviço parado."
  exit 0
fi

# Fallback: pidfile
if [[ ! -f "$PIDFILE" ]]; then
  echo "[AVISO] Nenhum pidfile encontrado em $PIDFILE." >&2
  exit 0
fi

PID="$(cat "$PIDFILE")"
if kill -0 "$PID" 2>/dev/null; then
  echo "[INFO] Enviando SIGTERM para PID $PID..."
  kill "$PID"
  for _ in {1..30}; do
    if ! kill -0 "$PID" 2>/dev/null; then
      echo "[OK]   Processo $PID encerrado."
      rm -f "$PIDFILE"
      exit 0
    fi
    sleep 1
  done
  echo "[ERRO] Processo não encerrou a tempo; enviando SIGKILL." >&2
  kill -9 "$PID" || true
fi

rm -f "$PIDFILE"
echo "[OK]   Parado."
