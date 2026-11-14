package local

import (
	"bytes"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"modernc.org/sqlite"
)

func TestTransferGenerationPreservesStoreHistory(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(ctx, InitInput{WorkspaceName: "Source", DisplayName: "Reviewer", Username: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateQuickWork(ctx, selection.Workspace.ID, QuickWorkInput{Title: "Original work", AcceptanceCriteria: []string{"Keep history"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.CheckoutGeneration(ctx); err != nil || got != "" {
		t.Fatalf("ordinary store generation = %q, %v", got, err)
	}
	checkpoint, err := store.CreateCheckpointWithFiles(ctx, work.WorkspaceID, work.Key, "source", []string{"tracked"}, "Historical note")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.CheckoutGeneration(ctx); err != nil || got != "" {
		t.Fatalf("enhanced v1 generation = %q, %v", got, err)
	}
	first, err := store.InitializeTransferGeneration(ctx)
	decoded, decodeErr := hex.DecodeString(first)
	if err != nil || decodeErr != nil || len(decoded) != 16 || first == selection.StoreID {
		t.Fatalf("first generation = %q, %v", first, err)
	}
	second, err := store.InitializeTransferGeneration(ctx)
	if err != nil || second == "" || second == first {
		t.Fatalf("repeated generation = %q, %v", second, err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, err := reopened.CheckoutGeneration(ctx); err != nil || got != second {
		t.Fatalf("persisted generation = %q, %v", got, err)
	}
	if got, err := reopened.Current(ctx); err != nil || got != selection {
		t.Fatalf("selection changed: %+v, %v", got, err)
	}
	if got, err := reopened.GetCheckpoint(ctx, work.WorkspaceID, work.Key, checkpoint.ID); err != nil || !reflect.DeepEqual(got, checkpoint) {
		t.Fatalf("historical checkpoint changed: %+v, %v", got, err)
	}
	if upgrade, err := reopened.PendingStoreUpgrade(ctx); err != nil || upgrade != nil {
		t.Fatalf("unexpected upgrade after receipt: %+v, %v", upgrade, err)
	}
	if _, err := reopened.CreateCheckpointWithFiles(ctx, work.WorkspaceID, work.Key, "destination", nil, "Destination note"); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationStoreCompatibilityRefusesBeforeMutation(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Initialize(ctx, InitInput{WorkspaceName: "Source", DisplayName: "Reviewer", Username: "reviewer"}); err != nil {
		t.Fatal(err)
	}
	// Keep a connection with the previous writer capabilities open across the
	// version transition. Startup validation alone cannot protect this case.
	previousDriver := &sqlite.Driver{}
	if err := previousDriver.RegisterDeterministicScalarFunction(enhancedWriterFunction, 0,
		func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) { return int64(1), nil }); err != nil {
		t.Fatal(err)
	}
	previous, err := previousDriver.Open(sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer previous.Close()
	oldWriter := previous.(driver.ExecerContext)
	generation, err := store.InitializeTransferGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oldWriter.ExecContext(ctx, `UPDATE workspaces SET name='Old writer must fail'`, nil); err == nil || !strings.Contains(err.Error(), transferWriterFunction) {
		t.Fatalf("open prior writer could mutate workspaces: %v", err)
	}
	if err := previous.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	t.Run("immediately previous enhanced binary", func(t *testing.T) {
		binary := os.Getenv("SPECGATE_PRE_TRANSFER_CLI")
		if binary == "" {
			t.Skip("set SPECGATE_PRE_TRANSFER_CLI to the immediately previous enhanced CLI binary")
		}
		configPath := filepath.Join(dir, "config.json")
		if err := os.WriteFile(configPath, []byte(`{"mode":"local"}`), 0600); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, binary, "--json", "--yes", "workspace", "create", "Old writer must fail")
		cmd.Dir = dir
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "SPECGATE_CONFIG_PATH=" + configPath, "SPECGATE_LOCAL_DIR=" + dir, "SPECGATE_NO_UPDATE_CHECK=1", "CI=1"}
		out, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 6 || !strings.Contains(string(out), "enhanced Local store version") {
			t.Fatalf("prior binary did not refuse a received store: %v %s", err, out)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("prior binary changed received database: %v", err)
		}
	})

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := reopened.CheckoutGeneration(ctx); err != nil || got != generation {
		t.Fatalf("generation changed: %q, %v", got, err)
	}
	if _, err := reopened.db.ExecContext(ctx, `UPDATE metadata SET value='future' WHERE key='enhanced_store_version'`); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.InitializeTransferGeneration(ctx); !errors.Is(err, ErrStoreIncompatible) {
		t.Fatalf("initialization over unknown format: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	infoBefore, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if unexpected, err := Open(path); !errors.Is(err, ErrStoreIncompatible) {
		if unexpected != nil {
			unexpected.Close()
		}
		t.Fatalf("open unknown format: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("opening unknown format changed database: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != infoBefore.Mode().Perm() {
		t.Fatalf("opening unknown format changed permissions: %v, %v", info, err)
	}
}
