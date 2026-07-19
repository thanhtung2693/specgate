package db

import (
	"strings"
	"testing"

	"gorm.io/gorm"
)

// tableColumns returns a set of column names present in the given table.
func tableColumns(t *testing.T, name string, gdb *gorm.DB, driver string) map[string]bool {
	t.Helper()
	cols := map[string]bool{}
	var rows []struct {
		ColumnName string `gorm:"column:column_name"`
	}
	if err := gdb.Raw(
		"SELECT column_name FROM information_schema.columns WHERE table_name = $1",
		name,
	).Scan(&rows).Error; err != nil {
		t.Fatalf("information_schema.columns(%s): %v", name, err)
	}
	for _, r := range rows {
		cols[strings.ToLower(r.ColumnName)] = true
	}
	return cols
}

// TestGovernanceEnvelopeSchema verifies the open (artifact_id, path)
// artifact_files shape and the governance envelope columns on artifacts.
// Per spec §Migration: artifact_files keyed by (artifact_id, path) with a role
// column; artifacts carries the envelope columns.
func TestGovernanceEnvelopeSchema(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		skillCols := tableColumns(t, "skills", gdb, name)
		if !skillCols["workspace_id"] {
			t.Fatalf("skills missing column workspace_id (have %v)", skillCols)
		}
		integrationCols := tableColumns(t, "integrations", gdb, name)
		if !integrationCols["workspace_id"] {
			t.Fatalf("integrations missing column workspace_id (have %v)", integrationCols)
		}
		oauthCols := tableColumns(t, "integration_oauth_states", gdb, name)
		if !oauthCols["workspace_id"] {
			t.Fatalf("integration_oauth_states missing column workspace_id (have %v)", oauthCols)
		}
		fileCols := tableColumns(t, "governance_files", gdb, name)
		if !fileCols["workspace_id"] {
			t.Fatalf("governance_files missing column workspace_id (have %v)", fileCols)
		}
		// artifact_files columns
		cols := tableColumns(t, "artifact_files", gdb, name)
		for _, want := range []string{"artifact_id", "path", "role", "object_key", "size_bytes", "content_sha256"} {
			if !cols[want] {
				t.Fatalf("artifact_files missing column %q (have %v)", want, cols)
			}
		}
		var nullable string
		if err := gdb.Raw(
			"SELECT is_nullable FROM information_schema.columns WHERE table_name = 'artifact_files' AND column_name = 'content_sha256'",
		).Scan(&nullable).Error; err != nil {
			t.Fatalf("artifact_files.content_sha256 nullability: %v", err)
		}
		if nullable != "NO" {
			t.Errorf("artifact_files.content_sha256 is_nullable=%q, want NO", nullable)
		}
		// artifacts envelope columns
		acols := tableColumns(t, "artifacts", gdb, name)
		for _, want := range []string{"source_kind", "source_id", "source_revision", "snapshot_digest", "authority", "policy_version", "policy_digest", "policy_snapshot_json"} {
			if !acols[want] {
				t.Fatalf("artifacts missing envelope column %q (have %v)", want, acols)
			}
		}
	})
}

func TestWorkspaceOwnershipColumnsAreRequired(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		for _, table := range []string{
			"artifacts", "artifact_attachments", "change_requests", "documents",
			"features", "gate_runs", "gate_tasks", "governance_files",
			"governance_feedback_events",
			"governance_threads", "integration_oauth_states",
			"integrations", "skills", "workboard_lifecycle_events",
		} {
			var nullable string
			if err := gdb.Raw(
				"SELECT is_nullable FROM information_schema.columns WHERE table_name = $1 AND column_name = 'workspace_id'",
				table,
			).Scan(&nullable).Error; err != nil {
				t.Fatalf("workspace_id nullability for %s: %v", table, err)
			}
			if nullable != "NO" {
				t.Errorf("%s.workspace_id is_nullable=%q, want NO", table, nullable)
			}
			var definition string
			if err := gdb.Raw(
				`SELECT pg_get_constraintdef(c.oid)
				 FROM pg_constraint c
				 JOIN pg_class t ON t.oid = c.conrelid
				 WHERE t.relname = ? AND c.conname = ?`,
				table, "ck_"+table+"_workspace_nonblank",
			).Scan(&definition).Error; err != nil {
				t.Fatalf("workspace_id constraint for %s: %v", table, err)
			}
			if !strings.Contains(strings.ToLower(definition), "btrim(workspace_id)") {
				t.Errorf("%s workspace constraint = %q, want btrim(workspace_id) check", table, definition)
			}
		}
	})
}

func TestRequiredSchemaStatusDetectsMissingWorkspaceOwnership(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		if err := gdb.Exec("ALTER TABLE artifacts ALTER COLUMN workspace_id DROP NOT NULL").Error; err != nil {
			t.Fatal(err)
		}
		status, err := RequiredSchemaStatus(t.Context(), gdb)
		if err != nil {
			t.Fatal(err)
		}
		if status.Status != "incompatible" {
			t.Fatalf("status = %q, want incompatible", status.Status)
		}
		if !containsString(status.Missing, "public.artifacts.workspace_id") {
			t.Fatalf("missing = %v, want artifacts.workspace_id", status.Missing)
		}
	})
}

func TestRequiredSchemaStatusDetectsMissingArtifactFileHash(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		if err := gdb.Exec("ALTER TABLE artifact_files ALTER COLUMN content_sha256 DROP NOT NULL").Error; err != nil {
			t.Fatal(err)
		}
		status, err := RequiredSchemaStatus(t.Context(), gdb)
		if err != nil {
			t.Fatal(err)
		}
		if status.Status != "incompatible" {
			t.Fatalf("status = %q, want incompatible", status.Status)
		}
		if !containsString(status.Missing, "public.artifact_files.content_sha256") {
			t.Fatalf("missing = %v, want artifact_files.content_sha256", status.Missing)
		}
	})
}

func TestRequiredSchemaStatusDetectsMissingDeliveryHead(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		if err := gdb.Exec("ALTER TABLE integration_delivery_links DROP COLUMN head_sha").Error; err != nil {
			t.Fatal(err)
		}
		status, err := RequiredSchemaStatus(t.Context(), gdb)
		if err != nil {
			t.Fatal(err)
		}
		if status.Status != "incompatible" {
			t.Fatalf("status = %q, want incompatible", status.Status)
		}
		if !containsString(status.Missing, "public.integration_delivery_links.head_sha") {
			t.Fatalf("missing = %v, want integration_delivery_links.head_sha", status.Missing)
		}
	})
}

func TestRequiredSchemaStatusDetectsCollapsedTrackerSchema(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		if err := gdb.Exec("ALTER TABLE tracker_links ALTER COLUMN resource_id DROP NOT NULL").Error; err != nil {
			t.Fatal(err)
		}
		if err := gdb.Exec("ALTER TABLE tracker_links ADD COLUMN lane TEXT NOT NULL DEFAULT ''").Error; err != nil {
			t.Fatal(err)
		}
		if err := gdb.Exec("ALTER TABLE tracker_links DROP CONSTRAINT tracker_links_change_request_id_key").Error; err != nil {
			t.Fatal(err)
		}
		status, err := RequiredSchemaStatus(t.Context(), gdb)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"public.tracker_links.resource_id", "public.tracker_links.no_lane", "public.tracker_links.change_request_id_unique"} {
			if !containsString(status.Missing, want) {
				t.Fatalf("missing = %v, want %s", status.Missing, want)
			}
		}
	})
}

func TestRequiredSchemaStatusDetectsTrackerResourceForeignKeyRemoval(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		if err := gdb.Exec("ALTER TABLE tracker_links DROP CONSTRAINT tracker_links_resource_id_fkey").Error; err != nil {
			t.Fatal(err)
		}
		status, err := RequiredSchemaStatus(t.Context(), gdb)
		if err != nil {
			t.Fatal(err)
		}
		if !containsString(status.Missing, "public.tracker_links.resource_id_fk") {
			t.Fatalf("missing = %v", status.Missing)
		}
	})
}

func TestRequiredSchemaStatusDetectsBlankWorkspaceConstraintRemoval(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		if err := gdb.Exec("ALTER TABLE artifacts DROP CONSTRAINT ck_artifacts_workspace_nonblank").Error; err != nil {
			t.Fatal(err)
		}
		status, err := RequiredSchemaStatus(t.Context(), gdb)
		if err != nil {
			t.Fatal(err)
		}
		if status.Status != "incompatible" {
			t.Fatalf("status = %q, want incompatible", status.Status)
		}
		if !containsString(status.Missing, "public.artifacts.workspace_id_nonblank") {
			t.Fatalf("missing = %v, want artifacts.workspace_id_nonblank", status.Missing)
		}
	})
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestPostgresFreshSchemaIncludesGovernanceTables(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		for _, table := range []string{
			"gate_tasks",
			"gate_runs",
			"users",
			"workspaces",
			"workspace_members",
		} {
			if cols := tableColumns(t, table, gdb, name); len(cols) == 0 {
				t.Fatalf("%s table missing in fresh schema on %s", table, name)
			}
		}
		if cols := tableColumns(t, "gate_tasks", gdb, name); !cols["workspace_id"] {
			t.Fatalf("gate_tasks missing column workspace_id on %s (have %v)", name, cols)
		}

		cols := tableColumns(t, "change_requests", gdb, name)
		for _, want := range []string{"governance_thread_id", "workspace_id"} {
			if !cols[want] {
				t.Fatalf("change_requests missing column %q in fresh schema on %s", want, name)
			}
		}
		if cols["epic_id"] {
			t.Fatalf("change_requests retains removed epic_id on %s", name)
		}
		if gdb.Migrator().HasIndex("change_requests", "idx_change_requests_epic") {
			t.Fatalf("change_requests retains removed epic index on %s", name)
		}
	})
}
