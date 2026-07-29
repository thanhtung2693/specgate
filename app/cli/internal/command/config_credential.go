package command

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// specgate config credential <username>
//
// Stores this machine's gateway credential for the selected server (ADR
// 2026-07-29 gateway-asserted identity). An appliance shared by a few
// developers verifies each request and records the authenticated username as
// the actor on approvals and delivery decisions, so the credential is what makes
// the name in the ledger that developer's own.
//
// The secret is read from a prompt or stdin, never from a flag: a flag lands in
// shell history and in process listings.
func newConfigCredentialCmd(deps *Deps) *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "credential <username>",
		Short: "Store this machine's gateway credential for the selected server",
		Long: "Store the credential the appliance gateway issued for you. It is kept per server,\n" +
			"so switching servers never sends one appliance's secret to another host, and it is\n" +
			"never printed back. Appliances with no gateway members need no credential.\n\n" +
			"The secret is read from a prompt, or from stdin when input is piped.",
		Example: "  specgate config credential tung\n" +
			"  specgate config credential tung --clear",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadFrom(deps.ConfigPath)
			if err != nil {
				code := deps.Printer.Error("config.credential", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			server := config.NormalizeServerURL(deps.ServerURL)
			if server == "" {
				code := deps.Printer.Error("config.credential", output.ErrorPayload{
					Code:    "usage",
					Message: "no server is selected; run `specgate config server <url>` first",
				})
				return &output.ExitError{Code: code}
			}

			if clear {
				delete(cfg.Credentials, server)
				if err := saveConfig(deps, cfg); err != nil {
					code := deps.Printer.Error("config.credential", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
					return &output.ExitError{Code: code, Err: err}
				}
				return reportCredentialState(deps, server, "", false)
			}

			if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
				code := deps.Printer.Error("config.credential", output.ErrorPayload{
					Code:    "usage",
					Message: "credential requires the username the appliance issued, or --clear",
				})
				return &output.ExitError{Code: code}
			}
			username := strings.TrimSpace(args[0])

			secret, err := readSecretInput(cmd, deps, "Gateway credential for "+username)
			if err != nil {
				code := deps.Printer.Error("config.credential", output.ErrorPayload{Code: "usage", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			if cfg.Credentials == nil {
				cfg.Credentials = map[string]config.ServerCredential{}
			}
			cfg.Credentials[server] = config.ServerCredential{Username: username, Secret: secret}
			if err := saveConfig(deps, cfg); err != nil {
				code := deps.Printer.Error("config.credential", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			return reportCredentialState(deps, server, username, true)
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "Remove the stored credential for the selected server")
	return cmd
}

// readSecretInput takes the secret from stdin when input is piped, and from a
// prompt otherwise. There is deliberately no flag for it: a flag value lands in
// shell history and in the process list.
func readSecretInput(cmd *cobra.Command, deps *Deps, title string) (string, error) {
	if !canPrompt(deps) || !sessionInteractive(deps) {
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", err
		}
		secret := strings.TrimRight(string(data), "\r\n")
		if strings.TrimSpace(secret) == "" {
			return "", errors.New("no secret on stdin; pipe it in or run interactively")
		}
		return secret, nil
	}
	secret, err := deps.Prompter.Secret(title)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(secret) == "" {
		return "", errors.New("credential secret is required")
	}
	return secret, nil
}

// reportCredentialState reports set/not-set only. The secret never appears in
// output, in either mode.
func reportCredentialState(deps *Deps, server, username string, set bool) error {
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("config.credential", map[string]any{
			"server": server, "username": username, "credential_set": set,
		})
		return nil
	}
	if !set {
		fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleSuccess, "Credential cleared for"), server)
		return nil
	}
	fmt.Fprintf(deps.Stdout, "%s %s %s\n",
		styled(deps, output.StyleSuccess, "Credential stored for"), styled(deps, output.StyleBold, username), "on "+server)
	return nil
}
