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
	"sync"
	"sync/atomic"
	"time"
)

// Task 表示一个下载任务。
type Task struct {
	URL      string // 下载地址
	Dest     string // 保存路径
	FileName string // 从 URL 提取的文件名

	Resume     bool // 是否开启断点续传
	Concurrent int  // 并发下载的线程数

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
		URL:        rawURL,
		Dest:       dest,
		FileName:   fileNameFromURL(rawURL),
		Concurrent: 1,
	}
}

func (t *Task) WithResume() *Task {
	t.Resume = true
	return t
}

func (t *Task) WithConcurrent(n int) *Task {
	if n < 1 {
		n = 1
	}
	t.Concurrent = n
	return t
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

	if t.Resume {
		// 如果开启断点续传，先检查本地文件是否存在
		if info, err := os.Stat(t.Dest); err == nil {
			t.Downloaded = info.Size()
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return t.fail(fmt.Errorf("创建请求失败: %w", err))
	}

	if t.Downloaded > 0 {
		str := fmt.Sprintf("bytes=%d-", t.Downloaded)
		req.Header.Set("Range", str)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return t.fail(fmt.Errorf("请求失败: %w", err))
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPartialContent:
		// 服务器支持断点续传，继续下载
		if resp.ContentLength > 0 {
			t.TotalSize = t.Downloaded + resp.ContentLength
		} else {
			t.TotalSize = 0
			fmt.Println("无法获取剩余文件大小，将以流模式续传")
		}
	case http.StatusOK:
		// 服务器不支持断点续传，重新下载
		atomic.StoreInt64(&t.Downloaded, 0)
		if resp.ContentLength > 0 {
			t.TotalSize = resp.ContentLength
		} else {
			fmt.Println("无法获取文件大小，将以流模式下载")
		}
	default:
		return t.fail(fmt.Errorf("服务器返回 %s", resp.Status))
	}

	if err := ensureParentDir(t.Dest); err != nil {
		return t.fail(err)
	}

	// 如果并发数大于1，并且文件大小已知，则使用并发下载
	if t.Concurrent > 1 && t.TotalSize > 0 && t.Downloaded == 0 {
		supportsRange, err := t.supportsRange(ctx)
		if err != nil {
			return t.fail(err)
		}
		if supportsRange {
			// 已经确认可以用 Range 分片下载，初始 GET 的响应体不再需要。
			// 主动关闭可以避免大文件响应体一直占着连接。
			resp.Body.Close()
			if err := t.downloadConcurrent(ctx); err != nil {
				return t.fail(err)
			}
			return nil
		}
		fmt.Println("服务器不支持 Range，并发下载降级为单线程")
	}

	var out *os.File
	if t.Downloaded > 0 {
		fmt.Printf("断点续传: 已下载 %d 字节，继续下载...\n", t.Downloaded)
		out, err = os.OpenFile(t.Dest, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return t.fail(fmt.Errorf("打开文件失败: %w", err))
		}
		defer out.Close()
	} else {
		out, err = os.Create(t.Dest)
		if err != nil {
			return t.fail(fmt.Errorf("创建文件失败: %w", err))
		}
		defer out.Close()
	}

	stopProgress := t.startProgress()
	defer stopProgress()

	_, err = io.Copy(&progressWriter{task: t, writer: out}, resp.Body)
	if err != nil {
		return t.fail(fmt.Errorf("下载中断: %w", err))
	}

	t.FinishTime = time.Now()
	t.Done = true
	t.updateSpeed()

	elapsed := t.FinishTime.Sub(t.StartTime).Seconds()
	downloaded := atomic.LoadInt64(&t.Downloaded)
	fmt.Printf("\n下载完成: %s (%.2f MB, 耗时 %.1fs, 平均速度 %.2f MB/s)\n",
		t.Dest, float64(downloaded)/1024/1024, elapsed, t.Speed/1024/1024)

	return nil
}

func (t *Task) fail(err error) error {
	t.Err = err
	t.FinishTime = time.Now()
	return err
}

func (t *Task) updateSpeed() {
	t.Speed = t.currentSpeed()
}

func (t *Task) currentSpeed() float64 {
	downloaded := atomic.LoadInt64(&t.Downloaded)
	elapsed := time.Since(t.StartTime).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(downloaded) / elapsed
}

type progressWriter struct {
	task   *Task
	writer io.Writer
}

func (w *progressWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if n > 0 {
		atomic.AddInt64(&w.task.Downloaded, int64(n))
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

func (t *Task) supportsRange(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return false, fmt.Errorf("创建 Range 探测请求失败: %w", err)
	}
	req.Header.Set("Range", "bytes=0-0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("Range 探测请求失败: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPartialContent:
		return true, nil
	case http.StatusOK:
		return false, nil
	default:
		return false, fmt.Errorf("Range 探测失败，服务器返回 %s", resp.Status)
	}
}

func (t *Task) printProgressLine() {
	downloaded := atomic.LoadInt64(&t.Downloaded)
	mbDownloaded := float64(downloaded) / 1024 / 1024
	mbSpeed := t.currentSpeed() / 1024 / 1024

	if t.TotalSize > 0 {
		// 知道文件大小，显示百分比
		mbTotal := float64(t.TotalSize) / 1024 / 1024
		pct := float64(downloaded) / float64(t.TotalSize) * 100
		fmt.Printf("\r下载中... %.2f MB / %.2f MB  %.0f%%  %.2f MB/s", mbDownloaded, mbTotal, pct, mbSpeed)
	} else {
		// 流模式：只显示已下载的大小和速度
		fmt.Printf("\r下载中... %.2f MB  %.2f MB/s", mbDownloaded, mbSpeed)
	}
}

func (t *Task) startProgress() func() {
	// 创建一个通道，用来通知 goroutine "下载完了，可以退出了"
	// chan struct{} 是 Go 里最轻量的信号通道：struct{}是空结构体，不占内存
	done := make(chan struct{})
	stopped := make(chan struct{})

	// go 关键字启动一个新的 goroutine 来打印下载进度
	go func() {
		defer close(stopped)

		// 创建一个定时器，每500ms发一次信号
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		// 监听多个通道的标准写法
		for {
			// select同一时间只执行一个case，如果多个case都满足条件，Go会随机选择一个执行
			select {
			case <-done:
				// done通道被关闭，说明下载完成或出错，打印最终进度后退出 goroutine
				t.printProgressLine()
				return

			case <-ticker.C:
				// 每隔 500ms 打印一次进度
				t.printProgressLine()
			}
		}
	}()

	return func() {
		close(done)
		<-stopped
	}
}

func (t *Task) downloadConcurrent(ctx context.Context) error {
	// 文件很小时，并发数可能大于字节数；这里把 worker 数压到合理范围，
	// 避免出现 0 字节分片，比如 3 字节文件却开 8 个 goroutine。
	workers := t.Concurrent
	if int64(workers) > t.TotalSize {
		workers = int(t.TotalSize)
	}
	if workers < 1 {
		workers = 1
	}

	// 计算每个分片的大小。最后一个分片会吃掉余数。
	chunkSize := t.TotalSize / int64(workers)

	// 创建并打开文件
	file, err := os.Create(t.Dest)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer file.Close()

	if err := file.Truncate(t.TotalSize); err != nil {
		return fmt.Errorf("预分配文件大小失败: %w", err)
	}

	// 启动进度显示 goroutine
	stopProgress := t.startProgress()
	defer stopProgress()

	// 启动多个 goroutine 下载分片
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			// 计算当前分片的起始和结束字节
			start := int64(index) * chunkSize
			var end int64
			if index == workers-1 {
				end = t.TotalSize - 1 // 最后一个分片可能比其他分片大，直接取到文件末尾
			} else {
				end = start + chunkSize - 1
			}
			expected := end - start + 1

			// 创建 Range 请求
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
			if err != nil {
				errOnce.Do(func() { firstErr = fmt.Errorf("创建请求失败: %w", err) })
				return
			}
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

			// 发送请求
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errOnce.Do(func() { firstErr = fmt.Errorf("分片 %d 请求失败: %w", index, err) })
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusPartialContent {
				errOnce.Do(func() {
					firstErr = fmt.Errorf("分片 %d 不支持 Range，服务器返回 %s", index, resp.Status)
				})
				return
			}

			if resp.ContentLength >= 0 && resp.ContentLength != expected {
				errOnce.Do(func() {
					firstErr = fmt.Errorf("分片 %d 长度不匹配: got %d, want %d", index, resp.ContentLength, expected)
				})
				return
			}

			// 使用 LimitedReader 限制每个 goroutine 最多写入自己负责的字节范围。
			// 这样即使服务器多返回了数据，也不会越界写坏其他分片。
			limited := &io.LimitedReader{
				R: resp.Body,
				N: expected,
			}
			writer := &writeAtProgressWriter{
				task:   t,
				file:   file,
				offset: start,
			}
			written, err := io.CopyBuffer(writer, limited, make([]byte, 32*1024))
			if err != nil {
				errOnce.Do(func() { firstErr = fmt.Errorf("分片 %d 写入失败: %w", index, err) })
				return
			}
			if written != expected {
				errOnce.Do(func() {
					firstErr = fmt.Errorf("分片 %d 下载不完整: got %d, want %d", index, written, expected)
				})
				return
			}
		}(i)
	}

	// 等待所有分片下载完成
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}

	t.FinishTime = time.Now()
	t.Done = true
	t.updateSpeed()

	elapsed := t.FinishTime.Sub(t.StartTime).Seconds()
	downloaded := atomic.LoadInt64(&t.Downloaded)
	fmt.Printf("\n并发下载完成: %s (%.2f MB, 耗时 %.1fs, 平均速度 %.2f MB/s)\n",
		t.Dest, float64(downloaded)/1024/1024, elapsed, t.Speed/1024/1024)

	return nil
}

type writeAtProgressWriter struct {
	task   *Task
	file   *os.File
	offset int64
}

func (w *writeAtProgressWriter) Write(p []byte) (int, error) {
	n, err := w.file.WriteAt(p, w.offset)
	if n > 0 {
		w.offset += int64(n)
		atomic.AddInt64(&w.task.Downloaded, int64(n))
	}
	return n, err
}
