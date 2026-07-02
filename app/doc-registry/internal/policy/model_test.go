package policy_test

import (
	"testing"

	"github.com/specgate/doc-registry/internal/policy"
)

func TestDigestOf_DeterministicForSameInput(t *testing.T) {
	t.Parallel()
	obj := map[string]any{"b": 2, "a": 1}
	d1, err := policy.DigestOf(obj)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := policy.DigestOf(obj)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatalf("not deterministic: %s vs %s", d1, d2)
	}
	// "sha256:" (7) + 64 hex chars = 71
	if len(d1) != 71 {
		t.Fatalf("unexpected digest length %d: %s", len(d1), d1)
	}
}

func TestDigestOf_KeyOrderIndependent(t *testing.T) {
	t.Parallel()
	a := map[string]any{"z": 1, "a": 2}
	b := map[string]any{"a": 2, "z": 1}
	da, err := policy.DigestOf(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := policy.DigestOf(b)
	if err != nil {
		t.Fatal(err)
	}
	if da != db {
		t.Fatalf("key order should not matter: %s vs %s", da, db)
	}
}

func TestGateRef_Key(t *testing.T) {
	t.Parallel()
	ref := policy.GateRef{Namespace: "acme", Name: "api-compat", Version: "2"}
	if ref.Key() != "acme/api-compat@2" {
		t.Fatalf("unexpected key %q", ref.Key())
	}
}
