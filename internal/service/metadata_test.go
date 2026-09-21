package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"AVDDesktop/internal/config"
	"AVDDesktop/internal/logging"
	"AVDDesktop/internal/sdk/localrepo"
	"AVDDesktop/internal/sdk/query"
)

// TestRepairSDKMetadata 验证 service 层能在创建前补写缺失的 package.xml。
//
// 场景：本项目安装器解压官方归档后目录里只有 source.properties，
// 官方工具（sdkmanager/avdmanager/Android Studio）看不到这些包，
// avdmanager 会以 `"emulator" package must be installed!` 失败。
func TestRepairSDKMetadata(t *testing.T) {
	tmp := t.TempDir()
	sdkRoot := filepath.Join(tmp, "sdk")
	writeTestFile(t, filepath.Join(sdkRoot, "emulator", "source.properties"),
		"Pkg.Revision=37.1.11\nPkg.Path=emulator\nPkg.Desc=Android Emulator\n")
	// 已有 package.xml 的包不能被改写
	existing := filepath.Join(sdkRoot, "platform-tools", localrepo.FileName)
	writeTestFile(t, filepath.Join(sdkRoot, "platform-tools", "source.properties"), "Pkg.Revision=37.0.1\n")
	writeTestFile(t, existing, "<!-- sentinel: 官方写入，不应被动 -->\n")

	settings := config.NewManager(filepath.Join(tmp, "settings.json"))
	if err := settings.Load(); err != nil {
		t.Fatalf("加载设置失败: %v", err)
	}
	if _, err := settings.Update(map[string]any{"sdkRoot": sdkRoot, "avdHome": filepath.Join(tmp, "avd")}); err != nil {
		t.Fatalf("写入设置失败: %v", err)
	}
	rt := NewRuntime("test", "test", settings, filepath.Join(tmp, "cache"), logging.Discard())
	t.Cleanup(rt.Shutdown)

	if missing := query.NewScanner(rt.Components().Paths.SdkRoot).MetadataMissing(); len(missing) != 1 || missing[0] != "emulator" {
		t.Fatalf("修复前缺失列表 = %v，期望 [emulator]", missing)
	}

	fixed := rt.repairSDKMetadata(context.Background(), nil)
	if len(fixed) != 1 || fixed[0] != "emulator" {
		t.Fatalf("修复结果 = %v，期望 [emulator]", fixed)
	}

	path := filepath.Join(sdkRoot, "emulator", localrepo.FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("emulator/package.xml 未生成: %v", err)
	}
	for _, want := range []string{
		`path="emulator"`,
		"<major>37</major>",
		"<minor>1</minor>",
		"<micro>11</micro>",
		"Android Emulator",
		`xsi:type="ns5:genericDetailsType"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("package.xml 缺少 %q：\n%s", want, data)
		}
	}

	if got, _ := os.ReadFile(existing); !strings.Contains(string(got), "sentinel") {
		t.Error("已有 package.xml 的包被改写了")
	}
	// 幂等：再次调用不应重复修复
	if again := rt.repairSDKMetadata(context.Background(), nil); len(again) != 0 {
		t.Errorf("重复修复应当无事可做，实际 %v", again)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
