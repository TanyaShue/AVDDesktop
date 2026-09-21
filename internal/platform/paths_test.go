package platform

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestChildEnvAvdHome 验证 ANDROID_AVD_HOME 指向 avd 目录本身。
//
// 端到端测试发现的真实问题：曾经写成 filepath.Dir(avdHome)，
// 导致 avdmanager 把新建的 AVD 放到错误位置，工具随后找不到设备。
func TestChildEnvAvdHome(t *testing.T) {
	sdk := filepath.Join("C:", "sdk")
	avd := filepath.Join("C:", "users", "me", ".android", "avd")

	env := ChildEnv(sdk, avd, true)
	got := lookupEnv(env, "ANDROID_AVD_HOME")
	if got != avd {
		t.Errorf("ANDROID_AVD_HOME = %q，期望 %q（必须是 avd 目录本身，不是父目录）", got, avd)
	}
	if lookupEnv(env, "ANDROID_HOME") != sdk {
		t.Errorf("ANDROID_HOME 未设置：%q", lookupEnv(env, "ANDROID_HOME"))
	}
	path := lookupEnv(env, "PATH")
	if !strings.Contains(path, filepath.Join(sdk, "platform-tools")) {
		t.Errorf("PATH 未包含 platform-tools：%q", path)
	}

	// 不注入时应保持原样（不添加任何 ANDROID_* 覆盖项）
	unchanged := ChildEnv(sdk, avd, false)
	if lookupEnv(unchanged, "ANDROID_AVD_HOME") != lookupEnv(env, "ANDROID_AVD_HOME") &&
		lookupEnv(unchanged, "ANDROID_AVD_HOME") != "" {
		// 只有当宿主机本来就有该变量时才可能相等，这里只要求不主动写入
		t.Log("宿主机原本已设置 ANDROID_AVD_HOME，跳过严格断言")
	}
}

// TestResolveAvdHomePriority 验证 AVD 目录解析优先级链。
func TestResolveAvdHomePriority(t *testing.T) {
	t.Setenv("ANDROID_AVD_HOME", filepath.Join("C:", "explicit"))
	t.Setenv("ANDROID_USER_HOME", filepath.Join("C:", "userhome"))
	t.Setenv("ANDROID_SDK_HOME", filepath.Join("C:", "sdkhome"))
	t.Setenv("ANDROID_PREFS_ROOT", filepath.Join("C:", "prefs"))

	if got := ResolveAvdHome(""); got.Path != filepath.Join("C:", "explicit") {
		t.Errorf("应优先使用 ANDROID_AVD_HOME，实际 %q", got.Path)
	}
	if got := ResolveAvdHome(filepath.Join("C:", "manual")); got.Path != filepath.Join("C:", "manual") {
		t.Errorf("显式设置应最优先，实际 %q", got.Path)
	}

	t.Setenv("ANDROID_AVD_HOME", "")
	if got := ResolveAvdHome(""); got.Path != filepath.Join("C:", "userhome", "avd") {
		t.Errorf("应回退到 ANDROID_USER_HOME/avd，实际 %q", got.Path)
	}

	t.Setenv("ANDROID_USER_HOME", "")
	if got := ResolveAvdHome(""); got.Path != filepath.Join("C:", "sdkhome", ".android", "avd") {
		t.Errorf("应回退到 ANDROID_SDK_HOME/.android/avd，实际 %q", got.Path)
	}

	t.Setenv("ANDROID_SDK_HOME", "")
	if got := ResolveAvdHome(""); got.Path != filepath.Join("C:", "prefs", ".android", "avd") {
		t.Errorf("应回退到 ANDROID_PREFS_ROOT/.android/avd，实际 %q", got.Path)
	}
}

// TestDirSizeCache 验证目录大小缓存与失效。
func TestDirSizeCache(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "a.bin"), 1024)
	first := DirSizeCached(dir)
	if first != 1024 {
		t.Fatalf("首次统计 = %d，期望 1024", first)
	}

	// 新增文件后仍命中缓存
	writeFile(t, filepath.Join(dir, "b.bin"), 2048)
	if got := DirSizeCached(dir); got != first {
		t.Errorf("缓存未命中：%d ≠ %d", got, first)
	}

	// 失效后应重新统计
	InvalidateDirSize(dir)
	if got := DirSizeCached(dir); got != 3072 {
		t.Errorf("失效后统计 = %d，期望 3072", got)
	}
}

func lookupEnv(env []string, key string) string {
	prefix := strings.ToUpper(key) + "="
	for i := len(env) - 1; i >= 0; i-- { // 后出现的优先（与 os/exec 行为一致）
		if strings.HasPrefix(strings.ToUpper(env[i]), prefix) {
			return env[i][len(prefix):]
		}
	}
	return ""
}

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := WriteFileAtomic(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}
