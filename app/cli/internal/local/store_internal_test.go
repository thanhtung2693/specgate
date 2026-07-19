package local

import (
	"net/url"
	"strings"
	"testing"
)

func TestSQLiteDSNBuildsHierarchicalWindowsFileURIAndEscapesPath(t *testing.T) {
	dsn := sqliteDSN("C:/Users/Jane #1/spec?gate.db")

	if strings.HasPrefix(dsn, "file:C:") {
		t.Fatalf("Windows drive path became an opaque URI: %s", dsn)
	}
	if !strings.Contains(dsn, "%23") || !strings.Contains(dsn, "%3F") {
		t.Fatalf("reserved path characters are not escaped: %s", dsn)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "file" || parsed.Path != "/C:/Users/Jane #1/spec?gate.db" {
		t.Fatalf("parsed URI = %#v", parsed)
	}
	if parsed.Query().Get("_txlock") != "immediate" {
		t.Fatalf("SQLite options missing from %s", dsn)
	}
}
