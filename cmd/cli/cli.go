// Package cli é o entrypoint do binário montado com cobra: serve (sobe a
// API), migrate (opera o runner de migrations) e seed (dados mínimos).
// Não conhece regra de negócio: traduz argv em chamadas ao bootstrap.
package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"workspace-api/cmd/bootstrap"
)

// NovoRootCommand monta a árvore de comandos SEM executar nada — os testes
// instanciam, injetam args/buffers e chamam Execute().
func NovoRootCommand() *cobra.Command {
	var caminhoConfig string

	raiz := &cobra.Command{
		Use:           "workspace-api",
		Short:         "API gerenciadora organization → workspace → user",
		Long:          "Template de API gerenciadora em Go com DDD, tenancy por organization e autorização granular.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	raiz.PersistentFlags().StringVar(&caminhoConfig, "config", "configs.json",
		"caminho do arquivo de configuração configs.json")

	raiz.AddCommand(comandoServe(&caminhoConfig))
	raiz.AddCommand(comandoMigrate(&caminhoConfig))
	raiz.AddCommand(comandoSeed(&caminhoConfig))
	return raiz
}

func comandoServe(caminho *string) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Sobe a API com boot completo (migrations automáticas, graceful shutdown)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return bootstrap.Serve(*caminho)
		},
	}
}

func comandoMigrate(caminho *string) *cobra.Command {
	migrate := &cobra.Command{
		Use:   "migrate",
		Short: "Operações sobre as migrations SQL de db/migrations",
	}
	migrate.AddCommand(
		comandoMigrateUp(caminho),
		comandoMigrateDown(caminho),
		comandoMigrateGoto(caminho),
		comandoMigrateForce(caminho),
		comandoMigrateStatus(caminho),
		comandoMigrateValidate(caminho),
		comandoMigrateCreate(caminho),
	)
	return migrate
}

func comandoMigrateUp(caminho *string) *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Aplica todas as migrations pendentes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			aplicadas, err := bootstrap.MigrateUp(*caminho)
			if err != nil {
				return err
			}
			return escreverLinha(cmd, "migrate up concluído: %d migration(s) aplicada(s).", aplicadas)
		},
	}
}

func comandoMigrateDown(caminho *string) *cobra.Command {
	return &cobra.Command{
		Use:   "down N",
		Short: "Reverte as últimas N migrations (rollback manual e explícito)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(args[0])
			if err != nil || n <= 0 {
				return fmt.Errorf("N deve ser um inteiro maior que zero, recebi %q", args[0])
			}
			if err := bootstrap.MigrateDown(*caminho, n); err != nil {
				return err
			}
			return escreverLinha(cmd, "migrate down concluído: %d migration(s) revertida(s).", n)
		},
	}
}

func comandoMigrateGoto(caminho *string) *cobra.Command {
	return &cobra.Command{
		Use:   "goto V",
		Short: "Sobe ou desce até exatamente a versão V",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			versao, err := strconv.ParseUint(args[0], 10, 32)
			if err != nil {
				return fmt.Errorf("V deve ser um número de versão válido, recebi %q", args[0])
			}
			if err := bootstrap.MigrateGoto(*caminho, uint(versao)); err != nil {
				return err
			}
			return escreverLinha(cmd, "migrate goto concluído: versão atual %d.", versao)
		},
	}
}

func comandoMigrateForce(caminho *string) *cobra.Command {
	return &cobra.Command{
		Use:   "force V",
		Short: "Marca a versão V manualmente sem executar SQL (recuperação de estado dirty)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			versao, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("V deve ser um inteiro (aceita -1 para limpar), recebi %q", args[0])
			}
			if err := bootstrap.MigrateForce(*caminho, versao); err != nil {
				return err
			}
			return escreverLinha(cmd, "migrate force concluído: versão marcada como %d.", versao)
		},
	}
}

func comandoMigrateStatus(caminho *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Versão atual e situação de cada migration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			estado, err := bootstrap.MigrateStatus(*caminho)
			if err != nil {
				return err
			}
			saida := cmd.OutOrStdout()
			fmt.Fprintf(saida, "versão atual: %d (dirty=%v)\n", estado.VersaoAtual, estado.Dirty)
			for _, linha := range estado.Linhas {
				fmt.Fprintf(saida, "  %04d  %-48s %s\n", linha.Versao, linha.Nome, linha.Situacao)
			}
			fmt.Fprintf(saida, "pendentes: %d\n", estado.Pendentes)
			return nil
		},
	}
}

func comandoMigrateValidate(caminho *string) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Confere pares up/down, sequência e SQL não vazio — SEM conexão com o banco",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := bootstrap.MigrateValidate(*caminho); err != nil {
				return err
			}
			_, err := cmd.OutOrStdout().Write([]byte("migrate validate ok: pares completos, sequência sem buracos, SQL presente.\n"))
			return err
		},
	}
}

func comandoMigrateCreate(caminho *string) *cobra.Command {
	return &cobra.Command{
		Use:   "create {dominio_subdominio_descricao}",
		Short: "Gera o próximo par NNNN_{descricao}.{up,down}.sql já no padrão de nome",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			up, down, err := bootstrap.MigrateCreate(*caminho, args[0])
			if err != nil {
				return err
			}
			return escreverLinha(cmd, "migration criada:\n  %s\n  %s", up, down)
		},
	}
}

func comandoSeed(caminho *string) *cobra.Command {
	return &cobra.Command{
		Use:   "seed",
		Short: "Semeia dados mínimos idempotentes (nunca automático no boot)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return bootstrap.Seed(*caminho)
		},
	}
}

// escreverLinha imprime no escritor do cobra (testável), não no os.Stdout.
func escreverLinha(cmd *cobra.Command, formato string, argumentos ...any) error {
	_, err := fmt.Fprintf(cmd.OutOrStdout(), formato+"\n", argumentos...)
	return err
}
