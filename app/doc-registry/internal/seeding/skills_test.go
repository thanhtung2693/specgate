package seeding_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/specgate/doc-registry/internal/seeding"
	"github.com/specgate/doc-registry/internal/settings"
	"github.com/specgate/doc-registry/internal/skills"
)

// Skills pruned to the starter rubric set (gate-consumes-Skills). SeedSkills
// manages skills only.
const wantSkillCount = 6

func discardLogger() *zerolog.Logger {
	l := zerolog.Nop()
	return &l
}

// TestSeedSkills_FreshDB verifies that on a fresh migrated database all starter
// skills are created and no overlays are touched (the seed carries none).
func TestSeedSkills_FreshDB(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, name string, skillsSvc *skills.Service, settingsSvc *settings.Service) {

		result, err := seeding.SeedSkills(context.Background(), skillsSvc, settingsSvc, discardLogger())
		if err != nil {
			t.Fatalf("[%s] SeedSkills: %v", name, err)
		}

		if got := len(result.SkillsCreated); got != wantSkillCount {
			t.Errorf("[%s] SkillsCreated=%d, want %d", name, got, wantSkillCount)
		}
		if got := len(result.SkillsUpdated); got != 0 {
			t.Errorf("[%s] SkillsUpdated=%d, want 0", name, got)
		}
		if got := len(result.SkillsExisting); got != 0 {
			t.Errorf("[%s] SkillsExisting=%d, want 0", name, got)
		}

		// Verify all skills are actually in the DB.
		all, err := skillsSvc.List(context.Background())
		if err != nil {
			t.Fatalf("[%s] List skills: %v", name, err)
		}
		if len(all) != wantSkillCount {
			t.Errorf("[%s] DB has %d skills, want %d", name, len(all), wantSkillCount)
		}
	})
}

// TestSeedSkills_Idempotent verifies that a second run produces zero changes.
func TestSeedSkills_Idempotent(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, name string, skillsSvc *skills.Service, settingsSvc *settings.Service) {

		// First run.
		if _, err := seeding.SeedSkills(context.Background(), skillsSvc, settingsSvc, discardLogger()); err != nil {
			t.Fatalf("[%s] first SeedSkills: %v", name, err)
		}

		// Second run must report every skill as existing, none created/updated.
		result, err := seeding.SeedSkills(context.Background(), skillsSvc, settingsSvc, discardLogger())
		if err != nil {
			t.Fatalf("[%s] second SeedSkills: %v", name, err)
		}

		if got := len(result.SkillsCreated); got != 0 {
			t.Errorf("[%s] second run: SkillsCreated=%d, want 0", name, got)
		}
		if got := len(result.SkillsUpdated); got != 0 {
			t.Errorf("[%s] second run: SkillsUpdated=%d, want 0", name, got)
		}
		if got := len(result.SkillsExisting); got != wantSkillCount {
			t.Errorf("[%s] second run: SkillsExisting=%d, want %d", name, got, wantSkillCount)
		}
	})
}

// TestSeedSkills_Partial verifies that pre-existing skills are treated as
// user-managed starter copies and the remainder are created.
func TestSeedSkills_Partial(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, name string, skillsSvc *skills.Service, settingsSvc *settings.Service) {

		// Pre-create one kept skill by hand.
		mustCreateSkill(t, skillsSvc, "prd-review")

		result, err := seeding.SeedSkills(context.Background(), skillsSvc, settingsSvc, discardLogger())
		if err != nil {
			t.Fatalf("[%s] SeedSkills: %v", name, err)
		}

		if got := len(result.SkillsCreated); got != wantSkillCount-1 {
			t.Errorf("[%s] SkillsCreated=%d, want %d", name, got, wantSkillCount-1)
		}
		if got := len(result.SkillsUpdated); got != 0 {
			t.Errorf("[%s] SkillsUpdated=%d, want 0", name, got)
		}
		if got := len(result.SkillsExisting); got != 1 {
			t.Errorf("[%s] SkillsExisting=%d, want 1", name, got)
		}
		if result.SkillsExisting[0] != "prd-review" {
			t.Errorf("[%s] SkillsExisting[0]=%q, want %q", name, result.SkillsExisting[0], "prd-review")
		}

		got, err := findSkillByName(t, skillsSvc, "prd-review")
		if err != nil {
			t.Fatalf("[%s] find prd-review: %v", name, err)
		}
		if got.Description != "test description" || got.Prompt != "test prompt text" {
			t.Errorf("[%s] prd-review overwritten: description=%q prompt=%q", name, got.Description, got.Prompt)
		}
	})
}

func TestSeedSkills_OverwriteExistingUpdatesStarterRubrics(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, name string, skillsSvc *skills.Service, settingsSvc *settings.Service) {

		existing := mustCreateSkill(t, skillsSvc, "review-impl")
		result, err := seeding.SeedSkillsWithOptions(context.Background(), skillsSvc, settingsSvc, discardLogger(), seeding.SkillSeedOptions{
			OverwriteExisting: true,
		})
		if err != nil {
			t.Fatalf("[%s] SeedSkillsWithOptions: %v", name, err)
		}

		if got := len(result.SkillsUpdated); got != 1 {
			t.Errorf("[%s] SkillsUpdated=%d, want 1", name, got)
		}
		if got := len(result.SkillsCreated); got != wantSkillCount-1 {
			t.Errorf("[%s] SkillsCreated=%d, want %d", name, got, wantSkillCount-1)
		}
		if got := len(result.SkillsExisting); got != 0 {
			t.Errorf("[%s] SkillsExisting=%d, want 0", name, got)
		}

		updated, err := skillsSvc.Get(context.Background(), existing.ID)
		if err != nil {
			t.Fatalf("[%s] Get updated skill: %v", name, err)
		}
		if updated.Description == "test description" || updated.Prompt == "test prompt text" {
			t.Errorf("[%s] existing skill was not overwritten: description=%q prompt=%q", name, updated.Description, updated.Prompt)
		}
		if updated.Name != "review-impl" {
			t.Errorf("[%s] updated skill name=%q, want review-impl", name, updated.Name)
		}
	})
}

func TestSeedSkills_StarterRubricsStayGateBounded(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, name string, skillsSvc *skills.Service, settingsSvc *settings.Service) {
		if _, err := seeding.SeedSkills(context.Background(), skillsSvc, settingsSvc, discardLogger()); err != nil {
			t.Fatalf("[%s] SeedSkills: %v", name, err)
		}
		all, err := skillsSvc.List(context.Background())
		if err != nil {
			t.Fatalf("[%s] List skills: %v", name, err)
		}

		byName := make(map[string]skills.Skill, len(all))
		for _, skill := range all {
			byName[skill.Name] = skill
			if strings.Contains(skill.Prompt, "release-rollout") || strings.Contains(skill.Prompt, "risks-analysis") {
				t.Errorf("[%s] %s prompt references non-seeded companion skills", name, skill.Name)
			}
			if strings.Contains(skill.Prompt, "Use `prd-writing`") || strings.Contains(skill.Prompt, "Use `spec-writing`") {
				t.Errorf("[%s] %s prompt routes to generic writing skills instead of gate evidence", name, skill.Name)
			}
		}

		for _, required := range []string{"rollout-risk", "acceptance-criteria", "task-breakdown", "spec-review", "prd-review", "review-impl"} {
			if _, ok := byName[required]; !ok {
				t.Fatalf("[%s] missing seeded skill %q", name, required)
			}
		}

		if !strings.Contains(byName["review-impl"].Prompt, "delivery evidence") {
			t.Errorf("[%s] review-impl prompt should judge delivery evidence, got: %s", name, byName["review-impl"].Prompt)
		}
		if strings.Contains(byName["task-breakdown"].Prompt, "Produce an **execution plan**") {
			t.Errorf("[%s] task-breakdown should judge implementation plan traceability, not produce a plan", name)
		}
	})
}

func findSkillByName(t *testing.T, svc *skills.Service, name string) (*skills.Skill, error) {
	t.Helper()
	all, err := svc.List(context.Background())
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i], nil
		}
	}
	t.Fatalf("skill %q not found", name)
	return nil, nil
}
