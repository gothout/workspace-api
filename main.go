// workspace-api é o entrypoint do binário: só delega ao CLI (cobra).
// Toda montagem do processo vive no cmd/bootstrap.
package main

import (
	"fmt"
	"os"

	"workspace-api/cmd/cli"

	_ "workspace-api/docs" // Swagger gerado e versionado (swag init)
)

func main() {
	if err := cli.NovoRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "workspace-api:", err.Error())
		os.Exit(1)
	}
}
