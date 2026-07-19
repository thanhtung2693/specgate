package observability

import (
	"testing"

	"github.com/specgate/doc-registry/internal/config"
)

func TestInitSentry_EmptyDSN_NoOp(t *testing.T) {
	t.Parallel()
	cleanup, err := InitSentry(&config.Config{Sentry: config.SentryConfig{}})
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
}
