package governanceops

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/workboard"
	"github.com/specgate/doc-registry/internal/workspace"
)

type fakeWorkItemStore struct {
	features map[string]*workboard.Feature // by id and by key
	created  *workboard.ChangeRequest
}

func (f *fakeWorkItemStore) GetFeature(_ context.Context, id string) (*workboard.Feature, error) {
	if ft, ok := f.features[id]; ok {
		return ft, nil
	}
	return nil, workboard.ErrNotFound
}

func (f *fakeWorkItemStore) GetFeatureByKey(_ context.Context, key string) (*workboard.Feature, error) {
	if ft, ok := f.features[key]; ok {
		return ft, nil
	}
	return nil, workboard.ErrNotFound
}

func (f *fakeWorkItemStore) CreateChangeRequest(_ context.Context, in workboard.ChangeRequest) (*workboard.ChangeRequest, error) {
	in.ID = "cr-created"
	in.Key = "CR-CREATED"
	f.created = &in
	return &in, nil
}

func newWorkCreateService(store *fakeWorkItemStore, files map[string]string) *Service {
	return &Service{
		WorkItems: store,
		Artifacts: &fakeContextPackArtifactReader{
			art: &artifact.Artifact{
				ID:        "art-canon",
				FeatureID: "feat-1",
				Status:    artifact.StatusApproved,
				Files:     []artifact.File{{ArtifactID: "art-canon", Path: "spec.md", Role: artifact.RoleSpec}},
			},
			files: files,
		},
	}
}

func TestCreateWorkItem_BindsFeatureAndCanonical(t *testing.T) {
	t.Parallel()
	feat := &workboard.Feature{ID: "feat-1", Key: "my-feature", Name: "My Feature", CanonicalArtifactID: "art-canon"}
	store := &fakeWorkItemStore{features: map[string]*workboard.Feature{"feat-1": feat, "my-feature": feat}}
	svc := newWorkCreateService(store, map[string]string{"spec.md": "# Spec\n"})

	out, err := svc.CreateWorkItem(context.Background(), CreateWorkItemInput{
		Feature: "my-feature", Title: "Do the thing",
		AcceptanceCriteria: []string{"Explicit AC"},
		CreatedBy:          "dev",
		SourceRefs:         []string{"specgate-local-work:local-work"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.created.FeatureID != "feat-1" || store.created.LeadArtifactID != "art-canon" {
		t.Fatalf("created CR = %+v, want feature feat-1 + lead art-canon", store.created)
	}
	if store.created.CreatedBy != "dev" || store.created.Title != "Do the thing" {
		t.Fatalf("attribution/title wrong: %+v", store.created)
	}
	if store.created.SourceRefs != `["specgate-local-work:local-work"]` {
		t.Fatalf("source refs = %s", store.created.SourceRefs)
	}
	if out.ChangeRequestKey != "CR-CREATED" || out.FeatureKey != "my-feature" || out.LeadArtifactID != "art-canon" {
		t.Fatalf("result = %+v", out)
	}
	if len(out.AcceptanceCriteria) != 1 || out.AcceptanceCriteria[0] != "Explicit AC" {
		t.Fatalf("acs = %v", out.AcceptanceCriteria)
	}
}

func TestCreateWorkItemParsesTrailingCheckBinding(t *testing.T) {
	t.Parallel()
	feat := &workboard.Feature{ID: "feat-1", Key: "my-feature", Name: "My Feature", CanonicalArtifactID: "art-canon"}
	store := &fakeWorkItemStore{features: map[string]*workboard.Feature{"my-feature": feat}}
	svc := newWorkCreateService(store, map[string]string{"spec.md": "# Spec\n"})

	out, err := svc.CreateWorkItem(context.Background(), CreateWorkItemInput{
		Feature:            "my-feature",
		Title:              "Do the thing",
		AcceptanceCriteria: []string{"Tests pass @check:tests"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var criteria []struct {
		Text                string `json:"text"`
		VerificationBinding string `json:"verification_binding"`
	}
	if err := json.Unmarshal([]byte(store.created.AcceptanceCriteria), &criteria); err != nil {
		t.Fatalf("decode stored criteria: %v", err)
	}
	if len(criteria) != 1 || criteria[0].Text != "Tests pass" || criteria[0].VerificationBinding != "tests" {
		t.Fatalf("stored criteria = %+v, want stripped text with tests binding", criteria)
	}
	if len(out.AcceptanceCriteria) != 1 || out.AcceptanceCriteria[0] != "Tests pass" {
		t.Fatalf("result criteria = %+v, want marker-free text", out.AcceptanceCriteria)
	}
}

func TestCreateWorkItemRejectsEmptyFinalAcceptanceCriteria(t *testing.T) {
	t.Parallel()
	feat := &workboard.Feature{ID: "feat-1", Key: "my-feature", CanonicalArtifactID: "art-canon"}
	store := &fakeWorkItemStore{features: map[string]*workboard.Feature{"my-feature": feat}}
	svc := newWorkCreateService(store, map[string]string{"spec.md": "# Spec\n"})

	_, err := svc.CreateWorkItem(context.Background(), CreateWorkItemInput{Feature: "my-feature", Title: "T"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want validation error", err)
	}
	if store.created != nil {
		t.Fatalf("created = %#v, want nil", store.created)
	}
}

func TestCreateWorkItem_UnknownFeatureNotFound(t *testing.T) {
	t.Parallel()
	svc := newWorkCreateService(&fakeWorkItemStore{features: map[string]*workboard.Feature{}}, nil)
	_, err := svc.CreateWorkItem(context.Background(), CreateWorkItemInput{Feature: "nope", Title: "T"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCreateWorkItem_FeatureWithoutCanonicalIsValidationError(t *testing.T) {
	t.Parallel()
	feat := &workboard.Feature{ID: "feat-1", Key: "my-feature"} // no canonical
	store := &fakeWorkItemStore{features: map[string]*workboard.Feature{"my-feature": feat}}
	svc := newWorkCreateService(store, nil)
	_, err := svc.CreateWorkItem(context.Background(), CreateWorkItemInput{Feature: "my-feature", Title: "T"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestCreateWorkItemRejectsWorkspaceMismatchedWithTrustedContext(t *testing.T) {
	t.Parallel()
	feat := &workboard.Feature{ID: "feat-1", Key: "my-feature", WorkspaceID: "ws-a", CanonicalArtifactID: "art-canon"}
	store := &fakeWorkItemStore{features: map[string]*workboard.Feature{"my-feature": feat}}
	svc := newWorkCreateService(store, nil)

	_, err := svc.CreateWorkItem(workspace.WithID(context.Background(), "ws-a"), CreateWorkItemInput{
		Feature:     "my-feature",
		Title:       "T",
		WorkspaceID: "ws-b",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want workspace validation error", err)
	}
	if store.created != nil {
		t.Fatalf("created = %#v, want no change request", store.created)
	}
}

func TestCreateWorkItemRejectsCrossWorkspaceCanonicalArtifact(t *testing.T) {
	t.Parallel()
	feat := &workboard.Feature{ID: "feat-a", Key: "feature-a", WorkspaceID: "ws-a", CanonicalArtifactID: "art-b"}
	store := &fakeWorkItemStore{features: map[string]*workboard.Feature{"feature-a": feat}}
	svc := &Service{
		WorkItems: store,
		Artifacts: &fakeContextPackArtifactReader{
			art:   &artifact.Artifact{ID: "art-b", WorkspaceID: "ws-b"},
			files: map[string]string{},
		},
	}

	_, err := svc.CreateWorkItem(workspace.WithID(context.Background(), "ws-a"), CreateWorkItemInput{
		Feature: "feature-a",
		Title:   "T",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want workspace-scoped not found", err)
	}
	if store.created != nil {
		t.Fatalf("created = %#v, want no change request", store.created)
	}
}

func TestCreateWorkItem_BindsExplicitApprovedArtifactForPortableImport(t *testing.T) {
	t.Parallel()
	feat := &workboard.Feature{
		ID: "feat-1", Key: "my-feature", WorkspaceID: "ws-a", CanonicalArtifactID: "art-v2",
	}
	store := &fakeWorkItemStore{features: map[string]*workboard.Feature{"my-feature": feat}}
	svc := &Service{
		WorkItems: store,
		Artifacts: &fakeContextPackArtifactReader{
			art: &artifact.Artifact{
				ID: "art-v1", FeatureID: "feat-1", WorkspaceID: "ws-a", Status: artifact.StatusSuperseded,
			},
		},
	}

	out, err := svc.CreateWorkItem(workspace.WithID(context.Background(), "ws-a"), CreateWorkItemInput{
		Feature: "my-feature", ArtifactID: "art-v1", Title: "Preserved work",
		AcceptanceCriteria: []string{"Uses v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.created.LeadArtifactID != "art-v1" || out.LeadArtifactID != "art-v1" {
		t.Fatalf("explicit artifact was not preserved: created=%+v out=%+v", store.created, out)
	}
}
