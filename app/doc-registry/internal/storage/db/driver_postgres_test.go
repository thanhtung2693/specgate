package db

import (
	"bytes"
	"context"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestPostgresLoggerSuppressesExpectedNotFound(t *testing.T) {
	var output bytes.Buffer
	postgresLogger(&output).Trace(context.Background(), time.Now(), func() (string, int64) {
		return "SELECT 1", 0
	}, gorm.ErrRecordNotFound)

	if output.Len() != 0 {
		t.Fatalf("record-not-found log = %q, want none", output.String())
	}
}
