package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"
)

// Task 表示一个下载任务。
type Task struct {
	URL      string // 下载地址
	Dest     string // 保存路径
	FileName string // 从 URL 提取的文件名

	// 状态字段，后续做进度展示或并发下载时会用到。
	TotalSize  int64
	Downloaded int64
	Speed      float64 // bytes/s
	Done       bool
	Err        error
	StartTime  time.Time
	FinishTime time.Time
}

// NewTask 构造一个下载任务。
func NewTask(rawURL, dest string) *Task {
	return &Task{
		URL:      rawURL,
		Dest:     dest,
		FileName: fileNameFromURL(rawURL),
	}
}

// Download 使用后台 context 执行下载。
func (t *Task) Download() error {
	return t.DownloadWithContext(context.Background())
}

// DownloadWithContext 执行下载，并允许调用方通过 context 取消任务。
func (t *Task) DownloadWithContext(ctx context.Context) error {
	t.StartTime = time.Now()
	t.FinishTime = time.Time{}
	t.TotalSize = 0
	t.Downloaded = 0
	t.Speed = 0
	t.Done = false
	t.Err = nil

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return t.fail(fmt.Errorf("创建请求失败: %w", err))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return t.fail(fmt.Errorf("请求失败: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return t.fail(fmt.Errorf("服务器返回 %s", resp.Status))
	}

	if resp.ContentLength > 0 {
		t.TotalSize = resp.ContentLength
	} else {
		fmt.Println("无法获取文件大小，将以流模式下载")
	}

	if err := ensureParentDir(t.Dest); err != nil {
		return t.fail(err)
	}

	out, err := os.Create(t.Dest)
	if err != nil {
		return t.fail(fmt.Errorf("创建文件失败: %w", err))
	}
	defer out.Close()

	written, err := io.Copy(&progressWriter{task: t, writer: out}, resp.Body)
	if err != nil {
		return t.fail(fmt.Errorf("下载中断: %w", err))
	}

	t.Downloaded = written
	t.FinishTime = time.Now()
	t.Done = true
	t.updateSpeed()

	elapsed := t.FinishTime.Sub(t.StartTime).Seconds()
	fmt.Printf("下载完成: %s (%.2f MB, 耗时 %.1fs, 平均速度 %.2f MB/s)\n",
		t.Dest, float64(written)/1024/1024, elapsed, t.Speed/1024/1024)

	return nil
}

func (t *Task) fail(err error) error {
	t.Err = err
	t.FinishTime = time.Now()
	return err
}

func (t *Task) updateSpeed() {
	elapsed := time.Since(t.StartTime).Seconds()
	if elapsed <= 0 {
		t.Speed = 0
		return
	}
	t.Speed = float64(t.Downloaded) / elapsed
}

type progressWriter struct {
	task   *Task
	writer io.Writer
}

func (w *progressWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if n > 0 {
		w.task.Downloaded += int64(n)
		w.task.updateSpeed()
	}
	return n, err
}

func ensureParentDir(dest string) error {
	dir := filepath.Dir(dest)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	return nil
}

func fileNameFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return path.Base(parsed.Path)
}
