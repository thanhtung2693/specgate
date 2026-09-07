package command

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestValidateArtifactPublishFieldsAcceptsOnlySourceCriterionSchema(t *testing.T) {
	body := map[string]any{
		"feature_key": "source-coverage", "request_type": "new_feature",
		"documents": []any{map[string]any{"path": "spec.md", "role": "spec", "content": "# Spec"}},
		"source_criteria": []any{map[string]any{"id": "req-1", "text": "Result", "source_path": "spec.md"}},
	}
	if err := validateArtifactPublishFields(body); err != nil {
		t.Fatalf("source criteria rejected: %v", err)
	}
	body["source_criteria"] = []any{map[string]any{"id": "req-1", "text": "Result", "source_path": "spec.md", "guessed": true}}
	if err := validateArtifactPublishFields(body); err == nil || !strings.Contains(err.Error(), "source_criteria[0].guessed") {
		t.Fatalf("unknown source criterion field error = %v", err)
	}
}

func TestLocalArtifactInputReadsDeferredSourceCriterion(t *testing.T) {
	input, err := localArtifactInput(map[string]any{
		"feature_key": "source-coverage", "request_type": "new_feature",
		"documents": []any{map[string]any{"path": "spec.md", "role": "spec", "content": "# Spec"}},
		"source_criteria": []any{map[string]any{"id": "req-1", "text": "Later", "source_path": "spec.md", "deferred_reason": "Out of scope"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := input.SourceCriteria; len(got) != 1 || got[0].DeferredReason != "Out of scope" {
		t.Fatalf("source criteria = %#v", got)
	}
	view := localArtifactView(local.Artifact{SourceCriteria: input.SourceCriteria})
	if got := view["source_criteria"].([]local.SourceCriterion); len(got) != 1 || got[0].ID != "req-1" {
		t.Fatalf("view source criteria = %#v", got)
	}
}
