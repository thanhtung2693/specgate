package api

import (
	"context"
	"testing"

	"github.com/specgate/doc-registry/internal/skills"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
)

func testHandlersSkills(t *testing.T) (*Handlers, func()) {
	t.Helper()
	db := newTestGormDB(t)
	svc := skills.NewService(storagedb.NewSkillRepository(db))
	h := &Handlers{Skills: svc}
	cleanup := func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
	return h, cleanup
}

func TestSkills_CRUD(t *testing.T) {
	t.Parallel()
	h, cleanup := testHandlersSkills(t)
	defer cleanup()
	ctx := context.Background()

	var createIn CreateSkillInput
	createIn.Body.Name = "My skill"
	createIn.Body.Description = "desc"
	createIn.Body.Prompt = "You are helpful.\n\n# Title\n\nbody"
	created, err := h.CreateSkill(ctx, &createIn)
	if err != nil {
		t.Fatal(err)
	}
	id := created.Body.ID
	if id == "" {
		t.Fatal("expected id")
	}

	list, err := h.ListSkills(ctx, &struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Body.Items) != 1 {
		t.Fatalf("count = %d", len(list.Body.Items))
	}
	if list.Body.Items[0].Name != "My skill" {
		t.Errorf("name = %q", list.Body.Items[0].Name)
	}

	var upd UpdateSkillInput
	upd.ID = id
	upd.Body.Name = "Renamed"
	upd.Body.Description = "desc"
	upd.Body.Prompt = "x"
	_, err = h.UpdateSkill(ctx, &upd)
	if err != nil {
		t.Fatal(err)
	}

	list2, err := h.ListSkills(ctx, &struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if list2.Body.Items[0].Name != "Renamed" {
		t.Errorf("name = %q", list2.Body.Items[0].Name)
	}

	del, err := h.DeleteSkill(ctx, &DeleteSkillInput{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	if !del.Body.OK {
		t.Error("expected ok")
	}
	list3, err := h.ListSkills(ctx, &struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list3.Body.Items) != 0 {
		t.Fatalf("count after delete = %d", len(list3.Body.Items))
	}
}

func TestCreateSkill_Validation(t *testing.T) {
	t.Parallel()
	h, cleanup := testHandlersSkills(t)
	defer cleanup()

	var createIn CreateSkillInput
	createIn.Body.Name = ""
	createIn.Body.Prompt = "p"
	_, err := h.CreateSkill(context.Background(), &createIn)
	if err == nil {
		t.Fatal("expected error")
	}

	var createIn2 CreateSkillInput
	createIn2.Body.Name = "n"
	createIn2.Body.Prompt = ""
	_, err = h.CreateSkill(context.Background(), &createIn2)
	if err == nil {
		t.Fatal("expected error")
	}
}
