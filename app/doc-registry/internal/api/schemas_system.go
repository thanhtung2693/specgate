package api

import "time"

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

type ListSkillsInput struct {
	WorkspaceID string `query:"workspace_id" required:"true"`
}

// ListSkillsOutput is the body for GET /skills.
type ListSkillsOutput struct {
	Body struct {
		Items []SkillDTO `json:"items"`
	} `json:"body"` // explicit tag: default Go encoding used "Body" and broke clients expecting lowercase
}

// SkillDTO is the API representation of a stored skill (table: skills).
type SkillDTO struct {
	ID          string    `json:"id" doc:"UUID"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Prompt      string    `json:"prompt"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ---------- POST /skills ----------

type CreateSkillInput struct {
	Body struct {
		WorkspaceID string `json:"workspace_id" required:"true"`
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
	ID          string `path:"id"`
	WorkspaceID string `query:"workspace_id" required:"true"`
	Body        struct {
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
	ID          string `path:"id"`
	WorkspaceID string `query:"workspace_id" required:"true"`
}

type DeleteSkillOutput struct {
	Body struct {
		OK bool `json:"ok"`
	} `json:"body"`
}
