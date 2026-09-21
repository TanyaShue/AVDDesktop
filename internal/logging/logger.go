// Package logging 提供结构化、按天滚动的应用日志。
//
// 设计（见 ARCHITECTURE.md §12）：文件保留 KeepLogDays 天，内存保留最近 N 条供 UI 实时查看。
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Level 日志级别。
type Level int

// 级别定义。
const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// ParseLevel 解析级别字符串。
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelWarn:
		return "WARN "
	case LevelError:
		return "ERROR"
	default:
		return "INFO "
	}
}

const memBuffer = 500

// Logger 是线程安全的文件日志器。
type Logger struct {
	mu       sync.Mutex
	dir      string
	level    Level
	keepDays int

	file *os.File
	day  string

	ring []string
	head int
	size int

	disabled bool
}

// New 创建日志器并在目录下按天写文件。
func New(dir string, level string, keepDays int) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	l := &Logger{
		dir:      dir,
		level:    ParseLevel(level),
		keepDays: keepDays,
		ring:     make([]string, memBuffer),
	}
	if err := l.rotateLocked(time.Now()); err != nil {
		return nil, err
	}
	l.cleanup()
	return l, nil
}

// Discard 返回一个不写任何地方的日志器（初始化失败时兜底）。
func Discard() *Logger {
	return &Logger{disabled: true, ring: make([]string, memBuffer)}
}

// SetLevel 动态调整级别。
func (l *Logger) SetLevel(level string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.level = ParseLevel(level)
	l.mu.Unlock()
}

// Debug 输出调试日志。
func (l *Logger) Debug(module, format string, args ...any) {
	l.log(LevelDebug, module, format, args...)
}

// Info 输出信息日志。
func (l *Logger) Info(module, format string, args ...any) {
	l.log(LevelInfo, module, format, args...)
}

// Warn 输出警告日志。
func (l *Logger) Warn(module, format string, args ...any) {
	l.log(LevelWarn, module, format, args...)
}

// Error 输出错误日志。
func (l *Logger) Error(module, format string, args ...any) {
	l.log(LevelError, module, format, args...)
}

// Tail 返回内存中最近 n 条日志（供诊断面板实时查看）。
func (l *Logger) Tail(n int) []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > l.size {
		n = l.size
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		idx := (l.head - n + i + memBuffer*2) % memBuffer
		out = append(out, l.ring[idx])
	}
	return out
}

// Close 关闭文件句柄。
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

func (l *Logger) log(level Level, module, format string, args ...any) {
	if l == nil || l.disabled || level < l.level {
		return
	}
	message := format
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	}
	line := fmt.Sprintf("%s [%s] %-12s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), level, module, message)

	l.mu.Lock()
	defer l.mu.Unlock()

	l.ring[l.head] = strings.TrimRight(line, "\n")
	l.head = (l.head + 1) % memBuffer
	if l.size < memBuffer {
		l.size++
	}

	if l.file == nil {
		return
	}
	if day := time.Now().Format("20060102"); day != l.day {
		_ = l.file.Close()
		l.file = nil
		if err := l.rotateLocked(time.Now()); err != nil {
			return
		}
	}
	_, _ = io.WriteString(l.file, line)
}

func (l *Logger) rotateLocked(now time.Time) error {
	l.day = now.Format("20060102")
	path := filepath.Join(l.dir, "app-"+l.day+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	l.file = f
	return nil
}

// cleanup 删除超过保留天数的日志文件。
func (l *Logger) cleanup() {
	if l.keepDays <= 0 {
		return
	}
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return
	}
	type entry struct {
		path string
		mod  time.Time
	}
	var files []entry
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "app-") || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, entry{path: filepath.Join(l.dir, e.Name()), mod: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	cutoff := time.Now().AddDate(0, 0, -l.keepDays)
	for _, f := range files {
		if f.mod.Before(cutoff) {
			_ = os.Remove(f.path)
		}
	}
}
