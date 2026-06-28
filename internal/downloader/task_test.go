package downloader

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadWithContextSuccess(t *testing.T) {
	const body = "hello from gdown"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "16")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "nested", "file.txt")
	task := NewTask(server.URL+"/file.txt", dest)

	if err := task.DownloadWithContext(context.Background()); err != nil {
		t.Fatalf("DownloadWithContext returned error: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != body {
		t.Fatalf("downloaded body = %q, want %q", got, body)
	}
	if !task.Done {
		t.Fatal("task.Done = false, want true")
	}
	if task.Downloaded != int64(len(body)) {
		t.Fatalf("task.Downloaded = %d, want %d", task.Downloaded, len(body))
	}
	if task.TotalSize != int64(len(body)) {
		t.Fatalf("task.TotalSize = %d, want %d", task.TotalSize, len(body))
	}
	if task.FileName != "file.txt" {
		t.Fatalf("task.FileName = %q, want file.txt", task.FileName)
	}
	if task.FinishTime.Before(task.StartTime) {
		t.Fatal("FinishTime is before StartTime")
	}
}

func TestDownloadWithContextHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	task := NewTask(server.URL, filepath.Join(t.TempDir(), "file.txt"))

	err := task.DownloadWithContext(context.Background())
	if err == nil {
		t.Fatal("DownloadWithContext returned nil, want error")
	}
	if task.Done {
		t.Fatal("task.Done = true, want false")
	}
	if task.Err == nil {
		t.Fatal("task.Err = nil, want recorded error")
	}
}

func TestDownloadWithContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dest := filepath.Join(t.TempDir(), "file.txt")
	task := NewTask("https://example.invalid/file.txt", dest)

	err := task.DownloadWithContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DownloadWithContext error = %v, want context.Canceled", err)
	}
	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("downloaded file exists or stat failed unexpectedly: %v", statErr)
	}
}
