package command

import "testing"

func TestNewLocalCommandsAreDeclaredInCapabilityManifest(t *testing.T) {
	root := NewRootCommand(DefaultDeps())
	for _, path := range [][]string{{"work", "checkpoint"}, {"artifact", "impact"}} {
		cmd, _, err := root.Find(path)
		if err != nil || cmd == nil || !localCommandSupported(cmd) {
			t.Fatalf("Local command %v is not declared in capabilities: %v", path, err)
		}
	}
}
