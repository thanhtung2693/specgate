package workspace

import (
	"context"
	"testing"
)

func TestNormalizeIDRejectsUnsafePathSegments(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"", ".", "..", "../ws-b", "ws/a", `ws\a`, "ws..b", "ws\nb"} {
		if got, ok := NormalizeID(id); ok || got != "" {
			t.Errorf("NormalizeID(%q) = %q, %v; want empty, false", id, got, ok)
		}
	}
	if got, ok := NormalizeID(" ws-a "); !ok || got != "ws-a" {
		t.Fatalf("NormalizeID safe ID = %q, %v; want ws-a, true", got, ok)
	}
	if got := ID(WithID(context.Background(), "../ws-b")); got != "" {
		t.Fatalf("WithID retained unsafe workspace ID %q", got)
	}
}
