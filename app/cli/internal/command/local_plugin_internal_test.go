package command

import (
	"context"
	"testing"
)

func TestEmbeddedLocalPluginRejectsTraversal(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"../package.json", "skills/../package.json", "/package.json"} {
		if _, err := (embeddedLocalPlugin{}).PluginFile(context.Background(), path); err == nil {
			t.Fatalf("PluginFile(%q) succeeded", path)
		}
	}
}
