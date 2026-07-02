package main

import (
	"os"

	"github.com/specgate/specgate/app/cli/internal/command"
)

func main() {
	deps := command.DefaultDeps()
	code := command.ExecuteForCode(command.NewRootCommand(deps))
	if code != 0 {
		os.Exit(code)
	}
}
