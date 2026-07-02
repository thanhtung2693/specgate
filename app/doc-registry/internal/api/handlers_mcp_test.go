package api

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/settings"
	"github.com/specgate/doc-registry/internal/skills"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
)

func testHandlersSettings(t *testing.T) (*Handlers, func()) {
	t.Helper()
	db := newTestGormDB(t)
	hexKey := hex.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	crypto, err := settings.NewCrypto(hexKey)
	if err != nil {
		t.Fatal(err)
	}
	repo := storagedb.NewSettingsRepository(db)
	svc, err := settings.NewServiceWithTTL(repo, crypto, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Update(map[string]string{
		settings.KeyMCPAddr:    ":8081",
		settings.KeyMCPEnabled: "true",
	}); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{
		Settings:       svc,
		MCPBootEnabled: true,
		Skills:         skills.NewService(storagedb.NewSkillRepository(db)),
	}
	cleanup := func() {
		svc.Stop()
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
	return h, cleanup
}

func TestMcpInfo_ReturnsConfig(t *testing.T) {
	t.Parallel()
	h, cleanup := testHandlersSettings(t)
	defer cleanup()

	out, err := h.McpInfo(context.Background(), &struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.Addr != ":8081" {
		t.Errorf("addr = %q", out.Body.Addr)
	}
	if len(out.Body.Tools) == 0 {
		t.Error("expected tools")
	}
	if out.Body.RestartRequired {
		t.Error("restart_required should be false when boot matches settings")
	}
}
