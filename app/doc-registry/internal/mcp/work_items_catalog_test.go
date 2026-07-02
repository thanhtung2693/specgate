package mcp

import "testing"

func TestInfoToolCatalog_IncludesWorkItemTools(t *testing.T) {
	t.Parallel()

	tools := InfoToolCatalog(false)
	want := map[string]bool{
		"resolve_work_item":    false,
		"list_work_items":      false,
		"read_delivery_review": false,
		"read_clarification":   false,
	}
	for _, tool := range tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("%s not found in catalog", name)
		}
	}
}

func TestInfoToolCatalog_ListWorkItemsIncludesWorkspaceFilters(t *testing.T) {
	t.Parallel()

	tools := InfoToolCatalog(false)
	for _, tool := range tools {
		if tool.Name != "list_work_items" {
			continue
		}
		if got := tool.InputSchema["workspace_id"]; got != "string?" {
			t.Fatalf("workspace_id schema = %v, want string?", got)
		}
		if got := tool.InputSchema["work_type"]; got != "string?" {
			t.Fatalf("work_type schema = %v, want string?", got)
		}
		return
	}
	t.Fatal("list_work_items not found in catalog")
}
