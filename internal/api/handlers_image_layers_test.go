package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/storage"
	_ "modernc.org/sqlite"
)

func newTestAPIServerWithDB(t *testing.T) *APIServer {
	t.Helper()
	t.Setenv(constants.EnvVarDataDir, t.TempDir())

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

	s := newTestAPIServerForImages()
	s.db = db
	s.layerUploadDiskSpaceCheck = func(context.Context, int64) error { return nil }
	return s
}

func newLayerUploadRequest(digest, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/images/layers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")
	if digest != "" {
		req.Header.Set("X-Layer-Digest", digest)
	}
	return req
}

func TestHandleLayerUpload_MissingDigestReturns400(t *testing.T) {
	s := newTestAPIServerForImages()

	rr := httptest.NewRecorder()
	s.handleLayerUpload().ServeHTTP(rr, newLayerUploadRequest("", "data"))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleLayerUpload_InvalidDigestReturns400(t *testing.T) {
	s := newTestAPIServerForImages()

	invalid := []string{
		"sha256:../../etc",
		"sha256:short",
		"md5:" + strings.Repeat("a", 64),
		strings.Repeat("a", 64),
	}
	for _, digest := range invalid {
		rr := httptest.NewRecorder()
		s.handleLayerUpload().ServeHTTP(rr, newLayerUploadRequest(digest, "data"))

		if rr.Code != http.StatusBadRequest {
			t.Errorf("digest %q: status = %d, want %d", digest, rr.Code, http.StatusBadRequest)
		}
	}
}

func TestHandleLayerUpload_DigestMismatchReturns400(t *testing.T) {
	t.Setenv(constants.EnvVarDataDir, t.TempDir())

	s := newTestAPIServerForImages()
	s.layerUploadDiskSpaceCheck = func(context.Context, int64) error { return nil }

	sum := sha256.Sum256([]byte("expected content"))
	digest := "sha256:" + hex.EncodeToString(sum[:])

	rr := httptest.NewRecorder()
	s.handleLayerUpload().ServeHTTP(rr, newLayerUploadRequest(digest, "different content"))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "digest mismatch") {
		t.Fatalf("body = %q, want digest mismatch error", rr.Body.String())
	}
}

func TestHandleLayerUpload_InsufficientDiskSpaceReturns507(t *testing.T) {
	s := newTestAPIServerForImages()
	s.layerUploadDiskSpaceCheck = func(context.Context, int64) error {
		return &insufficientDiskSpaceError{
			Path:           "/var/lib/haloy",
			RequiredBytes:  2048,
			AvailableBytes: 1024,
		}
	}

	sum := sha256.Sum256([]byte("data"))
	digest := "sha256:" + hex.EncodeToString(sum[:])

	rr := httptest.NewRecorder()
	s.handleLayerUpload().ServeHTTP(rr, newLayerUploadRequest(digest, "data"))

	if rr.Code != http.StatusInsufficientStorage {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusInsufficientStorage)
	}
}

func TestHandleLayerUploadAndCheck_RoundTrip(t *testing.T) {
	s := newTestAPIServerWithDB(t)

	content := "layer bytes"
	sum := sha256.Sum256([]byte(content))
	digest := "sha256:" + hex.EncodeToString(sum[:])

	rr := httptest.NewRecorder()
	s.handleLayerUpload().ServeHTTP(rr, newLayerUploadRequest(digest, content))
	if rr.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}

	var uploadResp apitypes.LayerUploadResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if uploadResp.Digest != digest || uploadResp.Size != int64(len(content)) {
		t.Errorf("upload response = %+v, want digest %s size %d", uploadResp, digest, len(content))
	}

	// Backdate last_used_at to confirm the check refreshes it.
	staleTime := time.Now().Add(-2 * time.Hour)
	if _, err := s.db.Exec(`UPDATE layers SET last_used_at = ? WHERE digest = ?`, staleTime, digest); err != nil {
		t.Fatalf("backdate layer: %v", err)
	}

	missingDigest := "sha256:" + strings.Repeat("0", 64)
	checkBody := `{"digests":["` + digest + `","` + missingDigest + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/images/layers/check", strings.NewReader(checkBody))
	rr = httptest.NewRecorder()
	s.handleLayerCheck().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("check status = %d, want %d: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var checkResp apitypes.LayerCheckResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &checkResp); err != nil {
		t.Fatalf("decode check response: %v", err)
	}
	if len(checkResp.Exists) != 1 || checkResp.Exists[0] != digest {
		t.Errorf("check exists = %v, want [%s]", checkResp.Exists, digest)
	}
	if len(checkResp.Missing) != 1 || checkResp.Missing[0] != missingDigest {
		t.Errorf("check missing = %v, want [%s]", checkResp.Missing, missingDigest)
	}

	var lastUsed time.Time
	if err := s.db.QueryRow(`SELECT last_used_at FROM layers WHERE digest = ?`, digest).Scan(&lastUsed); err != nil {
		t.Fatalf("read last_used_at: %v", err)
	}
	if !lastUsed.After(staleTime.Add(time.Hour)) {
		t.Errorf("last_used_at = %v, want refreshed past %v", lastUsed, staleTime)
	}
}

func TestHandleLayerCheck_InvalidDigestReturns400(t *testing.T) {
	s := newTestAPIServerForImages()

	body := `{"digests":["sha256:../../etc"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/images/layers/check", strings.NewReader(body))
	rr := httptest.NewRecorder()

	s.handleLayerCheck().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}
