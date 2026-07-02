package mcp

// ResourceMeta describes one MCP resource or template for GET /mcp/info.
type ResourceMeta struct {
	URI         string `json:"uri,omitempty"`
	URITemplate string `json:"uri_template,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mime_type,omitempty"`
}

// ToolMeta describes one MCP tool for GET /mcp/info.
type ToolMeta struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// InfoToolCatalog returns the public catalog of MCP tools (read-only UI).
// `gitLabToolsEnabled` mirrors runtime registration: repo_* are surfaced only
// when at least one connected GitLab integration exposes a repo config. The MCP
// settings UI uses this to attribute tools to the right card.
func InfoToolCatalog(gitLabToolsEnabled bool) []ToolMeta {
	var out []ToolMeta
	if gitLabToolsEnabled {
		out = append(out,
			ToolMeta{
				Name:        "repo_search",
				Description: "Search code in the configured project by keyword",
				InputSchema: map[string]any{
					"query": "string", "paths": "[string]?", "globs": "[string]?", "ref": "string?", "max_results": "int?",
				},
			},
			ToolMeta{
				Name:        "repo_context_pack",
				Description: "Return a compact cached repo context bundle for a query",
				InputSchema: map[string]any{
					"query": "string", "paths": "[string]?", "globs": "[string]?", "ref": "string?", "max_results": "int?",
				},
			},
			ToolMeta{
				Name:        "repo_list_files",
				Description: "List files and directories at a path in the repository",
				InputSchema: map[string]any{
					"path": "string?", "ref": "string?", "recursive": "bool?", "page": "int?",
				},
			},
			ToolMeta{
				Name:        "repo_list_symbols",
				Description: "List symbols in a file without bodies",
				InputSchema: map[string]any{"path": "string", "ref": "string?"},
			},
			ToolMeta{
				Name:        "repo_get_symbol",
				Description: "Get the body of a named symbol",
				InputSchema: map[string]any{"path": "string", "symbol": "string", "ref": "string?"},
			},
			ToolMeta{
				Name:        "repo_get_snippet",
				Description: "Get lines around a line number",
				InputSchema: map[string]any{"path": "string", "line": "int", "before": "int?", "after": "int?", "ref": "string?"},
			},
			ToolMeta{
				Name:        "repo_related_tests",
				Description: "Find related test files by naming convention",
				InputSchema: map[string]any{"path": "string", "ref": "string?"},
			},
			ToolMeta{
				Name:        "repo_read_file",
				Description: "Read allowlisted small files (docs, README, etc.)",
				InputSchema: map[string]any{"path": "string", "ref": "string?"},
			},
		)
	}
	out = append(out,
		ToolMeta{
			Name:        "search_knowledge",
			Description: "Semantic search over Governance Knowledge",
			InputSchema: map[string]any{
				"query": "string", "linked_feature_id": "string?", "linked_request_id": "string?",
				"document_types": "[string]?", "authority_levels": "[string]?", "limit": "int?",
			},
		},
		ToolMeta{
			Name:        "search_artifacts",
			Description: "List planning artifacts with optional filters",
			InputSchema: map[string]any{
				"feature_id": "string?", "status": "string?", "service": "string?", "limit": "int?",
			},
		},
		ToolMeta{
			Name:        "artifact_read_bundle",
			Description: "Read selected markdown files from an artifact bundle",
			InputSchema: map[string]any{"artifact_id": "string", "files": "[string]?", "max_chars": "int?"},
		},
		ToolMeta{
			Name:        "artifact_create",
			Description: "Publish a new planning artifact bundle",
			InputSchema: map[string]any{
				"feature_id": "string", "request_type": "string", "impact_level": "string", "version": "string",
				"artifact_phase": "string?", "artifact_completeness": "string?", "status_required": "string?",
				"impacted_services": "[string]", "files": "object",
			},
		},
		ToolMeta{
			Name:        "resolve_work_item",
			Description: "Resolve a tracker issue to its work item and Context Pack URI",
			InputSchema: map[string]any{
				"provider": "string", "issue_key": "string?", "issue_url": "string?",
			},
		},
		ToolMeta{
			Name:        "list_work_items",
			Description: "List ready or handed-off work items with Context Pack URIs",
			InputSchema: map[string]any{
				"ready": "bool?", "handed_off": "bool?", "work_type": "string?", "workspace_id": "string?", "mine": "bool?", "limit": "int?",
			},
		},
		ToolMeta{
			Name:        "read_delivery_review",
			Description: "Read the latest persisted delivery-review verdict for a work item",
			InputSchema: map[string]any{
				"change_request_id": "string",
			},
		},
		ToolMeta{
			Name:        "read_clarification",
			Description: "Read human clarification outcomes for blocked-ambiguity feedback on a work item",
			InputSchema: map[string]any{
				"change_request_id": "string", "since": "string?",
			},
		},
		ToolMeta{
			Name:        "draft_artifact_update",
			Description: "Open a draft-only artifact update proposal for a coding agent",
			InputSchema: map[string]any{
				"artifact_id": "string", "change_request_id": "string?", "summary": "string",
				"files": "object", "requested_by": "string?", "dedupe_key": "string?",
			},
		},
		ToolMeta{
			Name:        "report_implementation_feedback",
			Description: "Report coding-agent ambiguity, completion, or docs-update feedback for a work item",
			InputSchema: map[string]any{
				"change_request_id":    "string",
				"artifact_id":          "string?",
				"event_type":           "string",
				"severity":             "string",
				"summary":              "string",
				"evidence":             "[object]?",
				"suggested_correction": "string?",
				"affected_files":       "[string]?",
				"agent":                "object?",
				"run_id":               "string?",
				"dedupe_key":           "string?",
			},
		},
		ToolMeta{
			Name:        "run_llm_gates",
			Description: "Run all LLM quality gates for a change request's lead artifact and post verdicts to Doc Registry",
			InputSchema: map[string]any{"change_request_id": "string"},
		},
		ToolMeta{
			Name:        "trigger_delivery_review",
			Description: "Trigger the delivery review for a change request and persist the verdict for read_delivery_review",
			InputSchema: map[string]any{"change_request_id": "string"},
		},
	)
	return out
}

// InfoResourceCatalog returns MCP resources for GET /mcp/info when skills are enabled at runtime.
func InfoResourceCatalog(skillsEnabled bool) []ResourceMeta {
	if !skillsEnabled {
		return nil
	}
	return []ResourceMeta{
		{
			URI:         skillListURI,
			Name:        "skills",
			Description: "JSON list of skills (id, name, description, updated_at; no prompt).",
			MimeType:    "application/json",
		},
		{
			URITemplate: skillDetailURITemplate,
			Name:        "skill",
			Description: "JSON detail for one skill (includes prompt). Replace {id} with the skill UUID.",
			MimeType:    "application/json",
		},
	}
}
