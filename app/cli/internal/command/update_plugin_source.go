package command

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/specgate/specgate/app/cli/internal/workflow"
)

// releasePluginSource reads canonical plugin files from the same release as
// the CLI updater. Normal setup and repair use the embedded package.
type releasePluginSource struct {
	base    string
	timeout time.Duration
}

func (s releasePluginSource) PluginPackage(ctx context.Context) (*workflow.PluginPackage, error) {
	body, err := s.PluginFile(ctx, "package.json")
	if err != nil {
		return nil, err
	}
	var pkg workflow.PluginPackage
	if err := json.Unmarshal(body, &pkg); err != nil {
		return nil, fmt.Errorf("parse release plugin package: %w", err)
	}
	return &pkg, nil
}

func (s releasePluginSource) PluginFile(ctx context.Context, path string) ([]byte, error) {
	if !portablePluginPath(path) {
		return nil, fmt.Errorf("invalid release plugin path %q", path)
	}
	return downloadUpdateFile(ctx, &Deps{Timeout: s.timeout}, s.base+"/plugins/"+path, 4<<20)
}
