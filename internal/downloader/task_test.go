package downloader

import (
	"context"
	"errors"
	"fmt"
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

	// 下载到项目根目录，方便手动查看结果
	dest := filepath.Join("..", "..", "test_download_success.txt")
	// 测试结束后清理文件
	t.Cleanup(func() { os.Remove(dest) })

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

	task := NewTask(server.URL, filepath.Join("..", "..", "test_download_error.txt"))
	t.Cleanup(func() { os.Remove(filepath.Join("..", "..", "test_download_error.txt")) })

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

	dest := filepath.Join("..", "..", "test_download_canceled.txt")
	t.Cleanup(func() { os.Remove(dest) })
	task := NewTask("https://example.invalid/file.txt", dest)

	err := task.DownloadWithContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DownloadWithContext error = %v, want context.Canceled", err)
	}
	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("downloaded file exists or stat failed unexpectedly: %v", statErr)
	}
}

func TestDownloadWithResume(t *testing.T) {
	// 场景1：文件已存在，服务器支持断点续传，返回 206 Partial Content
	t.Run("resume with 206", func(t *testing.T) {
		const body = "hello from gdown"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Range") != "" {
				w.Header().Set("Content-Length", "16")
				w.WriteHeader((http.StatusPartialContent))
			} else {
				w.WriteHeader(http.StatusOK)
			}
			_, _ = w.Write([]byte(body))
		}))
		defer server.Close()

		dest := filepath.Join("..", "..", "test_download_resume.txt")
		t.Cleanup(func() { os.Remove(dest) })

		// 先写入8个字节的内容，模拟断点续传
		oldData := "old_data"
		if err := os.WriteFile(dest, []byte(oldData), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		task := NewTask(server.URL+"/file.txt", dest).WithResume()
		if err := task.DownloadWithContext(context.Background()); err != nil {
			t.Fatalf("DownloadWithContext returned error: %v", err)
		}

		// 断言1： 下载完成后，文件内容应该是oldData + new_data
		wantContent := oldData + body
		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("read downloaded file: %v", err)
		}
		if string(got) != wantContent {
			t.Fatalf("downloaded body = %q, want %q", got, wantContent)
		}

		// 断言2：Downloaded应该是旧内容 + 新内容的总和
		wantDownloaded := int64(len(oldData) + len(body))
		if task.Downloaded != wantDownloaded {
			t.Fatalf("task.Downloaded = %d, want %d", task.Downloaded, wantDownloaded)
		}

		// 断言3：Task 标记为完成
		if !task.Done {
			t.Fatal("task.Done = false, want true")
		}
	})

	// 场景2：文件已存在，服务器不支持断点续传，返回 200 OK
	t.Run("exist but server returns 200", func(t *testing.T) {
		const body = "hello from gdown"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "16")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
		}))
		defer server.Close()

		dest := filepath.Join("..", "..", "test_download_resume_200.txt")
		t.Cleanup(func() { os.Remove(dest) })

		// 先写入8个字节的内容，模拟断点续传
		oldData := "old_data"
		if err := os.WriteFile(dest, []byte(oldData), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		task := NewTask(server.URL+"/file.txt", dest).WithResume()
		if err := task.DownloadWithContext(context.Background()); err != nil {
			t.Fatalf("DownloadWithContext returned error: %v", err)
		}

		// 断言1： 服务器不支持断点续传，文件被覆盖，重新下载，只有新内容
		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("read downloaded file: %v", err)
		}
		if string(got) != body {
			t.Fatalf("downloaded body = %q, want %q", got, body)
		}

		// 断言2：Downloaded应该是新内容的长度
		wantDownloaded := int64(len(body))
		if task.Downloaded != wantDownloaded {
			t.Fatalf("task.Downloaded = %d, want %d", task.Downloaded, wantDownloaded)
		}
	})
}

func TestDownloadConcurrent(t *testing.T) {
	t.Run("multi chunk download", func(t *testing.T) {
		// 用 20 字节的内容，4 个并发 → 每块 5 字节
		const body = "AAAAABBBBBCCCCCDDDDD" // 20 字节
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 解析 Range 头，返回对应范围的字节
			rangeHeader := r.Header.Get("Range")
			if rangeHeader == "" {
				// 初始请求：返回 Content-Length，让 gdown 知道文件大小
				w.Header().Set("Content-Length", "20")
				w.WriteHeader(http.StatusOK)
				return
			}
			// Range 请求：解析并返回对应部分
			var start, end int
			fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)
			w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte(body[start : end+1]))
		}))
		defer server.Close()

		dest := filepath.Join("..", "..", "test_concurrent.txt")
		t.Cleanup(func() { os.Remove(dest) })

		task := NewTask(server.URL+"/file.bin", dest).WithConcurrent(4)
		if err := task.DownloadWithContext(context.Background()); err != nil {
			t.Fatalf("DownloadWithContext returned error: %v", err)
		}

		// 断言：文件内容完整
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
	})
}
