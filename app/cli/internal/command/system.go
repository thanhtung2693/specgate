package command

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func registerSystemCommands(root *cobra.Command, deps *Deps) {
	root.AddCommand(newVersionCmd(deps))
	root.AddCommand(newStatusCmd(deps))
	root.AddCommand(newDoctorCmd(deps))
	root.AddCommand(newOpenCmd(deps))
	root.AddCommand(newLocalUICmd(deps))
	root.AddCommand(newUpdateCmd(deps))
	root.AddCommand(newUninstallCmd(deps))
	root.AddCommand(newInitCmd(deps))
	root.AddCommand(newCleanupCmd(deps))
}

func truthyEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func isDevVersion(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "" || v == "dev" || v == "unknown"
}

func normalizeVersion(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.TrimPrefix(v, "version ")
	v = strings.TrimPrefix(v, "specgate ")
	v = strings.TrimPrefix(v, "v")
	return v
}

func userHomeDir(deps *Deps) (string, error) {
	if deps != nil && deps.UserHomeDir != nil {
		return deps.UserHomeDir()
	}
	return os.UserHomeDir()
}

func validateUsernamePrompt(value string) error {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) < 3 || len(value) > 40 {
		return fmt.Errorf("username must be 3-40 characters")
	}
	for i, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if !valid {
			return fmt.Errorf("username can only use lowercase letters, numbers, underscores, and hyphens")
		}
		if i == 0 && !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return fmt.Errorf("username must start with a letter or number")
		}
	}
	return nil
}
