package command

import (
	"fmt"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func storeUpgradePrompt(deps *Deps, prompt string, upgrade *local.StoreUpgrade) string {
	if upgrade == nil {
		return prompt
	}
	notice := fmt.Sprintf("This upgrades the entire Local store: older CLIs cannot write afterward. A pre-upgrade backup will be saved at %s. Restore that backup to return to the old CLI; later changes are not in it.", upgrade.BackupPath)
	if deps.Printer.Mode() != output.ModeJSON {
		fmt.Fprintln(deps.Stderr, notice)
	}
	return prompt + " " + notice
}

func confirmStoreWrite(deps *Deps, op, prompt string, upgrade *local.StoreUpgrade) (bool, error) {
	prompt = storeUpgradePrompt(deps, prompt, upgrade)
	if upgrade != nil && !deps.Yes && !sessionInteractive(deps) {
		payload := output.ErrorPayload{Code: "confirmation_required", Message: prompt + " Re-run with --yes to confirm the store upgrade."}
		code := deps.Printer.Error(op, payload)
		return false, &output.ExitError{Code: code}
	}
	return requireConfirm(deps, prompt)
}
