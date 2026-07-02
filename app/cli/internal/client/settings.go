package client

import (
	"context"
	"net/http"
)

// GetSettings returns all Doc Registry settings. Sensitive values (API keys) are
// masked unless the request carries trusted credentials.
func (c *Client) GetSettings(ctx context.Context) (map[string]string, error) {
	var resp struct {
		Settings map[string]string `json:"settings"`
	}
	if err := c.get(ctx, "/settings", &resp); err != nil {
		return nil, err
	}
	return resp.Settings, nil
}

// UpdateSettings PUTs a partial set of key-value settings (merged server-side,
// sensitive values encrypted at rest) and returns the full resulting set.
func (c *Client) UpdateSettings(ctx context.Context, settings map[string]string) (map[string]string, error) {
	body := map[string]any{"settings": settings}
	var resp struct {
		Settings map[string]string `json:"settings"`
	}
	if err := c.do(ctx, http.MethodPut, "/settings", body, &resp); err != nil {
		return nil, err
	}
	return resp.Settings, nil
}
