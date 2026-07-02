// Package seeding provides deploy-time idempotent seeding of starter rubric skills.
package seeding

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/specgate/doc-registry/internal/settings"
	"github.com/specgate/doc-registry/internal/skills"
)

type Result struct {
	SkillsCreated  []string // names of newly created skills
	SkillsUpdated  []string // names of existing skills overwritten by an explicit sync run
	SkillsExisting []string // names of skills already present
}

type SkillSeedOptions struct {
	OverwriteExisting bool
}

// seedSkill is the JSON shape for one entry in skills_seed.json.
type seedSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
}

// seedDocument is the top-level shape of skills_seed.json.
type seedDocument struct {
	Skills []seedSkill `json:"skills"`
}

// SeedSkills creates missing starter rubric skills from skills_seed.json. It is
// fully idempotent: a second call with the same database produces zero changes
// and zero errors. Existing skills are treated as user-managed and are never
// overwritten by seed defaults.
func SeedSkills(
	ctx context.Context,
	skillsSvc *skills.Service,
	settingsSvc *settings.Service,
	logger *zerolog.Logger,
) (Result, error) {
	return SeedSkillsWithOptions(ctx, skillsSvc, settingsSvc, logger, SkillSeedOptions{})
}

// SeedSkillsWithOptions creates missing starter rubric skills and, when
// explicitly requested, overwrites matching existing starter rows by stable
// skill name. Startup seeding should keep the default options so edited
// team-managed Skills are preserved.
func SeedSkillsWithOptions(
	ctx context.Context,
	skillsSvc *skills.Service,
	settingsSvc *settings.Service,
	logger *zerolog.Logger,
	opts SkillSeedOptions,
) (Result, error) {
	var doc seedDocument
	if err := json.Unmarshal(skillsSeedJSON, &doc); err != nil {
		return Result{}, fmt.Errorf("seeding: parse skills_seed.json: %w", err)
	}

	result, err := seedSkills(ctx, skillsSvc, doc.Skills, logger, opts)
	if err != nil {
		return result, err
	}

	return result, nil
}

// seedSkills creates missing skills. Matching is by stable skill name; once a
// skill exists, later seed runs leave it untouched so teams can edit the starter
// rubric in place.
func seedSkills(
	ctx context.Context,
	svc *skills.Service,
	seedList []seedSkill,
	logger *zerolog.Logger,
	opts SkillSeedOptions,
) (Result, error) {
	existing, err := svc.List(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("seeding: list skills: %w", err)
	}

	byName := make(map[string]skills.Skill, len(existing))
	for _, sk := range existing {
		byName[sk.Name] = sk
	}

	var result Result

	for _, s := range seedList {
		existing, exists := byName[s.Name]
		if exists {
			if opts.OverwriteExisting {
				if existing.Description == s.Description && existing.Prompt == s.Prompt {
					result.SkillsExisting = append(result.SkillsExisting, s.Name)
					continue
				}
				if _, err := svc.Update(ctx, existing.ID, skills.UpdateInput{
					Name:        s.Name,
					Description: s.Description,
					Prompt:      s.Prompt,
				}); err != nil {
					return result, fmt.Errorf("seeding: update skill %q: %w", s.Name, err)
				}
				result.SkillsUpdated = append(result.SkillsUpdated, s.Name)
				logger.Info().Str("skill", s.Name).Msg("seed: updated skill")
				continue
			}
			result.SkillsExisting = append(result.SkillsExisting, s.Name)
			continue
		}
		_, err := svc.Create(ctx, skills.CreateInput{
			Name:        s.Name,
			Description: s.Description,
			Prompt:      s.Prompt,
		})
		if err != nil {
			return result, fmt.Errorf("seeding: create skill %q: %w", s.Name, err)
		}
		result.SkillsCreated = append(result.SkillsCreated, s.Name)
		logger.Info().Str("skill", s.Name).Msg("seed: created skill")
	}

	return result, nil
}
