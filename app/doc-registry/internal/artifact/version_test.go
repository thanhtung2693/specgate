package artifact

import "testing"

func TestBumpVersion(t *testing.T) {
	cases := map[string]string{"": "v0.1", "v0.1": "v0.2", "v0.9": "v0.10", "v1.4": "v1.5"}
	for in, want := range cases {
		if got := bumpVersion(in); got != want {
			t.Errorf("bumpVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
