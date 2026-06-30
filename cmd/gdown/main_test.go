package main

import (
	"bytes"
	"errors"
	"flag"
	"testing"
	"time"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		want      config
		wantError bool
	}{
		{
			name: "new flag style",
			args: []string{"-o", "file.jpg", "https://example.com/file.jpg"},
			want: config{
				URL:        "https://example.com/file.jpg",
				Dest:       "file.jpg",
				Concurrent: 1,
			},
		},
		{
			name: "new flag style with timeout",
			args: []string{"-o", "file.jpg", "-timeout", "30s", "https://example.com/file.jpg"},
			want: config{
				URL:        "https://example.com/file.jpg",
				Dest:       "file.jpg",
				Timeout:    30 * time.Second,
				Concurrent: 1,
			},
		},
		{
			name: "old positional style",
			args: []string{"https://example.com/file.jpg", "file.jpg"},
			want: config{
				URL:        "https://example.com/file.jpg",
				Dest:       "file.jpg",
				Concurrent: 1,
			},
		},
		{
			name:      "missing output",
			args:      []string{"https://example.com/file.jpg"},
			wantError: true,
		},
		{
			name:      "negative timeout",
			args:      []string{"-o", "file.jpg", "-timeout", "-1s", "https://example.com/file.jpg"},
			wantError: true,
		},
		{
			name:      "too many URLs with output flag",
			args:      []string{"-o", "file.jpg", "https://example.com/a.jpg", "https://example.com/b.jpg"},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// bytes.Buffer 实现了 io.Writer，适合接住 usage 输出，避免测试时刷屏。
			var stderr bytes.Buffer
			got, err := parseArgs(tt.args, &stderr)

			if tt.wantError {
				if err == nil {
					t.Fatal("parseArgs returned nil error, want error")
				}
				return
			}

			if err != nil {
				t.Fatalf("parseArgs returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseArgs = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseArgsHelp(t *testing.T) {
	var stderr bytes.Buffer
	_, err := parseArgs([]string{"-h"}, &stderr)

	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("parseArgs error = %v, want flag.ErrHelp", err)
	}
	if stderr.Len() == 0 {
		t.Fatal("help output is empty")
	}
}
