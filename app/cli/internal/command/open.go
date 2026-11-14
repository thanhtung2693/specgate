package command

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/output"
)

func validateBrowserURL(target string) error {
	u, err := url.Parse(target)
	if err != nil || !utf8.ValidString(target) || strings.IndexFunc(target, unicode.IsControl) >= 0 ||
		(!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) || u.Hostname() == "" || u.Opaque != "" {
		return fmt.Errorf("web UI URL must be an absolute HTTP(S) URL without control characters")
	}
	return nil
}

func defaultOpener(target string) error {
	if err := validateBrowserURL(target); err != nil {
		return err
	}
	if err := launchBrowser(target); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}

func newOpenCmd(deps *Deps) *cobra.Command {
	var (
		artifactID string
		printOnly  bool
	)
	cmd := &cobra.Command{
		Use:   "open [work-ref|reviews|work]",
		Short: "Open the running Local review UI",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if artifactID != "" && len(args) > 0 {
				payload := output.ErrorPayload{Code: "validation_failed", Message: "pass a target argument or --artifact, not both"}
				code := deps.Printer.Error("open", payload)
				return &output.ExitError{Code: code}
			}
			return openLocalUI(cmd, deps, args, artifactID, printOnly)
		},
	}
	cmd.Flags().StringVar(&artifactID, "artifact", "", "Retired artifact destination")
	_ = cmd.Flags().MarkHidden("artifact")
	cmd.Flags().BoolVar(&printOnly, "print", false, "Print the UI URL without opening a browser")
	return cmd
}
