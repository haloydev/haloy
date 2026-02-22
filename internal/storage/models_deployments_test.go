package storage

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/config"
)

func newInMemoryDB(t *testing.T) *DB {
	t.Helper()

	rawDB, err := sql.Open(driverName, ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = rawDB.Close()
	})

	db := &DB{rawDB}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	return db
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return b
}

func TestDeployment_SaveGetAndImageRef(t *testing.T) {
	db := newInMemoryDB(t)

	img := config.Image{Repository: "nginx", Tag: "1.25"}
	deployment := Deployment{
		ID:              "20260222010101",
		AppName:         "app",
		RawDeployConfig: mustJSON(t, config.DeployConfig{TargetConfig: config.TargetConfig{Name: "app"}}),
		DeployedImage:   mustJSON(t, img),
	}

	if err := db.SaveDeployment(deployment); err != nil {
		t.Fatalf("SaveDeployment() error = %v", err)
	}

	got, err := db.GetDeployment(deployment.ID)
	if err != nil {
		t.Fatalf("GetDeployment() error = %v", err)
	}
	if got.ID != deployment.ID {
		t.Fatalf("GetDeployment().ID = %q, want %q", got.ID, deployment.ID)
	}
	if got.AppName != deployment.AppName {
		t.Fatalf("GetDeployment().AppName = %q, want %q", got.AppName, deployment.AppName)
	}

	imageRef, err := got.GetImageRef()
	if err != nil {
		t.Fatalf("GetImageRef() error = %v", err)
	}
	if imageRef != "nginx:1.25" {
		t.Fatalf("GetImageRef() = %q, want %q", imageRef, "nginx:1.25")
	}
}

func TestDeployment_GetDeployment_NotFound(t *testing.T) {
	db := newInMemoryDB(t)

	_, err := db.GetDeployment("missing")
	if err == nil {
		t.Fatal("GetDeployment() expected error for missing deployment")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("GetDeployment() error = %v, expected not found", err)
	}
}

func TestDeployment_PruneOldDeployments(t *testing.T) {
	db := newInMemoryDB(t)

	ids := []string{"20260222010101", "20260222010102", "20260222010103", "20260222010104"}
	for _, id := range ids {
		if err := db.SaveDeployment(Deployment{
			ID:              id,
			AppName:         "app",
			RawDeployConfig: mustJSON(t, config.DeployConfig{TargetConfig: config.TargetConfig{Name: "app"}}),
			DeployedImage:   mustJSON(t, config.Image{Repository: "nginx", Tag: id}),
		}); err != nil {
			t.Fatalf("SaveDeployment(%s) error = %v", id, err)
		}
	}

	if err := db.SaveDeployment(Deployment{
		ID:              "20260222010199",
		AppName:         "other-app",
		RawDeployConfig: mustJSON(t, config.DeployConfig{TargetConfig: config.TargetConfig{Name: "other-app"}}),
		DeployedImage:   mustJSON(t, config.Image{Repository: "redis", Tag: "7"}),
	}); err != nil {
		t.Fatalf("SaveDeployment(other-app) error = %v", err)
	}

	if err := db.PruneOldDeployments("app", 2); err != nil {
		t.Fatalf("PruneOldDeployments() error = %v", err)
	}

	history, err := db.GetDeploymentHistory("app", 10)
	if err != nil {
		t.Fatalf("GetDeploymentHistory() error = %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("GetDeploymentHistory() length = %d, want 2", len(history))
	}
	if history[0].ID != "20260222010104" || history[1].ID != "20260222010103" {
		t.Fatalf("remaining IDs = [%s, %s], want [20260222010104, 20260222010103]", history[0].ID, history[1].ID)
	}

	otherHistory, err := db.GetDeploymentHistory("other-app", 10)
	if err != nil {
		t.Fatalf("GetDeploymentHistory(other-app) error = %v", err)
	}
	if len(otherHistory) != 1 {
		t.Fatalf("GetDeploymentHistory(other-app) length = %d, want 1", len(otherHistory))
	}
}

func TestDeployment_GetImageRef_InvalidJSON(t *testing.T) {
	d := &Deployment{DeployedImage: []byte("not-json")}

	_, err := d.GetImageRef()
	if err == nil {
		t.Fatal("GetImageRef() expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse deployed image") {
		t.Fatalf("GetImageRef() error = %v, expected parse error", err)
	}
}
