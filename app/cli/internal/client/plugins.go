package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type PluginPackage struct {
	Schema      string   `json:"schema"`
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Skills      []string `json:"skills"`
	ServedFiles []string `json:"served_files"`
}

func (c *Client) PluginPackage(ctx context.Context) (*PluginPackage, error) {
	body, err := c.PluginFile(ctx, "package.json")
	if err != nil {
		return nil, err
	}
	var pkg PluginPackage
	if err := json.Unmarshal(body, &pkg); err != nil {
		return nil, fmt.Errorf("parse plugin package: %w", err)
	}
	return &pkg, nil
}

func (c *Client) PluginFile(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pluginURL(c.base, path), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseBody {
		return nil, fmt.Errorf("response exceeds 4 MiB limit")
	}
	if resp.StatusCode >= 400 {
		return nil, c.parseError(resp.StatusCode, data)
	}
	return data, nil
}

func pluginURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	path = strings.TrimLeft(strings.TrimSpace(path), "/")
	return base + "/plugins/" + path
}
