package avd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
)

const sampleConfig = `AvdId=Dev1
PlayStore.enabled=no
abi.type=x86_64
avd.ini.displayname=Dev1
avd.ini.encoding=UTF-8
hw.cpu.arch=x86_64
hw.device.name=medium_phone
hw.ramSize=2048
image.sysdir.1=system-images/android-34/google_apis/x86_64/
tag.display=Google APIs
tag.id=google_apis
target=android-34
`

func newTestStore(t *testing.T) (*Store, string, string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "avd")
	sdkRoot := filepath.Join(t.TempDir(), "sdk")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	return New(home, sdkRoot), home, sdkRoot
}

// writeAvd 在 AVD 主目录下写入一个设备的 .ini 与 config.ini。
func writeAvd(t *testing.T, home, name, iniContent, config string) string {
	t.Helper()
	dir := filepath.Join(home, name+".avd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if iniContent == "" {
		iniContent = "avd.ini.encoding=UTF-8\npath=" + dir + "\npath.rel=avd/" + name + ".avd\n"
	}
	if err := os.WriteFile(filepath.Join(home, name+".ini"), []byte(iniContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if config != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.ini"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestValidateName(t *testing.T) {
	s, home, _ := newTestStore(t)

	cases := []struct {
		name    string
		wantOK  bool
		keyword string
	}{
		{"Dev_1.2-3", true, ""},
		{"", false, "不能为空"},
		{"   ", false, "不能为空"},
		{"bad name", false, "只能包含"},
		{"中文名", false, "只能包含"},
		{strings.Repeat("a", 65), false, "只能包含"},
	}
	for _, c := range cases {
		got := s.ValidateName(c.name)
		if got.Valid != c.wantOK {
			t.Errorf("ValidateName(%q).Valid = %v，期望 %v（reason=%q）", c.name, got.Valid, c.wantOK, got.Reason)
		}
		if c.keyword != "" && !strings.Contains(got.Reason, c.keyword) {
			t.Errorf("ValidateName(%q).Reason = %q，应包含 %q", c.name, got.Reason, c.keyword)
		}
	}

	// 重名：应判为非法并给出可用建议
	writeAvd(t, home, "Taken", "", sampleConfig)
	dup := s.ValidateName("Taken")
	if dup.Valid {
		t.Fatal("已存在的名称应判为非法")
	}
	if dup.Suggest != "Taken_2" {
		t.Fatalf("重名建议 = %q，期望 Taken_2", dup.Suggest)
	}
	if !s.ValidateName(dup.Suggest).Valid {
		t.Fatalf("建议的名称应当可用: %q", dup.Suggest)
	}
}

// TestResolveUsesIniPath 覆盖实测结论：.ini 文件名不保证等于 .avd 目录名。
func TestResolveUsesIniPath(t *testing.T) {
	s, home, _ := newTestStore(t)
	realDir := filepath.Join(home, "Medium_Phone.avd")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ini := "avd.ini.encoding=UTF-8\npath=" + realDir + "\n"
	if err := os.WriteFile(filepath.Join(home, "Medium_Phone_API_36.1.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}

	l := s.Resolve("Medium_Phone_API_36.1")
	if l.Dir != realDir {
		t.Fatalf("应跟随 .ini 里的 path：期望 %q，实际 %q", realDir, l.Dir)
	}
	if !l.Exists {
		t.Fatal("目录存在时 Exists 应为 true")
	}
	if l.ConfigIni != filepath.Join(realDir, "config.ini") {
		t.Fatalf("config.ini 路径错误: %q", l.ConfigIni)
	}
	if l.IniPath != filepath.Join(home, "Medium_Phone_API_36.1.ini") {
		t.Fatalf(".ini 路径错误: %q", l.IniPath)
	}
	if !s.Exists("Medium_Phone_API_36.1") {
		t.Fatal("Exists 应为 true")
	}
}

// TestResolveRelativePath 覆盖 path.rel 的相对路径解析。
func TestResolveRelativePath(t *testing.T) {
	s, home, _ := newTestStore(t)
	writeAvd(t, home, "Rel", "avd.ini.encoding=UTF-8\npath.rel=avd/Rel.avd\n", sampleConfig)

	l := s.Resolve("Rel")
	if l.Dir != filepath.Join(home, "avd", "Rel.avd") {
		t.Fatalf("相对路径应基于 AVD 主目录解析: %q", l.Dir)
	}
	if l.Exists {
		t.Fatal("该目录并不存在，Exists 应为 false")
	}

	// 既没有 path 也没有 path.rel 时退化为 <name>.avd
	writeAvd(t, home, "Plain", "avd.ini.encoding=UTF-8\n", "")
	if got := s.Resolve("Plain").Dir; got != filepath.Join(home, "Plain.avd") {
		t.Fatalf("退化路径错误: %q", got)
	}
}

func TestReadConfigAndList(t *testing.T) {
	s, home, sdkRoot := newTestStore(t)
	writeAvd(t, home, "Dev1", "", sampleConfig)
	// 镜像目录存在 → 不应报告 Broken
	if err := os.MkdirAll(filepath.Join(sdkRoot, "system-images", "android-34", "google_apis", "x86_64"), 0o755); err != nil {
		t.Fatal(err)
	}

	config, err := s.ReadConfig("Dev1")
	if err != nil {
		t.Fatalf("ReadConfig 失败: %v", err)
	}
	for k, want := range map[string]string{
		"target":         "android-34",
		"tag.id":         "google_apis",
		"abi.type":       "x86_64",
		"hw.device.name": "medium_phone",
	} {
		if config[k] != want {
			t.Errorf("config[%q] = %q，期望 %q", k, config[k], want)
		}
	}

	items, err := s.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("期望 1 个设备，实际 %d", len(items))
	}
	got := items[0]
	if got.Name != "Dev1" || got.API != "34" || got.Tag != "google_apis" || got.ABI != "x86_64" {
		t.Fatalf("摘要字段错误: %+v", got)
	}
	if got.DeviceProfileID != "medium_phone" {
		t.Errorf("DeviceProfileID = %q", got.DeviceProfileID)
	}
	if got.Broken != "" {
		t.Errorf("镜像存在时不应报 Broken: %q", got.Broken)
	}
	if got.State != domain.AvdStopped {
		t.Errorf("默认状态应为 stopped，实际 %q", got.State)
	}
}

func TestListReportsBrokenConfig(t *testing.T) {
	s, home, _ := newTestStore(t)
	// 镜像目录不存在
	writeAvd(t, home, "NoImage", "", sampleConfig)
	// config.ini 缺失
	writeAvd(t, home, "NoConfig", "", "")

	items, err := s.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("期望 2 个设备（损坏的也要列出），实际 %d", len(items))
	}
	if items[0].Name != "NoConfig" || items[1].Name != "NoImage" {
		t.Fatalf("应按名称排序: %+v", items)
	}
	if !strings.Contains(items[0].Broken, "config.ini") {
		t.Errorf("缺 config.ini 应标记 Broken，实际 %q", items[0].Broken)
	}
	if !strings.Contains(items[1].Broken, "系统镜像已不存在") {
		t.Errorf("镜像缺失应标记 Broken，实际 %q", items[1].Broken)
	}
}

func TestReadConfigMissing(t *testing.T) {
	s, home, _ := newTestStore(t)
	writeAvd(t, home, "NoConfig", "", "")
	_, err := s.ReadConfig("NoConfig")
	if err == nil {
		t.Fatal("缺 config.ini 应报错")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Code != domain.CodePathNotFound {
		t.Fatalf("错误码应为 PATH_NOT_FOUND，实际 %v", err)
	}
}

func TestDeleteRemovesFiles(t *testing.T) {
	s, home, _ := newTestStore(t)
	dir := writeAvd(t, home, "Gone", "", sampleConfig)

	if err := s.Delete("Gone"); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("设备目录未删除: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "Gone.ini")); !os.IsNotExist(err) {
		t.Errorf(".ini 未删除: %v", err)
	}

	err := s.Delete("Gone")
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Code != domain.CodeAvdNotFound {
		t.Fatalf("删除不存在的设备应返回 AVD_NOT_FOUND，实际 %v", err)
	}
}

// 真实样本（本机 `avdmanager list device` 的前几块）。
const deviceListSample = `Available devices definitions:
id: 0 or "ai_glasses_device"
    Name: AI Glasses
    OEM : Google
    Tag : ai-glasses
---------
id: 1 or "automotive_1024p_landscape"
    Name: Automotive (1024p landscape)
    OEM : Google
    Tag : android-automotive-playstore
---------
id: 12 or "medium_phone"
    Name: Medium Phone
    OEM : Google
    Tag : android-desktop
---------
`

func TestParseDeviceList(t *testing.T) {
	profiles := ParseDeviceList(deviceListSample)
	if len(profiles) != 3 {
		t.Fatalf("期望解析出 3 个档案，实际 %d: %+v", len(profiles), profiles)
	}
	first := profiles[0]
	if first.ID != "ai_glasses_device" || first.Index != 0 || first.Name != "AI Glasses" || first.OEM != "Google" || first.Tag != "ai-glasses" {
		t.Fatalf("首个档案解析错误: %+v", first)
	}
	if profiles[2].ID != "medium_phone" || profiles[2].Index != 12 || profiles[2].Name != "Medium Phone" {
		t.Fatalf("第三个档案解析错误: %+v", profiles[2])
	}
	for _, p := range profiles {
		if p.Name == "" || strings.HasPrefix(p.ID, "id") {
			t.Errorf("解析出异常档案: %+v", p)
		}
	}

	if got := ParseDeviceList(""); len(got) != 0 {
		t.Errorf("空输出应返回空列表，实际 %+v", got)
	}
	if got := ParseDeviceList("something went wrong\n"); len(got) != 0 {
		t.Errorf("非档案输出应返回空列表，实际 %+v", got)
	}
}

func TestEnsureImageSkipsInstalled(t *testing.T) {
	sdkRoot := filepath.Join(t.TempDir(), "sdk")
	tools := platform.NewTools(sdkRoot)
	const pkg = "system-images;android-34;google_apis;x86_64"

	// 未安装：没有 sdkmanager 时应报“工具缺失”，而不是静默成功
	if already, err := EnsureImage(context.Background(), tools, nil, pkg, nil); err == nil || already {
		t.Fatalf("未安装且无 sdkmanager 时应报错，实际 already=%v err=%v", already, err)
	}

	// 目录存在但缺 package.xml（例如手工拷贝）→ 仍应视为未安装
	dir := filepath.Join(sdkRoot, "system-images", "android-34", "google_apis", "x86_64")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if already, err := EnsureImage(context.Background(), tools, nil, pkg, nil); err == nil || already {
		t.Fatalf("缺 package.xml 时应尝试安装，实际 already=%v err=%v", already, err)
	}

	// 目录与 package.xml 都在 → 直接跳过，不调用 sdkmanager
	if err := os.WriteFile(filepath.Join(dir, "package.xml"), []byte("<ns2:repository/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	already, err := EnsureImage(context.Background(), tools, nil, pkg, nil)
	if err != nil {
		t.Fatalf("已安装时不应报错: %v", err)
	}
	if !already {
		t.Fatal("已安装时应返回 alreadyInstalled=true")
	}

	// 非法的镜像路径应报参数错误
	if _, err := EnsureImage(context.Background(), tools, nil, "platform-tools", nil); err == nil {
		t.Fatal("非法镜像路径应报错")
	}
}
