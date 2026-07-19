package command

import (
	"fmt"
	"os"

	"github.com/mattn/go-isatty"

	"github.com/specgate/specgate/app/cli/internal/output"
)

// canPrompt reports whether command wizards may ask for input. `--plain` is
// output-only and intentionally non-interactive; `--json` implies `--no-input`.
func canPrompt(deps *Deps) bool {
	return deps != nil && !deps.NoInput && deps.Printer != nil && deps.Printer.Mode() == output.ModeHuman
}

// sessionInteractive reports whether the CLI may show confirmation prompts:
// prompt-capable output mode plus terminal stdin.
func sessionInteractive(deps *Deps) bool {
	if !canPrompt(deps) {
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
//     ceremony, and --yes remains an explicit automation signal.
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
