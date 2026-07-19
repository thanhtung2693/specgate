package s3

import (
	"bytes"
	"testing"
)

func TestObjectKey(t *testing.T) {
	t.Parallel()
	got := ObjectKey("", "my-feature", "v1.2", "manifest.json")
	want := "artifacts/my-feature/v1.2/manifest.json"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	prefixed := ObjectKey("doc-registry/", "my-feature", "v1.2", "manifest.json")
	wantPrefixed := "doc-registry/artifacts/my-feature/v1.2/manifest.json"
	if prefixed != wantPrefixed {
		t.Fatalf("prefixed = %q, want %q", prefixed, wantPrefixed)
	}
}

func TestCreateBucketInput_DefaultRegionOmitsLocationConstraint(t *testing.T) {
	t.Parallel()
	in := createBucketInput("doc-registry", "us-east-1")
	if in.CreateBucketConfiguration != nil {
		t.Fatal("expected no location constraint for us-east-1")
	}
}

func TestCreateBucketInput_NonDefaultRegionIncludesLocationConstraint(t *testing.T) {
	t.Parallel()
	in := createBucketInput("doc-registry", "ap-southeast-1")
	if in.CreateBucketConfiguration == nil {
		t.Fatal("expected location constraint for non-default region")
	}
	if got := string(in.CreateBucketConfiguration.LocationConstraint); got != "ap-southeast-1" {
		t.Fatalf("LocationConstraint = %q, want %q", got, "ap-southeast-1")
	}
}

func TestReadObjectBodyRejectsBodyAboveCallerLimit(t *testing.T) {
	t.Parallel()
	if _, err := readObjectBody(bytes.NewBufferString("four"), 3); err == nil {
		t.Fatal("expected oversized object read to fail")
	}
}
