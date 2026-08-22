#!/usr/bin/env bash
set -euo pipefail

APP="workspace-api"
REPO="https://github.com/gothout/workspace-api.git"
BRANCH="ox-alpha/code"
WORKDIR="/opt/${APP}"
GOBIN="/usr/local/bin"
OPENCODE_CONFIG_DIR="/root/.config/opencode"

echo "==========================================="
echo "  Setup workspace-api + OpenCode"
echo "  Branch: $BRANCH"
echo "==========================================="

# --- 1. Dependências básicas ---
echo ""
echo "[1/7] Verificando dependências..."
if ! command -v git &>/dev/null; then
  echo "  -> Instalando git..."
  apt-get update -qq && apt-get install -y -qq git
fi

if ! command -v curl &>/dev/null; then
  echo "  -> Instalando curl..."
  apt-get update -qq && apt-get install -y -qq curl
fi

if ! command -v go &>/dev/null; then
  echo "  -> Instalando Go 1.22+..."
  apt-get update -qq && apt-get install -y -qq golang-go
fi

echo "  git: $(git --version | head -n1)"
echo "  go:  $(go version)"

# --- 2. Clone / atualização do repositório ---
echo ""
echo "[2/7] Clonando/atualizando repositório em $WORKDIR..."
if [[ -d "$WORKDIR/.git" ]]; then
  echo "  -> Já existe; fazendo pull..."
  cd "$WORKDIR"
  git fetch origin
  git checkout "$BRANCH"
  git pull origin "$BRANCH"
else
  git clone "$REPO" "$WORKDIR"
  cd "$WORKDIR"
  git checkout "$BRANCH"
fi

# --- 3. Build ---
echo ""
echo "[3/7] Compilando workspace-api..."
go build -o "${GOBIN}/${APP}" ./cmd/server
echo "  -> Binário: ${GOBIN}/${APP}"

# --- 4. Instalação do serviço ---
echo ""
echo "[4/7] Instalando serviço..."
if [[ -f "./scripts/ops/install-service.sh" ]]; then
  bash ./scripts/ops/install-service.sh "${GOBIN}/${APP}"
else
  echo "  [AVISO] install-service.sh não encontrado; pulando."
fi

# --- 5. OpenCode ---
echo ""
echo "[5/7] Configurando OpenCode..."
if ! command -v opencode &>/dev/null; then
  echo "  -> Instalando OpenCode..."
  curl -fsSL https://opencode.ai/install | bash
  # Garante que esteja no PATH desta sessão
  export PATH="$HOME/.local/bin:/usr/local/bin:$PATH"
fi

mkdir -p "$OPENCODE_CONFIG_DIR"
if [[ ! -f "${OPENCODE_CONFIG_DIR}/config.toml" ]]; then
  cat > "${OPENCODE_CONFIG_DIR}/config.toml" <<'EOF'
[model]
# Substitua pelo provider/modelo que você tem acesso.
# Exemplos:
#   provider = "openrouter"; model = "ox-alpha-free"
#   provider = "anthropic";  model = "claude-sonnet-4-20250514"
#   provider = "openai";     model = "gpt-4.1"
provider = "openrouter"
model = "ox-alpha-free"
# api_key = "sua-chave-aqui"

[permissions]
# Permite que o agente execute comandos e edite arquivos
allow = ["*"]
EOF
  echo "  -> Config criada em ${OPENCODE_CONFIG_DIR}/config.toml"
  echo "  -> EDITE O ARQUIVO e coloque sua chave/modelo antes de rodar o Ralph."
else
  echo "  -> Config já existe; não sobrescrevi."
fi

# --- 6. Configuração da aplicação ---
echo ""
echo "[6/7] Configuração da aplicação..."
CONFIG_FILE="/etc/${APP}/configs.json"
if [[ ! -f "$CONFIG_FILE" ]]; then
  echo "  -> Config padrão criada em $CONFIG_FILE"
else
  echo "  -> Config já existe; não sobrescrevi."
fi

# --- 7. Instruções finais ---
echo ""
echo "==========================================="
echo "  Instalação concluída!"
echo "==========================================="
echo ""
echo "Próximos passos:"
echo ""
echo "  1. Edite a config do workspace-api:"
echo "     nano $CONFIG_FILE"
echo ""
echo "  2. Inicie o serviço:"
echo "     cd $WORKDIR && ./scripts/ops/start.sh"
echo ""
echo "  3. Veja os logs em tempo real:"
echo "     ./scripts/ops/logs.sh"
echo ""
echo "  4. Para parar:"
echo "     ./scripts/ops/stop.sh"
echo ""
echo "  5. Para rodar o Ralph loop com OpenCode:"
echo "     cd $WORKDIR"
echo "     opencode run scripts/ralph/run-loop.sh"
echo ""
echo "  IMPORTANTE: confirme que o modelo em"
echo "  ${OPENCODE_CONFIG_DIR}/config.toml tem tool use (capacidade"
echo "  de executar comandos/editar arquivos). Modelos free/unlimited"
echo "  geralmente não têm — se não funcionar, use Claude Sonnet 4,"
echo "  GPT-4.1 ou Kimi K2."
echo ""
