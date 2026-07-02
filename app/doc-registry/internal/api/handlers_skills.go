package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/skills"
)

func skillDTO(s skills.Skill) SkillDTO {
	return SkillDTO{
		ID:          s.ID,
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

func (h *Handlers) ListSkills(ctx context.Context, _ *struct{}) (*ListSkillsOutput, error) {
	svc, err := h.requireSkills()
	if err != nil {
		return nil, err
	}
	list, err := svc.List(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("list skills", err)
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
	s, err := svc.Create(ctx, skillCreateInput(in))
	if err != nil {
		return nil, huma.Error400BadRequest("create skill", err)
	}
	return newCreateSkillOutput(s), nil
}

func (h *Handlers) UpdateSkill(ctx context.Context, in *UpdateSkillInput) (*UpdateSkillOutput, error) {
	svc, err := h.requireSkills()
	if err != nil {
		return nil, err
	}
	s, err := svc.Update(ctx, in.ID, skillUpdateInput(in))
	if err != nil {
		if skills.IsNotFound(err) {
			return nil, skillNotFoundError()
		}
		return nil, huma.Error400BadRequest("update skill", err)
	}
	return newUpdateSkillOutput(s), nil
}

func (h *Handlers) DeleteSkill(ctx context.Context, in *DeleteSkillInput) (*DeleteSkillOutput, error) {
	svc, err := h.requireSkills()
	if err != nil {
		return nil, err
	}
	if err := svc.Delete(ctx, in.ID); err != nil {
		if skills.IsNotFound(err) {
			return nil, skillNotFoundError()
		}
		return nil, huma.Error500InternalServerError("delete skill", err)
	}
	return newDeleteSkillOutput(), nil
}
