package command

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/output"
)

const (
	initWordmarkMinColumns = 58
	initWordmark           = ` ____  ____  _____  ____   ____    _  _____  _____
/ ___||  _ \| ____|/ ___| / ___|  / \|_   _|| ____|
\___ \| |_) |  _|| |    | |  _  / _ \ | |  |  _|
 ___) |  __/| |__| |___ | |_| |/ ___ \| |  | |___
|____/|_|   |_____|\____| \____/_/   \_\_|  |_____|`
)

func writeInitWelcome(deps *Deps) {
	if deps == nil || deps.Stderr == nil || !sessionInteractive(deps) || deps.Printer == nil || !stderrColorEnabled(deps, output.ModeHuman) || !deps.Printer.StderrColorEnabled() {
		return
	}
	mark := initWordmark
	if initTerminalColumns() < initWordmarkMinColumns {
		mark = "SpecGate"
	}
	fmt.Fprintln(deps.Stderr, deps.Printer.StyleStderr(mark, output.StyleAction))
	fmt.Fprintln(deps.Stderr)
}

func initTerminalColumns() int {
	columns, err := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS")))
	if err != nil || columns <= 0 {
		return 80
	}
	return columns
}
