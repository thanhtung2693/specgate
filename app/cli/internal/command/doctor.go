package command

import "github.com/spf13/cobra"

type doctorCheck struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Command string `json:"command,omitempty"`
}

func newDoctorCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check Local setup health",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLocalDoctor(cmd.Context(), deps)
		},
	}
}
