package command

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestVerifyUsesSelectedWorkspace(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: "specgate"}
	verify := &cobra.Command{Use: "verify"}
	root.AddCommand(verify)

	if !commandUsesSelectedWorkspace(verify) {
		t.Fatal("verify must use the selected workspace")
	}
}
