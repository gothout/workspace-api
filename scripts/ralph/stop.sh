#!/bin/bash
# Para o ralph loop em background (mata o grupo de processos inteiro)
# Uso: ./stop.sh
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PID_FILE="$SCRIPT_DIR/.ralph.pid"

if [ ! -f "$PID_FILE" ]; then
  echo "Nenhum ralph rodando (sem $PID_FILE)."
  exit 0
fi

PID="$(cat "$PID_FILE")"
if ! kill -0 "$PID" 2>/dev/null; then
  echo "Processo $PID já não existe. Limpando pid file."
  rm -f "$PID_FILE"
  exit 0
fi

echo "Parando ralph (PID $PID) e filhos..."
# O loop roda em setsid: o PID é o líder do grupo; sinal negativo mata o grupo.
kill -TERM -- -"$PID" 2>/dev/null || kill -TERM "$PID" 2>/dev/null || true

for _ in $(seq 1 10); do
  kill -0 "$PID" 2>/dev/null || break
  sleep 1
done

if kill -0 "$PID" 2>/dev/null; then
  echo "Ainda vivo, forçando SIGKILL..."
  kill -KILL -- -"$PID" 2>/dev/null || kill -KILL "$PID" 2>/dev/null || true
fi

rm -f "$PID_FILE"
echo "Ralph parado."
