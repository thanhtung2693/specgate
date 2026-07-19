package identity

import "testing"

func TestNormalizeUsername(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "lowercases and trims", input: " Thanhtung2693 ", want: "thanhtung2693"},
		{name: "allows underscore", input: "thanhtung_2693", want: "thanhtung_2693"},
		{name: "allows hyphen", input: "thanh-tung", want: "thanh-tung"},
		{name: "rejects short", input: "tt", wantErr: true},
		{name: "rejects leading underscore", input: "_thanhtung", wantErr: true},
		{name: "rejects spaces", input: "thanh tung", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeUsername(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeUsername(%q) succeeded, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeUsername(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeUsername(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestWorkspaceSlug(t *testing.T) {
	t.Parallel()
	if got := WorkspaceSlug("Acme Engineering!"); got != "acme-engineering" {
		t.Fatalf("WorkspaceSlug = %q, want acme-engineering", got)
	}
}

func TestWorkspaceSlugUsesASCIISlugCharacters(t *testing.T) {
	t.Parallel()
	if got := WorkspaceSlug("Túng Workspace"); got != "t-ng-workspace" {
		t.Fatalf("WorkspaceSlug = %q, want t-ng-workspace", got)
	}
}
