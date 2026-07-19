package command

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/client"
)

//go:embed all:local_plugin_assets
var localPluginAssets embed.FS

type embeddedLocalPlugin struct{}

func (embeddedLocalPlugin) PluginPackage(context.Context) (*client.PluginPackage, error) {
	body, err := fs.ReadFile(localPluginAssets, "local_plugin_assets/package.json")
	if err != nil {
		return nil, fmt.Errorf("read embedded Local plugin package: %w", err)
	}
	var pkg client.PluginPackage
	if err := json.Unmarshal(body, &pkg); err != nil {
		return nil, fmt.Errorf("parse embedded Local plugin package: %w", err)
	}
	return &pkg, nil
}

func (embeddedLocalPlugin) PluginFile(_ context.Context, requestedPath string) ([]byte, error) {
	requestedPath = strings.TrimSpace(requestedPath)
	if requestedPath == "" || strings.HasPrefix(requestedPath, "/") {
		return nil, fmt.Errorf("embedded Local plugin file %q not found", requestedPath)
	}
	for _, segment := range strings.Split(requestedPath, "/") {
		if segment == ".." {
			return nil, fmt.Errorf("embedded Local plugin file %q not found", requestedPath)
		}
	}
	cleanPath := path.Clean(requestedPath)
	if cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") {
		return nil, fmt.Errorf("embedded Local plugin file %q not found", requestedPath)
	}
	body, err := fs.ReadFile(localPluginAssets, "local_plugin_assets/"+cleanPath)
	if err != nil {
		return nil, fmt.Errorf("embedded Local plugin file %q not found", requestedPath)
	}
	return body, nil
}
