package command

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/local"
)

// A bundle stays internally consistent or says why not. Re-deriving the verdict
// from the evidence the bundle carries is what protects a reviewer from a
// stored verdict produced under different enforcement rules — the checksum only
// proves the file was not edited afterwards.
func TestDeliveryHandoffVerdictDisagreementIsDetected(t *testing.T) {
	bundle := deliveryHandoffBundle{
		SchemaVersion: deliveryHandoffSchemaVersion,
		Work: local.WorkItem{
			Key:                "LOCAL-1",
			Title:              "Fix timeout",
			AcceptanceCriteria: []string{"Retries stop @check:unit"},
		},
		Review: local.DeliveryReview{Verdict: "passed", Summary: "recorded before bindings were enforced"},
		Report: local.DeliveryReport{Body: map[string]any{
			"criteria": []any{map[string]any{
				"criterion_id": "local-1", "claim": "satisfied",
				"evidence": map[string]any{"heading": "checked by hand"},
			}},
			"checks": []any{map[string]any{"name": "unit", "status": "skipped"}},
		}},
	}

	verdict, summary := local.DeliveryVerdict(bundle.Report.Body, bundle.Work.AcceptanceCriteria)
	if verdict != "failed" {
		t.Fatalf("recomputed verdict = %q, want failed", verdict)
	}
	if verdict == bundle.Review.Verdict {
		t.Fatal("test fixture no longer models a disagreement")
	}
	if !strings.Contains(summary, "unit") {
		t.Fatalf("recomputed summary = %q, want the bound check named", summary)
	}
}

func TestDeliveryHandoffChecksumCoversPayloadNotItself(t *testing.T) {
	bundle := deliveryHandoffBundle{
		SchemaVersion: deliveryHandoffSchemaVersion,
		Work:          local.WorkItem{Key: "LOCAL-1", AcceptanceCriteria: []string{"Retries stop"}},
		Review:        local.DeliveryReview{Verdict: "passed"},
		Report:        local.DeliveryReport{Body: map[string]any{"checks": []any{}}},
	}
	sum := deliveryHandoffChecksum(bundle)
	if sum == "" {
		t.Fatal("checksum is empty")
	}

	// Recording the checksum on the bundle must not change the checksum.
	bundle.Checksum = sum
	if again := deliveryHandoffChecksum(bundle); again != sum {
		t.Fatalf("checksum is not self-excluding: %q then %q", sum, again)
	}

	bundle.Review.Verdict = "failed"
	if changed := deliveryHandoffChecksum(bundle); changed == sum {
		t.Fatal("checksum did not change when the payload changed")
	}
}
