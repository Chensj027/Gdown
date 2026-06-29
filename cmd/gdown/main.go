package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"gdown/internal/downloader"
)

// config 保存命令行解析后的配置。
// 这样 main 只负责“串流程”，具体参数规则可以单独测试。
type config struct {
	URL     string
	Dest    string
	Timeout time.Duration
}

func main() {
	cfg, err := parseArgs(os.Args[1:], os.Stderr)
	if err != nil {
		// flag 包把 -h / -help 表示成 flag.ErrHelp。
		// 用户主动看帮助不是错误，所以这里用退出码 0。
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "参数错误: %v\n", err)
		os.Exit(1)
	}

	// context 是 Go 里传递取消信号和超时控制的标准方式。
	// 这里 timeout=0 表示不设置超时，适合下载大文件。
	ctx := context.Background()
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	task := downloader.NewTask(cfg.URL, cfg.Dest)

	fmt.Printf("开始下载: %s -> %s\n", cfg.URL, cfg.Dest)
	if cfg.Timeout > 0 {
		fmt.Printf("超时限制: %s\n", cfg.Timeout)
	}

	if err := task.DownloadWithContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "下载失败: %v\n", err)
		os.Exit(1)
	}
}

func parseArgs(args []string, stderr io.Writer) (config, error) {
	// flag.NewFlagSet 比直接使用全局 flag 更适合测试：
	// 每个测试用例都能创建一套独立参数，不会互相污染。
	fs := flag.NewFlagSet("gdown", flag.ContinueOnError)
	fs.SetOutput(stderr)

	output := fs.String("o", "", "输出文件路径")
	timeout := fs.Duration("timeout", 0, "下载超时时间，例如 30s、2m；0 表示不限制")

	fs.Usage = func() {
		fmt.Fprintln(stderr, "用法:")
		fmt.Fprintln(stderr, "  gdown -o <输出文件路径> [-timeout 30s] <URL>")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "兼容旧写法:")
		fmt.Fprintln(stderr, "  gdown <URL> <输出文件路径>")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "参数:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}

	if *timeout < 0 {
		fs.Usage()
		return config{}, fmt.Errorf("-timeout 不能是负数")
	}

	positional := fs.Args()

	// 新写法：gdown -o file.jpg URL
	if *output != "" {
		if len(positional) != 1 {
			fs.Usage()
			return config{}, fmt.Errorf("使用 -o 时需要且只需要一个 URL")
		}
		return config{
			URL:     positional[0],
			Dest:    *output,
			Timeout: *timeout,
		}, nil
	}

	// 旧写法：gdown URL file.jpg
	// 先保留兼容，等你熟悉 flag 后再考虑是否移除。
	if len(positional) == 2 {
		return config{
			URL:     positional[0],
			Dest:    positional[1],
			Timeout: *timeout,
		}, nil
	}

	fs.Usage()
	return config{}, fmt.Errorf("缺少 -o 输出文件路径，或旧写法参数数量不正确")
}
