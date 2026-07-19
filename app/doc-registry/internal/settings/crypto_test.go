package settings

import (
	"encoding/hex"
	"strings"
	"testing"
)

func testHexKey(t *testing.T) string {
	t.Helper()
	return hex.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
}

func TestCrypto_RoundTrip(t *testing.T) {
	t.Parallel()
	c, err := NewCrypto(testHexKey(t))
	if err != nil {
		t.Fatal(err)
	}
	original := "encrypted-test-token"
	enc, err := c.Encrypt(original)
	if err != nil {
		t.Fatal(err)
	}
	if enc == original {
		t.Fatal("encrypted value should differ from plaintext")
	}
	dec, err := c.Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != original {
		t.Fatalf("decrypt mismatch: got %q, want %q", dec, original)
	}
}

func TestCrypto_EmptyPlaintext(t *testing.T) {
	t.Parallel()
	c, err := NewCrypto(testHexKey(t))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := c.Encrypt("")
	if err != nil {
		t.Fatal(err)
	}
	dec, err := c.Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != "" {
		t.Fatalf("expected empty, got %q", dec)
	}
}

func TestCrypto_WrongKey(t *testing.T) {
	t.Parallel()
	c1, _ := NewCrypto(testHexKey(t))
	enc, _ := c1.Encrypt("secret")

	otherKey := hex.EncodeToString([]byte("abcdefghijklmnopabcdefghijklmnop"))
	c2, _ := NewCrypto(otherKey)
	_, err := c2.Decrypt(enc)
	if err == nil {
		t.Fatal("expected decrypt to fail with wrong key")
	}
}

func TestNewCrypto_EmptyKey(t *testing.T) {
	t.Parallel()
	_, err := NewCrypto("")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestNewCrypto_ShortKey(t *testing.T) {
	t.Parallel()
	_, err := NewCrypto("aabb")
	if err == nil {
		t.Fatal("expected error for short key")
	}
	if !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewCrypto_InvalidHex(t *testing.T) {
	t.Parallel()
	_, err := NewCrypto("not-valid-hex!")
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}
