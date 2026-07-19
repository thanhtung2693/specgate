package e2e

import (
	"os"
	"strings"
	"testing"
)

func TestHandoffCompletionNamesItsCodingAgent(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("handoff.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	if !strings.Contains(script, `--arg agent "SpecGate CLI E2E"`) {
		t.Fatal("handoff smoke does not declare a stable coding-agent name")
	}
	if !strings.Contains(script, `.agent.name = $agent`) {
		t.Fatal("handoff smoke does not write the coding-agent name into its completion report")
	}
}

func TestHandoffAcceptsOnlySuccessOrGovernanceExitForReviewReadback(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("handoff.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, prefix := range []string{"SUBMIT", "STATUS"} {
		for _, want := range []string{
			prefix + "_CODE=0",
			"|| " + prefix + "_CODE=$?",
			`[ "$` + prefix + `_CODE" -ne 0 ] && [ "$` + prefix + `_CODE" -ne 1 ]`,
		} {
			if !strings.Contains(script, want) {
				t.Errorf("handoff smoke missing %q", want)
			}
		}
	}
}
