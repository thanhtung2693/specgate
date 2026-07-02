package mcp

import "testing"

func TestParseSkillDetailURI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		uri  string
		want string
		ok   bool
	}{
		{"specgate://skills/fe32ba6e-14ee-4800-8ee8-d514687b3c98", "fe32ba6e-14ee-4800-8ee8-d514687b3c98", true},
		{"specgate://skills/", "", false},
		{"specgate://skills", "", false},
		{"specgate://skills/a/b", "", false},
		{"https://example.com/skills/x", "", false},
	}
	for _, tc := range tests {
		got, ok := parseSkillDetailURI(tc.uri)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseSkillDetailURI(%q) = (%q, %v), want (%q, %v)", tc.uri, got, ok, tc.want, tc.ok)
		}
	}
}
