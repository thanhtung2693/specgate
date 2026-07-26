package integrations

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var workRefMarkerPattern = regexp.MustCompile(`<!-- specgate-work-ref: ([A-Za-z0-9_-]+) -->`)

func parseWorkRefMarkers(texts ...string) []string {
	var refs []string
	seen := map[string]struct{}{}
	for _, text := range texts {
		for _, m := range workRefMarkerPattern.FindAllStringSubmatch(text, -1) {
			ref := strings.TrimSpace(m[1])
			if ref == "" {
				continue
			}
			key := strings.ToLower(ref)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			refs = append(refs, ref)
		}
	}
	return refs
}

func equalsRef(ref string, value string) bool {
	return strings.TrimSpace(value) != "" && strings.EqualFold(strings.TrimSpace(ref), strings.TrimSpace(value))
}

var providerPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

func normalizeIntegration(in *Integration) error {
	in.ID = strings.TrimSpace(in.ID)
	in.Provider = strings.ToLower(strings.TrimSpace(in.Provider))
	in.Name = strings.TrimSpace(in.Name)
	in.Status = strings.TrimSpace(in.Status)
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.ConfigJSON = strings.TrimSpace(in.ConfigJSON)
	in.LastError = strings.TrimSpace(in.LastError)
	if in.Provider == "" {
		return fmt.Errorf("%w: provider is required", ErrValidation)
	}
	if !providerPattern.MatchString(in.Provider) {
		return fmt.Errorf("%w: provider must use lowercase letters, numbers, dash, or underscore", ErrValidation)
	}
	if in.Name == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	if in.Status == "" {
		in.Status = StatusConnected
	}
	if !validStatus(in.Status) {
		return fmt.Errorf("%w: unsupported status %q", ErrValidation, in.Status)
	}
	return normalizeJSONField(&in.ConfigJSON, "config_json")
}

func normalizeResource(in *Resource) error {
	in.ResourceType = strings.ToLower(strings.TrimSpace(in.ResourceType))
	in.ExternalID = strings.TrimSpace(in.ExternalID)
	in.ExternalKey = strings.TrimSpace(in.ExternalKey)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.DefaultRef = strings.TrimSpace(in.DefaultRef)
	in.ConfigJSON = strings.TrimSpace(in.ConfigJSON)
	if in.ResourceType == "" {
		return fmt.Errorf("%w: resource_type is required", ErrValidation)
	}
	if in.ExternalKey == "" {
		return fmt.Errorf("%w: external_key is required", ErrValidation)
	}
	return normalizeJSONField(&in.ConfigJSON, "config_json")
}

func normalizeWebhookEvent(in *WebhookEvent) error {
	in.Provider = strings.ToLower(strings.TrimSpace(in.Provider))
	in.EventType = strings.ToLower(strings.TrimSpace(in.EventType))
	in.ExternalEventID = strings.TrimSpace(in.ExternalEventID)
	in.PayloadJSON = strings.TrimSpace(in.PayloadJSON)
	in.Status = strings.TrimSpace(in.Status)
	in.Error = strings.TrimSpace(in.Error)
	if in.Provider == "" {
		return fmt.Errorf("%w: provider is required", ErrValidation)
	}
	if in.EventType == "" {
		return fmt.Errorf("%w: event_type is required", ErrValidation)
	}
	if in.Status == "" {
		in.Status = WebhookStatusPending
	}
	if !validWebhookStatus(in.Status) {
		return fmt.Errorf("%w: unsupported webhook status %q", ErrValidation, in.Status)
	}
	return normalizeJSONField(&in.PayloadJSON, "payload_json")
}

func normalizeJSONField(value *string, field string) error {
	if strings.TrimSpace(*value) == "" {
		*value = "{}"
		return nil
	}
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(*value), &raw); err != nil {
		return fmt.Errorf("%w: %s must be valid JSON", ErrValidation, field)
	}
	return nil
}

func validStatus(status string) bool {
	switch status {
	case StatusConnected, StatusDisabled, StatusError:
		return true
	default:
		return false
	}
}

func validWebhookStatus(status string) bool {
	switch status {
	case WebhookStatusPending, WebhookStatusProcessed, WebhookStatusFailed, WebhookStatusIgnored:
		return true
	default:
		return false
	}
}
