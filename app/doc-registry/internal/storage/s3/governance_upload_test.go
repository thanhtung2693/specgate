package s3

import (
	"strings"
	"testing"
)

func TestGovernanceUploadObjectKey_shape(t *testing.T) {
	t.Parallel()
	key := GovernanceUploadObjectKey("", "image/png")
	if !strings.HasPrefix(key, "governance/resources/uploads/") {
		t.Fatalf("key = %q", key)
	}
	if !strings.HasSuffix(key, ".png") {
		t.Fatalf("expected .png suffix, got %q", key)
	}
	prefixed := GovernanceUploadObjectKey("doc-registry/", "image/png")
	if !strings.HasPrefix(prefixed, "doc-registry/governance/resources/uploads/") {
		t.Fatalf("prefixed key = %q", prefixed)
	}
}

func TestExtForGovernanceContentType(t *testing.T) {
	t.Parallel()
	if extForGovernanceContentType("image/jpeg") != ".jpg" {
		t.Fatal()
	}
	if extForGovernanceContentType("application/pdf") != ".pdf" {
		t.Fatal()
	}
	if extForGovernanceContentType("text/markdown") != ".md" {
		t.Fatal()
	}
	if extForGovernanceContentType("audio/mpeg") != ".mp3" {
		t.Fatal()
	}
	if extForGovernanceContentType("audio/wav") != ".wav" {
		t.Fatal()
	}
	if extForGovernanceContentType("audio/webm") != ".webm" {
		t.Fatal()
	}
	if extForGovernanceContentType("audio/mp4") != ".m4a" {
		t.Fatal()
	}
	if extForGovernanceContentType("unknown/thing") != ".bin" {
		t.Fatal()
	}
}

func TestAllowedGovernanceUploadContentType(t *testing.T) {
	t.Parallel()
	if !AllowedGovernanceUploadContentType("image/png") {
		t.Fatal()
	}
	if !AllowedGovernanceUploadContentType("application/pdf") {
		t.Fatal()
	}
	if !AllowedGovernanceUploadContentType("audio/mpeg") {
		t.Fatal("audio/mpeg should be allowed")
	}
	if !AllowedGovernanceUploadContentType("audio/webm") {
		t.Fatal("audio/webm should be allowed")
	}
	if !AllowedGovernanceUploadContentType("AUDIO/WAV") {
		t.Fatal("MIME match should be case-insensitive")
	}
	if AllowedGovernanceUploadContentType("application/zip") {
		t.Fatal()
	}
	if AllowedGovernanceUploadContentType("") {
		t.Fatal()
	}
}
