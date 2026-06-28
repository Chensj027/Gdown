package main

import (
	"fmt"
	"os"

	"gdown/internal/downloader"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("用法: gdown <URL> <输出文件路径>")
		fmt.Println("示例: gdown https://example.com/file.zip ./file.zip")
		os.Exit(1)
	}

	url := os.Args[1]
	dest := os.Args[2]
	task := downloader.NewTask(url, dest)

	fmt.Printf("开始下载: %s -> %s\n", url, dest)

	if err := task.Download(); err != nil {
		fmt.Fprintf(os.Stderr, "下载失败: %v\n", err)
		os.Exit(1)
	}
}
