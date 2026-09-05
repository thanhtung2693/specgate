package local_test

import (
	"database/sql"
	"errors"
	"github.com/specgate/specgate/app/cli/internal/local"
	"testing"
)

func localReviewID(t *testing.T, store *local.Store, workspaceID, ref string) string {
	t.Helper()
	review, err := store.DeliveryStatus(t.Context(), workspaceID, ref)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	return review.ID
}

func TestDeliveryVerdictRejectsContradictoryDuplicateCriteria(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		criteria := []any{
			map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "verified"}},
			map[string]any{"criterion_id": "local-1", "claim": "not_done"},
		}
		if reverse {
			criteria[0], criteria[1] = criteria[1], criteria[0]
		}
		verdict, summary := local.DeliveryVerdict(map[string]any{"criteria": criteria}, []string{"Works"})
		if verdict != "failed" {
			t.Fatalf("contradictory criteria passed: %s (%s)", verdict, summary)
		}
	}
}
