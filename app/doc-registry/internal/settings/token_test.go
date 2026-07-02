package settings

import (
	"encoding/hex"
	"testing"
)

func TestGenerateToken(t *testing.T) {
	t.Parallel()

	a, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if len(a) != 64 {
		t.Fatalf("token length = %d, want 64 hex chars", len(a))
	}
	if _, err := hex.DecodeString(a); err != nil {
		t.Fatalf("token is not valid hex: %v", err)
	}

	b, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if a == b {
		t.Fatal("two GenerateToken calls returned the same value")
	}
}
