package local

import (
	"context"
	"errors"
	"fmt"
)

var ErrStoreIncompatible = errors.New("incompatible Local store or export")

// ValidateHandoffExport refuses evidence the frozen v1 format cannot preserve.
func (s *Store) ValidateHandoffExport(ctx context.Context, workspaceID, ref string) error {
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return err
	}
	var count int
	err = s.db.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM verification_contracts WHERE workspace_id=? AND work_id=?) +
 (SELECT COUNT(*) FROM work_checkpoints WHERE workspace_id=? AND work_id=?) +
 (SELECT COUNT(*) FROM acceptance_bases WHERE workspace_id=? AND work_id=?) +
 (SELECT COUNT(*) FROM artifacts WHERE workspace_id=? AND id=? AND source_lineage_json!='')`,
		workspaceID, work.ID, workspaceID, work.ID, workspaceID, work.ID, workspaceID, work.ArtifactID).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: delivery-handoff/v1 cannot preserve Local verification, checkpoint, lineage or acceptance basis; use a Local database backup", ErrStoreIncompatible)
	}
	return nil
}
