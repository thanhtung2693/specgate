package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxResponseBody = 4 << 20 // 4 MiB

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return err
	}
	if len(data) > maxResponseBody {
		return fmt.Errorf("response exceeds 4 MiB limit")
	}

	if resp.StatusCode >= 400 {
		return c.parseError(resp.StatusCode, data)
	}

	if out == nil {
		return nil
	}

	// Unwrap Huma v2 { "body": ... } envelope.
	var wrapper struct {
		Body json.RawMessage `json:"body"`
	}
	if jsonErr := json.Unmarshal(data, &wrapper); jsonErr == nil && len(wrapper.Body) > 0 && wrapper.Body[0] == '{' {
		return json.Unmarshal(wrapper.Body, out)
	}
	return json.Unmarshal(data, out)
}

func (c *Client) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	base, err := url.Parse(c.base)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	ref, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse request path: %w", err)
	}
	joinedPath := strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(ref.Path, "/")
	joinedRawPath := strings.TrimRight(base.EscapedPath(), "/") + "/" + strings.TrimLeft(ref.EscapedPath(), "/")
	base.Path = joinedPath
	if joinedRawPath != joinedPath {
		base.RawPath = joinedRawPath
	} else {
		base.RawPath = ""
	}
	base.RawQuery = ref.RawQuery
	base.Fragment = ref.Fragment
	if workspace := workspaceID(ctx); workspace != "" {
		query := base.Query()
		if query.Get("workspace_id") == "" {
			query.Set("workspace_id", workspace)
			base.RawQuery = query.Encode()
		}
	}
	if allWorkspaces(ctx) {
		query := base.Query()
		if query.Get("workspace_id") == "" {
			query.Set("all_workspaces", "true")
			base.RawQuery = query.Encode()
		}
	}
	target := base.String()

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.credentialUser != "" && c.credentialSecret != "" {
		req.SetBasicAuth(c.credentialUser, c.credentialSecret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) parseError(status int, data []byte) *APIError {
	// Try to decode RFC 9457 / Huma error body.
	var rfc struct {
		Title  string `json:"title"`
		Detail string `json:"detail"`
		Errors []struct {
			Message  string `json:"message"`
			Location string `json:"location,omitempty"`
			Value    any    `json:"value,omitempty"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(data, &rfc)

	kind := ErrorGeneric
	switch status {
	case http.StatusBadRequest:
		kind = ErrorUsage
	case http.StatusForbidden:
		kind = ErrorForbidden
	case http.StatusNotFound:
		kind = ErrorNotFound
	case http.StatusConflict:
		kind = ErrorConflict
	case http.StatusUnprocessableEntity:
		kind = ErrorIncompatible
	case http.StatusServiceUnavailable:
		kind = ErrorUnavailable
	}

	msg := rfc.Title
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", status)
	}
	details := map[string]any{}
	if len(rfc.Errors) > 0 {
		errors := make([]map[string]any, 0, len(rfc.Errors))
		for _, e := range rfc.Errors {
			item := map[string]any{"message": e.Message}
			if e.Location != "" {
				item["location"] = e.Location
			}
			if e.Value != nil {
				item["value"] = e.Value
			}
			errors = append(errors, item)
		}
		details["errors"] = errors
	}
	return &APIError{Kind: kind, Status: status, Message: msg, Detail: rfc.Detail, Details: details}
}
