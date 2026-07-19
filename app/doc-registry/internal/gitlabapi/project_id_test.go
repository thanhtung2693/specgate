package gitlabapi

import "testing"

func TestProjectAPIPathSegment(t *testing.T) {
	t.Parallel()
	if got := ProjectAPIPathSegment("123"); got != "123" {
		t.Errorf("numeric: got %q", got)
	}
	if got := ProjectAPIPathSegment("  foo/bar  "); got != "foo%2Fbar" {
		t.Errorf("plain path: got %q", got)
	}
	enc := "acme%2Fprojects%2Fspecgate"
	if got := ProjectAPIPathSegment(enc); got != enc {
		t.Errorf("pre-encoded: got %q want %q", got, enc)
	}
	if got := ProjectAPIPathSegment("/a/b/"); got != "a%2Fb" {
		t.Errorf("trim slashes: got %q", got)
	}
}
