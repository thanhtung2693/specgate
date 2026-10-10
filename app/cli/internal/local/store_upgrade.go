package local

import "context"

// StoreUpgrade describes the first enhanced write before it changes compatibility.
type StoreUpgrade struct {
	Scope                 string `json:"scope"`
	BackupPath            string `json:"backup_path"`
	RequiresSupportingCLI bool   `json:"requires_supporting_cli"`
}

func (s *Store) PendingStoreUpgrade(ctx context.Context) (*StoreUpgrade, error) {
	enabled, err := s.enhancedStoreEnabled(ctx)
	if err != nil || enabled {
		return nil, err
	}
	return &StoreUpgrade{Scope: "entire_local_store", BackupPath: s.enhancedStoreBackupPath(), RequiresSupportingCLI: true}, nil
}
