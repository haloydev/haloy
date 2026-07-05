package layerstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/storage"
	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) (*LayerStore, string) {
	t.Helper()

	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	t.Setenv(constants.EnvVarDataDir, dataDir)

	rawDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = rawDB.Close()
	})

	db := &storage.DB{DB: rawDB}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	store, err := New(db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return store, dataDir
}

func digestFor(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestValidateDigest(t *testing.T) {
	valid := digestFor([]byte("content"))
	if err := ValidateDigest(valid); err != nil {
		t.Errorf("ValidateDigest(%q) = %v, want nil", valid, err)
	}

	invalid := []string{
		"",
		"sha256:",
		"abc123",
		"sha256:../../etc",
		"sha256:..%2f..%2fetc",
		"sha512:" + strings.Repeat("a", 64),
		"sha256:" + strings.Repeat("a", 63),
		"sha256:" + strings.Repeat("a", 65),
		"sha256:" + strings.Repeat("A", 64),
		"sha256:" + strings.Repeat("a", 63) + "/",
	}
	for _, digest := range invalid {
		if err := ValidateDigest(digest); err == nil {
			t.Errorf("ValidateDigest(%q) = nil, want error", digest)
		}
	}
}

func TestStoreLayerRoundTrip(t *testing.T) {
	store, _ := newTestStore(t)

	content := []byte("layer content")
	digest := digestFor(content)

	size, err := store.StoreLayer(digest, strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("StoreLayer() error = %v", err)
	}
	if size != int64(len(content)) {
		t.Errorf("StoreLayer() size = %d, want %d", size, len(content))
	}

	path, err := store.GetLayerPath(digest)
	if err != nil {
		t.Fatalf("GetLayerPath() error = %v", err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(stored) != string(content) {
		t.Errorf("stored layer content = %q, want %q", stored, content)
	}

	absent := digestFor([]byte("not stored"))
	missing, exists, err := store.HasLayers([]string{digest, absent})
	if err != nil {
		t.Fatalf("HasLayers() error = %v", err)
	}
	if len(exists) != 1 || exists[0] != digest {
		t.Errorf("HasLayers() exists = %v, want [%s]", exists, digest)
	}
	if len(missing) != 1 || missing[0] != absent {
		t.Errorf("HasLayers() missing = %v, want [%s]", missing, absent)
	}
}

func TestDiffIDsByDigest(t *testing.T) {
	blobDigest := digestFor([]byte("compressed blob"))
	diffID := digestFor([]byte("uncompressed layer"))
	layerPaths := []string{"blobs/sha256/" + strings.TrimPrefix(blobDigest, "sha256:")}

	config := []byte(`{"rootfs":{"type":"layers","diff_ids":["` + diffID + `"]}}`)
	diffIDs := diffIDsByDigest(config, layerPaths)
	if got := diffIDs[blobDigest]; got != diffID {
		t.Errorf("diffIDsByDigest()[%s] = %q, want %q", blobDigest, got, diffID)
	}

	// Count mismatch between diff_ids and layers must be ignored.
	if diffIDs := diffIDsByDigest(config, append(layerPaths, "blobs/sha256/other")); diffIDs != nil {
		t.Errorf("diffIDsByDigest() with count mismatch = %v, want nil", diffIDs)
	}

	// Unparseable config must be ignored.
	if diffIDs := diffIDsByDigest([]byte("not json"), layerPaths); diffIDs != nil {
		t.Errorf("diffIDsByDigest() with bad config = %v, want nil", diffIDs)
	}
}

func TestStoreLayerRejectsDigestMismatch(t *testing.T) {
	store, _ := newTestStore(t)

	digest := digestFor([]byte("expected content"))
	_, err := store.StoreLayer(digest, strings.NewReader("different content"))
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("StoreLayer() error = %v, want ErrDigestMismatch", err)
	}

	if _, err := store.GetLayerPath(digest); err == nil {
		t.Error("GetLayerPath() after failed store = nil, want error")
	}
}

func TestStoreLayerRejectsTraversalDigest(t *testing.T) {
	store, dataDir := newTestStore(t)

	escaped := filepath.Join(filepath.Dir(dataDir), "evil")
	_, err := store.StoreLayer("sha256:../../evil", strings.NewReader("payload"))
	if err == nil {
		t.Fatal("StoreLayer() with traversal digest = nil, want error")
	}
	if _, statErr := os.Stat(escaped); !os.IsNotExist(statErr) {
		t.Errorf("traversal digest created path outside the store: %s", escaped)
	}
}

func TestGetLayerPathRejectsTraversalDigest(t *testing.T) {
	store, dataDir := newTestStore(t)

	// Plant a file outside the layer store that a traversal digest would reach.
	outsideDir := filepath.Join(filepath.Dir(dataDir), "secret")
	if err := os.MkdirAll(outsideDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(outsideDir, "layer.tar"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := store.GetLayerPath("sha256:../../secret"); err == nil {
		t.Error("GetLayerPath() with traversal digest = nil, want error")
	}
}

func TestDeleteLayerRejectsTraversalDigest(t *testing.T) {
	store, dataDir := newTestStore(t)

	outsideDir := filepath.Join(filepath.Dir(dataDir), "precious")
	if err := os.MkdirAll(outsideDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	if err := store.DeleteLayer("sha256:../../precious"); err == nil {
		t.Error("DeleteLayer() with traversal digest = nil, want error")
	}
	if _, err := os.Stat(outsideDir); err != nil {
		t.Errorf("traversal digest deleted path outside the store: %v", err)
	}
}

func TestAssembleImageTarRejectsTraversalLayerPath(t *testing.T) {
	store, dataDir := newTestStore(t)

	outsideDir := filepath.Join(filepath.Dir(dataDir), "loot")
	if err := os.MkdirAll(outsideDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(outsideDir, "layer.tar"), []byte("loot"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	req := apitypes.ImageAssembleRequest{
		ImageRef: "app:latest",
		Config:   []byte(`{}`),
		Manifest: apitypes.ImageManifestEntry{
			Config: "config.json",
			Layers: []string{"../../loot/layer.tar"},
		},
	}

	if _, err := store.AssembleImageTar(req); err == nil {
		t.Error("AssembleImageTar() with traversal layer path = nil, want error")
	}
}
