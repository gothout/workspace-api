#!/usr/bin/env bash
set -euo pipefail

APP="workspace-api"
LOGFILE="/var/log/${APP}/${APP}.log"

if command -v journalctl &>/dev/null && systemctl list-unit-files "${APP}.service" &>/dev/null; then
  echo "[INFO] Exibindo logs do systemd em tempo real (Ctrl+C para sair)..."
  sudo journalctl -u "${APP}.service" -f --no-hostname
  exit 0
fi

# Fallback: tail no arquivo
if [[ ! -f "$LOGFILE" ]]; then
  echo "[AVISO] Arquivo de log não encontrado: $LOGFILE" >&2
  echo "        O serviço já foi iniciado?" >&2
  exit 1
fi

echo "[INFO] Exibindo $LOGFILE em tempo real (Ctrl+C para sair)..."
tail -f "$LOGFILE"
