#!/usr/bin/env bash
set -euo pipefail

APP="workspace-api"
USER="workspace-api"
GROUP="workspace-api"
BIN_SRC="${1:-./workspace-api}"
BIN_DST="/usr/local/bin/${APP}"
CONFIG_DIR="/etc/${APP}"
DATA_DIR="/var/lib/${APP}"
LOG_DIR="/var/log/${APP}"
SERVICE_SRC="scripts/ops/${APP}.service"
SERVICE_DST="/etc/systemd/system/${APP}.service"

if [[ ! -f "$BIN_SRC" ]]; then
  echo "[ERRO] Binário não encontrado: $BIN_SRC" >&2
  echo "       Compile antes: go build -o workspace-api ./cmd/server" >&2
  exit 1
fi

if [[ ! -f "$SERVICE_SRC" ]]; then
  echo "[ERRO] Template systemd não encontrado: $SERVICE_SRC" >&2
  exit 1
fi

echo "[INFO] Instalando binário..."
sudo cp "$BIN_SRC" "$BIN_DST"
sudo chmod +x "$BIN_DST"

echo "[INFO] Criando usuário/sistema..."
if ! id -u "$USER" &>/dev/null; then
  sudo useradd --system --no-create-home --home-dir "$DATA_DIR" "$USER"
fi

echo "[INFO] Criando diretórios..."
sudo mkdir -p "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"
sudo chown "root:$GROUP" "$CONFIG_DIR"
sudo chmod 750 "$CONFIG_DIR"
sudo chown "$USER:$GROUP" "$DATA_DIR" "$LOG_DIR"
sudo chmod 750 "$DATA_DIR" "$LOG_DIR"

if [[ ! -f "${CONFIG_DIR}/configs.json" ]]; then
  echo "[INFO] Criando config vazio (edite antes de subir o serviço)..."
  sudo tee "${CONFIG_DIR}/configs.json" >/dev/null <<'EOF'
{
  "server": {
    "port": 8080,
    "base_domain": "localhost"
  },
  "database": {
    "dsn": "postgres://workspace_api:workspace_api@localhost:5432/workspace_api?sslmode=disable"
  },
  "jwt": {
    "secret": "MUDE-ESTE-SECRET-EM-PRODUCAO",
    "ttl_hours": 24
  }
}
EOF
  sudo chmod 640 "${CONFIG_DIR}/configs.json"
  sudo chown "root:$GROUP" "${CONFIG_DIR}/configs.json"
fi

echo "[INFO] Instalando unit do systemd..."
sudo cp "$SERVICE_SRC" "$SERVICE_DST"
sudo systemctl daemon-reload

echo "[OK]   Instalação concluída."
echo ""
echo "Próximos passos:"
echo "  1. Edite a config: sudo nano ${CONFIG_DIR}/configs.json"
echo "  2. Suba o serviço:  sudo ./scripts/ops/start.sh"
echo "  3. Veja os logs:    ./scripts/ops/logs.sh"
