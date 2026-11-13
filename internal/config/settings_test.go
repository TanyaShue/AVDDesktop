package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Load 必须修复手工编辑出来的非法取值：否则非法值一直留在内存里，
// 之后每次 Update 都会在 validate 处失败，设置页任何字段都保存不了。
func TestLoadRepairsInvalidValuesAndKeepsUpdateWorking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	raw := `{"version":1,"theme":"blue","logLevel":"verbose","keepLogDays":-3,` +
		`"jdkMirrorSourceId":"","mirrorSourceId":"nju"}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewManager(path)
	if err := m.Load(); err != nil {
		t.Fatalf("Load 返回错误：%v", err)
	}

	got := m.Get()
	def := Defaults()
	if got.Theme != def.Theme {
		t.Errorf("theme = %q，非法值应回退为默认 %q", got.Theme, def.Theme)
	}
	if got.LogLevel != def.LogLevel {
		t.Errorf("logLevel = %q，非法值应回退为默认 %q", got.LogLevel, def.LogLevel)
	}
	if got.KeepLogDays != def.KeepLogDays {
		t.Errorf("keepLogDays = %d，非法值应回退为默认 %d", got.KeepLogDays, def.KeepLogDays)
	}
	if got.JDKMirrorSourceID != def.JDKMirrorSourceID {
		t.Errorf("jdkMirrorSourceId = %q，空值应回退为默认 %q", got.JDKMirrorSourceID, def.JDKMirrorSourceID)
	}
	// 合法字段必须原样保留（不能因为修非法值把用户设置一起丢掉）。
	if got.MirrorSourceID != "nju" {
		t.Errorf("mirrorSourceId = %q，合法字段应保留", got.MirrorSourceID)
	}
	if _, err := m.Update(map[string]any{"theme": "dark"}); err != nil {
		t.Fatalf("非法设置文件加载后 Update 仍应可用，实际报错：%v", err)
	}
}

// Update 必须拒绝非法补丁（而不是像 Load 那样静默修正）。
func TestUpdateRejectsInvalidPatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	m := NewManager(path)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	before := m.Get()

	if _, err := m.Update(map[string]any{"theme": "blue"}); err == nil {
		t.Fatal("Update 应拒绝非法主题")
	}
	if _, err := m.Update(map[string]any{"keepLogDays": 999}); err == nil {
		t.Fatal("Update 应拒绝越界的日志保留天数")
	}
	if after := m.Get(); after != before {
		t.Errorf("失败的 Update 不得改动内存设置：%+v → %+v", before, after)
	}
}

// 损坏的设置文件必须被备份，且备份失败时绝不覆盖原文件。
func TestLoadBacksUpCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewManager(path)
	if err := m.Load(); err != nil {
		t.Fatalf("Load 返回错误：%v", err)
	}

	backup, err := os.ReadFile(path + ".corrupt")
	if err != nil {
		t.Fatalf("损坏文件应被备份到 .corrupt：%v", err)
	}
	if !strings.Contains(string(backup), "not json") {
		t.Errorf("备份内容 = %q，应保留原始内容", string(backup))
	}
	if got := m.Get(); got != Defaults() {
		t.Errorf("损坏文件加载后应为默认设置，实际 %+v", got)
	}
}
