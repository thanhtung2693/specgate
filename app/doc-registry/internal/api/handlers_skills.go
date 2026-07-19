package api

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/skills"
)

func skillDTO(s skills.Skill) SkillDTO {
	return SkillDTO{
		ID:          s.ID,
		WorkspaceID: s.WorkspaceID,
		Name:        s.Name,
		Description: s.Description,
		Prompt:      s.Prompt,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
	}
}

func skillDTOs(list []skills.Skill) []SkillDTO {
	out := make([]SkillDTO, len(list))
	for i := range list {
		out[i] = skillDTO(list[i])
	}
	return out
}

func skillCreateInput(in *CreateSkillInput) skills.CreateInput {
	return skills.CreateInput{
		WorkspaceID: in.Body.WorkspaceID,
		Name:        in.Body.Name,
		Description: in.Body.Description,
		Prompt:      in.Body.Prompt,
	}
}

func skillUpdateInput(in *UpdateSkillInput) skills.UpdateInput {
	return skills.UpdateInput{
		Name:        in.Body.Name,
		Description: in.Body.Description,
		Prompt:      in.Body.Prompt,
	}
}

func skillNotFoundError() error {
	return huma.Error404NotFound("skill not found")
}

func newCreateSkillOutput(s *skills.Skill) *CreateSkillOutput {
	out := &CreateSkillOutput{}
	out.Body = skillDTO(*s)
	return out
}

func newUpdateSkillOutput(s *skills.Skill) *UpdateSkillOutput {
	out := &UpdateSkillOutput{}
	out.Body = skillDTO(*s)
	return out
}

func newDeleteSkillOutput() *DeleteSkillOutput {
	out := &DeleteSkillOutput{}
	out.Body.OK = true
	return out
}

func (h *Handlers) requireSkills() (*skills.Service, error) {
	if h.Skills == nil {
		return nil, huma.Error503ServiceUnavailable("skills registry not configured")
	}
	return h.Skills, nil
}

func skillWorkspaceContext(ctx context.Context, workspaceID string) (context.Context, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return nil, err
	}
	return skills.WithWorkspace(ctx, workspaceID), nil
}

func mapSkillError(op string, err error) error {
	switch {
	case errors.Is(err, skills.ErrWorkspaceRequired), errors.Is(err, skills.ErrWorkspaceMismatch):
		return huma.Error400BadRequest(op, err)
	case skills.IsNotFound(err):
		return skillNotFoundError()
	default:
		return huma.Error500InternalServerError(op, err)
	}
}

func (h *Handlers) ListSkills(ctx context.Context, in *ListSkillsInput) (*ListSkillsOutput, error) {
	svc, err := h.requireSkills()
	if err != nil {
		return nil, err
	}
	ctx, err = skillWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	list, err := svc.List(ctx)
	if err != nil {
		return nil, mapSkillError("list skills", err)
	}
	out := &ListSkillsOutput{}
	out.Body.Items = skillDTOs(list)
	return out, nil
}

func (h *Handlers) CreateSkill(ctx context.Context, in *CreateSkillInput) (*CreateSkillOutput, error) {
	svc, err := h.requireSkills()
	if err != nil {
		return nil, err
	}
	ctx, err = skillWorkspaceContext(ctx, in.Body.WorkspaceID)
	if err != nil {
		return nil, err
	}
	s, err := svc.Create(ctx, skillCreateInput(in))
	if err != nil {
		return nil, mapSkillError("create skill", err)
	}
	return newCreateSkillOutput(s), nil
}

func (h *Handlers) UpdateSkill(ctx context.Context, in *UpdateSkillInput) (*UpdateSkillOutput, error) {
	svc, err := h.requireSkills()
	if err != nil {
		return nil, err
	}
	ctx, err = skillWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	s, err := svc.Update(ctx, in.ID, skillUpdateInput(in))
	if err != nil {
		return nil, mapSkillError("update skill", err)
	}
	return newUpdateSkillOutput(s), nil
}

func (h *Handlers) DeleteSkill(ctx context.Context, in *DeleteSkillInput) (*DeleteSkillOutput, error) {
	svc, err := h.requireSkills()
	if err != nil {
		return nil, err
	}
	ctx, err = skillWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := svc.Delete(ctx, in.ID); err != nil {
		return nil, mapSkillError("delete skill", err)
	}
	return newDeleteSkillOutput(), nil
}
