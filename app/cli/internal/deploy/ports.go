package deploy

import (
	"context"
	"fmt"
)

// checkDocker verifies the Docker daemon and Compose plugin are reachable.
func checkDocker(ctx context.Context, runner CommandRunner) error {
	if err := runner.Run(ctx, "docker", "version", "--format", "{{.Server.Version}}"); err != nil {
		return fmt.Errorf("docker daemon not reachable: %w", err)
	}
	if err := runner.Run(ctx, "docker", "compose", "version"); err != nil {
		return fmt.Errorf("docker compose plugin not available: %w", err)
	}
	return nil
}
