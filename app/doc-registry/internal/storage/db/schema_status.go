package db

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type SchemaStatus struct {
	Status  string   `json:"status"`
	Message string   `json:"message"`
	Missing []string `json:"missing,omitempty"`
}

type requiredColumn struct {
	table   string
	column  string
	notNull bool
}

// requiredSchemaColumns are the columns that a running Doc Registry needs.
// The development schema is a collapsed fresh-install schema, so checking them
// at boot turns an old persisted database into a clear startup failure instead
// of a later missing-column error on an arbitrary request.
var requiredSchemaColumns = []requiredColumn{
	{"artifacts", "artifact_completeness", false},
	{"artifacts", "artifact_phase", false},
	{"artifacts", "authority", false},
	{"artifacts", "policy_digest", false},
	{"artifacts", "policy_snapshot_json", false},
	{"artifacts", "policy_version", false},
	{"artifacts", "impact_level", false},
	{"artifacts", "lineage_root_id", false},
	{"artifacts", "parent_artifact_id", false},
	{"artifacts", "snapshot_digest", false},
	{"artifacts", "source_id", false},
	{"artifacts", "source_kind", false},
	{"artifacts", "source_revision", false},
	{"artifact_files", "content_sha256", true},
	{"artifacts", "workspace_id", true},
	{"artifact_attachments", "workspace_id", true},
	{"change_requests", "workspace_id", true},
	{"documents", "workspace_id", true},
	{"features", "workspace_id", true},
	{"gate_runs", "completion_feedback_event_id", true},
	{"gate_runs", "workspace_id", true},
	{"gate_tasks", "workspace_id", true},
	{"governance_feedback_events", "workspace_id", true},
	{"governance_files", "workspace_id", true},
	{"governance_threads", "workspace_id", true},
	{"integration_delivery_links", "head_sha", true},
	{"tracker_links", "resource_id", true},
	{"tracker_links", "change_request_id", true},
	{"integration_oauth_states", "workspace_id", true},
	{"integrations", "workspace_id", true},
	{"skills", "workspace_id", true},
	{"workboard_lifecycle_events", "workspace_id", true},
}

var requiredWorkspaceConstraintTables = []string{
	"artifacts", "artifact_attachments", "change_requests", "documents",
	"features", "gate_runs", "gate_tasks", "governance_feedback_events",
	"governance_files", "governance_threads",
	"integration_oauth_states", "integrations", "skills", "workboard_lifecycle_events",
}

func RequiredSchemaStatus(ctx context.Context, gdb *gorm.DB) (SchemaStatus, error) {
	var schema string
	if err := gdb.WithContext(ctx).Raw(`SELECT current_schema()`).Scan(&schema).Error; err != nil {
		return SchemaStatus{}, fmt.Errorf("read current schema: %w", err)
	}
	missing := make([]string, 0)
	for _, required := range requiredSchemaColumns {
		var nullable string
		if err := gdb.WithContext(ctx).Raw(`
				SELECT is_nullable
				FROM information_schema.columns
				WHERE table_schema = current_schema()
				  AND table_name = ?
				  AND column_name = ?
			`, required.table, required.column).Scan(&nullable).Error; err != nil {
			return SchemaStatus{}, fmt.Errorf("check required schema column %s.%s: %w", required.table, required.column, err)
		}
		if nullable == "" || (required.notNull && nullable != "NO") {
			missing = append(missing, schema+"."+required.table+"."+required.column)
		}
	}
	for _, table := range requiredWorkspaceConstraintTables {
		var found bool
		constraint := "ck_" + table + "_workspace_nonblank"
		if err := gdb.WithContext(ctx).Raw(`
				SELECT EXISTS (
					SELECT 1 FROM pg_constraint c
					JOIN pg_class t ON t.oid = c.conrelid
					JOIN pg_namespace n ON n.oid = t.relnamespace
					WHERE n.nspname = current_schema()
					  AND t.relname = ?
					  AND c.conname = ?
					  AND pg_get_constraintdef(c.oid) ILIKE '%btrim(workspace_id)%'
				)`, table, constraint).Scan(&found).Error; err != nil {
			return SchemaStatus{}, fmt.Errorf("check workspace constraint %s: %w", table, err)
		}
		if !found {
			missing = append(missing, schema+"."+table+".workspace_id_nonblank")
		}
	}
	var laneExists bool
	if err := gdb.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'tracker_links' AND column_name = 'lane')`).Scan(&laneExists).Error; err != nil {
		return SchemaStatus{}, fmt.Errorf("check obsolete tracker lane column: %w", err)
	}
	if laneExists {
		missing = append(missing, schema+".tracker_links.no_lane")
	}
	var primaryLinkUnique bool
	if err := gdb.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid JOIN pg_namespace n ON n.oid = t.relnamespace WHERE n.nspname = current_schema() AND t.relname = 'tracker_links' AND c.contype = 'u' AND pg_get_constraintdef(c.oid) = 'UNIQUE (change_request_id)')`).Scan(&primaryLinkUnique).Error; err != nil {
		return SchemaStatus{}, fmt.Errorf("check tracker primary link uniqueness: %w", err)
	}
	if !primaryLinkUnique {
		missing = append(missing, schema+".tracker_links.change_request_id_unique")
	}
	var trackerResourceFK bool
	if err := gdb.WithContext(ctx).Raw(`SELECT EXISTS (
		SELECT 1 FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_class rt ON rt.oid = c.confrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = current_schema() AND t.relname = 'tracker_links'
		  AND c.contype = 'f' AND rt.relname = 'integration_resources'
		  AND pg_get_constraintdef(c.oid) ILIKE '%FOREIGN KEY (resource_id)%ON DELETE CASCADE%'
	)`).Scan(&trackerResourceFK).Error; err != nil {
		return SchemaStatus{}, fmt.Errorf("check tracker resource foreign key: %w", err)
	}
	if !trackerResourceFK {
		missing = append(missing, schema+".tracker_links.resource_id_fk")
	}
	if len(missing) > 0 {
		return SchemaStatus{
			Status:  "incompatible",
			Message: "database schema is missing required columns or ownership constraints: " + strings.Join(missing, ", "),
			Missing: missing,
		}, nil
	}
	return SchemaStatus{Status: "ok", Message: "database schema is compatible"}, nil
}
