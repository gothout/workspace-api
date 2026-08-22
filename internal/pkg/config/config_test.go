package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func escreverTemporario(t *testing.T, conteudo string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "configs.json")
	if err := os.WriteFile(path, []byte(conteudo), 0o600); err != nil {
		t.Fatalf("falha ao criar arquivo temporário: %v", err)
	}
	return path
}

func exemploValido() string {
	return `{
	  "app": {"name": "workspace-api", "env": "dev", "version": "0.1.0", "base_domain": "localhost"},
	  "server": {"http": {"port": 8080, "read_timeout_sec": 15, "write_timeout_sec": 30, "idle_timeout_sec": 60, "shutdown_timeout_sec": 10, "trusted_proxy": [], "cors": {"allowed_origins": []}}},
	  "security": {"jwt_secret": "segredo-de-teste", "jwt_ttl_min": 60, "jwt_refresh_ttl_hours": 168},
	  "databases": {
	    "postgres": {"host": "localhost", "port": 5432, "user": "workspace", "pass": "workspace", "name": "workspace", "ssl_mode": "disable",
	      "pool": {"max_open_conns": 25, "max_idle_conns": 10, "conn_max_lifetime_min": 5, "conn_max_idle_time_min": 2}},
	    "migrations": {"path": "db/migrations", "auto_run": true, "lock_timeout_sec": 5, "statement_timeout_min": 10}
	  }
	}`
}

func TestUseAntesDoInitDevolveErro(t *testing.T) {
	ResetarParaTeste()
	if _, err := Use(); err == nil {
		t.Fatal("Use antes do Init deveria devolver erro")
	}
}

func TestMustUseAntesDoInitEntraEmPanico(t *testing.T) {
	ResetarParaTeste()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustUse antes do Init deveria entrar em pânico")
		}
	}()
	MustUse()
}

func TestInitComCaminhoVazioFalha(t *testing.T) {
	ResetarParaTeste()
	err := Init("")
	if err == nil || !strings.Contains(err.Error(), "--config") {
		t.Fatalf("esperava erro citando --config, obtive: %v", err)
	}
}

func TestInitComArquivoAusenteFalha(t *testing.T) {
	ResetarParaTeste()
	err := Init(filepath.Join(t.TempDir(), "nao-existe.json"))
	if err == nil || !strings.Contains(err.Error(), "falha ao ler") {
		t.Fatalf("esperava erro de leitura, obtive: %v", err)
	}
}

func TestInitComJsonMalformadoFalha(t *testing.T) {
	ResetarParaTeste()
	path := escreverTemporario(t, `{"app": `)
	err := Init(path)
	if err == nil || !strings.Contains(err.Error(), "interpretar") {
		t.Fatalf("esperava erro de interpretação, obtive: %v", err)
	}
}

func TestInitComChaveObrigatoriaFaltandoFalha(t *testing.T) {
	casos := map[string]string{
		"app.name ausente": strings.Replace(exemploValido(), `"workspace-api"`, `""`, 1),
		"base_domain ausente": strings.Replace(
			strings.Replace(exemploValido(), `"localhost"`, `""`, 1), `"segredo-de-teste"`, `"outro-segredo"`, 1),
		"porta inválida": strings.Replace(exemploValido(), `"port": 8080`, `"port": -1`, 1),
	}
	for nome, json := range casos {
		t.Run(nome, func(t *testing.T) {
			ResetarParaTeste()
			err := Init(escreverTemporario(t, json))
			if err == nil {
				t.Fatal("Init deveria reprovar configuração incompleta")
			}
			if strings.Contains(err.Error(), "workspace") && strings.Contains(err.Error(), "senha") {
				t.Fatalf("erro não deve despejar segredo: %v", err)
			}
		})
	}
}

func TestInitComSegredoAusenteFalha(t *testing.T) {
	ResetarParaTeste()
	json := strings.Replace(exemploValido(), `"segredo-de-teste"`, `""`, 1)
	err := Init(escreverTemporario(t, json))
	if err == nil || !strings.Contains(err.Error(), "jwt_secret") {
		t.Fatalf("esperava erro de jwt_secret, obtive: %v", err)
	}
}

func TestInitValidoCarregaEExpõePorUse(t *testing.T) {
	ResetarParaTeste()
	if err := Init(escreverTemporario(t, exemploValido())); err != nil {
		t.Fatalf("Init com config válida falhou: %v", err)
	}
	cfg, err := Use()
	if err != nil {
		t.Fatalf("Use após Init válido falhou: %v", err)
	}
	if cfg.App.Name != "workspace-api" || cfg.App.BaseDomain != "localhost" {
		t.Errorf("campos de app incorretos: %+v", cfg.App)
	}
	if cfg.Databases.Postgres.Pool.MaxOpenConns != 25 {
		t.Errorf("pool não mapeado: %+v", cfg.Databases.Postgres.Pool)
	}
	if !cfg.Databases.Migrations.AutoRun {
		t.Error("migrations.auto_run deveria ser true")
	}
	read, write, idle := cfg.Server.HTTP.Timeouts()
	if read != 15*1e9 || write != 30*1e9 || idle != 60*1e9 {
		t.Errorf("timeouts incorretos: %s %s %s", read, write, idle)
	}
}

func TestSegredoExemploReprovaSomenteEmProducao(t *testing.T) {
	ResetarParaTeste()
	json := strings.Replace(exemploValido(), `"env": "dev"`, `"env": "producao"`, 1)
	json = strings.Replace(json, `"segredo-de-teste"`, `"trocar-em-producao"`, 1)
	err := Init(escreverTemporario(t, json))
	if err == nil || !strings.Contains(err.Error(), "jwt_secret") {
		t.Fatalf("segredo de exemplo em produção deveria reprovar, obtive: %v", err)
	}
}
