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

// specgate open [work-ref|reviews|artifacts|work] [--artifact <id>]
//
// Without a target it opens the configured web URL. A bare argument matching a
// section name opens that section page; anything else is treated as a work ref
// and opens the work item page.
func newOpenCmd(deps *Deps) *cobra.Command {
	var (
		artifactID string
		printOnly  bool
	)
	cmd := &cobra.Command{
		Use:   "open [work-ref|reviews|artifacts|work]",
		Short: "Open the SpecGate web UI, optionally deep-linked to a work item, section, or artifact",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if artifactID != "" && len(args) > 0 {
				payload := output.ErrorPayload{Code: "validation_failed", Message: "pass a target argument or --artifact, not both"}
				code := deps.Printer.Error("open", payload)
				return &output.ExitError{Code: code}
			}
			meta, err := deps.Client.Meta(cmd.Context())
			webURL := deps.ServerURL
			hasWebURL := err == nil && meta.WebURL != ""
			if hasWebURL {
				webURL = meta.WebURL
			}
			if err := validateBrowserURL(webURL); err != nil {
				code := deps.Printer.Error("open", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code}
			}
			// Deep links point at web UI pages; without an advertised web_url
			// the fallback is the API server, where those routes don't exist.
			// Bare `open` still opens the configured server URL; deployments
			// with web_url get deep links automatically.
			if !hasWebURL && (artifactID != "" || len(args) > 0) {
				payload := output.ErrorPayload{
					Code:    "unavailable",
					Message: "this server does not advertise a web UI URL — set APP_BASE_URL on Doc Registry (deep links need the web app, not the API)",
				}
				code := deps.Printer.Error("open", payload)
				return &output.ExitError{Code: code}
			}
			base := strings.TrimRight(webURL, "/")
			target := webURL
			switch {
			case artifactID != "":
				target = base + "/artifacts?artifact=" + url.QueryEscape(artifactID)
			case len(args) == 1:
				switch args[0] {
				case "reviews", "artifacts", "work":
					target = base + "/" + args[0]
				default:
					work, err := deps.Client.ResolveWorkRef(cmd.Context(), args[0])
					if err != nil {
						code := deps.Printer.Error("open", mapWorkRefError(args[0], err))
						return &output.ExitError{Code: code, Err: err}
					}
					target = base + "/work/" + url.PathEscape(work.ChangeRequestKey)
				}
			}
			if err := validateBrowserURL(target); err != nil {
				code := deps.Printer.Error("open", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code}
			}
			if !printOnly {
				if deps.Opener == nil {
					return fmt.Errorf("no opener configured")
				}
				if err := deps.Opener(target); err != nil {
					payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
					code := deps.Printer.Error("open", payload)
					return &output.ExitError{Code: code, Err: err}
				}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("open", map[string]string{"url": target})
				return nil
			}
			if printOnly {
				fmt.Fprintln(deps.Stdout, target)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleSuccess, "Opening"), styled(deps, output.StyleAction, target))
			return nil
		},
	}
	cmd.Flags().StringVar(&artifactID, "artifact", "", "Open a specific artifact in the Artifacts page")
	cmd.Flags().BoolVar(&printOnly, "print", false, "Print the UI URL without opening a browser")
	return cmd
}
