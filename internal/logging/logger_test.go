package logging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLevelFilteringAndRing(t *testing.T) {
	dir := t.TempDir()
	l, err := New(Options{Dir: dir, Level: "info", KeepDays: 7, MaxBytes: defaultMaxBytes})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	l.Debug("test", "调试消息应当被过滤：%d", 1)
	l.Info("detect", "开始自检 %s", "ok")
	l.Warn("download", "重试第 %d 次", 2)
	l.Error("install", "安装失败")

	ring := l.Tail(10)
	if len(ring) != 3 {
		t.Fatalf("内存缓冲应有 3 条（debug 被过滤），实际 %d：%+v", len(ring), ring)
	}
	if ring[0].Level != "INFO" || ring[2].Level != "ERROR" {
		t.Errorf("级别记录不正确：%+v", ring)
	}
	if ring[1].Module != "download" {
		t.Errorf("模块名不正确：%q", ring[1].Module)
	}

	data, err := os.ReadFile(filepath.Join(dir, "app-"+time.Now().Format("20060102")+".log"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, "调试消息") {
		t.Error("debug 级别不应写入文件")
	}
	for _, want := range []string{"开始自检 ok", "重试第 2 次", "安装失败"} {
		if !strings.Contains(content, want) {
			t.Errorf("日志文件缺少 %q\n%s", want, content)
		}
	}
}

func TestSetLevelTakesEffectImmediately(t *testing.T) {
	l, err := New(Options{Dir: t.TempDir(), Level: "error"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	l.Info("test", "不应出现")
	if got := len(l.Tail(10)); got != 0 {
		t.Fatalf("error 级别下 info 不应记录，实际 %d 条", got)
	}
	l.SetLevel("debug")
	l.Info("test", "现在应出现")
	if got := len(l.Tail(10)); got != 1 {
		t.Fatalf("切换级别后应有 1 条，实际 %d 条", got)
	}
	if l.Level() != "DEBUG" {
		t.Errorf("Level() = %q", l.Level())
	}
}

func TestSinkReceivesEntries(t *testing.T) {
	l, err := New(Options{Dir: t.TempDir(), Level: "info"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	var mu sync.Mutex
	var got []Entry
	l.SetSink(func(e Entry) {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	})

	l.Info("mirror", "测速完成")
	l.Warn("speedtest", "连接失败")

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("Sink 应收到 2 条，实际 %d", len(got))
	}
	if got[0].Module != "mirror" || got[1].Level != "WARN" {
		t.Errorf("Sink 内容不正确：%+v", got)
	}
	if got[0].At == 0 {
		t.Error("Sink 应带时间戳")
	}
}

func TestEntrySeqMatchesBetweenSinkAndTail(t *testing.T) {
	// 前端控制台靠 Entry.Seq 去重：同一行的实时推送（Sink）与历史回填（Tail）
	// 必须带同一个序号，否则启动瞬间会重复插入同一行。
	l, err := New(Options{Dir: t.TempDir(), Level: "info"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	var mu sync.Mutex
	var pushed []Entry
	l.SetSink(func(e Entry) {
		mu.Lock()
		pushed = append(pushed, e)
		mu.Unlock()
	})

	for i := 0; i < 3; i++ {
		l.Info("boot", "启动 %d", i)
	}

	ring := l.Tail(10)
	if len(ring) != 3 {
		t.Fatalf("Tail 应返回 3 条，实际 %d", len(ring))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(pushed) != len(ring) {
		t.Fatalf("Sink 与 Tail 条数不一致：%d / %d", len(pushed), len(ring))
	}
	seen := make(map[uint64]bool, len(ring))
	for i, e := range ring {
		if e.Seq == 0 {
			t.Fatalf("第 %d 条缺少序号：%+v", i, e)
		}
		if seen[e.Seq] {
			t.Fatalf("序号重复：%d", e.Seq)
		}
		seen[e.Seq] = true
		if pushed[i].Seq != e.Seq {
			t.Fatalf("实时推送与历史回填序号不一致：%+v / %+v", pushed[i], e)
		}
		if i > 0 && ring[i-1].Seq >= e.Seq {
			t.Fatalf("序号应递增：%d → %d", ring[i-1].Seq, e.Seq)
		}
	}
}

func TestSizeRotation(t *testing.T) {
	dir := t.TempDir()
	l, err := New(Options{Dir: dir, Level: "info", MaxBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	for i := 0; i < 40; i++ {
		l.Info("rotate", "这是一条用于触发滚动的较长日志消息，序号=%02d", i)
	}
	files := l.Files()
	if len(files) < 2 {
		t.Fatalf("应产生多个日志文件，实际 %d：%v", len(files), files)
	}
	// 最新的文件应当是我们当前写入的那个
	if filepath.Dir(files[0]) != dir {
		t.Errorf("日志文件目录不正确：%s", files[0])
	}
}

func TestNopLoggerIsSafe(t *testing.T) {
	var l Interface = Nop()
	l.Debug("m", "d")
	l.Info("m", "i")
	l.Warn("m", "w")
	l.Error("m", "e")
	Or(nil).Info("m", "nil 也安全")
}

func TestEntryJSONShape(t *testing.T) {
	// 前端统一日志面板按这些键名消费日志，必须断言真实序列化结果，
	// 而不是"给字段赋值再断言等于该值"（那种断言运行期不可能失败）。
	e := Entry{At: 1, Level: "INFO", Module: "detect", Message: "x", Seq: 7}
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"at", "level", "module", "message", "seq"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("日志条目缺少 JSON 字段 %q（前端 useApp.ts / TaskDrawer 依赖它）：%s", key, raw)
		}
	}
}
