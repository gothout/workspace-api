# scripts

Utilitários do projeto.

## Ralph loop (`scripts/ralph/`)

Agente autônomo de execução estruturada. Lê o `prd.json`, pega a próxima
story não concluída, lê os docs obrigatórios, implementa, roda os checks e
atualiza o progresso.

- `ralph.sh` — runner genérico (suporta opencode, claude, kimi-cli etc.)
- `run-loop.sh` — wrapper para subir o loop com **opencode**
- `AGENT.md` — prompt do agente com as regras do projeto
- `prd.json` — Product Requirements Document com as fases (#1 a #7)
- `prd.json.example` — exemplo de formato
- `progress.txt` — log de progresso e padrões descobertos

Uso:

```bash
./scripts/ralph/run-loop.sh [max_iterations]
```
