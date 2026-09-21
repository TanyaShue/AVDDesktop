// Package logging 提供结构化、按天+按大小滚动的应用日志。
//
// 设计要点：
//   - Interface 是各领域包依赖的最小接口，避免 internal 包之间互相引用具体类型
//   - 领域包通过构造函数注入日志器，未注入时使用 Nop()，保证可测试性
//   - Sink 把每条日志实时推给 UI（诊断面板），文件仍按天+大小滚动保留
//   - 保留内存环形缓冲，即使未配置 Sink 也能取到最近日志
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

// 级别定义（数值越大越严重）。
const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// String 返回固定宽度的大写级别名，便于对齐阅读。
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

// ParseLevel 解析级别字符串（非法值回退为 info）。
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug", "trace":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error", "fatal":
		return LevelError
	default:
		return LevelInfo
	}
}

// Entry 是一条日志的结构化表示（也会推送给前端）。
//
// Seq 是进程内递增序号，唯一标识一条日志：实时推送（sink）与历史回填（Tail）
// 携带同一序号，前端据此去重，不会把同一行插入两次。
type Entry struct {
	Seq     uint64 `json:"seq"`
	At      int64  `json:"at"`
	Level   string `json:"level"`
	Module  string `json:"module"`
	Message string `json:"message"`
}

// Interface 是领域包依赖的最小日志接口。
//
// 约定：实现必须可被多个 goroutine 并发调用。
type Interface interface {
	Debug(module, format string, args ...any)
	Info(module, format string, args ...any)
	Warn(module, format string, args ...any)
	Error(module, format string, args ...any)
}

// nopLogger 是零依赖的空实现。
type nopLogger struct{}

func (nopLogger) Debug(string, string, ...any) {}
func (nopLogger) Info(string, string, ...any)  {}
func (nopLogger) Warn(string, string, ...any)  {}
func (nopLogger) Error(string, string, ...any) {}

// Nop 返回空日志器（未注入时的默认值）。
func Nop() Interface { return nopLogger{} }

// Or 返回非空日志器：nil 时回退到 Nop。
func Or(l Interface) Interface {
	if l == nil {
		return Nop()
	}
	return l
}

const (
	// memBuffer 是内存环形缓冲的容量。
	memBuffer = 800
	// defaultMaxBytes 是单个日志文件的默认大小上限（超过则滚动）。
	defaultMaxBytes = 8 << 20
)

// Logger 是文件 + 内存 + 事件三路输出的日志器。
type Logger struct {
	mu       sync.Mutex
	dir      string
	level    Level
	keepDays int
	maxBytes int64

	file    *os.File
	day     string
	seq     int // 当日日志文件滚动序号（与 Entry.Seq 无关）
	written int64
	logSeq  uint64 // 每写入一条日志递增，作为 Entry.Seq

	ring []Entry
	head int
	size int

	sink func(Entry)

	stdout io.Writer

	disabled bool
}

// Options 是日志器构造参数。
type Options struct {
	Dir      string
	Level    string
	KeepDays int
	MaxBytes int64
	// Stdout 非空时同时输出到该 writer（开发模式下常用 os.Stdout）。
	Stdout io.Writer
}

// New 创建日志器并在目录下按天写文件。
func New(opts Options) (*Logger, error) {
	if opts.Dir == "" {
		return nil, fmt.Errorf("日志目录为空")
	}
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return nil, err
	}
	l := &Logger{
		dir:      opts.Dir,
		level:    ParseLevel(opts.Level),
		keepDays: opts.KeepDays,
		maxBytes: opts.MaxBytes,
		ring:     make([]Entry, memBuffer),
		stdout:   opts.Stdout,
	}
	if l.maxBytes <= 0 {
		l.maxBytes = defaultMaxBytes
	}
	if err := l.rotateLocked(time.Now()); err != nil {
		return nil, err
	}
	l.cleanup()
	return l, nil
}

// Discard 返回一个不写任何地方的日志器（初始化失败时兜底，仍保留内存缓冲）。
func Discard() *Logger {
	return &Logger{disabled: true, ring: make([]Entry, memBuffer), maxBytes: defaultMaxBytes}
}

// SetLevel 动态调整级别（设置页修改时调用）。
func (l *Logger) SetLevel(level string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.level = ParseLevel(level)
	l.mu.Unlock()
}

// Level 返回当前级别名。
func (l *Logger) Level() string {
	if l == nil {
		return "info"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.TrimSpace(l.level.String())
}

// SetSink 注册实时日志订阅者（推送给 UI）；传 nil 取消订阅。
func (l *Logger) SetSink(fn func(Entry)) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.sink = fn
	l.mu.Unlock()
}

// Debug 输出调试日志。
func (l *Logger) Debug(module, format string, args ...any) {
	l.log(LevelDebug, module, format, args...)
}

// Info 输出信息日志。
func (l *Logger) Info(module, format string, args ...any) { l.log(LevelInfo, module, format, args...) }

// Warn 输出警告日志。
func (l *Logger) Warn(module, format string, args ...any) { l.log(LevelWarn, module, format, args...) }

// Error 输出错误日志。
func (l *Logger) Error(module, format string, args ...any) {
	l.log(LevelError, module, format, args...)
}

// Tail 返回内存中最近 n 条日志。
func (l *Logger) Tail(n int) []Entry {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > l.size {
		n = l.size
	}
	out := make([]Entry, 0, n)
	for i := 0; i < n; i++ {
		idx := (l.head - n + i + memBuffer*2) % memBuffer
		out = append(out, l.ring[idx])
	}
	return out
}

// Dir 返回日志目录。
func (l *Logger) Dir() string {
	if l == nil {
		return ""
	}
	return l.dir
}

// Files 返回当前保留的日志文件（按修改时间倒序）。
func (l *Logger) Files() []string {
	if l == nil {
		return nil
	}
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil
	}
	type item struct {
		path string
		mod  time.Time
	}
	var items []item
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, item{path: filepath.Join(l.dir, e.Name()), mod: info.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mod.After(items[j].mod) })
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.path)
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
	if l == nil || level < l.levelValue() {
		return
	}
	message := format
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	}
	now := time.Now()
	entry := Entry{
		At:      now.UnixMilli(),
		Level:   strings.TrimSpace(level.String()),
		Module:  module,
		Message: message,
	}

	l.mu.Lock()
	l.logSeq++
	entry.Seq = l.logSeq
	l.pushRingLocked(entry)
	sink := l.sink
	stdout := l.stdout
	if !l.disabled {
		l.writeLocked(now, level, module, message)
	}
	l.mu.Unlock()

	if stdout != nil {
		_, _ = io.WriteString(stdout, formatLine(now, level, module, message))
	}
	if sink != nil {
		sink(entry)
	}
}

// levelValue 读取级别（加锁版本，供 log 内部使用）。
func (l *Logger) levelValue() Level {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.level
}

func (l *Logger) pushRingLocked(entry Entry) {
	l.ring[l.head] = entry
	l.head = (l.head + 1) % memBuffer
	if l.size < memBuffer {
		l.size++
	}
}

func (l *Logger) writeLocked(now time.Time, level Level, module, message string) {
	day := now.Format("20060102")
	if l.file != nil && (day != l.day || l.written >= l.maxBytes) {
		_ = l.file.Close()
		l.file = nil
		if day != l.day {
			l.seq = 0
		}
	}
	if l.file == nil {
		if err := l.rotateLocked(now); err != nil {
			return
		}
	}
	line := formatLine(now, level, module, message)
	n, err := io.WriteString(l.file, line)
	if err == nil {
		l.written += int64(n)
	}
}

// rotateLocked 打开（或滚动到）新的日志文件。
func (l *Logger) rotateLocked(now time.Time) error {
	l.day = now.Format("20060102")
	name := "app-" + l.day + ".log"
	if l.seq > 0 {
		name = fmt.Sprintf("app-%s.%d.log", l.day, l.seq)
	}
	path := filepath.Join(l.dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	info, statErr := f.Stat()
	if statErr == nil {
		l.written = info.Size()
	}
	// 打开时已超限：直接进入下一个序号文件
	if l.written >= l.maxBytes {
		_ = f.Close()
		l.seq++
		return l.rotateLocked(now)
	}
	l.file = f
	return nil
}

// cleanup 删除超过保留天数的日志文件。
func (l *Logger) cleanup() {
	if l.keepDays <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -l.keepDays)
	for _, path := range l.Files() {
		info, err := os.Stat(path)
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		_ = os.Remove(path)
	}
}

func formatLine(at time.Time, level Level, module, message string) string {
	return fmt.Sprintf("%s [%s] %-14s %s\n",
		at.Format("2006-01-02 15:04:05.000"), level, module, message)
}
