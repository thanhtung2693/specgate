package command

import (
	"fmt"
	"os"

	"github.com/mattn/go-isatty"

	"github.com/specgate/specgate/app/cli/internal/output"
)

// sessionInteractive reports whether the CLI may prompt: input is enabled
// (no --no-input, which --json implies) and stdin is a terminal.
func sessionInteractive(deps *Deps) bool {
	if deps.NoInput {
		return false
	}
	if deps.StdinIsTTY != nil {
		return deps.StdinIsTTY()
	}
	f, ok := deps.Stdin.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

// requireConfirm gates an action behind a yes/no confirmation, prompting only
// in interactive terminal sessions.
//
// It returns (true, nil) when the caller may proceed. Behaviour by mode:
//   - --yes: proceed without prompting.
//   - non-interactive (--no-input, implied by --json, or stdin not a TTY):
//     proceed without prompting — the confirmed actions are non-destructive
//     ceremony, and --yes stays accepted for compatibility.
//   - interactive terminal: prompt; a decline prints "Cancelled." and returns
//     (false, nil).
func requireConfirm(deps *Deps, prompt string) (bool, error) {
	if deps.Yes || !sessionInteractive(deps) {
		return true, nil
	}
	confirmed, err := deps.Prompter.Confirm(prompt, false)
	if err != nil {
		return false, &output.ExitError{Code: output.ExitUsage, Err: err}
	}
	if !confirmed {
		fmt.Fprintln(deps.Stdout, "Cancelled.")
		return false, nil
	}
	return true, nil
}
