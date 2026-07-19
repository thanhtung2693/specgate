package skills

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	storagedb "github.com/specgate/doc-registry/internal/storage/db"
)

// Skill is a user-defined skill for governance/agent prompts.
type Skill struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Prompt      string    `json:"prompt"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateInput is used for POST /skills.
type CreateInput struct {
	WorkspaceID string
	Name        string
	Description string
	Prompt      string
}

// UpdateInput is used for PUT /skills/{id} (full replace).
type UpdateInput struct {
	Name        string
	Description string
	Prompt      string
}

// Service manages skills in Postgres.
type Service struct {
	repo *storagedb.SkillRepository
}

func NewService(repo *storagedb.SkillRepository) *Service {
	return &Service{repo: repo}
}

func skillFromRow(r storagedb.SkillRow) Skill {
	return Skill{
		ID:          r.ID,
		WorkspaceID: r.WorkspaceID,
		Name:        r.Name,
		Description: r.Description,
		Prompt:      r.Prompt,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

type normalizedSkillInput struct {
	name        string
	description string
	prompt      string
}

func normalizeSkillInput(name, description, prompt string) (normalizedSkillInput, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return normalizedSkillInput{}, errors.New("name is required")
	}
	if strings.TrimSpace(prompt) == "" {
		return normalizedSkillInput{}, errors.New("prompt is required")
	}
	return normalizedSkillInput{
		name:        name,
		description: strings.TrimSpace(description),
		prompt:      prompt,
	}, nil
}

// Get returns one skill by id or a not-found error from the repository ([IsNotFound]).
func (s *Service) Get(ctx context.Context, id string) (*Skill, error) {
	workspaceID, err := requireWorkspace(ctx, "")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("id is required")
	}
	row, err := s.repo.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	sk := skillFromRow(*row)
	return &sk, nil
}

func (s *Service) List(ctx context.Context) ([]Skill, error) {
	workspaceID, err := requireWorkspace(ctx, "")
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]Skill, len(rows))
	for i := range rows {
		out[i] = skillFromRow(rows[i])
	}
	return out, nil
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*Skill, error) {
	workspaceID, err := requireWorkspace(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	fields, err := normalizeSkillInput(in.Name, in.Description, in.Prompt)
	if err != nil {
		return nil, err
	}
	row := &storagedb.SkillRow{
		ID:          uuid.NewString(),
		WorkspaceID: workspaceID,
		Name:        fields.name,
		Description: fields.description,
		Prompt:      fields.prompt,
	}
	if err := s.repo.Create(ctx, row); err != nil {
		return nil, err
	}
	sk := skillFromRow(*row)
	return &sk, nil
}

func (s *Service) Update(ctx context.Context, id string, in UpdateInput) (*Skill, error) {
	workspaceID, err := requireWorkspace(ctx, "")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("id is required")
	}
	fields, err := normalizeSkillInput(in.Name, in.Description, in.Prompt)
	if err != nil {
		return nil, err
	}
	row, err := s.repo.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	row.Name = fields.name
	row.Description = fields.description
	row.Prompt = fields.prompt
	if err := s.repo.Update(ctx, workspaceID, row); err != nil {
		return nil, err
	}
	sk := skillFromRow(*row)
	return &sk, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	workspaceID, err := requireWorkspace(ctx, "")
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, workspaceID, id)
}

// IsNotFound reports whether err is gorm.ErrRecordNotFound.
func IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
