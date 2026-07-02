package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/specgate/doc-registry/internal/governanceprofile"
)

type fakeProfileCatalog struct {
	listFn   func(context.Context) ([]governanceprofile.ResolvedProfile, error)
	importFn func(context.Context, []governanceprofile.ImportInput) ([]governanceprofile.ResolvedProfile, error)
}

func (f *fakeProfileCatalog) ListProfiles(ctx context.Context) ([]governanceprofile.ResolvedProfile, error) {
	return f.listFn(ctx)
}

func (f *fakeProfileCatalog) ImportProfiles(ctx context.Context, in []governanceprofile.ImportInput) ([]governanceprofile.ResolvedProfile, error) {
	return f.importFn(ctx, in)
}

func TestSpecgateListProfilesHandler(t *testing.T) {
	t.Parallel()

	handler := NewSpecgateListProfilesHandler(&fakeProfileCatalog{
		listFn: func(context.Context) ([]governanceprofile.ResolvedProfile, error) {
			return []governanceprofile.ResolvedProfile{{FullKey: "generic_change", Version: "1"}}, nil
		},
	})
	out, err := handler(context.Background(), struct{}{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var got SpecgateListProfilesResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].FullKey != "generic_change" {
		t.Fatalf("unexpected items: %+v", got.Items)
	}
}

func TestSpecgateImportProfilesHandler(t *testing.T) {
	t.Parallel()

	handler := NewSpecgateImportProfilesHandler(&fakeProfileCatalog{
		importFn: func(_ context.Context, in []governanceprofile.ImportInput) ([]governanceprofile.ResolvedProfile, error) {
			if len(in) != 1 || in[0].Namespace != "checkout-team" || in[0].Key != "bug_fix" {
				t.Fatalf("unexpected import input: %+v", in)
			}
			return []governanceprofile.ResolvedProfile{{FullKey: "checkout-team/bug_fix", Version: "2"}}, nil
		},
	})
	out, err := handler(context.Background(), SpecgateImportProfilesInput{
		Profiles: []SpecgateImportProfileDTO{{
			Namespace: "checkout-team",
			Key:       "bug_fix",
			Version:   "2",
			Definition: governanceprofile.Definition{
				DisplayName:    "Bug fix",
				ChangeType:     "bug_fix",
				RequiredRoles:  []string{"spec"},
				RequiredTopics: []string{"goal"},
				EnabledGates:   []string{"spec_completeness"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var got SpecgateImportProfilesResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].FullKey != "checkout-team/bug_fix" {
		t.Fatalf("unexpected items: %+v", got.Items)
	}
}
