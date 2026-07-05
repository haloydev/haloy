package apiclient

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPostFileStreamsMultipartUpload(t *testing.T) {
	content := bytes.Repeat([]byte("haloy layer data "), 4096)
	filePath := filepath.Join(t.TempDir(), "image.tar")
	if err := os.WriteFile(filePath, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var received []byte
	var filename string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != "/v1/images/upload" {
			http.NotFound(w, r)
			return
		}
		if r.ContentLength > 0 {
			t.Errorf("expected streamed (chunked) upload, got Content-Length %d", r.ContentLength)
		}
		file, header, err := r.FormFile("image")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()
		filename = header.Filename
		received, err = io.ReadAll(file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client, err := New(srv.URL, "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := client.PostFile(context.Background(), "images/upload", "image", filePath); err != nil {
		t.Fatalf("PostFile() error = %v", err)
	}
	if filename != "image.tar" {
		t.Errorf("uploaded filename = %q, want %q", filename, "image.tar")
	}
	if !bytes.Equal(received, content) {
		t.Errorf("uploaded content length = %d, want %d", len(received), len(content))
	}
}

func TestPostFileReturnsServerError(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "image.tar")
	if err := os.WriteFile(filePath, []byte("tar"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "disk full", http.StatusInsufficientStorage)
	}))
	defer srv.Close()

	client, err := New(srv.URL, "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = client.PostFile(context.Background(), "images/upload", "image", filePath)
	if err == nil {
		t.Fatal("PostFile() error = nil, want error")
	}
	want := "file upload failed with status 507: disk full"
	if err.Error() != want {
		t.Fatalf("PostFile() error = %q, want %q", err.Error(), want)
	}
}

func TestStreamReturnsResponseBodyForNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/logs/postgres" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "no running containers found for the specified app", http.StatusNotFound)
	}))
	defer srv.Close()

	client, err := New(srv.URL, "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = client.Stream(context.Background(), "logs/postgres", func(data string) bool {
		t.Fatalf("handler called with %q", data)
		return false
	})
	if err == nil {
		t.Fatal("Stream() error = nil, want error")
	}

	want := "stream returned status 404: no running containers found for the specified app"
	if err.Error() != want {
		t.Fatalf("Stream() error = %q, want %q", err.Error(), want)
	}
}

func TestStreamReturnsStatusForNonOKStatusWithoutBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	client, err := New(srv.URL, "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = client.Stream(context.Background(), "logs/postgres", func(data string) bool {
		t.Fatalf("handler called with %q", data)
		return false
	})
	if err == nil {
		t.Fatal("Stream() error = nil, want error")
	}

	want := "stream returned status 418"
	if err.Error() != want {
		t.Fatalf("Stream() error = %q, want %q", err.Error(), want)
	}
}
