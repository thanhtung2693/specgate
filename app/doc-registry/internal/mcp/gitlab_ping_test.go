package mcp

import (
	"testing"
)

func TestNormalizeGitLabAPIURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"https://gitlab.com/api/v4", "https://gitlab.com/api/v4"},
		{"https://gitlab.com/api/v4/", "https://gitlab.com/api/v4"},
		{"https://gitlab.com", "https://gitlab.com/api/v4"},
		{"https://gitlab.com/", "https://gitlab.com/api/v4"},
		{"https://code.example.com/api/v4", "https://code.example.com/api/v4"},
	}
	for _, tc := range tests {
		if got := normalizeGitLabAPIURL(tc.in); got != tc.want {
			t.Errorf("normalizeGitLabAPIURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
