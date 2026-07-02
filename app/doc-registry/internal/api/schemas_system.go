package api

import "time"

// ---------- GET /mcp/info ----------

// McpToolDTO describes one MCP tool for the settings UI.
type McpToolDTO struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// McpResourceDTO describes one MCP resource or URI template for the settings UI.
type McpResourceDTO struct {
	URI         string `json:"uri,omitempty"`
	URITemplate string `json:"uri_template,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mime_type,omitempty"`
}

// McpInfoResponse is the body for GET /mcp/info.
type McpInfoResponse struct {
	Body struct {
		Addr            string           `json:"addr"`
		RestartRequired bool             `json:"restart_required"`
		Tools           []McpToolDTO     `json:"tools"`
		Resources       []McpResourceDTO `json:"resources,omitempty"`
	}
}

// ---------- GET /settings ----------

type GetSettingsOutput struct {
	Body struct {
		Settings map[string]string `json:"settings"`
	}
}

// ---------- PUT /settings ----------

type UpdateSettingsInput struct {
	Body struct {
		Settings map[string]string `json:"settings" required:"true" doc:"key-value pairs to update"`
	}
}

type UpdateSettingsOutput struct {
	Body struct {
		Settings map[string]string `json:"settings"`
	}
}

// ---------- GET /skills ----------

// ListSkillsOutput is the body for GET /skills.
type ListSkillsOutput struct {
	Body struct {
		Items []SkillDTO `json:"items"`
	} `json:"body"` // explicit tag: default Go encoding used "Body" and broke clients expecting lowercase
}

// SkillDTO is the API representation of a stored skill (table: skills).
type SkillDTO struct {
	ID          string    `json:"id" doc:"UUID"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Prompt      string    `json:"prompt"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ---------- POST /skills ----------

type CreateSkillInput struct {
	Body struct {
		Name        string `json:"name" required:"true"`
		Description string `json:"description"`
		Prompt      string `json:"prompt" required:"true"`
	}
}

type CreateSkillOutput struct {
	Body SkillDTO `json:"body"`
}

// ---------- PUT /skills/{id} ----------

type UpdateSkillInput struct {
	ID   string `path:"id"`
	Body struct {
		Name        string `json:"name" required:"true"`
		Description string `json:"description"`
		Prompt      string `json:"prompt" required:"true"`
	}
}

type UpdateSkillOutput struct {
	Body SkillDTO `json:"body"`
}

// ---------- DELETE /skills/{id} ----------

type DeleteSkillInput struct {
	ID string `path:"id"`
}

type DeleteSkillOutput struct {
	Body struct {
		OK bool `json:"ok"`
	} `json:"body"`
}
