# scripts

Utilitários do projeto.

## Ralph loop (`scripts/ralph/`)

Agente autônomo de execução estruturada. Lê o `prd.json`, pega a próxima
story não concluída, lê os docs obrigatórios, implementa, roda os checks e
atualiza o progresso.

- `ralph.sh` — runner genérico (suporta opencode, claude, kimi-cli etc.;
  `--model` para trocar o modelo, default `opencode-go/ox-alpha-free`)
- `run-loop.sh` — wrapper para subir o loop com **opencode** (foreground)
- `start.sh` — sobe o loop em background (opencode + Ox Alpha), grava PID e log
- `stop.sh` — para o loop em background (mata o grupo de processos)
- `view.sh` — status + `tail -f` do log (`view.sh status` só o resumo)
- `AGENT.md` — prompt do agente com as regras do projeto
- `prd.json` — Product Requirements Document com as fases (#1 a #7)
- `prd.json.example` — exemplo de formato
- `progress.txt` — log de progresso e padrões descobertos

Uso:

```bash
./scripts/ralph/start.sh [max_iterations]   # sobe em background
./scripts/ralph/view.sh                      # acompanha o log
./scripts/ralph/stop.sh                      # para o loop

# foreground (sem background):
./scripts/ralph/run-loop.sh [max_iterations]
```
