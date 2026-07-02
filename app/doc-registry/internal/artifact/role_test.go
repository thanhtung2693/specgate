package artifact

import "testing"

func TestNormalizeRole(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want Role
	}{
		{"spec", RoleSpec},
		{"SPEC", RoleSpec},
		{"  Spec  ", RoleSpec},
		{"design", RoleDesign},
		{"plan", RolePlan},
		{"verification", RoleVerification},
		{"research", RoleResearch},
		{"reference", RoleReference},
		{"unspecified", RoleUnspecified},
		{"", RoleUnspecified},
		{"unknown_value", RoleUnspecified},
		{"custom:my-role", Role("custom:my-role")},
		{"CUSTOM:my-role", Role("custom:my-role")},
	}
	for _, tc := range cases {
		got := NormalizeRole(tc.in)
		if got != tc.want {
			t.Errorf("NormalizeRole(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeRoleRejectsEmptyCustom(t *testing.T) {
	t.Parallel()
	// "custom:" with nothing after the colon must fall back to unspecified.
	if got := NormalizeRole("custom:"); got != RoleUnspecified {
		t.Errorf("NormalizeRole(%q) = %q, want %q", "custom:", got, RoleUnspecified)
	}
}
