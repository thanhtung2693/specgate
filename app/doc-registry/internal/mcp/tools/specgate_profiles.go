package tools

import (
	"context"
	"encoding/json"

	"github.com/specgate/doc-registry/internal/governanceprofile"
)

type SpecgateProfileCatalog interface {
	ListProfiles(ctx context.Context) ([]governanceprofile.ResolvedProfile, error)
	ImportProfiles(ctx context.Context, in []governanceprofile.ImportInput) ([]governanceprofile.ResolvedProfile, error)
}

type SpecgateListProfilesResult struct {
	Items []governanceprofile.ResolvedProfile `json:"items"`
}

func NewSpecgateListProfilesHandler(svc SpecgateProfileCatalog) func(context.Context, struct{}) (string, error) {
	return func(ctx context.Context, _ struct{}) (string, error) {
		items, err := svc.ListProfiles(ctx)
		if err != nil {
			return "", err
		}
		out, err := json.Marshal(SpecgateListProfilesResult{Items: items})
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
}

type SpecgateImportProfileDTO struct {
	Namespace   string                       `json:"namespace"`
	Key         string                       `json:"key"`
	Version     string                       `json:"version"`
	DisplayName string                       `json:"display_name,omitempty"`
	ChangeType  string                       `json:"change_type,omitempty"`
	SourceRepo  string                       `json:"source_repo,omitempty"`
	SourcePath  string                       `json:"source_path,omitempty"`
	Definition  governanceprofile.Definition `json:"definition"`
}

type SpecgateImportProfilesInput struct {
	Profiles []SpecgateImportProfileDTO `json:"profiles"`
}

type SpecgateImportProfilesResult struct {
	Items []governanceprofile.ResolvedProfile `json:"items"`
}

func NewSpecgateImportProfilesHandler(svc SpecgateProfileCatalog) func(context.Context, SpecgateImportProfilesInput) (string, error) {
	return func(ctx context.Context, in SpecgateImportProfilesInput) (string, error) {
		imports := make([]governanceprofile.ImportInput, 0, len(in.Profiles))
		for _, p := range in.Profiles {
			imports = append(imports, governanceprofile.ImportInput{
				Namespace:   p.Namespace,
				Key:         p.Key,
				Version:     p.Version,
				Definition:  p.Definition,
				SourceRepo:  p.SourceRepo,
				SourcePath:  p.SourcePath,
				DisplayName: p.DisplayName,
				ChangeType:  p.ChangeType,
			})
		}
		items, err := svc.ImportProfiles(ctx, imports)
		if err != nil {
			return "", err
		}
		out, err := json.Marshal(SpecgateImportProfilesResult{Items: items})
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
}
