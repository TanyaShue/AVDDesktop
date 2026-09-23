package avd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"AVDDesktop/internal/domain"
)

// fullConfig 是一份贴近官方 avdmanager 输出的 config.ini（含注释与未知键）。
const fullConfig = `# 由 avdmanager 生成的配置
AvdId=Dev1
PlayStore.enabled=no
abi.type=x86_64
disk.dataPartition.size=6G
hw.cpu.arch=x86_64
hw.cpu.ncore=4
hw.device.name=medium_phone
hw.lcd.density=420
hw.lcd.height=2400
hw.lcd.width=1080
hw.ramSize=2048
image.sysdir.1=system-images/android-34/google_apis/x86_64/
showDeviceFrame=yes
skin.dynamic=yes
skin.name=1080x2400
skin.path=_no_skin
tag.id=google_apis
target=android-34
vm.heapSize=256
`

func fullHardware() *domain.AvdHardware {
	return &domain.AvdHardware{
		RAMMB:           4096,
		HeapMB:          512,
		CPUCores:        6,
		LCDWidth:        720,
		LCDHeight:       1280,
		LCDDensity:      320,
		DataPartitionMB: 4096,
		SDCardMB:        8192,
	}
}

func TestValidateHardware(t *testing.T) {
	if err := ValidateHardware(nil); err != nil {
		t.Fatalf("nil 应表示沿用设备档案默认值：%v", err)
	}
	if err := ValidateHardware(&domain.AvdHardware{}); err != nil {
		t.Fatalf("全零值应合法：%v", err)
	}
	if err := ValidateHardware(fullHardware()); err != nil {
		t.Fatalf("合法参数被拒绝：%v", err)
	}

	cases := []struct {
		name string
		hw   domain.AvdHardware
		want string
	}{
		{"内存过小", domain.AvdHardware{RAMMB: 128}, "内存"},
		{"内存过大", domain.AvdHardware{RAMMB: 99999}, "内存"},
		{"堆过大", domain.AvdHardware{HeapMB: 8192}, "VM 堆"},
		{"核心数为 0 以外的负数", domain.AvdHardware{CPUCores: -1}, "CPU 核心"},
		{"核心数过多", domain.AvdHardware{CPUCores: 64}, "CPU 核心"},
		{"宽度过小", domain.AvdHardware{LCDWidth: 100, LCDHeight: 1280}, "屏幕宽度"},
		{"高度过大", domain.AvdHardware{LCDWidth: 720, LCDHeight: 99999}, "屏幕高度"},
		{"密度过小", domain.AvdHardware{LCDDensity: 20}, "屏幕密度"},
		{"数据分区过小", domain.AvdHardware{DataPartitionMB: 100}, "数据分区"},
		{"SD 卡过大", domain.AvdHardware{SDCardMB: 999999}, "SD 卡"},
		{"只有宽度", domain.AvdHardware{LCDWidth: 720}, "同时给出宽度和高度"},
		{"只有高度", domain.AvdHardware{LCDHeight: 1280}, "同时给出宽度和高度"},
	}
	for _, c := range cases {
		err := ValidateHardware(&c.hw)
		if err == nil {
			t.Fatalf("%s：应当报错", c.name)
		}
		var appErr *domain.AppError
		if !errors.As(err, &appErr) || appErr.Code != domain.CodeInvalidArgument {
			t.Fatalf("%s：错误码应为 INVALID_ARGUMENT，实际 %v", c.name, err)
		}
		if !strings.Contains(appErr.Message, c.want) {
			t.Fatalf("%s：错误信息 %q 应包含 %q", c.name, appErr.Message, c.want)
		}
	}
}

func TestDescribeHardware(t *testing.T) {
	if got := DescribeHardware(nil); got != "" {
		t.Fatalf("nil 应返回空串，实际 %q", got)
	}
	got := DescribeHardware(fullHardware())
	for _, want := range []string{"内存 4096 MB", "VM 堆 512 MB", "CPU 核心 6 核",
		"分辨率 720×1280 @ 320 dpi", "数据分区 4G", "SD 卡 8G"} {
		if !strings.Contains(got, want) {
			t.Errorf("摘要 %q 应包含 %q", got, want)
		}
	}
	// 只有密度时不应出现"分辨率"
	only := DescribeHardware(&domain.AvdHardware{LCDDensity: 240})
	if strings.Contains(only, "分辨率") || !strings.Contains(only, "屏幕密度 240 dpi") {
		t.Errorf("只有密度时的摘要错误: %q", only)
	}
}

func TestApplyHardwareUpdatesConfig(t *testing.T) {
	s, home, _ := newTestStore(t)
	writeAvd(t, home, "Dev1", "", fullConfig)

	if err := s.ApplyHardware("Dev1", fullHardware()); err != nil {
		t.Fatalf("ApplyHardware 失败: %v", err)
	}
	config, err := s.ReadConfig("Dev1")
	if err != nil {
		t.Fatalf("ReadConfig 失败: %v", err)
	}
	for k, want := range map[string]string{
		"hw.ramSize":              "4096",
		"vm.heapSize":             "512",
		"hw.cpu.ncore":            "6",
		"hw.lcd.width":            "720",
		"hw.lcd.height":           "1280",
		"hw.lcd.density":          "320",
		"disk.dataPartition.size": "4096M",
		"sdcard.size":             "8192M",
		"hw.sdCard":               "yes",
		// 分辨率变了，皮肤名要跟上；_no_skin 保持不变（hw.lcd.* 才会生效）
		"skin.name": "720x1280",
		"skin.path": "_no_skin",
		// 未涉及的键原样保留
		"target":          "android-34",
		"tag.id":          "google_apis",
		"abi.type":        "x86_64",
		"showDeviceFrame": "yes",
	} {
		if config[k] != want {
			t.Errorf("config[%q] = %q，期望 %q", k, config[k], want)
		}
	}

	// 注释与键顺序原样保留（只就地改值，不做重排）。
	raw, err := os.ReadFile(filepath.Join(home, "Dev1.avd", "config.ini"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.HasPrefix(text, "# 由 avdmanager 生成的配置\n") {
		t.Errorf("注释应保留在文件开头，实际:\n%s", text)
	}
	if strings.Index(text, "hw.ramSize=4096") < strings.Index(text, "disk.dataPartition.size=4096M") {
		t.Errorf("原地更新不应改变键的顺序:\n%s", text)
	}
	// 写回后必须以单个换行结尾；新增的键追加在末尾（已有键顺序不变）
	if !strings.HasSuffix(text, "sdcard.size=8192M\nhw.sdCard=yes\n") {
		t.Errorf("新增键应追加在末尾且文件以换行结尾，实际:\n%s", text)
	}
	if strings.Contains(text, "\r") || strings.HasSuffix(text, "\n\n") {
		t.Errorf("文件换行格式不正确: %q", text)
	}

	// 幂等：重复应用不应不断追加内容
	if err := s.ApplyHardware("Dev1", fullHardware()); err != nil {
		t.Fatalf("第二次 ApplyHardware 失败: %v", err)
	}
	again, err := os.ReadFile(filepath.Join(home, "Dev1.avd", "config.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != text {
		t.Errorf("重复应用不应改变内容:\n第一次:\n%s\n第二次:\n%s", text, string(again))
	}

	// 设备摘要应带出生效的硬件参数（界面直接展示）
	items, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("期望 1 个设备，实际 %d", len(items))
	}
	got := items[0]
	if got.RAMMB != 4096 || got.CPUCores != 6 || got.LCDWidth != 720 || got.LCDHeight != 1280 {
		t.Fatalf("摘要硬件字段错误: %+v", got)
	}
}

// TestApplyHardwareForcesNoSkin 覆盖档案自带真实皮肤时的处理：
// 模拟器使用 skin.path，不改它的话请求的分辨率不会生效。
func TestApplyHardwareForcesNoSkin(t *testing.T) {
	s, home, _ := newTestStore(t)
	writeAvd(t, home, "Skinned", "", "abi.type=x86_64\nhw.device.name=pixel\n"+
		"skin.dynamic=no\nskin.path=skins/pixel_2\nskin.name=pixel_2\n"+
		"image.sysdir.1=system-images/android-34/google_apis/x86_64/\ntarget=android-34\n")

	if err := s.ApplyHardware("Skinned", &domain.AvdHardware{LCDWidth: 1080, LCDHeight: 2400}); err != nil {
		t.Fatalf("ApplyHardware 失败: %v", err)
	}
	config, err := s.ReadConfig("Skinned")
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"hw.lcd.width":  "1080",
		"hw.lcd.height": "2400",
		"skin.path":     "_no_skin",
		"skin.dynamic":  "yes",
		"skin.name":     "1080x2400",
	} {
		if config[k] != want {
			t.Errorf("config[%q] = %q，期望 %q", k, config[k], want)
		}
	}

	// 没有皮肤配置的档案：只写 hw.lcd.*，不凭空造出皮肤名
	writeAvd(t, home, "Plain", "", "abi.type=x86_64\nimage.sysdir.1=system-images/android-34/google_apis/x86_64/\n")
	if err := s.ApplyHardware("Plain", &domain.AvdHardware{LCDWidth: 800, LCDHeight: 1280}); err != nil {
		t.Fatalf("ApplyHardware 失败: %v", err)
	}
	plain, err := s.ReadConfig("Plain")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain["skin.name"]; ok {
		t.Errorf("无皮肤配置不应新增 skin.name：%v", plain["skin.name"])
	}
	if _, ok := plain["skin.path"]; ok {
		t.Errorf("无皮肤配置不应新增 skin.path：%v", plain["skin.path"])
	}
	if plain["hw.lcd.width"] != "800" || plain["hw.lcd.height"] != "1280" {
		t.Errorf("分辨率未写入：%v", plain)
	}
}

// TestApplyHardwareAppendsMissingKeys 覆盖旧配置缺少硬件键的情况：
// 新键追加到末尾，已有内容（含 CRLF 文件）保持不变。
func TestApplyHardwareAppendsMissingKeys(t *testing.T) {
	s, home, _ := newTestStore(t)
	writeAvd(t, home, "Old", "", "abi.type=x86_64\r\nhw.device.name=medium_phone\r\n"+
		"image.sysdir.1=system-images/android-34/google_apis/x86_64/\r\ntarget=android-34\r\n")

	if err := s.ApplyHardware("Old", &domain.AvdHardware{RAMMB: 2048, CPUCores: 2}); err != nil {
		t.Fatalf("ApplyHardware 失败: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "Old.avd", "config.ini"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "hw.ramSize=2048\nhw.cpu.ncore=2\n") {
		t.Errorf("缺失的键应追加到末尾:\n%s", text)
	}
	if strings.Contains(text, "\r") {
		t.Errorf("写回的文件应为统一换行（无 CR）:\n%q", text)
	}
	config, err := s.ReadConfig("Old")
	if err != nil {
		t.Fatal(err)
	}
	if config["abi.type"] != "x86_64" || config["target"] != "android-34" {
		t.Errorf("原有键被破坏：%v", config)
	}
}

func TestApplyHardwareNoopAndErrors(t *testing.T) {
	s, home, _ := newTestStore(t)

	// nil / 全零：不读文件也不报错（连 config.ini 不存在也应安全）
	writeAvd(t, home, "NoConfig", "", "")
	if err := s.ApplyHardware("NoConfig", nil); err != nil {
		t.Fatalf("nil 覆盖项应为空操作：%v", err)
	}
	if err := s.ApplyHardware("NoConfig", &domain.AvdHardware{}); err != nil {
		t.Fatalf("全零覆盖项应为空操作：%v", err)
	}

	// 设备不存在
	err := s.ApplyHardware("Missing", &domain.AvdHardware{RAMMB: 2048})
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Code != domain.CodeAvdNotFound {
		t.Fatalf("设备不存在应返回 AVD_NOT_FOUND，实际 %v", err)
	}

	// 设备存在但 config.ini 缺失
	writeAvd(t, home, "Broken", "", "")
	err = s.ApplyHardware("Broken", &domain.AvdHardware{RAMMB: 2048})
	if !errors.As(err, &appErr) || appErr.Code != domain.CodePathNotFound {
		t.Fatalf("缺 config.ini 应返回 PATH_NOT_FOUND，实际 %v", err)
	}
}

// TestApplyIniChanges 直接覆盖文本合并逻辑的边界：重复键、对齐空格、注释与行尾。
func TestApplyIniChanges(t *testing.T) {
	in := "# 注释\r\nhw.ramSize = 2048\r\n\r\nunknown.key=keep\r\n"
	got := applyIniChanges(in, []iniChange{
		{key: "hw.ramSize", value: "4096"},
		{key: "hw.lcd.width", value: "720"},
		{key: "hw.lcd.height", value: "1280"},
		{key: "hw.lcd.width", value: "800"}, // 同一键重复时以最后一次为准
	})
	want := "# 注释\nhw.ramSize =4096\n\nunknown.key=keep\nhw.lcd.width=800\nhw.lcd.height=1280\n"
	if got != want {
		t.Fatalf("合并结果错误:\n实际: %q\n期望: %q", got, want)
	}
}
